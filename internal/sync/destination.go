// internal/sync/destination.go
package sync

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
)

// DestinationSyncer copies backup snapshots to secondary destinations.
type DestinationSyncer struct {
	db *database.Database
}

// NewDestinationSyncer constructs a DestinationSyncer.
func NewDestinationSyncer(db *database.Database) *DestinationSyncer {
	return &DestinationSyncer{db: db}
}

// SyncStatusEntry holds the sync status for one destination of a snapshot.
type SyncStatusEntry struct {
	ID            int     `json:"id"`
	SnapshotID    int     `json:"snapshot_id"`
	DestinationID int     `json:"destination_id"`
	DestName      string  `json:"destination_name"`
	Status        string  `json:"status"` // pending, in_progress, success, failed
	RetryCount    int     `json:"retry_count"`
	LastError     string  `json:"last_error,omitempty"`
	SyncedAt      *string `json:"synced_at,omitempty"`
}

// QueueSync creates pending destination_sync_status rows for every enabled
// non-primary destination so the snapshot will be copied there.
func (ds *DestinationSyncer) QueueSync(snapshotID int) error {
	rows, err := ds.db.DB().Query(
		`SELECT id FROM destinations WHERE enabled = 1 AND is_primary = 0`,
	)
	if err != nil {
		return fmt.Errorf("query destinations: %w", err)
	}

	// Collect all IDs before closing rows so no connection is held open
	// while we issue INSERT statements (avoids a two-connection race on
	// shared in-memory SQLite databases used in tests).
	var destIDs []int
	for rows.Next() {
		var destID int
		if err := rows.Scan(&destID); err != nil {
			rows.Close()
			return fmt.Errorf("scan destination id: %w", err)
		}
		destIDs = append(destIDs, destID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate destinations: %w", err)
	}
	rows.Close()

	for _, destID := range destIDs {
		_, err := ds.db.DB().Exec(
			`INSERT OR IGNORE INTO destination_sync_status
			  (snapshot_id, destination_id, status, retry_count)
			 VALUES (?, ?, 'pending', 0)`,
			snapshotID, destID,
		)
		if err != nil {
			return fmt.Errorf("insert sync status for destination %d: %w", destID, err)
		}
	}
	return nil
}

// ProcessQueue processes all pending and retryable failed sync entries.
func (ds *DestinationSyncer) ProcessQueue(ctx context.Context) error {
	rows, err := ds.db.DB().QueryContext(ctx,
		`SELECT dss.id, dss.snapshot_id, dss.destination_id, dss.retry_count,
		        bs.snapshot_path, bs.checksum_sha256, d.path
		 FROM destination_sync_status dss
		 INNER JOIN backup_snapshots bs ON bs.id = dss.snapshot_id
		 INNER JOIN destinations d ON d.id = dss.destination_id
		 WHERE dss.status IN ('pending', 'failed') AND dss.retry_count < 5`,
	)
	if err != nil {
		return fmt.Errorf("query pending syncs: %w", err)
	}
	defer rows.Close()

	type job struct {
		id           int
		snapshotID   int
		destID       int
		retryCount   int
		snapshotPath string
		checksum     sql.NullString
		destBasePath string
	}

	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(
			&j.id, &j.snapshotID, &j.destID, &j.retryCount,
			&j.snapshotPath, &j.checksum, &j.destBasePath,
		); err != nil {
			return fmt.Errorf("scan sync job: %w", err)
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating sync jobs: %w", err)
	}

	for _, j := range jobs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Mark in_progress
		if _, err := ds.db.DB().ExecContext(ctx,
			`UPDATE destination_sync_status SET status = 'in_progress' WHERE id = ?`, j.id,
		); err != nil {
			continue
		}

		syncErr := ds.copyAndVerify(j.snapshotPath, j.destBasePath, j.checksum)

		if syncErr == nil {
			now := time.Now().UTC().Format(time.RFC3339)
			ds.db.DB().ExecContext(ctx,
				`UPDATE destination_sync_status
				 SET status = 'success', synced_at = ?, last_error = NULL
				 WHERE id = ?`,
				now, j.id,
			)
		} else {
			newRetry := j.retryCount + 1
			newStatus := "pending"
			if newRetry >= 5 {
				newStatus = "failed"
			}
			ds.db.DB().ExecContext(ctx,
				`UPDATE destination_sync_status
				 SET status = ?, retry_count = ?, last_error = ?
				 WHERE id = ?`,
				newStatus, newRetry, syncErr.Error(), j.id,
			)
		}
	}
	return nil
}

// RetryFailed resets a specific failed sync back to pending so it will be
// picked up by the next ProcessQueue call.
func (ds *DestinationSyncer) RetryFailed(syncStatusID int) error {
	res, err := ds.db.DB().Exec(
		`UPDATE destination_sync_status
		 SET status = 'pending', retry_count = 0, last_error = NULL
		 WHERE id = ? AND status = 'failed'`,
		syncStatusID,
	)
	if err != nil {
		return fmt.Errorf("reset sync status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("sync status %d not found or not failed", syncStatusID)
	}
	return nil
}

// GetSyncStatus returns sync status for all destinations of a snapshot.
func (ds *DestinationSyncer) GetSyncStatus(snapshotID int) ([]SyncStatusEntry, error) {
	rows, err := ds.db.DB().Query(
		`SELECT dss.id, dss.snapshot_id, dss.destination_id, d.name,
		        dss.status, dss.retry_count, dss.last_error, dss.synced_at
		 FROM destination_sync_status dss
		 INNER JOIN destinations d ON d.id = dss.destination_id
		 WHERE dss.snapshot_id = ?
		 ORDER BY dss.id ASC`,
		snapshotID,
	)
	if err != nil {
		return nil, fmt.Errorf("query sync status: %w", err)
	}
	defer rows.Close()

	var entries []SyncStatusEntry
	for rows.Next() {
		var e SyncStatusEntry
		var lastErr sql.NullString
		var syncedAt sql.NullString
		if err := rows.Scan(
			&e.ID, &e.SnapshotID, &e.DestinationID, &e.DestName,
			&e.Status, &e.RetryCount, &lastErr, &syncedAt,
		); err != nil {
			return nil, fmt.Errorf("scan sync status: %w", err)
		}
		if lastErr.Valid {
			e.LastError = lastErr.String
		}
		if syncedAt.Valid {
			s := syncedAt.String
			e.SyncedAt = &s
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// RecoverStale resets any in_progress syncs to pending (called on startup to
// recover from a crash or restart mid-sync).
func (ds *DestinationSyncer) RecoverStale() error {
	_, err := ds.db.DB().Exec(
		`UPDATE destination_sync_status SET status = 'pending' WHERE status = 'in_progress'`,
	)
	return err
}

// loadPrimaryPath returns the path of the primary destination from the DB.
func (ds *DestinationSyncer) loadPrimaryPath() string {
	var path string
	ds.db.DB().QueryRow("SELECT path FROM destinations WHERE is_primary=1 AND enabled=1 LIMIT 1").Scan(&path)
	return path
}

// copyAndVerify copies snapshotPath into destBasePath. The snapshot is
// typically a directory tree, so we use rsync for efficient local copy
// (preserves hard links, permissions, etc.). Falls back to cp -a if
// rsync is not available.
func (ds *DestinationSyncer) copyAndVerify(snapshotPath, destBasePath string, checksum sql.NullString) error {
	// Check if source exists
	info, err := os.Stat(snapshotPath)
	if err != nil {
		return fmt.Errorf("source not found: %w", err)
	}

	if info.IsDir() {
		// Calculate relative path from the primary destination.
		// snapshotPath:  /Primary/ServerName/type/source/timestamp
		// destBasePath:  /Secondary
		// We need:       /Secondary/ServerName/type/source/timestamp
		// Strategy: find the primary base path and compute relative.
		primaryBase := ds.loadPrimaryPath()
		relPath := snapshotPath
		if primaryBase != "" {
			if r, err := filepath.Rel(primaryBase, snapshotPath); err == nil {
				relPath = r
			}
		}
		destPath := filepath.Join(destBasePath, relPath)

		if err := os.MkdirAll(destPath, 0o755); err != nil {
			return fmt.Errorf("create destination directory: %w", err)
		}

		cmd := exec.CommandContext(context.Background(),
			"rsync", "-a", "--delete", snapshotPath+"/", destPath+"/")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("rsync local copy failed: %v\nOutput: %s", err, string(output))
		}
		return nil
	}

	// Single file: copy with checksum verification
	rel := filepath.Base(snapshotPath)
	destPath := filepath.Join(destBasePath, rel)

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	src, err := os.Open(snapshotPath)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer dst.Close()

	h := sha256.New()
	w := io.MultiWriter(dst, h)
	if _, err := io.Copy(w, src); err != nil {
		return fmt.Errorf("copy file: %w", err)
	}

	if checksum.Valid && checksum.String != "" {
		got := fmt.Sprintf("%x", h.Sum(nil))
		if got != checksum.String {
			return fmt.Errorf("checksum mismatch: expected %s got %s", checksum.String, got)
		}
	}
	return nil
}
