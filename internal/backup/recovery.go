package backup

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
)

// RecoveryResult holds the outcome of a crash recovery sweep.
type RecoveryResult struct {
	RunsRecovered  int
	SyncsRecovered int
	FilesCleanedUp int
	Errors         []string
}

// RecoverFromCrash detects and cleans up state from a previous crash.
// Should be called on application startup, after database migration.
//
// It does three things:
//  1. Marks backup_runs with status='running' as 'failed'.
//  2. Resets destination_sync_status rows with status='in_progress' back to 'pending'.
//  3. Removes partial files on disk whose snapshot_path exists but has no
//     corresponding completed backup_snapshots record, provided the file is
//     inside backupDir.
func RecoverFromCrash(db *database.Database, backupDir string) *RecoveryResult {
	result := &RecoveryResult{}

	// -------------------------------------------------------------------------
	// Step 1: Mark stale backup runs as failed.
	// -------------------------------------------------------------------------
	staleRunIDs, snapshotPaths, err := fetchStaleRunInfo(db)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("query stale runs: %v", err))
	} else {
		for _, runID := range staleRunIDs {
			if err := markRunFailed(db, runID); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("mark run %d failed: %v", runID, err))
			} else {
				log.Printf("RecoverFromCrash: marked backup_run %d as failed (interrupted by application restart)", runID)
				result.RunsRecovered++
			}
		}

		// -------------------------------------------------------------------------
		// Step 3: Clean up partial files for each recovered run.
		// -------------------------------------------------------------------------
		absBackupDir, err := filepath.Abs(backupDir)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("resolve backup dir: %v", err))
		} else {
			for runID, paths := range snapshotPaths {
				for _, snapshotPath := range paths {
					cleaned, cleanErr := maybeRemovePartialFile(db, runID, snapshotPath, absBackupDir)
					if cleanErr != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("cleanup partial file %q (run %d): %v", snapshotPath, runID, cleanErr))
					} else if cleaned {
						result.FilesCleanedUp++
					}
				}
			}
		}
	}

	// -------------------------------------------------------------------------
	// Step 2: Reset stale destination syncs back to pending.
	// -------------------------------------------------------------------------
	syncsReset, err := resetStaleSyncs(db)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("reset stale syncs: %v", err))
	} else {
		if syncsReset > 0 {
			log.Printf("RecoverFromCrash: reset %d in_progress destination syncs to pending", syncsReset)
		}
		result.SyncsRecovered = syncsReset
	}

	return result
}

// fetchStaleRunInfo returns:
//   - a list of run IDs with status='running'
//   - a map from run ID to snapshot paths found for that run (may be empty)
func fetchStaleRunInfo(db *database.Database) ([]int, map[int][]string, error) {
	rows, err := db.DB().Query(
		`SELECT id FROM backup_runs WHERE status = 'running'`,
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var runIDs []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, nil, err
		}
		runIDs = append(runIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	if len(runIDs) == 0 {
		return nil, map[int][]string{}, nil
	}

	// Collect snapshot paths for these runs.
	snapshotPaths := make(map[int][]string, len(runIDs))
	for _, runID := range runIDs {
		pathRows, err := db.DB().Query(
			`SELECT snapshot_path FROM backup_snapshots WHERE run_id = ?`, runID,
		)
		if err != nil {
			return runIDs, snapshotPaths, fmt.Errorf("query snapshots for run %d: %w", runID, err)
		}
		for pathRows.Next() {
			var p string
			if err := pathRows.Scan(&p); err != nil {
				pathRows.Close()
				return runIDs, snapshotPaths, err
			}
			snapshotPaths[runID] = append(snapshotPaths[runID], p)
		}
		pathRows.Close()
		if err := pathRows.Err(); err != nil {
			return runIDs, snapshotPaths, err
		}
	}

	return runIDs, snapshotPaths, nil
}

// markRunFailed updates a single backup_run to failed status.
func markRunFailed(db *database.Database, runID int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.DB().Exec(
		`UPDATE backup_runs
		 SET status = 'failed',
		     error_message = 'interrupted by application restart',
		     finished_at = ?
		 WHERE id = ?`,
		now, runID,
	)
	return err
}

// resetStaleSyncs resets all in_progress destination_sync_status rows to pending.
// Returns the number of rows updated.
func resetStaleSyncs(db *database.Database) (int, error) {
	res, err := db.DB().Exec(
		`UPDATE destination_sync_status SET status = 'pending' WHERE status = 'in_progress'`,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// maybeRemovePartialFile checks whether snapshotPath is:
//  1. Inside absBackupDir (safety guard — never delete outside backup dir).
//  2. Present on disk.
//  3. Not associated with a completed snapshot in backup_snapshots for the given run.
//
// If all three conditions hold it removes the file and returns (true, nil).
func maybeRemovePartialFile(db *database.Database, runID int, snapshotPath, absBackupDir string) (bool, error) {
	// Safety: resolve to absolute path and ensure it is inside absBackupDir.
	absPath, err := filepath.Abs(snapshotPath)
	if err != nil {
		return false, fmt.Errorf("resolve path: %w", err)
	}
	if !strings.HasPrefix(absPath, absBackupDir+string(filepath.Separator)) && absPath != absBackupDir {
		// Path is outside the backup directory — skip it.
		return false, nil
	}

	// Check file exists on disk.
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("stat file: %w", err)
	}

	// Check whether there is a *completed* snapshot record for this run and path.
	// A "completed" snapshot is one whose parent run ended in 'success'.
	// If the run was stale (running), it's a partial file.
	var count int
	err = db.DB().QueryRow(
		`SELECT COUNT(*) FROM backup_snapshots bs
		 INNER JOIN backup_runs br ON br.id = bs.run_id
		 WHERE bs.snapshot_path = ?
		   AND br.status = 'success'`,
		snapshotPath,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("query snapshot record: %w", err)
	}
	if count > 0 {
		// A completed snapshot references this path — do not delete it.
		return false, nil
	}

	// Safe to remove the partial file.
	if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("remove partial file: %w", err)
	}
	log.Printf("RecoverFromCrash: removed partial file %q (run %d)", absPath, runID)
	return true, nil
}
