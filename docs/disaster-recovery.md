# BackupManager — Disaster Recovery

This document describes what to do when the backup *server* itself fails — the meta-disaster scenario where the system managing your backups needs to be recovered.

---

## What Data Needs to Be Recovered

| Data | Location | Criticality |
|---|---|---|
| SQLite database | `<BM_DATA_DIR>/backupmanager.db` | Critical — contains all server configs, job definitions, snapshot metadata, settings (incl. JWT secret and encryption key) |
| Backup files | `<BM_DATA_DIR>/backups/` | Critical — the actual backed-up data |
| DB self-backups | `<BM_DATA_DIR>/db-backups/` | Recovery aid — 7 daily copies of the database |
| Binary + config | `/usr/local/bin/backupmanager`, `/etc/backupmanager/env` | Easily rebuilt |

---

## Scenario 1: Backup Server Disk Failure (OS/System Disk)

The OS disk failed but the data disk (containing `BM_DATA_DIR`) is intact.

### Recovery steps

1. **Provision a new server** (same or different hardware).
2. **Reinstall the OS** (Ubuntu 22.04/24.04).
3. **Attach or mount the intact data disk** at the same path (e.g., `/var/lib/backupmanager`).
4. **Reinstall BackupManager** following the Deployment Guide.
5. **Restore environment variables** in `/etc/backupmanager/env`. If you saved the original `BM_JWT_SECRET`:
   ```bash
   # /etc/backupmanager/env
   BM_JWT_SECRET=<original secret>
   BM_DATA_DIR=/var/lib/backupmanager
   ```
   If the JWT secret is lost, see **Key Recovery** below.
6. **Start the service**:
   ```bash
   systemctl start backupmanager
   ```
7. The existing database and backup files are picked up automatically.

---

## Scenario 2: Complete Server Loss (All Disks)

Both the OS disk and data disk are unrecoverable.

### Recovery steps

1. **Locate your latest offsite backup** of the data directory. If you followed the deployment guide, you should have a recent copy at `offsite-storage:/backupmanager-meta/`.
2. **Provision a new server** and install BackupManager (follow the Deployment Guide).
3. **Restore the data directory**:
   ```bash
   rsync -av offsite-storage:/backupmanager-meta/ /var/lib/backupmanager/
   chown -R backupmanager:backupmanager /var/lib/backupmanager
   ```
4. **Restore environment variables**. The JWT secret must match the one used when the database was created. It is stored in the `settings` table under key `jwt_secret` — you can retrieve it directly if the DB is readable:
   ```bash
   sqlite3 /var/lib/backupmanager/backupmanager.db \
     "SELECT value FROM settings WHERE key='jwt_secret';"
   ```
5. Set `BM_JWT_SECRET=<value from DB>` in `/etc/backupmanager/env`.
6. **Start the service**.

---

## Recovering the BackupManager Database

### From the self-backup copies

BackupManager writes daily SQLite copies to `<BM_DATA_DIR>/db-backups/`:

```bash
ls -lt /var/lib/backupmanager/db-backups/
# backupmanager_2026-03-19.db
# backupmanager_2026-03-18.db
# ...

# Restore the most recent copy
systemctl stop backupmanager
cp /var/lib/backupmanager/db-backups/backupmanager_2026-03-19.db \
   /var/lib/backupmanager/backupmanager.db
systemctl start backupmanager
```

### From a WAL checkpoint file

SQLite WAL mode writes a `-wal` file alongside the main database. If the main database file is corrupted but the `-wal` file exists, SQLite will replay it on next open. No manual intervention needed — just starting the service will trigger recovery.

### Database corruption check

```bash
sqlite3 /var/lib/backupmanager/backupmanager.db "PRAGMA integrity_check;"
# Should print: ok
```

If integrity check fails:
```bash
# Try to recover what we can
sqlite3 /var/lib/backupmanager/backupmanager.db ".recover" | \
  sqlite3 /var/lib/backupmanager/backupmanager_recovered.db
```

---

## Restoring from Encrypted Backups

If encryption is enabled, snapshot files on disk are AES-256-GCM encrypted. To restore them:

### Prerequisites

You need:
1. The encrypted snapshot file (e.g., `/var/lib/backupmanager/backups/web-prod/web/wordpress/2026-03-19_020001`)
2. The master encryption key

### Retrieving the master key

The master key is stored in the `settings` table as an Argon2-wrapped, hex-encoded blob:

```bash
sqlite3 /var/lib/backupmanager/backupmanager.db \
  "SELECT value FROM settings WHERE key='encryption_master_key';"
```

To unwrap it you need the wrapping password (the one set when encryption was configured). The key derivation uses Argon2id (1 iteration, 64 MB, 4 threads).

If you exported the raw key to a file at any point:
```bash
# The key file contains 32 raw bytes
hexdump -C /path/to/master.key
```

### Decrypting a backup file

Using the BackupManager binary (if available):

```bash
# The binary exposes no standalone decrypt CLI yet.
# Use Go directly:
cat > /tmp/decrypt.go << 'EOF'
package main

import (
    "crypto/aes"
    "crypto/cipher"
    "encoding/hex"
    "fmt"
    "os"
)

func main() {
    keyHex := os.Args[1]   // 32-byte key as hex (64 chars)
    input   := os.Args[2]
    output  := os.Args[3]

    key, _ := hex.DecodeString(keyHex)
    ct, _  := os.ReadFile(input)

    block, _ := aes.NewCipher(key)
    gcm, _   := cipher.NewGCM(block)
    nonce    := ct[:gcm.NonceSize()]
    plain, err := gcm.Open(nil, nonce, ct[gcm.NonceSize():], nil)
    if err != nil {
        fmt.Fprintln(os.Stderr, "decrypt failed:", err)
        os.Exit(1)
    }
    os.WriteFile(output, plain, 0600)
    fmt.Println("decrypted to", output)
}
EOF

go run /tmp/decrypt.go <key-hex-64-chars> encrypted_snapshot.bin decrypted_output
```

**Wire format reminder**: `[12-byte nonce][ciphertext + 16-byte GCM tag]`

---

## Key Recovery Procedures

### JWT secret lost

**Impact**: All current user sessions are invalid; all stored server credentials (passwords) are unreadable.

**Recovery**:
1. If the secret is in the `settings` table, read it:
   ```bash
   sqlite3 backupmanager.db "SELECT value FROM settings WHERE key='jwt_secret';"
   ```
2. If the DB is unavailable, generate a new secret:
   ```bash
   openssl rand -hex 32
   ```
   Set it as `BM_JWT_SECRET` in the environment.
3. After restarting with a new secret, re-enter all server passwords via the UI (they will be re-encrypted with the new credential key derived from the new JWT secret).

### Encryption master key lost (password forgotten)

**Impact**: All encrypted snapshots become unreadable.

**If you exported a key file** before the incident:
```bash
# Import the raw 32-byte key file
hexdump -C master.key   # verify it's 32 bytes
```
Use it directly in the decrypt snippet above.

**If no key file exists and the password is forgotten**: the data is permanently unrecoverable (AES-256-GCM with Argon2id key derivation provides no backdoor). This is why it is critical to:
- Store the wrapping password in a secure password manager.
- Export the raw key to a secure offline location periodically.

---

## Recovery Playbooks

BackupManager can generate LLM-assisted recovery playbooks for each server via:
```
POST /api/recovery/playbooks/generate/{server_id}
```

These playbooks cover common scenarios (complete server loss, database corruption, single service failure) and are stored in the `recovery_playbooks` table. They can be retrieved at any time from the Recovery page — even if the server being recovered is offline.

**Best practice**: After initial setup and after any significant change to server configuration, regenerate recovery playbooks and export them to offline storage (e.g., print or save to a separate secure location). This ensures you have recovery runbooks even if the BackupManager UI is unavailable.

---

## Recovery Checklist

When a disaster occurs, work through this checklist:

- [ ] 1. Identify what failed: OS disk, data disk, both, network, or application
- [ ] 2. Locate the most recent backup of `backupmanager.db` (check `db-backups/` and offsite)
- [ ] 3. Locate the JWT secret (from DB or environment file backup)
- [ ] 4. Provision replacement infrastructure if needed
- [ ] 5. Restore database to new server
- [ ] 6. Restore backup files if data disk was lost
- [ ] 7. Set environment variables (especially `BM_JWT_SECRET`)
- [ ] 8. Start BackupManager and verify it starts cleanly
- [ ] 9. Test connection to each managed server
- [ ] 10. Trigger a manual backup run for each critical server to verify the pipeline works
- [ ] 11. Verify notification channels are working (Settings → Notifications → Test)
- [ ] 12. Update IP/hostname references if the backup server moved
- [ ] 13. Re-enter any server passwords if the JWT secret changed
