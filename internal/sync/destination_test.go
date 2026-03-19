// internal/sync/destination_test.go
package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
)

// openTestDB opens an in-memory SQLite database and runs migrations.
func openTestDB(t *testing.T) *database.Database {
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

// insertDestination inserts a secondary (non-primary) destination and returns its ID.
func insertDestination(t *testing.T, db *database.Database, name string, path string, isPrimary, enabled int) int {
	t.Helper()
	res, err := db.DB().Exec(
		`INSERT INTO destinations (name, type, path, is_primary, retention_daily, retention_weekly, retention_monthly, enabled)
		 VALUES (?, 'local', ?, ?, 7, 4, 3, ?)`,
		name, path, isPrimary, enabled,
	)
	if err != nil {
		t.Fatalf("insert destination: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

// insertSnapshot creates a minimal backup_run + backup_snapshot and returns the snapshot ID.
func insertSnapshot(t *testing.T, db *database.Database, snapshotPath, checksum string) int {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)

	// Insert a minimal server + source + job + run chain to satisfy FKs.
	srvRes, err := db.DB().Exec(
		`INSERT INTO servers (name, type, host, port, connection_type, status, created_at, updated_at)
		 VALUES ('srv', 'linux', 'localhost', 22, 'ssh', 'online', ?, ?)`, now, now,
	)
	if err != nil {
		t.Fatalf("insert server: %v", err)
	}
	srvID, _ := srvRes.LastInsertId()

	srcRes, err := db.DB().Exec(
		`INSERT INTO backup_sources (server_id, name, type, priority, enabled, created_at)
		 VALUES (?, 'src', 'web', 0, 1, ?)`, srvID, now,
	)
	if err != nil {
		t.Fatalf("insert source: %v", err)
	}
	srcID, _ := srcRes.LastInsertId()

	jobRes, err := db.DB().Exec(
		`INSERT INTO backup_jobs (name, server_id, schedule, retention_daily, retention_weekly, retention_monthly, timeout_minutes, enabled, created_at, updated_at)
		 VALUES ('job', ?, '0 3 * * *', 7, 4, 3, 120, 1, ?, ?)`, srvID, now, now,
	)
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}
	jobID, _ := jobRes.LastInsertId()

	runRes, err := db.DB().Exec(
		`INSERT INTO backup_runs (job_id, status, created_at) VALUES (?, 'success', ?)`, jobID, now,
	)
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}
	runID, _ := runRes.LastInsertId()

	snapRes, err := db.DB().Exec(
		`INSERT INTO backup_snapshots (run_id, source_id, snapshot_path, size_bytes, checksum_sha256, created_at)
		 VALUES (?, ?, ?, 0, ?, ?)`,
		runID, srcID, snapshotPath, checksum, now,
	)
	if err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	snapID, _ := snapRes.LastInsertId()
	return int(snapID)
}

// TestQueueSync verifies that QueueSync creates pending entries for each enabled
// non-primary destination and ignores primary / disabled destinations.
func TestQueueSync(t *testing.T) {
	db := openTestDB(t)
	syncer := NewDestinationSyncer(db)

	dir := t.TempDir()
	snapID := insertSnapshot(t, db, filepath.Join(dir, "snap.tar.gz"), "")

	// Primary destination — should be ignored.
	insertDestination(t, db, "Primary", dir, 1, 1)
	// Disabled secondary — should be ignored.
	insertDestination(t, db, "Disabled", dir, 0, 0)
	// Two enabled secondaries.
	dest1 := insertDestination(t, db, "NAS1", dir, 0, 1)
	dest2 := insertDestination(t, db, "NAS2", dir, 0, 1)

	if err := syncer.QueueSync(snapID); err != nil {
		t.Fatalf("QueueSync: %v", err)
	}

	rows, err := db.DB().Query(
		`SELECT destination_id, status FROM destination_sync_status WHERE snapshot_id = ? ORDER BY destination_id`,
		snapID,
	)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	got := map[int]string{}
	for rows.Next() {
		var destID int
		var status string
		if err := rows.Scan(&destID, &status); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[destID] = status
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(got), got)
	}
	for _, destID := range []int{dest1, dest2} {
		if got[destID] != "pending" {
			t.Errorf("expected pending for dest %d, got %q", destID, got[destID])
		}
	}
}

// TestProcessQueue_Success verifies that a pending entry is copied and marked success.
func TestProcessQueue_Success(t *testing.T) {
	db := openTestDB(t)
	syncer := NewDestinationSyncer(db)

	srcDir := t.TempDir()
	destDir := t.TempDir()

	// Create a real source file.
	srcFile := filepath.Join(srcDir, "backup.tar.gz")
	if err := os.WriteFile(srcFile, []byte("hello backup"), 0o644); err != nil {
		t.Fatalf("write src file: %v", err)
	}

	snapID := insertSnapshot(t, db, srcFile, "")
	destID := insertDestination(t, db, "NAS", destDir, 0, 1)

	// Insert pending entry manually.
	db.DB().Exec(
		`INSERT INTO destination_sync_status (snapshot_id, destination_id, status, retry_count) VALUES (?, ?, 'pending', 0)`,
		snapID, destID,
	)

	if err := syncer.ProcessQueue(context.Background()); err != nil {
		t.Fatalf("ProcessQueue: %v", err)
	}

	var status string
	var syncedAt *string
	db.DB().QueryRow(
		`SELECT status, synced_at FROM destination_sync_status WHERE snapshot_id=? AND destination_id=?`,
		snapID, destID,
	).Scan(&status, &syncedAt)

	if status != "success" {
		t.Errorf("expected success, got %q", status)
	}
	if syncedAt == nil {
		t.Error("expected synced_at to be set")
	}

	// Verify file was actually copied.
	destFile := filepath.Join(destDir, "backup.tar.gz")
	if _, err := os.Stat(destFile); os.IsNotExist(err) {
		t.Error("expected file to be copied to destination")
	}
}

// TestProcessQueue_Failure verifies that a missing source file increments retry
// and records the error.
func TestProcessQueue_Failure(t *testing.T) {
	db := openTestDB(t)
	syncer := NewDestinationSyncer(db)

	destDir := t.TempDir()

	// Snapshot points to a non-existent file.
	snapID := insertSnapshot(t, db, "/nonexistent/path/backup.tar.gz", "")
	destID := insertDestination(t, db, "NAS", destDir, 0, 1)

	db.DB().Exec(
		`INSERT INTO destination_sync_status (snapshot_id, destination_id, status, retry_count) VALUES (?, ?, 'pending', 0)`,
		snapID, destID,
	)

	if err := syncer.ProcessQueue(context.Background()); err != nil {
		t.Fatalf("ProcessQueue: %v", err)
	}

	var status string
	var retryCount int
	var lastError *string
	db.DB().QueryRow(
		`SELECT status, retry_count, last_error FROM destination_sync_status WHERE snapshot_id=? AND destination_id=?`,
		snapID, destID,
	).Scan(&status, &retryCount, &lastError)

	if status != "pending" {
		t.Errorf("expected pending after first failure, got %q", status)
	}
	if retryCount != 1 {
		t.Errorf("expected retry_count=1, got %d", retryCount)
	}
	if lastError == nil || *lastError == "" {
		t.Error("expected last_error to be set")
	}
}

// TestProcessQueue_MaxRetries verifies that after 5 retries the status stays failed.
func TestProcessQueue_MaxRetries(t *testing.T) {
	db := openTestDB(t)
	syncer := NewDestinationSyncer(db)

	destDir := t.TempDir()
	snapID := insertSnapshot(t, db, "/nonexistent/missing.tar.gz", "")
	destID := insertDestination(t, db, "NAS", destDir, 0, 1)

	// Pre-set retry_count to 4 so one more failure should hit the limit.
	db.DB().Exec(
		`INSERT INTO destination_sync_status (snapshot_id, destination_id, status, retry_count) VALUES (?, ?, 'pending', 4)`,
		snapID, destID,
	)

	if err := syncer.ProcessQueue(context.Background()); err != nil {
		t.Fatalf("ProcessQueue: %v", err)
	}

	var status string
	var retryCount int
	db.DB().QueryRow(
		`SELECT status, retry_count FROM destination_sync_status WHERE snapshot_id=? AND destination_id=?`,
		snapID, destID,
	).Scan(&status, &retryCount)

	if status != "failed" {
		t.Errorf("expected failed after max retries, got %q", status)
	}
	if retryCount != 5 {
		t.Errorf("expected retry_count=5, got %d", retryCount)
	}
}

// TestRecoverStale verifies that in_progress entries are reset to pending on startup.
func TestRecoverStale(t *testing.T) {
	db := openTestDB(t)
	syncer := NewDestinationSyncer(db)

	dir := t.TempDir()
	snapID := insertSnapshot(t, db, filepath.Join(dir, "snap.tar.gz"), "")
	destID := insertDestination(t, db, "NAS", dir, 0, 1)

	db.DB().Exec(
		`INSERT INTO destination_sync_status (snapshot_id, destination_id, status, retry_count) VALUES (?, ?, 'in_progress', 0)`,
		snapID, destID,
	)

	if err := syncer.RecoverStale(); err != nil {
		t.Fatalf("RecoverStale: %v", err)
	}

	var status string
	db.DB().QueryRow(
		`SELECT status FROM destination_sync_status WHERE snapshot_id=? AND destination_id=?`,
		snapID, destID,
	).Scan(&status)

	if status != "pending" {
		t.Errorf("expected pending after RecoverStale, got %q", status)
	}
}

// TestGetSyncStatus verifies that all sync status entries for a snapshot are returned.
func TestGetSyncStatus(t *testing.T) {
	db := openTestDB(t)
	syncer := NewDestinationSyncer(db)

	dir := t.TempDir()
	snapID := insertSnapshot(t, db, filepath.Join(dir, "snap.tar.gz"), "")
	dest1 := insertDestination(t, db, "NAS1", dir, 0, 1)
	dest2 := insertDestination(t, db, "NAS2", dir, 0, 1)

	now := time.Now().UTC().Format(time.RFC3339)
	db.DB().Exec(
		`INSERT INTO destination_sync_status (snapshot_id, destination_id, status, retry_count, synced_at) VALUES (?, ?, 'success', 0, ?)`,
		snapID, dest1, now,
	)
	db.DB().Exec(
		`INSERT INTO destination_sync_status (snapshot_id, destination_id, status, retry_count, last_error) VALUES (?, ?, 'failed', 3, 'timeout')`,
		snapID, dest2,
	)

	entries, err := syncer.GetSyncStatus(snapID)
	if err != nil {
		t.Fatalf("GetSyncStatus: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	byDest := map[int]SyncStatusEntry{}
	for _, e := range entries {
		byDest[e.DestinationID] = e
	}

	e1 := byDest[dest1]
	if e1.Status != "success" {
		t.Errorf("expected success for dest1, got %q", e1.Status)
	}
	if e1.SyncedAt == nil {
		t.Error("expected synced_at for dest1")
	}
	if e1.DestName != "NAS1" {
		t.Errorf("expected dest name NAS1, got %q", e1.DestName)
	}

	e2 := byDest[dest2]
	if e2.Status != "failed" {
		t.Errorf("expected failed for dest2, got %q", e2.Status)
	}
	if e2.LastError != "timeout" {
		t.Errorf("expected last_error 'timeout', got %q", e2.LastError)
	}
	if e2.RetryCount != 3 {
		t.Errorf("expected retry_count=3, got %d", e2.RetryCount)
	}
}
