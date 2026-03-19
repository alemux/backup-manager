# BackupManager

Internal backup management software for Linux Ubuntu and Windows physical servers. A single Go binary with an embedded React web interface that helps non-expert sysadmins configure, schedule, monitor, and restore incremental backups — with step-by-step guidance for every operation.

## What It Does

BackupManager connects to your servers (Linux via SSH, Windows via FTP), copies your data incrementally, orchestrates MySQL dumps, monitors server health, and notifies you via Telegram and email when something goes wrong. It also provides interactive disaster recovery playbooks so you know exactly what to do when things break.

### Key Features

- **Incremental Backups** — rsync for Linux (delta transfer), FTP with manifest-based change detection for Windows
- **MySQL Dump Orchestration** — connects via SSH, runs mysqldump, copies the dump, verifies checksum, cleans up old dumps on the remote server
- **Auto-Discovery** — scans Linux servers to find NGINX, MySQL, PM2, Certbot, Node.js, crontab, UFW automatically
- **Retention Policies** — configurable daily/weekly/monthly snapshot retention with automatic cleanup
- **Health Monitoring** — checks server reachability, disk space, service status every 5 minutes
- **Notifications** — Telegram bot + SMTP email with anti-flood protection
- **Disaster Recovery Playbooks** — interactive step-by-step guides with copy-paste commands
- **AI Assistant** — integrated LLM chat with context from your servers, logs, and backup status
- **Multi-Destination** — backup to local disk, NAS, USB, with independent sync status tracking
- **Encryption at Rest** — AES-256-GCM with Argon2 key wrapping
- **Audit Log** — tracks every action with user, timestamp, IP
- **Single Binary** — zero dependencies, just run `./backupmanager`

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    BackupManager (Go binary)                    │
│                                                                 │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐ │
│  │  Web Layer  │  │  Core Layer │  │  Infrastructure Layer   │ │
│  │             │  │             │  │                           │ │
│  │ React SPA   │  │ Scheduler   │  │ SSH Connector            │ │
│  │ (embedded)  │  │ Job Runner  │  │ FTP/SFTP Connector       │ │
│  │ REST API    │  │ Orchestrator│  │ MySQL Dump Engine        │ │
│  │ WebSocket   │  │ Retention   │  │ Rsync/Incremental Engine │ │
│  │ Auth+JWT    │  │ Integrity   │  │ Encryption (AES-256)     │ │
│  │             │  │ Health Check│  │ Notifier (TG + Email)    │ │
│  │             │  │ Audit Log   │  │ LLM Client               │ │
│  └─────────────┘  └─────────────┘  └─────────────────────────┘ │
│                                                                 │
│                    ┌──────────────┐                              │
│                    │   SQLite DB  │                              │
│                    └──────────────┘                              │
└─────────────────────────────────────────────────────────────────┘
```

**Stack:** Go 1.22+ | React 18 + TypeScript | Tailwind CSS | SQLite (WAL mode)

## Quick Start

### Prerequisites

- Go 1.22+ (`brew install go` on macOS, `sudo apt install golang` on Ubuntu)
- Node.js 18+ (for building the frontend)

### Build & Run

```bash
# Clone
git clone https://github.com/alemux/backup-manager.git
cd backup-manager

# Build frontend
cd frontend && npm ci && npm run build && cd ..

# Copy frontend assets for embedding
cp -r frontend/dist cmd/server/static

# Build the binary
go build -o backupmanager ./cmd/server

# Run
./backupmanager
```

Open `http://localhost:8080` in your browser. Default credentials: `admin` / `admin`.

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `BM_PORT` | `8080` | HTTP server port |
| `BM_DATA_DIR` | `./data` | Data directory (database, backups, logs) |
| `BM_JWT_SECRET` | auto-generated | JWT signing secret (persisted in DB) |
| `BM_LOG_LEVEL` | `info` | Log level |
| `BM_TIMEZONE` | `Local` | Timezone for retention calculations |

### Using the Makefile

```bash
make build        # Build frontend + Go binary
make run          # Run the server (dev mode)
make test         # Run all tests
make clean        # Remove binary and data
```

## Project Status

### Phase 1: Foundation ✅
- [x] Go project scaffold, config, Makefile
- [x] SQLite database with full 19-table schema
- [x] Authentication (bcrypt + JWT + auto-refresh)
- [x] Auth API (login, logout, password reset)
- [x] React frontend shell (login, layout, sidebar, routing)
- [x] Embedded frontend in single Go binary

### Phase 2: Server Management ✅
- [x] Server CRUD API + connection test
- [x] SSH connector (command execution + SFTP)
- [x] FTP connector (file transfer)
- [x] Auto-discovery service (NGINX, MySQL, PM2, Certbot, Node.js, Crontab, UFW)
- [x] Backup sources CRUD with dependency cycle detection
- [x] Servers UI with Add Server wizard (Linux 6-step / Windows 4-step)

### Phase 3: Backup Engine ✅
- [x] Incremental sync (rsync via SSH with bandwidth limiting)
- [x] FTP incremental sync with manifest-based change detection + rate limiting
- [x] MySQL dump orchestrator (remote execution, checksum verification, cleanup)
- [x] Job runner + orchestrator with dependency graph (topological sort)
- [x] Cron scheduler with missed backup detection
- [x] Retention policy engine (daily/weekly/monthly, timezone-aware)
- [x] Jobs API (CRUD, manual trigger, runs history with pagination)
- [x] Jobs UI (schedule selector, run history, manual trigger)

### Phase 4: Monitoring & Notifications ✅
- [x] Health check system (Linux SSH checks + Windows FTP checks, configurable thresholds)
- [x] Telegram notifier with anti-flood protection
- [x] Email notifier (SMTP) with HTML templates
- [x] Notification manager (central dispatcher, config API, test send)
- [x] WebSocket real-time updates (hub with auto-reconnect)
- [x] Dashboard UI (live status, backup timeline, disk usage chart, alerts)

### Phase 5: Security & Integrity ✅
- [x] AES-256-GCM encryption at rest with Argon2id key wrapping
- [x] Backup integrity verification (checksum + SQL dump validation)
- [x] Audit log with filtering, pagination, CSV export, middleware
- [x] CSRF protection (double-submit cookie) + login rate limiting (SQLite-persisted)
- [x] Credential encryption in database (server passwords, SSH keys)

### Phase 6: Advanced Features (next)
- [ ] Multi-destination sync
- [ ] Snapshots UI with calendar
- [ ] Disaster recovery playbooks
- [ ] AI assistant
- [ ] Documentation page
- [ ] Bandwidth throttling
- [ ] Startup crash recovery

## Documentation

Detailed technical documentation is available in the `docs/` directory:

| Document | Description |
|----------|-------------|
| [Design Specification](docs/superpowers/specs/2026-03-18-backupmanager-design.md) | Complete system design — architecture, backup engine, frontend, security, health checks, notifications, auto-discovery, AI assistant, multi-destination |
| [Implementation Plan](docs/superpowers/plans/2026-03-18-backupmanager-plan.md) | 40-task implementation plan divided into 6 phases, with TDD approach and exact file paths |

## Project Structure

```
backup-manager/
├── cmd/server/main.go          # Entry point (embeds frontend)
├── internal/
│   ├── api/                    # REST API handlers + router
│   ├── auth/                   # JWT, bcrypt, middleware, password reset
│   ├── config/                 # Environment-based configuration
│   ├── database/               # SQLite connection, migrations, schema
│   └── setup/                  # First-run admin user + data dirs
├── frontend/                   # React 18 + TypeScript + Tailwind
│   ├── src/
│   │   ├── components/         # Layout, Sidebar
│   │   ├── pages/              # Login, Dashboard, placeholders
│   │   ├── hooks/              # useAuth
│   │   ├── api/                # API client
│   │   └── types/              # TypeScript types
│   └── package.json
├── docs/                       # Design spec + implementation plan
├── Makefile
└── go.mod
```

## Development

```bash
# Run Go backend (dev mode)
make run

# Run React frontend dev server (with hot reload, proxies to :8080)
cd frontend && npm run dev

# Run tests
make test
```

## Target Hardware

Designed for a dedicated backup machine:
- CPU: Intel i5 9th gen or equivalent
- RAM: 32GB (Go uses very little — most is for the OS cache)
- Storage: as much as your backups need
- OS: Ubuntu Linux (recommended)

## License

Internal use only.
