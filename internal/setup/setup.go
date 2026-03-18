package setup

import (
	"fmt"
	"os"

	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/backupmanager/backupmanager/internal/database"
)

func EnsureAdminUser(db *database.Database, username, email, password string) error {
	var count int
	if err := db.DB().QueryRow("SELECT COUNT(*) FROM users WHERE is_admin = 1").Scan(&count); err != nil {
		return fmt.Errorf("check admin: %w", err)
	}
	if count > 0 {
		return nil
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	_, err = db.DB().Exec(
		"INSERT INTO users (username, email, password_hash, is_admin) VALUES (?, ?, ?, 1)",
		username, email, hash,
	)
	if err != nil {
		return fmt.Errorf("insert admin: %w", err)
	}
	return nil
}

func EnsureDataDirs(dataDir string) error {
	dirs := []string{
		dataDir,
		dataDir + "/backups",
		dataDir + "/logs",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	return nil
}
