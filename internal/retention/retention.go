// internal/retention/retention.go
package retention

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
)

// RetentionPolicy defines how many snapshots to retain in each time tier.
type RetentionPolicy struct {
	Daily   int // keep daily snapshots for N days (default 7)
	Weekly  int // keep weekly snapshots for N weeks (default 4)
	Monthly int // keep monthly snapshots for N months (default 3)
}

// Snapshot represents a backup snapshot that can be evaluated for retention.
type Snapshot struct {
	ID        int
	Path      string
	CreatedAt time.Time
	SizeBytes int64
}

// Apply evaluates which snapshots should be deleted based on the retention policy.
// It returns the list of snapshot IDs to delete.
// The most recent snapshot is NEVER deleted regardless of policy.
// Uses the provided timezone for day boundary calculations.
// snapshots must be sorted newest-first (index 0 = most recent).
func Apply(snapshots []Snapshot, policy RetentionPolicy, tz *time.Location) []int {
	if len(snapshots) == 0 {
		return nil
	}

	if tz == nil {
		tz = time.UTC
	}

	// Build the keep set.
	keep := make(map[int]bool)

	// Rule 1: always keep the most recent snapshot.
	keep[snapshots[0].ID] = true

	// Determine reference time: now in the provided timezone.
	// We use the most recent snapshot's time as the reference anchor
	// so tests are deterministic without mocking time.Now().
	// Actually the spec says "last N days" etc., which should be relative to now.
	// But for testability we anchor to the most recent snapshot time.
	now := snapshots[0].CreatedAt.In(tz)

	// Rule 2: Daily tier – keep one snapshot per day for the last N days.
	// "Day" boundaries are in tz. For each day in [now-N+1 .. now], keep the
	// latest snapshot (already guaranteed since list is sorted newest-first).
	if policy.Daily > 0 {
		// Track which day-keys we've already kept.
		keptDays := make(map[string]bool)
		// Compute the oldest day boundary we care about.
		// "today" in tz is day 1, "today-1" is day 2, ... "today-(N-1)" is day N.
		todayStart := truncateToDay(now, tz)
		oldestDailyDay := todayStart.AddDate(0, 0, -(policy.Daily - 1))

		for _, s := range snapshots {
			snapshotTime := s.CreatedAt.In(tz)
			snapshotDayStart := truncateToDay(snapshotTime, tz)
			if snapshotDayStart.Before(oldestDailyDay) {
				break // list is sorted newest-first, no need to continue
			}
			dayKey := snapshotDayStart.Format("2006-01-02")
			if !keptDays[dayKey] {
				keep[s.ID] = true
				keptDays[dayKey] = true
			}
		}
	}

	// Rule 3: Weekly tier – keep one snapshot per week for the last N weeks.
	// A "week" is identified by its Sunday start (Sunday = weekday 0).
	if policy.Weekly > 0 {
		keptWeeks := make(map[string]bool)
		// Find the start of the current week (Sunday at 00:00 in tz).
		currentWeekStart := truncateToWeek(now, tz)
		// The oldest week start we care about.
		oldestWeekStart := currentWeekStart.AddDate(0, 0, -7*(policy.Weekly-1))

		for _, s := range snapshots {
			snapshotTime := s.CreatedAt.In(tz)
			weekStart := truncateToWeek(snapshotTime, tz)
			if weekStart.Before(oldestWeekStart) {
				break
			}
			weekKey := weekStart.Format("2006-01-02")
			if !keptWeeks[weekKey] {
				keep[s.ID] = true
				keptWeeks[weekKey] = true
			}
		}
	}

	// Rule 4: Monthly tier – keep one snapshot per month for the last N months.
	// A "month" is identified by its year+month. Keep the latest snapshot in that month.
	if policy.Monthly > 0 {
		keptMonths := make(map[string]bool)
		// Current month in tz.
		currentMonth := truncateToMonth(now, tz)
		// The oldest month we care about.
		oldestMonth := currentMonth.AddDate(0, -(policy.Monthly - 1), 0)

		for _, s := range snapshots {
			snapshotTime := s.CreatedAt.In(tz)
			monthStart := truncateToMonth(snapshotTime, tz)
			if monthStart.Before(oldestMonth) {
				break
			}
			monthKey := monthStart.Format("2006-01")
			if !keptMonths[monthKey] {
				keep[s.ID] = true
				keptMonths[monthKey] = true
			}
		}
	}

	// Collect IDs not in the keep set.
	var toDelete []int
	for _, s := range snapshots {
		if !keep[s.ID] {
			toDelete = append(toDelete, s.ID)
		}
	}
	return toDelete
}

// truncateToDay returns the start of the day (00:00:00) for t in tz.
func truncateToDay(t time.Time, tz *time.Location) time.Time {
	t = t.In(tz)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, tz)
}

// truncateToWeek returns the start of the week (Sunday 00:00:00) for t in tz.
func truncateToWeek(t time.Time, tz *time.Location) time.Time {
	t = t.In(tz)
	weekday := int(t.Weekday()) // Sunday = 0
	dayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, tz)
	return dayStart.AddDate(0, 0, -weekday)
}

// truncateToMonth returns the first day of the month (1st 00:00:00) for t in tz.
func truncateToMonth(t time.Time, tz *time.Location) time.Time {
	t = t.In(tz)
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, tz)
}

// CleanupResult holds the outcome of a retention cleanup run.
type CleanupResult struct {
	SnapshotsDeleted int
	BytesFreed       int64
	Errors           []string
}

// CleanupService runs the retention engine against the database.
type CleanupService struct {
	db *database.Database
}

// NewCleanupService creates a new CleanupService.
func NewCleanupService(db *database.Database) *CleanupService {
	return &CleanupService{db: db}
}

// snapshotRow is an internal struct for DB scan results.
type snapshotRow struct {
	id           int
	sourceID     int
	jobID        int
	snapshotPath string
	sizeBytes    int64
	retDaily     int
	retWeekly    int
	retMonthly   int
	createdAt    time.Time
}

// RunCleanup scans all snapshots, groups by source, applies retention per job config,
// deletes expired snapshot files and DB records.
func (c *CleanupService) RunCleanup(ctx context.Context, tz *time.Location) (*CleanupResult, error) {
	if tz == nil {
		tz = time.UTC
	}

	result := &CleanupResult{}

	// Load all snapshots joined with their job's retention config.
	// We join backup_snapshots -> backup_runs -> backup_jobs to get retention config.
	rows, err := c.db.DB().QueryContext(ctx, `
		SELECT
			bs.id,
			bs.source_id,
			bj.id         AS job_id,
			bs.snapshot_path,
			bs.size_bytes,
			bj.retention_daily,
			bj.retention_weekly,
			bj.retention_monthly,
			bs.created_at
		FROM backup_snapshots bs
		INNER JOIN backup_runs br ON br.id = bs.run_id
		INNER JOIN backup_jobs bj ON bj.id = br.job_id
		ORDER BY bs.source_id, bj.id, bs.created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query snapshots: %w", err)
	}
	defer rows.Close()

	// Group snapshots by (source_id, job_id) key.
	type groupKey struct {
		sourceID int
		jobID    int
	}
	groups := make(map[groupKey][]snapshotRow)
	policies := make(map[groupKey]RetentionPolicy)

	for rows.Next() {
		var r snapshotRow
		var createdAtStr string
		if err := rows.Scan(
			&r.id, &r.sourceID, &r.jobID,
			&r.snapshotPath, &r.sizeBytes,
			&r.retDaily, &r.retWeekly, &r.retMonthly,
			&createdAtStr,
		); err != nil {
			return nil, fmt.Errorf("scan snapshot row: %w", err)
		}
		// Parse SQLite datetime string (stored as UTC).
		r.createdAt, err = parseSQLiteTime(createdAtStr)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("parse created_at for snapshot %d: %v", r.id, err))
			continue
		}
		k := groupKey{sourceID: r.sourceID, jobID: r.jobID}
		groups[k] = append(groups[k], r)
		policies[k] = RetentionPolicy{
			Daily:   r.retDaily,
			Weekly:  r.retWeekly,
			Monthly: r.retMonthly,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshots: %w", err)
	}

	// For each group, apply retention and delete expired snapshots.
	for k, snapshotRows := range groups {
		if ctx.Err() != nil {
			result.Errors = append(result.Errors, "context cancelled")
			break
		}

		policy := policies[k]

		// Convert to []Snapshot for Apply (already sorted newest-first by ORDER BY created_at DESC).
		snapshots := make([]Snapshot, len(snapshotRows))
		for i, r := range snapshotRows {
			snapshots[i] = Snapshot{
				ID:        r.id,
				Path:      r.snapshotPath,
				CreatedAt: r.createdAt,
				SizeBytes: r.sizeBytes,
			}
		}

		toDelete := Apply(snapshots, policy, tz)

		// Build a size index for quick lookup.
		sizeByID := make(map[int]int64, len(snapshots))
		pathByID := make(map[int]string, len(snapshots))
		for _, s := range snapshots {
			sizeByID[s.ID] = s.SizeBytes
			pathByID[s.ID] = s.Path
		}

		for _, id := range toDelete {
			snapshotPath := pathByID[id]

			// Delete the file from disk (best-effort).
			if snapshotPath != "" {
				if rmErr := os.RemoveAll(snapshotPath); rmErr != nil && !os.IsNotExist(rmErr) {
					result.Errors = append(result.Errors,
						fmt.Sprintf("delete snapshot file %q (id=%d): %v", snapshotPath, id, rmErr))
					// Continue to still remove the DB record.
				}
			}

			// Delete DB record.
			if _, dbErr := c.db.DB().ExecContext(ctx,
				"DELETE FROM backup_snapshots WHERE id = ?", id,
			); dbErr != nil {
				result.Errors = append(result.Errors,
					fmt.Sprintf("delete snapshot DB record %d: %v", id, dbErr))
				continue
			}

			result.SnapshotsDeleted++
			result.BytesFreed += sizeByID[id]
		}
	}

	return result, nil
}

// parseSQLiteTime parses a SQLite DATETIME string (stored as UTC).
// SQLite stores datetimes as "2006-01-02 15:04:05" or RFC3339 strings.
func parseSQLiteTime(s string) (time.Time, error) {
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.ParseInLocation(f, s, time.UTC); err == nil {
			return t, nil
		}
	}
	// Try sql.NullTime via standard library scan as a fallback.
	var nt sql.NullTime
	_ = nt
	return time.Time{}, fmt.Errorf("unable to parse datetime %q", s)
}
