<!-- title: Managing Servers -->
<!-- category: Setup -->

# Managing Servers

A **server** in BackupManager is any remote machine whose data you want to protect.
BackupManager supports Linux/Unix servers over SSH and Windows servers over WinRM.

## Adding a Server

Navigate to **Servers → Add Server** and complete the wizard:

1. **Name** — a friendly label (e.g. `prod-web-01`)
2. **Hostname / IP** — the address BackupManager will connect to
3. **Connection type** — SSH or WinRM
4. **Port** — defaults to 22 (SSH) or 5985 (WinRM)
5. **Credentials** — see below

### SSH Connections (Linux / macOS)

BackupManager connects using a dedicated service account. You can authenticate with:

- **Password** — stored AES-256 encrypted in the database
- **SSH private key** — paste the PEM-encoded key; the public key must be in `~/.ssh/authorized_keys` on the target

```bash
# Create a dedicated backup user on the target server
sudo useradd -m -s /bin/bash backupmanager
sudo -u backupmanager ssh-keygen -t ed25519 -f ~/.ssh/id_backup -N ""
# Copy the public key to authorized_keys
```

### WinRM Connections (Windows)

Ensure WinRM is enabled on the target:

```powershell
# Run as Administrator on the Windows server
Enable-PSRemoting -Force
Set-Item WSMan:\localhost\Service\Auth\Basic -Value $true
```

BackupManager uses Basic authentication over HTTPS by default.
Set up a valid certificate or trust a self-signed certificate in your CA store.

## Testing the Connection

Click **Test Connection** before saving. BackupManager will:

1. Open a connection to the target
2. Verify credentials
3. Check required tools are installed (`rsync`, `mysqldump`, etc.)
4. Report any issues

## Auto-Discovery

After a server is saved, BackupManager runs auto-discovery to find backup candidates:

- **Directories** — common paths such as `/var/www`, `/home`, `/etc`, `C:\inetpub`
- **MySQL / MariaDB databases** — detected via socket or TCP
- **PostgreSQL databases** — detected via socket
- **Docker volumes** — if Docker is installed

Discovered sources appear in **Servers → \<server\> → Sources**.
You can enable or disable individual sources before creating a job.

## Editing and Deleting Servers

- **Edit** — update connection details or credentials at any time
- **Delete** — removes the server and all associated jobs; existing snapshots are kept

## Server Health

The dashboard shows a health badge for each server:

| Badge | Meaning |
| ----- | ------- |
| Healthy | Last backup succeeded; server reachable |
| Warning | Last backup succeeded; server unreachable for check |
| Error | Last backup failed |
| Unknown | No backup has run yet |

Go to **Servers → \<server\>** to see detailed health history.
