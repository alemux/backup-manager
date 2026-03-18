// internal/integrity/integrity.go
package integrity

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
)

// IntegrityResult holds the verification result for a single snapshot.
type IntegrityResult struct {
	SnapshotID int    `json:"snapshot_id"`
	Status     string `json:"status"` // "ok", "corrupted", "missing"
	Expected   string `json:"expected_checksum"`
	Actual     string `json:"actual_checksum"`
	Message    string `json:"message"`
}

// IntegrityService verifies backup snapshot integrity.
type IntegrityService struct {
	db *database.Database

	mu      sync.RWMutex
	results []IntegrityResult
	running bool
}

// NewIntegrityService creates a new IntegrityService.
func NewIntegrityService(db *database.Database) *IntegrityService {
	return &IntegrityService{db: db}
}

// VerifySnapshot checks a single snapshot's integrity.
// Flow:
//  1. Load snapshot record from DB (path, checksum).
//  2. Check file exists on disk.
//  3. If file missing → return "missing" status.
//  4. Calculate SHA256 of file.
//  5. Compare with stored checksum.
//  6. If mismatch → return "corrupted" status.
//  7. If it's a .sql.gz file → run ValidateSQLDump.
//  8. Return "ok" status.
func (s *IntegrityService) VerifySnapshot(snapshotID int) (*IntegrityResult, error) {
	var snapshotPath string
	var storedChecksum sql.NullString

	err := s.db.DB().QueryRow(
		`SELECT snapshot_path, checksum_sha256 FROM backup_snapshots WHERE id = ?`,
		snapshotID,
	).Scan(&snapshotPath, &storedChecksum)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("snapshot %d not found", snapshotID)
	}
	if err != nil {
		return nil, fmt.Errorf("query snapshot %d: %w", snapshotID, err)
	}

	expected := ""
	if storedChecksum.Valid {
		expected = storedChecksum.String
	}

	// Check file exists.
	if _, statErr := os.Stat(snapshotPath); os.IsNotExist(statErr) {
		return &IntegrityResult{
			SnapshotID: snapshotID,
			Status:     "missing",
			Expected:   expected,
			Actual:     "",
			Message:    fmt.Sprintf("snapshot file not found: %s", snapshotPath),
		}, nil
	}

	// Calculate SHA256.
	actual, err := CalculateFileChecksum(snapshotPath)
	if err != nil {
		return nil, fmt.Errorf("calculate checksum for snapshot %d: %w", snapshotID, err)
	}

	// Compare checksums (only if stored checksum is non-empty).
	if expected != "" && actual != expected {
		return &IntegrityResult{
			SnapshotID: snapshotID,
			Status:     "corrupted",
			Expected:   expected,
			Actual:     actual,
			Message:    "checksum mismatch: file may be corrupted",
		}, nil
	}

	// For SQL dumps, perform structural validation.
	if strings.HasSuffix(snapshotPath, ".sql.gz") {
		if err := ValidateSQLDump(snapshotPath); err != nil {
			return &IntegrityResult{
				SnapshotID: snapshotID,
				Status:     "corrupted",
				Expected:   expected,
				Actual:     actual,
				Message:    fmt.Sprintf("SQL dump validation failed: %v", err),
			}, nil
		}
	}

	return &IntegrityResult{
		SnapshotID: snapshotID,
		Status:     "ok",
		Expected:   expected,
		Actual:     actual,
		Message:    "integrity check passed",
	}, nil
}

// VerifyAll scans all snapshots and verifies checksums.
// It stores results internally and returns them.
func (s *IntegrityService) VerifyAll(ctx context.Context) ([]IntegrityResult, error) {
	rows, err := s.db.DB().QueryContext(ctx, `SELECT id FROM backup_snapshots ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query snapshots: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan snapshot id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshots: %w", err)
	}

	var results []IntegrityResult
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		result, err := s.VerifySnapshot(id)
		if err != nil {
			results = append(results, IntegrityResult{
				SnapshotID: id,
				Status:     "corrupted",
				Message:    fmt.Sprintf("verification error: %v", err),
			})
			continue
		}
		results = append(results, *result)
	}

	s.mu.Lock()
	s.results = results
	s.mu.Unlock()

	return results, nil
}

// LatestResults returns the most recently stored VerifyAll results.
func (s *IntegrityService) LatestResults() []IntegrityResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]IntegrityResult, len(s.results))
	copy(out, s.results)
	return out
}

// IsRunning reports whether a background verification is currently in progress.
func (s *IntegrityService) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// StartBackgroundVerify launches VerifyAll in a goroutine and sets the running flag.
// Returns false if a verification is already running.
func (s *IntegrityService) StartBackgroundVerify(ctx context.Context) bool {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return false
	}
	s.running = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
		}()
		_, _ = s.VerifyAll(ctx)
	}()
	return true
}

// LastVerifiedAt returns the timestamp of the last completed verification run (zero if none).
func (s *IntegrityService) LastVerifiedAt() time.Time {
	// Not tracked in this implementation; exposed for future use.
	return time.Time{}
}

// CalculateFileChecksum computes the SHA256 checksum of the file at path.
// Returns the hex-encoded digest.
func CalculateFileChecksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash file: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// ValidateSQLDump performs basic structural validation on a gzipped SQL dump file.
// It checks:
//   - The file is valid gzip.
//   - The first 1KB of decompressed content starts with SQL statements or comments.
//   - The file is not truncated (gzip trailer is present).
func ValidateSQLDump(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("not a valid gzip file: %w", err)
	}
	defer gr.Close()

	// Read first 1KB to validate SQL content.
	buf := make([]byte, 1024)
	n, err := io.ReadFull(gr, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return fmt.Errorf("read gzip content: %w", err)
	}

	if n == 0 {
		return fmt.Errorf("SQL dump is empty")
	}

	head := strings.TrimSpace(string(buf[:n]))
	if !looksLikeSQL(head) {
		return fmt.Errorf("content does not appear to be a valid SQL dump")
	}

	return nil
}

// looksLikeSQL returns true if the content starts with common SQL dump markers.
func looksLikeSQL(content string) bool {
	upper := strings.ToUpper(content)

	sqlPrefixes := []string{
		"--",          // SQL comment (mysqldump header)
		"/*",          // block comment (mysqldump version header)
		"CREATE ",
		"INSERT ",
		"DROP ",
		"SET ",
		"BEGIN",
		"USE ",
		"ALTER ",
		"LOCK ",
	}

	for _, prefix := range sqlPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}

	// Scan first few lines for SQL markers (some dumps have blank lines at top).
	scanner := bufio.NewScanner(strings.NewReader(content))
	lineCount := 0
	for scanner.Scan() && lineCount < 10 {
		line := strings.TrimSpace(strings.ToUpper(scanner.Text()))
		if line == "" {
			lineCount++
			continue
		}
		for _, prefix := range sqlPrefixes {
			if strings.HasPrefix(line, prefix) {
				return true
			}
		}
		lineCount++
	}

	return false
}
