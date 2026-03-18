package backup

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/backupmanager/backupmanager/internal/connector"
)

const (
	defaultRemoteStagingDir = "/var/backups/backupmanager"
	defaultRetentionDays    = 3
	defaultTimeoutMinutes   = 30
)

// MySQLDumpConfig holds configuration for performing a MySQL dump.
type MySQLDumpConfig struct {
	DBName           string
	MySQLUser        string
	MySQLPassword    string
	RemoteStagingDir string // default: /var/backups/backupmanager
	RetentionDays    int    // how many days to keep dumps on remote, default 3
	TimeoutMinutes   int    // per-database timeout, default 30
}

// DumpResult contains the results of a successful dump and copy operation.
type DumpResult struct {
	RemotePath   string // path of dump on remote server
	LocalPath    string // path of dump on local backup machine
	SizeBytes    int64
	Checksum     string // SHA256
	Duration     time.Duration
	DatabaseName string
}

// MySQLDumpOrchestrator orchestrates the dump, copy, and cleanup lifecycle.
type MySQLDumpOrchestrator struct{}

// NewMySQLDumpOrchestrator creates a new MySQLDumpOrchestrator.
func NewMySQLDumpOrchestrator() *MySQLDumpOrchestrator {
	return &MySQLDumpOrchestrator{}
}

// BuildDumpCommand returns the mysqldump command string for the given config and output path.
func BuildDumpCommand(cfg MySQLDumpConfig, outputPath string) string {
	return fmt.Sprintf(
		"mysqldump --single-transaction --routines --triggers -u %s -p'%s' %s | gzip > %s",
		cfg.MySQLUser,
		cfg.MySQLPassword,
		cfg.DBName,
		outputPath,
	)
}

// BuildChecksumCommand returns the sha256sum command string for the given file path.
func BuildChecksumCommand(filePath string) string {
	return fmt.Sprintf("sha256sum %s", filePath)
}

// ParseChecksumOutput extracts the checksum hex string from sha256sum output.
// sha256sum output format: "<hex>  <filename>"
func ParseChecksumOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	parts := strings.Fields(output)
	if len(parts) < 1 {
		return ""
	}
	return parts[0]
}

// BuildCleanupCommand returns the find command to delete old dumps on the remote server.
func BuildCleanupCommand(stagingDir string, retentionDays int) string {
	return fmt.Sprintf(
		`find %s -name "*.sql.gz" -mtime +%d -delete`,
		stagingDir,
		retentionDays,
	)
}

// effectiveConfig returns a copy of cfg with defaults applied for zero-value fields.
func effectiveConfig(cfg MySQLDumpConfig) MySQLDumpConfig {
	if cfg.RemoteStagingDir == "" {
		cfg.RemoteStagingDir = defaultRemoteStagingDir
	}
	if cfg.RetentionDays == 0 {
		cfg.RetentionDays = defaultRetentionDays
	}
	if cfg.TimeoutMinutes == 0 {
		cfg.TimeoutMinutes = defaultTimeoutMinutes
	}
	return cfg
}

// DumpAndCopy performs the full dump flow on a connected server:
//  1. Ensures the remote staging directory exists.
//  2. Executes mysqldump piped through gzip to a timestamped remote path.
//  3. Calculates the remote SHA256 checksum.
//  4. Copies the dump to the local destination directory.
//  5. Calculates and verifies the local checksum against the remote checksum.
//  6. Returns a DumpResult with metadata.
func (o *MySQLDumpOrchestrator) DumpAndCopy(
	ctx context.Context,
	conn connector.Connector,
	cfg MySQLDumpConfig,
	localDestDir string,
) (*DumpResult, error) {
	cfg = effectiveConfig(cfg)

	start := time.Now()

	// 1. Ensure remote staging dir exists.
	mkdirCmd := fmt.Sprintf("mkdir -p %s", cfg.RemoteStagingDir)
	result, err := conn.RunCommand(ctx, mkdirCmd)
	if err != nil {
		return nil, fmt.Errorf("failed to create remote staging dir: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("mkdir -p failed (exit %d): %s", result.ExitCode, result.Stderr)
	}

	// 2. Build the remote dump path with a timestamp.
	timestamp := time.Now().UTC().Format("2006-01-02_150405")
	dumpFileName := fmt.Sprintf("%s_%s.sql.gz", cfg.DBName, timestamp)
	remotePath := fmt.Sprintf("%s/%s", cfg.RemoteStagingDir, dumpFileName)

	// 3. Execute mysqldump with timeout derived from config.
	dumpCmd := BuildDumpCommand(cfg, remotePath)
	dumpCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMinutes)*time.Minute)
	defer cancel()

	result, err = conn.RunCommand(dumpCtx, dumpCmd)
	if err != nil {
		return nil, fmt.Errorf("mysqldump execution failed: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("mysqldump returned non-zero exit code %d: %s", result.ExitCode, result.Stderr)
	}

	// 4. Calculate remote checksum.
	checksumCmd := BuildChecksumCommand(remotePath)
	result, err = conn.RunCommand(ctx, checksumCmd)
	if err != nil {
		return nil, fmt.Errorf("remote checksum command failed: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("sha256sum failed (exit %d): %s", result.ExitCode, result.Stderr)
	}
	remoteChecksum := ParseChecksumOutput(result.Stdout)
	if remoteChecksum == "" {
		return nil, fmt.Errorf("could not parse remote checksum from output: %q", result.Stdout)
	}

	// 5. Copy the dump to local destination.
	localPath := fmt.Sprintf("%s/%s", strings.TrimRight(localDestDir, "/"), dumpFileName)
	if err := conn.CopyFile(ctx, remotePath, localPath); err != nil {
		return nil, fmt.Errorf("failed to copy dump to local destination: %w", err)
	}

	// 6. Calculate local checksum and verify.
	localChecksum, err := sha256File(localPath)
	if err != nil {
		return nil, fmt.Errorf("failed to compute local checksum: %w", err)
	}
	if localChecksum != remoteChecksum {
		return nil, fmt.Errorf("checksum mismatch: remote=%s local=%s", remoteChecksum, localChecksum)
	}

	// 7. Gather local file size.
	fi, err := os.Stat(localPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat local file: %w", err)
	}

	return &DumpResult{
		RemotePath:   remotePath,
		LocalPath:    localPath,
		SizeBytes:    fi.Size(),
		Checksum:     localChecksum,
		Duration:     time.Since(start),
		DatabaseName: cfg.DBName,
	}, nil
}

// CleanupRemote removes dumps older than RetentionDays on the remote server.
func (o *MySQLDumpOrchestrator) CleanupRemote(
	ctx context.Context,
	conn connector.Connector,
	cfg MySQLDumpConfig,
) error {
	cfg = effectiveConfig(cfg)

	cmd := BuildCleanupCommand(cfg.RemoteStagingDir, cfg.RetentionDays)
	result, err := conn.RunCommand(ctx, cmd)
	if err != nil {
		return fmt.Errorf("cleanup command failed: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("cleanup command returned non-zero exit code %d: %s", result.ExitCode, result.Stderr)
	}
	return nil
}

// sha256File computes the SHA256 hex digest of a local file.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
