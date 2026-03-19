# BackupManager — Infrastructure Layer

The infrastructure layer encapsulates all external I/O: remote server communication, file transfer, encryption, database access, and integrity verification. It exposes clean interfaces so the core layer can be tested without real connections.

Packages: `internal/connector/`, `internal/sync/`, `internal/encryption/`, `internal/database/`, `internal/integrity/`

---

## SSH Connector (`internal/connector/ssh.go`)

`SSHConnector` implements the `Connector` interface using `golang.org/x/crypto/ssh` and `github.com/pkg/sftp`.

### Connector interface

```go
type Connector interface {
    Connect() error
    Close() error
    RunCommand(ctx context.Context, cmd string) (*CommandResult, error)
    CopyFile(ctx context.Context, remotePath, localPath string) error
    UploadFile(ctx context.Context, localPath, remotePath string) error
    ListFiles(ctx context.Context, remotePath string) ([]FileInfo, error)
    ReadFile(ctx context.Context, remotePath string, w io.Writer) error
    FileExists(ctx context.Context, remotePath string) (bool, error)
    RemoveFile(ctx context.Context, remotePath string) error
}
```

### Authentication methods

Priority order (first available wins):

1. **Inline private key** (`SSHConfig.PrivateKey` — PEM string): parsed with `ssh.ParsePrivateKey`.
2. **Key file path** (`SSHConfig.KeyPath`): loaded from disk, then parsed.
3. **Password** (`SSHConfig.Password`): `ssh.Password` auth method.

At least one method must be provided; otherwise `Connect()` returns an error.

### `RunCommand`

Runs a command via a new SSH session. Context cancellation is supported: if `ctx.Done()` fires, the session is closed, which signals the remote process. The goroutine is waited for before returning.

Returns `CommandResult{ Stdout, Stderr, ExitCode }`. Non-zero exit codes are returned as data (not errors) so callers can inspect them.

### SFTP operations

`Connect()` opens an SSH connection and then opens an SFTP subsystem client on top of it. `CopyFile` and `UploadFile` use the SFTP client for reliable binary file transfer. `MkdirAll` creates intermediate remote directories automatically on upload.

### Host key verification

`HostKeyCallback: ssh.InsecureIgnoreHostKey()` is used. BackupManager operates on a trusted LAN; adding host key management would add significant UX friction without proportional security benefit in this deployment model. The risk is documented in code.

---

## FTP Connector (`internal/connector/ftp.go`)

`FTPConnector` implements `Connector` using `github.com/jlaffaye/ftp` for Windows server support (where SSH is typically unavailable).

### Key methods

- `Connect()`: dials the FTP server, authenticates with username/password.
- `ListFiles(ctx, path)`: calls `FTPClient.List(path)` and converts entries to `[]FileInfo`.
- `ReadFile(ctx, remotePath, w)`: streams the file via `RETR` into the provided `io.Writer`.
- `CopyFile`: internally calls `ReadFile` into a local `os.File`.

FTP does not support `RunCommand`, `UploadFile`, or `RemoveFile` in the current implementation (returns `"not supported"` errors where relevant).

---

## Rsync Sync Engine (`internal/sync/rsync.go`)

`RsyncSyncer` invokes the local `rsync` binary over SSH. It is used for Linux servers.

### Command construction (`BuildCommand`)

```
rsync -avz --stats --partial
      -e "ssh -p <port> [-i <keyPath>] -o StrictHostKeyChecking=no"
      [--bwlimit=<KBps>]
      [--delete]
      [--dry-run]
      [--exclude=<pattern> ...]
      user@host:remotePath
      destPath
```

Flags explained:
- `-a` — archive mode (preserves permissions, timestamps, symlinks)
- `-v` — verbose (enables stats parsing)
- `-z` — compress data in transit
- `--stats` — emit transfer statistics (parsed to get `FilesCopied` and `BytesCopied`)
- `--partial` — keep partial files on interruption (resumable transfers)
- `--bwlimit` — bandwidth cap in KB/s (converted from Mbps by the orchestrator)

### Statistics parsing

`ParseRsyncStats(output)` uses compiled regexes to extract:
- `Number of regular files transferred: N`
- `Total transferred file size: N`
- `Number of deleted files: N`

Returns zero values for any field it cannot parse (rsync output format varies between versions).

### Error handling

`rsync` exits non-zero for real errors. Context cancellation (timeout) is treated as a hard error. Other non-zero exits are recorded in `result.Errors` but the partial statistics are still returned.

---

## FTP Sync Engine (`internal/sync/ftp_sync.go`)

`FTPSyncer` implements incremental backup for FTP (Windows) servers using a local manifest file to detect changes between runs.

### Manifest-based change detection

The manifest (`.backup_manifest.json`) is stored alongside the downloaded files in `destPath`. It contains a map of remote path → `ManifestEntry{Path, Size, ModTime, Checksum}`.

**Comparison logic** (`internal/sync/manifest.go`):
- **New**: path in remote listing, not in manifest → download
- **Modified**: path in both, but `Size` or `ModTime` differs → download
- **Deleted**: path in manifest, not in remote listing → optionally delete

This avoids re-downloading unchanged files on every backup run.

### Download pipeline

For each file to download:

```
FTPConnector.ReadFile(ctx, remotePath) → io.Pipe writer
                                             ↓
                                    rateLimitedReader (if bandwidth limit set)
                                             ↓
                              io.MultiWriter → local file + sha256.New()
```

The SHA-256 checksum is computed during download at no extra I/O cost.

### Rate limiting

`golang.org/x/time/rate` token-bucket limiter. Configured from `SyncOptions.BandwidthLimitKBps`. Burst is set to `max(bytesPerSec, 32KB)` to ensure `WaitN` never errors when the OS issues a large read.

### Manifest persistence

After all downloads complete, `manifest.Save(manifestPath)` atomically writes the updated manifest to disk. Errors here are non-fatal (appended to `result.Errors`).

---

## MySQL Dump Orchestration (`internal/backup/mysql_dump.go`)

`MySQLDumpOrchestrator.DumpAndCopy` performs a full database dump cycle over an existing `Connector` (SSH).

### Flow

```
1. SSH: mkdir -p <RemoteStagingDir>
2. SSH: mysqldump --single-transaction --routines --triggers \
                  -u <user> -p'<password>' <DBName> | gzip > <remote_path>
         remote_path = <RemoteStagingDir>/<DBName>_<timestamp>.sql.gz
3. SSH: sha256sum <remote_path>   → remoteChecksum
4. SFTP: CopyFile(<remote_path>, <localPath>)
5. Local: sha256File(<localPath>) → localChecksum
6. Assert localChecksum == remoteChecksum (data integrity)
7. Return DumpResult{RemotePath, LocalPath, SizeBytes, Checksum, Duration}
```

`--single-transaction` ensures a consistent InnoDB snapshot without a global lock.

### Remote cleanup

`CleanupRemote(ctx, conn, cfg)` runs:
```bash
find <stagingDir> -name "*.sql.gz" -mtime +<retentionDays> -delete
```
Default retention on the remote: 3 days (configurable via `MySQLDumpConfig.RetentionDays`).

---

## AES-256 Encryption (`internal/encryption/`)

### File encryption (`encryption.go`)

**Algorithm**: AES-256-GCM (authenticated encryption).

**Wire format**: `[12-byte random nonce][ciphertext + 16-byte GCM authentication tag]`

```go
func EncryptBytes(plaintext, key []byte) ([]byte, error)
func DecryptBytes(ciphertext, key []byte) ([]byte, error)
func EncryptFile(inputPath, outputPath string, key []byte) error
func DecryptFile(inputPath, outputPath string, key []byte) error
```

The nonce is prepended to the ciphertext so it travels alongside the data. A fresh nonce is generated for every encryption operation (`crypto/rand`).

**Limitation**: `EncryptFile` loads the entire file into memory. Suitable for database dumps and config files (typically well under 1 GB). For larger files, chunked encryption (64 KB chunks with per-chunk nonces) would be needed.

### Key management (`keymanager.go`)

**Master key**: 32 random bytes generated by `crypto/rand`.

**Key wrapping**: the master key is encrypted with a password-derived key using **Argon2id**:

| Argon2id parameter | Value |
|---|---|
| Time (iterations) | 1 |
| Memory | 64 MB (65536 KiB) |
| Threads | 4 |
| Key length | 32 bytes |
| Salt | 16 random bytes |

**Wrapped key format**: `[16-byte salt][12-byte nonce][32-byte encrypted key + 16-byte GCM tag]`

The wrapped key is stored as a hex string in `settings` table under key `encryption_master_key`.

```go
WrapKey(masterKey []byte, password string) ([]byte, error)
UnwrapKey(wrappedKey []byte, password string) ([]byte, error)
```

**Key derivation for server credentials**: the JWT secret is hashed with SHA-256 to produce a 32-byte AES key used to encrypt server passwords in the `servers` table (`encrypted_password` column). This means changing the JWT secret invalidates all stored credentials.

---

## Notification System Details

### Telegram (`internal/notification/telegram.go`)

Uses the Telegram Bot API (`https://api.telegram.org/bot<token>/sendMessage`). Messages are formatted in Markdown (bold labels, monospace values). Anti-flood tracks last-send time per `alertKey` in an in-memory map.

### Email (`internal/notification/email.go`)

Uses Go's `net/smtp` package with `smtp.PlainAuth`. Messages are HTML with a simple table layout. Recipients are a comma-separated list stored in `notifications_config.email_recipients`. Anti-flood is per `alertKey:email`.

---

## Database Layer (`internal/database/`)

### Opening

`database.Open(path)` opens a SQLite database with `mattn/go-sqlite3`. WAL mode is enabled for better concurrent read performance. Foreign keys are enforced via `PRAGMA foreign_keys = ON`.

### Migrations

`db.Migrate()` applies numbered SQL migration files embedded in the binary (`internal/database/migrations/*.sql`). The `schema_migrations` table tracks applied versions. Migrations are idempotent (`CREATE TABLE IF NOT EXISTS`).

### Credential encryption

`internal/database/crypto.go` provides `EncryptString` / `DecryptString` helpers used by `ServersHandler` to store and retrieve server passwords. The encryption key is derived from the JWT secret via SHA-256.

---

## Integrity Verification (`internal/integrity/integrity.go`)

`IntegrityService.VerifySnapshot(snapshotID)`:

1. Loads `snapshot_path` and `checksum_sha256` from the database.
2. Computes `sha256.Sum256` of the file on disk.
3. Compares with the stored checksum.
4. Returns `ok` (match), `mismatch`, or `missing` (file not found).

`VerifyAll()` iterates all snapshots with a stored checksum and calls `VerifySnapshot` for each. Results are aggregated.
