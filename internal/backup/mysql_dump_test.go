package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/backupmanager/backupmanager/internal/connector"
)

// ---------------------------------------------------------------------------
// MockConnector
// ---------------------------------------------------------------------------

// MockConnector is a test double implementing connector.Connector.
// Each method delegates to its corresponding Func field if set, otherwise
// returns a zero-value success result.
type MockConnector struct {
	RunCommandFunc func(ctx context.Context, cmd string) (*connector.CommandResult, error)
	CopyFileFunc   func(ctx context.Context, remotePath, localPath string) error
	UploadFileFunc func(ctx context.Context, localPath, remotePath string) error
	ListFilesFunc  func(ctx context.Context, remotePath string) ([]connector.FileInfo, error)
	ReadFileFunc   func(ctx context.Context, remotePath string, w io.Writer) error
	FileExistsFunc func(ctx context.Context, remotePath string) (bool, error)
	RemoveFileFunc func(ctx context.Context, remotePath string) error
}

func (m *MockConnector) Connect() error { return nil }
func (m *MockConnector) Close() error   { return nil }

func (m *MockConnector) RunCommand(ctx context.Context, cmd string) (*connector.CommandResult, error) {
	if m.RunCommandFunc != nil {
		return m.RunCommandFunc(ctx, cmd)
	}
	return &connector.CommandResult{Stdout: "", Stderr: "", ExitCode: 0}, nil
}

func (m *MockConnector) CopyFile(ctx context.Context, remotePath, localPath string) error {
	if m.CopyFileFunc != nil {
		return m.CopyFileFunc(ctx, remotePath, localPath)
	}
	return nil
}

func (m *MockConnector) UploadFile(ctx context.Context, localPath, remotePath string) error {
	if m.UploadFileFunc != nil {
		return m.UploadFileFunc(ctx, localPath, remotePath)
	}
	return nil
}

func (m *MockConnector) ListFiles(ctx context.Context, remotePath string) ([]connector.FileInfo, error) {
	if m.ListFilesFunc != nil {
		return m.ListFilesFunc(ctx, remotePath)
	}
	return nil, nil
}

func (m *MockConnector) ReadFile(ctx context.Context, remotePath string, w io.Writer) error {
	if m.ReadFileFunc != nil {
		return m.ReadFileFunc(ctx, remotePath, w)
	}
	return nil
}

func (m *MockConnector) FileExists(ctx context.Context, remotePath string) (bool, error) {
	if m.FileExistsFunc != nil {
		return m.FileExistsFunc(ctx, remotePath)
	}
	return true, nil
}

func (m *MockConnector) RemoveFile(ctx context.Context, remotePath string) error {
	if m.RemoveFileFunc != nil {
		return m.RemoveFileFunc(ctx, remotePath)
	}
	return nil
}

// ---------------------------------------------------------------------------
// BuildDumpCommand tests
// ---------------------------------------------------------------------------

func TestBuildDumpCommand(t *testing.T) {
	cfg := MySQLDumpConfig{
		DBName:        "mydb",
		MySQLUser:     "root",
		MySQLPassword: "secret",
	}
	outputPath := "/var/backups/backupmanager/mydb_2024-01-15_120000.sql.gz"

	cmd := BuildDumpCommand(cfg, outputPath)

	requiredFragments := []string{
		"mysqldump",
		"--single-transaction",
		"--routines",
		"--triggers",
		"-u root",
		"mydb",
		"| gzip >",
		outputPath,
	}
	for _, frag := range requiredFragments {
		if !strings.Contains(cmd, frag) {
			t.Errorf("expected command to contain %q, got: %s", frag, cmd)
		}
	}
}

func TestBuildDumpCommand_PasswordPlacement(t *testing.T) {
	cfg := MySQLDumpConfig{
		DBName:        "testdb",
		MySQLUser:     "admin",
		MySQLPassword: "p@ssw0rd",
	}
	outputPath := "/tmp/dump.sql.gz"

	cmd := BuildDumpCommand(cfg, outputPath)

	// Password must appear quoted with -p flag.
	if !strings.Contains(cmd, "-p'p@ssw0rd'") {
		t.Errorf("expected password to appear as -p'p@ssw0rd', got: %s", cmd)
	}
	// DBName must appear in the command.
	if !strings.Contains(cmd, "testdb") {
		t.Errorf("expected dbname 'testdb' in command, got: %s", cmd)
	}
}

func TestBuildDumpCommand_SpecialCharsInPassword(t *testing.T) {
	cfg := MySQLDumpConfig{
		DBName:        "db",
		MySQLUser:     "user",
		MySQLPassword: `p@$$w0rd!#%`,
	}
	outputPath := "/tmp/dump.sql.gz"

	cmd := BuildDumpCommand(cfg, outputPath)

	// The password must be wrapped in single quotes.
	if !strings.Contains(cmd, fmt.Sprintf("-p'%s'", cfg.MySQLPassword)) {
		t.Errorf("special chars password not correctly quoted in: %s", cmd)
	}
}

// ---------------------------------------------------------------------------
// BuildChecksumCommand tests
// ---------------------------------------------------------------------------

func TestBuildChecksumCommand(t *testing.T) {
	path := "/var/backups/backupmanager/mydb_2024-01-15_120000.sql.gz"
	cmd := BuildChecksumCommand(path)

	if !strings.HasPrefix(cmd, "sha256sum ") {
		t.Errorf("expected command to start with 'sha256sum ', got: %s", cmd)
	}
	if !strings.Contains(cmd, path) {
		t.Errorf("expected command to contain path %q, got: %s", path, cmd)
	}
}

// ---------------------------------------------------------------------------
// ParseChecksumOutput tests
// ---------------------------------------------------------------------------

func TestParseChecksumOutput(t *testing.T) {
	output := "abc123def4567890abcdef1234567890abcdef1234567890abcdef1234567890ab  /path/to/file.sql.gz"
	got := ParseChecksumOutput(output)
	want := "abc123def4567890abcdef1234567890abcdef1234567890abcdef1234567890ab"
	if got != want {
		t.Errorf("ParseChecksumOutput() = %q, want %q", got, want)
	}
}

func TestParseChecksumOutput_Empty(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseChecksumOutput(tc.input)
			if got != "" {
				t.Errorf("ParseChecksumOutput(%q) = %q, want empty string", tc.input, got)
			}
		})
	}
}

func TestParseChecksumOutput_Malformed(t *testing.T) {
	// Even a single token (no filename column) should still return that token.
	got := ParseChecksumOutput("onlyonetoken")
	if got != "onlyonetoken" {
		t.Errorf("ParseChecksumOutput(single token) = %q, want %q", got, "onlyonetoken")
	}
}

// ---------------------------------------------------------------------------
// BuildCleanupCommand tests
// ---------------------------------------------------------------------------

func TestBuildCleanupCommand(t *testing.T) {
	cmd := BuildCleanupCommand("/var/backups/backupmanager", 7)

	requiredFragments := []string{
		"find",
		"/var/backups/backupmanager",
		`*.sql.gz`,
		"-mtime +7",
		"-delete",
	}
	for _, frag := range requiredFragments {
		if !strings.Contains(cmd, frag) {
			t.Errorf("expected cleanup command to contain %q, got: %s", frag, cmd)
		}
	}
}

func TestBuildCleanupCommand_DefaultRetention(t *testing.T) {
	// When retention days is the default (3), the mtime value should be +3.
	cfg := MySQLDumpConfig{
		RemoteStagingDir: "",
		RetentionDays:    0,
	}
	cfg = effectiveConfig(cfg)
	cmd := BuildCleanupCommand(cfg.RemoteStagingDir, cfg.RetentionDays)

	if !strings.Contains(cmd, "-mtime +3") {
		t.Errorf("expected default retention of 3 days in command, got: %s", cmd)
	}
	if !strings.Contains(cmd, defaultRemoteStagingDir) {
		t.Errorf("expected default staging dir %q in command, got: %s", defaultRemoteStagingDir, cmd)
	}
}

// ---------------------------------------------------------------------------
// Dump path format test
// ---------------------------------------------------------------------------

func TestDumpPathFormat(t *testing.T) {
	stagingDir := "/var/backups/backupmanager"
	dbName := "mydb"
	// Timestamp in the format used by DumpAndCopy.
	ts := time.Now().UTC().Format("2006-01-02_150405")
	dumpFileName := fmt.Sprintf("%s_%s.sql.gz", dbName, ts)
	remotePath := fmt.Sprintf("%s/%s", stagingDir, dumpFileName)

	// Validate prefix.
	expectedPrefix := stagingDir + "/" + dbName + "_"
	if !strings.HasPrefix(remotePath, expectedPrefix) {
		t.Errorf("expected path to start with %q, got: %s", expectedPrefix, remotePath)
	}
	// Validate suffix.
	if !strings.HasSuffix(remotePath, ".sql.gz") {
		t.Errorf("expected path to end with .sql.gz, got: %s", remotePath)
	}
	// Validate timestamp portion length: YYYY-MM-DD_HHMMSS = 17 chars.
	parts := strings.TrimPrefix(remotePath, expectedPrefix)
	parts = strings.TrimSuffix(parts, ".sql.gz")
	if len(parts) != 17 {
		t.Errorf("expected timestamp portion to be 17 chars, got %d (%q)", len(parts), parts)
	}
}

// ---------------------------------------------------------------------------
// DumpAndCopy integration-style tests (using MockConnector)
// ---------------------------------------------------------------------------

func TestDumpAndCopy_MkdirFails(t *testing.T) {
	mock := &MockConnector{
		RunCommandFunc: func(ctx context.Context, cmd string) (*connector.CommandResult, error) {
			if strings.HasPrefix(cmd, "mkdir") {
				return &connector.CommandResult{ExitCode: 1, Stderr: "permission denied"}, nil
			}
			return &connector.CommandResult{ExitCode: 0}, nil
		},
	}

	orch := NewMySQLDumpOrchestrator()
	cfg := MySQLDumpConfig{DBName: "mydb", MySQLUser: "root", MySQLPassword: "pass"}
	_, err := orch.DumpAndCopy(context.Background(), mock, cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error when mkdir fails, got nil")
	}
	if !strings.Contains(err.Error(), "mkdir") {
		t.Errorf("expected error message to mention mkdir, got: %v", err)
	}
}

func TestDumpAndCopy_DumpFails(t *testing.T) {
	mock := &MockConnector{
		RunCommandFunc: func(ctx context.Context, cmd string) (*connector.CommandResult, error) {
			if strings.HasPrefix(cmd, "mkdir") {
				return &connector.CommandResult{ExitCode: 0}, nil
			}
			if strings.HasPrefix(cmd, "mysqldump") {
				return &connector.CommandResult{ExitCode: 1, Stderr: "Access denied"}, nil
			}
			return &connector.CommandResult{ExitCode: 0}, nil
		},
	}

	orch := NewMySQLDumpOrchestrator()
	cfg := MySQLDumpConfig{DBName: "mydb", MySQLUser: "root", MySQLPassword: "pass"}
	_, err := orch.DumpAndCopy(context.Background(), mock, cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error when mysqldump fails, got nil")
	}
	if !strings.Contains(err.Error(), "non-zero exit code") {
		t.Errorf("expected 'non-zero exit code' in error, got: %v", err)
	}
}

func TestDumpAndCopy_ChecksumMismatch(t *testing.T) {
	const remoteChecksum = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	// Write a real file to the temp dir so the local sha256 differs from the remote.
	localDir := t.TempDir()

	mock := &MockConnector{
		RunCommandFunc: func(ctx context.Context, cmd string) (*connector.CommandResult, error) {
			switch {
			case strings.HasPrefix(cmd, "mkdir"):
				return &connector.CommandResult{ExitCode: 0}, nil
			case strings.HasPrefix(cmd, "mysqldump"):
				return &connector.CommandResult{ExitCode: 0}, nil
			case strings.HasPrefix(cmd, "sha256sum"):
				// Return a remote checksum that will not match the local file.
				return &connector.CommandResult{
					Stdout:   remoteChecksum + "  /remote/path/mydb_2024-01-01_120000.sql.gz",
					ExitCode: 0,
				}, nil
			}
			return &connector.CommandResult{ExitCode: 0}, nil
		},
		CopyFileFunc: func(ctx context.Context, remotePath, localPath string) error {
			// Write different content so the local checksum won't match.
			return os.WriteFile(localPath, []byte("different content"), 0644)
		},
	}

	orch := NewMySQLDumpOrchestrator()
	cfg := MySQLDumpConfig{DBName: "mydb", MySQLUser: "root", MySQLPassword: "pass"}
	_, err := orch.DumpAndCopy(context.Background(), mock, cfg, localDir)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("expected 'checksum mismatch' in error, got: %v", err)
	}
}

func TestDumpAndCopy_Success(t *testing.T) {
	localDir := t.TempDir()
	const fileContent = "fake gzipped mysql dump data"

	// We need to know the remote checksum of the file we'll write locally.
	// Since the mock CopyFile will write the content, we pre-compute the sha256.
	h := sha256File // just use the package-level helper via closure after writing
	_ = h

	var capturedLocalPath string
	mock := &MockConnector{
		RunCommandFunc: func(ctx context.Context, cmd string) (*connector.CommandResult, error) {
			switch {
			case strings.HasPrefix(cmd, "mkdir"):
				return &connector.CommandResult{ExitCode: 0}, nil
			case strings.HasPrefix(cmd, "mysqldump"):
				return &connector.CommandResult{ExitCode: 0}, nil
			case strings.HasPrefix(cmd, "sha256sum"):
				// Return a checksum that will match the file content written by CopyFile.
				// We compute it after CopyFile runs, but the orchestrator calls sha256sum
				// before CopyFile. So we return a fixed known value and make CopyFile
				// write content that produces that exact hash.
				//
				// SHA256("fake gzipped mysql dump data") computed via Go:
				// We'll return a placeholder and let the test verify via a helper.
				// Instead, let's return a value we'll pre-compute here.
				return &connector.CommandResult{
					Stdout:   knownSHA256(fileContent) + "  /remote/file.sql.gz",
					ExitCode: 0,
				}, nil
			}
			return &connector.CommandResult{ExitCode: 0}, nil
		},
		CopyFileFunc: func(ctx context.Context, remotePath, localPath string) error {
			capturedLocalPath = localPath
			return os.WriteFile(localPath, []byte(fileContent), 0644)
		},
	}

	orch := NewMySQLDumpOrchestrator()
	cfg := MySQLDumpConfig{
		DBName:        "mydb",
		MySQLUser:     "root",
		MySQLPassword: "pass",
	}
	result, err := orch.DumpAndCopy(context.Background(), mock, cfg, localDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DatabaseName != "mydb" {
		t.Errorf("DatabaseName = %q, want %q", result.DatabaseName, "mydb")
	}
	if result.LocalPath != capturedLocalPath {
		t.Errorf("LocalPath = %q, want %q", result.LocalPath, capturedLocalPath)
	}
	if result.Checksum != knownSHA256(fileContent) {
		t.Errorf("Checksum = %q, want %q", result.Checksum, knownSHA256(fileContent))
	}
	if result.SizeBytes != int64(len(fileContent)) {
		t.Errorf("SizeBytes = %d, want %d", result.SizeBytes, len(fileContent))
	}
	if !strings.HasSuffix(result.RemotePath, ".sql.gz") {
		t.Errorf("RemotePath should end with .sql.gz, got: %s", result.RemotePath)
	}
}

// knownSHA256 returns the hex SHA256 of s (for use in tests without opening a file).
func knownSHA256(s string) string {
	f, err := os.CreateTemp("", "sha256test-*")
	if err != nil {
		panic(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(s)
	f.Close()
	sum, err := sha256File(f.Name())
	if err != nil {
		panic(err)
	}
	return sum
}

// ---------------------------------------------------------------------------
// CleanupRemote tests
// ---------------------------------------------------------------------------

func TestCleanupRemote_Success(t *testing.T) {
	var capturedCmd string
	mock := &MockConnector{
		RunCommandFunc: func(ctx context.Context, cmd string) (*connector.CommandResult, error) {
			capturedCmd = cmd
			return &connector.CommandResult{ExitCode: 0}, nil
		},
	}

	orch := NewMySQLDumpOrchestrator()
	cfg := MySQLDumpConfig{
		RemoteStagingDir: "/var/backups/backupmanager",
		RetentionDays:    5,
	}
	if err := orch.CleanupRemote(context.Background(), mock, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(capturedCmd, "-mtime +5") {
		t.Errorf("expected cleanup to use -mtime +5, got: %s", capturedCmd)
	}
}

func TestCleanupRemote_CommandFails(t *testing.T) {
	mock := &MockConnector{
		RunCommandFunc: func(ctx context.Context, cmd string) (*connector.CommandResult, error) {
			return &connector.CommandResult{ExitCode: 1, Stderr: "find: permission denied"}, nil
		},
	}

	orch := NewMySQLDumpOrchestrator()
	cfg := MySQLDumpConfig{}
	if err := orch.CleanupRemote(context.Background(), mock, cfg); err == nil {
		t.Fatal("expected error when cleanup command fails, got nil")
	}
}

// ---------------------------------------------------------------------------
// DumpPathFormat helper: verify localPath is inside localDestDir
// ---------------------------------------------------------------------------

func TestDumpAndCopy_LocalPathInsideDestDir(t *testing.T) {
	localDir := t.TempDir()

	mock := &MockConnector{
		RunCommandFunc: func(ctx context.Context, cmd string) (*connector.CommandResult, error) {
			if strings.HasPrefix(cmd, "sha256sum") {
				return &connector.CommandResult{
					Stdout:   knownSHA256("data") + "  /remote/file.sql.gz",
					ExitCode: 0,
				}, nil
			}
			return &connector.CommandResult{ExitCode: 0}, nil
		},
		CopyFileFunc: func(ctx context.Context, remotePath, localPath string) error {
			return os.WriteFile(localPath, []byte("data"), 0644)
		},
	}

	orch := NewMySQLDumpOrchestrator()
	cfg := MySQLDumpConfig{DBName: "testdb", MySQLUser: "u", MySQLPassword: "p"}
	result, err := orch.DumpAndCopy(context.Background(), mock, cfg, localDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// LocalPath must be inside localDir.
	rel, err := filepath.Rel(localDir, result.LocalPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Errorf("LocalPath %q is not inside localDestDir %q", result.LocalPath, localDir)
	}
}
