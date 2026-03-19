package backup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/backupmanager/backupmanager/internal/database"
)

// newRecoveryTestDB opens an in-memory SQLite database and runs migrations.
func newRecoveryTestDB(t *testing.T) *database.Database {
	t.Helper()
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// insertMinimalJob inserts the minimum required rows (server + job) and returns jobID.
func insertMinimalJob(t *testing.T, db *database.Database) int {
	t.Helper()
	res, err := db.DB().Exec(
		`INSERT INTO servers (name, type, host, port, connection_type) VALUES ('srv','linux','localhost',22,'ssh')`,
	)
	if err != nil {
		t.Fatalf("insert server: %v", err)
	}
	serverID, _ := res.LastInsertId()

	res, err = db.DB().Exec(
		`INSERT INTO backup_jobs (name, server_id, schedule) VALUES ('job','`+itoa(int(serverID))+`','@daily')`,
	)
	if err != nil {
		// Fallback with Exec + args
		res, err = db.DB().Exec(
			`INSERT INTO backup_jobs (name, server_id, schedule) VALUES (?, ?, ?)`,
			"job", serverID, "@daily",
		)
		if err != nil {
			t.Fatalf("insert job: %v", err)
		}
	}
	jobID, _ := res.LastInsertId()
	return int(jobID)
}

// itoa is a tiny int-to-string helper to avoid importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	result := []byte{}
	for n > 0 {
		result = append([]byte{byte('0' + n%10)}, result...)
		n /= 10
	}
	return string(result)
}

// insertRunWithStatus inserts a backup_run with the given status and returns its ID.
func insertRunWithStatus(t *testing.T, db *database.Database, jobID int, status string) int {
	t.Helper()
	res, err := db.DB().Exec(
		`INSERT INTO backup_runs (job_id, status, started_at) VALUES (?, ?, datetime('now'))`,
		jobID, status,
	)
	if err != nil {
		t.Fatalf("insert backup_run: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

// insertSource inserts a minimal backup_source and returns its ID.
func insertSource(t *testing.T, db *database.Database, serverID int) int {
	t.Helper()
	res, err := db.DB().Exec(
		`INSERT INTO backup_sources (server_id, name, type) VALUES (?, 'src', 'config')`,
		serverID,
	)
	if err != nil {
		t.Fatalf("insert backup_source: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

// insertDestination inserts a minimal destination and returns its ID.
func insertDestination(t *testing.T, db *database.Database) int {
	t.Helper()
	res, err := db.DB().Exec(
		`INSERT INTO destinations (name, type, path) VALUES ('dest','local','/tmp/dest')`,
	)
	if err != nil {
		t.Fatalf("insert destination: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

// insertDestinationSync inserts a destination_sync_status row.
func insertDestinationSync(t *testing.T, db *database.Database, snapshotID, destID int, status string) {
	t.Helper()
	_, err := db.DB().Exec(
		`INSERT INTO destination_sync_status (snapshot_id, destination_id, status) VALUES (?, ?, ?)`,
		snapshotID, destID, status,
	)
	if err != nil {
		t.Fatalf("insert destination_sync_status: %v", err)
	}
}

// insertSnapshot inserts a backup_snapshot and returns its ID.
func insertSnapshot(t *testing.T, db *database.Database, runID, sourceID int, snapshotPath string) int {
	t.Helper()
	res, err := db.DB().Exec(
		`INSERT INTO backup_snapshots (run_id, source_id, snapshot_path, size_bytes) VALUES (?, ?, ?, 0)`,
		runID, sourceID, snapshotPath,
	)
	if err != nil {
		t.Fatalf("insert backup_snapshot: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

// -------------------------------------------------------------------------
// Tests
// -------------------------------------------------------------------------

func TestRecoverFromCrash_MarksRunsFailed(t *testing.T) {
	db := newRecoveryTestDB(t)
	jobID := insertMinimalJob(t, db)
	runID := insertRunWithStatus(t, db, jobID, "running")

	result := RecoverFromCrash(db, t.TempDir())

	if result.RunsRecovered != 1 {
		t.Errorf("RunsRecovered = %d, want 1", result.RunsRecovered)
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}

	var status, errMsg string
	var finishedAt *string
	err := db.DB().QueryRow(
		`SELECT status, COALESCE(error_message,''), finished_at FROM backup_runs WHERE id = ?`, runID,
	).Scan(&status, &errMsg, &finishedAt)
	if err != nil {
		t.Fatalf("query run: %v", err)
	}
	if status != "failed" {
		t.Errorf("status = %q, want %q", status, "failed")
	}
	if errMsg != "interrupted by application restart" {
		t.Errorf("error_message = %q, want %q", errMsg, "interrupted by application restart")
	}
	if finishedAt == nil || *finishedAt == "" {
		t.Error("finished_at should be set after recovery")
	}
}

func TestRecoverFromCrash_ResetsSyncs(t *testing.T) {
	db := newRecoveryTestDB(t)
	jobID := insertMinimalJob(t, db)

	// Need a server_id for source; reuse the one created by insertMinimalJob.
	var serverID int
	db.DB().QueryRow(`SELECT server_id FROM backup_jobs WHERE id = ?`, jobID).Scan(&serverID)
	sourceID := insertSource(t, db, serverID)
	destID := insertDestination(t, db)

	// Insert a run in a terminal state (not running), just to have snapshot rows.
	runID := insertRunWithStatus(t, db, jobID, "success")
	snapshotID := insertSnapshot(t, db, runID, sourceID, "/tmp/snap.sql.gz")
	insertDestinationSync(t, db, snapshotID, destID, "in_progress")

	result := RecoverFromCrash(db, t.TempDir())

	if result.SyncsRecovered != 1 {
		t.Errorf("SyncsRecovered = %d, want 1", result.SyncsRecovered)
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}

	var syncStatus string
	db.DB().QueryRow(
		`SELECT status FROM destination_sync_status WHERE snapshot_id = ? AND destination_id = ?`,
		snapshotID, destID,
	).Scan(&syncStatus)
	if syncStatus != "pending" {
		t.Errorf("sync status = %q, want %q", syncStatus, "pending")
	}
}

func TestRecoverFromCrash_NoStaleState(t *testing.T) {
	db := newRecoveryTestDB(t)
	// No running jobs or in_progress syncs — just an empty DB.

	result := RecoverFromCrash(db, t.TempDir())

	if result.RunsRecovered != 0 {
		t.Errorf("RunsRecovered = %d, want 0", result.RunsRecovered)
	}
	if result.SyncsRecovered != 0 {
		t.Errorf("SyncsRecovered = %d, want 0", result.SyncsRecovered)
	}
	if result.FilesCleanedUp != 0 {
		t.Errorf("FilesCleanedUp = %d, want 0", result.FilesCleanedUp)
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
}

func TestRecoverFromCrash_MultipleRuns(t *testing.T) {
	db := newRecoveryTestDB(t)
	jobID := insertMinimalJob(t, db)

	// Insert three stale running runs and one already-failed run.
	id1 := insertRunWithStatus(t, db, jobID, "running")
	id2 := insertRunWithStatus(t, db, jobID, "running")
	id3 := insertRunWithStatus(t, db, jobID, "running")
	_ = insertRunWithStatus(t, db, jobID, "failed") // should be unaffected

	result := RecoverFromCrash(db, t.TempDir())

	if result.RunsRecovered != 3 {
		t.Errorf("RunsRecovered = %d, want 3", result.RunsRecovered)
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}

	for _, id := range []int{id1, id2, id3} {
		var status string
		db.DB().QueryRow(`SELECT status FROM backup_runs WHERE id = ?`, id).Scan(&status)
		if status != "failed" {
			t.Errorf("run %d: status = %q, want %q", id, status, "failed")
		}
	}
}

func TestRecoverFromCrash_PartialFileCleanup(t *testing.T) {
	db := newRecoveryTestDB(t)
	backupDir := t.TempDir()
	jobID := insertMinimalJob(t, db)

	var serverID int
	db.DB().QueryRow(`SELECT server_id FROM backup_jobs WHERE id = ?`, jobID).Scan(&serverID)
	sourceID := insertSource(t, db, serverID)

	// Create a partial file inside the backup directory.
	partialFile := filepath.Join(backupDir, "partial.sql.gz")
	if err := os.WriteFile(partialFile, []byte("partial"), 0644); err != nil {
		t.Fatalf("create partial file: %v", err)
	}

	// Insert a stale running run with a snapshot pointing to the partial file.
	runID := insertRunWithStatus(t, db, jobID, "running")
	insertSnapshot(t, db, runID, sourceID, partialFile)

	result := RecoverFromCrash(db, backupDir)

	if result.RunsRecovered != 1 {
		t.Errorf("RunsRecovered = %d, want 1", result.RunsRecovered)
	}
	if result.FilesCleanedUp != 1 {
		t.Errorf("FilesCleanedUp = %d, want 1", result.FilesCleanedUp)
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
	if _, err := os.Stat(partialFile); !os.IsNotExist(err) {
		t.Error("expected partial file to be removed, but it still exists")
	}
}

func TestRecoverFromCrash_DoesNotDeleteFilesOutsideBackupDir(t *testing.T) {
	db := newRecoveryTestDB(t)
	backupDir := t.TempDir()
	jobID := insertMinimalJob(t, db)

	var serverID int
	db.DB().QueryRow(`SELECT server_id FROM backup_jobs WHERE id = ?`, jobID).Scan(&serverID)
	sourceID := insertSource(t, db, serverID)

	// Create a file outside the backup directory.
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "important.sql.gz")
	if err := os.WriteFile(outsideFile, []byte("important data"), 0644); err != nil {
		t.Fatalf("create outside file: %v", err)
	}

	// Stale run with snapshot pointing outside backupDir.
	runID := insertRunWithStatus(t, db, jobID, "running")
	insertSnapshot(t, db, runID, sourceID, outsideFile)

	result := RecoverFromCrash(db, backupDir)

	if result.FilesCleanedUp != 0 {
		t.Errorf("FilesCleanedUp = %d, want 0 (must not delete outside backup dir)", result.FilesCleanedUp)
	}
	if _, err := os.Stat(outsideFile); err != nil {
		t.Errorf("outside file was unexpectedly removed: %v", err)
	}
}

func TestRecoverFromCrash_DoesNotDeleteCompletedSnapshots(t *testing.T) {
	db := newRecoveryTestDB(t)
	backupDir := t.TempDir()
	jobID := insertMinimalJob(t, db)

	var serverID int
	db.DB().QueryRow(`SELECT server_id FROM backup_jobs WHERE id = ?`, jobID).Scan(&serverID)
	sourceID := insertSource(t, db, serverID)

	// A file that a *successful* run references.
	sharedFile := filepath.Join(backupDir, "shared.sql.gz")
	if err := os.WriteFile(sharedFile, []byte("real data"), 0644); err != nil {
		t.Fatalf("create shared file: %v", err)
	}

	// Successful run → snapshot pointing to sharedFile.
	successRunID := insertRunWithStatus(t, db, jobID, "success")
	insertSnapshot(t, db, successRunID, sourceID, sharedFile)

	// Stale running run that also references the same path.
	staleRunID := insertRunWithStatus(t, db, jobID, "running")
	insertSnapshot(t, db, staleRunID, sourceID, sharedFile)

	result := RecoverFromCrash(db, backupDir)

	if result.FilesCleanedUp != 0 {
		t.Errorf("FilesCleanedUp = %d, want 0 (file referenced by successful run must survive)", result.FilesCleanedUp)
	}
	if _, err := os.Stat(sharedFile); err != nil {
		t.Errorf("shared file was unexpectedly removed: %v", err)
	}
}
