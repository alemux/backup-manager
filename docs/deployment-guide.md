# BackupManager — Deployment Guide

This guide covers installing BackupManager on Ubuntu 22.04/24.04 as a production service.

---

## Hardware Requirements

| Resource | Minimum | Recommended |
|---|---|---|
| CPU | 1 core | 2+ cores |
| RAM | 512 MB | 2 GB |
| Disk | 10 GB | Size of all backups × retention multiplier |
| Network | LAN access to managed servers | — |
| OS | Ubuntu 22.04 LTS | Ubuntu 24.04 LTS |

BackupManager itself is lightweight. Storage requirements are dominated by backup data volume.

---

## Dependencies

### Runtime dependencies (on the backup server)

```bash
# rsync (required for Linux server backups)
apt-get install -y rsync

# Optional: mysqldump (for database backup verification)
apt-get install -y mysql-client
```

BackupManager bundles the Go runtime — no Go installation needed in production.

---

## Installation from Binary

### 1. Download or build the binary

**From source** (requires Go 1.21+ and Node.js 20+):

```bash
git clone <repo-url> /opt/backupmanager-src
cd /opt/backupmanager-src
make build
cp bin/backupmanager /usr/local/bin/backupmanager
chmod +x /usr/local/bin/backupmanager
```

### 2. Create system user and directories

```bash
# Dedicated system user (no login shell)
useradd --system --no-create-home --shell /usr/sbin/nologin backupmanager

# Data directory
mkdir -p /var/lib/backupmanager/backups
chown -R backupmanager:backupmanager /var/lib/backupmanager
chmod 750 /var/lib/backupmanager
```

### 3. Configure environment variables

Create `/etc/backupmanager/env`:

```bash
mkdir -p /etc/backupmanager
cat > /etc/backupmanager/env << 'EOF'
BM_PORT=8080
BM_DATA_DIR=/var/lib/backupmanager
BM_LOG_LEVEL=info
BM_JWT_SECRET=<generate with: openssl rand -hex 32>
BM_TIMEZONE=Europe/Rome
EOF

chown root:backupmanager /etc/backupmanager/env
chmod 640 /etc/backupmanager/env
```

**Generate a strong JWT secret:**
```bash
openssl rand -hex 32
```

### 4. Create systemd service

Create `/etc/systemd/system/backupmanager.service`:

```ini
[Unit]
Description=BackupManager backup orchestration server
After=network.target
Wants=network-online.target

[Service]
Type=simple
User=backupmanager
Group=backupmanager
EnvironmentFile=/etc/backupmanager/env
ExecStart=/usr/local/bin/backupmanager
WorkingDirectory=/var/lib/backupmanager
Restart=on-failure
RestartSec=5s

# Harden the service
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/lib/backupmanager
ProtectHome=true
PrivateTmp=true

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=backupmanager

[Install]
WantedBy=multi-user.target
```

### 5. Enable and start

```bash
systemctl daemon-reload
systemctl enable backupmanager
systemctl start backupmanager
systemctl status backupmanager
```

### 6. Check logs

```bash
journalctl -u backupmanager -f
```

Look for:
```
BackupManager starting on :8080
```

---

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `BM_PORT` | `8080` | HTTP listen port |
| `BM_DATA_DIR` | `./data` | Root data directory (contains DB and backups) |
| `BM_LOG_LEVEL` | `info` | Log verbosity (`debug`, `info`, `warn`, `error`) |
| `BM_JWT_SECRET` | (auto-generated) | JWT signing secret; if empty, a random secret is generated and stored in the DB. Changing this invalidates all existing sessions AND all encrypted server credentials. |
| `BM_TIMEZONE` | `Local` | IANA timezone for retention day/week/month boundaries (e.g. `Europe/Rome`, `America/New_York`) |

`BM_DATA_DIR` controls both the database path (`<BM_DATA_DIR>/backupmanager.db`) and backup storage (`<BM_DATA_DIR>/backups/`).

---

## First-Run Setup

1. Navigate to `http://<server-ip>:8080`
2. Log in with default credentials: `admin` / `admin`
3. **Immediately change the password**: Settings → Users → Edit admin
4. Add servers via the Servers page
5. Create backup sources for each server
6. Create backup jobs with a cron schedule
7. Configure notification channels (Settings → Notifications)
8. Optionally configure the AI assistant API key (Settings → Assistant)

The startup log will warn if default admin credentials are still in use:
```
WARNING: Default admin credentials are in use. Change your password after first login.
```

---

## Reverse Proxy (Recommended for Production)

Place BackupManager behind Nginx with TLS:

```nginx
server {
    listen 443 ssl http2;
    server_name backup.example.com;

    ssl_certificate     /etc/letsencrypt/live/backup.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/backup.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }
}
```

`proxy_http_version 1.1` and the `Upgrade` header are required for WebSocket support.

---

## Updating

```bash
# 1. Build new binary
cd /opt/backupmanager-src
git pull
make build

# 2. Stop service
systemctl stop backupmanager

# 3. Back up database
cp /var/lib/backupmanager/backupmanager.db \
   /var/lib/backupmanager/backupmanager.db.pre-update-$(date +%Y%m%d)

# 4. Install new binary
cp bin/backupmanager /usr/local/bin/backupmanager

# 5. Start service
systemctl start backupmanager
journalctl -u backupmanager -f
```

Database migrations run automatically on startup.

---

## Backup of BackupManager Itself

BackupManager includes a self-backup mechanism: every day at 03:00 (server local time), the SQLite database file is copied to `<BM_DATA_DIR>/db-backups/`, keeping the last 7 copies.

This is registered in `scheduler.StartSQLiteBackup()` as:
```
0 3 * * *   backup.BackupSQLiteDB(dbPath, backupDir)
```

For additional redundancy, include the data directory in your external backup strategy:
```bash
rsync -av /var/lib/backupmanager/ offsite-storage:/backupmanager-meta/
```

---

## Firewall

BackupManager listens on a single TCP port (default 8080). Restrict access appropriately:

```bash
# Allow only from LAN (192.168.1.0/24) if not using reverse proxy
ufw allow from 192.168.1.0/24 to any port 8080

# Or if using Nginx:
ufw allow 443
ufw deny 8080
```

---

## Troubleshooting

### Service won't start
```bash
journalctl -u backupmanager --no-pager | tail -30
```
Common causes:
- Port 8080 already in use (`ss -tlnp | grep 8080`)
- Data directory not writable by `backupmanager` user
- Invalid `BM_JWT_SECRET` (must be non-empty or omitted)

### Database locked errors
SQLite WAL mode handles concurrent reads well, but only one process may open the DB for writing. Check there is only one running instance:
```bash
ps aux | grep backupmanager
```

### Backups failing
1. Check `GET /api/runs?status=failed` for recent errors.
2. Test server connectivity: `POST /api/servers/test-connection`.
3. Verify `rsync` is installed and in PATH: `which rsync`.
4. Check disk space on the backup server: `df -h /var/lib/backupmanager`.

### JWT secret changed accidentally
If `BM_JWT_SECRET` is changed (or the auto-generated one is lost because the DB was replaced), all sessions are invalidated and all stored server credentials become unreadable. Recovery:
1. Log in fresh (sessions will fail until new login).
2. Re-enter server passwords via the Servers UI (they will be re-encrypted with the new key).

### WebSocket disconnects frequently
Ensure your reverse proxy has a long `proxy_read_timeout` (at least 120s). The server sends pings every 30 seconds; proxies that timeout before 30s will drop the connection.
