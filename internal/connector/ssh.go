package connector

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SSHConnector implements the Connector interface using SSH and SFTP.
type SSHConnector struct {
	config     SSHConfig
	sshClient  *ssh.Client
	sftpClient *sftp.Client
}

// NewSSHConnector creates a new SSHConnector with the given configuration.
func NewSSHConnector(config SSHConfig) *SSHConnector {
	if config.Port == 0 {
		config.Port = 22
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	return &SSHConnector{config: config}
}

// buildAuthMethods constructs the SSH authentication methods from the config.
func buildAuthMethods(config SSHConfig) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	// Private key content takes priority over key file path.
	if config.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(config.PrivateKey))
		if err != nil {
			return nil, fmt.Errorf("parse inline private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	} else if config.KeyPath != "" {
		keyBytes, err := os.ReadFile(config.KeyPath)
		if err != nil {
			return nil, fmt.Errorf("read private key file %q: %w", config.KeyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(keyBytes)
		if err != nil {
			return nil, fmt.Errorf("parse private key file %q: %w", config.KeyPath, err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	if config.Password != "" {
		methods = append(methods, ssh.Password(config.Password))
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("no authentication method provided: supply Password, PrivateKey, or KeyPath")
	}

	return methods, nil
}

// Connect establishes the SSH connection and creates an SFTP sub-system client.
func (c *SSHConnector) Connect() error {
	authMethods, err := buildAuthMethods(c.config)
	if err != nil {
		return fmt.Errorf("build auth methods: %w", err)
	}

	sshCfg := &ssh.ClientConfig{
		User:            c.config.Username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // known risk, acceptable for backup agents
		Timeout:         c.config.Timeout,
	}

	addr := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)
	conn, err := net.DialTimeout("tcp", addr, c.config.Timeout)
	if err != nil {
		return fmt.Errorf("tcp dial %s: %w", addr, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, sshCfg)
	if err != nil {
		conn.Close()
		return fmt.Errorf("ssh handshake %s: %w", addr, err)
	}
	c.sshClient = ssh.NewClient(sshConn, chans, reqs)

	c.sftpClient, err = sftp.NewClient(c.sshClient)
	if err != nil {
		c.sshClient.Close()
		c.sshClient = nil
		return fmt.Errorf("create sftp client: %w", err)
	}

	return nil
}

// Close terminates the SFTP and SSH connections.
func (c *SSHConnector) Close() error {
	var errs []error
	if c.sftpClient != nil {
		if err := c.sftpClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close sftp: %w", err))
		}
		c.sftpClient = nil
	}
	if c.sshClient != nil {
		if err := c.sshClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close ssh: %w", err))
		}
		c.sshClient = nil
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// RunCommand executes cmd on the remote server, respecting ctx cancellation.
// It returns the combined stdout, stderr, and exit code.
func (c *SSHConnector) RunCommand(ctx context.Context, cmd string) (*CommandResult, error) {
	if c.sshClient == nil {
		return nil, fmt.Errorf("not connected")
	}

	session, err := c.sshClient.NewSession()
	if err != nil {
		return nil, fmt.Errorf("new ssh session: %w", err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	// Run command in a goroutine so we can respect context cancellation.
	type result struct {
		err error
	}
	done := make(chan result, 1)
	go func() {
		done <- result{err: session.Run(cmd)}
	}()

	select {
	case <-ctx.Done():
		// Signal the remote process to stop by closing the session.
		session.Close()
		<-done // wait for goroutine to finish
		return nil, ctx.Err()
	case res := <-done:
		session.Close()
		exitCode := 0
		if res.err != nil {
			var exitErr *ssh.ExitError
			if ok := isExitError(res.err, &exitErr); ok {
				exitCode = exitErr.ExitStatus()
			} else {
				return nil, fmt.Errorf("run command %q: %w", cmd, res.err)
			}
		}
		return &CommandResult{
			Stdout:   stdoutBuf.String(),
			Stderr:   stderrBuf.String(),
			ExitCode: exitCode,
		}, nil
	}
}

// isExitError reports whether err is an *ssh.ExitError and, if so, stores it in target.
func isExitError(err error, target **ssh.ExitError) bool {
	if exitErr, ok := err.(*ssh.ExitError); ok {
		*target = exitErr
		return true
	}
	return false
}

// CopyFile downloads remotePath from the server to localPath.
// Intermediate local directories are created as needed.
func (c *SSHConnector) CopyFile(ctx context.Context, remotePath, localPath string) error {
	if c.sftpClient == nil {
		return fmt.Errorf("not connected")
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(localPath), 0o750); err != nil {
		return fmt.Errorf("create local dirs for %q: %w", localPath, err)
	}

	remoteFile, err := c.sftpClient.Open(remotePath)
	if err != nil {
		return fmt.Errorf("open remote file %q: %w", remotePath, err)
	}
	defer remoteFile.Close()

	localFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create local file %q: %w", localPath, err)
	}
	defer localFile.Close()

	if _, err := io.Copy(localFile, remoteFile); err != nil {
		return fmt.Errorf("copy remote %q to local %q: %w", remotePath, localPath, err)
	}
	return nil
}

// UploadFile uploads localPath to remotePath on the server.
// Intermediate remote directories are created as needed.
func (c *SSHConnector) UploadFile(ctx context.Context, localPath, remotePath string) error {
	if c.sftpClient == nil {
		return fmt.Errorf("not connected")
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	localFile, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local file %q: %w", localPath, err)
	}
	defer localFile.Close()

	remoteDir := filepath.Dir(remotePath)
	if err := c.sftpClient.MkdirAll(remoteDir); err != nil {
		return fmt.Errorf("create remote dirs %q: %w", remoteDir, err)
	}

	remoteFile, err := c.sftpClient.Create(remotePath)
	if err != nil {
		return fmt.Errorf("create remote file %q: %w", remotePath, err)
	}
	defer remoteFile.Close()

	if _, err := io.Copy(remoteFile, localFile); err != nil {
		return fmt.Errorf("upload local %q to remote %q: %w", localPath, remotePath, err)
	}
	return nil
}

// ListFiles returns metadata for all entries in remotePath.
func (c *SSHConnector) ListFiles(ctx context.Context, remotePath string) ([]FileInfo, error) {
	if c.sftpClient == nil {
		return nil, fmt.Errorf("not connected")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := c.sftpClient.ReadDir(remotePath)
	if err != nil {
		return nil, fmt.Errorf("list remote dir %q: %w", remotePath, err)
	}

	files := make([]FileInfo, 0, len(entries))
	for _, e := range entries {
		files = append(files, FileInfo{
			Path:    filepath.Join(remotePath, e.Name()),
			Size:    e.Size(),
			ModTime: e.ModTime(),
			IsDir:   e.IsDir(),
		})
	}
	return files, nil
}

// ReadFile streams the contents of remotePath into w.
func (c *SSHConnector) ReadFile(ctx context.Context, remotePath string, w io.Writer) error {
	if c.sftpClient == nil {
		return fmt.Errorf("not connected")
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	remoteFile, err := c.sftpClient.Open(remotePath)
	if err != nil {
		return fmt.Errorf("open remote file %q: %w", remotePath, err)
	}
	defer remoteFile.Close()

	if _, err := io.Copy(w, remoteFile); err != nil {
		return fmt.Errorf("read remote file %q: %w", remotePath, err)
	}
	return nil
}

// FileExists returns true if remotePath exists on the server.
func (c *SSHConnector) FileExists(ctx context.Context, remotePath string) (bool, error) {
	if c.sftpClient == nil {
		return false, fmt.Errorf("not connected")
	}

	if err := ctx.Err(); err != nil {
		return false, err
	}

	_, err := c.sftpClient.Stat(remotePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat remote file %q: %w", remotePath, err)
	}
	return true, nil
}

// RemoveFile deletes remotePath from the server.
func (c *SSHConnector) RemoveFile(ctx context.Context, remotePath string) error {
	if c.sftpClient == nil {
		return fmt.Errorf("not connected")
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := c.sftpClient.Remove(remotePath); err != nil {
		return fmt.Errorf("remove remote file %q: %w", remotePath, err)
	}
	return nil
}

// Ensure SSHConnector satisfies Connector at compile time.
var _ Connector = (*SSHConnector)(nil)
