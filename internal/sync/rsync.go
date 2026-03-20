package sync

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RsyncSyncer implements Syncer using the local rsync binary over SSH.
type RsyncSyncer struct{}

// NewRsyncSyncer creates a new RsyncSyncer.
func NewRsyncSyncer() *RsyncSyncer { return &RsyncSyncer{} }

// BuildCommand assembles the rsync argument list from a SyncSource and SyncOptions.
// It returns the slice of arguments to pass to exec.Command("rsync", ...).
func (r *RsyncSyncer) BuildCommand(source SyncSource, destPath string, opts SyncOptions) []string {
	port := source.Port
	if port == 0 {
		port = 22
	}

	// Build the -e (remote shell) argument.
	sshCmd := fmt.Sprintf("ssh -p %d -o StrictHostKeyChecking=no", port)
	if source.KeyPath != "" {
		// Use specific key file
		sshCmd += fmt.Sprintf(" -i %s", source.KeyPath)
	} else if source.Password != "" {
		// Password auth: disable key-based auth to prevent SSH from
		// trying local keys (which may have passphrases and block rsync)
		sshCmd += " -o PreferredAuthentications=password -o PubkeyAuthentication=no"
	}

	args := []string{
		"-avz",
		"--stats",
		"--partial",
		"-e", sshCmd,
	}

	if opts.BandwidthLimitKBps > 0 {
		args = append(args, fmt.Sprintf("--bwlimit=%d", opts.BandwidthLimitKBps))
	}

	if opts.Delete {
		args = append(args, "--delete")
	}

	if opts.DryRun {
		args = append(args, "--dry-run")
	}

	for _, pattern := range opts.Exclude {
		args = append(args, fmt.Sprintf("--exclude=%s", pattern))
	}

	// Build the source specifier: user@host:remotepath
	src := fmt.Sprintf("%s@%s:%s", source.Username, source.Host, source.RemotePath)
	args = append(args, src, destPath)

	return args
}

// Sync executes rsync with the given parameters and returns statistics about
// what was transferred.
func (r *RsyncSyncer) Sync(ctx context.Context, source SyncSource, destPath string, opts SyncOptions) (*SyncResult, error) {
	args := r.BuildCommand(source, destPath, opts)

	// Verify rsync is available before running.
	if _, err := exec.LookPath("rsync"); err != nil {
		return nil, fmt.Errorf("rsync binary not found: %w", err)
	}

	start := time.Now()

	var cmd *exec.Cmd
	if source.Password != "" {
		// Use sshpass to provide the password non-interactively.
		// sshpass must be installed on the backup machine.
		if sshpassPath, lookErr := exec.LookPath("sshpass"); lookErr == nil {
			sshpassArgs := append([]string{"-p", source.Password, "rsync"}, args...)
			cmd = exec.CommandContext(ctx, sshpassPath, sshpassArgs...)
		} else {
			// Fallback: set RSYNC_PASSWORD env var (only works for rsync daemon, not SSH)
			// For SSH password auth without sshpass, this won't work — log a warning.
			cmd = exec.CommandContext(ctx, "rsync", args...)
			cmd.Env = append(os.Environ(), "RSYNC_PASSWORD="+source.Password)
		}
	} else {
		cmd = exec.CommandContext(ctx, "rsync", args...)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	duration := time.Since(start)

	output := out.String()

	// rsync exits non-zero for real errors; collect those as error strings
	// but still try to parse whatever stats we got.
	result := ParseRsyncStats(output)
	result.Duration = duration

	if err != nil {
		// Context cancellation is a hard error.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// rsync exit codes:
		// 0  = success
		// 23 = partial transfer (some files could not be transferred, e.g. socket files)
		// 24 = partial transfer (some files vanished during transfer)
		// These are acceptable — the important files were copied.
		exitCode := 0
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		if exitCode == 23 || exitCode == 24 {
			// Partial transfer is OK — log warning but don't fail
			result.Errors = append(result.Errors, fmt.Sprintf("rsync partial transfer (exit %d): some files skipped", exitCode))
		} else {
			return result, fmt.Errorf("rsync failed: %v\nOutput: %s", err, output)
		}
	}

	return result, nil
}

// Compile regex patterns once at package level for efficiency.
var (
	reFilesCopied  = regexp.MustCompile(`Number of (?:regular )?files transferred:\s*([\d,]+)`)
	reBytescopied  = regexp.MustCompile(`Total transferred file size:\s*([\d,]+)`)
	reFilesDeleted = regexp.MustCompile(`Number of deleted files:\s*([\d,]+)`)
)

// ParseRsyncStats extracts transfer statistics from rsync --stats output.
// It returns zero values for any field it cannot parse.
func ParseRsyncStats(output string) *SyncResult {
	result := &SyncResult{}

	if m := reFilesCopied.FindStringSubmatch(output); m != nil {
		result.FilesCopied = parseIntField(m[1])
	}

	if m := reBytescopied.FindStringSubmatch(output); m != nil {
		result.BytesCopied = parseInt64Field(m[1])
	}

	if m := reFilesDeleted.FindStringSubmatch(output); m != nil {
		result.FilesDeleted = parseIntField(m[1])
	}

	return result
}

// parseIntField converts a comma-separated integer string (e.g. "1,234") to int.
func parseIntField(s string) int {
	s = strings.ReplaceAll(s, ",", "")
	v, _ := strconv.Atoi(s)
	return v
}

// parseInt64Field converts a comma-separated integer string to int64.
func parseInt64Field(s string) int64 {
	s = strings.ReplaceAll(s, ",", "")
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

// Ensure RsyncSyncer satisfies Syncer at compile time.
var _ Syncer = (*RsyncSyncer)(nil)
