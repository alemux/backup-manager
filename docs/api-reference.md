# BackupManager — API Reference

All endpoints are served under the same host and port (default `8080`). Unless noted, all responses have `Content-Type: application/json`.

**Auth**: All protected endpoints require the `token` httpOnly JWT cookie (set at login). State-changing requests (POST/PUT/DELETE) also require the `X-CSRF-Token` header equal to the `csrf_token` cookie value.

**Response envelope**:
- Success: `{ "data": <payload> }` — helpers wrap with `data` key
- Error: `{ "error": "message" }`
- Some list endpoints return flat arrays or objects directly (noted per endpoint)

---

## Auth

### POST /api/auth/login
Login. **Public** (no cookie, no CSRF required).

**Request body:**
```json
{ "username": "admin", "password": "admin" }
```

**Response 200:**
```json
{
  "data": {
    "user_id": 1,
    "username": "admin",
    "email": "admin@localhost",
    "is_admin": true
  }
}
```
Sets cookies: `token` (httpOnly, JWT, 24h) and `csrf_token` (readable by JS).

**Errors:** `400` missing fields, `401` invalid credentials.

---

### POST /api/auth/logout
Logout. Clears the `token` cookie.

**Response 200:**
```json
{ "data": { "message": "logged out" } }
```

---

### POST /api/auth/reset-password
Request a password reset link. Always returns 200 to prevent email enumeration.

**Request body:**
```json
{ "email": "admin@localhost" }
```

**Response 200:**
```json
{ "data": { "message": "if that email exists, a reset link has been sent" } }
```
The raw reset token is currently logged to stdout (email delivery not yet implemented).

---

### POST /api/auth/reset-password/confirm
Complete a password reset.

**Request body:**
```json
{ "token": "<raw-token>", "new_password": "newpass123" }
```

**Response 200:**
```json
{ "data": { "message": "password updated successfully" } }
```

**Errors:** `400` invalid/expired token or missing fields.

---

## System Health

### GET /api/health
Liveness probe. **Public.**

**Response 200:**
```json
{ "status": "ok" }
```

---

## Servers

All protected (require auth).

### GET /api/servers
List all servers.

**Response 200:** Array of server objects.
```json
[
  {
    "id": 1,
    "name": "web-prod",
    "type": "linux",
    "host": "192.168.1.10",
    "port": 22,
    "connection_type": "ssh",
    "username": "backup",
    "status": "online",
    "created_at": "2026-03-01T10:00:00Z",
    "updated_at": "2026-03-01T10:00:00Z"
  }
]
```

---

### POST /api/servers
Create a server.

**Request body:**
```json
{
  "name": "web-prod",
  "type": "linux",
  "host": "192.168.1.10",
  "port": 22,
  "connection_type": "ssh",
  "username": "backup",
  "password": "secret",
  "ssh_key_path": ""
}
```
`password` is encrypted at rest. `type`: `"linux"` | `"windows"`. `connection_type`: `"ssh"` | `"ftp"`.

**Response 201:** Created server object.

**Errors:** `400` validation, `500` DB error.

---

### GET /api/servers/{id}
Get a single server by ID.

**Response 200:** Server object. **404** if not found.

---

### PUT /api/servers/{id}
Update a server. Same body as POST.

**Response 200:** Updated server object.

---

### DELETE /api/servers/{id}
Delete a server and all associated data (ON DELETE CASCADE).

**Response 204** No Content. **404** if not found.

---

### POST /api/servers/test-connection
Test connectivity to a server without saving it.

**Request body:** Same fields as POST /api/servers (no `id`).

**Response 200:**
```json
{ "data": { "success": true, "message": "connection successful" } }
```

---

### POST /api/servers/{id}/discover
Discover services running on a server (auto-populates backup sources).

**Response 200:** List of discovered services.

---

## Sources

### GET /api/servers/{id}/sources
List backup sources for a server.

**Response 200:**
```json
[
  {
    "id": 3,
    "server_id": 1,
    "name": "wordpress",
    "type": "web",
    "source_path": "/var/www/wordpress",
    "db_name": null,
    "depends_on": null,
    "priority": 0,
    "enabled": true,
    "created_at": "2026-03-01T10:00:00Z"
  }
]
```
`type`: `"web"` | `"database"` | `"config"`.

---

### POST /api/servers/{id}/sources
Create a backup source.

**Request body:**
```json
{
  "name": "wordpress",
  "type": "web",
  "source_path": "/var/www/wordpress",
  "db_name": null,
  "depends_on": null,
  "priority": 0,
  "enabled": true
}
```

**Response 201:** Created source object.

---

### PUT /api/sources/{id}
Update a backup source. Same body as POST.

**Response 200:** Updated source object.

---

### DELETE /api/sources/{id}
Delete a backup source.

**Response 204** No Content.

---

## Jobs

### GET /api/jobs
List all backup jobs with their last run info.

**Response 200:** Array of job objects.
```json
[
  {
    "id": 1,
    "name": "nightly-web",
    "server_id": 1,
    "server_name": "web-prod",
    "schedule": "0 2 * * *",
    "retention_daily": 7,
    "retention_weekly": 4,
    "retention_monthly": 3,
    "bandwidth_limit_mbps": null,
    "timeout_minutes": 120,
    "enabled": true,
    "source_ids": [3, 4],
    "last_run": {
      "id": 42,
      "status": "success",
      "started_at": "2026-03-19T02:00:01Z",
      "finished_at": "2026-03-19T02:12:44Z",
      "total_size_bytes": 1048576
    },
    "created_at": "2026-03-01T10:00:00Z"
  }
]
```

---

### POST /api/jobs
Create a backup job.

**Request body:**
```json
{
  "name": "nightly-web",
  "server_id": 1,
  "schedule": "0 2 * * *",
  "retention_daily": 7,
  "retention_weekly": 4,
  "retention_monthly": 3,
  "bandwidth_limit_mbps": null,
  "timeout_minutes": 120,
  "enabled": true,
  "source_ids": [3, 4]
}
```
`schedule` must be a valid 5-field cron expression. `source_ids` must belong to `server_id`.

**Response 201:** Created job object.

---

### GET /api/jobs/{id}
Get a single job.

**Response 200:** Job object. **404** if not found.

---

### PUT /api/jobs/{id}
Update a job. Same body as POST.

**Response 200:** Updated job object.

---

### DELETE /api/jobs/{id}
Delete a job.

**Response 204** No Content.

---

### POST /api/jobs/{id}/trigger
Manually trigger a job immediately (runs in background).

**Response 202:**
```json
{ "data": { "run_id": 43 } }
```

**Errors:** `404` job not found, `500` trigger not configured.

---

## Runs

### GET /api/runs
List backup runs with filtering and pagination.

**Query parameters:**
| Param | Type | Description |
|---|---|---|
| `job_id` | int | Filter by job |
| `server_id` | int | Filter by server |
| `status` | string | `pending` \| `running` \| `success` \| `failed` \| `timeout` |
| `from` | datetime | Lower bound on `started_at` (RFC3339) |
| `to` | datetime | Upper bound on `started_at` (RFC3339) |
| `page` | int | Page number (default 1) |
| `per_page` | int | Items per page (default 20) |

**Response 200:**
```json
{
  "runs": [...],
  "total": 156,
  "page": 1,
  "per_page": 20
}
```

---

### GET /api/runs/{id}/logs
Get run detail with snapshots.

**Response 200:**
```json
{
  "run": {
    "id": 42,
    "job_id": 1,
    "status": "success",
    "started_at": "2026-03-19T02:00:01Z",
    "finished_at": "2026-03-19T02:12:44Z",
    "total_size_bytes": 1048576,
    "files_copied": 234,
    "error_message": null,
    "created_at": "2026-03-19T02:00:01Z"
  },
  "snapshots": [
    {
      "id": 88,
      "source_id": 3,
      "snapshot_path": "/data/backups/web-prod/web/wordpress/2026-03-19_020001",
      "size_bytes": 1048576,
      "checksum_sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
      "created_at": "2026-03-19T02:12:44Z"
    }
  ]
}
```

---

## Snapshots

### GET /api/snapshots
List snapshots with optional filtering.

**Query parameters:** `job_id`, `server_id`, `source_id`, `from`, `to`, `page`, `per_page`.

**Response 200:** Paginated list.

---

### GET /api/snapshots/{id}
Get a single snapshot.

**Response 200:** Snapshot object.

---

### GET /api/snapshots/{id}/download
Download the snapshot file.

**Response 200:** Binary file stream (`application/octet-stream`).

---

### GET /api/snapshots/calendar
Calendar data for snapshot heat-map.

**Query parameters:** `year` (int), `month` (int, 1–12).

**Response 200:** Array of `{ date: "2026-03-19", count: 5, total_bytes: 1048576 }`.

---

## Health

### GET /api/health/servers
Current health status for all servers (latest check per server).

**Response 200:**
```json
[
  {
    "server_id": 1,
    "overall": "ok",
    "checked_at": "2026-03-19T14:00:00Z",
    "checks": [
      { "check_type": "reachability", "status": "ok", "message": "reachable", "value": "" },
      { "check_type": "disk", "status": "warning", "message": "disk 82% used", "value": "82%" }
    ]
  }
]
```

---

### GET /api/health/servers/{id}/history
Health check history for a single server.

**Query parameters:** `limit` (default 50).

**Response 200:** Array of `ServerHealth` objects ordered newest-first.

---

## Dashboard

### GET /api/dashboard/summary
Aggregate statistics for the dashboard.

**Response 200:**
```json
{
  "data": {
    "total_servers": 5,
    "healthy_servers": 4,
    "total_jobs": 8,
    "jobs_run_today": 3,
    "success_rate_7d": 0.96,
    "total_snapshots": 142,
    "total_backup_bytes": 10737418240
  }
}
```

---

## Notifications

### GET /api/notifications/config
Get all notification configurations.

**Response 200:** Array of config objects per event type.

---

### PUT /api/notifications/config
Update notification configuration.

**Request body:**
```json
{
  "event_type": "backup_failed",
  "telegram_enabled": true,
  "telegram_chat_id": "123456789",
  "email_enabled": false,
  "email_recipients": ""
}
```

**Response 200:** Updated config.

---

### POST /api/notifications/test
Send a test notification.

**Request body:**
```json
{ "channel": "telegram", "recipient": "123456789" }
```

**Response 200:**
```json
{ "data": { "success": true } }
```

---

### GET /api/notifications/log
Paginated notification send history.

**Response 200:** Array of `{ event_type, channel, recipient, message, status, error_message, created_at }`.

---

## Destinations

### GET /api/destinations
List all backup destinations.

**Response 200:**
```json
[
  {
    "id": 1,
    "name": "NAS-primary",
    "type": "nas",
    "path": "/mnt/nas/backups",
    "is_primary": true,
    "retention_daily": 7,
    "retention_weekly": 4,
    "retention_monthly": 3,
    "enabled": true,
    "created_at": "2026-03-01T10:00:00Z"
  }
]
```

---

### POST /api/destinations
Create a destination. `type`: `"local"` | `"nas"` | `"usb"` | `"s3"`.

**Response 201:** Created destination.

---

### PUT /api/destinations/{id}
Update a destination.

**Response 200:** Updated destination.

---

### DELETE /api/destinations/{id}
Delete a destination.

**Response 204** No Content.

---

### GET /api/destinations/status
Sync status matrix: all `(snapshot_id, destination_id)` pairs.

**Response 200:** Array of sync status records.

---

### POST /api/destinations/{id}/retry/{snapshot_id}
Retry a failed sync for a specific snapshot to a specific destination.

**Response 202:**
```json
{ "data": { "queued": true } }
```

---

## Integrity

### POST /api/integrity/verify/{snapshot_id}
Verify the SHA-256 checksum of a single snapshot.

**Response 200:**
```json
{ "data": { "snapshot_id": 88, "result": "ok" } }
```
`result`: `"ok"` | `"mismatch"` | `"missing"`.

---

### POST /api/integrity/verify-all
Verify all snapshots with stored checksums (background task).

**Response 202:**
```json
{ "data": { "started": true } }
```

---

### GET /api/integrity/status
Get the result of the most recent verify-all run.

**Response 200:**
```json
{
  "data": {
    "total": 142,
    "ok": 141,
    "mismatch": 1,
    "missing": 0,
    "last_run_at": "2026-03-19T03:00:00Z"
  }
}
```

---

## Recovery Playbooks

### GET /api/recovery/playbooks
List all recovery playbooks.

**Response 200:** Array of playbooks.

---

### GET /api/recovery/playbooks/{id}
Get a single playbook.

**Response 200:**
```json
{
  "id": 1,
  "server_id": 1,
  "title": "Restore web-prod from snapshot",
  "scenario": "complete_server_loss",
  "steps": "## Step 1\n...",
  "created_at": "...",
  "updated_at": "..."
}
```

---

### POST /api/recovery/playbooks/generate/{server_id}
Generate recovery playbooks for a server using LLM context.

**Response 201:** Array of generated playbooks.

---

### PUT /api/recovery/playbooks/{id}
Update a playbook (edit steps).

**Response 200:** Updated playbook.

---

### DELETE /api/recovery/playbooks/{id}
Delete a playbook.

**Response 204** No Content.

---

## AI Assistant

### POST /api/assistant/chat
Send a message to the AI assistant.

**Request body:**
```json
{ "message": "What failed in the last backup run?" }
```

**Response 200:**
```json
{
  "data": {
    "id": 42,
    "role": "assistant",
    "content": "The last run for job 'nightly-web' failed because...",
    "created_at": "2026-03-19T14:00:00Z"
  }
}
```

If `assistant_api_key` is not configured, returns a 200 with a guidance message.

---

### GET /api/assistant/conversations
Get conversation history for the authenticated user.

**Response 200:** Array of `Message` objects ordered by time.

---

### DELETE /api/assistant/conversations
Clear all conversation history for the authenticated user.

**Response 204** No Content.

---

## Documentation

**Public** (no auth required).

### GET /api/docs
List all documentation articles.

**Response 200:**
```json
[{ "slug": "getting-started", "title": "Getting Started" }, ...]
```

---

### GET /api/docs/{slug}
Get a single doc article by slug.

**Response 200:**
```json
{ "slug": "getting-started", "title": "Getting Started", "content": "# Getting Started\n..." }
```

**404** if slug not found.

---

### GET /api/docs/search?q=…
Search documentation articles.

**Query parameters:** `q` (search term).

**Response 200:** Array of matching articles.

---

## Settings

### GET /api/settings
Get all settings as a key-value map.

**Response 200:**
```json
{
  "data": {
    "assistant_provider": "openai",
    "assistant_model": "gpt-4o-mini",
    "timezone": "Europe/Rome"
  }
}
```

---

### PUT /api/settings
Update settings. Send only the keys you want to change.

**Request body:**
```json
{ "timezone": "America/New_York", "assistant_provider": "anthropic" }
```

**Response 200:** All settings after update.

---

## Users

Admin-only endpoints.

### GET /api/users
List all users.

**Response 200:** Array of `{ id, username, email, is_admin, created_at }`.

---

### POST /api/users
Create a user.

**Request body:**
```json
{ "username": "jane", "email": "jane@example.com", "password": "pass", "is_admin": false }
```

**Response 201:** Created user.

---

### PUT /api/users/{id}
Update a user.

**Response 200:** Updated user.

---

### DELETE /api/users/{id}
Delete a user.

**Response 204** No Content.

---

## Audit Log

### GET /api/audit
List audit log entries with filtering and pagination.

**Query parameters:** `user_id`, `action`, `from` (datetime), `to` (datetime), `page`, `per_page`.

**Response 200:**
```json
{
  "entries": [
    {
      "id": 1,
      "user_id": 1,
      "action": "DELETE /api/servers/3",
      "target": "3",
      "ip_address": "192.168.1.5",
      "details": "",
      "created_at": "2026-03-19T12:00:00Z"
    }
  ],
  "total": 345,
  "page": 1,
  "per_page": 20
}
```

---

### GET /api/audit/export
Export audit log as CSV (all matching rows, no pagination).

**Response 200:** `text/csv` file download.

---

## WebSocket Endpoints

Not REST. Require the `token` cookie (browser sends automatically).

### WS /ws/logs
Real-time backup run log streaming.

### WS /ws/status
Real-time server health and job status updates.

**Message format:**
```json
{
  "type": "log|status|health",
  "server_id": 1,
  "data": { ... },
  "timestamp": "2026-03-19T14:00:00Z"
}
```
