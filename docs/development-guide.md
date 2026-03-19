# BackupManager — Development Guide

---

## Prerequisites

| Tool | Version | Install |
|---|---|---|
| Go | 1.21+ | https://go.dev/dl/ |
| Node.js | 20+ | https://nodejs.org |
| npm | 9+ | Bundled with Node |
| rsync | any | `apt install rsync` |
| gcc | any | Required for `mattn/go-sqlite3` CGo build (`apt install build-essential`) |

---

## Dev Environment Setup

### 1. Clone the repository

```bash
git clone <repo-url> server_backup_manager
cd server_backup_manager
```

### 2. Install Go dependencies

```bash
go mod download
```

### 3. Install frontend dependencies

```bash
cd frontend
npm ci
cd ..
```

---

## Running in Development

### Backend only (embedded static files from last build)

```bash
make run
# or
go run ./cmd/server
```

The server starts on `http://localhost:8080`. The embedded frontend is whatever was last built into `cmd/server/static/`.

### Frontend dev server with hot reload

In one terminal:
```bash
make run
```

In another terminal:
```bash
cd frontend
npm run dev
```

Vite starts on `http://localhost:5173` and proxies `/api/` and `/ws/` to the Go server. The proxy is configured in `frontend/vite.config.ts`.

### Full production build

```bash
make build
# Produces: bin/backupmanager
./bin/backupmanager
```

The build sequence:
1. `npm ci && npm run build` — TypeScript compilation + Vite bundle → `frontend/dist/`
2. `cp -r frontend/dist cmd/server/static` — copies to embed target
3. `go build -o bin/backupmanager ./cmd/server` — compiles Go + embeds static files

---

## Running Tests

### All tests

```bash
make test
# Equivalent to: go test ./cmd/... ./internal/... -v -cover
```

### Short tests (skip integration tests)

```bash
make test-short
# Equivalent to: go test ./cmd/... ./internal/... -short -v
```

Integration tests (SSH, FTP) check `testing.Short()` and skip themselves when the `-short` flag is set.

### Single package

```bash
go test ./internal/backup/... -v
go test ./internal/retention/... -v -run TestApply
```

### Coverage report

```bash
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

---

## Project Structure for Contributors

```
internal/
├── api/           HTTP layer — add new endpoints here
├── backup/        Backup execution pipeline
│   ├── orchestrator.go    Main backup pipeline
│   ├── runner.go          Thin wrapper called by scheduler
│   ├── mysql_dump.go      MySQL-specific dump logic
│   ├── preflight.go       Pre-run environment checks
│   └── recovery.go        Crash recovery on startup
├── scheduler/     Cron-based job scheduling
├── retention/     Snapshot pruning algorithm
├── health/        Server health checks
├── notification/  Telegram + email dispatching
├── assistant/     LLM chat integration
├── audit/         Audit log recording + query
├── connector/     SSH and FTP remote connections
├── sync/          Rsync and FTP file sync engines
├── encryption/    AES-256-GCM file encryption
├── database/      SQLite open + migrate + credential crypto
├── integrity/     SHA-256 snapshot verification
├── recovery/      Recovery playbook generation
├── config/        Environment variable loading
└── setup/         First-run setup utilities
```

---

## Code Conventions

### Go

- Standard `gofmt` formatting (run `gofmt -w .` or use an editor plugin).
- All exported types and functions have doc comments.
- Error values are wrapped with context: `fmt.Errorf("doing X: %w", err)`.
- HTTP handlers: parse request → validate → call service → return `JSON()` or `Error()`.
- No global mutable state outside of the `Scheduler`'s cron entries map (protected by `sync.Mutex`).
- Test files end in `_test.go`. Tests use `testing.T` directly (no test framework).
- Integration tests guard with `if testing.Short() { t.Skip(...) }`.

### TypeScript / React

- Components are functional with hooks.
- Server state is managed via TanStack Query (`useQuery`, `useMutation`).
- Local UI state uses `useState` / `useReducer`.
- No inline styles — Tailwind utility classes only.
- API calls live in `frontend/src/api/` (one file per resource).

---

## Adding a New Feature

### New API endpoint

1. Add a handler function in the appropriate `internal/api/<resource>_handler.go` file, or create a new `<resource>_handler.go`.
2. Register the route in `internal/api/router.go` — protected routes go in the `protected` mux and are then mounted on the outer `mux` with `authSvc.RequireAuth(protected)`.
3. Write a test in `internal/api/<resource>_handler_test.go`.

Example skeleton:

```go
// internal/api/widgets_handler.go
type WidgetsHandler struct { db *database.Database }

func NewWidgetsHandler(db *database.Database) *WidgetsHandler { ... }

func (h *WidgetsHandler) List(w http.ResponseWriter, r *http.Request) {
    // query DB, call JSON(w, http.StatusOK, result)
}
```

```go
// internal/api/router.go
widgetsHandler := NewWidgetsHandler(db)
protected.HandleFunc("GET /api/widgets", widgetsHandler.List)
mux.Handle("/api/widgets", authSvc.RequireAuth(protected))
```

### New database table

1. Add `CREATE TABLE IF NOT EXISTS ...` to `internal/database/migrations/001_initial_schema.sql` (for the current schema) or create `002_add_widgets.sql` for incremental migrations.
2. Add any necessary indexes.
3. Run `make test` to verify migrations apply cleanly.

### New background service

1. Implement business logic in a new package under `internal/`.
2. Wire it up in `cmd/server/main.go` — create, configure, and start it after `db.Migrate()`.
3. Add a stop call in the graceful shutdown block if needed.

### New frontend page

1. Create `frontend/src/pages/WidgetsPage.tsx`.
2. Add the API client in `frontend/src/api/widgets.ts`.
3. Register the route in `frontend/src/App.tsx`.
4. Add a navigation link in `frontend/src/components/Sidebar.tsx`.
5. Add TypeScript types in `frontend/src/types/index.ts`.

---

## TDD Workflow

BackupManager's core packages are designed to be testable in isolation.

### Writing a test

```go
func TestTopologicalSort_WithCycle(t *testing.T) {
    sources := []BackupSourceRecord{
        {ID: 1, DependsOn: &two},
        {ID: 2, DependsOn: &one},
    }
    _, err := TopologicalSort(sources)
    if err == nil {
        t.Fatal("expected cycle error, got nil")
    }
}
```

### Test patterns

**In-memory SQLite database** (no file I/O):
```go
db, err := database.Open(":memory:")
```

**Mock Syncer** (avoid actual rsync):
```go
type mockSyncer struct { result *sync.SyncResult; err error }
func (m *mockSyncer) Sync(...) (*sync.SyncResult, error) { return m.result, m.err }

orch := NewOrchestrator(db)
orch.SetRsyncSyncer(&mockSyncer{result: &sync.SyncResult{FilesCopied: 3}})
orch.SetSkipPreflight(true)
```

**HTTP handler tests** use `httptest.NewRecorder()` and `httptest.NewRequest()`:
```go
w := httptest.NewRecorder()
r := httptest.NewRequest("GET", "/api/jobs", nil)
handler.List(w, r)
if w.Code != http.StatusOK { t.Fatalf("expected 200, got %d", w.Code) }
```

### Running specific tests

```bash
# Run tests matching a pattern
go test ./internal/backup/... -run TestOrchestrator -v

# Run with race detector
go test -race ./internal/...
```

---

## Dependency Management

```bash
# Add a new dependency
go get github.com/example/pkg@v1.2.3
go mod tidy

# Update all dependencies
go get -u ./...
go mod tidy
```

The `go.sum` file must be committed alongside `go.mod`.

---

## Linting

```bash
# Frontend
cd frontend && npm run lint

# Go (requires golangci-lint)
golangci-lint run ./...
```
