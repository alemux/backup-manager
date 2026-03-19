package backup

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
	"github.com/backupmanager/backupmanager/internal/sync"
)

// ---------------------------------------------------------------------------
// MockSyncer
// ---------------------------------------------------------------------------

// MockSyncer implements sync.Syncer for testing.
type MockSyncer struct {
	SyncFunc func(ctx context.Context, source sync.SyncSource, destPath string, opts sync.SyncOptions) (*sync.SyncResult, error)
}

func (m *MockSyncer) Sync(ctx context.Context, source sync.SyncSource, destPath string, opts sync.SyncOptions) (*sync.SyncResult, error) {
	if m.SyncFunc != nil {
		return m.SyncFunc(ctx, source, destPath, opts)
	}
	return &sync.SyncResult{
		FilesCopied: 1,
		BytesCopied: 1024,
		Duration:    100 * time.Millisecond,
	}, nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func setupTestDB(t *testing.T) *database.Database {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func insertTestServer(t *testing.T, db *database.Database, name, serverType string) int {
	t.Helper()
	res, err := db.DB().Exec(
		`INSERT INTO servers (name, type, host, port, connection_type, username)
		 VALUES (?, ?, '10.0.0.1', 22, 'ssh', 'backup')`,
		name, serverType,
	)
	if err != nil {
		t.Fatalf("insert test server: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func insertTestSource(t *testing.T, db *database.Database, serverID int, name, srcType, sourcePath string, dependsOn *int, priority int) int {
	t.Helper()
	var depVal interface{} = nil
	if dependsOn != nil {
		depVal = *dependsOn
	}
	res, err := db.DB().Exec(
		`INSERT INTO backup_sources (server_id, name, type, source_path, depends_on, priority, enabled)
		 VALUES (?, ?, ?, ?, ?, ?, 1)`,
		serverID, name, srcType, sourcePath, depVal, priority,
	)
	if err != nil {
		t.Fatalf("insert test source: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func insertTestJob(t *testing.T, db *database.Database, name string, serverID int, sourceIDs []int) int {
	t.Helper()
	res, err := db.DB().Exec(
		`INSERT INTO backup_jobs (name, server_id, schedule, timeout_minutes)
		 VALUES (?, ?, '0 2 * * *', 120)`,
		name, serverID,
	)
	if err != nil {
		t.Fatalf("insert test job: %v", err)
	}
	jobID, _ := res.LastInsertId()
	for _, srcID := range sourceIDs {
		_, err := db.DB().Exec(
			`INSERT INTO backup_job_sources (job_id, source_id) VALUES (?, ?)`,
			jobID, srcID,
		)
		if err != nil {
			t.Fatalf("insert job source: %v", err)
		}
	}
	return int(jobID)
}

// ---------------------------------------------------------------------------
// TopologicalSort tests
// ---------------------------------------------------------------------------

func TestTopologicalSort_NoDeps(t *testing.T) {
	sources := []BackupSourceRecord{
		{ID: 3, Name: "config", Priority: 2},
		{ID: 1, Name: "web", Priority: 0},
		{ID: 2, Name: "db", Priority: 1},
	}

	sorted, err := TopologicalSort(sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 3 {
		t.Fatalf("expected 3 sources, got %d", len(sorted))
	}
	// Should be sorted by priority: web(0), db(1), config(2).
	if sorted[0].Name != "web" {
		t.Errorf("sorted[0] = %q, want %q", sorted[0].Name, "web")
	}
	if sorted[1].Name != "db" {
		t.Errorf("sorted[1] = %q, want %q", sorted[1].Name, "db")
	}
	if sorted[2].Name != "config" {
		t.Errorf("sorted[2] = %q, want %q", sorted[2].Name, "config")
	}
}

func TestTopologicalSort_WithDeps(t *testing.T) {
	// A -> B -> C (C depends on B, B depends on A)
	idA, idB := 1, 2
	sources := []BackupSourceRecord{
		{ID: 3, Name: "C", DependsOn: &idB, Priority: 0},
		{ID: 1, Name: "A", Priority: 0},
		{ID: 2, Name: "B", DependsOn: &idA, Priority: 0},
	}

	sorted, err := TopologicalSort(sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 3 {
		t.Fatalf("expected 3 sources, got %d", len(sorted))
	}
	// Must be A, B, C in order.
	if sorted[0].Name != "A" {
		t.Errorf("sorted[0] = %q, want %q", sorted[0].Name, "A")
	}
	if sorted[1].Name != "B" {
		t.Errorf("sorted[1] = %q, want %q", sorted[1].Name, "B")
	}
	if sorted[2].Name != "C" {
		t.Errorf("sorted[2] = %q, want %q", sorted[2].Name, "C")
	}
}

func TestTopologicalSort_Cycle(t *testing.T) {
	// A depends on B, B depends on A.
	idA, idB := 1, 2
	sources := []BackupSourceRecord{
		{ID: 1, Name: "A", DependsOn: &idB},
		{ID: 2, Name: "B", DependsOn: &idA},
	}

	_, err := TopologicalSort(sources)
	if err == nil {
		t.Fatal("expected cycle detection error, got nil")
	}
}

func TestTopologicalSort_MultipleTrees(t *testing.T) {
	// Tree 1: A(prio 0) -> B(prio 0)
	// Tree 2: C(prio 1) -> D(prio 1)
	idA, idC := 1, 3
	sources := []BackupSourceRecord{
		{ID: 4, Name: "D", DependsOn: &idC, Priority: 1},
		{ID: 2, Name: "B", DependsOn: &idA, Priority: 0},
		{ID: 3, Name: "C", Priority: 1},
		{ID: 1, Name: "A", Priority: 0},
	}

	sorted, err := TopologicalSort(sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 4 {
		t.Fatalf("expected 4 sources, got %d", len(sorted))
	}

	// A must come before B, C must come before D.
	posA, posB, posC, posD := -1, -1, -1, -1
	for i, s := range sorted {
		switch s.Name {
		case "A":
			posA = i
		case "B":
			posB = i
		case "C":
			posC = i
		case "D":
			posD = i
		}
	}
	if posA >= posB {
		t.Errorf("A (pos %d) should come before B (pos %d)", posA, posB)
	}
	if posC >= posD {
		t.Errorf("C (pos %d) should come before D (pos %d)", posC, posD)
	}
}

// ---------------------------------------------------------------------------
// ExecuteJob tests
// ---------------------------------------------------------------------------

func TestExecuteJob_CreatesRunRecord(t *testing.T) {
	db := setupTestDB(t)
	serverID := insertTestServer(t, db, "srv1", "linux")
	srcID := insertTestSource(t, db, serverID, "web-files", "web", "/var/www", nil, 0)
	jobID := insertTestJob(t, db, "daily-backup", serverID, []int{srcID})

	orch := NewOrchestrator(db)
	orch.SetBackupDir(t.TempDir())
	orch.SetSkipPreflight(true)
	orch.SetRsyncSyncer(&MockSyncer{})

	result, err := orch.ExecuteJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify run record exists in DB.
	var status string
	err = db.DB().QueryRow("SELECT status FROM backup_runs WHERE id = ?", result.RunID).Scan(&status)
	if err != nil {
		t.Fatalf("query run record: %v", err)
	}
	if status != "success" {
		t.Errorf("run status = %q, want %q", status, "success")
	}
}

func TestExecuteJob_UpdatesRunStatus(t *testing.T) {
	db := setupTestDB(t)
	serverID := insertTestServer(t, db, "srv1", "linux")
	srcID := insertTestSource(t, db, serverID, "web-files", "web", "/var/www", nil, 0)
	jobID := insertTestJob(t, db, "daily-backup", serverID, []int{srcID})

	orch := NewOrchestrator(db)
	orch.SetBackupDir(t.TempDir())
	orch.SetSkipPreflight(true)

	// Use a syncer that fails.
	orch.SetRsyncSyncer(&MockSyncer{
		SyncFunc: func(ctx context.Context, source sync.SyncSource, destPath string, opts sync.SyncOptions) (*sync.SyncResult, error) {
			return nil, os.ErrPermission
		},
	})

	result, err := orch.ExecuteJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("result.Status = %q, want %q", result.Status, "failed")
	}

	// Verify DB record reflects failure.
	var status string
	err = db.DB().QueryRow("SELECT status FROM backup_runs WHERE id = ?", result.RunID).Scan(&status)
	if err != nil {
		t.Fatalf("query run record: %v", err)
	}
	if status != "failed" {
		t.Errorf("DB run status = %q, want %q", status, "failed")
	}
}

func TestExecuteJob_CreatesSnapshots(t *testing.T) {
	db := setupTestDB(t)
	serverID := insertTestServer(t, db, "srv1", "linux")
	src1 := insertTestSource(t, db, serverID, "web-files", "web", "/var/www", nil, 0)
	src2 := insertTestSource(t, db, serverID, "config-files", "config", "/etc/nginx", nil, 1)
	jobID := insertTestJob(t, db, "daily-backup", serverID, []int{src1, src2})

	orch := NewOrchestrator(db)
	orch.SetBackupDir(t.TempDir())
	orch.SetSkipPreflight(true)
	orch.SetRsyncSyncer(&MockSyncer{})

	result, err := orch.ExecuteJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "success" {
		t.Fatalf("result.Status = %q, want %q", result.Status, "success")
	}

	// Verify snapshot records exist.
	var snapshotCount int
	err = db.DB().QueryRow(
		"SELECT COUNT(*) FROM backup_snapshots WHERE run_id = ?", result.RunID,
	).Scan(&snapshotCount)
	if err != nil {
		t.Fatalf("query snapshots: %v", err)
	}
	if snapshotCount != 2 {
		t.Errorf("snapshot count = %d, want 2", snapshotCount)
	}

	// Verify each source has a snapshot.
	for _, srcID := range []int{src1, src2} {
		var path string
		err := db.DB().QueryRow(
			"SELECT snapshot_path FROM backup_snapshots WHERE run_id = ? AND source_id = ?",
			result.RunID, srcID,
		).Scan(&path)
		if err != nil {
			t.Errorf("snapshot for source %d: %v", srcID, err)
		}
		if path == "" {
			t.Errorf("snapshot path for source %d is empty", srcID)
		}
	}
}

func TestExecuteJob_SkipsDependentsOnFailure(t *testing.T) {
	db := setupTestDB(t)
	serverID := insertTestServer(t, db, "srv1", "linux")
	// A -> B: B depends on A. If A fails, B should be skipped.
	srcA := insertTestSource(t, db, serverID, "source-A", "web", "/var/www/a", nil, 0)
	srcB := insertTestSource(t, db, serverID, "source-B", "web", "/var/www/b", &srcA, 1)
	jobID := insertTestJob(t, db, "chain-backup", serverID, []int{srcA, srcB})

	callCount := 0
	orch := NewOrchestrator(db)
	orch.SetBackupDir(t.TempDir())
	orch.SetSkipPreflight(true)
	orch.SetRsyncSyncer(&MockSyncer{
		SyncFunc: func(ctx context.Context, source sync.SyncSource, destPath string, opts sync.SyncOptions) (*sync.SyncResult, error) {
			callCount++
			// Fail on first call (source A).
			return nil, os.ErrPermission
		},
	})

	result, err := orch.ExecuteJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Syncer should only be called once (for A); B should be skipped without calling sync.
	if callCount != 1 {
		t.Errorf("syncer called %d times, expected 1 (B should be skipped)", callCount)
	}

	// Verify source results.
	if len(result.SourceResults) != 2 {
		t.Fatalf("expected 2 source results, got %d", len(result.SourceResults))
	}
	for _, sr := range result.SourceResults {
		if sr.SourceName == "source-A" && sr.Status != "failed" {
			t.Errorf("source-A status = %q, want %q", sr.Status, "failed")
		}
		if sr.SourceName == "source-B" && sr.Status != "skipped" {
			t.Errorf("source-B status = %q, want %q", sr.Status, "skipped")
		}
	}
}

func TestExecuteJob_WindowsServerUsesFTP(t *testing.T) {
	db := setupTestDB(t)
	serverID := insertTestServer(t, db, "win-srv", "windows")
	// Update server to be windows type with ftp connection.
	db.DB().Exec("UPDATE servers SET type = 'windows', connection_type = 'ftp' WHERE id = ?", serverID)
	srcID := insertTestSource(t, db, serverID, "web-files", "web", "/inetpub/wwwroot", nil, 0)
	jobID := insertTestJob(t, db, "win-backup", serverID, []int{srcID})

	rsyncCalled := false
	ftpCalled := false

	orch := NewOrchestrator(db)
	orch.SetBackupDir(t.TempDir())
	orch.SetSkipPreflight(true)
	orch.SetRsyncSyncer(&MockSyncer{
		SyncFunc: func(ctx context.Context, source sync.SyncSource, destPath string, opts sync.SyncOptions) (*sync.SyncResult, error) {
			rsyncCalled = true
			return &sync.SyncResult{FilesCopied: 1, BytesCopied: 512}, nil
		},
	})
	orch.SetFTPSyncer(&MockSyncer{
		SyncFunc: func(ctx context.Context, source sync.SyncSource, destPath string, opts sync.SyncOptions) (*sync.SyncResult, error) {
			ftpCalled = true
			return &sync.SyncResult{FilesCopied: 1, BytesCopied: 512}, nil
		},
	})

	_, err := orch.ExecuteJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rsyncCalled {
		t.Error("rsync syncer was called for a windows server")
	}
	if !ftpCalled {
		t.Error("FTP syncer was NOT called for a windows server")
	}
}

func TestExecuteJob_RunRecordHasSizeAndFiles(t *testing.T) {
	db := setupTestDB(t)
	serverID := insertTestServer(t, db, "srv1", "linux")
	srcID := insertTestSource(t, db, serverID, "web-files", "web", "/var/www", nil, 0)
	jobID := insertTestJob(t, db, "daily-backup", serverID, []int{srcID})

	orch := NewOrchestrator(db)
	orch.SetBackupDir(t.TempDir())
	orch.SetSkipPreflight(true)
	orch.SetRsyncSyncer(&MockSyncer{
		SyncFunc: func(ctx context.Context, source sync.SyncSource, destPath string, opts sync.SyncOptions) (*sync.SyncResult, error) {
			return &sync.SyncResult{FilesCopied: 42, BytesCopied: 98765}, nil
		},
	})

	result, err := orch.ExecuteJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify DB record has correct totals.
	var totalSize int64
	var filesCopied int
	err = db.DB().QueryRow(
		"SELECT total_size_bytes, files_copied FROM backup_runs WHERE id = ?", result.RunID,
	).Scan(&totalSize, &filesCopied)
	if err != nil {
		t.Fatalf("query run record: %v", err)
	}
	if totalSize != 98765 {
		t.Errorf("total_size_bytes = %d, want 98765", totalSize)
	}
	if filesCopied != 42 {
		t.Errorf("files_copied = %d, want 42", filesCopied)
	}
}

func TestExecuteJob_FinishedAtIsSet(t *testing.T) {
	db := setupTestDB(t)
	serverID := insertTestServer(t, db, "srv1", "linux")
	srcID := insertTestSource(t, db, serverID, "web-files", "web", "/var/www", nil, 0)
	jobID := insertTestJob(t, db, "daily-backup", serverID, []int{srcID})

	orch := NewOrchestrator(db)
	orch.SetBackupDir(t.TempDir())
	orch.SetSkipPreflight(true)
	orch.SetRsyncSyncer(&MockSyncer{})

	result, err := orch.ExecuteJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var finishedAt sql.NullString
	err = db.DB().QueryRow(
		"SELECT finished_at FROM backup_runs WHERE id = ?", result.RunID,
	).Scan(&finishedAt)
	if err != nil {
		t.Fatalf("query run record: %v", err)
	}
	if !finishedAt.Valid || finishedAt.String == "" {
		t.Error("finished_at is not set on completed run")
	}
}
