# BackupManager — Core Layer

The core layer contains all business logic. It has no dependency on `net/http` — it receives plain Go values, performs work, and returns results. This makes it straightforwardly testable.

Packages: `internal/backup/`, `internal/scheduler/`, `internal/retention/`, `internal/health/`, `internal/audit/`, `internal/notification/`, `internal/assistant/`

---

## Scheduler (`internal/scheduler/scheduler.go`)

The scheduler wraps `github.com/robfig/cron/v3` and adds job management and missed-backup detection.

### How it works

```
scheduler.Start()
  └── Query: SELECT id, name, schedule FROM backup_jobs WHERE enabled = 1
       └── For each job: cron.AddFunc(schedule, makeJobFunc(jobID))
  └── cron.Start()   ← starts the background cron goroutine
  └── StartMissedBackupDetection(15 * time.Minute)
```

### Key types

```go
type Scheduler struct {
    cron    *cron.Cron
    runner  RunnerInterface    // backup.Runner (or a test mock)
    db      *database.Database
    entries map[int]cron.EntryID   // jobID → cron entry
}
```

### Job lifecycle

| Method | Description |
|---|---|
| `AddJob(jobID, schedule)` | Registers a new cron entry; logs it |
| `RemoveJob(jobID)` | Removes the cron entry by ID |
| `UpdateJob(jobID, schedule)` | `RemoveJob` + `AddJob` |
| `TriggerJob(jobID)` | Launches `executeJob` in a background goroutine immediately |

### Missed backup detection

Every 15 minutes, `detectMissedBackups()` queries all enabled jobs and calculates the expected interval by computing the distance between two consecutive future cron fires. If `now - last_run > 2 × interval`, a `WARNING` is logged. This acts as a passive alerting layer.

### SQLite self-backup

`StartSQLiteBackup(dbPath, backupDir)` registers a daily cron at `0 3 * * *` (03:00 system time) that copies the live SQLite database file to `backupDir`, retaining the last 7 copies.

---

## Backup Orchestrator (`internal/backup/orchestrator.go`)

`Orchestrator.ExecuteJob(ctx, jobID)` is the main backup pipeline entry point.

### Execution pipeline

```
1. loadJob(jobID)              → job name, timeout, bandwidth limit
2. loadServer(job.ServerID)    → host, port, credentials, type (linux/windows)
3. loadJobSources(jobID)       → enabled sources with depends_on and priority
4. RunPreflight(ctx, …)        → abort early if environment is unhealthy
5. createRun(jobID)            → INSERT INTO backup_runs (status='running')
6. TopologicalSort(sources)    → dependency-aware ordering
7. For each source:
   ├── ctx.Err() check (timeout support)
   ├── Skip if dependency failed (propagate failure)
   ├── executeSource(src, server, job, timestamp)
   └── createSnapshot(runID, sourceID, destPath, size, checksum)
8. updateRunStatus(runID, status, totalSize, filesCopied, errMsg)
```

### Source execution

```go
func (o *Orchestrator) executeSource(...) SourceResult {
    switch src.Type {
    case "web", "config":
        syncer := chooseSyncer(server.Type)
        // Linux  → rsync over SSH
        // Windows→ FTP with manifest
        result, err := syncer.Sync(ctx, source, destPath, opts)
    case "database":
        // MySQL dump via SSH connector
        result, err := syncer.Sync(ctx, source, destPath, opts)
    }
}
```

### Dependency graph and topological sort

Sources can declare `depends_on` (a foreign key to another `backup_sources.id`). The orchestrator builds a directed graph and runs **Kahn's algorithm** to produce a safe execution order. If a cycle is detected, the run fails before any source executes.

Sources at the same topological level are sorted by ascending `priority` (lower number = earlier).

If a source fails, all sources that depend on it (transitively) are marked `skipped`.

### Pre-flight checks (`internal/backup/preflight.go`)

Before a `backup_run` record is created, preflight checks verify the environment:

| Check | What it verifies |
|---|---|
| Disk space | Local backup directory has sufficient free space |
| Network | Target server host:port is reachable via TCP |
| Source availability | Remote path exists on the server |

Preflight failure returns an error without creating a run record, keeping the run history clean.

### Crash recovery (`internal/backup/recovery.go`)

`RecoverFromCrash(db, backupDir)` is called at startup (before the scheduler starts). It:

1. Marks all `backup_runs` with `status='running'` as `'failed'` with message `"interrupted by application restart"`.
2. Resets `destination_sync_status` rows with `status='in_progress'` back to `'pending'`.
3. Deletes partial files on disk whose snapshot path is inside `backupDir` and has no corresponding completed snapshot.

---

## Retention Policy Engine (`internal/retention/retention.go`)

The retention engine decides which snapshots to delete based on a three-tier policy.

### Policy structure

```go
type RetentionPolicy struct {
    Daily   int  // keep one snapshot per day for N days
    Weekly  int  // keep one snapshot per week for N weeks
    Monthly int  // keep one snapshot per month for N months
}
```

### Algorithm (`Apply`)

Snapshots are processed **newest-first**. The engine builds a `keep` set:

1. **Always keep** the most recent snapshot (regardless of policy).
2. **Daily tier**: for each day in the window `[today - (Daily-1) .. today]`, keep the newest snapshot in that day (in the configured timezone).
3. **Weekly tier**: for each week (Sun–Sat boundary), keep the newest snapshot in that week.
4. **Monthly tier**: for each calendar month, keep the newest snapshot.

Any snapshot not in the keep set is returned in `toDelete`.

### Timezone handling

All day/week/month boundaries are calculated in the configured timezone (`BM_TIMEZONE` env var, default `"Local"`). This prevents midnight-boundary edge cases where a backup taken at 23:55 local time would be attributed to the wrong day in UTC.

### CleanupService

`CleanupService.RunCleanup(ctx, tz)` applies retention per `(source_id, job_id)` group:
1. Loads all snapshots joined to their job's retention config.
2. Groups by `(source_id, job_id)`.
3. Calls `Apply()` for each group.
4. For each ID to delete: `os.RemoveAll(snapshotPath)` + `DELETE FROM backup_snapshots WHERE id = ?`.

---

## Health Monitoring (`internal/health/`)

### HealthMonitor

`HealthMonitor` runs `checkAll()` at startup and then on a configurable interval (default: every 5 minutes). It queries all servers from the database and runs `CheckServer` for each one.

### Check types

| Check type | Server type | Command / method |
|---|---|---|
| `reachability` | both | TCP dial to host:port with 5 s timeout |
| `disk` | linux | `df -h /` — parses percentage used |
| `nginx` | linux | `systemctl is-active nginx` |
| `mysql` | linux | `mysqladmin ping` |
| `pm2` | linux | `pm2 jlist` — counts online processes |
| `cpu` | linux | `uptime` — 1-minute load average |
| `ram` | linux | `free -m` — calculates used/total ratio |
| `ftp` | windows | FTP login attempt |

### Status levels

Each check returns `"ok"`, `"warning"`, or `"critical"`. The `overall` field for a server is the worst status among all its checks (`critical > warning > ok`).

Results are stored in `health_checks` table and queryable via the API.

---

## Audit Logging (`internal/audit/audit.go`)

### AuditService

```go
func (s *AuditService) Log(userID *int, action, target, ip, details string) error
```

Inserts a row into `audit_log`. The `action` field is `"METHOD /path"` (e.g. `"DELETE /api/servers/5"`). `target` is the last URL path segment.

### AuditMiddleware

`AuditMiddleware` wraps the protected router. It runs the handler first, then — for POST/PUT/DELETE — logs the request. This means the log entry is written even if the handler returns an error. User identity is extracted from the JWT claims stored in the request context.

The audit log is **append-only** from the application's perspective (no UPDATE or DELETE of audit rows).

### Query and Export

`Query(QueryOptions)` supports filtering by user, action, date range, with pagination. `ExportCSV` streams all matching rows as CSV without pagination.

---

## Notification System (`internal/notification/`)

### Event types

```go
const (
    EventBackupSuccess     EventType = "backup_success"
    EventBackupFailed      EventType = "backup_failed"
    EventServerUnreachable EventType = "server_unreachable"
    EventServiceDown       EventType = "service_down"
    EventDiskSpaceLow      EventType = "disk_space_low"
    EventIntegrityFailed   EventType = "integrity_failed"
    EventMissedBackup      EventType = "missed_backup"
    EventRecoveryActivated EventType = "recovery_activated"
    EventUserLogin         EventType = "user_login"
)
```

### Manager

`Manager.Notify(event)`:
1. Loads config from `notifications_config` for the event type.
2. If Telegram is enabled and a chat ID exists, calls `TelegramNotifier.SendWithAntiFlood`.
3. If email is enabled and recipients exist, calls `EmailNotifier.SendWithAntiFlood`.
4. Returns `nil` if at least one channel succeeded (or none were configured).

### Anti-flood

Both notifiers implement anti-flood via an in-memory map keyed by `"serverName:eventType"`. Repeated events within the cooldown window (default: 30 minutes) are suppressed. The last-sent time is tracked per `alertKey`.

### Message formatting

- **Telegram**: Markdown with bold labels and monospace values.
- **Email**: HTML table with a clean layout, sent via SMTP (`net/smtp`).

---

## LLM Assistant (`internal/assistant/`)

### AssistantService

Settings (`assistant_provider`, `assistant_api_key`, `assistant_model`) are loaded from the `settings` table at startup and refreshed on every `Chat()` call.

Supported providers:
- `"openai"` (default): `https://api.openai.com/v1/chat/completions`, default model `gpt-4o-mini`
- `"anthropic"`: `https://api.anthropic.com/v1/messages`, default model `claude-3-5-haiku-20241022`

### Context building (`internal/assistant/context.go`)

`BuildContext(db, message, history)` queries the database for live operational data and assembles a system prompt containing:
- List of servers and their health status
- Recent backup job results
- Active alerts and failed runs
- Total snapshot count and disk usage

This gives the LLM real-time situational awareness without requiring the user to describe the environment in every message.

### Conversation persistence

Each user's conversation is stored in `llm_conversations`. The last 10 messages are loaded as context on each `Chat()` call (to stay within token budgets). `ClearHistory(userID)` deletes all messages for a user.
