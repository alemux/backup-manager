<!-- title: Getting Started -->
<!-- category: Setup -->

# Getting Started

Welcome to BackupManager — a self-hosted solution for automated server backups.
This guide walks you through installation, first login, and adding your first server.

## Installation

BackupManager ships as a single statically-linked binary.

```bash
# Download the latest release
curl -L https://github.com/backupmanager/releases/latest/download/backupmanager -o backupmanager
chmod +x backupmanager

# Or build from source
git clone https://github.com/backupmanager/backupmanager
cd backupmanager
make build
```

## Running the Server

```bash
# Start with default settings (SQLite, port 8080)
./backupmanager serve

# Custom database and port
./backupmanager serve --db /data/backups.db --addr :9000
```

The server will print a one-time admin password on the first run:

```
[INFO] First run detected. Admin password: Xk9mP2vQr8
[INFO] Listening on :8080
```

**Save this password immediately.** It is shown only once.

## First Login

1. Open your browser and navigate to `http://your-server:8080`
2. Log in with username `admin` and the password shown above
3. You will be prompted to change your password

## Changing Your Password

After first login, go to **Settings → Security → Change Password**.
Choose a strong password with at least 12 characters.

## Adding Your First Server

1. Click **Servers** in the left sidebar
2. Click **Add Server**
3. Fill in the connection details:
   - **Hostname or IP** — the address of the server to back up
   - **Connection type** — SSH (Linux) or WinRM (Windows)
   - **Credentials** — username and SSH key or password
4. Click **Test Connection** to verify connectivity
5. Click **Save**

Once saved, BackupManager will auto-discover backup sources (directories, databases)
on the target server.

## Next Steps

- Read **Managing Servers** to learn about connection types and auto-discovery
- Read **Setting Up Schedules** to automate your first backup job
- Read **How Backups Work** to understand incremental backups and deduplication
