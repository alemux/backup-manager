package backup

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
	"github.com/backupmanager/backupmanager/internal/sync"
)

func setupRunnerTestDB(t *testing.T) *database.Database {
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

func TestRunner_PreventsConcurrentRuns(t *testing.T) {
	db := setupRunnerTestDB(t)
	serverID := insertTestServer(t, db, "srv1", "linux")
	srcID := insertTestSource(t, db, serverID, "web-files", "web", "/var/www", nil, 0)
	jobID := insertTestJob(t, db, "daily-backup", serverID, []int{srcID})

	// Insert a "running" run for this server's job.
	_, err := db.DB().Exec(
		`INSERT INTO backup_runs (job_id, status, started_at) VALUES (?, 'running', datetime('now'))`,
		jobID,
	)
	if err != nil {
		t.Fatalf("insert running run: %v", err)
	}

	orch := NewOrchestrator(db)
	orch.SetBackupDir(t.TempDir())
	orch.SetRsyncSyncer(&MockSyncer{})

	runner := NewRunner(orch, db)

	_, err = runner.Run(context.Background(), jobID)
	if err == nil {
		t.Fatal("expected error for concurrent run, got nil")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("expected 'already running' error, got: %v", err)
	}
}

func TestRunner_RespectsTimeout(t *testing.T) {
	db := setupRunnerTestDB(t)
	serverID := insertTestServer(t, db, "srv1", "linux")
	srcID := insertTestSource(t, db, serverID, "web-files", "web", "/var/www", nil, 0)

	// Create job with very short timeout (we set it to 1 minute in DB, but
	// we'll override it to something shorter via direct SQL update).
	jobID := insertTestJob(t, db, "quick-backup", serverID, []int{srcID})
	// Set timeout to minimum (we can't go below 1 minute in DB, but the runner
	// reads timeout_minutes). We'll make the syncer block long enough.
	_, err := db.DB().Exec("UPDATE backup_jobs SET timeout_minutes = 1 WHERE id = ?", jobID)
	if err != nil {
		t.Fatalf("update timeout: %v", err)
	}

	orch := NewOrchestrator(db)
	orch.SetBackupDir(t.TempDir())
	// Syncer that blocks and respects context cancellation.
	orch.SetRsyncSyncer(&MockSyncer{
		SyncFunc: func(ctx context.Context, source sync.SyncSource, destPath string, opts sync.SyncOptions) (*sync.SyncResult, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(10 * time.Minute):
				return &sync.SyncResult{}, nil
			}
		},
	})

	runner := NewRunner(orch, db)

	// Use a parent context with a much shorter timeout to actually test this quickly.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	result, err := runner.Run(ctx, jobID)

	// The run should either return with timeout status or an error about timeout.
	if err != nil {
		// This is acceptable: the context timed out.
		if !strings.Contains(err.Error(), "timed out") && !strings.Contains(err.Error(), "context deadline exceeded") {
			t.Errorf("expected timeout-related error, got: %v", err)
		}
		return
	}

	// If no error, the result should indicate timeout.
	if result.Status != "timeout" {
		t.Errorf("result.Status = %q, want %q", result.Status, "timeout")
	}
}

func TestRunner_SuccessfulRun(t *testing.T) {
	db := setupRunnerTestDB(t)
	serverID := insertTestServer(t, db, "srv1", "linux")
	srcID := insertTestSource(t, db, serverID, "web-files", "web", "/var/www", nil, 0)
	jobID := insertTestJob(t, db, "daily-backup", serverID, []int{srcID})

	orch := NewOrchestrator(db)
	orch.SetBackupDir(t.TempDir())
	orch.SetRsyncSyncer(&MockSyncer{
		SyncFunc: func(ctx context.Context, source sync.SyncSource, destPath string, opts sync.SyncOptions) (*sync.SyncResult, error) {
			return &sync.SyncResult{FilesCopied: 10, BytesCopied: 5000}, nil
		},
	})

	runner := NewRunner(orch, db)
	result, err := runner.Run(context.Background(), jobID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "success" {
		t.Errorf("result.Status = %q, want %q", result.Status, "success")
	}
	if result.TotalSize != 5000 {
		t.Errorf("result.TotalSize = %d, want 5000", result.TotalSize)
	}
	if result.FilesCopied != 10 {
		t.Errorf("result.FilesCopied = %d, want 10", result.FilesCopied)
	}
}

func TestRunner_AllowsRunAfterPreviousCompletes(t *testing.T) {
	db := setupRunnerTestDB(t)
	serverID := insertTestServer(t, db, "srv1", "linux")
	srcID := insertTestSource(t, db, serverID, "web-files", "web", "/var/www", nil, 0)
	jobID := insertTestJob(t, db, "daily-backup", serverID, []int{srcID})

	// Insert a completed (not running) run for this server's job.
	_, err := db.DB().Exec(
		`INSERT INTO backup_runs (job_id, status, started_at, finished_at) VALUES (?, 'success', datetime('now', '-1 hour'), datetime('now'))`,
		jobID,
	)
	if err != nil {
		t.Fatalf("insert completed run: %v", err)
	}

	orch := NewOrchestrator(db)
	orch.SetBackupDir(t.TempDir())
	orch.SetRsyncSyncer(&MockSyncer{})

	runner := NewRunner(orch, db)
	result, err := runner.Run(context.Background(), jobID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "success" {
		t.Errorf("result.Status = %q, want %q", result.Status, "success")
	}
}
