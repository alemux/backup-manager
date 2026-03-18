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
