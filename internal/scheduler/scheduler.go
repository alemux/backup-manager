// internal/scheduler/scheduler.go
package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/backupmanager/backupmanager/internal/backup"
	"github.com/backupmanager/backupmanager/internal/database"
	"github.com/robfig/cron/v3"
)

// RunnerInterface is the interface the Scheduler uses to execute backup jobs.
// This allows tests to inject a mock.
type RunnerInterface interface {
	Run(ctx context.Context, jobID int) (*backup.RunResult, error)
}

// Scheduler manages cron-based backup job execution.
type Scheduler struct {
	cron    *cron.Cron
	runner  RunnerInterface
	db      *database.Database
	mu      sync.Mutex
	entries map[int]cron.EntryID // jobID → cron entry ID
}

// New creates a new Scheduler with the given runner and database.
func New(runner RunnerInterface, db *database.Database) *Scheduler {
	return &Scheduler{
		cron:    cron.New(),
		runner:  runner,
		db:      db,
		entries: make(map[int]cron.EntryID),
	}
}

// CheckBandwidthWindow checks whether the current time falls within a period
// where bandwidth-limited jobs should run. For jobs with bandwidth_limit_mbps
// set, this always returns true (the limit is applied during sync, not here).
// This function is a hook point for future time-of-day policy enforcement.
func CheckBandwidthWindow(bandwidthLimitMbps *int) bool {
	// Current policy: always allow execution regardless of time-of-day.
	// The bandwidth limit is enforced at the sync layer via SyncOptions.BandwidthLimitKBps.
	// A future implementation could restrict high-bandwidth jobs to off-peak hours.
	return true
}

// StartSQLiteBackup registers a daily cron job that copies the SQLite database
// file to the given backupDir, keeping the last 7 copies.
func (s *Scheduler) StartSQLiteBackup(dbPath, backupDir string) {
	_, err := s.cron.AddFunc("0 3 * * *", func() {
		if err := backup.BackupSQLiteDB(dbPath, backupDir); err != nil {
			log.Printf("scheduler: SQLite self-backup failed: %v", err)
		} else {
			log.Printf("scheduler: SQLite self-backup completed to %s", backupDir)
		}
	})
	if err != nil {
		log.Printf("scheduler: failed to register SQLite self-backup job: %v", err)
	}
}

// Start loads all enabled jobs from the DB and starts the cron scheduler.
func (s *Scheduler) Start() error {
	rows, err := s.db.DB().Query(
		`SELECT id, name, schedule FROM backup_jobs WHERE enabled = 1`,
	)
	if err != nil {
		return fmt.Errorf("load enabled jobs: %w", err)
	}
	defer rows.Close()

	type jobRow struct {
		id       int
		name     string
		schedule string
	}
	var jobs []jobRow
	for rows.Next() {
		var j jobRow
		if err := rows.Scan(&j.id, &j.name, &j.schedule); err != nil {
			return fmt.Errorf("scan job row: %w", err)
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate job rows: %w", err)
	}

	for _, j := range jobs {
		if err := s.AddJob(j.id, j.schedule); err != nil {
			log.Printf("scheduler: failed to register job %d (%s): %v", j.id, j.name, err)
		}
	}

	s.cron.Start()
	log.Printf("scheduler: started with %d job(s)", len(jobs))

	s.StartMissedBackupDetection(15 * time.Minute)

	return nil
}

// Stop gracefully stops the scheduler.
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
	log.Println("scheduler: stopped")
}

// AddJob registers a job with its cron schedule.
func (s *Scheduler) AddJob(jobID int, schedule string) error {
	entryID, err := s.cron.AddFunc(schedule, s.makeJobFunc(jobID))
	if err != nil {
		return fmt.Errorf("add cron job %d with schedule %q: %w", jobID, schedule, err)
	}

	s.mu.Lock()
	s.entries[jobID] = entryID
	s.mu.Unlock()

	log.Printf("scheduler: registered job %d with schedule %q", jobID, schedule)
	return nil
}

// RemoveJob removes a job from the scheduler.
func (s *Scheduler) RemoveJob(jobID int) {
	s.mu.Lock()
	entryID, ok := s.entries[jobID]
	if ok {
		delete(s.entries, jobID)
	}
	s.mu.Unlock()

	if ok {
		s.cron.Remove(entryID)
		log.Printf("scheduler: removed job %d", jobID)
	}
}

// UpdateJob updates a job's cron schedule.
func (s *Scheduler) UpdateJob(jobID int, schedule string) error {
	// Remove existing entry (if any) then add with new schedule.
	s.RemoveJob(jobID)
	return s.AddJob(jobID, schedule)
}

// TriggerJob manually triggers a job immediately in a background goroutine.
func (s *Scheduler) TriggerJob(jobID int) error {
	// Verify the job exists in the DB.
	var name string
	err := s.db.DB().QueryRow(
		`SELECT name FROM backup_jobs WHERE id = ?`, jobID,
	).Scan(&name)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("job %d not found", jobID)
		}
		return fmt.Errorf("lookup job %d: %w", jobID, err)
	}

	go s.executeJob(jobID)
	log.Printf("scheduler: manually triggered job %d (%s)", jobID, name)
	return nil
}

// StartMissedBackupDetection runs a periodic check for missed backups.
func (s *Scheduler) StartMissedBackupDetection(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			s.detectMissedBackups()
		}
	}()
}

// makeJobFunc returns a cron-compatible function for the given job ID.
func (s *Scheduler) makeJobFunc(jobID int) func() {
	return func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("scheduler: panic in job %d: %v", jobID, r)
			}
		}()
		s.executeJob(jobID)
	}
}

// executeJob loads the job name and calls the runner, logging the outcome.
func (s *Scheduler) executeJob(jobID int) {
	// Load job name for logging.
	var name string
	err := s.db.DB().QueryRow(
		`SELECT name FROM backup_jobs WHERE id = ?`, jobID,
	).Scan(&name)
	if err != nil {
		log.Printf("scheduler: cannot load job %d for execution: %v", jobID, err)
		return
	}

	log.Printf("scheduler: starting backup job: %s (id=%d)", name, jobID)
	result, err := s.runner.Run(context.Background(), jobID)
	if err != nil {
		log.Printf("scheduler: backup job %s (id=%d) failed: %v", name, jobID, err)
		return
	}
	log.Printf("scheduler: backup job %s (id=%d) completed with status=%s files=%d size=%d bytes",
		name, jobID, result.Status, result.FilesCopied, result.TotalSize)
}

// detectMissedBackups scans enabled jobs and logs warnings for jobs whose
// last run is older than twice their expected schedule interval.
func (s *Scheduler) detectMissedBackups() {
	rows, err := s.db.DB().Query(
		`SELECT id, name, schedule FROM backup_jobs WHERE enabled = 1`,
	)
	if err != nil {
		log.Printf("scheduler: missed backup detection query failed: %v", err)
		return
	}
	defer rows.Close()

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

	for rows.Next() {
		var (
			jobID    int
			name     string
			schedule string
		)
		if err := rows.Scan(&jobID, &name, &schedule); err != nil {
			log.Printf("scheduler: missed backup detection scan error: %v", err)
			continue
		}

		// Parse the schedule to determine the interval.
		sched, err := parser.Parse(schedule)
		if err != nil {
			// Cannot determine interval; skip.
			continue
		}

		// Calculate the expected interval by seeing when the next two fires
		// would occur from an arbitrary reference point.
		now := time.Now()
		next1 := sched.Next(now)
		next2 := sched.Next(next1)
		interval := next2.Sub(next1)
		if interval <= 0 {
			continue
		}

		// Check the most recent backup run for this job.
		// SQLite stores datetime as a string, so we scan into a NullString
		// and parse it ourselves.
		var lastRunStr sql.NullString
		err = s.db.DB().QueryRow(
			`SELECT MAX(started_at) FROM backup_runs WHERE job_id = ?`, jobID,
		).Scan(&lastRunStr)
		if err != nil {
			log.Printf("scheduler: cannot query last run for job %d: %v", jobID, err)
			continue
		}

		if !lastRunStr.Valid {
			// Job has never run — not necessarily a missed backup.
			continue
		}

		// SQLite datetime format: "2006-01-02 15:04:05" (UTC).
		lastRunTime, parseErr := time.Parse("2006-01-02 15:04:05", lastRunStr.String)
		if parseErr != nil {
			// Try alternate format with T separator.
			lastRunTime, parseErr = time.Parse(time.RFC3339, lastRunStr.String)
			if parseErr != nil {
				log.Printf("scheduler: cannot parse last run time %q for job %d: %v", lastRunStr.String, jobID, parseErr)
				continue
			}
		}
		lastRunTime = lastRunTime.UTC()

		age := now.Sub(lastRunTime)
		if age > 2*interval {
			log.Printf("scheduler: WARNING missed backup: job %s (id=%d) last ran %v ago (expected every %v)",
				name, jobID, age.Truncate(time.Second), interval.Truncate(time.Second))
		}
	}
}
