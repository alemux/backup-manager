# BackupManager Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build BackupManager — an internal Go+React web application for managing incremental backups of Linux and Windows servers with a professional UI, health monitoring, notifications, and disaster recovery guidance.

**Architecture:** Go monolith serving a React SPA (embedded in the binary). SQLite for persistence. Three-layer architecture: Web (API + auth + WebSocket), Core (scheduler, orchestrator, retention, integrity, health), Infrastructure (SSH/FTP connectors, rsync, MySQL dump, encryption, notifications, LLM). Single binary deployment.

**Tech Stack:** Go 1.22+, SQLite (via go-sqlite3), React 18 + TypeScript, Tailwind CSS, Shadcn/UI, React Query, Recharts, WebSocket (gorilla/websocket), SSH (golang.org/x/crypto/ssh), bcrypt, JWT (golang-jwt), AES-256-GCM.

**Spec:** `docs/superpowers/specs/2026-03-18-backupmanager-design.md`

---

## Phases Overview

The project is divided into 6 phases. Each phase produces working, testable, committable software. Phases are sequential — each builds on the previous.

| Phase | Name | Tasks | Description |
|-------|------|-------|-------------|
| 1 | Foundation | 1-6 | Go project scaffold, SQLite, auth (login/logout/password reset/JWT refresh), basic API, React shell, build pipeline |
| 2 | Server Management | 7-12 | Server CRUD, SSH/FTP connectors, auto-discovery, connection test API, add-server wizard UI |
| 3 | Backup Engine | 13-20 | Incremental sync (rsync/FTP), MySQL dump orchestration, job runner, scheduler, dependency graph, retention, missed backup detection |
| 4 | Monitoring & Notifications | 21-26 | Health checks, Telegram bot, email SMTP, notification matrix, anti-flood, dashboard |
| 5 | Security & Integrity | 27-31 | AES-256 encryption (incl. re-encryption of existing backups), integrity verification, audit log, CSRF hardening, credential encryption |
| 6 | Advanced Features | 32-40 | Multi-destination sync, snapshots UI with calendar, disaster recovery playbooks, AI assistant, docs page, bandwidth throttling, startup recovery, final polish |

---

## Phase 1: Foundation

### Task 1: Go Project Scaffold + Makefile

**Files:**
- Create: `cmd/server/main.go`
- Create: `go.mod`
- Create: `Makefile`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `.gitignore`

- [ ] **Step 1: Initialize Go module**

```bash
cd /Users/amussini/Lavoro/server_backup_manager
go mod init github.com/backupmanager/backupmanager
```

- [ ] **Step 2: Create .gitignore**

```gitignore
# Binaries
/bin/
/backupmanager
*.exe

# Frontend build
/frontend/node_modules/
/frontend/dist/
/frontend/.next/

# Database
*.db
*.db-journal
*.db-wal
*.db-shm

# IDE
.idea/
.vscode/
*.swp

# OS
.DS_Store
Thumbs.db

# Env
.env
*.key

# Test coverage
coverage.out
coverage.html
```

- [ ] **Step 3: Write config test**

```go
// internal/config/config_test.go
package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg := LoadDefaults()
	if cfg.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Port)
	}
	if cfg.DataDir != "./data" {
		t.Errorf("expected default data dir ./data, got %s", cfg.DataDir)
	}
	if cfg.DBPath != "./data/backupmanager.db" {
		t.Errorf("expected default db path, got %s", cfg.DBPath)
	}
}

func TestLoadFromEnv(t *testing.T) {
	os.Setenv("BM_PORT", "9090")
	os.Setenv("BM_DATA_DIR", "/tmp/bm-test")
	defer os.Unsetenv("BM_PORT")
	defer os.Unsetenv("BM_DATA_DIR")

	cfg := Load()
	if cfg.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Port)
	}
	if cfg.DataDir != "/tmp/bm-test" {
		t.Errorf("expected data dir /tmp/bm-test, got %s", cfg.DataDir)
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL — package/functions not defined

- [ ] **Step 5: Write config implementation**

```go
// internal/config/config.go
package config

import (
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	Port       int
	DataDir    string
	DBPath     string
	BackupDir  string
	LogLevel   string
	JWTSecret  string
	Timezone   string
}

func LoadDefaults() Config {
	dataDir := "./data"
	return Config{
		Port:      8080,
		DataDir:   dataDir,
		DBPath:    filepath.Join(dataDir, "backupmanager.db"),
		BackupDir: filepath.Join(dataDir, "backups"),
		LogLevel:  "info",
		JWTSecret: "",
		Timezone:  "Local",
	}
}

func Load() Config {
	cfg := LoadDefaults()

	if v := os.Getenv("BM_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Port = p
		}
	}
	if v := os.Getenv("BM_DATA_DIR"); v != "" {
		cfg.DataDir = v
		cfg.DBPath = filepath.Join(v, "backupmanager.db")
		cfg.BackupDir = filepath.Join(v, "backups")
	}
	if v := os.Getenv("BM_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("BM_JWT_SECRET"); v != "" {
		cfg.JWTSecret = v
	}
	if v := os.Getenv("BM_TIMEZONE"); v != "" {
		cfg.Timezone = v
	}
	return cfg
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 7: Create main.go entry point**

```go
// cmd/server/main.go
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/backupmanager/backupmanager/internal/config"
)

func main() {
	cfg := config.Load()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("BackupManager starting on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 8: Create Makefile**

```makefile
.PHONY: build run test clean frontend

BINARY=bin/backupmanager
GO=go

build: frontend
	$(GO) build -o $(BINARY) ./cmd/server

run:
	$(GO) run ./cmd/server

test:
	$(GO) test ./... -v -cover

test-short:
	$(GO) test ./... -short -v

clean:
	rm -rf $(BINARY) data/

frontend:
	cd frontend && npm ci && npm run build

dev:
	$(GO) run ./cmd/server
```

- [ ] **Step 9: Verify build compiles**

Run: `go build -o bin/backupmanager ./cmd/server`
Expected: Binary created at `bin/backupmanager`

- [ ] **Step 10: Commit**

```bash
git add -A
git commit -m "feat: project scaffold with config, main entry point, Makefile"
```

---

### Task 2: SQLite Database + Migrations

**Files:**
- Create: `internal/database/database.go`
- Create: `internal/database/database_test.go`
- Create: `internal/database/migrations.go`
- Create: `internal/database/migrations_test.go`
- Create: `internal/database/migrations/001_initial_schema.sql`

- [ ] **Step 1: Install go-sqlite3 dependency**

```bash
go get github.com/mattn/go-sqlite3
```

- [ ] **Step 2: Write database connection test**

```go
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/database/ -v -run TestOpen`
Expected: FAIL

- [ ] **Step 4: Write database.go**

```go
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

	// Run integrity check on startup
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
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/database/ -v -run TestOpen`
Expected: PASS

- [ ] **Step 6: Write migration SQL**

```sql
-- migrations/001_initial_schema.sql
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    is_admin INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS servers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK(type IN ('linux', 'windows')),
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    connection_type TEXT NOT NULL CHECK(connection_type IN ('ssh', 'ftp')),
    username TEXT,
    encrypted_password TEXT,
    ssh_key_path TEXT,
    status TEXT NOT NULL DEFAULT 'unknown' CHECK(status IN ('online', 'offline', 'warning', 'unknown')),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS backup_sources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK(type IN ('web', 'database', 'config')),
    source_path TEXT,
    db_name TEXT,
    depends_on INTEGER REFERENCES backup_sources(id),
    priority INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS backup_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    schedule TEXT NOT NULL,
    retention_daily INTEGER NOT NULL DEFAULT 7,
    retention_weekly INTEGER NOT NULL DEFAULT 4,
    retention_monthly INTEGER NOT NULL DEFAULT 3,
    bandwidth_limit_mbps INTEGER DEFAULT NULL,
    timeout_minutes INTEGER NOT NULL DEFAULT 120,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS backup_job_sources (
    job_id INTEGER NOT NULL REFERENCES backup_jobs(id) ON DELETE CASCADE,
    source_id INTEGER NOT NULL REFERENCES backup_sources(id) ON DELETE CASCADE,
    PRIMARY KEY (job_id, source_id)
);

CREATE TABLE IF NOT EXISTS backup_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id INTEGER NOT NULL REFERENCES backup_jobs(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'running', 'success', 'failed', 'timeout')),
    started_at DATETIME,
    finished_at DATETIME,
    total_size_bytes INTEGER DEFAULT 0,
    files_copied INTEGER DEFAULT 0,
    error_message TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS backup_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id INTEGER NOT NULL REFERENCES backup_runs(id) ON DELETE CASCADE,
    source_id INTEGER NOT NULL REFERENCES backup_sources(id),
    snapshot_path TEXT NOT NULL,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    checksum_sha256 TEXT,
    is_encrypted INTEGER NOT NULL DEFAULT 0,
    retention_expires_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS destinations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK(type IN ('local', 'nas', 'usb', 's3')),
    path TEXT NOT NULL,
    is_primary INTEGER NOT NULL DEFAULT 0,
    retention_daily INTEGER NOT NULL DEFAULT 7,
    retention_weekly INTEGER NOT NULL DEFAULT 4,
    retention_monthly INTEGER NOT NULL DEFAULT 3,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS destination_sync_status (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id INTEGER NOT NULL REFERENCES backup_snapshots(id) ON DELETE CASCADE,
    destination_id INTEGER NOT NULL REFERENCES destinations(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'in_progress', 'success', 'failed')),
    retry_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    synced_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(snapshot_id, destination_id)
);

CREATE TABLE IF NOT EXISTS health_checks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    check_type TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('ok', 'warning', 'critical')),
    message TEXT,
    value TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS health_check_config (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    check_type TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    warning_threshold TEXT,
    critical_threshold TEXT,
    interval_seconds INTEGER NOT NULL DEFAULT 300,
    UNIQUE(server_id, check_type)
);

CREATE TABLE IF NOT EXISTS audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER REFERENCES users(id),
    action TEXT NOT NULL,
    target TEXT,
    ip_address TEXT,
    details TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS notifications_config (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL UNIQUE,
    telegram_enabled INTEGER NOT NULL DEFAULT 0,
    email_enabled INTEGER NOT NULL DEFAULT 0,
    telegram_chat_id TEXT,
    email_recipients TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS notifications_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL,
    channel TEXT NOT NULL CHECK(channel IN ('telegram', 'email')),
    recipient TEXT NOT NULL,
    message TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('sent', 'failed')),
    error_message TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS recovery_playbooks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id INTEGER REFERENCES servers(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    scenario TEXT NOT NULL,
    steps TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS discovery_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    service_name TEXT NOT NULL,
    service_data TEXT NOT NULL,
    discovered_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS llm_conversations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
    content TEXT NOT NULL,
    context_data TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_backup_runs_job_id ON backup_runs(job_id);
CREATE INDEX IF NOT EXISTS idx_backup_runs_status ON backup_runs(status);
CREATE INDEX IF NOT EXISTS idx_backup_snapshots_run_id ON backup_snapshots(run_id);
CREATE INDEX IF NOT EXISTS idx_backup_snapshots_source_id ON backup_snapshots(source_id);
CREATE INDEX IF NOT EXISTS idx_health_checks_server_id ON health_checks(server_id);
CREATE INDEX IF NOT EXISTS idx_health_checks_created_at ON health_checks(created_at);
CREATE INDEX IF NOT EXISTS idx_audit_log_created_at ON audit_log(created_at);
CREATE INDEX IF NOT EXISTS idx_audit_log_user_id ON audit_log(user_id);
CREATE INDEX IF NOT EXISTS idx_notifications_log_created_at ON notifications_log(created_at);
CREATE INDEX IF NOT EXISTS idx_destination_sync_status_status ON destination_sync_status(status);
```

- [ ] **Step 7: Write migrations test**

```go
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
```

- [ ] **Step 8: Run test to verify it fails**

Run: `go test ./internal/database/ -v -run TestMigrate`
Expected: FAIL

- [ ] **Step 9: Write migrations.go**

**Embed strategy:** Place migration SQL files inside `internal/database/migrations/` (not the project-root `migrations/` directory). Go's `embed` directive only works with files relative to the package directory. Move the SQL file from `migrations/001_initial_schema.sql` to `internal/database/migrations/001_initial_schema.sql`. The project-root `migrations/` directory is removed.

```go
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
	sqlBytes, err := migrationsFS.ReadFile("migrations/001_initial_schema.sql")
	if err != nil {
		return fmt.Errorf("read migration: %w", err)
	}

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Check if already applied
	var count int
	_ = tx.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 1").Scan(&count)
	if count > 0 {
		return nil // Already applied
	}

	// Execute migration
	statements := strings.Split(string(sqlBytes), ";")
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" || strings.HasPrefix(stmt, "--") {
			continue
		}
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("execute migration statement: %w\nStatement: %s", err, stmt)
		}
	}

	// Record migration
	if _, err := tx.Exec("INSERT OR IGNORE INTO schema_migrations (version) VALUES (1)"); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}

	return tx.Commit()
}
```

- [ ] **Step 10: Run test to verify it passes**

Run: `go test ./internal/database/ -v -run TestMigrate`
Expected: PASS

- [ ] **Step 11: Commit**

```bash
git add -A
git commit -m "feat: SQLite database with migrations and full schema"
```

---

### Task 3: Authentication — JWT + bcrypt

**Files:**
- Create: `internal/auth/auth.go`
- Create: `internal/auth/auth_test.go`
- Create: `internal/auth/middleware.go`
- Create: `internal/auth/middleware_test.go`

- [ ] **Step 1: Install dependencies**

```bash
go get golang.org/x/crypto/bcrypt
go get github.com/golang-jwt/jwt/v5
```

- [ ] **Step 2: Write auth test**

```go
// internal/auth/auth_test.go
package auth

import (
	"testing"
	"time"
)

func TestHashAndVerifyPassword(t *testing.T) {
	password := "testPassword123!"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("hash failed: %v", err)
	}
	if !CheckPassword(password, hash) {
		t.Error("password should match")
	}
	if CheckPassword("wrongPassword", hash) {
		t.Error("wrong password should not match")
	}
}

func TestGenerateAndValidateJWT(t *testing.T) {
	secret := "test-secret-key-32-bytes-long!!"
	svc := NewService(secret)

	token, err := svc.GenerateToken(1, "admin", true)
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("validate token failed: %v", err)
	}
	if claims.UserID != 1 {
		t.Errorf("expected user_id 1, got %d", claims.UserID)
	}
	if claims.Username != "admin" {
		t.Errorf("expected username admin, got %s", claims.Username)
	}
	if !claims.IsAdmin {
		t.Error("expected is_admin true")
	}
}

func TestExpiredJWT(t *testing.T) {
	secret := "test-secret-key-32-bytes-long!!"
	svc := NewService(secret)
	svc.tokenDuration = -1 * time.Hour // Force expired

	token, err := svc.GenerateToken(1, "admin", true)
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}

	_, err = svc.ValidateToken(token)
	if err == nil {
		t.Error("expired token should fail validation")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/auth/ -v`
Expected: FAIL

- [ ] **Step 4: Write auth.go**

```go
// internal/auth/auth.go
package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type Claims struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
	jwt.RegisteredClaims
}

type Service struct {
	secret        []byte
	tokenDuration time.Duration
}

func NewService(secret string) *Service {
	return &Service{
		secret:        []byte(secret),
		tokenDuration: 24 * time.Hour,
	}
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (s *Service) GenerateToken(userID int, username string, isAdmin bool) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   userID,
		Username: username,
		IsAdmin:  isAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.tokenDuration)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

func (s *Service) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/auth/ -v`
Expected: PASS

- [ ] **Step 6: Write middleware test**

```go
// internal/auth/middleware_test.go
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddlewareRejectsNoToken(t *testing.T) {
	svc := NewService("test-secret-key-32-bytes-long!!")
	handler := svc.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestMiddlewareAcceptsValidCookie(t *testing.T) {
	svc := NewService("test-secret-key-32-bytes-long!!")
	token, _ := svc.GenerateToken(1, "admin", true)

	handler := svc.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := GetClaims(r)
		if claims == nil {
			t.Error("claims should be in context")
			return
		}
		if claims.UserID != 1 {
			t.Errorf("expected user_id 1, got %d", claims.UserID)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
```

- [ ] **Step 7: Run test to verify it fails**

Run: `go test ./internal/auth/ -v -run TestMiddleware`
Expected: FAIL

- [ ] **Step 8: Write middleware.go**

```go
// internal/auth/middleware.go
package auth

import (
	"context"
	"net/http"
)

type contextKey string

const claimsKey contextKey = "claims"

func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("token")
		if err != nil {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		claims, err := s.ValidateToken(cookie.Value)
		if err != nil {
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetClaims(r *http.Request) *Claims {
	claims, _ := r.Context().Value(claimsKey).(*Claims)
	return claims
}
```

- [ ] **Step 9: Run all auth tests**

Run: `go test ./internal/auth/ -v`
Expected: ALL PASS

- [ ] **Step 10: Commit**

```bash
git add -A
git commit -m "feat: authentication with bcrypt password hashing and JWT middleware"
```

---

### Task 4: Auth API Endpoints (Login, Logout, Password Reset, JWT Refresh)

**Files:**
- Create: `internal/api/router.go`
- Create: `internal/api/auth_handler.go`
- Create: `internal/api/auth_handler_test.go`
- Create: `internal/api/response.go`
- Create: `internal/auth/reset_token.go`
- Create: `internal/auth/reset_token_test.go`

- [ ] **Step 1: Write response helpers**

```go
// internal/api/response.go
package api

import (
	"encoding/json"
	"net/http"
)

func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func Error(w http.ResponseWriter, status int, message string) {
	JSON(w, status, map[string]string{"error": message})
}
```

- [ ] **Step 2: Write auth handler test**

```go
// internal/api/auth_handler_test.go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/backupmanager/backupmanager/internal/database"
)

func setupTestDB(t *testing.T) *database.Database {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestLoginSuccess(t *testing.T) {
	db := setupTestDB(t)
	authSvc := auth.NewService("test-secret-32-bytes-long-key!!")
	handler := NewAuthHandler(db, authSvc)

	// Create test user
	hash, _ := auth.HashPassword("password123")
	db.DB().Exec("INSERT INTO users (username, email, password_hash, is_admin) VALUES (?, ?, ?, ?)",
		"admin", "admin@test.com", hash, 1)

	body, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "password123",
	})
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.Login(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}

	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "token" {
			found = true
			if !c.HttpOnly {
				t.Error("cookie should be httpOnly")
			}
			if c.SameSite != http.SameSiteStrictMode {
				t.Error("cookie should have SameSite=Strict")
			}
		}
	}
	if !found {
		t.Error("token cookie not set")
	}
}

func TestLoginWrongPassword(t *testing.T) {
	db := setupTestDB(t)
	authSvc := auth.NewService("test-secret-32-bytes-long-key!!")
	handler := NewAuthHandler(db, authSvc)

	hash, _ := auth.HashPassword("password123")
	db.DB().Exec("INSERT INTO users (username, email, password_hash, is_admin) VALUES (?, ?, ?, ?)",
		"admin", "admin@test.com", hash, 1)

	body, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "wrongpassword",
	})
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.Login(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestLogout(t *testing.T) {
	db := setupTestDB(t)
	authSvc := auth.NewService("test-secret-32-bytes-long-key!!")
	handler := NewAuthHandler(db, authSvc)

	req := httptest.NewRequest("POST", "/api/auth/logout", nil)
	w := httptest.NewRecorder()

	handler.Logout(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	cookies := w.Result().Cookies()
	for _, c := range cookies {
		if c.Name == "token" && c.MaxAge < 0 {
			return // Cookie cleared
		}
	}
	t.Error("token cookie should be cleared")
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/api/ -v -run TestLogin`
Expected: FAIL

- [ ] **Step 4: Write auth handler implementation**

```go
// internal/api/auth_handler.go
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/backupmanager/backupmanager/internal/database"
)

type AuthHandler struct {
	db      *database.Database
	authSvc *auth.Service
}

func NewAuthHandler(db *database.Database, authSvc *auth.Service) *AuthHandler {
	return &AuthHandler{db: db, authSvc: authSvc}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var id int
	var hash string
	var isAdmin bool
	err := h.db.DB().QueryRow(
		"SELECT id, password_hash, is_admin FROM users WHERE username = ?",
		req.Username,
	).Scan(&id, &hash, &isAdmin)
	if err != nil {
		Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if !auth.CheckPassword(req.Password, hash) {
		Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := h.authSvc.GenerateToken(id, req.Username, isAdmin)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(24 * time.Hour / time.Second),
	})

	JSON(w, http.StatusOK, map[string]interface{}{
		"user_id":  id,
		"username": req.Username,
		"is_admin": isAdmin,
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	JSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/api/ -v`
Expected: PASS

- [ ] **Step 6: Write router.go**

```go
// internal/api/router.go
package api

import (
	"net/http"

	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/backupmanager/backupmanager/internal/database"
)

func NewRouter(db *database.Database, authSvc *auth.Service) http.Handler {
	mux := http.NewServeMux()
	authHandler := NewAuthHandler(db, authSvc)

	// Public routes
	mux.HandleFunc("POST /api/auth/login", authHandler.Login)
	mux.HandleFunc("POST /api/auth/logout", authHandler.Logout)

	// Health check
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Protected routes (will be added in later tasks)
	// protected := authSvc.RequireAuth(protectedMux)

	return mux
}
```

- [ ] **Step 7: Write password reset tests**

Test: generate reset token (random, 1h expiry), store in DB, validate token, consume token (one-time use), reject expired token, reject already-used token.

- [ ] **Step 8: Implement password reset flow**

- `POST /api/auth/reset-password` — accepts email, generates random token, stores hash in `password_reset_tokens` table (add to migration) with 1h expiry, sends email via notification.Email (if configured, else logs token for dev). Returns 200 regardless (no email enumeration).
- `POST /api/auth/reset-password/confirm` — accepts token + new password, validates token not expired/used, updates user password, invalidates token.
- Create `internal/auth/reset_token.go` with `GenerateResetToken()`, `StoreResetToken()`, `ValidateResetToken()`, `ConsumeResetToken()`.

- [ ] **Step 9: Write JWT refresh middleware test**

Test: request with JWT expiring within 1 hour gets a refreshed cookie in response, request with JWT expiring in >1 hour does not get refreshed.

- [ ] **Step 10: Implement JWT automatic refresh middleware**

Add `RefreshMiddleware` to auth package: on every authenticated request, check if JWT expires within 1 hour. If so, generate a new token and set updated cookie. This silently extends sessions for active users.

- [ ] **Step 11: Run all auth tests**

Run: `go test ./internal/auth/ ./internal/api/ -v`
Expected: ALL PASS

- [ ] **Step 12: Commit**

```bash
git add -A
git commit -m "feat: auth API with login, logout, password reset, JWT auto-refresh"
```

---

### Task 5: React Frontend Shell

**Files:**
- Create: `frontend/package.json`
- Create: `frontend/tsconfig.json`
- Create: `frontend/vite.config.ts`
- Create: `frontend/tailwind.config.ts`
- Create: `frontend/postcss.config.js`
- Create: `frontend/index.html`
- Create: `frontend/src/main.tsx`
- Create: `frontend/src/App.tsx`
- Create: `frontend/src/pages/LoginPage.tsx`
- Create: `frontend/src/pages/DashboardPage.tsx`
- Create: `frontend/src/components/Layout.tsx`
- Create: `frontend/src/components/Sidebar.tsx`
- Create: `frontend/src/api/client.ts`
- Create: `frontend/src/hooks/useAuth.ts`
- Create: `frontend/src/types/index.ts`

- [ ] **Step 1: Initialize React project with Vite**

```bash
cd frontend
npm create vite@latest . -- --template react-ts
npm install
npm install tailwindcss @tailwindcss/vite
npm install react-router-dom @tanstack/react-query
npm install recharts lucide-react
npm install -D @types/react @types/react-dom
```

Note: The subagent should use the latest stable versions available and configure Tailwind CSS v4+ with the Vite plugin (not PostCSS). Use `shadcn/ui` init if available, otherwise set up components manually with Tailwind.

- [ ] **Step 2: Configure Vite with API proxy**

```typescript
// frontend/vite.config.ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/ws': { target: 'ws://localhost:8080', ws: true },
    },
  },
  build: {
    outDir: 'dist',
  },
})
```

- [ ] **Step 3: Create API client**

```typescript
// frontend/src/api/client.ts
const BASE = '';

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || res.statusText);
  }
  return res.json();
}

export const api = {
  login: (username: string, password: string) =>
    request('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  logout: () => request('/api/auth/logout', { method: 'POST' }),
  healthCheck: () => request<{ status: string }>('/api/health'),
};
```

- [ ] **Step 4: Create types**

```typescript
// frontend/src/types/index.ts
export interface User {
  user_id: number;
  username: string;
  is_admin: boolean;
}

export interface Server {
  id: number;
  name: string;
  type: 'linux' | 'windows';
  host: string;
  port: number;
  connection_type: 'ssh' | 'ftp';
  status: 'online' | 'offline' | 'warning' | 'unknown';
  created_at: string;
}

export interface BackupRun {
  id: number;
  job_id: number;
  status: 'pending' | 'running' | 'success' | 'failed' | 'timeout';
  started_at: string;
  finished_at: string;
  total_size_bytes: number;
  files_copied: number;
  error_message?: string;
}
```

- [ ] **Step 5: Create useAuth hook**

```typescript
// frontend/src/hooks/useAuth.ts
import { useState, useCallback } from 'react';
import { api } from '../api/client';
import type { User } from '../types';

export function useAuth() {
  const [user, setUser] = useState<User | null>(() => {
    const saved = localStorage.getItem('bm_user');
    return saved ? JSON.parse(saved) : null;
  });

  const login = useCallback(async (username: string, password: string) => {
    const data = await api.login(username, password) as User;
    setUser(data);
    localStorage.setItem('bm_user', JSON.stringify(data));
    return data;
  }, []);

  const logout = useCallback(async () => {
    await api.logout();
    setUser(null);
    localStorage.removeItem('bm_user');
  }, []);

  // Call this on any 401 response to clear stale localStorage state
  const handleUnauthorized = useCallback(() => {
    setUser(null);
    localStorage.removeItem('bm_user');
  }, []);

  return { user, login, logout, handleUnauthorized, isAuthenticated: !!user };
}
// Note: The API client (client.ts) should intercept 401 responses and call
// handleUnauthorized() to clear stale user state from localStorage.
// This prevents the UI from believing the user is logged in when the
// server-side session (httpOnly JWT cookie) has expired.
```

- [ ] **Step 6: Create Layout + Sidebar components**

The subagent should create:
- `Layout.tsx` — main layout with sidebar, top bar with notifications bell and user menu
- `Sidebar.tsx` — navigation sidebar with items: Dashboard, Servers, Jobs, Snapshots, Recovery, Docs, Assistant, Settings, Audit Log. Use `lucide-react` icons. Active item highlighted.
- Both components use Tailwind for styling
- Sidebar is collapsible on mobile

- [ ] **Step 7: Create LoginPage**

```typescript
// frontend/src/pages/LoginPage.tsx
// Login form with username/password fields
// Calls useAuth().login on submit
// Shows error message on failure
// Redirects to / on success
// Clean, professional design with Tailwind
// BackupManager logo/title centered
```

- [ ] **Step 8: Create DashboardPage (placeholder)**

```typescript
// frontend/src/pages/DashboardPage.tsx
// Placeholder with welcome message
// Will be populated in later tasks
export default function DashboardPage() {
  return (
    <div className="p-6">
      <h1 className="text-2xl font-bold">Dashboard</h1>
      <p className="text-gray-500 mt-2">BackupManager is running.</p>
    </div>
  );
}
```

- [ ] **Step 9: Create App.tsx with routing**

```typescript
// frontend/src/App.tsx
// React Router setup:
//   / → DashboardPage (requires auth)
//   /login → LoginPage
//   /servers → placeholder
//   /jobs → placeholder
//   /snapshots → placeholder
//   /recovery → placeholder
//   /docs → placeholder
//   /assistant → placeholder
//   /settings → placeholder
//   /audit → placeholder
// Redirect to /login if not authenticated
// Wrap authenticated routes in Layout
```

- [ ] **Step 10: Verify frontend builds**

Run: `cd frontend && npm run build`
Expected: Build succeeds, output in `frontend/dist/`

- [ ] **Step 11: Commit**

```bash
git add -A
git commit -m "feat: React frontend shell with login, layout, sidebar, routing"
```

---

### Task 6: Embed Frontend in Go Binary + First Run Setup

**Files:**
- Modify: `cmd/server/main.go`
- Create: `internal/setup/setup.go`
- Create: `internal/setup/setup_test.go`

- [ ] **Step 1: Write first-run setup test**

```go
// internal/setup/setup_test.go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/setup/ -v`
Expected: FAIL

- [ ] **Step 3: Write setup.go**

```go
// internal/setup/setup.go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/setup/ -v`
Expected: PASS

- [ ] **Step 5: Update main.go to embed frontend and wire everything**

```go
// cmd/server/main.go
// - Embed frontend/dist using embed.FS
// - Load config
// - Ensure data directories
// - Open SQLite + run migrations (fail hard if migration fails)
// - Ensure admin user exists (default: admin/admin, log warning to change)
// - Generate JWT secret if not set (random 32 bytes, store in settings table)
// - Create auth service
// - Create API router
// - Serve embedded frontend for non-API routes (SPA fallback)
// - Start HTTP server
// - Graceful shutdown on SIGINT/SIGTERM
```

The subagent should implement the full main.go with embed, graceful shutdown, and all wiring.

- [ ] **Step 6: Build and verify full stack**

Run: `cd frontend && npm run build && cd .. && go build -o bin/backupmanager ./cmd/server`
Expected: Binary created, starts and serves both API and frontend

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat: embed React frontend in Go binary, first-run admin setup"
```

---

## Phase 2: Server Management

### Task 7: Server CRUD API

**Files:**
- Create: `internal/api/servers_handler.go`
- Create: `internal/api/servers_handler_test.go`

- [ ] **Step 1: Write server CRUD tests**

Test: list servers (empty), create server (Linux SSH), create server (Windows FTP), get server by ID, update server, delete server, validation errors (missing host, invalid type).

- [ ] **Step 2: Run tests to verify they fail**

- [ ] **Step 3: Implement ServersHandler**

Endpoints:
- `GET /api/servers` — list all
- `POST /api/servers` — create (validate: name, type, host, port, connection_type required)
- `GET /api/servers/{id}` — get by ID
- `PUT /api/servers/{id}` — update
- `DELETE /api/servers/{id}` — delete (cascade backup_sources, etc.)
- `POST /api/servers/test-connection` — test SSH/FTP connectivity without saving (accepts host, port, connection_type, credentials; returns success/failure with error message). Used by Add Server Wizard step 2.

- [ ] **Step 4: Run tests to verify they pass**

- [ ] **Step 5: Wire into router.go (protected routes)**

- [ ] **Step 6: Commit**

```bash
git commit -m "feat: server CRUD API endpoints"
```

---

### Task 8: SSH Connector

**Files:**
- Create: `internal/connector/connector.go` (interface)
- Create: `internal/connector/ssh.go`
- Create: `internal/connector/ssh_test.go`

- [ ] **Step 1: Define Connector interface**

```go
// internal/connector/connector.go
type Connector interface {
    Connect() error
    Close() error
    RunCommand(cmd string) (stdout string, stderr string, exitCode int, err error)
    CopyFile(remotePath, localPath string) error
    ListFiles(remotePath string) ([]FileInfo, error)
}

type FileInfo struct {
    Path    string
    Size    int64
    ModTime time.Time
    IsDir   bool
}
```

- [ ] **Step 2: Write SSH connector tests (unit tests with interface mocking)**

Test: command execution, file listing, error handling for connection refused.
Note: Full integration tests require a real SSH server — mark with `//go:build integration`.

- [ ] **Step 3: Implement SSH connector using golang.org/x/crypto/ssh**

```bash
go get golang.org/x/crypto/ssh
```

Support both password and key-based auth. Implement `RunCommand`, `CopyFile` (via SFTP — `github.com/pkg/sftp`), `ListFiles`.

- [ ] **Step 4: Run tests**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat: SSH connector with command execution and SFTP file transfer"
```

---

### Task 9: FTP Connector

**Files:**
- Create: `internal/connector/ftp.go`
- Create: `internal/connector/ftp_test.go`

- [ ] **Step 1: Write FTP connector tests**

Test: connect, list files, download file, manifest-based change detection.

- [ ] **Step 2: Implement FTP connector**

```bash
go get github.com/jlaffaye/ftp
```

Implement the `Connector` interface. Add `FTPManifest` struct for tracking file state (path + size + mtime + sha256). `RunCommand` returns error (FTP doesn't support commands).

- [ ] **Step 3: Run tests**

- [ ] **Step 4: Commit**

```bash
git commit -m "feat: FTP connector with manifest-based change detection"
```

---

### Task 10: Auto-Discovery Service

**Files:**
- Create: `internal/discovery/discovery.go`
- Create: `internal/discovery/discovery_test.go`

- [ ] **Step 1: Write discovery tests**

Test: parse NGINX vhosts from `sites-enabled` output, parse MySQL databases from `SHOW DATABASES`, parse PM2 process list from JSON, parse Certbot certificates, handle missing services gracefully.

- [ ] **Step 2: Implement discovery service**

For each service, run detection command via SSH connector, parse output, return structured results. Store in `discovery_results` table.

Services: NGINX, MySQL, PM2, Certbot, Node.js, Crontab, UFW.

- [ ] **Step 3: Add API endpoint `POST /api/servers/{id}/discover`**

- [ ] **Step 4: Run tests**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat: auto-discovery of services on Linux servers via SSH"
```

---

### Task 11: Backup Sources CRUD API

**Files:**
- Create: `internal/api/sources_handler.go`
- Create: `internal/api/sources_handler_test.go`

- [ ] **Step 1: Write tests**

Test: create source (web, database, config types), list sources for server, dependency setting, validation, **cycle detection** (reject `depends_on` that would create a circular dependency — A→B→C→A should return 400 error).

- [ ] **Step 2: Implement SourcesHandler**

Endpoints:
- `GET /api/servers/{id}/sources` — list sources for server
- `POST /api/servers/{id}/sources` — create source
- `PUT /api/sources/{id}` — update
- `DELETE /api/sources/{id}` — delete

- [ ] **Step 3: Run tests**

- [ ] **Step 4: Commit**

```bash
git commit -m "feat: backup sources CRUD API"
```

---

### Task 12: Servers UI + Add Server Wizard

**Files:**
- Create: `frontend/src/pages/ServersPage.tsx`
- Create: `frontend/src/pages/ServerDetailPage.tsx`
- Create: `frontend/src/components/AddServerWizard.tsx`
- Create: `frontend/src/components/ServerCard.tsx`
- Create: `frontend/src/api/servers.ts`

- [ ] **Step 1: Create API client for servers**

Functions: `listServers`, `createServer`, `getServer`, `updateServer`, `deleteServer`, `discoverServices`, `listSources`, `createSource`.

- [ ] **Step 2: Build ServersPage**

Grid of server cards showing name, type, status, host, last backup time.

- [ ] **Step 3: Build AddServerWizard**

Multi-step wizard:
- Linux flow (6 steps): type → connection → auto-discovery → source selection → MySQL setup (with copyable commands) → scheduling
- Windows flow (4 steps): type → connection → source selection → scheduling
- Connection test button at step 2
- Auto-discovery results displayed with checkboxes at step 3

- [ ] **Step 4: Build ServerDetailPage**

Detail view: server info, discovered services, configured sources, backup history (placeholder).

- [ ] **Step 5: Verify UI works with backend**

Run both frontend dev server and Go backend, test full wizard flow.

- [ ] **Step 6: Commit**

```bash
git commit -m "feat: servers UI with add server wizard, detail page, auto-discovery"
```

---

## Phase 3: Backup Engine

### Task 13: Incremental Sync Engine (rsync via SSH)

**Files:**
- Create: `internal/sync/sync.go`
- Create: `internal/sync/rsync.go`
- Create: `internal/sync/rsync_test.go`

- [ ] **Step 1: Define Syncer interface**

```go
type SyncResult struct {
    FilesCopied   int
    BytesCopied   int64
    FilesDeleted  int
    Duration      time.Duration
    Errors        []string
}

type Syncer interface {
    Sync(ctx context.Context, source SyncSource, destPath string, opts SyncOptions) (*SyncResult, error)
}

type SyncSource struct {
    ServerID   int
    RemotePath string
    Connector  connector.Connector
}

type SyncOptions struct {
    BandwidthLimitMbps int
    Exclude            []string
    DryRun             bool
}
```

- [ ] **Step 2: Write rsync tests**

Test: build rsync command with correct flags, parse rsync output for stats, handle bandwidth limit flag, handle SSH key path.

- [ ] **Step 3: Implement rsync syncer**

Execute rsync via `os/exec` with flags: `-avz --delete --stats --partial`. Add `--bwlimit` if configured. Parse output for files copied, bytes transferred.

- [ ] **Step 4: Run tests**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat: rsync-based incremental sync engine for SSH servers"
```

---

### Task 14: FTP Incremental Sync Engine

**Files:**
- Create: `internal/sync/ftp_sync.go`
- Create: `internal/sync/ftp_sync_test.go`
- Create: `internal/sync/manifest.go`
- Create: `internal/sync/manifest_test.go`

- [ ] **Step 1: Write manifest tests**

Test: save manifest, load manifest, compare manifests (detect new/modified/deleted files), handle missing mtime (always compute hash).

- [ ] **Step 2: Implement manifest**

JSON file stored alongside backup: tracks each file's path, size, mtime (nullable), SHA256.

- [ ] **Step 3: Write FTP sync tests**

Test: first sync (all files new), incremental sync (only changed files), rate limiting.

- [ ] **Step 4: Implement FTP syncer**

Compare remote file list with local manifest. Download changed/new files. Application-level rate limiting on read loop (configurable bytes/sec). Update manifest after sync.

- [ ] **Step 5: Run tests**

- [ ] **Step 6: Commit**

```bash
git commit -m "feat: FTP incremental sync with manifest-based change detection"
```

---

### Task 15: MySQL Dump Orchestrator

**Files:**
- Create: `internal/backup/mysql_dump.go`
- Create: `internal/backup/mysql_dump_test.go`

- [ ] **Step 1: Write MySQL dump tests**

Test: build dump command string, parse checksum output, handle dump timeout, handle dump failure (non-zero exit code), remote cleanup command.

- [ ] **Step 2: Implement MySQL dump orchestrator**

Full flow per spec Section 2.2:
1. Connect SSH
2. Execute mysqldump with `--single-transaction --routines --triggers` piped to gzip
3. Path: `/var/backups/backupmanager/dbname_YYYY-MM-DD.sql.gz`
4. Calculate remote SHA256
5. Copy via SFTP
6. Verify local SHA256 matches
7. Register snapshot
8. Cleanup remote dumps older than configured days
9. Return result with status

Configurable timeout (default 30 min per database).

- [ ] **Step 3: Run tests**

- [ ] **Step 4: Commit**

```bash
git commit -m "feat: MySQL dump orchestrator with remote execution, verification, cleanup"
```

---

### Task 16: Backup Job Runner + Orchestrator

**Files:**
- Create: `internal/backup/orchestrator.go`
- Create: `internal/backup/orchestrator_test.go`
- Create: `internal/backup/runner.go`
- Create: `internal/backup/runner_test.go`

- [ ] **Step 1: Write orchestrator tests**

Test: resolve dependency graph, execute sources in priority order, handle partial failure (one source fails, others continue), timeout handling, record run in database.

- [ ] **Step 2: Implement orchestrator**

- Sort backup sources by dependency graph (topological sort)
- Execute each source using appropriate syncer (rsync for SSH, FTP sync for FTP, MySQL dump for databases)
- Track progress in `backup_runs` table
- Create `backup_snapshots` entries for each copied source
- Calculate and store checksums
- Handle job timeout (kill after configured minutes)
- Return aggregate result

- [ ] **Step 3: Write runner tests**

Test: runner picks up pending job, executes orchestrator, updates run status, concurrent runs prevented for same server.

- [ ] **Step 4: Implement runner**

Job runner that:
- Takes a job ID
- Creates a `backup_run` entry (status=running)
- Calls orchestrator
- Updates run status (success/failed/timeout)
- Logs all output

- [ ] **Step 5: Run all tests**

- [ ] **Step 6: Commit**

```bash
git commit -m "feat: backup orchestrator with dependency graph and job runner"
```

---

### Task 17: Scheduler (Cron)

**Files:**
- Create: `internal/scheduler/scheduler.go`
- Create: `internal/scheduler/scheduler_test.go`

- [ ] **Step 1: Write scheduler tests**

Test: parse cron expression, calculate next run time, trigger job at scheduled time, skip if previous run still active, respect bandwidth windows.

- [ ] **Step 2: Implement scheduler**

```bash
go get github.com/robfig/cron/v3
```

- Wraps `robfig/cron` library
- Loads all enabled jobs from DB on startup
- Registers each job's schedule
- On trigger: calls job runner in a goroutine
- Provides methods: `Start()`, `Stop()`, `AddJob()`, `RemoveJob()`, `UpdateJob()`
- Smart scheduling: checks bandwidth limits before starting (stub for time-of-day windows until Task 38 implements full bandwidth throttling)
- **Missed backup detection:** periodic check (every 15 minutes) scans all enabled jobs. If a job's last run is older than expected (based on schedule) by more than 2× the schedule interval, trigger a "missed_backup" notification event via notification manager

- [ ] **Step 3: Run tests**

- [ ] **Step 4: Commit**

```bash
git commit -m "feat: cron-based backup scheduler with bandwidth awareness"
```

---

### Task 18: Retention Policy Engine

**Files:**
- Create: `internal/retention/retention.go`
- Create: `internal/retention/retention_test.go`

- [ ] **Step 1: Write retention tests**

Test: keep all daily for 7 days, keep weekly (Sunday) for 4 weeks, keep monthly (1st) for 3 months, delete expired, never delete the most recent snapshot, timezone-aware day boundaries.

- [ ] **Step 2: Implement retention engine**

- `Apply(snapshots []Snapshot, policy RetentionPolicy, timezone string) []SnapshotToDelete`
- Uses configured timezone for day boundaries
- Returns list of snapshots to delete
- Cleanup runner: periodically scans all snapshots, applies policies, deletes files + DB entries
- Pre-deletion: log what will be deleted, verify there's at least 1 remaining snapshot

- [ ] **Step 3: Run tests**

- [ ] **Step 4: Commit**

```bash
git commit -m "feat: retention policy engine with timezone-aware cleanup"
```

---

### Task 19: Backup Jobs API + Manual Trigger

**Files:**
- Create: `internal/api/jobs_handler.go`
- Create: `internal/api/jobs_handler_test.go`
- Create: `internal/api/runs_handler.go`

- [ ] **Step 1: Write tests**

Test: create job, list jobs, update schedule, trigger manual backup, list runs for job, get run logs.

- [ ] **Step 2: Implement JobsHandler**

Endpoints:
- `GET /api/jobs` — list all jobs with last run info
- `POST /api/jobs` — create job (with source selection)
- `PUT /api/jobs/{id}` — update
- `DELETE /api/jobs/{id}` — delete
- `POST /api/jobs/{id}/trigger` — manual trigger (runs in background goroutine)
- `GET /api/runs` — list runs (filterable by job, server, status, date range)
- `GET /api/runs/{id}/logs` — get logs for a specific run

- [ ] **Step 3: Run tests**

- [ ] **Step 4: Commit**

```bash
git commit -m "feat: backup jobs API with manual trigger and run history"
```

---

### Task 20: Jobs UI

**Files:**
- Create: `frontend/src/pages/JobsPage.tsx`
- Create: `frontend/src/components/JobCard.tsx`
- Create: `frontend/src/components/CreateJobModal.tsx`
- Create: `frontend/src/components/ScheduleSelector.tsx`
- Create: `frontend/src/components/RunHistory.tsx`
- Create: `frontend/src/api/jobs.ts`

- [ ] **Step 1: Create API client for jobs**

- [ ] **Step 2: Build JobsPage**

List of job cards: name, server, schedule (human-readable), last run status + time, next run time, avg duration. Manual trigger button per job.

- [ ] **Step 3: Build CreateJobModal**

Form: select server, select sources, set schedule (visual cron picker: frequency dropdown + time picker), retention policy, bandwidth limit, timeout.

- [ ] **Step 4: Build ScheduleSelector**

Visual cron configuration: "Every day at 03:00", "Every Sunday at 02:00", custom cron expression (advanced mode).

- [ ] **Step 5: Build RunHistory component**

Table: run ID, status (with color), started, duration, size, files, error. Click to view logs.

- [ ] **Step 6: Commit**

```bash
git commit -m "feat: jobs UI with schedule selector, manual trigger, run history"
```

---

## Phase 4: Monitoring & Notifications

### Task 21: Health Check Service

**Files:**
- Create: `internal/health/health.go`
- Create: `internal/health/health_test.go`
- Create: `internal/health/checks.go`

- [ ] **Step 1: Write health check tests**

Test: parse disk space output, parse systemctl status, parse PM2 JSON, determine status level (ok/warning/critical), Windows FTP-only checks (TCP + FTP handshake only).

- [ ] **Step 2: Implement health checks**

Linux checks: reachability (TCP connect), SSH, disk space, NGINX, MySQL, PM2, CPU, RAM.
Windows checks: reachability (TCP connect), FTP handshake.

Each check returns `CheckResult{Type, Status, Message, Value}`. Health service runs all checks for a server, stores results in `health_checks` table.

- [ ] **Step 3: Implement health monitor loop**

Background goroutine: every N seconds (configurable per server, default 300), run all enabled checks. Compare with previous state — if status changed, trigger notification.

- [ ] **Step 4: Add health API endpoints**

`GET /api/health/servers` — all servers current health.
`GET /api/health/servers/{id}/history` — check history for one server.

- [ ] **Step 5: Run tests**

- [ ] **Step 6: Commit**

```bash
git commit -m "feat: proactive health check system with configurable thresholds"
```

---

### Task 22: Telegram Notifier

**Files:**
- Create: `internal/notification/telegram.go`
- Create: `internal/notification/telegram_test.go`

- [ ] **Step 1: Write Telegram tests**

Test: format message, send API call (mock HTTP), handle API error, anti-flood (suppress duplicate alerts).

- [ ] **Step 2: Implement Telegram notifier**

Uses Telegram Bot API HTTP endpoint. Formats messages with markdown. Implements anti-flood: tracks last alert per server+event, suppresses if within 30 minutes.

- [ ] **Step 3: Run tests**

- [ ] **Step 4: Commit**

```bash
git commit -m "feat: Telegram notification with anti-flood protection"
```

---

### Task 23: Email Notifier (SMTP)

**Files:**
- Create: `internal/notification/email.go`
- Create: `internal/notification/email_test.go`

- [ ] **Step 1: Write email tests**

Test: build email message, format HTML template, handle SMTP connection error.

- [ ] **Step 2: Implement SMTP email notifier**

Uses Go `net/smtp`. HTML email templates for different event types. Supports multiple recipients. Anti-flood same as Telegram.

- [ ] **Step 3: Run tests**

- [ ] **Step 4: Commit**

```bash
git commit -m "feat: SMTP email notifications with HTML templates"
```

---

### Task 24: Notification Manager + API

**Files:**
- Create: `internal/notification/manager.go`
- Create: `internal/notification/manager_test.go`
- Create: `internal/api/notifications_handler.go`

- [ ] **Step 1: Write manager tests**

Test: dispatch notification to correct channels based on config, log all notifications, test notification sends on all channels.

- [ ] **Step 2: Implement notification manager**

Central dispatcher: receives events, checks `notifications_config` table, sends via Telegram and/or Email as configured. Logs to `notifications_log`. Provides test send endpoint.

- [ ] **Step 3: Add notifications API**

`GET /api/notifications/config` — get all notification configs.
`PUT /api/notifications/config` — update configs.
`POST /api/notifications/test` — send test notification.
`GET /api/notifications/log` — notification history.

- [ ] **Step 4: Run tests**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat: notification manager with Telegram + Email dispatching"
```

---

### Task 25: WebSocket Real-Time Updates

**Files:**
- Create: `internal/websocket/hub.go`
- Create: `internal/websocket/hub_test.go`

- [ ] **Step 1: Write WebSocket hub tests**

Test: client registration, broadcast message, client disconnect, heartbeat ping/pong.

- [ ] **Step 2: Implement WebSocket hub**

```bash
go get github.com/gorilla/websocket
```

- Hub manages connected clients
- Auth via httpOnly cookie on upgrade request
- Message types: `log`, `status`, `health`
- JSON message format: `{"type": "...", "server_id": "...", "data": {...}, "timestamp": "..."}`
- Ping every 30s, close after 3 missed pongs
- Integrate with backup runner (stream logs live) and health checker (stream status changes)

- [ ] **Step 3: Add WebSocket endpoints to router**

`/ws/logs` — real-time log streaming during backup runs.
`/ws/status` — backup status and health check updates.

- [ ] **Step 4: Run tests**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat: WebSocket hub for real-time log streaming and status updates"
```

---

### Task 26: Dashboard UI

**Files:**
- Modify: `frontend/src/pages/DashboardPage.tsx`
- Create: `frontend/src/components/ServerStatusCard.tsx`
- Create: `frontend/src/components/BackupTimeline.tsx`
- Create: `frontend/src/components/DiskUsageChart.tsx`
- Create: `frontend/src/components/AlertsList.tsx`
- Create: `frontend/src/hooks/useWebSocket.ts`
- Create: `frontend/src/api/dashboard.ts`
- Create: `internal/api/dashboard_handler.go`

- [ ] **Step 1: Create dashboard summary API**

`GET /api/dashboard/summary` — returns: server statuses, recent backup results, next scheduled backups, disk usage per destination, active alerts, growth trend data (last 30 days).

- [ ] **Step 2: Create useWebSocket hook**

WebSocket client with auto-reconnect (exponential backoff: 1s, 2s, 4s, max 30s). Parses JSON messages, dispatches to handlers by type.

- [ ] **Step 3: Build DashboardPage components**

- **ServerStatusCard**: server name, type, status icon/color, last check time
- **BackupTimeline**: next 24h scheduled backups as timeline, currently running backups with progress
- **DiskUsageChart**: bar chart showing used/free per destination (Recharts)
- **AlertsList**: active alerts with severity, timestamp, dismiss button
- **Growth trend**: line chart of total backup size over 30 days

- [ ] **Step 4: Integrate WebSocket for live updates**

Dashboard auto-updates server status and backup progress without page refresh.

- [ ] **Step 5: Commit**

```bash
git commit -m "feat: dashboard UI with live status, disk usage, alerts, growth trend"
```

---

## Phase 5: Security & Integrity

### Task 27: AES-256 Encryption at Rest

**Files:**
- Create: `internal/encryption/encryption.go`
- Create: `internal/encryption/encryption_test.go`
- Create: `internal/encryption/keymanager.go`
- Create: `internal/encryption/keymanager_test.go`

- [ ] **Step 1: Write encryption tests**

Test: encrypt then decrypt file (round-trip), wrong key fails, generate master key, wrap key with password (Argon2), unwrap key with correct password, unwrap fails with wrong password.

- [ ] **Step 2: Implement AES-256-GCM file encryption**

- `EncryptFile(inputPath, outputPath string, key []byte) error`
- `DecryptFile(inputPath, outputPath string, key []byte) error`
- Stream-based (don't load entire file in memory)
- File format: `[12-byte nonce][encrypted data][16-byte GCM tag]`

- [ ] **Step 3: Implement key manager**

- `GenerateMasterKey() ([]byte, error)` — 32 random bytes
- `WrapKey(masterKey []byte, password string) ([]byte, error)` — Argon2id + AES wrap
- `UnwrapKey(wrappedKey []byte, password string) ([]byte, error)`
- `ExportKeyFile(key []byte, path string) error`
- Store wrapped key in `settings` table
- Settings API for re-displaying/downloading key (requires password confirmation)

- [ ] **Step 4: Run tests**

- [ ] **Step 5: Implement master key loading at startup**

On application startup in `main.go`: if encryption is enabled in settings, load wrapped key from `settings` table, unwrap using a startup password (from env var `BM_MASTER_PASSWORD` or prompt). Store unwrapped key in-memory in a `KeyManager` singleton. All handlers that need the key (backup pipeline, download endpoint) access it via `KeyManager.GetKey()`.

- [ ] **Step 6: Integrate with backup pipeline**

If encryption enabled: after backup file lands on disk, encrypt it using `KeyManager.GetKey()`, delete plaintext. Snapshots table tracks `is_encrypted`. Download endpoint decrypts on-the-fly using same key.

- [ ] **Step 7: Implement background re-encryption job**

When encryption is enabled on a system with existing unencrypted snapshots:
- Background goroutine scans all snapshots where `is_encrypted = 0`
- Encrypts each file, updates `is_encrypted` flag, deletes plaintext
- Progress visible in Settings UI
- Snapshots UI shows encrypted/unencrypted badge on each snapshot

- [ ] **Step 8: Run tests**

- [ ] **Step 9: Commit**

```bash
git commit -m "feat: AES-256-GCM encryption at rest with Argon2 key wrapping and re-encryption"
```

---

### Task 28: Backup Integrity Verification

**Files:**
- Create: `internal/integrity/integrity.go`
- Create: `internal/integrity/integrity_test.go`

- [ ] **Step 1: Write integrity tests**

Test: calculate file checksum, verify checksum matches, detect corrupted file, MySQL dump test restore (parse SQL header as smoke test).

- [ ] **Step 2: Implement integrity checker**

- Post-backup verification: compare local checksum with remote
- Periodic integrity scan: re-calculate checksums for stored snapshots, flag mismatches
- MySQL dump smoke test: decompress, verify SQL syntax in header (not full restore — that requires a MySQL server which may not be available on backup machine)
- API endpoint: `POST /api/integrity/verify/{snapshot_id}`

- [ ] **Step 3: Run tests**

- [ ] **Step 4: Commit**

```bash
git commit -m "feat: backup integrity verification with checksum and dump validation"
```

---

### Task 29: Audit Log

**Files:**
- Create: `internal/audit/audit.go`
- Create: `internal/audit/audit_test.go`
- Create: `internal/api/audit_handler.go`
- Create: `internal/api/audit_handler_test.go`

- [ ] **Step 1: Write audit tests**

Test: log action, query by date range, query by user, query by action type, pagination.

- [ ] **Step 2: Implement audit service**

- `Log(userID int, action, target, ip, details string) error`
- Query with filters: user, action, date range, pagination
- CSV export
- Integrate audit logging into all API handlers via middleware

- [ ] **Step 3: Add audit API**

`GET /api/audit` — paginated, filterable audit log.
`GET /api/audit/export` — CSV download.

- [ ] **Step 4: Run tests**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat: audit log with filtering, pagination, CSV export"
```

---

### Task 30: CSRF Protection + Rate Limiting

**Files:**
- Create: `internal/api/csrf.go`
- Create: `internal/api/csrf_test.go`
- Create: `internal/api/ratelimit.go`
- Create: `internal/api/ratelimit_test.go`

- [ ] **Step 1: Write CSRF tests**

Test: reject POST without CSRF token, accept POST with valid CSRF token (double-submit cookie), GET requests bypass CSRF.

- [ ] **Step 2: Implement CSRF middleware**

Double-submit cookie pattern: set CSRF token in a non-httpOnly cookie, require it in `X-CSRF-Token` header for state-changing requests (POST/PUT/DELETE).

- [ ] **Step 3: Write rate limiting tests**

Test: allow 5 requests, block 6th, unblock after window expires.

- [ ] **Step 4: Implement rate limiter**

In-memory rate limiter per IP for login endpoint. 5 attempts per 5 minutes, 15 minute block. Also persist blocked IPs in SQLite `login_blocks` table so blocks survive process restarts. On startup, load active blocks from DB.

- [ ] **Step 5: Wire into router**

- [ ] **Step 6: Update frontend API client to include CSRF token**

- [ ] **Step 7: Run tests**

- [ ] **Step 8: Commit**

```bash
git commit -m "feat: CSRF protection and login rate limiting"
```

---

### Task 31: Credential Encryption in Database

**Files:**
- Modify: `internal/database/database.go`
- Create: `internal/database/crypto.go`
- Create: `internal/database/crypto_test.go`

- [ ] **Step 1: Write tests**

Test: encrypt credential string, decrypt credential string, round-trip, wrong key fails.

- [ ] **Step 2: Implement credential encryption**

Server passwords and SSH keys stored in DB are encrypted with an app-level key (derived from JWT secret or separate config). `EncryptCredential(plaintext, key) → ciphertext`. `DecryptCredential(ciphertext, key) → plaintext`.

- [ ] **Step 3: Update server handlers to encrypt/decrypt on save/load**

- [ ] **Step 4: Run tests**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat: encrypt server credentials at rest in SQLite"
```

---

## Phase 6: Advanced Features

### Task 32: Multi-Destination Sync

**Files:**
- Create: `internal/sync/destination.go`
- Create: `internal/sync/destination_test.go`
- Create: `internal/api/destinations_handler.go`
- Create: `internal/api/destinations_handler_test.go`

- [ ] **Step 1: Write destination sync tests**

Test: sync to secondary destination after backup, retry on failure (exponential backoff), state machine transitions (pending→in_progress→success/failed), reset in_progress on restart, max retries reached → alert.

- [ ] **Step 2: Implement destination syncer**

- After primary backup completes, queue sync jobs for all enabled secondary destinations
- Copy files from primary to secondary destination path
- Verify checksum at destination
- Track in `destination_sync_status` table
- Retry with exponential backoff (1min, 5min, 30min), max 5 retries
- On restart: reset stale `in_progress` to `pending`

- [ ] **Step 3: Add destinations API**

`GET /api/destinations`, `POST /api/destinations`, `PUT /api/destinations/{id}`, `DELETE /api/destinations/{id}`.
`GET /api/destinations/status` — sync status matrix.
`POST /api/destinations/{id}/retry/{snapshot_id}` — manual retry.

- [ ] **Step 4: Run tests**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat: multi-destination sync with retry and status tracking"
```

---

### Task 33: Snapshots UI with Calendar

**Files:**
- Create: `frontend/src/pages/SnapshotsPage.tsx`
- Create: `frontend/src/components/SnapshotCalendar.tsx`
- Create: `frontend/src/components/SnapshotDetail.tsx`
- Create: `frontend/src/components/SnapshotFilters.tsx`
- Create: `frontend/src/api/snapshots.ts`
- Create: `internal/api/snapshots_handler.go`

- [ ] **Step 1: Add snapshots API**

`GET /api/snapshots` — list with filters (date range, server, type, source).
`GET /api/snapshots/{id}` — detail (size, checksum, integrity, encryption status, retention expiry).
`GET /api/snapshots/{id}/download` — download (decrypt on-the-fly if encrypted).
`GET /api/snapshots/calendar` — aggregated data for calendar view (date → count + status).

- [ ] **Step 2: Build SnapshotCalendar**

Monthly calendar view. Each day shows colored indicators (green=all ok, red=failures, gray=no backup). Click a day → list of that day's snapshots.

- [ ] **Step 3: Build SnapshotFilters**

Filter bar: server dropdown, type dropdown (web/db/config), date range picker.

- [ ] **Step 4: Build SnapshotDetail**

Detail panel: source info, size, checksum, encryption status, integrity verification status, retention expiry, destination sync status matrix. Actions: download, verify integrity, compare with another snapshot.

- [ ] **Step 5: Commit**

```bash
git commit -m "feat: snapshots UI with calendar navigation, filters, detail view"
```

---

### Task 34: Disaster Recovery Playbooks

**Files:**
- Create: `internal/recovery/playbook.go`
- Create: `internal/recovery/playbook_test.go`
- Create: `internal/recovery/generator.go`
- Create: `internal/api/recovery_handler.go`
- Create: `frontend/src/pages/RecoveryPage.tsx`
- Create: `frontend/src/components/PlaybookWizard.tsx`

- [ ] **Step 1: Write playbook generator tests**

Test: generate playbook for server with NGINX+MySQL+PM2, generate for database-only restore, generate for single project restore.

- [ ] **Step 2: Implement playbook generator**

Auto-generates recovery playbooks based on server's configured services and backup sources. Each playbook has:
- Title and scenario description
- Numbered steps with exact commands (copy-paste ready)
- Verification checks between steps
- Links to relevant snapshots needed

Templates for scenarios: full server recovery, single database restore, single project restore, config-only restore, certificate restore.

- [ ] **Step 3: Add recovery API**

`GET /api/recovery/playbooks` — list all.
`GET /api/recovery/playbooks/{id}` — get with steps.
`POST /api/recovery/playbooks/generate/{server_id}` — auto-generate for server.
`PUT /api/recovery/playbooks/{id}` — edit playbook.

- [ ] **Step 4: Build RecoveryPage**

List of playbooks by server. Click → interactive wizard with step-by-step flow, checkboxes per step, copyable commands.

- [ ] **Step 5: Build PlaybookWizard**

Interactive step-by-step execution: numbered steps, copy buttons, verification prompts, completion tracking. Progress saved in localStorage so user can resume.

- [ ] **Step 6: Commit**

```bash
git commit -m "feat: disaster recovery playbooks with auto-generation and interactive wizard"
```

---

### Task 35: AI Assistant

**Files:**
- Create: `internal/assistant/assistant.go`
- Create: `internal/assistant/assistant_test.go`
- Create: `internal/assistant/context.go`
- Create: `internal/api/assistant_handler.go`
- Create: `frontend/src/pages/AssistantPage.tsx`
- Create: `frontend/src/components/ChatMessage.tsx`
- Create: `frontend/src/api/assistant.ts`

- [ ] **Step 1: Write context builder tests**

Test: build context within 4000 token budget, prioritize relevant logs based on keywords, trim conversation history to ~1000 tokens.

- [ ] **Step 2: Implement context builder**

Builds LLM context from:
- Server config summary (~500 tokens)
- Relevant recent logs (~2000 tokens, keyword-matched)
- Current health/backup status (~500 tokens)
- Conversation history (~1000 tokens, oldest dropped first)
Token estimation: ~4 chars per token.

- [ ] **Step 3: Implement assistant service**

- Configurable LLM provider (OpenAI or Anthropic API)
- Builds system prompt with BackupManager context
- Sends user message + context to LLM API
- Stores conversation in `llm_conversations` table
- Returns assistant response

- [ ] **Step 4: Add assistant API**

`POST /api/assistant/chat` — send message, get response.
`GET /api/assistant/conversations` — conversation history.
`DELETE /api/assistant/conversations` — clear history.

- [ ] **Step 5: Build AssistantPage**

Chat UI: message list (user/assistant), input field, send button. Auto-scroll. Loading indicator while LLM responds. Markdown rendering for assistant responses.

- [ ] **Step 6: Commit**

```bash
git commit -m "feat: AI assistant with context-aware LLM integration"
```

---

### Task 36: Documentation Page

**Files:**
- Create: `frontend/src/pages/DocsPage.tsx`
- Create: `frontend/src/components/DocsSidebar.tsx`
- Create: `frontend/src/components/DocViewer.tsx`
- Create: `docs/user-guide/` (multiple .md files)

- [ ] **Step 1: Write user-facing documentation**

Create markdown docs:
- `getting-started.md` — first setup, adding first server
- `servers.md` — managing servers, connection types
- `backups.md` — how backups work, incremental strategy
- `scheduling.md` — setting up schedules, retention policies
- `recovery.md` — disaster recovery procedures
- `notifications.md` — configuring Telegram and email
- `faq.md` — common questions and troubleshooting

- [ ] **Step 2: Add docs API**

`GET /api/docs` — list available docs (embedded in binary).
`GET /api/docs/{slug}` — get doc content as markdown.
`GET /api/docs/search?q=...` — full-text search across docs. Strategy: at startup, load all embedded markdown files into an in-memory index (simple case-insensitive substring matching on content + titles). Given the small corpus (~10 docs), this is sufficient and requires no external library.

- [ ] **Step 3: Build DocsPage**

Left sidebar with doc categories, right panel with rendered markdown. Full-text search bar. Table of contents generated from headings.

- [ ] **Step 4: Commit**

```bash
git commit -m "feat: documentation page with full-text search"
```

---

### Task 37: Settings UI + Audit Log UI

**Files:**
- Create: `frontend/src/pages/SettingsPage.tsx`
- Create: `frontend/src/pages/AuditLogPage.tsx`
- Create: `frontend/src/components/NotificationSettings.tsx`
- Create: `frontend/src/components/DestinationSettings.tsx`
- Create: `frontend/src/components/EncryptionSettings.tsx`
- Create: `frontend/src/components/UserManagement.tsx`

- [ ] **Step 1: Build SettingsPage**

Tabs:
- **Notifications**: Telegram bot token, chat ID, test send. SMTP host, port, user, password, test send. Per-event enable/disable toggles.
- **Destinations**: list destinations, add new, configure retention per destination.
- **Encryption**: enable/disable, generate key, download key file, re-display key (requires password).
- **Users**: list users, add user, change password, delete user.
- **General**: timezone, default retention policy, bandwidth limits.

- [ ] **Step 2: Build AuditLogPage**

Table: timestamp, user, action, target, IP. Filters: date range, user, action type. Pagination. CSV export button.

- [ ] **Step 3: Add settings API endpoints**

`GET /api/settings`, `PUT /api/settings`.
`GET /api/users`, `POST /api/users`, `PUT /api/users/{id}`, `DELETE /api/users/{id}`.
`POST /api/settings/encryption/generate-key`, `GET /api/settings/encryption/download-key`.

- [ ] **Step 4: Commit**

```bash
git commit -m "feat: settings UI and audit log UI"
```

---

### Task 38: Bandwidth Throttling + Pre-Flight Checks + Final Polish

**Files:**
- Modify: `internal/scheduler/scheduler.go`
- Modify: `internal/backup/orchestrator.go`
- Modify: `internal/sync/rsync.go`
- Modify: `internal/sync/ftp_sync.go`
- Create: `internal/backup/preflight.go`
- Create: `internal/backup/preflight_test.go`

- [ ] **Step 1: Write pre-flight check tests**

Test: skip backup if disk space insufficient (< 1.5x estimated size), skip if server unreachable, log skip reason.

- [ ] **Step 2: Implement pre-flight checks**

Before each backup job:
- Check local disk space vs estimated backup size × 1.5
- Check target server reachability
- Check no duplicate job running for same server
- If any fail: skip job, send alert, log reason

- [ ] **Step 3: Implement bandwidth throttling**

- rsync: pass `--bwlimit=X` flag (Kbytes/sec)
- FTP: application-level rate limiting on read loop using `golang.org/x/time/rate`
- Scheduler checks time-of-day bandwidth windows before starting jobs

- [ ] **Step 4: SQLite self-backup**

Daily background job: copy `backupmanager.db` to `data/db-backup/backupmanager_YYYY-MM-DD.db`. Keep last 7 copies.

- [ ] **Step 5: Run full test suite**

Run: `go test ./... -v -cover`
Expected: ALL PASS, >70% coverage on core packages

- [ ] **Step 6: Build final binary and test end-to-end**

```bash
cd frontend && npm run build && cd ..
go build -o bin/backupmanager ./cmd/server
./bin/backupmanager
```

Verify: login, add server, configure backup, run manual backup, check dashboard, verify notifications.

- [ ] **Step 7: Commit**

```bash
git commit -m "feat: bandwidth throttling, pre-flight checks, SQLite self-backup, final polish"
```

---

### Task 39: Startup Recovery + Crash Cleanup

**Files:**
- Create: `internal/backup/recovery.go`
- Create: `internal/backup/recovery_test.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Write startup recovery tests**

Test: `in_progress` backup runs are marked `failed` on startup, partial snapshot files on disk are cleaned up, `in_progress` destination syncs are reset to `pending`.

- [ ] **Step 2: Implement startup recovery**

`RecoverFromCrash(db, backupDir)`:
- Scan `backup_runs` for `status = 'running'` — update to `failed` with error "interrupted by application restart"
- For each interrupted run: check for partial files in backup directory, remove them
- Scan `destination_sync_status` for `status = 'in_progress'` — reset to `pending` for re-queue
- Log all recovery actions

- [ ] **Step 3: Wire into main.go startup**

Call `RecoverFromCrash()` after database migration, before starting scheduler.

- [ ] **Step 4: Add migration failure guard to main.go**

If `db.Migrate()` returns an error, log the error clearly and call `os.Exit(1)` — do not start the application with an un-migrated database.

- [ ] **Step 5: Run tests**

- [ ] **Step 6: Commit**

```bash
git commit -m "feat: startup crash recovery and migration failure guard"
```

---

## Documentation Tasks (parallel with implementation)

### Task 40: Technical Documentation (can run in parallel with Phase 4-6)

**Files:**
- Create: `docs/architecture.md`
- Create: `docs/web-layer.md`
- Create: `docs/core-layer.md`
- Create: `docs/infrastructure-layer.md`
- Create: `docs/api-reference.md`
- Create: `docs/database-schema.md`
- Create: `docs/deployment-guide.md`
- Create: `docs/development-guide.md`
- Create: `docs/disaster-recovery.md`

Each document should be self-contained and reference other docs where needed. Written so an AI agent or developer can understand the component in isolation.

- [ ] **Step 1: Write architecture.md** — high-level overview, layer diagram, data flow
- [ ] **Step 2: Write web-layer.md** — API patterns, auth flow, WebSocket protocol, frontend structure
- [ ] **Step 3: Write core-layer.md** — scheduler, orchestrator, retention, integrity, health checks
- [ ] **Step 4: Write infrastructure-layer.md** — connectors, sync engines, encryption, notifications
- [ ] **Step 5: Write api-reference.md** — all endpoints with request/response examples
- [ ] **Step 6: Write database-schema.md** — all tables with column descriptions, relationships, indexes
- [ ] **Step 7: Write deployment-guide.md** — installation on Ubuntu, systemd service, first-run setup
- [ ] **Step 8: Write development-guide.md** — dev setup, testing, building, contributing
- [ ] **Step 9: Write disaster-recovery.md** — what to do if the backup server itself fails
- [ ] **Step 10: Commit**

```bash
git commit -m "docs: complete technical documentation for all layers"
```
