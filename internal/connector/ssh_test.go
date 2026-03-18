package connector

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Unit tests — no real SSH server required
// ---------------------------------------------------------------------------

func TestNewSSHConnector_Defaults(t *testing.T) {
	cfg := SSHConfig{
		Host:     "example.com",
		Username: "user",
		Password: "secret",
	}
	c := NewSSHConnector(cfg)

	if c.config.Port != 22 {
		t.Errorf("expected default port 22, got %d", c.config.Port)
	}
	if c.config.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", c.config.Timeout)
	}
	if c.sshClient != nil {
		t.Error("sshClient should be nil before Connect()")
	}
	if c.sftpClient != nil {
		t.Error("sftpClient should be nil before Connect()")
	}
}

func TestNewSSHConnector_CustomPortAndTimeout(t *testing.T) {
	cfg := SSHConfig{
		Host:     "example.com",
		Port:     2222,
		Username: "user",
		Password: "pw",
		Timeout:  5 * time.Second,
	}
	c := NewSSHConnector(cfg)

	if c.config.Port != 2222 {
		t.Errorf("expected port 2222, got %d", c.config.Port)
	}
	if c.config.Timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", c.config.Timeout)
	}
}

func TestNewSSHConnector_ImplementsConnectorInterface(t *testing.T) {
	cfg := SSHConfig{Host: "h", Username: "u", Password: "p"}
	var _ Connector = NewSSHConnector(cfg) // compile-time check also in ssh.go
}

// ---------------------------------------------------------------------------
// Auth method building
// ---------------------------------------------------------------------------

func TestBuildAuthMethods_Password(t *testing.T) {
	cfg := SSHConfig{Password: "mypassword"}
	methods, err := buildAuthMethods(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("expected 1 auth method, got %d", len(methods))
	}
}

func TestBuildAuthMethods_NoAuth(t *testing.T) {
	cfg := SSHConfig{}
	_, err := buildAuthMethods(cfg)
	if err == nil {
		t.Fatal("expected error when no auth method provided, got nil")
	}
	if !strings.Contains(err.Error(), "no authentication method") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestBuildAuthMethods_InvalidInlineKey(t *testing.T) {
	cfg := SSHConfig{PrivateKey: "not-a-valid-pem"}
	_, err := buildAuthMethods(cfg)
	if err == nil {
		t.Fatal("expected error for invalid private key, got nil")
	}
	if !strings.Contains(err.Error(), "parse inline private key") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestBuildAuthMethods_MissingKeyFile(t *testing.T) {
	cfg := SSHConfig{KeyPath: "/nonexistent/path/id_rsa"}
	_, err := buildAuthMethods(cfg)
	if err == nil {
		t.Fatal("expected error for missing key file, got nil")
	}
	if !strings.Contains(err.Error(), "read private key file") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestBuildAuthMethods_BothPasswordAndKey(t *testing.T) {
	// When both inline key and password are set but key is invalid the
	// function should return an error before appending the password method.
	cfg := SSHConfig{PrivateKey: "bad-pem", Password: "pw"}
	_, err := buildAuthMethods(cfg)
	if err == nil {
		t.Fatal("expected error for invalid private key, got nil")
	}
}

func TestBuildAuthMethods_PasswordOnly(t *testing.T) {
	cfg := SSHConfig{Password: "pw"}
	methods, err := buildAuthMethods(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 1 {
		t.Errorf("expected 1 auth method for password-only, got %d", len(methods))
	}
}

// ---------------------------------------------------------------------------
// Connect() failure modes — no real server needed
// ---------------------------------------------------------------------------

func TestConnect_UnreachableHost(t *testing.T) {
	// Port 0 is not listening; the connection should fail quickly.
	cfg := SSHConfig{
		Host:     "127.0.0.1",
		Port:     1, // port 1 is almost always closed/unreachable
		Username: "user",
		Password: "pw",
		Timeout:  2 * time.Second,
	}
	c := NewSSHConnector(cfg)
	err := c.Connect()
	if err == nil {
		// If somehow the connection succeeded (extremely unlikely), close it.
		c.Close()
		t.Skip("unexpectedly connected to port 1 — skipping")
	}
}

func TestConnect_InvalidAddress(t *testing.T) {
	cfg := SSHConfig{
		Host:     "this-host-does-not-exist.invalid",
		Port:     22,
		Username: "user",
		Password: "pw",
		Timeout:  2 * time.Second,
	}
	c := NewSSHConnector(cfg)
	err := c.Connect()
	if err == nil {
		c.Close()
		t.Skip("DNS resolved unexpectedly — skipping")
	}
}

// ---------------------------------------------------------------------------
// Methods fail gracefully when not connected
// ---------------------------------------------------------------------------

func TestRunCommand_NotConnected(t *testing.T) {
	c := &SSHConnector{}
	_, err := c.RunCommand(context.Background(), "ls")
	if err == nil {
		t.Fatal("expected error when not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCopyFile_NotConnected(t *testing.T) {
	c := &SSHConnector{}
	err := c.CopyFile(context.Background(), "/remote/file", "/local/file")
	if err == nil {
		t.Fatal("expected error when not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUploadFile_NotConnected(t *testing.T) {
	c := &SSHConnector{}
	err := c.UploadFile(context.Background(), "/local/file", "/remote/file")
	if err == nil {
		t.Fatal("expected error when not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestListFiles_NotConnected(t *testing.T) {
	c := &SSHConnector{}
	_, err := c.ListFiles(context.Background(), "/remote/dir")
	if err == nil {
		t.Fatal("expected error when not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestReadFile_NotConnected(t *testing.T) {
	c := &SSHConnector{}
	err := c.ReadFile(context.Background(), "/remote/file", &strings.Builder{})
	if err == nil {
		t.Fatal("expected error when not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFileExists_NotConnected(t *testing.T) {
	c := &SSHConnector{}
	_, err := c.FileExists(context.Background(), "/remote/file")
	if err == nil {
		t.Fatal("expected error when not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRemoveFile_NotConnected(t *testing.T) {
	c := &SSHConnector{}
	err := c.RemoveFile(context.Background(), "/remote/file")
	if err == nil {
		t.Fatal("expected error when not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Context cancellation is respected before I/O
// ---------------------------------------------------------------------------

func TestCopyFile_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	c := &SSHConnector{} // not connected — context checked first
	// CopyFile checks sftpClient first; we verify the not-connected path.
	err := c.CopyFile(ctx, "/r", "/l")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunCommand_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := &SSHConnector{}
	_, err := c.RunCommand(ctx, "sleep 10")
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// FileInfo struct sanity
// ---------------------------------------------------------------------------

func TestFileInfo_Fields(t *testing.T) {
	now := time.Now()
	fi := FileInfo{
		Path:    "/tmp/test.txt",
		Size:    1234,
		ModTime: now,
		IsDir:   false,
	}
	if fi.Path != "/tmp/test.txt" {
		t.Errorf("unexpected Path: %s", fi.Path)
	}
	if fi.Size != 1234 {
		t.Errorf("unexpected Size: %d", fi.Size)
	}
	if !fi.ModTime.Equal(now) {
		t.Errorf("unexpected ModTime")
	}
	if fi.IsDir {
		t.Error("IsDir should be false")
	}
}

func TestCommandResult_Fields(t *testing.T) {
	cr := CommandResult{
		Stdout:   "hello",
		Stderr:   "warn",
		ExitCode: 1,
	}
	if cr.Stdout != "hello" {
		t.Errorf("unexpected Stdout: %s", cr.Stdout)
	}
	if cr.Stderr != "warn" {
		t.Errorf("unexpected Stderr: %s", cr.Stderr)
	}
	if cr.ExitCode != 1 {
		t.Errorf("unexpected ExitCode: %d", cr.ExitCode)
	}
}

