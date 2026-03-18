package setup

import (
	"testing"

	"github.com/backupmanager/backupmanager/internal/database"
)

func TestFirstRunCreatesAdminUser(t *testing.T) {
	db, _ := database.Open(t.TempDir() + "/test.db")
	defer db.Close()
	db.Migrate()

	err := EnsureAdminUser(db, "admin", "admin@test.com", "initialPassword1!")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}

	var count int
	db.DB().QueryRow("SELECT COUNT(*) FROM users WHERE username = 'admin'").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 admin user, got %d", count)
	}
}

func TestFirstRunSkipsIfAdminExists(t *testing.T) {
	db, _ := database.Open(t.TempDir() + "/test.db")
	defer db.Close()
	db.Migrate()

	EnsureAdminUser(db, "admin", "admin@test.com", "pass1")
	EnsureAdminUser(db, "admin", "admin@test.com", "pass2") // Should not error

	var count int
	db.DB().QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 user, got %d", count)
	}
}
