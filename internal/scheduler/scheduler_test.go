// internal/scheduler/scheduler_test.go
package scheduler

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/backupmanager/backupmanager/internal/backup"
	"github.com/backupmanager/backupmanager/internal/database"
)

// ---------------------------------------------------------------------------
// MockRunner
// ---------------------------------------------------------------------------

// MockRunner implements RunnerInterface for testing.
type MockRunner struct {
	RunFunc func(ctx context.Context, jobID int) (*backup.RunResult, error)
	mu      sync.Mutex
	calls   []int // track jobIDs passed to Run
}

func (m *MockRunner) Run(ctx context.Context, jobID int) (*backup.RunResult, error) {
	m.mu.Lock()
	m.calls = append(m.calls, jobID)
	m.mu.Unlock()

	if m.RunFunc != nil {
		return m.RunFunc(ctx, jobID)
	}
	return &backup.RunResult{Status: "success"}, nil
}

func (m *MockRunner) Calls() []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]int, len(m.calls))
	copy(out, m.calls)
	return out
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

func insertJob(t *testing.T, db *database.Database, name, schedule string, enabled bool) int {
	t.Helper()
	enabledVal := 1
	if !enabled {
		enabledVal = 0
	}
	// Insert a minimal server first (required FK).
	res, err := db.DB().Exec(
		`INSERT INTO servers (name, type, host, port, connection_type, username)
		 VALUES ('test-server', 'linux', '127.0.0.1', 22, 'ssh', 'backup')`,
	)
	if err != nil {
		t.Fatalf("insert server: %v", err)
	}
	serverID, _ := res.LastInsertId()

	res, err = db.DB().Exec(
		`INSERT INTO backup_jobs (name, server_id, schedule, timeout_minutes, enabled)
		 VALUES (?, ?, ?, 120, ?)`,
		name, serverID, schedule, enabledVal,
	)
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSchedulerAddRemoveJob(t *testing.T) {
	db := setupTestDB(t)
	runner := &MockRunner{}
	s := New(runner, db)
	defer s.Stop()
	s.cron.Start()

	jobID := 42

	// Add a job.
	if err := s.AddJob(jobID, "@every 1h"); err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	s.mu.Lock()
	_, tracked := s.entries[jobID]
	s.mu.Unlock()
	if !tracked {
		t.Error("job should be tracked after AddJob")
	}

	// Remove it.
	s.RemoveJob(jobID)

	s.mu.Lock()
	_, tracked = s.entries[jobID]
	s.mu.Unlock()
	if tracked {
		t.Error("job should not be tracked after RemoveJob")
	}
}

func TestSchedulerUpdateJob(t *testing.T) {
	db := setupTestDB(t)
	runner := &MockRunner{}
	s := New(runner, db)
	defer s.Stop()
	s.cron.Start()

	jobID := 10

	// Add with initial schedule.
	if err := s.AddJob(jobID, "@every 1h"); err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	s.mu.Lock()
	firstEntryID := s.entries[jobID]
	s.mu.Unlock()

	// Update to a different schedule.
	if err := s.UpdateJob(jobID, "@every 2h"); err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}

	s.mu.Lock()
	newEntryID, tracked := s.entries[jobID]
	s.mu.Unlock()

	if !tracked {
		t.Error("job should still be tracked after UpdateJob")
	}
	if newEntryID == firstEntryID {
		t.Error("entry ID should have changed after UpdateJob (old removed, new added)")
	}
}

func TestSchedulerTriggerJob(t *testing.T) {
	db := setupTestDB(t)
	runner := &MockRunner{}
	s := New(runner, db)
	defer s.Stop()

	// Insert a real job in the DB so TriggerJob can look it up.
	jobID := insertJob(t, db, "trigger-test", "@every 1h", true)

	if err := s.TriggerJob(jobID); err != nil {
		t.Fatalf("TriggerJob: %v", err)
	}

	// Give the background goroutine time to execute.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		calls := runner.Calls()
		if len(calls) > 0 && calls[0] == jobID {
			return // success
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("runner.Run was not called with jobID=%d within timeout; calls=%v", jobID, runner.Calls())
}

func TestSchedulerTriggerJob_NotFound(t *testing.T) {
	db := setupTestDB(t)
	runner := &MockRunner{}
	s := New(runner, db)
	defer s.Stop()

	err := s.TriggerJob(99999)
	if err == nil {
		t.Error("expected error for non-existent job")
	}
}

func TestSchedulerStartLoadsFromDB(t *testing.T) {
	db := setupTestDB(t)
	runner := &MockRunner{}

	// Insert two enabled jobs and one disabled.
	insertJob(t, db, "enabled-1", "@every 1h", true)
	insertJob(t, db, "enabled-2", "@every 2h", true)
	insertJob(t, db, "disabled-1", "@every 3h", false)

	s := New(runner, db)
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	s.mu.Lock()
	count := len(s.entries)
	s.mu.Unlock()

	// Only the two enabled jobs should be registered.
	if count != 2 {
		t.Errorf("expected 2 registered entries, got %d", count)
	}
}

func TestSchedulerStopIsClean(t *testing.T) {
	db := setupTestDB(t)
	runner := &MockRunner{}
	s := New(runner, db)

	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Stop should not panic or block indefinitely.
	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Error("Stop did not complete within 5 seconds")
	}
}

func TestSchedulerJobFiresOnSchedule(t *testing.T) {
	db := setupTestDB(t)
	runner := &MockRunner{}

	// Insert a real job so executeJob can find its name.
	jobID := insertJob(t, db, "fast-job", "@every 1s", true)

	s := New(runner, db)
	// Register with a 1-second interval.
	if err := s.AddJob(jobID, "@every 1s"); err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	s.cron.Start()
	defer s.Stop()

	// Wait up to 3 seconds for at least one automatic execution.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(runner.Calls()) > 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("cron job did not fire within 3 seconds")
}

func TestMissedBackupDetection(t *testing.T) {
	db := setupTestDB(t)
	runner := &MockRunner{}

	// Insert a job.
	jobID := insertJob(t, db, "old-job", "@every 1h", true)

	// Insert a backup_run with a started_at far in the past (3 hours ago),
	// which is > 2× the 1-hour interval.
	_, err := db.DB().Exec(
		`INSERT INTO backup_runs (job_id, status, started_at, finished_at)
		 VALUES (?, 'success', datetime('now', '-3 hours'), datetime('now', '-3 hours', '+1 minute'))`,
		jobID,
	)
	if err != nil {
		t.Fatalf("insert old run: %v", err)
	}

	s := New(runner, db)

	// detectMissedBackups should not panic and should log the warning.
	// We can't easily capture the log output, but we verify no panic occurs
	// and the function completes.
	completed := make(chan struct{})
	go func() {
		s.detectMissedBackups()
		close(completed)
	}()

	select {
	case <-completed:
		// success — function ran without panic
	case <-time.After(5 * time.Second):
		t.Error("detectMissedBackups did not complete within 5 seconds")
	}
}

func TestMissedBackupDetection_RecentRun_NoWarning(t *testing.T) {
	db := setupTestDB(t)
	runner := &MockRunner{}

	// Insert a job.
	jobID := insertJob(t, db, "recent-job", "@every 1h", true)

	// Insert a very recent run (5 minutes ago) — well within the 2-hour threshold.
	_, err := db.DB().Exec(
		`INSERT INTO backup_runs (job_id, status, started_at, finished_at)
		 VALUES (?, 'success', datetime('now', '-5 minutes'), datetime('now', '-4 minutes'))`,
		jobID,
	)
	if err != nil {
		t.Fatalf("insert recent run: %v", err)
	}

	s := New(runner, db)

	// Should complete without issues.
	done := make(chan struct{})
	go func() {
		s.detectMissedBackups()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("detectMissedBackups timed out")
	}
}
