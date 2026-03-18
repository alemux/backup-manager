//go:build integration

package connector

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Integration tests — require a real SSH server
// Run with: go test -tags integration ./internal/connector/...
// ---------------------------------------------------------------------------

func TestIntegration_ConnectAndRunCommand(t *testing.T) {
	cfg := SSHConfig{
		Host:     "localhost",
		Port:     22,
		Username: "testuser",
		Password: "testpassword",
		Timeout:  10 * time.Second,
	}
	c := NewSSHConnector(cfg)
	if err := c.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	res, err := c.RunCommand(ctx, "echo hello")
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if strings.TrimSpace(res.Stdout) != "hello" {
		t.Errorf("expected 'hello', got %q", res.Stdout)
	}
	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", res.ExitCode)
	}
}

func TestIntegration_FileExists_Missing(t *testing.T) {
	cfg := SSHConfig{
		Host:     "localhost",
		Port:     22,
		Username: "testuser",
		Password: "testpassword",
		Timeout:  10 * time.Second,
	}
	c := NewSSHConnector(cfg)
	if err := c.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	exists, err := c.FileExists(context.Background(), "/nonexistent/path/file.txt")
	if err != nil {
		t.Fatalf("FileExists: %v", err)
	}
	if exists {
		t.Error("expected file to not exist")
	}
}
