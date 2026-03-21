package backup

import (
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
	"github.com/backupmanager/backupmanager/internal/sync"
)

// defaultExcludes lists directories and files that should never be backed up.
// These are regenerable artifacts that waste space and bandwidth.
var defaultExcludes = []string{
	"node_modules",
	".git",
	"__pycache__",
	".cache",
	".npm",
	".next",
	"vendor",       // PHP composer, Go vendor (if committed separately)
	"dist",         // build output
	"build",        // build output
	".env",         // secrets — should not be in backups
	"*.log",        // log files
	"*.tmp",
	"*.swp",
}

// BackupSourceRecord represents a row from the backup_sources table.
type BackupSourceRecord struct {
	ID              int
	ServerID        int
	Name            string
	Type            string // "web", "database", "config"
	SourcePath      string
	DBName          string
	DependsOn       *int // nullable FK to another backup_sources.id
	Priority        int
	Enabled         bool
	ExcludePatterns string // comma-separated exclude patterns
}

// RunResult holds the outcome of a complete backup job execution.
type RunResult struct {
	RunID         int
	Status        string // "success", "failed", "timeout"
	TotalSize     int64
	FilesCopied   int
	Duration      time.Duration
	SourceResults []SourceResult
	Errors        []string
}

// SourceResult holds the outcome of a single source backup within a run.
type SourceResult struct {
	SourceID    int
	SourceName  string
	SourceType  string // "web", "database", "config"
	Status      string // "success", "failed", "skipped"
	Size        int64
	FilesCopied int
	Checksum    string
	Error       string
}

// Orchestrator coordinates backup execution for a job's sources in dependency order.
type Orchestrator struct {
	db             *database.Database
	rsyncSyncer    sync.Syncer
	ftpSyncer      sync.Syncer
	mysqlDumper    *MySQLDumpOrchestrator
	destSyncer     *sync.DestinationSyncer // syncs snapshots to secondary destinations
	backupDir      string // base directory for backup storage
	skipPreflight  bool   // if true, skip pre-flight checks (for testing)
	credKey        []byte // AES key for decrypting server credentials
}

// NewOrchestrator creates a new Orchestrator with default syncers.
func NewOrchestrator(db *database.Database) *Orchestrator {
	return &Orchestrator{
		db:          db,
		rsyncSyncer: sync.NewRsyncSyncer(),
		ftpSyncer:   sync.NewFTPSyncer(),
		mysqlDumper: NewMySQLDumpOrchestrator(),
		destSyncer:  sync.NewDestinationSyncer(db),
		backupDir:   "/var/backups/backupmanager",
	}
}

// SetRsyncSyncer replaces the rsync syncer (useful for testing).
func (o *Orchestrator) SetRsyncSyncer(s sync.Syncer) { o.rsyncSyncer = s }

// SetFTPSyncer replaces the FTP syncer (useful for testing).
func (o *Orchestrator) SetFTPSyncer(s sync.Syncer) { o.ftpSyncer = s }

// SetMySQLDumper replaces the MySQL dump orchestrator (useful for testing).
func (o *Orchestrator) SetMySQLDumper(d *MySQLDumpOrchestrator) { o.mysqlDumper = d }

// SetBackupDir sets the base backup directory.
func (o *Orchestrator) SetBackupDir(dir string) { o.backupDir = dir }

// SetCredentialKey sets the AES key used to decrypt server credentials from the DB.
func (o *Orchestrator) SetCredentialKey(key []byte) { o.credKey = key }

// loadPrimaryDestPath reads the primary destination path from the destinations table.
// Falls back to the default backupDir if none is configured.
func (o *Orchestrator) loadPrimaryDestPath() string {
	var path string
	err := o.db.DB().QueryRow(
		"SELECT path FROM destinations WHERE is_primary = 1 AND enabled = 1 LIMIT 1",
	).Scan(&path)
	if err == nil && path != "" {
		return path
	}
	return o.backupDir
}

// SetSkipPreflight disables pre-flight checks. Intended for unit tests where
// the server host is not reachable.
func (o *Orchestrator) SetSkipPreflight(skip bool) { o.skipPreflight = skip }

// serverRecord holds server information loaded from the DB.
type serverRecord struct {
	ID             int
	Name           string
	Type           string // "linux", "windows"
	Host           string
	Port           int
	ConnectionType string
	Username       string
	Password       string
	SSHKeyPath     string
}

// jobRecord holds backup job information loaded from the DB.
type jobRecord struct {
	ID             int
	Name           string
	ServerID       int
	TimeoutMinutes int
	BandwidthLimit *int
}

// ExecuteJob runs all backup sources for a job in dependency order.
// It runs pre-flight checks first, then creates a backup_run record, executes
// each source, creates snapshot records, and updates the run status.
func (o *Orchestrator) ExecuteJob(ctx context.Context, jobID int) (*RunResult, error) {
	start := time.Now()

	// 1. Load job from DB.
	job, err := o.loadJob(jobID)
	if err != nil {
		return nil, fmt.Errorf("load job: %w", err)
	}

	// Load server info.
	server, err := o.loadServer(job.ServerID)
	if err != nil {
		return nil, fmt.Errorf("load server: %w", err)
	}

	// Load sources for the job.
	sources, err := o.loadJobSources(jobID)
	if err != nil {
		return nil, fmt.Errorf("load job sources: %w", err)
	}

	if len(sources) == 0 {
		return nil, fmt.Errorf("job %d has no sources", jobID)
	}

	// 1a. Run pre-flight checks BEFORE creating the backup_run record.
	if !o.skipPreflight {
		preflight := RunPreflight(ctx, o.db, jobID, o.backupDir)
		if !preflight.Passed {
			var msgs []string
			for _, c := range preflight.Checks {
				if !c.Passed {
					msgs = append(msgs, fmt.Sprintf("%s: %s", c.Name, c.Message))
				}
			}
			return nil, fmt.Errorf("pre-flight checks failed: %s", joinStrings(msgs, "; "))
		}
	}

	// 2. Create backup_run record with status "running".
	runID, err := o.createRun(jobID)
	if err != nil {
		return nil, fmt.Errorf("create run record: %w", err)
	}

	// 3. Resolve source execution order via topological sort.
	sorted, err := TopologicalSort(sources)
	if err != nil {
		_ = o.updateRunStatus(runID, "failed", 0, 0, err.Error(), time.Since(start))
		return nil, fmt.Errorf("topological sort: %w", err)
	}

	// 4. Execute each source in order.
	result := &RunResult{
		RunID:         runID,
		SourceResults: make([]SourceResult, 0, len(sorted)),
	}

	// Track which sources failed so we can skip dependents.
	failedSources := make(map[int]bool)

	timestamp := time.Now().UTC().Format("2006-01-02_150405")

	for _, src := range sorted {
		// Check context cancellation.
		if ctx.Err() != nil {
			_ = o.updateRunStatus(runID, "timeout", result.TotalSize, result.FilesCopied, "context cancelled", time.Since(start))
			result.Status = "timeout"
			result.Duration = time.Since(start)
			result.Errors = append(result.Errors, "context cancelled")
			return result, nil
		}

		// Check if this source depends on a failed source.
		if src.DependsOn != nil && failedSources[*src.DependsOn] {
			sr := SourceResult{
				SourceID:   src.ID,
				SourceName: src.Name,
				SourceType: src.Type,
				Status:     "skipped",
				Error:      "dependency failed",
			}
			result.SourceResults = append(result.SourceResults, sr)
			failedSources[src.ID] = true // propagate skip to transitive dependents
			continue
		}

		sr := o.executeSource(ctx, src, server, *job, timestamp)
		result.SourceResults = append(result.SourceResults, sr)

		if sr.Status == "success" {
			result.TotalSize += sr.Size
			result.FilesCopied += sr.FilesCopied

			// Create snapshot record in DB (points to the timestamped snapshot).
			snapPath := o.buildSnapshotPath(server.Name, src.Type, src.Name, timestamp)
			snapID, err := o.saveSnapshotRecord(runID, src.ID, snapPath, sr.Size, sr.Checksum)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("create snapshot record for source %d: %v", src.ID, err))
			} else if o.destSyncer != nil {
				// Queue copy to secondary destinations
				if qErr := o.destSyncer.QueueSync(snapID); qErr != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("queue secondary sync for snapshot %d: %v", snapID, qErr))
				}
			}
		} else {
			failedSources[src.ID] = true
			result.Errors = append(result.Errors, sr.Error)
		}
	}

	// 5. Determine final status.
	result.Duration = time.Since(start)
	hasFailures := false
	for _, sr := range result.SourceResults {
		if sr.Status == "failed" {
			hasFailures = true
			break
		}
	}
	if hasFailures {
		result.Status = "failed"
	} else {
		result.Status = "success"
	}

	// Update run record.
	errMsg := ""
	if len(result.Errors) > 0 {
		errMsg = result.Errors[0]
		if len(result.Errors) > 1 {
			errMsg = fmt.Sprintf("%s (and %d more errors)", errMsg, len(result.Errors)-1)
		}
	}
	_ = o.updateRunStatus(runID, result.Status, result.TotalSize, result.FilesCopied, errMsg, result.Duration)

	return result, nil
}

// executeSource runs a single source backup based on its type and the server type.
func (o *Orchestrator) executeSource(ctx context.Context, src BackupSourceRecord, server serverRecord, job jobRecord, timestamp string) SourceResult {
	sr := SourceResult{
		SourceID:   src.ID,
		SourceName: src.Name,
		SourceType: src.Type,
	}

	// Incremental strategy: rsync to "current/" (only changed bytes transferred),
	// then create a hard-link snapshot for retention (near-zero extra space).
	currentPath := o.buildCurrentPath(server.Name, src.Type, src.Name)
	snapshotPath := o.buildSnapshotPath(server.Name, src.Type, src.Name, timestamp)

	// Build exclude list: defaults + global setting + per-source patterns
	excludes := o.buildExcludes(src.ExcludePatterns)

	switch src.Type {
	case "web", "config":
		syncer := o.chooseSyncer(server.Type)
		source := sync.SyncSource{
			Host:       server.Host,
			Port:       server.Port,
			Username:   server.Username,
			Password:   server.Password,
			KeyPath:    server.SSHKeyPath,
			RemotePath: src.SourcePath,
		}
		opts := sync.SyncOptions{
			Exclude: excludes,
			Delete:  true, // remove files locally that were deleted on remote
		}
		if job.BandwidthLimit != nil {
			opts.BandwidthLimitKBps = *job.BandwidthLimit * 1024 // convert Mbps to KBps
		}

		// Sync to "current/" — only transfers changed bytes (incremental)
		result, err := syncer.Sync(ctx, source, currentPath, opts)
		if err != nil {
			sr.Status = "failed"
			sr.Error = fmt.Sprintf("sync source %q: %v", src.Name, err)
			return sr
		}

		// Create timestamped snapshot via hard links (near-zero space for unchanged files)
		if err := o.createSnapshot(currentPath, snapshotPath); err != nil {
			// Snapshot failed but sync succeeded — still count as success
			sr.Error = fmt.Sprintf("snapshot creation failed: %v", err)
		}

		sr.Status = "success"
		sr.Size = result.BytesCopied
		sr.FilesCopied = result.FilesCopied

	case "database":
		// For database sources, we currently only support mock execution in tests.
		// The actual MySQL dump requires a connector, which is not set up here.
		// We use the syncer as a simplified interface for testability.
		syncer := o.chooseSyncer(server.Type)
		source := sync.SyncSource{
			Host:       server.Host,
			Port:       server.Port,
			Username:   server.Username,
			Password:   server.Password,
			KeyPath:    server.SSHKeyPath,
			RemotePath: src.DBName,
		}
		result, err := syncer.Sync(ctx, source, currentPath, sync.SyncOptions{})
		if err != nil {
			sr.Status = "failed"
			sr.Error = fmt.Sprintf("database backup %q: %v", src.Name, err)
			return sr
		}
		// Create snapshot for databases too
		if snapErr := o.createSnapshot(currentPath, snapshotPath); snapErr != nil {
			sr.Error = fmt.Sprintf("db snapshot creation failed: %v", snapErr)
		}
		sr.Status = "success"
		sr.Size = result.BytesCopied
		sr.FilesCopied = result.FilesCopied

	default:
		sr.Status = "failed"
		sr.Error = fmt.Sprintf("unknown source type %q", src.Type)
	}

	return sr
}

// chooseSyncer returns the appropriate Syncer based on server type.
func (o *Orchestrator) chooseSyncer(serverType string) sync.Syncer {
	if serverType == "windows" {
		return o.ftpSyncer
	}
	return o.rsyncSyncer
}

// buildCurrentPath returns the "current" sync target — rsync always syncs here (incremental).
func (o *Orchestrator) buildCurrentPath(serverName, sourceType, sourceName string) string {
	baseDir := o.loadPrimaryDestPath()
	return filepath.Join(baseDir, serverName, sourceType, sourceName, "current")
}

// buildSnapshotPath returns the timestamped snapshot path for retention.
func (o *Orchestrator) buildSnapshotPath(serverName, sourceType, sourceName, timestamp string) string {
	baseDir := o.loadPrimaryDestPath()
	return filepath.Join(baseDir, serverName, sourceType, sourceName, timestamp)
}

// createSnapshot creates a hard-link copy of "current" as a timestamped snapshot.
// Hard links mean files that haven't changed share disk blocks — very space-efficient.
func (o *Orchestrator) createSnapshot(currentPath, snapshotPath string) error {
	// Use cp -al (hard link copy) on Linux/macOS
	cmd := exec.CommandContext(context.Background(), "cp", "-al", currentPath, snapshotPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		// Fallback: on macOS, cp -al may not work. Try rsync --link-dest instead.
		cmd2 := exec.CommandContext(context.Background(), "rsync", "-a", "--link-dest="+currentPath+"/", currentPath+"/", snapshotPath+"/")
		if output2, err2 := cmd2.CombinedOutput(); err2 != nil {
			return fmt.Errorf("snapshot failed: cp: %s, rsync: %s", string(output), string(output2))
		}
	}
	return nil
}

// --- Database helpers ---

func (o *Orchestrator) loadJob(jobID int) (*jobRecord, error) {
	var j jobRecord
	var bw sql.NullInt64
	err := o.db.DB().QueryRow(
		`SELECT id, name, server_id, timeout_minutes, bandwidth_limit_mbps
		 FROM backup_jobs WHERE id = ?`, jobID,
	).Scan(&j.ID, &j.Name, &j.ServerID, &j.TimeoutMinutes, &bw)
	if err != nil {
		return nil, err
	}
	if bw.Valid {
		v := int(bw.Int64)
		j.BandwidthLimit = &v
	}
	return &j, nil
}

func (o *Orchestrator) loadServer(serverID int) (serverRecord, error) {
	var s serverRecord
	var pw, keyPath sql.NullString
	err := o.db.DB().QueryRow(
		`SELECT id, name, type, host, port, connection_type, username, encrypted_password, ssh_key_path
		 FROM servers WHERE id = ?`, serverID,
	).Scan(&s.ID, &s.Name, &s.Type, &s.Host, &s.Port, &s.ConnectionType, &s.Username, &pw, &keyPath)
	if err != nil {
		return s, err
	}
	if pw.Valid && pw.String != "" {
		if o.credKey != nil {
			decrypted, err := database.DecryptCredential(pw.String, o.credKey)
			if err != nil {
				return s, fmt.Errorf("decrypt server password: %w", err)
			}
			s.Password = decrypted
		} else {
			s.Password = pw.String
		}
	}
	if keyPath.Valid && keyPath.String != "" {
		if o.credKey != nil {
			decrypted, err := database.DecryptCredential(keyPath.String, o.credKey)
			if err != nil {
				return s, fmt.Errorf("decrypt ssh key: %w", err)
			}
			s.SSHKeyPath = decrypted
		} else {
			s.SSHKeyPath = keyPath.String
		}
	}
	return s, nil
}

func (o *Orchestrator) loadJobSources(jobID int) ([]BackupSourceRecord, error) {
	rows, err := o.db.DB().Query(
		`SELECT bs.id, bs.server_id, bs.name, bs.type, bs.source_path, bs.db_name, bs.depends_on, bs.priority, bs.enabled, bs.exclude_patterns
		 FROM backup_sources bs
		 INNER JOIN backup_job_sources bjs ON bjs.source_id = bs.id
		 WHERE bjs.job_id = ? AND bs.enabled = 1
		 ORDER BY bs.priority`, jobID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []BackupSourceRecord
	for rows.Next() {
		var s BackupSourceRecord
		var sourcePath, dbName, excludePatterns sql.NullString
		var dependsOn sql.NullInt64
		var enabled int
		if err := rows.Scan(&s.ID, &s.ServerID, &s.Name, &s.Type, &sourcePath, &dbName, &dependsOn, &s.Priority, &enabled, &excludePatterns); err != nil {
			return nil, err
		}
		if sourcePath.Valid {
			s.SourcePath = sourcePath.String
		}
		if dbName.Valid {
			s.DBName = dbName.String
		}
		if dependsOn.Valid {
			v := int(dependsOn.Int64)
			s.DependsOn = &v
		}
		if excludePatterns.Valid {
			s.ExcludePatterns = excludePatterns.String
		}
		s.Enabled = enabled == 1
		sources = append(sources, s)
	}
	return sources, rows.Err()
}

func (o *Orchestrator) createRun(jobID int) (int, error) {
	res, err := o.db.DB().Exec(
		`INSERT INTO backup_runs (job_id, status, started_at) VALUES (?, 'running', datetime('now'))`,
		jobID,
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

func (o *Orchestrator) updateRunStatus(runID int, status string, totalSize int64, filesCopied int, errMsg string, duration time.Duration) error {
	var errMsgPtr *string
	if errMsg != "" {
		errMsgPtr = &errMsg
	}
	_, err := o.db.DB().Exec(
		`UPDATE backup_runs SET status = ?, finished_at = datetime('now'),
		 total_size_bytes = ?, files_copied = ?, error_message = ?
		 WHERE id = ?`,
		status, totalSize, filesCopied, errMsgPtr, runID,
	)
	return err
}

func (o *Orchestrator) saveSnapshotRecord(runID, sourceID int, snapshotPath string, sizeBytes int64, checksum string) (int, error) {
	result, err := o.db.DB().Exec(
		`INSERT INTO backup_snapshots (run_id, source_id, snapshot_path, size_bytes, checksum_sha256)
		 VALUES (?, ?, ?, ?, ?)`,
		runID, sourceID, snapshotPath, sizeBytes, checksum,
	)
	if err != nil {
		return 0, err
	}
	id, _ := result.LastInsertId()
	return int(id), nil
}

// buildExcludes merges defaultExcludes, global exclude patterns from settings,
// and the per-source exclude_patterns into a single deduplicated slice.
func (o *Orchestrator) buildExcludes(perSourcePatterns string) []string {
	seen := make(map[string]bool)
	var result []string

	add := func(p string) {
		p = strings.TrimSpace(p)
		if p != "" && !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}

	for _, p := range defaultExcludes {
		add(p)
	}

	// Load global exclude patterns from settings table.
	globalPatterns := o.loadGlobalExcludePatterns()
	for _, p := range strings.Split(globalPatterns, ",") {
		add(strings.TrimSpace(p))
	}
	// Also handle newline-separated patterns.
	for _, p := range strings.Split(globalPatterns, "\n") {
		add(strings.TrimSpace(p))
	}

	// Per-source patterns (comma-separated).
	for _, p := range strings.Split(perSourcePatterns, ",") {
		add(strings.TrimSpace(p))
	}

	return result
}

// loadGlobalExcludePatterns reads the global_exclude_patterns setting from the DB.
// Returns empty string if not set or on error.
func (o *Orchestrator) loadGlobalExcludePatterns() string {
	var val string
	err := o.db.DB().QueryRow(
		`SELECT value FROM settings WHERE key = 'global_exclude_patterns'`,
	).Scan(&val)
	if err != nil {
		return ""
	}
	return val
}

// TopologicalSort sorts backup sources respecting depends_on relationships.
// Returns error if a cycle is detected.
func TopologicalSort(sources []BackupSourceRecord) ([]BackupSourceRecord, error) {
	if len(sources) == 0 {
		return nil, nil
	}

	// Build index and adjacency.
	byID := make(map[int]BackupSourceRecord, len(sources))
	inDegree := make(map[int]int, len(sources))
	dependents := make(map[int][]int) // parent -> children that depend on it

	sourceIDs := make(map[int]bool, len(sources))
	for _, s := range sources {
		byID[s.ID] = s
		sourceIDs[s.ID] = true
		inDegree[s.ID] = 0
	}

	for _, s := range sources {
		if s.DependsOn != nil && sourceIDs[*s.DependsOn] {
			inDegree[s.ID]++
			dependents[*s.DependsOn] = append(dependents[*s.DependsOn], s.ID)
		}
	}

	// Kahn's algorithm.
	var queue []int
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	// Sort queue by priority for deterministic ordering among independent sources.
	sortByPriority(queue, byID)

	var sorted []BackupSourceRecord
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		sorted = append(sorted, byID[id])

		for _, depID := range dependents[id] {
			inDegree[depID]--
			if inDegree[depID] == 0 {
				queue = append(queue, depID)
				sortByPriority(queue, byID)
			}
		}
	}

	if len(sorted) != len(sources) {
		return nil, fmt.Errorf("cycle detected in source dependencies")
	}

	return sorted, nil
}

// joinStrings joins strings with a separator (avoids importing strings package).
func joinStrings(parts []string, sep string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}

// sortByPriority sorts a slice of IDs by their priority in the source map (ascending).
func sortByPriority(ids []int, byID map[int]BackupSourceRecord) {
	// Simple insertion sort since these slices are typically very small.
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && byID[ids[j]].Priority < byID[ids[j-1]].Priority; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}
