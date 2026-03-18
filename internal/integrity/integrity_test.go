// internal/integrity/integrity_test.go
package integrity

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/backupmanager/backupmanager/internal/database"
)

// --- helpers ---

func openTestDB(t *testing.T) *database.Database {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open DB: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// insertSnapshot inserts a minimal backup_snapshot row and returns its ID.
// runID must reference an existing backup_runs row.
func insertSnapshot(t *testing.T, db *database.Database, snapshotPath, checksum string) int {
	t.Helper()

	// Insert a bare-minimum server, job, source, run so FK constraints are satisfied.
	_, err := db.DB().Exec(`INSERT INTO servers (name, type, host, port, connection_type, status, created_at, updated_at)
		VALUES ('s','linux','localhost',22,'ssh','unknown','2024-01-01','2024-01-01')`)
	if err != nil {
		t.Fatalf("insert server: %v", err)
	}

	_, err = db.DB().Exec(`INSERT INTO backup_jobs (name, server_id, schedule, retention_daily, retention_weekly, retention_monthly, timeout_minutes, enabled, created_at, updated_at)
		VALUES ('j',1,'@daily',7,4,3,120,1,'2024-01-01','2024-01-01')`)
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}

	_, err = db.DB().Exec(`INSERT INTO backup_sources (server_id, name, type, source_path, created_at)
		VALUES (1,'src','web','/data','2024-01-01')`)
	if err != nil {
		t.Fatalf("insert source: %v", err)
	}

	res, err := db.DB().Exec(`INSERT INTO backup_runs (job_id, status, created_at)
		VALUES (1,'success','2024-01-01')`)
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}
	runID, _ := res.LastInsertId()

	var checksumVal interface{}
	if checksum != "" {
		checksumVal = checksum
	}

	snapRes, err := db.DB().Exec(
		`INSERT INTO backup_snapshots (run_id, source_id, snapshot_path, size_bytes, checksum_sha256, created_at)
		 VALUES (?, 1, ?, 0, ?, '2024-01-01')`,
		runID, snapshotPath, checksumVal,
	)
	if err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	id, _ := snapRes.LastInsertId()
	return int(id)
}

// sha256OfBytes returns the hex SHA256 of data.
func sha256OfBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// writeGzipSQL writes a gzip-compressed SQL dump to path.
func writeGzipSQL(t *testing.T, path, sqlContent string) {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, err := gw.Write([]byte(sqlContent))
	if err != nil {
		t.Fatalf("write gzip: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

// --- CalculateFileChecksum ---

func TestCalculateFileChecksum(t *testing.T) {
	data := []byte("hello, backup world!")
	f, err := os.CreateTemp(t.TempDir(), "chk-*.bin")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()

	got, err := CalculateFileChecksum(f.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := sha256OfBytes(data)
	if got != want {
		t.Errorf("checksum mismatch: got %q, want %q", got, want)
	}
}

func TestCalculateFileChecksum_Missing(t *testing.T) {
	_, err := CalculateFileChecksum("/nonexistent/path/file.bin")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// --- VerifySnapshot ---

func TestVerifySnapshot_OK(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()

	data := []byte("backup content")
	fpath := filepath.Join(dir, "snap.bin")
	if err := os.WriteFile(fpath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	checksum := sha256OfBytes(data)

	snapID := insertSnapshot(t, db, fpath, checksum)
	svc := NewIntegrityService(db)

	result, err := svc.VerifySnapshot(snapID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("expected status 'ok', got %q (message: %s)", result.Status, result.Message)
	}
	if result.Expected != checksum {
		t.Errorf("expected checksum %q, got %q", checksum, result.Expected)
	}
	if result.Actual != checksum {
		t.Errorf("actual checksum %q, want %q", result.Actual, checksum)
	}
}

func TestVerifySnapshot_Corrupted(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()

	data := []byte("backup content")
	fpath := filepath.Join(dir, "snap.bin")
	if err := os.WriteFile(fpath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	// Store a wrong checksum.
	wrongChecksum := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	snapID := insertSnapshot(t, db, fpath, wrongChecksum)
	svc := NewIntegrityService(db)

	result, err := svc.VerifySnapshot(snapID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "corrupted" {
		t.Errorf("expected status 'corrupted', got %q", result.Status)
	}
	if result.Expected != wrongChecksum {
		t.Errorf("expected checksum %q, got %q", wrongChecksum, result.Expected)
	}
}

func TestVerifySnapshot_MissingFile(t *testing.T) {
	db := openTestDB(t)

	snapID := insertSnapshot(t, db, "/nonexistent/path/snap.bin", "")
	svc := NewIntegrityService(db)

	result, err := svc.VerifySnapshot(snapID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "missing" {
		t.Errorf("expected status 'missing', got %q", result.Status)
	}
}

// --- VerifyAll ---

func TestVerifyAll(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()

	data := []byte("all ok")
	fpath := filepath.Join(dir, "snap.bin")
	if err := os.WriteFile(fpath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	snapID := insertSnapshot(t, db, fpath, sha256OfBytes(data))
	svc := NewIntegrityService(db)

	results, err := svc.VerifyAll(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].SnapshotID != snapID {
		t.Errorf("wrong snapshot ID: got %d, want %d", results[0].SnapshotID, snapID)
	}
	if results[0].Status != "ok" {
		t.Errorf("expected status 'ok', got %q", results[0].Status)
	}
}

// --- ValidateSQLDump ---

func TestValidateSQLDump_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dump.sql.gz")

	sqlContent := "-- MySQL dump 10.13\n-- Host: localhost\nCREATE TABLE t (id INT);\nINSERT INTO t VALUES (1);\n"
	writeGzipSQL(t, path, sqlContent)

	if err := ValidateSQLDump(path); err != nil {
		t.Errorf("unexpected error for valid SQL dump: %v", err)
	}
}

func TestValidateSQLDump_InvalidGzip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notgzip.sql.gz")

	// Write raw (non-gzip) bytes.
	if err := os.WriteFile(path, []byte("this is not gzip data"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ValidateSQLDump(path); err == nil {
		t.Fatal("expected error for non-gzip file, got nil")
	}
}

func TestValidateSQLDump_NotSQL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notsql.sql.gz")

	// Gzip-compress arbitrary binary-like content.
	writeGzipSQL(t, path, "BINARY DATA XYZ 12345 random bytes that look nothing like SQL")

	if err := ValidateSQLDump(path); err == nil {
		t.Fatal("expected error for non-SQL gzip content, got nil")
	}
}
