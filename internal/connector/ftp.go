package connector

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

// FTPConnector implements the Connector interface using plain FTP.
// Falls back to the `lftp` command-line tool for FTPS servers that
// require TLS session reuse (not supported by the Go FTP library).
type FTPConnector struct {
	config  FTPConfig
	conn    *ftp.ServerConn
	useLFTP bool // true when Go library failed and lftp is used as fallback
}

// NewFTPConnector creates a new FTPConnector with the given configuration.
// Default port is 21 and default timeout is 30s when not specified.
func NewFTPConnector(config FTPConfig) *FTPConnector {
	if config.Port == 0 {
		config.Port = 21
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	return &FTPConnector{config: config}
}

// Connect dials the FTP server and authenticates with the configured credentials.
// If UseTLS is true, uses explicit FTPS (FTP over TLS).
func (c *FTPConnector) Connect() error {
	addr := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)

	var opts []ftp.DialOption
	opts = append(opts, ftp.DialWithTimeout(c.config.Timeout))

	if c.config.UseTLS {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: true,
			ClientSessionCache: tls.NewLRUClientSessionCache(32),
		}
		opts = append(opts, ftp.DialWithExplicitTLS(tlsConfig))
	}

	conn, err := ftp.Dial(addr, opts...)
	if err != nil {
		if !c.config.UseTLS {
			tlsConfig := &tls.Config{
				InsecureSkipVerify: true,
				ClientSessionCache: tls.NewLRUClientSessionCache(32),
			}
			opts2 := []ftp.DialOption{
				ftp.DialWithTimeout(c.config.Timeout),
				ftp.DialWithExplicitTLS(tlsConfig),
			}
			conn2, err2 := ftp.Dial(addr, opts2...)
			if err2 != nil {
				// Go FTP library failed — check if lftp is available as fallback
				if _, lftpErr := exec.LookPath("lftp"); lftpErr == nil {
					c.useLFTP = true
					return nil // Will use lftp for all operations
				}
				return fmt.Errorf("ftp dial %s: %w (also tried TLS: %v)", addr, err, err2)
			}
			conn = conn2
		} else {
			// FTPS dial failed — check if lftp is available as fallback
			if _, lftpErr := exec.LookPath("lftp"); lftpErr == nil {
				c.useLFTP = true
				return nil // Will use lftp for all operations
			}
			return fmt.Errorf("ftps dial %s: %w", addr, err)
		}
	}

	if err := conn.Login(c.config.Username, c.config.Password); err != nil {
		conn.Quit() //nolint:errcheck
		// Login failed — try lftp
		if _, lftpErr := exec.LookPath("lftp"); lftpErr == nil {
			c.useLFTP = true
			return nil
		}
		return fmt.Errorf("ftp login %s: %w", addr, err)
	}

	c.conn = conn
	return nil
}

// Close terminates the FTP connection.
func (c *FTPConnector) Close() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Quit()
	c.conn = nil
	if err != nil {
		return fmt.Errorf("ftp quit: %w", err)
	}
	return nil
}

// RunCommand is not supported by FTP and always returns an error.
func (c *FTPConnector) RunCommand(_ context.Context, _ string) (*CommandResult, error) {
	return nil, fmt.Errorf("FTP does not support command execution")
}

// CopyFile downloads remotePath from the FTP server to localPath.
// Intermediate local directories are created as needed.
func (c *FTPConnector) CopyFile(ctx context.Context, remotePath, localPath string) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(localPath), 0o750); err != nil {
		return fmt.Errorf("create local dirs for %q: %w", localPath, err)
	}

	resp, err := c.conn.Retr(remotePath)
	if err != nil {
		return fmt.Errorf("ftp retr %q: %w", remotePath, err)
	}
	defer resp.Close()

	localFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create local file %q: %w", localPath, err)
	}
	defer localFile.Close()

	if _, err := io.Copy(localFile, resp); err != nil {
		return fmt.Errorf("copy remote %q to local %q: %w", remotePath, localPath, err)
	}
	return nil
}

// UploadFile uploads localPath to remotePath on the FTP server.
func (c *FTPConnector) UploadFile(ctx context.Context, localPath, remotePath string) error {
	if c.conn == nil {
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

	if err := c.conn.Stor(remotePath, localFile); err != nil {
		return fmt.Errorf("ftp stor %q: %w", remotePath, err)
	}
	return nil
}

// ListFiles returns metadata for all entries in the remote directory.
// ModTime may be zero if the FTP server does not report modification times.
func (c *FTPConnector) ListFiles(ctx context.Context, remotePath string) ([]FileInfo, error) {
	if c.conn == nil {
		// If not connected via Go library, try lftp fallback
		return c.listFilesLFTP(ctx, remotePath)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := c.conn.List(remotePath)
	if err != nil {
		// TLS session reuse issue — try lftp as fallback
		if c.config.UseTLS {
			if files, lftpErr := c.listFilesLFTP(ctx, remotePath); lftpErr == nil {
				return files, nil
			}
		}
		return nil, fmt.Errorf("ftp list %q: %w", remotePath, err)
	}

	files := make([]FileInfo, 0, len(entries))
	for _, e := range entries {
		files = append(files, FileInfo{
			Path:    filepath.Join(remotePath, e.Name),
			Size:    int64(e.Size),
			ModTime: e.Time, // may be zero value if server doesn't provide it
			IsDir:   e.Type == ftp.EntryTypeFolder,
		})
	}
	return files, nil
}

// listFilesLFTP uses the `lftp` command-line tool as a fallback for FTPS
// servers that require TLS session reuse (not supported by the Go FTP library).
func (c *FTPConnector) listFilesLFTP(ctx context.Context, remotePath string) ([]FileInfo, error) {
	lftpPath, err := exec.LookPath("lftp")
	if err != nil {
		return nil, fmt.Errorf("lftp not installed (needed for FTPS): %w", err)
	}

	// Build lftp command — use `ls` for standard long-listing format
	lftpCmd := fmt.Sprintf(
		"set ssl:verify-certificate no; set ftp:ssl-force true; set ftp:ssl-protect-data true; set ftp:ssl-protect-list true; ls %s; quit",
		remotePath,
	)
	port := c.config.Port
	if port == 0 {
		port = 21
	}

	cmd := exec.CommandContext(ctx, lftpPath,
		"-u", c.config.Username+","+c.config.Password,
		"-p", strconv.Itoa(port),
		"-e", lftpCmd,
		c.config.Host,
	)
	var out strings.Builder
	var errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("lftp failed: %v: %s", err, errOut.String())
	}

	output := out.String()
	if output == "" {
		// Empty listing — could be an empty directory or wrong path
		return []FileInfo{}, nil
	}

	// Parse `ls -l` output from lftp
	// Common formats:
	//   drwxr-xr-x  2 user group  4096 Mar 20 12:00 dirname
	//   -rw-r--r--  1 user group  1234 Mar 20 12:00 filename
	//   drwxr-xr-x  2 user group  4096 2026-03-20 12:00 dirname
	var files []FileInfo
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "total") {
			continue
		}

		// Must start with permission string (d, -, l, etc.)
		if len(trimmed) < 10 {
			continue
		}
		firstChar := trimmed[0]
		if firstChar != 'd' && firstChar != '-' && firstChar != 'l' {
			continue
		}

		isDir := firstChar == 'd'
		fields := strings.Fields(trimmed)

		// Find the filename: everything after the 8th field for standard ls,
		// or after the date/time fields. The safest approach: find name after
		// the time pattern (HH:MM or YYYY)
		name := ""
		size := int64(0)

		if len(fields) >= 9 {
			name = strings.Join(fields[8:], " ")
			if s, err := strconv.ParseInt(fields[4], 10, 64); err == nil {
				size = s
			}
		} else if len(fields) >= 4 {
			// Minimal format: permissions size date name
			name = fields[len(fields)-1]
		}

		if name == "" || name == "." || name == ".." {
			continue
		}

		entryPath := remotePath
		if entryPath == "/" || entryPath == "" {
			entryPath = "/" + name
		} else {
			entryPath = strings.TrimRight(entryPath, "/") + "/" + name
		}

		files = append(files, FileInfo{
			Path:  entryPath,
			Size:  size,
			IsDir: isDir,
		})
	}
	return files, nil
}

// ReadFile streams the contents of remotePath into w.
func (c *FTPConnector) ReadFile(ctx context.Context, remotePath string, w io.Writer) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	resp, err := c.conn.Retr(remotePath)
	if err != nil {
		return fmt.Errorf("ftp retr %q: %w", remotePath, err)
	}
	defer resp.Close()

	if _, err := io.Copy(w, resp); err != nil {
		return fmt.Errorf("read remote file %q: %w", remotePath, err)
	}
	return nil
}

// FileExists returns true if remotePath exists on the FTP server.
// It uses FileSize to probe existence; any error is treated as non-existence.
func (c *FTPConnector) FileExists(ctx context.Context, remotePath string) (bool, error) {
	if c.conn == nil {
		return false, fmt.Errorf("not connected")
	}

	if err := ctx.Err(); err != nil {
		return false, err
	}

	_, err := c.conn.FileSize(remotePath)
	if err != nil {
		return false, nil
	}
	return true, nil
}

// RemoveFile deletes remotePath from the FTP server.
func (c *FTPConnector) RemoveFile(ctx context.Context, remotePath string) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := c.conn.Delete(remotePath); err != nil {
		return fmt.Errorf("ftp delete %q: %w", remotePath, err)
	}
	return nil
}

// Ensure FTPConnector satisfies Connector at compile time.
var _ Connector = (*FTPConnector)(nil)
