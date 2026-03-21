# BackupManager

Internal backup management software for Linux Ubuntu and Windows physical servers. A single Go binary with an embedded React web interface that helps non-expert sysadmins configure, schedule, monitor, and restore incremental backups — with step-by-step guidance for every operation.

## What It Does

BackupManager connects to your servers (Linux via SSH, Windows via FTP), copies your data incrementally, orchestrates MySQL dumps, monitors server health, and notifies you via Telegram and email when something goes wrong. It also provides interactive disaster recovery playbooks so you know exactly what to do when things break.

### Key Features

- **Incremental Backups** — rsync to a `current/` directory (only changed bytes transferred), then hard-link snapshots for retention (multiple snapshots share disk space for unchanged files)
- **Smart Excludes** — automatically skips `node_modules`, `.git`, `__pycache__`, `vendor`, `dist`, `build`, `.env`, `*.log` etc. Configurable globally and per-source
- **MySQL Dump Orchestration** — connects via SSH, runs mysqldump, copies the dump, verifies checksum, cleans up old dumps on the remote server
- **Auto-Discovery** — scans Linux servers to find NGINX, MySQL, Redis, PM2, Certbot, Node.js, crontab, UFW automatically
- **Periodic Rescan** — every 24h re-scans servers, detects changes (new databases, removed vhosts, new services), notifies you, auto-regenerates recovery playbooks
- **Retention Policies** — configurable daily/weekly/monthly snapshot retention with automatic cleanup, timezone-aware
- **Health Monitoring** — checks server reachability, disk space, service status (NGINX, MySQL, Redis, PM2) every 5 minutes
- **Notifications** — Telegram bot + SMTP email with anti-flood protection. Notifies on backup success/failure, server issues, discovery changes
- **Disaster Recovery Playbooks** — auto-generated interactive step-by-step guides with copy-paste commands. Auto-updated when server configuration changes
- **AI Assistant** — integrated LLM chat (OpenAI/Anthropic) with context from your servers, logs, and backup status
- **Multi-Destination** — primary destination (where backups are saved) + secondary destinations (NAS, USB, cloud) with automatic replication and sync status tracking
- **Encryption at Rest** — AES-256-GCM with Argon2 key wrapping for backup files and server credentials
- **Audit Log** — tracks every action with user, timestamp, IP. CSV export
- **Single Binary** — zero dependencies, just run `./backupmanager`

## How Backups Work

```
Server remoto                    BackupManager
                                 ┌─────────────────────────────┐
/var/www/project/ ──rsync──────► │ Primary Destination         │
/etc/nginx/       (incremental)  │ /backups/server/current/    │
/root/.pm2/                      │         │                   │
/etc/letsencrypt/                │         ▼ hard-link snapshot│
                                 │ /backups/server/2026-03-21/ │
                                 │ /backups/server/2026-03-20/ │
                                 │         │                   │
                                 │         ▼ auto-sync         │
                                 │ Secondary Destinations      │
                                 │ (NAS, USB, cloud)           │
                                 └─────────────────────────────┘
```

**Incremental:** rsync transfers only changed bytes to `current/`. After sync, a timestamped snapshot is created using hard links — unchanged files share disk blocks, so 10 snapshots of mostly-identical data use barely more space than 1 copy.

**Default excludes** (configurable globally and per-source):
`node_modules`, `.git`, `__pycache__`, `.cache`, `.npm`, `.next`, `vendor`, `dist`, `build`, `.env`, `*.log`, `*.tmp`, `*.swp`

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
│  │ CSRF        │  │ Health Check│  │ Notifier (TG + Email)    │ │
│  │ Rate Limit  │  │ Audit Log   │  │ LLM Client               │ │
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
- `sshpass` for SSH password authentication (`brew install sshpass` / `sudo apt install sshpass`)
- `rsync` (pre-installed on most Linux/macOS systems)

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

**Important:** Change the default password after first login.

### First-Time Setup

1. **Login** with `admin` / `admin`
2. **Settings > Destinations** — configure your Primary Destination (where backups are saved) and optionally secondary destinations (NAS, USB)
3. **Settings > Notifications** — add Telegram bot token + chat ID, SMTP config. Enable per-event notifications. Click "Save Changes"
4. **Servers > Add Server** — add your Linux (SSH) or Windows (FTP) servers
5. **Jobs > Create Job** — schedule backups with cron expressions
6. **Jobs > Run Now** — test your first backup

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `BM_PORT` | `8080` | HTTP server port |
| `BM_DATA_DIR` | `./data` | Data directory (database, logs) |
| `BM_JWT_SECRET` | auto-generated | JWT signing secret (persisted in DB) |
| `BM_LOG_LEVEL` | `info` | Log level |
| `BM_TIMEZONE` | `Local` | Timezone for retention calculations |

**Note:** Backup storage location is configured in Settings > Destinations, not via environment variables.

### Using the Makefile

```bash
make build        # Build frontend + embed + Go binary
make run          # Run the server (dev mode)
make test         # Run all tests
make clean        # Remove binary and data
```

## Pages & Features

| Page | What it does |
|------|-------------|
| **Dashboard** | Server status, recent backups, disk usage chart, active alerts, live WebSocket updates |
| **Servers** | Add/manage servers, auto-discovery, rescan with change detection, manage backup sources |
| **Jobs** | Create/schedule backup jobs, manual trigger, run history with status |
| **Snapshots** | Calendar view, filter by server/type/date, download snapshots |
| **Recovery** | Auto-generated disaster recovery playbooks with interactive step-by-step wizard |
| **Docs** | Built-in documentation with full-text search |
| **Assistant** | AI chat (OpenAI/Anthropic) with server/backup context |
| **Settings** | Notifications (Telegram/Email), destinations (primary/secondary), encryption, users, global excludes |
| **Audit Log** | Action history with filters and CSV export |

## Auto-Discovery

When you add a Linux server, BackupManager scans for installed services:

| Service | What it detects |
|---------|----------------|
| **NGINX** | Version, vhosts, root paths |
| **MySQL** | Version, database list |
| **Redis** | Version, status, RDB dump path, database count |
| **PM2** | Processes, paths, status |
| **Certbot** | Certificates, domains, expiry dates |
| **Node.js** | Version |
| **Crontab** | Scheduled jobs |
| **UFW** | Firewall status and rules |

Auto-rescan runs every 24 hours. If changes are detected (new database, removed vhost, new PM2 process), you get a notification and recovery playbooks are auto-regenerated.

## Security

- **Authentication:** bcrypt password hashing, JWT in httpOnly cookies with SameSite=Strict
- **CSRF:** double-submit cookie pattern on all state-changing requests
- **Rate limiting:** 5 login attempts per 5 minutes, 15-minute block (SQLite-persisted)
- **Credential encryption:** server passwords and SSH keys encrypted with AES-256 in the database
- **Backup encryption:** optional AES-256-GCM file encryption with Argon2id key wrapping
- **Audit trail:** every action logged with user, timestamp, IP address

## Project Structure

```
backup-manager/
├── cmd/server/main.go              # Entry point (embeds frontend)
├── internal/
│   ├── api/                        # REST API handlers + router (50+ endpoints)
│   ├── assistant/                  # AI assistant with context builder
│   ├── audit/                      # Audit logging service + middleware
│   ├── auth/                       # JWT, bcrypt, middleware, password reset
│   ├── backup/                     # Orchestrator, runner, MySQL dump, preflight, recovery
│   ├── config/                     # Environment-based configuration
│   ├── connector/                  # SSH and FTP connectors
│   ├── database/                   # SQLite connection, migrations, credential encryption
│   ├── discovery/                  # Auto-discovery, rescan, change detection
│   ├── docs/                       # Embedded user documentation (markdown)
│   ├── encryption/                 # AES-256-GCM file encryption, key management
│   ├── health/                     # Proactive health checks
│   ├── integrity/                  # Backup integrity verification
│   ├── notification/               # Telegram, Email, notification manager
│   ├── recovery/                   # Disaster recovery playbook generator
│   ├── retention/                  # Retention policy engine
│   ├── scheduler/                  # Cron scheduler with missed backup detection
│   ├── setup/                      # First-run admin user + data dirs
│   ├── sync/                       # rsync, FTP sync, manifest, multi-destination
│   └── websocket/                  # Real-time WebSocket hub
├── frontend/                       # React 18 + TypeScript + Tailwind
│   ├── src/
│   │   ├── components/             # Layout, Sidebar, AddServerWizard, JobCard, etc.
│   │   ├── pages/                  # Dashboard, Servers, Jobs, Snapshots, Recovery, etc.
│   │   ├── hooks/                  # useAuth, useWebSocket
│   │   ├── api/                    # Typed API clients (servers, jobs, snapshots, etc.)
│   │   └── types/                  # TypeScript interfaces
│   └── package.json
├── docs/                           # Technical documentation (9 documents)
│   ├── architecture.md
│   ├── web-layer.md
│   ├── core-layer.md
│   ├── infrastructure-layer.md
│   ├── api-reference.md
│   ├── database-schema.md
│   ├── deployment-guide.md
│   ├── development-guide.md
│   └── disaster-recovery.md
├── Makefile
└── go.mod
```

## Development

```bash
# Run Go backend (dev mode)
make run

# Run React frontend dev server (with hot reload, proxies to :8080)
cd frontend && npm run dev

# Run all backend tests
make test

# Run tests for a specific package
go test github.com/backupmanager/backupmanager/internal/backup/ -v
```

## Deployment on Ubuntu

```bash
# Install dependencies
sudo apt update && sudo apt install -y golang nodejs npm rsync sshpass

# Build
cd backup-manager
cd frontend && npm ci && npm run build && cd ..
cp -r frontend/dist cmd/server/static
go build -o backupmanager ./cmd/server

# Create systemd service
sudo tee /etc/systemd/system/backupmanager.service << 'EOF'
[Unit]
Description=BackupManager
After=network.target

[Service]
Type=simple
User=backupmanager
WorkingDirectory=/opt/backupmanager
ExecStart=/opt/backupmanager/backupmanager
Environment=BM_PORT=8080
Environment=BM_DATA_DIR=/opt/backupmanager/data
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# Install and start
sudo mkdir -p /opt/backupmanager
sudo cp backupmanager /opt/backupmanager/
sudo useradd -r -s /bin/false backupmanager
sudo chown -R backupmanager:backupmanager /opt/backupmanager
sudo systemctl daemon-reload
sudo systemctl enable backupmanager
sudo systemctl start backupmanager
```

## Target Hardware

Designed for a dedicated backup machine:
- **CPU:** Intel i5 9th gen or equivalent (any modern CPU works)
- **RAM:** 32GB recommended (Go uses very little — most is for OS cache)
- **Storage:** as much as your backups need
- **OS:** Ubuntu Linux 22.04+ recommended
- **Network:** LAN access to servers being backed up

## Documentation

Technical documentation is available in the `docs/` directory:

| Document | Description |
|----------|-------------|
| [Design Specification](docs/superpowers/specs/2026-03-18-backupmanager-design.md) | Complete system design (12 sections) |
| [Implementation Plan](docs/superpowers/plans/2026-03-18-backupmanager-plan.md) | 40-task implementation plan (6 phases) |
| [Architecture](docs/architecture.md) | System architecture and data flow |
| [API Reference](docs/api-reference.md) | All 50+ REST endpoints |
| [Database Schema](docs/database-schema.md) | All tables and relationships |
| [Deployment Guide](docs/deployment-guide.md) | Production installation |
| [Development Guide](docs/development-guide.md) | Dev setup and contributing |
| [Disaster Recovery](docs/disaster-recovery.md) | What to do if the backup server fails |

## License

Internal use only.
