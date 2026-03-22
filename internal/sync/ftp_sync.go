package sync

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/backupmanager/backupmanager/internal/connector"
	"golang.org/x/time/rate"
)

// FTPSyncer implements Syncer using an FTP connection for incremental backups.
// For FTPS servers, uses `lftp mirror` which handles TLS session reuse correctly.
type FTPSyncer struct{}

// NewFTPSyncer creates a new FTPSyncer.
func NewFTPSyncer() *FTPSyncer { return &FTPSyncer{} }

// manifestFileName is the name of the manifest file stored in the destination
// directory.
const manifestFileName = ".backup_manifest.json"

// Sync connects to the FTP server and mirrors files to destPath.
// Uses lftp mirror as primary method (handles FTPS correctly).
// Falls back to Go FTP library for plain FTP.
func (f *FTPSyncer) Sync(ctx context.Context, source SyncSource, destPath string, opts SyncOptions) (*SyncResult, error) {
	// Try lftp first — it handles FTPS correctly
	if lftpPath, err := exec.LookPath("lftp"); err == nil {
		return f.syncWithLFTP(ctx, lftpPath, source, destPath, opts)
	}

	// Fallback: use Go FTP library (may fail with FTPS TLS session reuse)
	return f.syncWithGoLib(ctx, source, destPath, opts)
}

// syncWithLFTP uses the `lftp mirror` command for reliable FTP/FTPS sync.
// If opts.LogFunc is set, each line of output is streamed in real time.
// If opts.Tracker is set, the running command is registered for external stop.
func (f *FTPSyncer) syncWithLFTP(ctx context.Context, lftpPath string, source SyncSource, destPath string, opts SyncOptions) (*SyncResult, error) {
	start := time.Now()

	// Create destination directory
	if err := os.MkdirAll(destPath, 0o755); err != nil {
		return nil, fmt.Errorf("create dest dir: %w", err)
	}

	remotePath := source.RemotePath
	if remotePath == "" {
		remotePath = "/"
	}

	port := source.Port
	if port == 0 {
		port = 21
	}

	// Build exclude options for lftp mirror
	var excludeArgs string
	for _, pattern := range opts.Exclude {
		excludeArgs += fmt.Sprintf(" --exclude-glob %s", pattern)
	}

	// Quote paths to handle spaces and special characters
	mirrorCmd := fmt.Sprintf(
		`set ssl:verify-certificate no; set ftp:ssl-force true; set ftp:ssl-protect-data true; set ftp:ssl-protect-list true; set net:max-retries 3; mirror --delete --verbose%s "%s" "%s"; quit`,
		excludeArgs, remotePath, destPath,
	)

	cmd := exec.CommandContext(ctx, lftpPath,
		"-u", source.Username+","+source.Password,
		"-p", strconv.Itoa(port),
		"-e", mirrorCmd,
		source.Host,
	)

	// Register the command with the process tracker.
	if opts.Tracker != nil {
		opts.Tracker.Set(cmd)
		defer opts.Tracker.Clear()
	}

	// If LogFunc is set, stream output line by line.
	if opts.LogFunc != nil {
		return f.lftpWithStreaming(ctx, cmd, opts.LogFunc, source, port, remotePath, destPath, start)
	}

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	duration := time.Since(start)
	output := out.String()

	// Log the lftp output for debugging
	log.Printf("[FTP SYNC] lftp mirror command: %s@%s:%d %s -> %s", source.Username, source.Host, port, remotePath, destPath)
	if len(output) > 500 {
		log.Printf("[FTP SYNC] lftp output (first 500 chars): %s", output[:500])
	} else if output != "" {
		log.Printf("[FTP SYNC] lftp output: %s", output)
	} else {
		log.Printf("[FTP SYNC] lftp produced no output")
	}
	if err != nil {
		log.Printf("[FTP SYNC] lftp exit error: %v", err)
	}

	result := parseLFTPMirrorOutput(output)
	result.Duration = duration

	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		result.Errors = append(result.Errors, fmt.Sprintf("lftp mirror warning: %v", err))
	}

	return result, nil
}

// lftpWithStreaming runs lftp with piped output, streaming each line to logFn.
func (f *FTPSyncer) lftpWithStreaming(ctx context.Context, cmd *exec.Cmd, logFn func(string), source SyncSource, port int, remotePath, destPath string, start time.Time) (*SyncResult, error) {
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start lftp: %w", err)
	}

	log.Printf("[FTP SYNC] lftp mirror command: %s@%s:%d %s -> %s", source.Username, source.Host, port, remotePath, destPath)

	// Read stdout and stderr together.
	merged := io.MultiReader(stdoutPipe, stderrPipe)
	var accumulated bytes.Buffer
	scanner := bufio.NewScanner(merged)
	for scanner.Scan() {
		line := scanner.Text()
		accumulated.WriteString(line)
		accumulated.WriteString("\n")
		logFn(line)
	}

	waitErr := cmd.Wait()
	duration := time.Since(start)
	output := accumulated.String()

	result := parseLFTPMirrorOutput(output)
	result.Duration = duration

	if waitErr != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		result.Errors = append(result.Errors, fmt.Sprintf("lftp mirror warning: %v", waitErr))
	}

	return result, nil
}

// parseLFTPMirrorOutput parses lftp mirror --verbose output to extract stats.
// Lines like: "Transferring file `filename'" and "Total: X files, Y bytes"
func parseLFTPMirrorOutput(output string) *SyncResult {
	result := &SyncResult{}

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "Transferring file") || strings.Contains(line, "Getting file") {
			result.FilesCopied++
		}
	}

	// Try to parse "Total: N directories, M files, B bytes transferred"
	reTotal := regexp.MustCompile(`(\d+)\s+files?.*?(\d[\d,]*)\s+bytes?\s+transferred`)
	if m := reTotal.FindStringSubmatch(output); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			result.FilesCopied = n
		}
		bytesStr := strings.ReplaceAll(m[2], ",", "")
		if b, err := strconv.ParseInt(bytesStr, 10, 64); err == nil {
			result.BytesCopied = b
		}
	}

	return result
}

// syncWithGoLib is the fallback using the Go FTP library (for plain FTP).
func (f *FTPSyncer) syncWithGoLib(ctx context.Context, source SyncSource, destPath string, opts SyncOptions) (*SyncResult, error) {
	start := time.Now()
	result := &SyncResult{}

	cfg := connector.FTPConfig{
		Host:     source.Host,
		Port:     source.Port,
		Username: source.Username,
		Password: source.Password,
	}
	conn := connector.NewFTPConnector(cfg)
	if err := conn.Connect(); err != nil {
		return nil, fmt.Errorf("ftp connect: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	// ── 2. List all remote files recursively ──────────────────────────────────
	remoteFiles, err := listAllFiles(ctx, conn, source.RemotePath)
	if err != nil {
		return nil, fmt.Errorf("list remote files: %w", err)
	}

	// Build ManifestEntry slices from the listing (no checksums yet — we let
	// the manifest's mtime/size optimisation avoid unnecessary downloads).
	remoteEntries := make([]ManifestEntry, 0, len(remoteFiles))
	for _, fi := range remoteFiles {
		remoteEntries = append(remoteEntries, ManifestEntry{
			Path:    fi.Path,
			Size:    fi.Size,
			ModTime: fi.ModTime,
			// Checksum is deliberately empty here; Compare uses mtime+size when
			// available, and we only compute checksums after downloading.
		})
	}

	// ── 3. Load (or create) local manifest ───────────────────────────────────
	manifestPath := filepath.Join(destPath, manifestFileName)
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("load manifest: %w", err)
	}

	// ── 4. Compare remote listing with manifest ───────────────────────────────
	newFiles, modified, deleted := manifest.Compare(remoteEntries)

	toDownload := make([]string, 0, len(newFiles)+len(modified))
	toDownload = append(toDownload, newFiles...)
	toDownload = append(toDownload, modified...)

	// Build a lookup map for remote file metadata.
	remoteByPath := make(map[string]connector.FileInfo, len(remoteFiles))
	for _, fi := range remoteFiles {
		remoteByPath[fi.Path] = fi
	}

	// ── 5. Download new / modified files ─────────────────────────────────────
	var limiter *rate.Limiter
	if opts.BandwidthLimitKBps > 0 {
		// Convert KB/s to bytes/s. The burst must be at least as large as the
		// largest single Read call the OS may issue (typically 32 KB) so that
		// WaitN never returns an error due to n > burst.
		bytesPerSec := opts.BandwidthLimitKBps * 1024
		burst := bytesPerSec // one second of data as maximum burst
		if burst < 32*1024 {
			burst = 32 * 1024
		}
		limiter = rate.NewLimiter(rate.Limit(bytesPerSec), burst)
	}

	for _, remotePath := range toDownload {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		if opts.DryRun {
			result.FilesCopied++
			if fi, ok := remoteByPath[remotePath]; ok {
				result.BytesCopied += fi.Size
			}
			continue
		}

		localPath := localFilePath(destPath, source.RemotePath, remotePath)

		checksum, bytesWritten, dlErr := downloadFile(ctx, conn, remotePath, localPath, limiter)
		if dlErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("download %q: %v", remotePath, dlErr))
			continue
		}

		result.FilesCopied++
		result.BytesCopied += bytesWritten

		// Update manifest entry with verified checksum.
		fi := remoteByPath[remotePath]
		manifest.Entries[remotePath] = ManifestEntry{
			Path:     remotePath,
			Size:     fi.Size,
			ModTime:  fi.ModTime,
			Checksum: checksum,
		}
	}

	// ── 6. Track deleted files ────────────────────────────────────────────────
	if opts.Delete {
		for _, p := range deleted {
			delete(manifest.Entries, p)
			result.FilesDeleted++
		}
	} else {
		// Even without deletion we mark them as deleted in the result count so
		// the caller is aware. Manifest entries are kept until Delete is enabled.
		result.FilesDeleted = len(deleted)
	}

	// ── 7 & 8. Update and save manifest ──────────────────────────────────────
	// Ensure unchanged remote files (not downloaded) remain in the manifest.
	for _, re := range remoteEntries {
		if _, alreadyUpdated := manifest.Entries[re.Path]; !alreadyUpdated {
			// File exists in manifest from a previous run; keep it as-is.
			// (The entry was already loaded from disk.)
		}
	}

	if !opts.DryRun {
		if err := os.MkdirAll(destPath, 0o750); err != nil {
			return result, fmt.Errorf("create dest dir %q: %w", destPath, err)
		}
		if err := manifest.Save(manifestPath); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("save manifest: %v", err))
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// listAllFiles walks the remote directory tree and returns FileInfo for all
// non-directory entries.
func listAllFiles(ctx context.Context, conn *connector.FTPConnector, root string) ([]connector.FileInfo, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	entries, err := conn.ListFiles(ctx, root)
	if err != nil {
		return nil, err
	}

	var files []connector.FileInfo
	for _, e := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if e.IsDir {
			sub, err := listAllFiles(ctx, conn, e.Path)
			if err != nil {
				return nil, err
			}
			files = append(files, sub...)
		} else {
			files = append(files, e)
		}
	}
	return files, nil
}

// localFilePath converts a remote absolute path to a local destination path.
// It strips the remote root prefix so the directory structure is preserved
// relative to destPath.
func localFilePath(destPath, remoteRoot, remotePath string) string {
	rel, err := filepath.Rel(remoteRoot, remotePath)
	if err != nil || rel == "" {
		// Fallback: use the base name only.
		rel = filepath.Base(remotePath)
	}
	return filepath.Join(destPath, rel)
}

// downloadFile downloads remotePath from conn into localPath, optionally
// throttled by limiter, and returns the SHA-256 checksum and byte count.
func downloadFile(ctx context.Context, conn *connector.FTPConnector, remotePath, localPath string, limiter *rate.Limiter) (checksum string, n int64, err error) {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o750); err != nil {
		return "", 0, fmt.Errorf("create local dirs: %w", err)
	}

	f, err := os.Create(localPath)
	if err != nil {
		return "", 0, fmt.Errorf("create local file: %w", err)
	}
	defer f.Close()

	h := sha256.New()

	// Use a pipe so we can hash and write simultaneously while streaming from
	// the FTP connection.
	pr, pw := io.Pipe()

	// ReadFile streams into pw in a goroutine.
	readErr := make(chan error, 1)
	go func() {
		err := conn.ReadFile(ctx, remotePath, pw)
		pw.CloseWithError(err) //nolint:errcheck
		readErr <- err
	}()

	var src io.Reader = pr
	if limiter != nil {
		src = &rateLimitedReader{r: pr, limiter: limiter, ctx: ctx}
	}

	n, err = io.Copy(io.MultiWriter(f, h), src)
	pr.Close() //nolint:errcheck // drain ensures goroutine exits

	if rErr := <-readErr; rErr != nil && err == nil {
		err = rErr
	}
	if err != nil {
		return "", 0, fmt.Errorf("stream remote file: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// calculateFileChecksum computes the SHA-256 hex digest of the file at path.
func calculateFileChecksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %q: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %q: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// rateLimitedReader wraps an io.Reader and enforces a token-bucket rate limit.
type rateLimitedReader struct {
	r       io.Reader
	limiter *rate.Limiter
	ctx     context.Context
}

func (rl *rateLimitedReader) Read(p []byte) (int, error) {
	n, err := rl.r.Read(p)
	if n > 0 {
		// Wait for the limiter to allow n bytes.
		if waitErr := rl.limiter.WaitN(rl.ctx, n); waitErr != nil {
			return n, waitErr
		}
	}
	return n, err
}

// Ensure FTPSyncer satisfies Syncer at compile time.
var _ Syncer = (*FTPSyncer)(nil)
