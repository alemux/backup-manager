package connector

import (
	"context"
	"io"
	"time"
)

// FileInfo represents a remote file's metadata.
type FileInfo struct {
	Path    string
	Size    int64
	ModTime time.Time
	IsDir   bool
}

// CommandResult holds the output of a remote command execution.
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Connector defines the interface for connecting to remote servers.
type Connector interface {
	// Connect establishes the connection to the remote server.
	Connect() error
	// Close terminates the connection.
	Close() error
	// RunCommand executes a command on the remote server.
	RunCommand(ctx context.Context, cmd string) (*CommandResult, error)
	// CopyFile downloads a remote file to a local path.
	CopyFile(ctx context.Context, remotePath, localPath string) error
	// UploadFile uploads a local file to the remote server.
	UploadFile(ctx context.Context, localPath, remotePath string) error
	// ListFiles lists files in a remote directory.
	ListFiles(ctx context.Context, remotePath string) ([]FileInfo, error)
	// ReadFile reads a remote file's contents into a writer.
	ReadFile(ctx context.Context, remotePath string, w io.Writer) error
	// FileExists checks if a remote file exists.
	FileExists(ctx context.Context, remotePath string) (bool, error)
	// RemoveFile deletes a remote file.
	RemoveFile(ctx context.Context, remotePath string) error
}

// SSHConfig holds SSH connection parameters.
type SSHConfig struct {
	Host       string
	Port       int
	Username   string
	Password   string
	PrivateKey string // PEM-encoded private key content
	KeyPath    string // Path to private key file
	Timeout    time.Duration
}

// FTPConfig holds FTP connection parameters.
type FTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	Timeout  time.Duration
	UseTLS   bool // use explicit FTPS (FTP over TLS)
}
