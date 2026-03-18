// internal/database/database.go
package database

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
	db *sql.DB
}

func Open(path string) (*Database, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	var result string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		db.Close()
		return nil, fmt.Errorf("integrity check failed: %w", err)
	}
	if result != "ok" {
		db.Close()
		return nil, fmt.Errorf("database integrity check failed: %s", result)
	}

	return &Database{db: db}, nil
}

func (d *Database) DB() *sql.DB {
	return d.db
}

func (d *Database) Close() error {
	return d.db.Close()
}
