package backup

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"syscall"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
)

// PreflightResult holds the overall result of all pre-flight checks.
type PreflightResult struct {
	Passed bool    `json:"passed"`
	Checks []Check `json:"checks"`
}

// Check represents a single pre-flight check result.
type Check struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
}

// RunPreflight checks conditions before starting a backup job.
// It runs three checks:
//  1. Disk space on backupDir vs estimated backup size (avg of last 3 runs × 1.5).
//  2. Server reachability via TCP with a 5-second timeout.
//  3. No duplicate "running" backup_run for the same server.
//
// Returns a PreflightResult with all check details. The caller should inspect
// Result.Passed before proceeding with execution.
func RunPreflight(ctx context.Context, db *database.Database, jobID int, backupDir string) *PreflightResult {
	result := &PreflightResult{}

	// Load job and server info.
	type jobInfo struct {
		serverID int
		host     string
		port     int
	}
	var info jobInfo
	err := db.DB().QueryRow(
		`SELECT bj.server_id, s.host, s.port
		 FROM backup_jobs bj
		 INNER JOIN servers s ON s.id = bj.server_id
		 WHERE bj.id = ?`, jobID,
	).Scan(&info.serverID, &info.host, &info.port)
	if err != nil {
		// If we can't load job/server, fail all checks.
		result.Checks = []Check{
			{Name: "disk_space", Passed: false, Message: fmt.Sprintf("cannot load job info: %v", err)},
			{Name: "server_reachable", Passed: false, Message: fmt.Sprintf("cannot load job info: %v", err)},
			{Name: "no_duplicate_run", Passed: false, Message: fmt.Sprintf("cannot load job info: %v", err)},
		}
		result.Passed = false
		return result
	}

	// Check 1: Disk space.
	diskCheck := checkDiskSpace(db, jobID, backupDir)
	result.Checks = append(result.Checks, diskCheck)

	// Check 2: Server reachability.
	reachCheck := checkServerReachable(info.host, info.port)
	result.Checks = append(result.Checks, reachCheck)

	// Check 3: No duplicate run.
	dupCheck := checkNoDuplicateRun(db, info.serverID)
	result.Checks = append(result.Checks, dupCheck)

	// Overall passed only if all checks passed.
	allPassed := true
	for _, c := range result.Checks {
		if !c.Passed {
			allPassed = false
			break
		}
	}
	result.Passed = allPassed
	return result
}

// checkDiskSpace verifies there is sufficient disk space on backupDir.
// It estimates required space from the average size of the last 3 successful runs × 1.5.
// If no prior runs exist (first run), the check passes.
func checkDiskSpace(db *database.Database, jobID int, backupDir string) Check {
	check := Check{Name: "disk_space"}

	// Query average size of last 3 successful runs for this job.
	rows, err := db.DB().Query(
		`SELECT total_size_bytes FROM backup_runs
		 WHERE job_id = ? AND status = 'success'
		 ORDER BY started_at DESC LIMIT 3`, jobID,
	)
	if err != nil {
		check.Passed = true // be permissive on query error
		check.Message = fmt.Sprintf("cannot query prior runs (skipping check): %v", err)
		return check
	}
	defer rows.Close()

	var sizes []int64
	for rows.Next() {
		var sz int64
		if scanErr := rows.Scan(&sz); scanErr == nil {
			sizes = append(sizes, sz)
		}
	}

	// First run — no history, pass.
	if len(sizes) == 0 {
		check.Passed = true
		check.Message = "no prior runs; skipping disk space estimate"
		return check
	}

	var total int64
	for _, s := range sizes {
		total += s
	}
	avg := total / int64(len(sizes))
	required := int64(float64(avg) * 1.5)

	// Check available disk space via syscall.
	var stat syscall.Statfs_t
	if err := syscall.Statfs(backupDir, &stat); err != nil {
		check.Passed = true // can't check → don't block
		check.Message = fmt.Sprintf("cannot stat filesystem (skipping check): %v", err)
		return check
	}

	// Available bytes = blocks available to non-root × block size.
	available := int64(stat.Bavail) * int64(stat.Bsize)
	if available < required {
		check.Passed = false
		check.Message = fmt.Sprintf(
			"insufficient disk space: available %d bytes, required %d bytes (avg %d × 1.5)",
			available, required, avg,
		)
		return check
	}

	check.Passed = true
	check.Message = fmt.Sprintf(
		"disk space OK: available %d bytes, required %d bytes",
		available, required,
	)
	return check
}

// checkServerReachable verifies the server is reachable via TCP.
func checkServerReachable(host string, port int) Check {
	check := Check{Name: "server_reachable"}

	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		check.Passed = false
		check.Message = fmt.Sprintf("cannot reach server at %s: %v", addr, err)
		return check
	}
	conn.Close()

	check.Passed = true
	check.Message = fmt.Sprintf("server reachable at %s", addr)
	return check
}

// checkNoDuplicateRun verifies no other backup run with status "running" exists
// for the same server.
func checkNoDuplicateRun(db *database.Database, serverID int) Check {
	check := Check{Name: "no_duplicate_run"}

	var runningCount int
	err := db.DB().QueryRow(
		`SELECT COUNT(*) FROM backup_runs br
		 INNER JOIN backup_jobs bj ON bj.id = br.job_id
		 WHERE bj.server_id = ? AND br.status = 'running'`, serverID,
	).Scan(&runningCount)
	if err != nil && err != sql.ErrNoRows {
		check.Passed = true // be permissive on query error
		check.Message = fmt.Sprintf("cannot check running jobs (skipping): %v", err)
		return check
	}

	if runningCount > 0 {
		check.Passed = false
		check.Message = fmt.Sprintf("another backup is already running for server %d", serverID)
		return check
	}

	check.Passed = true
	check.Message = "no concurrent runs detected"
	return check
}

// BackupSQLiteDB copies the database file at dbPath to backupDir with a
// datestamped filename. It retains only the last 7 copies, deleting older ones.
func BackupSQLiteDB(dbPath, backupDir string) error {
	// Ensure backup directory exists.
	if err := mkdirAll(backupDir); err != nil {
		return fmt.Errorf("create db-backup dir: %w", err)
	}

	today := time.Now().UTC().Format("2006-01-02")
	destName := fmt.Sprintf("backupmanager_%s.db", today)
	destPath := joinPath(backupDir, destName)

	// Copy the file.
	if err := copyFile(dbPath, destPath); err != nil {
		return fmt.Errorf("copy database: %w", err)
	}

	// Prune old backups — keep last 7.
	if err := pruneOldDBBackups(backupDir, 7); err != nil {
		// Non-fatal: log would happen at the caller level.
		return fmt.Errorf("prune old db backups: %w", err)
	}

	return nil
}
