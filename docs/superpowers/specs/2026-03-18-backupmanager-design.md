# BackupManager — Design Specification

## Overview

BackupManager is an internal company tool for managing incremental backups of Linux Ubuntu and Windows physical servers onto a dedicated local backup machine. It provides a professional web interface for configuration, monitoring, scheduling, disaster recovery guidance, and an AI assistant for operational support.

**Stack:** Go monolith (single binary) + React SPA (embedded) + SQLite

**Target users:** Non-expert sysadmins who need step-by-step guidance for backup setup and disaster recovery.

---

## 1. Architecture

### 1.1 High-Level Architecture

Single Go binary containing three layers:

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
         │                    │                      │
         ▼                    ▼                      ▼
   ┌──────────┐        ┌──────────┐           ┌──────────┐
   │ Disco 1  │───────►│   NAS    │───────►   │ Dest. N  │
   │ (locale) │  sync  │ (rete)   │  sync     │ (futuro) │
   └──────────┘        └──────────┘           └──────────┘
```

### 1.2 Layer Responsibilities

| Layer | Responsibility |
|-------|---------------|
| **Web Layer** | UI serving, REST API, WebSocket real-time streaming, JWT authentication, password recovery |
| **Core Layer** | Business logic: scheduling, backup orchestration, retention policy, integrity verification, health monitoring, audit logging, dependency graph |
| **Infrastructure Layer** | Remote connections (SSH/FTP/SFTP), MySQL dump execution, incremental file sync, AES-256 encryption, Telegram/Email notifications, LLM API client |

### 1.3 Database (SQLite)

Tables:
- `users` — user accounts with bcrypt-hashed passwords
- `servers` — registered servers (type: linux/windows, connection: SSH/FTP, credentials encrypted with app-level key)
- `backup_sources` — what to copy from each server (type: web/db/config, paths, dependencies)
- `backup_jobs` — scheduling configuration, bandwidth limits, retention policy
- `backup_runs` — execution history (status, duration, size, errors)
- `backup_snapshots` — versioned file snapshots with checksum, retention expiry
- `destinations` — backup destinations (local disk, NAS, USB, future S3)
- `destination_sync_status` — per-snapshot sync status for each destination
- `health_checks` — server health check results history
- `health_check_config` — thresholds and check intervals per server
- `audit_log` — all user actions with timestamp, user, action, target, IP
- `notifications_config` — notification channels and preferences
- `notifications_log` — sent notification history
- `recovery_playbooks` — disaster recovery procedures per server/service
- `discovery_results` — auto-discovery scan results cache
- `llm_conversations` — AI assistant conversation history
- `settings` — global application settings

### 1.4 Project Structure

```
backup-manager/
├── cmd/server/main.go          # Entry point
├── internal/
│   ├── api/                    # REST API handlers + routes
│   │   ├── router.go
│   │   ├── middleware.go
│   │   ├── servers.go
│   │   ├── jobs.go
│   │   ├── snapshots.go
│   │   ├── recovery.go
│   │   ├── settings.go
│   │   ├── health.go
│   │   ├── audit.go
│   │   └── assistant.go
│   ├── auth/                   # JWT, login, password recovery
│   ├── backup/                 # Orchestrator, job runner, dependency graph
│   ├── connector/              # SSH, FTP/SFTP connectors (interface-based)
│   ├── database/               # SQLite migrations, queries, models
│   ├── discovery/              # Auto-discovery of services on remote servers
│   ├── encryption/             # AES-256-GCM at rest
│   ├── health/                 # Proactive health checks
│   ├── integrity/              # Checksum verification, test restore
│   ├── notification/           # Telegram bot, SMTP email
│   ├── retention/              # Retention policy engine + cleanup
│   ├── scheduler/              # Cron-like scheduler with bandwidth awareness
│   ├── sync/                   # Incremental sync engine (rsync, FTP manifest)
│   └── websocket/              # Real-time log streaming
├── frontend/                   # React app
│   ├── src/
│   │   ├── components/         # Reusable UI components
│   │   ├── pages/              # Dashboard, Servers, Jobs, Snapshots, Recovery, Docs, Assistant, Settings, AuditLog
│   │   ├── hooks/              # Custom React hooks
│   │   ├── api/                # API client functions
│   │   ├── types/              # TypeScript types
│   │   └── utils/              # Utilities
│   ├── public/
│   └── package.json
├── docs/                       # Technical documentation (modular)
│   ├── architecture.md
│   ├── web-layer.md
│   ├── core-layer.md
│   ├── infrastructure-layer.md
│   ├── api-reference.md
│   ├── database-schema.md
│   ├── deployment-guide.md
│   ├── disaster-recovery.md
│   └── development-guide.md
├── migrations/                 # SQL migration files
├── configs/                    # Example configuration files
├── go.mod
├── go.sum
└── Makefile
```

---

## 2. Backup Engine

### 2.1 Incremental Strategy

| Source Type | Method | Detail |
|-------------|--------|--------|
| Web files (Linux) | rsync via SSH | Delta transfer — only changed bytes within files |
| Web files (Windows) | FTP + local manifest | Manifest tracks path + size + SHA256. SHA256 is the authoritative change detector; mtime used only as optimization hint (skip hash if mtime unchanged). If mtime unavailable from FTP server, hash is always computed |
| MySQL dump | Full dump + deduplication | Always full dump; if hash matches previous, hard link instead of new copy |
| Linux config | rsync via SSH | Same delta transfer logic |
| SQLite files | File copy + checksum | Copy only if file hash changed |

### 2.2 MySQL Dump Orchestration

Full flow:
1. BackupManager connects via SSH to remote server
2. Executes: `mysqldump --single-transaction --routines --triggers dbname | gzip > /var/backups/backupmanager/dbname_YYYY-MM-DD.sql.gz`
3. Waits for completion (timeout: configurable, default 30 minutes per database)
4. Calculates remote checksum: `sha256sum` on the dump file
5. Copies dump via SCP/SFTP
6. Verifies local checksum matches remote
7. If OK: registers snapshot, applies retention policy
8. Remote cleanup: deletes dumps older than configured retention days on remote server (configurable per server, default 3 days)
9. Sends notification (success/failure)

The setup wizard provides exact commands for:
- Creating a dedicated MySQL backup user with minimal read-only permissions
- Creating the `/var/backups/backupmanager/` directory with correct ownership and `0700` permissions
- Testing connection and first manual dump

### 2.3 Retention Policy

Configurable per server and per backup type. Default "Standard" policy:
- Last 7 days: keep every daily snapshot
- Last 4 weeks: keep 1 per week (Sunday)
- Last 3 months: keep 1 per month (1st)

Automatic cleanup removes expired snapshots. Dashboard shows occupied space and growth projection.

Each destination can have its own independent retention policy.

All retention calculations use the **backup server's local timezone** (configurable in Settings, defaults to system timezone). Day boundaries for "keep Sunday" and "keep 1st of month" use this timezone.

### 2.4 Backup Dependency Graph

Backup sources can declare dependencies. The orchestrator respects execution order:
1. MySQL dumps (Priority 1 — database first)
2. Web files (Priority 2 — then files)
3. Configuration files (Priority 3 — then configs)

All sources within a dependency group share the same snapshot timestamp. Note: true transactional point-in-time consistency is only guaranteed for MySQL dumps (via `--single-transaction`). File-level backups provide best-effort consistency — files modified during transfer may reflect a slightly different point in time. For most use cases (web projects), this is acceptable since the database holds the authoritative state.

---

## 3. Frontend (React SPA)

### 3.1 Page Structure

| Page | Purpose |
|------|---------|
| **Dashboard** | Overview: server status, last backup results, next scheduled, disk usage, growth trend, active alerts |
| **Servers** | Server list with health status, detail view with discovered services, configured sources, backup history. "Add server" wizard |
| **Jobs** | All backup jobs with scheduling, last/next run, avg duration, outcome. Manual trigger, visual cron config, bandwidth throttling |
| **Snapshots** | Calendar navigation, filter by server/type/source, per-snapshot details (size, checksum, integrity), download/restore/compare actions, retention indicator |
| **Recovery** | Disaster recovery playbooks per scenario, interactive wizard with numbered steps, copy-paste commands, verification checks, completion checkboxes |
| **Docs** | Categorized documentation, full-text search, how-to guides, FAQ, troubleshooting |
| **Assistant** | AI chat with automatic context (server config, recent logs, health status), conversation history |
| **Settings** | Notifications (Telegram/SMTP), retention defaults, bandwidth limits, destinations, users, encryption, test notifications |
| **Audit Log** | Chronological action table, filters by user/action/date, CSV export |

### 3.2 Add Server Wizard Flow

#### Linux Server Flow (6 steps):
1. **Server type** — Linux Ubuntu (SSH)
2. **Connection** — Host, SSH port, credentials (password or SSH key), connection test
3. **Auto-Discovery** — Scan and display found services (NGINX, MySQL, PM2, Certbot)
4. **Source selection** — Choose what to back up (web files, databases, configs)
5. **MySQL setup** (if databases selected) — Step-by-step commands with copy buttons for creating backup user
6. **Scheduling** — Frequency, time, retention policy, first full backup trigger

#### Windows Server Flow (4 steps):
1. **Server type** — Windows (FTP)
2. **Connection** — Host, FTP port, credentials, connection test
3. **Source selection** — Manually specify FTP paths to back up (web files, database dump locations)
4. **Scheduling** — Frequency, time, retention policy, first full backup trigger

### 3.3 Tech Stack (Frontend)

- React 18+ with TypeScript
- Tailwind CSS for styling
- Shadcn/UI component library
- React Query for API state management
- React Router for navigation
- Recharts for dashboard graphs
- WebSocket client for real-time updates

---

## 4. Health Check System

### 4.1 Checks Performed — Linux Servers (every 5 minutes, configurable)

| Check | Method | Alert Threshold |
|-------|--------|----------------|
| Reachability | TCP connect on SSH port | Timeout > 5s |
| SSH service | SSH handshake | Connection refused |
| Disk space | `df -h` via SSH | < 10% free |
| NGINX status | `systemctl status nginx` via SSH | inactive |
| MySQL status | `mysqladmin ping` via SSH | not running |
| PM2 processes | `pm2 jlist` via SSH | any stopped |
| CPU load | `uptime` via SSH | > 90% for 5min |
| RAM available | `free -m` via SSH | < 10% free |

### 4.1.1 Checks Performed — Windows Servers (FTP only)

| Check | Method | Alert Threshold |
|-------|--------|----------------|
| Reachability | TCP connect on FTP port | Timeout > 5s |
| FTP service | FTP handshake + login | Connection refused or auth failed |

Note: Windows servers accessed via FTP have limited health monitoring (no OS-level checks). Only connectivity and FTP service availability are checked.

### 4.2 Status Levels

- **OK** (green) — all checks passing
- **Warning** (yellow) — approaching thresholds (e.g., disk at 15%)
- **Critical** (red) — action required (server down, service dead)

Thresholds configurable per check per server. Health check history stored for trend analysis.

---

## 5. Notifications

### 5.1 Notification Matrix

| Event | Telegram | Email |
|-------|----------|-------|
| Backup success | Optional | Optional |
| Backup failed | Yes | Yes |
| Server unreachable | Yes | Yes |
| Service down | Yes | Yes |
| Disk space low | Yes | Yes |
| Integrity check failed | Yes | Yes |
| Missed backup | Yes | Yes |
| Recovery playbook activated | Yes | No (intentional: Telegram is faster for urgent recovery situations) |
| User login | No | Yes |

### 5.2 Anti-Flood

If a server is down, one alert is sent, then reminders every 30 minutes until resolved. No notification storm.

Each notification type is independently configurable: enable/disable, Telegram chat/group, multiple email recipients.

---

## 6. Security

### 6.1 Authentication

- Bcrypt password hashing
- JWT tokens in httpOnly cookies with `SameSite=Strict` policy (CSRF protection), 24h expiry, automatic refresh
- CSRF token required for all state-changing API requests (double-submit cookie pattern)
- Password recovery via email with time-limited token (1h)
- Rate limiting: max 5 login attempts per 5 minutes, IP blocked for 15 minutes after

### 6.2 Encryption at Rest

- AES-256-GCM encryption for all stored backups
- Master key generated on first setup, displayed and downloadable as a `.key` file
- Master key is also stored encrypted in SQLite (wrapped with a key derived from the admin password via Argon2) as a recovery fallback. Note: this recovery path requires the admin password — if both the key file AND the admin password are lost, encrypted backups are unrecoverable
- The Settings page allows re-displaying and re-downloading the master key (requires admin password confirmation)
- Encryption optional at setup — can be enabled later
- When enabling encryption on existing backups: only new snapshots are encrypted; a background job can optionally re-encrypt existing snapshots; the UI clearly marks encrypted vs unencrypted snapshots
- File format: `original → gzip compress → AES-256-GCM encrypt → .snapshot.enc`
- Restoration requires master key

### 6.3 Audit Log

Every action tracked with: timestamp, user, action type, target, IP address, details.

Tracked actions: login/logout, config changes, snapshot downloads, manual backup triggers, user management, recovery playbook activation.

---

## 7. Auto-Discovery

### 7.1 Linux Server Discovery

On SSH connection, BackupManager scans for:

| Service | Detection | Extracted Info |
|---------|-----------|----------------|
| NGINX | `which nginx` + `systemctl status` | Vhosts from `/etc/nginx/sites-enabled/`, root paths |
| MySQL | `which mysql` + `systemctl status` | Database list (`SHOW DATABASES`), sizes |
| PM2 | `which pm2` + `pm2 jlist` | Process names, paths, status, ecosystem files |
| Certbot | `which certbot` + `certbot certificates` | Domains, expiry dates, cert paths |
| Node.js | `which node` + `node -v` | Installed version |
| Crontab | `crontab -l` | Scheduled jobs (for documentation) |
| UFW | `ufw status` | Active firewall rules (for documentation) |

### 7.2 Windows Servers

No auto-discovery — manual FTP path configuration only.

---

## 8. AI Assistant

### 8.1 Integration

- Configurable LLM provider (OpenAI, Anthropic, or other) via API key in Settings
- Each query includes automatic context, bounded and prioritized:
  - Server config summary (always included, ~500 tokens max)
  - Relevant recent logs: only logs related to the user's question, last 50 entries max (~2000 tokens)
  - Current health/backup status summary (~500 tokens)
  - Conversation history: ~1000 tokens (roughly last 10 messages; older messages dropped when budget exceeded)
  - Total context budget: max 4000 tokens to stay within LLM limits and control costs
  - Context selection uses keyword matching from user's question to pick relevant logs/servers
- Full conversation history stored in DB for reference; only the most recent messages fitting the ~1000 token budget are sent to the LLM
- Context-aware responses specific to user's environment

### 8.2 Use Cases

- "Why did last night's backup fail?" → reads logs, explains error, suggests fix
- "How do I add a new database?" → step-by-step guidance
- "Server not responding, what do I do?" → troubleshooting checklist
- General sysadmin questions with environment context

---

## 9. Multi-Destination

### 9.1 Destination Types

| Type | Connection | Use Case |
|------|------------|----------|
| Local disk | Direct path | Primary, always active |
| NAS (SMB/NFS) | Network mount | Automatic post-backup sync |
| USB disk | Mount point | Manual or scheduled sync |
| S3-compatible | API key + endpoint | Future, off-site backup |

### 9.2 Sync Flow

1. Backup completes on primary destination (local disk)
2. Async sync job copies to secondary destinations
3. Each destination independently tracks sync status and verifies checksum at destination after copy
4. Dashboard shows matrix: per-backup status on every destination
5. Alert if any destination falls behind (e.g., "NAS not updated for 3 days")

Each destination has its own independent retention policy.

### 9.3 Sync Failure Handling

- `destination_sync_status` state machine: `pending → in_progress → success | failed`
- On failure: automatic retry with exponential backoff (1min, 5min, 30min), max 5 retries
- After max retries: status marked `failed`, alert sent, manual retry available from UI
- On application restart: any `in_progress` syncs are reset to `pending` and re-queued
- Sync queue is persisted in SQLite — no data loss on restart

---

## 10. Non-Functional Requirements

- **Performance:** Handle 2-10 servers without issue on i5 9th gen + 32GB RAM
- **Bandwidth:** Configurable throttling per time-of-day (e.g., 50Mbps during work hours, unlimited at night). For SSH/rsync: uses `--bwlimit` flag. For FTP: application-level rate limiting on the read/write loop in Go.
- **Scheduling:** Smart scheduling avoids concurrent backups on bandwidth-limited networks
- **Deployment:** Single binary, zero external dependencies, `./backupmanager` to start
- **Updates:** Replace binary and restart. SQLite migrations run automatically on startup — if a migration fails, the application logs the error and refuses to start (no silent corruption).
- **Logging:** Structured JSON logs, queryable from UI
- **Concurrency:** Go goroutines for parallel backup execution across servers
- **Job Timeout:** Each backup job has a configurable timeout (default: 2 hours). Jobs exceeding timeout are killed and marked as failed with alert.

---

## 11. Failure Modes

| Scenario | Behavior |
|----------|----------|
| **Backup machine runs out of disk** | Pre-flight check before each backup: if available space < estimated backup size × 1.5, job is skipped and alert sent. Dashboard shows disk usage warnings at 80% and 90% thresholds. |
| **Backup job hangs indefinitely** | Configurable timeout per job (default 2h). On timeout: process killed, job marked `failed`, alert sent, next scheduled job proceeds normally. |
| **SQLite database corrupted** | On startup, BackupManager runs `PRAGMA integrity_check`. If corruption detected: logs error, attempts recovery from WAL, and alerts admin. SQLite DB itself is backed up daily to a separate location on the backup machine. |
| **Remote server unreachable during backup** | SSH/FTP connection timeout: 30s. Job marked `failed`, alert sent, automatic retry at next scheduled time. No partial snapshot saved. |
| **Remote MySQL dump fails mid-execution** | SSH command exit code checked. On non-zero: job marked `failed`, partial dump file cleaned up on remote server, alert sent. |
| **Application crash during backup** | On restart: incomplete `in_progress` runs are detected and marked `failed`. Partial files on disk are cleaned up. Sync queue is re-evaluated. |
| **Network interruption during file transfer** | rsync handles resume natively. FTP transfers: if interrupted, partial file is discarded and the file is re-queued for next run. |
| **Master encryption key lost and admin password forgotten** | Encrypted backups are permanently unrecoverable. The key file download and SQLite-stored copy (decryptable with admin password) are the two recovery paths — but both depend on having at least one of: the key file OR the admin password. UI prominently warns during setup and periodically reminds to verify key file accessibility. |

---

## 12. API Reference (Core Endpoints)

| Method | Endpoint | Purpose |
|--------|----------|---------|
| POST | `/api/auth/login` | Authenticate, receive JWT cookie |
| POST | `/api/auth/logout` | Invalidate session |
| POST | `/api/auth/reset-password` | Request password reset email |
| GET | `/api/servers` | List all servers |
| POST | `/api/servers` | Add new server |
| GET | `/api/servers/:id` | Server detail with health status |
| POST | `/api/servers/:id/discover` | Trigger auto-discovery |
| GET | `/api/jobs` | List all backup jobs |
| POST | `/api/jobs` | Create new backup job |
| POST | `/api/jobs/:id/trigger` | Manually trigger a backup |
| GET | `/api/runs` | List backup runs (filterable) |
| GET | `/api/runs/:id/logs` | Get run logs |
| GET | `/api/snapshots` | List snapshots (filterable by date, server, type) |
| GET | `/api/snapshots/:id/download` | Download a snapshot |
| GET | `/api/health` | Current health status of all servers |
| GET | `/api/destinations` | List backup destinations |
| GET | `/api/audit` | Audit log (filterable, paginated) |
| POST | `/api/assistant/chat` | Send message to AI assistant |
| GET | `/api/dashboard/summary` | Dashboard summary data |
| WS | `/ws/logs` | Real-time log streaming |
| WS | `/ws/status` | Real-time backup status updates |

### WebSocket Protocol

- **Connection:** Standard WebSocket upgrade on `/ws/logs` and `/ws/status`
- **Authentication:** The browser automatically sends the httpOnly JWT cookie on the WebSocket upgrade request — no separate token needed. Consistent with Section 6.1 security model.
- **Message format:** JSON `{"type": "log|status|health", "server_id": "...", "data": {...}, "timestamp": "..."}`
- **Reconnection:** Client implements exponential backoff reconnection (1s, 2s, 4s, max 30s)
- **Heartbeat:** Server sends ping every 30s, client responds with pong. Connection closed after 3 missed pongs.
