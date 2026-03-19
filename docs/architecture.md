# BackupManager — System Architecture

## Overview

BackupManager is a self-hosted backup orchestration server written in Go with a React frontend. A single binary serves both the REST API and the embedded SPA. Data is stored in SQLite.

---

## System Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                        Browser / Client                         │
│              React SPA  (Vite + Tailwind + React Query)         │
└───────────────────────────┬─────────────────────────────────────┘
                            │  HTTPS / WSS  (port 8080 default)
┌───────────────────────────▼─────────────────────────────────────┐
│                        WEB LAYER                                │
│  ┌────────────┐  ┌──────────────┐  ┌──────────────────────────┐ │
│  │ Auth / JWT │  │ CSRF Middle- │  │    REST API Handlers     │ │
│  │ Middleware │  │    ware      │  │  (auth, servers, jobs…)  │ │
│  └────────────┘  └──────────────┘  └──────────────────────────┘ │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │              WebSocket Hub  (/ws/logs, /ws/status)          │ │
│  └─────────────────────────────────────────────────────────────┘ │
└───────────────────────────┬─────────────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────────────┐
│                        CORE LAYER                               │
│  ┌─────────────┐ ┌──────────────┐ ┌──────────────────────────┐  │
│  │  Scheduler  │ │ Orchestrator │ │   Retention Engine       │  │
│  │  (robfig    │ │  (dep graph, │ │  (daily/weekly/monthly)  │  │
│  │   cron/v3)  │ │  topo sort)  │ │                          │  │
│  └─────────────┘ └──────────────┘ └──────────────────────────┘  │
│  ┌─────────────┐ ┌──────────────┐ ┌──────────────────────────┐  │
│  │   Health    │ │    Audit     │ │  Notification Manager    │  │
│  │   Monitor   │ │   Service    │ │  (Telegram + Email)      │  │
│  └─────────────┘ └──────────────┘ └──────────────────────────┘  │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │        LLM Assistant  (OpenAI / Anthropic)               │    │
│  └──────────────────────────────────────────────────────────┘    │
└───────────────────────────┬─────────────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────────────┐
│                    INFRASTRUCTURE LAYER                         │
│  ┌──────────┐ ┌─────────┐ ┌──────────┐ ┌────────────────────┐  │
│  │  SSH     │ │  FTP    │ │  Rsync   │ │   FTP Syncer       │  │
│  │Connector │ │Connector│ │  Syncer  │ │ (manifest-based)   │  │
│  └──────────┘ └─────────┘ └──────────┘ └────────────────────┘  │
│  ┌──────────┐ ┌─────────────────────┐ ┌────────────────────┐   │
│  │  MySQL   │ │   AES-256-GCM       │ │   Integrity        │   │
│  │  Dump    │ │   Encryption +      │ │   Checker          │   │
│  │Orchestr. │ │   Argon2 Key Wrap   │ │   (SHA-256)        │   │
│  └──────────┘ └─────────────────────┘ └────────────────────┘   │
└──────────────────────────────────────────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────────────┐
│                  STORAGE (local filesystem)                     │
│   data/backupmanager.db    data/backups/<server>/<type>/…       │
└─────────────────────────────────────────────────────────────────┘
```

---

## Three-Layer Design

### Web Layer (`internal/api/`, `internal/auth/`, `internal/websocket/`)
Responsible for HTTP routing, authentication, CSRF protection, and real-time push via WebSockets. Handlers are thin: they parse requests, delegate to services, and marshal responses.

### Core Layer (`internal/backup/`, `internal/scheduler/`, `internal/retention/`, `internal/health/`, `internal/audit/`, `internal/notification/`, `internal/assistant/`)
Contains all business logic. No HTTP types appear here. The scheduler owns the cron clock; the orchestrator owns the backup execution pipeline; the retention engine owns the cleanup algorithm.

### Infrastructure Layer (`internal/connector/`, `internal/sync/`, `internal/encryption/`, `internal/database/`, `internal/integrity/`)
Abstracts external systems: remote servers (SSH/FTP), file transfer (rsync/FTP manifest), encryption, the SQLite database, and integrity verification.

---

## Data Flow — Backup Operation

```
1. Scheduler fires cron job (or API trigger)
       │
       ▼
2. backup.Runner.Run(ctx, jobID)
       │
       ▼
3. Orchestrator.ExecuteJob(ctx, jobID)
       │
       ├── 3a. RunPreflight() — check disk space, network, source availability
       │
       ├── 3b. Create backup_run record (status = "running")
       │
       ├── 3c. TopologicalSort(sources) — respect depends_on
       │
       └── 3d. For each source in sorted order:
               │
               ├─ source.type == "web" | "config"
               │    └── chooseSyncer(server.Type):
               │         ├─ Linux  → RsyncSyncer.Sync() via rsync binary over SSH
               │         └─ Windows→ FTPSyncer.Sync() via manifest comparison
               │
               └─ source.type == "database"
                    └── MySQLDumpOrchestrator.DumpAndCopy()
                         1. SSH: mkdir -p staging dir
                         2. SSH: mysqldump | gzip > remote_path
                         3. SSH: sha256sum remote_path
                         4. SFTP: CopyFile remote → local
                         5. Verify local checksum == remote checksum
       │
       ▼
4. Create backup_snapshot record per successful source
       │
       ▼
5. Update backup_run (status = "success" | "failed")
       │
       ▼
6. Retention.CleanupService.RunCleanup() — prune expired snapshots
       │
       ▼
7. Sync snapshots to configured destinations (rsync / ftp)
       │
       ▼
8. Notification.Manager.Notify() — Telegram / email
```

---

## Technology Choices and Rationale

| Technology | Why |
|---|---|
| **Go** | Single binary deployment, excellent concurrency for parallel backup jobs, strong standard library |
| **SQLite** (mattn/go-sqlite3) | Zero-config embedded database; backup operators don't want to manage a DB server |
| **robfig/cron/v3** | Battle-tested cron parser supporting standard 5-field and descriptor expressions |
| **gorilla/websocket** | Stable WebSocket implementation; real-time log streaming without polling |
| **pkg/sftp** | Pure-Go SFTP client; no external `sftp` binary dependency |
| **jlaffaye/ftp** | Pure-Go FTP client for Windows server support |
| **golang-jwt/jwt** | JWT HS256 tokens in httpOnly cookies; avoids XSS token theft |
| **golang.org/x/crypto** | Argon2id (key derivation) + bcrypt (passwords) from the Go team |
| **AES-256-GCM** | Authenticated encryption; nonce prepended to ciphertext; standard library |
| **React 19 + Vite** | Fast HMR, tree-shaking; SPA embedded in the Go binary at build time |
| **TanStack Query** | Server-state caching, background refresh, automatic retry |
| **Tailwind CSS v4** | Utility-first; no separate CSS build step via the Vite plugin |
| **Recharts** | Declarative charts for disk usage and health trends |

---

## Project Structure

```
server_backup_manager/
├── cmd/server/
│   ├── main.go                  Entry point: wires all layers, starts HTTP server
│   └── static/                  Embedded frontend build (generated by `make build`)
├── frontend/                    React SPA source
│   ├── src/
│   │   ├── api/                 HTTP client wrappers (one file per resource)
│   │   ├── components/          Reusable UI components
│   │   ├── hooks/               useAuth, useWebSocket
│   │   ├── pages/               One component per route
│   │   └── types/               Shared TypeScript type definitions
│   └── vite.config.ts
├── internal/
│   ├── api/                     HTTP handlers + router + CSRF + rate limiter
│   ├── assistant/               LLM chat service (OpenAI / Anthropic)
│   ├── audit/                   Audit log service and middleware
│   ├── auth/                    JWT service, bcrypt, middleware
│   ├── backup/                  Orchestrator, runner, MySQL dump, recovery, preflight
│   ├── config/                  Environment variable loading
│   ├── connector/               SSH and FTP connectors (Connector interface)
│   ├── database/                SQLite open, migrate, credential crypto
│   ├── discovery/               Auto-discover services on remote servers
│   ├── docs/                    Embedded markdown documentation content
│   ├── encryption/              AES-256-GCM file encryption, Argon2 key manager
│   ├── health/                  Health monitor, check parsers
│   ├── integrity/               SHA-256 snapshot integrity verification
│   ├── notification/            Telegram and email notifiers, anti-flood manager
│   ├── recovery/                Recovery playbook generator
│   ├── retention/               Snapshot retention policy engine
│   ├── scheduler/               Cron scheduler, missed backup detection
│   ├── setup/                   First-run setup (data dirs, admin user)
│   ├── sync/                    Rsync and FTP sync engines, manifest
│   └── websocket/               WebSocket hub, client pump goroutines
├── go.mod
├── go.sum
└── Makefile
```
