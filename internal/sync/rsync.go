package sync

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
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
// what was transferred. If opts.LogFunc is set, each line of output is streamed
// to it in real time. If opts.Tracker is set, the running command is registered
// so it can be stopped externally.
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
		if sshpassPath, lookErr := exec.LookPath("sshpass"); lookErr == nil {
			sshpassArgs := append([]string{"-p", source.Password, "rsync"}, args...)
			cmd = exec.CommandContext(ctx, sshpassPath, sshpassArgs...)
		} else {
			cmd = exec.CommandContext(ctx, "rsync", args...)
			cmd.Env = append(os.Environ(), "RSYNC_PASSWORD="+source.Password)
		}
	} else {
		cmd = exec.CommandContext(ctx, "rsync", args...)
	}

	// Register the command with the process tracker so it can be stopped externally.
	if opts.Tracker != nil {
		opts.Tracker.Set(cmd)
		defer opts.Tracker.Clear()
	}

	// If LogFunc is set, use pipes to stream output line by line.
	// Otherwise, fall back to buffered output for backward compatibility.
	if opts.LogFunc != nil {
		return r.syncWithStreaming(ctx, cmd, opts.LogFunc, start)
	}

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	duration := time.Since(start)
	output := out.String()

	result := ParseRsyncStats(output)
	result.Duration = duration

	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		exitCode := 0
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		if exitCode == 23 || exitCode == 24 {
			result.Errors = append(result.Errors, fmt.Sprintf("rsync partial transfer (exit %d): some files skipped", exitCode))
		} else {
			return result, fmt.Errorf("rsync failed: %v\nOutput: %s", err, output)
		}
	}

	return result, nil
}

// syncWithStreaming runs rsync and streams stdout/stderr line by line to logFn.
// It accumulates the full output for final stats parsing.
func (r *RsyncSyncer) syncWithStreaming(ctx context.Context, cmd *exec.Cmd, logFn func(string), start time.Time) (*SyncResult, error) {
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start rsync: %w", err)
	}

	// Merge stdout and stderr into a combined reader.
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

	result := ParseRsyncStats(output)
	result.Duration = duration

	if waitErr != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		exitCode := 0
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		if exitCode == 23 || exitCode == 24 {
			result.Errors = append(result.Errors, fmt.Sprintf("rsync partial transfer (exit %d): some files skipped", exitCode))
		} else {
			return result, fmt.Errorf("rsync failed: %v\nOutput: %s", waitErr, output)
		}
	}

	return result, nil
}

// DryRun executes rsync with --dry-run --stats and returns what would be transferred.
func (r *RsyncSyncer) DryRun(ctx context.Context, source SyncSource, destPath string, opts SyncOptions) (*DryRunResult, error) {
	opts.DryRun = true
	args := r.BuildCommand(source, destPath, opts)

	if _, err := exec.LookPath("rsync"); err != nil {
		return nil, fmt.Errorf("rsync binary not found: %w", err)
	}

	var cmd *exec.Cmd
	if source.Password != "" {
		if sshpassPath, lookErr := exec.LookPath("sshpass"); lookErr == nil {
			sshpassArgs := append([]string{"-p", source.Password, "rsync"}, args...)
			cmd = exec.CommandContext(ctx, sshpassPath, sshpassArgs...)
		} else {
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
	output := out.String()

	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		exitCode := 0
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		if exitCode != 23 && exitCode != 24 {
			return nil, fmt.Errorf("rsync dry-run failed: %v\nOutput: %s", err, output)
		}
	}

	result := ParseDryRunStats(output)
	return result, nil
}

// ParseDryRunStats extracts dry-run statistics from rsync --dry-run --stats output.
func ParseDryRunStats(output string) *DryRunResult {
	result := &DryRunResult{}

	if m := reTotalFiles.FindStringSubmatch(output); m != nil {
		result.TotalFiles = parseIntField(m[1])
	}
	if m := reFilesCopied.FindStringSubmatch(output); m != nil {
		result.FilesToTransfer = parseIntField(m[1])
	}
	if m := reBytescopied.FindStringSubmatch(output); m != nil {
		result.BytesToTransfer = parseInt64Field(m[1])
	}
	if m := reTotalSize.FindStringSubmatch(output); m != nil {
		result.BytesTotal = parseInt64Field(m[1])
	}

	result.HumanSize = HumanizeBytes(result.BytesToTransfer)
	return result
}

// Compile regex patterns once at package level for efficiency.
var (
	reFilesCopied  = regexp.MustCompile(`Number of (?:regular )?files transferred:\s*([\d,]+)`)
	reBytescopied  = regexp.MustCompile(`Total transferred file size:\s*([\d,]+)`)
	reFilesDeleted = regexp.MustCompile(`Number of deleted files:\s*([\d,]+)`)
	reTotalFiles   = regexp.MustCompile(`Number of files:\s*([\d,]+)`)
	reTotalSize    = regexp.MustCompile(`Total file size:\s*([\d,]+)`)
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
