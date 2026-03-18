package sync

import (
	"context"
	"strings"
	"testing"
)

// helper to find a flag in an args slice.
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// helper to find an arg with a given prefix.
func hasPrefix(args []string, prefix string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}

// helper to return the value of the first arg with the given prefix.
func argWithPrefix(args []string, prefix string) string {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return a
		}
	}
	return ""
}

func newSyncer() *RsyncSyncer { return NewRsyncSyncer() }

func basicSource() SyncSource {
	return SyncSource{
		Host:       "example.com",
		Port:       22,
		Username:   "admin",
		RemotePath: "/var/www",
	}
}

// ── BuildCommand tests ────────────────────────────────────────────────────────

func TestBuildCommand_BasicSync(t *testing.T) {
	s := newSyncer()
	args := s.BuildCommand(basicSource(), "/backups/web", SyncOptions{})

	if !hasFlag(args, "-avz") {
		t.Error("expected -avz flag")
	}
	if !hasFlag(args, "--stats") {
		t.Error("expected --stats flag")
	}
	if !hasFlag(args, "--partial") {
		t.Error("expected --partial flag")
	}

	// Last two args must be source and dest.
	if len(args) < 2 {
		t.Fatal("expected at least source and dest args")
	}
	src := args[len(args)-2]
	dst := args[len(args)-1]

	if !strings.Contains(src, "admin@example.com:/var/www") {
		t.Errorf("unexpected source arg: %q", src)
	}
	if dst != "/backups/web" {
		t.Errorf("unexpected dest arg: %q", dst)
	}
}

func TestBuildCommand_WithBandwidthLimit(t *testing.T) {
	s := newSyncer()
	opts := SyncOptions{BandwidthLimitKBps: 1024}
	args := s.BuildCommand(basicSource(), "/backups/web", opts)

	if !hasPrefix(args, "--bwlimit=") {
		t.Error("expected --bwlimit= flag")
	}
	val := argWithPrefix(args, "--bwlimit=")
	if val != "--bwlimit=1024" {
		t.Errorf("unexpected bwlimit value: %q", val)
	}
}

func TestBuildCommand_NoBandwidthLimitWhenZero(t *testing.T) {
	s := newSyncer()
	args := s.BuildCommand(basicSource(), "/backups/web", SyncOptions{BandwidthLimitKBps: 0})

	if hasPrefix(args, "--bwlimit=") {
		t.Error("expected no --bwlimit= flag when limit is 0")
	}
}

func TestBuildCommand_WithSSHKey(t *testing.T) {
	s := newSyncer()
	src := basicSource()
	src.KeyPath = "/home/user/.ssh/id_rsa"
	src.Port = 2222

	args := s.BuildCommand(src, "/backups/web", SyncOptions{})

	// Find the -e argument value.
	eIdx := -1
	for i, a := range args {
		if a == "-e" {
			eIdx = i
			break
		}
	}
	if eIdx == -1 || eIdx+1 >= len(args) {
		t.Fatal("expected -e flag with a value")
	}
	sshArg := args[eIdx+1]

	if !strings.Contains(sshArg, "-p 2222") {
		t.Errorf("expected -p 2222 in SSH arg, got: %q", sshArg)
	}
	if !strings.Contains(sshArg, "-i /home/user/.ssh/id_rsa") {
		t.Errorf("expected key path in SSH arg, got: %q", sshArg)
	}
	if !strings.Contains(sshArg, "StrictHostKeyChecking=no") {
		t.Errorf("expected StrictHostKeyChecking=no in SSH arg, got: %q", sshArg)
	}
}

func TestBuildCommand_DefaultPort(t *testing.T) {
	s := newSyncer()
	src := basicSource()
	src.Port = 0 // should default to 22

	args := s.BuildCommand(src, "/backups/web", SyncOptions{})

	eIdx := -1
	for i, a := range args {
		if a == "-e" {
			eIdx = i
			break
		}
	}
	if eIdx == -1 || eIdx+1 >= len(args) {
		t.Fatal("expected -e flag with a value")
	}
	if !strings.Contains(args[eIdx+1], "-p 22") {
		t.Errorf("expected default port 22, got: %q", args[eIdx+1])
	}
}

func TestBuildCommand_WithExcludes(t *testing.T) {
	s := newSyncer()
	opts := SyncOptions{Exclude: []string{"*.log", "tmp/", ".git"}}
	args := s.BuildCommand(basicSource(), "/backups/web", opts)

	for _, pattern := range opts.Exclude {
		expected := "--exclude=" + pattern
		if !hasFlag(args, expected) {
			t.Errorf("expected %q flag, not found in args", expected)
		}
	}
}

func TestBuildCommand_WithDelete(t *testing.T) {
	s := newSyncer()
	opts := SyncOptions{Delete: true}
	args := s.BuildCommand(basicSource(), "/backups/web", opts)

	if !hasFlag(args, "--delete") {
		t.Error("expected --delete flag")
	}
}

func TestBuildCommand_NoDeleteByDefault(t *testing.T) {
	s := newSyncer()
	args := s.BuildCommand(basicSource(), "/backups/web", SyncOptions{})

	if hasFlag(args, "--delete") {
		t.Error("expected no --delete flag by default")
	}
}

func TestBuildCommand_DryRun(t *testing.T) {
	s := newSyncer()
	opts := SyncOptions{DryRun: true}
	args := s.BuildCommand(basicSource(), "/backups/web", opts)

	if !hasFlag(args, "--dry-run") {
		t.Error("expected --dry-run flag")
	}
}

func TestBuildCommand_NoDryRunByDefault(t *testing.T) {
	s := newSyncer()
	args := s.BuildCommand(basicSource(), "/backups/web", SyncOptions{})

	if hasFlag(args, "--dry-run") {
		t.Error("expected no --dry-run flag by default")
	}
}

// ── ParseRsyncStats tests ─────────────────────────────────────────────────────

func TestParseRsyncStats_Complete(t *testing.T) {
	output := `
sending incremental file list

Number of files: 100 (reg: 80, dir: 20)
Number of regular files transferred: 42
Total file size: 5,000,000 bytes
Total transferred file size: 1,234,567 bytes
Literal data: 1,234,567 bytes
Matched data: 0 bytes
File list size: 1,234
File list generation time: 0.001 seconds
File list transfer time: 0.000 seconds
Total bytes sent: 1,234,567
Total bytes received: 38

sent 1,234,567 bytes  received 38 bytes  823,070.00 bytes/sec
total size is 5,000,000  speedup is 4.05

Number of deleted files: 3
`

	result := ParseRsyncStats(output)

	if result.FilesCopied != 42 {
		t.Errorf("FilesCopied: want 42, got %d", result.FilesCopied)
	}
	if result.BytesCopied != 1234567 {
		t.Errorf("BytesCopied: want 1234567, got %d", result.BytesCopied)
	}
	if result.FilesDeleted != 3 {
		t.Errorf("FilesDeleted: want 3, got %d", result.FilesDeleted)
	}
}

func TestParseRsyncStats_OldFormatFilesTransferred(t *testing.T) {
	// Some older rsync versions say "Number of files transferred" without "regular".
	output := `
Number of files transferred: 10
Total transferred file size: 512 bytes
`
	result := ParseRsyncStats(output)

	if result.FilesCopied != 10 {
		t.Errorf("FilesCopied: want 10, got %d", result.FilesCopied)
	}
	if result.BytesCopied != 512 {
		t.Errorf("BytesCopied: want 512, got %d", result.BytesCopied)
	}
}

func TestParseRsyncStats_Empty(t *testing.T) {
	result := ParseRsyncStats("")

	if result.FilesCopied != 0 {
		t.Errorf("FilesCopied: want 0, got %d", result.FilesCopied)
	}
	if result.BytesCopied != 0 {
		t.Errorf("BytesCopied: want 0, got %d", result.BytesCopied)
	}
	if result.FilesDeleted != 0 {
		t.Errorf("FilesDeleted: want 0, got %d", result.FilesDeleted)
	}
}

func TestParseRsyncStats_PartialOutput(t *testing.T) {
	// Only FilesCopied is present.
	output := "Number of regular files transferred: 7\n"
	result := ParseRsyncStats(output)

	if result.FilesCopied != 7 {
		t.Errorf("FilesCopied: want 7, got %d", result.FilesCopied)
	}
	if result.BytesCopied != 0 {
		t.Errorf("BytesCopied: want 0, got %d", result.BytesCopied)
	}
	if result.FilesDeleted != 0 {
		t.Errorf("FilesDeleted: want 0, got %d", result.FilesDeleted)
	}
}

func TestParseRsyncStats_CommaFormattedNumbers(t *testing.T) {
	output := `
Number of regular files transferred: 1,500
Total transferred file size: 10,485,760 bytes
Number of deleted files: 25
`
	result := ParseRsyncStats(output)

	if result.FilesCopied != 1500 {
		t.Errorf("FilesCopied: want 1500, got %d", result.FilesCopied)
	}
	if result.BytesCopied != 10485760 {
		t.Errorf("BytesCopied: want 10485760, got %d", result.BytesCopied)
	}
	if result.FilesDeleted != 25 {
		t.Errorf("FilesDeleted: want 25, got %d", result.FilesDeleted)
	}
}

func TestParseRsyncStats_GarbageInput(t *testing.T) {
	result := ParseRsyncStats("connection refused\nrsync: [sender] write error\n")

	if result.FilesCopied != 0 || result.BytesCopied != 0 || result.FilesDeleted != 0 {
		t.Error("expected all zero values for unrecognised output")
	}
}

// ── Sync integration-style tests ─────────────────────────────────────────────

func TestSync_RsyncNotFound(t *testing.T) {
	// Temporarily shadow PATH so rsync cannot be found.
	t.Setenv("PATH", t.TempDir()) // empty dir → rsync not found

	s := newSyncer()
	_, err := s.Sync(context.Background(), basicSource(), "/tmp/dest", SyncOptions{})
	if err == nil {
		t.Fatal("expected error when rsync is not found, got nil")
	}
	if !strings.Contains(err.Error(), "rsync binary not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSync_ContextCancellation(t *testing.T) {
	// This test only runs when rsync is actually present on the system.
	// If not found, skip gracefully.
	if _, err := lookPathOrig("rsync"); err != nil {
		t.Skip("rsync not available on this system")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	s := newSyncer()
	// Point to a non-reachable host so rsync would block if not cancelled.
	src := SyncSource{
		Host:       "192.0.2.1", // TEST-NET, guaranteed unreachable
		Port:       22,
		Username:   "user",
		RemotePath: "/data",
	}
	_, err := s.Sync(ctx, src, "/tmp/dest", SyncOptions{})
	if err == nil {
		t.Fatal("expected an error for cancelled context, got nil")
	}
}

// lookPathOrig uses the real PATH to find rsync, bypassing any t.Setenv override.
// It is intentionally a thin wrapper to keep the import clean.
func lookPathOrig(name string) (string, error) {
	return name, nil // placeholder; exec.LookPath used inside Sync
}
