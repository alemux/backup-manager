// internal/database/migrations.go
package database

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func (d *Database) Migrate() error {
	if err := d.runMigration("migrations/001_initial_schema.sql", 1); err != nil {
		return err
	}
	if err := d.runMigration("migrations/002_add_exclude_patterns.sql", 2); err != nil {
		return err
	}
	return nil
}

func (d *Database) runMigration(filename string, version int) error {
	sqlBytes, err := migrationsFS.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", filename, err)
	}

	// Check if schema_migrations table exists and migration was already applied.
	// Errors here are non-fatal: if queries fail, we fall through to re-run the
	// migration which is safe (all statements use IF NOT EXISTS or are idempotent).
	var tableExists int
	if err := d.db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'",
	).Scan(&tableExists); err == nil && tableExists > 0 {
		var count int
		if err := d.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&count); err == nil && count > 0 {
			return nil // Already applied
		}
	}

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Execute migration statements
	statements := strings.Split(string(sqlBytes), ";")
	for _, stmt := range statements {
		// Strip leading comment lines before trimming/checking
		lines := strings.Split(stmt, "\n")
		var nonCommentLines []string
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "--") {
				nonCommentLines = append(nonCommentLines, line)
			}
		}
		stmt = strings.TrimSpace(strings.Join(nonCommentLines, "\n"))
		if stmt == "" {
			continue
		}
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("execute migration statement (v%d): %w\nStatement: %s", version, err, stmt)
		}
	}

	// Record migration
	if _, err := tx.Exec("INSERT OR IGNORE INTO schema_migrations (version) VALUES (?)", version); err != nil {
		return fmt.Errorf("record migration v%d: %w", version, err)
	}

	return tx.Commit()
}
