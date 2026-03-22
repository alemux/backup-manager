package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
	"github.com/backupmanager/backupmanager/internal/sync"
)

// Runner wraps the Orchestrator with job-level concerns such as
// concurrency prevention and timeout enforcement.
type Runner struct {
	orchestrator *Orchestrator
	db           *database.Database
}

// NewRunner creates a new Runner.
func NewRunner(orch *Orchestrator, db *database.Database) *Runner {
	return &Runner{
		orchestrator: orch,
		db:           db,
	}
}

// Run executes a backup job. It checks for concurrent runs on the same server
// and enforces the job timeout.
func (r *Runner) Run(ctx context.Context, jobID int) (*RunResult, error) {
	// 1. Load the job to get server_id and timeout.
	var serverID int
	var timeoutMinutes int
	err := r.db.DB().QueryRow(
		`SELECT server_id, timeout_minutes FROM backup_jobs WHERE id = ?`, jobID,
	).Scan(&serverID, &timeoutMinutes)
	if err != nil {
		return nil, fmt.Errorf("load job %d: %w", jobID, err)
	}

	// 2. Check no other run is "running" for the same server.
	var runningCount int
	err = r.db.DB().QueryRow(
		`SELECT COUNT(*) FROM backup_runs br
		 INNER JOIN backup_jobs bj ON bj.id = br.job_id
		 WHERE bj.server_id = ? AND br.status = 'running'`, serverID,
	).Scan(&runningCount)
	if err != nil {
		return nil, fmt.Errorf("check concurrent runs: %w", err)
	}
	if runningCount > 0 {
		return nil, fmt.Errorf("another backup is already running for server %d", serverID)
	}

	// 3. Create context with timeout.
	timeout := time.Duration(timeoutMinutes) * time.Minute
	if timeout <= 0 {
		timeout = 120 * time.Minute // default 2 hours
	}
	tCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 4. Call orchestrator.ExecuteJob.
	result, err := r.orchestrator.ExecuteJob(tCtx, jobID)
	if err != nil {
		// Check if the error was due to context timeout.
		if tCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("job %d timed out after %v: %w", jobID, timeout, err)
		}
		return nil, err
	}

	// 5. If context timed out during execution, the orchestrator may have
	// already set the status to "timeout". We double-check here.
	if tCtx.Err() == context.DeadlineExceeded && result.Status != "timeout" {
		result.Status = "timeout"
	}

	return result, nil
}

// RunWithOptions executes a backup job with live streaming and stop capability.
// logFunc is called for each line of rsync/lftp output.
// tracker allows external code to stop the running process.
func (r *Runner) RunWithOptions(ctx context.Context, jobID int, logFunc func(string), tracker *sync.ProcessTracker) (*RunResult, error) {
	var serverID int
	var timeoutMinutes int
	err := r.db.DB().QueryRow(
		`SELECT server_id, timeout_minutes FROM backup_jobs WHERE id = ?`, jobID,
	).Scan(&serverID, &timeoutMinutes)
	if err != nil {
		return nil, fmt.Errorf("load job %d: %w", jobID, err)
	}

	var runningCount int
	err = r.db.DB().QueryRow(
		`SELECT COUNT(*) FROM backup_runs br
		 INNER JOIN backup_jobs bj ON bj.id = br.job_id
		 WHERE bj.server_id = ? AND br.status = 'running'`, serverID,
	).Scan(&runningCount)
	if err != nil {
		return nil, fmt.Errorf("check concurrent runs: %w", err)
	}
	if runningCount > 0 {
		return nil, fmt.Errorf("another backup is already running for server %d", serverID)
	}

	timeout := time.Duration(timeoutMinutes) * time.Minute
	if timeout <= 0 {
		timeout = 120 * time.Minute
	}
	tCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := r.orchestrator.ExecuteJobWithOptions(tCtx, jobID, logFunc, tracker)
	if err != nil {
		if tCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("job %d timed out after %v: %w", jobID, timeout, err)
		}
		return nil, err
	}

	if tCtx.Err() == context.DeadlineExceeded && result.Status != "timeout" {
		result.Status = "timeout"
	}

	return result, nil
}
