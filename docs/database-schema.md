# BackupManager — Database Schema

BackupManager uses a single SQLite database at `data/backupmanager.db`. All migrations are in `internal/database/migrations/001_initial_schema.sql` and are applied automatically at startup.

---

## Entity-Relationship Diagram

```
┌─────────┐       ┌──────────────────┐       ┌─────────────────┐
│  users  │       │    servers       │       │  destinations   │
│─────────│       │──────────────────│       │─────────────────│
│ id (PK) │       │ id (PK)          │       │ id (PK)         │
│username │       │ name             │       │ name            │
│ email   │       │ type             │       │ type            │
│password │       │ host             │       │ path            │
│is_admin │       │ port             │       │ is_primary      │
└────┬────┘       │ connection_type  │       │ retention_*     │
     │            │ username         │       │ enabled         │
     │            │ encrypted_pass   │       └────────┬────────┘
     │            │ ssh_key_path     │                │
     │            │ status           │                │
     │            └────────┬─────────┘                │
     │                     │                          │
     │            ┌────────┴─────────┐                │
     │            │  backup_sources  │                │
     │            │─────────────────-│                │
     │            │ id (PK)          │                │
     │            │ server_id (FK)   │                │
     │            │ name             │                │
     │            │ type             │                │
     │            │ source_path      │                │
     │            │ db_name          │                │
     │            │ depends_on (FK→id)│               │
     │            │ priority         │                │
     │            │ enabled          │                │
     │            └────────┬─────────┘                │
     │                     │ M:N                      │
     │            ┌────────┴─────────┐                │
     │            │backup_job_sources│                │
     │            │──────────────────│                │
     │            │ job_id (FK)      │                │
     │            │ source_id (FK)   │                │
     │            └────────┬─────────┘                │
     │                     │                          │
     │            ┌────────┴──────────┐               │
     │            │   backup_jobs     │               │
     │            │───────────────────│               │
     │            │ id (PK)           │               │
     │            │ name              │               │
     │            │ server_id (FK)    │               │
     │            │ schedule          │               │
     │            │ retention_daily   │               │
     │            │ retention_weekly  │               │
     │            │ retention_monthly │               │
     │            │ bandwidth_limit   │               │
     │            │ timeout_minutes   │               │
     │            │ enabled           │               │
     │            └────────┬──────────┘               │
     │                     │                          │
     │            ┌────────┴──────────┐               │
     │            │   backup_runs     │               │
     │            │───────────────────│               │
     │            │ id (PK)           │               │
     │            │ job_id (FK)       │               │
     │            │ status            │               │
     │            │ started_at        │               │
     │            │ finished_at       │               │
     │            │ total_size_bytes  │               │
     │            │ files_copied      │               │
     │            │ error_message     │               │
     │            └────────┬──────────┘               │
     │                     │                          │
     │            ┌────────┴──────────┐               │
     │            │backup_snapshots   │               │
     │            │───────────────────│               │
     │            │ id (PK)           ├───────────────┤
     │            │ run_id (FK)       │               │
     │            │ source_id (FK)    │  ┌────────────┴────────────┐
     │            │ snapshot_path     │  │ destination_sync_status │
     │            │ size_bytes        │  │─────────────────────────│
     │            │ checksum_sha256   │  │ id (PK)                 │
     │            │ is_encrypted      │  │ snapshot_id (FK)        │
     │            │ retention_exp     │  │ destination_id (FK)     │
     │            └───────────────────┘  │ status                  │
     │                                   │ retry_count             │
     │                                   │ last_error              │
     │                                   │ synced_at               │
     │                                   └─────────────────────────┘
     │
     ├─────────────────────────────────────────────────────────────
     │
┌────┴─────────┐   ┌──────────────────┐   ┌──────────────────────┐
│  audit_log   │   │  health_checks   │   │llm_conversations     │
│──────────────│   │──────────────────│   │──────────────────────│
│ id (PK)      │   │ id (PK)          │   │ id (PK)              │
│ user_id (FK) │   │ server_id (FK)   │   │ user_id (FK)         │
│ action       │   │ check_type       │   │ role                 │
│ target       │   │ status           │   │ content              │
│ ip_address   │   │ message          │   │ context_data         │
│ details      │   │ value            │   │ created_at           │
│ created_at   │   │ created_at       │   └──────────────────────┘
└──────────────┘   └──────────────────┘
```

---

## Table Definitions

### `schema_migrations`
Tracks applied migration versions.

| Column | Type | Constraints |
|---|---|---|
| `version` | INTEGER | PRIMARY KEY |
| `applied_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |

---

### `users`
Application user accounts.

| Column | Type | Constraints |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `username` | TEXT | NOT NULL UNIQUE |
| `email` | TEXT | NOT NULL UNIQUE |
| `password_hash` | TEXT | NOT NULL (bcrypt) |
| `is_admin` | INTEGER | NOT NULL DEFAULT 0 |
| `created_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |
| `updated_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |

---

### `servers`
Managed remote servers.

| Column | Type | Constraints |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `name` | TEXT | NOT NULL |
| `type` | TEXT | NOT NULL CHECK IN ('linux', 'windows') |
| `host` | TEXT | NOT NULL |
| `port` | INTEGER | NOT NULL |
| `connection_type` | TEXT | NOT NULL CHECK IN ('ssh', 'ftp') |
| `username` | TEXT | |
| `encrypted_password` | TEXT | AES-256-GCM encrypted |
| `ssh_key_path` | TEXT | |
| `status` | TEXT | NOT NULL DEFAULT 'unknown' CHECK IN ('online', 'offline', 'warning', 'unknown') |
| `created_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |
| `updated_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |

---

### `backup_sources`
Individual backup targets within a server.

| Column | Type | Constraints |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `server_id` | INTEGER | NOT NULL REFERENCES servers(id) ON DELETE CASCADE |
| `name` | TEXT | NOT NULL |
| `type` | TEXT | NOT NULL CHECK IN ('web', 'database', 'config') |
| `source_path` | TEXT | Remote filesystem path (web/config) |
| `db_name` | TEXT | Database name (database type) |
| `depends_on` | INTEGER | REFERENCES backup_sources(id) — dependency edge |
| `priority` | INTEGER | NOT NULL DEFAULT 0 — lower = earlier |
| `enabled` | INTEGER | NOT NULL DEFAULT 1 |
| `created_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |

---

### `backup_jobs`
Scheduled backup jobs.

| Column | Type | Constraints |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `name` | TEXT | NOT NULL |
| `server_id` | INTEGER | NOT NULL REFERENCES servers(id) ON DELETE CASCADE |
| `schedule` | TEXT | NOT NULL — cron expression |
| `retention_daily` | INTEGER | NOT NULL DEFAULT 7 |
| `retention_weekly` | INTEGER | NOT NULL DEFAULT 4 |
| `retention_monthly` | INTEGER | NOT NULL DEFAULT 3 |
| `bandwidth_limit_mbps` | INTEGER | NULL = unlimited |
| `timeout_minutes` | INTEGER | NOT NULL DEFAULT 120 |
| `enabled` | INTEGER | NOT NULL DEFAULT 1 |
| `created_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |
| `updated_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |

---

### `backup_job_sources`
Many-to-many join: which sources belong to which job.

| Column | Type | Constraints |
|---|---|---|
| `job_id` | INTEGER | NOT NULL REFERENCES backup_jobs(id) ON DELETE CASCADE |
| `source_id` | INTEGER | NOT NULL REFERENCES backup_sources(id) ON DELETE CASCADE |
| (composite PK) | | PRIMARY KEY (job_id, source_id) |

---

### `backup_runs`
Execution records for each backup job run.

| Column | Type | Constraints |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `job_id` | INTEGER | NOT NULL REFERENCES backup_jobs(id) ON DELETE CASCADE |
| `status` | TEXT | NOT NULL DEFAULT 'pending' CHECK IN ('pending', 'running', 'success', 'failed', 'timeout') |
| `started_at` | DATETIME | |
| `finished_at` | DATETIME | |
| `total_size_bytes` | INTEGER | DEFAULT 0 |
| `files_copied` | INTEGER | DEFAULT 0 |
| `error_message` | TEXT | |
| `created_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |

---

### `backup_snapshots`
One snapshot record per successful source backup within a run.

| Column | Type | Constraints |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `run_id` | INTEGER | NOT NULL REFERENCES backup_runs(id) ON DELETE CASCADE |
| `source_id` | INTEGER | NOT NULL REFERENCES backup_sources(id) |
| `snapshot_path` | TEXT | NOT NULL — absolute local path |
| `size_bytes` | INTEGER | NOT NULL DEFAULT 0 |
| `checksum_sha256` | TEXT | Hex digest |
| `is_encrypted` | INTEGER | NOT NULL DEFAULT 0 |
| `retention_expires_at` | DATETIME | Nullable — computed expiry |
| `created_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |

---

### `destinations`
Storage destinations for multi-destination sync.

| Column | Type | Constraints |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `name` | TEXT | NOT NULL |
| `type` | TEXT | NOT NULL CHECK IN ('local', 'nas', 'usb', 's3') |
| `path` | TEXT | NOT NULL |
| `is_primary` | INTEGER | NOT NULL DEFAULT 0 |
| `retention_daily` | INTEGER | NOT NULL DEFAULT 7 |
| `retention_weekly` | INTEGER | NOT NULL DEFAULT 4 |
| `retention_monthly` | INTEGER | NOT NULL DEFAULT 3 |
| `enabled` | INTEGER | NOT NULL DEFAULT 1 |
| `created_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |

---

### `destination_sync_status`
Tracks sync state of each snapshot to each destination.

| Column | Type | Constraints |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `snapshot_id` | INTEGER | NOT NULL REFERENCES backup_snapshots(id) ON DELETE CASCADE |
| `destination_id` | INTEGER | NOT NULL REFERENCES destinations(id) ON DELETE CASCADE |
| `status` | TEXT | NOT NULL DEFAULT 'pending' CHECK IN ('pending', 'in_progress', 'success', 'failed') |
| `retry_count` | INTEGER | NOT NULL DEFAULT 0 |
| `last_error` | TEXT | |
| `synced_at` | DATETIME | |
| `created_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |
| (unique) | | UNIQUE(snapshot_id, destination_id) |

---

### `health_checks`
Time-series health check results per server.

| Column | Type | Constraints |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `server_id` | INTEGER | NOT NULL REFERENCES servers(id) ON DELETE CASCADE |
| `check_type` | TEXT | NOT NULL (`reachability`, `disk`, `nginx`, `mysql`, `pm2`, `cpu`, `ram`, `ftp`) |
| `status` | TEXT | NOT NULL CHECK IN ('ok', 'warning', 'critical') |
| `message` | TEXT | |
| `value` | TEXT | Raw metric value |
| `created_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |

---

### `health_check_config`
Per-server, per-check-type configuration and thresholds.

| Column | Type | Constraints |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `server_id` | INTEGER | NOT NULL REFERENCES servers(id) ON DELETE CASCADE |
| `check_type` | TEXT | NOT NULL |
| `enabled` | INTEGER | NOT NULL DEFAULT 1 |
| `warning_threshold` | TEXT | |
| `critical_threshold` | TEXT | |
| `interval_seconds` | INTEGER | NOT NULL DEFAULT 300 |
| (unique) | | UNIQUE(server_id, check_type) |

---

### `audit_log`
Append-only audit trail.

| Column | Type | Constraints |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `user_id` | INTEGER | REFERENCES users(id) — nullable (system actions) |
| `action` | TEXT | NOT NULL (`METHOD /path`) |
| `target` | TEXT | Last path segment |
| `ip_address` | TEXT | |
| `details` | TEXT | |
| `created_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |

---

### `notifications_config`
Per-event-type notification configuration.

| Column | Type | Constraints |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `event_type` | TEXT | NOT NULL UNIQUE |
| `telegram_enabled` | INTEGER | NOT NULL DEFAULT 0 |
| `email_enabled` | INTEGER | NOT NULL DEFAULT 0 |
| `telegram_chat_id` | TEXT | |
| `email_recipients` | TEXT | Comma-separated |
| `created_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |

---

### `notifications_log`
History of sent/failed notifications.

| Column | Type | Constraints |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `event_type` | TEXT | NOT NULL |
| `channel` | TEXT | NOT NULL CHECK IN ('telegram', 'email') |
| `recipient` | TEXT | NOT NULL |
| `message` | TEXT | NOT NULL |
| `status` | TEXT | NOT NULL CHECK IN ('sent', 'failed') |
| `error_message` | TEXT | |
| `created_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |

---

### `recovery_playbooks`
Disaster recovery runbooks per server.

| Column | Type | Constraints |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `server_id` | INTEGER | REFERENCES servers(id) ON DELETE CASCADE |
| `title` | TEXT | NOT NULL |
| `scenario` | TEXT | NOT NULL |
| `steps` | TEXT | NOT NULL — Markdown |
| `created_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |
| `updated_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |

---

### `discovery_results`
Cached service discovery results.

| Column | Type | Constraints |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `server_id` | INTEGER | NOT NULL REFERENCES servers(id) ON DELETE CASCADE |
| `service_name` | TEXT | NOT NULL |
| `service_data` | TEXT | NOT NULL — JSON |
| `discovered_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |

---

### `llm_conversations`
Per-user LLM conversation history.

| Column | Type | Constraints |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `user_id` | INTEGER | NOT NULL REFERENCES users(id) |
| `role` | TEXT | NOT NULL CHECK IN ('user', 'assistant') |
| `content` | TEXT | NOT NULL |
| `context_data` | TEXT | JSON snapshot of system context at message time |
| `created_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |

---

### `settings`
Key-value store for application configuration.

| Column | Type | Constraints |
|---|---|---|
| `key` | TEXT | PRIMARY KEY |
| `value` | TEXT | NOT NULL |
| `updated_at` | DATETIME | DEFAULT CURRENT_TIMESTAMP |

**Known keys:**

| Key | Description |
|---|---|
| `jwt_secret` | Auto-generated 32-byte hex JWT signing secret |
| `encryption_master_key` | Hex-encoded Argon2-wrapped AES-256 master key |
| `assistant_provider` | `"openai"` or `"anthropic"` |
| `assistant_api_key` | LLM provider API key |
| `assistant_model` | Model name |
| `timezone` | IANA timezone string (e.g. `"Europe/Rome"`) |
| `reset_token:<hash>` | Temporary password reset token (expires 1h) |
| `login_rate_limit_requests` | Max login attempts per window |
| `login_rate_limit_window_seconds` | Rate limit window duration |

---

## Indexes

| Index | Table | Column(s) | Rationale |
|---|---|---|---|
| `idx_backup_runs_job_id` | backup_runs | job_id | Filter runs by job (dashboard, history) |
| `idx_backup_runs_status` | backup_runs | status | Filter by status (running/failed) |
| `idx_backup_snapshots_run_id` | backup_snapshots | run_id | Load snapshots for a run |
| `idx_backup_snapshots_source_id` | backup_snapshots | source_id | Load snapshots per source (retention) |
| `idx_health_checks_server_id` | health_checks | server_id | Load checks for a server |
| `idx_health_checks_created_at` | health_checks | created_at | Prune old health data |
| `idx_audit_log_created_at` | audit_log | created_at | Date-range filter on audit queries |
| `idx_audit_log_user_id` | audit_log | user_id | Filter audit by user |
| `idx_notifications_log_created_at` | notifications_log | created_at | History pruning |
| `idx_destination_sync_status_status` | destination_sync_status | status | Find pending/failed syncs quickly |

---

## Migration Strategy

Migrations are numbered SQL files embedded in the binary at `internal/database/migrations/`. `db.Migrate()` reads the `schema_migrations` table to find the highest applied version, then executes all higher-numbered files in order.

All DDL statements use `CREATE TABLE IF NOT EXISTS` making them safe to re-execute. To add a new migration:

1. Create `internal/database/migrations/002_add_feature.sql`
2. Write `CREATE TABLE IF NOT EXISTS` or `ALTER TABLE` statements
3. Rebuild; `Migrate()` will apply the new file on next startup

**Backup before migration**: The self-backup cron copies the live SQLite file nightly. Before any schema change in production, run `cp data/backupmanager.db data/backupmanager.db.pre-migration`.
