// internal/database/migrations_test.go
package database

import (
	"testing"
)

func TestMigrateCreatesAllTables(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	expectedTables := []string{
		"users", "servers", "backup_sources", "backup_jobs",
		"backup_job_sources", "backup_runs", "backup_snapshots",
		"destinations", "destination_sync_status", "health_checks",
		"health_check_config", "audit_log", "notifications_config",
		"notifications_log", "recovery_playbooks", "discovery_results",
		"llm_conversations", "settings", "schema_migrations",
	}

	for _, table := range expectedTables {
		var name string
		err := db.DB().QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s not found: %v", table, err)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("first migration failed: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("second migration failed (not idempotent): %v", err)
	}
}
