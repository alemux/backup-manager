package connector

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Unit tests — no real FTP server required
// ---------------------------------------------------------------------------

func TestNewFTPConnector_Defaults(t *testing.T) {
	cfg := FTPConfig{
		Host:     "example.com",
		Username: "user",
		Password: "secret",
	}
	c := NewFTPConnector(cfg)

	if c.config.Port != 21 {
		t.Errorf("expected default port 21, got %d", c.config.Port)
	}
	if c.config.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", c.config.Timeout)
	}
	if c.conn != nil {
		t.Error("conn should be nil before Connect()")
	}
}

func TestNewFTPConnector_CustomPortAndTimeout(t *testing.T) {
	cfg := FTPConfig{
		Host:     "example.com",
		Port:     2121,
		Username: "user",
		Password: "pw",
		Timeout:  10 * time.Second,
	}
	c := NewFTPConnector(cfg)

	if c.config.Port != 2121 {
		t.Errorf("expected port 2121, got %d", c.config.Port)
	}
	if c.config.Timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", c.config.Timeout)
	}
}

func TestFTPConnector_ImplementsInterface(t *testing.T) {
	cfg := FTPConfig{Host: "h", Username: "u", Password: "p"}
	var _ Connector = NewFTPConnector(cfg) // compile-time check also in ftp.go
}

// ---------------------------------------------------------------------------
// RunCommand always returns an error
// ---------------------------------------------------------------------------

func TestFTPRunCommand_ReturnsError(t *testing.T) {
	c := NewFTPConnector(FTPConfig{Host: "h", Username: "u", Password: "p"})
	_, err := c.RunCommand(context.Background(), "ls")
	if err == nil {
		t.Fatal("expected error from RunCommand, got nil")
	}
	if !strings.Contains(err.Error(), "FTP does not support command execution") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Connect() failure modes — no real server needed
// ---------------------------------------------------------------------------

func TestFTPConnect_UnreachableHost(t *testing.T) {
	cfg := FTPConfig{
		Host:     "127.0.0.1",
		Port:     1, // port 1 is almost always closed/unreachable
		Username: "user",
		Password: "pw",
		Timeout:  2 * time.Second,
	}
	c := NewFTPConnector(cfg)
	err := c.Connect()
	if err == nil {
		c.Close()
		t.Skip("unexpectedly connected to port 1 — skipping")
	}
}

// ---------------------------------------------------------------------------
// Methods return proper error when not connected
// ---------------------------------------------------------------------------

func TestFTP_NotConnected_CopyFile(t *testing.T) {
	c := &FTPConnector{}
	err := c.CopyFile(context.Background(), "/remote/file", "/local/file")
	if err == nil {
		t.Fatal("expected error when not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFTP_NotConnected_UploadFile(t *testing.T) {
	c := &FTPConnector{}
	err := c.UploadFile(context.Background(), "/local/file", "/remote/file")
	if err == nil {
		t.Fatal("expected error when not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFTP_NotConnected_ListFiles(t *testing.T) {
	c := &FTPConnector{}
	_, err := c.ListFiles(context.Background(), "/remote/dir")
	if err == nil {
		t.Fatal("expected error when not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFTP_NotConnected_ReadFile(t *testing.T) {
	c := &FTPConnector{}
	err := c.ReadFile(context.Background(), "/remote/file", &strings.Builder{})
	if err == nil {
		t.Fatal("expected error when not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFTP_NotConnected_FileExists(t *testing.T) {
	c := &FTPConnector{}
	_, err := c.FileExists(context.Background(), "/remote/file")
	if err == nil {
		t.Fatal("expected error when not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFTP_NotConnected_RemoveFile(t *testing.T) {
	c := &FTPConnector{}
	err := c.RemoveFile(context.Background(), "/remote/file")
	if err == nil {
		t.Fatal("expected error when not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFTP_NotConnected_RunCommand(t *testing.T) {
	c := &FTPConnector{}
	_, err := c.RunCommand(context.Background(), "ls")
	if err == nil {
		t.Fatal("expected error when not connected")
	}
	// RunCommand always returns this error regardless of connection state
	if !strings.Contains(err.Error(), "FTP does not support command execution") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Close() is safe when not connected
// ---------------------------------------------------------------------------

func TestFTP_Close_NotConnected(t *testing.T) {
	c := &FTPConnector{}
	if err := c.Close(); err != nil {
		t.Errorf("Close() on disconnected connector should return nil, got: %v", err)
	}
}
