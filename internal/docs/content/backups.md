<!-- title: How Backups Work -->
<!-- category: Backups -->

# How Backups Work

BackupManager uses an **incremental-forever** strategy: the first backup is a full copy,
and every subsequent run transfers only changed blocks or files.
This keeps storage usage low while allowing fast, granular restores.

## Incremental Strategy

### File Backups (rsync-based)

For directory sources, BackupManager uses `rsync` with hard-links:

1. A full copy is taken on first run
2. Each subsequent run creates a new snapshot directory
3. Unchanged files are hard-linked from the previous snapshot (zero extra disk space)
4. Changed and new files are transferred and stored in the new snapshot

This means every snapshot appears to be a full copy but only stores the delta.

```
snapshots/
  2024-01-01T00:00:00Z/   ← full copy (100 GB)
  2024-01-02T00:00:00Z/   ← hard-links + 2 GB of changes
  2024-01-03T00:00:00Z/   ← hard-links + 500 MB of changes
```

### Database Backups (MySQL / PostgreSQL)

Databases are backed up with logical dumps:

- **MySQL / MariaDB** — `mysqldump --single-transaction --routines --triggers`
- **PostgreSQL** — `pg_dump --format=custom`

Dumps are compressed with `gzip` before storage.
Incremental database backups use binary log shipping when the target supports it.

## Dependency Graph

BackupManager tracks dependencies between sources on the same server.
Before backing up a database, it ensures the containing volume or directory is also captured.

Example dependency chain:

```
/var/lib/mysql (directory)
  └── myapp_db (MySQL database)
      └── depends on /var/lib/mysql being consistent
```

The dependency graph prevents partial backups that could leave data in an inconsistent state.

## Compression and Deduplication

- All transferred data is compressed with `zstd` (level 3 by default)
- Block-level deduplication is applied across snapshots of the same source
- Encryption at rest uses AES-256-GCM with a per-snapshot key derived from the destination key

## Snapshot Lifecycle

```
RUNNING → COMPLETED (success)
        → FAILED    (error; logs attached)
        → PARTIAL   (some sources completed, others failed)
```

Each snapshot records:

- Start and end time
- Bytes transferred and stored
- Per-source status
- Checksum (SHA-256) for integrity verification

## Integrity Verification

Go to **Snapshots → \<snapshot\> → Verify** to run a checksum verification.
BackupManager computes SHA-256 of each stored file and compares it to the value recorded at backup time.
Any mismatch is flagged as a corruption event and triggers a notification.
