// internal/database/database_test.go
package database

import (
	"os"
	"testing"
)

func TestOpenCreatesFile(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file was not created")
	}
}

func TestOpenRunsIntegrityCheck(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	var result string
	err = db.DB().QueryRow("PRAGMA integrity_check").Scan(&result)
	if err != nil {
		t.Fatalf("integrity check failed: %v", err)
	}
	if result != "ok" {
		t.Errorf("expected integrity_check=ok, got %s", result)
	}
}
