package backup

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
)

// ---------------------------------------------------------------------------
// Helpers local to preflight tests
// ---------------------------------------------------------------------------

// insertServerWithHostPort inserts a server with a specific host/port and
// returns its ID. Used to control which host:port the preflight check connects to.
func insertServerWithHostPort(t *testing.T, db *database.Database, host string, port int) int {
	t.Helper()
	res, err := db.DB().Exec(
		`INSERT INTO servers (name, type, host, port, connection_type, username)
		 VALUES (?, 'linux', ?, ?, 'ssh', 'backup')`,
		fmt.Sprintf("srv-%s-%d", host, port), host, port,
	)
	if err != nil {
		t.Fatalf("insert server with host:port: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

// startTCPListener opens a listener on 127.0.0.1 with an ephemeral port and
// returns it plus the assigned port number.
func startTCPListener(t *testing.T) (net.Listener, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start listener: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	port := ln.Addr().(*net.TCPAddr).Port
	return ln, port
}

// ---------------------------------------------------------------------------
// Disk space checks
// ---------------------------------------------------------------------------

func TestPreflight_DiskSpaceOK(t *testing.T) {
	db := setupTestDB(t)
	serverID := insertTestServer(t, db, "disk-ok-srv", "linux")
	srcID := insertTestSource(t, db, serverID, "web", "web", "/var/www", nil, 0)
	jobID := insertTestJob(t, db, "disk-ok-job", serverID, []int{srcID})

	// Insert completed runs with small sizes — 1 KiB average × 1.5 << real available disk.
	for i := 0; i < 3; i++ {
		_, err := db.DB().Exec(
			`INSERT INTO backup_runs (job_id, status, started_at, finished_at, total_size_bytes)
			 VALUES (?, 'success', datetime('now', '-1 day'), datetime('now', '-1 day', '+1 minute'), 1024)`,
			jobID,
		)
		if err != nil {
			t.Fatalf("insert run: %v", err)
		}
	}

	backupDir := t.TempDir()
	result := RunPreflight(context.Background(), db, jobID, backupDir)

	diskCheck := findCheck(result, "disk_space")
	if diskCheck == nil {
		t.Fatal("disk_space check not found in results")
	}
	if !diskCheck.Passed {
		t.Errorf("expected disk_space check to pass (small estimated size), got: %s", diskCheck.Message)
	}
}

func TestPreflight_DiskSpaceLow(t *testing.T) {
	db := setupTestDB(t)
	serverID := insertTestServer(t, db, "disk-low-srv", "linux")
	srcID := insertTestSource(t, db, serverID, "web", "web", "/var/www", nil, 0)
	jobID := insertTestJob(t, db, "disk-low-job", serverID, []int{srcID})

	// Insert completed runs with enormous sizes — 1 PiB average × 1.5 >> available disk.
	const enormousSize = int64(1) << 50 // 1 PiB
	for i := 0; i < 3; i++ {
		_, err := db.DB().Exec(
			`INSERT INTO backup_runs (job_id, status, started_at, finished_at, total_size_bytes)
			 VALUES (?, 'success', datetime('now', '-1 day'), datetime('now', '-1 day', '+1 minute'), ?)`,
			jobID, enormousSize,
		)
		if err != nil {
			t.Fatalf("insert run: %v", err)
		}
	}

	backupDir := t.TempDir()
	result := RunPreflight(context.Background(), db, jobID, backupDir)

	diskCheck := findCheck(result, "disk_space")
	if diskCheck == nil {
		t.Fatal("disk_space check not found in results")
	}
	if diskCheck.Passed {
		t.Errorf("expected disk_space check to fail (enormous estimated size), message: %s", diskCheck.Message)
	}
}

// ---------------------------------------------------------------------------
// Server reachability checks
// ---------------------------------------------------------------------------

func TestPreflight_ServerReachable(t *testing.T) {
	db := setupTestDB(t)

	ln, port := startTCPListener(t)
	_ = ln // kept open so TCP connect succeeds

	serverID := insertServerWithHostPort(t, db, "127.0.0.1", port)
	srcID := insertTestSource(t, db, serverID, "web", "web", "/var/www", nil, 0)
	jobID := insertTestJob(t, db, "reachable-job", serverID, []int{srcID})

	backupDir := t.TempDir()
	result := RunPreflight(context.Background(), db, jobID, backupDir)

	check := findCheck(result, "server_reachable")
	if check == nil {
		t.Fatal("server_reachable check not found")
	}
	if !check.Passed {
		t.Errorf("expected server_reachable to pass (listener open), got: %s", check.Message)
	}
}

func TestPreflight_ServerUnreachable(t *testing.T) {
	db := setupTestDB(t)

	// Find a free port then close the listener immediately so nothing is listening.
	ln, port := startTCPListener(t)
	ln.Close()

	serverID := insertServerWithHostPort(t, db, "127.0.0.1", port)
	srcID := insertTestSource(t, db, serverID, "web", "web", "/var/www", nil, 0)
	jobID := insertTestJob(t, db, "unreachable-job", serverID, []int{srcID})

	backupDir := t.TempDir()
	result := RunPreflight(context.Background(), db, jobID, backupDir)

	check := findCheck(result, "server_reachable")
	if check == nil {
		t.Fatal("server_reachable check not found")
	}
	if check.Passed {
		t.Errorf("expected server_reachable to fail (nothing listening), message: %s", check.Message)
	}
}

// ---------------------------------------------------------------------------
// Duplicate run checks
// ---------------------------------------------------------------------------

func TestPreflight_NoDuplicateRun(t *testing.T) {
	db := setupTestDB(t)

	ln, port := startTCPListener(t)
	_ = ln

	serverID := insertServerWithHostPort(t, db, "127.0.0.1", port)
	srcID := insertTestSource(t, db, serverID, "web", "web", "/var/www", nil, 0)
	jobID := insertTestJob(t, db, "no-dup-job", serverID, []int{srcID})

	backupDir := t.TempDir()
	result := RunPreflight(context.Background(), db, jobID, backupDir)

	check := findCheck(result, "no_duplicate_run")
	if check == nil {
		t.Fatal("no_duplicate_run check not found")
	}
	if !check.Passed {
		t.Errorf("expected no_duplicate_run to pass (no running jobs), got: %s", check.Message)
	}
}

func TestPreflight_DuplicateRunBlocked(t *testing.T) {
	db := setupTestDB(t)

	ln, port := startTCPListener(t)
	_ = ln

	serverID := insertServerWithHostPort(t, db, "127.0.0.1", port)
	srcID := insertTestSource(t, db, serverID, "web", "web", "/var/www", nil, 0)
	jobID := insertTestJob(t, db, "dup-job", serverID, []int{srcID})

	// Insert a currently running backup_run.
	_, err := db.DB().Exec(
		`INSERT INTO backup_runs (job_id, status, started_at) VALUES (?, 'running', datetime('now'))`,
		jobID,
	)
	if err != nil {
		t.Fatalf("insert running run: %v", err)
	}

	backupDir := t.TempDir()
	result := RunPreflight(context.Background(), db, jobID, backupDir)

	check := findCheck(result, "no_duplicate_run")
	if check == nil {
		t.Fatal("no_duplicate_run check not found")
	}
	if check.Passed {
		t.Errorf("expected no_duplicate_run to fail (running job exists), message: %s", check.Message)
	}
	if result.Passed {
		t.Error("expected overall PreflightResult.Passed to be false when a check fails")
	}
}

// ---------------------------------------------------------------------------
// SQLite self-backup tests
// ---------------------------------------------------------------------------

func TestBackupSQLiteDB(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "backupmanager.db")

	content := []byte("fake sqlite data")
	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatalf("write src db: %v", err)
	}

	backupDir := t.TempDir()

	if err := BackupSQLiteDB(srcPath, backupDir); err != nil {
		t.Fatalf("BackupSQLiteDB: %v", err)
	}

	today := time.Now().UTC().Format("2006-01-02")
	expected := filepath.Join(backupDir, fmt.Sprintf("backupmanager_%s.db", today))

	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Errorf("expected backup file %s to exist", expected)
	}

	got, err := os.ReadFile(expected)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("backup content = %q, want %q", string(got), string(content))
	}
}

func TestBackupSQLiteDB_KeepsLast7(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "backupmanager.db")
	if err := os.WriteFile(srcPath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write src db: %v", err)
	}

	backupDir := t.TempDir()

	// Pre-populate with 9 old backup files (days 10 through 2 ago).
	for i := 10; i >= 2; i-- {
		date := time.Now().UTC().AddDate(0, 0, -i).Format("2006-01-02")
		name := filepath.Join(backupDir, fmt.Sprintf("backupmanager_%s.db", date))
		if err := os.WriteFile(name, []byte("old"), 0o644); err != nil {
			t.Fatalf("write old backup: %v", err)
		}
	}

	// BackupSQLiteDB adds today's copy (total = 10 files), then prunes to 7.
	if err := BackupSQLiteDB(srcPath, backupDir); err != nil {
		t.Fatalf("BackupSQLiteDB: %v", err)
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var count int
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}

	if count > 7 {
		t.Errorf("expected at most 7 backup files after pruning, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// findCheck is a small helper to locate a named check in a PreflightResult.
// ---------------------------------------------------------------------------

func findCheck(result *PreflightResult, name string) *Check {
	for _, c := range result.Checks {
		c := c
		if c.Name == name {
			return &c
		}
	}
	return nil
}
