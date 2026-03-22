// internal/api/servers_handler.go
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/backupmanager/backupmanager/internal/connector"
	"github.com/backupmanager/backupmanager/internal/database"
	"github.com/backupmanager/backupmanager/internal/discovery"
	"github.com/backupmanager/backupmanager/internal/notification"
	"github.com/backupmanager/backupmanager/internal/recovery"
)

// Server represents a server record returned in API responses.
// encrypted_password and ssh_key_path are never included.
type Server struct {
	ID             int       `json:"id"`
	Name           string    `json:"name"`
	Type           string    `json:"type"`
	Host           string    `json:"host"`
	Port           int       `json:"port"`
	ConnectionType string    `json:"connection_type"`
	Username       string    `json:"username"`
	UseTLS         bool      `json:"use_tls"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// serverRequest is the incoming payload for create/update.
type serverRequest struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	ConnectionType string `json:"connection_type"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	SSHKeyPath     string `json:"ssh_key_path"`
	UseTLS         bool   `json:"use_tls"`
}

// ServersHandler handles all /api/servers routes.
type ServersHandler struct {
	db        *database.Database
	credMgr   *database.CredentialManager
	notifyMgr *notification.Manager
}

// NewServersHandler constructs a ServersHandler without credential encryption.
// Prefer NewServersHandlerWithKey for production use.
func NewServersHandler(db *database.Database) *ServersHandler {
	return &ServersHandler{db: db}
}

// NewServersHandlerWithKey constructs a ServersHandler that encrypts/decrypts
// server credentials using the provided 32-byte AES-256 key.
func NewServersHandlerWithKey(db *database.Database, credKey []byte) *ServersHandler {
	return &ServersHandler{
		db:      db,
		credMgr: database.NewCredentialManager(credKey),
	}
}

// SetNotifyManager attaches a notification manager used by the Rescan endpoint.
func (h *ServersHandler) SetNotifyManager(mgr *notification.Manager) {
	h.notifyMgr = mgr
}

// List handles GET /api/servers
func (h *ServersHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.DB().QueryContext(r.Context(),
		`SELECT id, name, type, host, port, connection_type, COALESCE(username,''), COALESCE(use_tls,0), status, created_at, updated_at
		 FROM servers ORDER BY id ASC`)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query servers")
		return
	}
	defer rows.Close()

	servers := make([]Server, 0)
	for rows.Next() {
		var s Server
		var createdAt, updatedAt string
		var useTLSInt int
		if err := rows.Scan(&s.ID, &s.Name, &s.Type, &s.Host, &s.Port, &s.ConnectionType, &s.Username, &useTLSInt, &s.Status, &createdAt, &updatedAt); err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan server")
			return
		}
		s.UseTLS = useTLSInt != 0
		s.CreatedAt = parseTime(createdAt)
		s.UpdatedAt = parseTime(updatedAt)
		servers = append(servers, s)
	}
	if err := rows.Err(); err != nil {
		Error(w, http.StatusInternalServerError, "row iteration error")
		return
	}

	JSON(w, http.StatusOK, servers)
}

// Create handles POST /api/servers
func (h *ServersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req serverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := validateServerRequest(req); err != "" {
		Error(w, http.StatusBadRequest, err)
		return
	}

	encPassword, err := h.encryptCred(req.Password)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to encrypt password")
		return
	}
	encSSHKey, err := h.encryptCred(req.SSHKeyPath)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to encrypt ssh key")
		return
	}

	useTLSInt := 0
	if req.UseTLS {
		useTLSInt = 1
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := h.db.DB().ExecContext(r.Context(),
		`INSERT INTO servers (name, type, host, port, connection_type, username, encrypted_password, ssh_key_path, use_tls, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'unknown', ?, ?)`,
		req.Name, req.Type, req.Host, req.Port, req.ConnectionType,
		nullableString(req.Username), nullableString(encPassword), nullableString(encSSHKey),
		useTLSInt, now, now,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to create server")
		return
	}

	id, _ := result.LastInsertId()
	server, ok := h.fetchServer(r, int(id))
	if !ok {
		Error(w, http.StatusInternalServerError, "failed to retrieve created server")
		return
	}

	JSON(w, http.StatusCreated, server)
}

// Get handles GET /api/servers/{id}
func (h *ServersHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	server, found := h.fetchServer(r, id)
	if !found {
		Error(w, http.StatusNotFound, "server not found")
		return
	}

	JSON(w, http.StatusOK, server)
}

// Update handles PUT /api/servers/{id}
func (h *ServersHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	var req serverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := validateServerRequest(req); err != "" {
		Error(w, http.StatusBadRequest, err)
		return
	}

	encPassword, err := h.encryptCred(req.Password)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to encrypt password")
		return
	}
	encSSHKey, err := h.encryptCred(req.SSHKeyPath)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to encrypt ssh key")
		return
	}

	updateTLSInt := 0
	if req.UseTLS {
		updateTLSInt = 1
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := h.db.DB().ExecContext(r.Context(),
		`UPDATE servers SET name=?, type=?, host=?, port=?, connection_type=?, username=?, encrypted_password=?, ssh_key_path=?, use_tls=?, updated_at=?
		 WHERE id=?`,
		req.Name, req.Type, req.Host, req.Port, req.ConnectionType,
		nullableString(req.Username), nullableString(encPassword), nullableString(encSSHKey),
		updateTLSInt, now, id,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to update server")
		return
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		Error(w, http.StatusNotFound, "server not found")
		return
	}

	server, found := h.fetchServer(r, id)
	if !found {
		Error(w, http.StatusInternalServerError, "failed to retrieve updated server")
		return
	}

	JSON(w, http.StatusOK, server)
}

// Delete handles DELETE /api/servers/{id}
func (h *ServersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	res, err := h.db.DB().ExecContext(r.Context(), "DELETE FROM servers WHERE id=?", id)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to delete server")
		return
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		Error(w, http.StatusNotFound, "server not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// BrowseFTP handles POST /api/servers/browse-ftp
// Connects to an FTP server and lists the contents of the specified path.
func (h *ServersHandler) BrowseFTP(w http.ResponseWriter, r *http.Request) {
	type browseRequest struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
		UseTLS   bool   `json:"use_tls"`
		Path     string `json:"path"`
	}

	var req browseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Host == "" {
		Error(w, http.StatusBadRequest, "host is required")
		return
	}
	if req.Path == "" {
		req.Path = "/"
	}
	port := req.Port
	if port == 0 {
		port = 21
	}

	conn := connector.NewFTPConnector(connector.FTPConfig{
		Host:     req.Host,
		Port:     port,
		Username: req.Username,
		Password: req.Password,
		Timeout:  15 * time.Second,
		UseTLS:   req.UseTLS,
	})
	if err := conn.Connect(); err != nil {
		Error(w, http.StatusBadGateway, fmt.Sprintf("FTP connection failed: %v", err))
		return
	}
	defer conn.Close()

	files, err := conn.ListFiles(r.Context(), req.Path)
	if err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("FTP list failed: %v", err))
		return
	}

	type ftpEntry struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size"`
	}

	entries := make([]ftpEntry, 0, len(files))
	for _, f := range files {
		// Derive the entry name from the path
		name := f.Path
		// Strip the leading directory prefix to get just the filename
		prefix := req.Path
		if len(prefix) > 0 && prefix[len(prefix)-1] != '/' {
			prefix += "/"
		}
		if len(f.Path) > len(prefix) && f.Path[:len(prefix)] == prefix {
			name = f.Path[len(prefix):]
		}

		// Build canonical entry path
		entryPath := req.Path
		if len(entryPath) == 0 || entryPath[len(entryPath)-1] != '/' {
			entryPath += "/"
		}
		if entryPath == "/" {
			entryPath = "/" + name
		} else {
			entryPath = entryPath + name
		}

		entries = append(entries, ftpEntry{
			Name:  name,
			Path:  entryPath,
			IsDir: f.IsDir,
			Size:  f.Size,
		})
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"path":    req.Path,
		"entries": entries,
	})
}

// BrowseFTPServer handles POST /api/servers/{id}/browse-ftp
// Uses the server's saved (encrypted) credentials to connect and list FTP contents.
// Also returns existing sources and detects new folders not covered by any source.
func (h *ServersHandler) BrowseFTPServer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	// Parse optional path from request body
	type browseReq struct {
		Path string `json:"path"`
	}
	var req browseReq
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req) // ignore decode errors; path defaults to "/"
	}
	if req.Path == "" {
		req.Path = "/"
	}

	// Fetch server credentials
	type serverRow struct {
		host           string
		port           int
		connectionType string
		username       string
		password       sql.NullString
		useTLS         int
	}
	var row serverRow
	err := h.db.DB().QueryRowContext(r.Context(),
		`SELECT host, port, connection_type, COALESCE(username,''), encrypted_password, COALESCE(use_tls,0)
		 FROM servers WHERE id=?`, id,
	).Scan(&row.host, &row.port, &row.connectionType, &row.username, &row.password, &row.useTLS)
	if err == sql.ErrNoRows {
		Error(w, http.StatusNotFound, "server not found")
		return
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query server")
		return
	}

	if row.connectionType != "ftp" {
		Error(w, http.StatusBadRequest, "browse-ftp is only supported for FTP servers")
		return
	}

	password := ""
	if row.password.Valid {
		decPassword, err := h.decryptCred(row.password.String)
		if err != nil {
			Error(w, http.StatusInternalServerError, "failed to decrypt server password")
			return
		}
		password = decPassword
	}

	port := row.port
	if port == 0 {
		port = 21
	}

	conn := connector.NewFTPConnector(connector.FTPConfig{
		Host:     row.host,
		Port:     port,
		Username: row.username,
		Password: password,
		Timeout:  15 * time.Second,
		UseTLS:   row.useTLS != 0,
	})
	if err := conn.Connect(); err != nil {
		Error(w, http.StatusBadGateway, fmt.Sprintf("FTP connection failed: %v", err))
		return
	}
	defer conn.Close()

	files, err := conn.ListFiles(r.Context(), req.Path)
	if err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("FTP list failed: %v", err))
		return
	}

	type ftpEntry struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size"`
	}

	entries := make([]ftpEntry, 0, len(files))
	for _, f := range files {
		name := f.Path
		prefix := req.Path
		if len(prefix) > 0 && prefix[len(prefix)-1] != '/' {
			prefix += "/"
		}
		if len(f.Path) > len(prefix) && f.Path[:len(prefix)] == prefix {
			name = f.Path[len(prefix):]
		}

		entryPath := req.Path
		if len(entryPath) == 0 || entryPath[len(entryPath)-1] != '/' {
			entryPath += "/"
		}
		if entryPath == "/" {
			entryPath = "/" + name
		} else {
			entryPath = entryPath + name
		}

		entries = append(entries, ftpEntry{
			Name:  name,
			Path:  entryPath,
			IsDir: f.IsDir,
			Size:  f.Size,
		})
	}

	// Load existing backup sources for this server
	sourceRows, err := h.db.DB().QueryContext(r.Context(),
		`SELECT COALESCE(source_path,'') FROM backup_sources WHERE server_id=? AND enabled=1`, id)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query sources")
		return
	}
	defer sourceRows.Close()

	existingSources := make([]string, 0)
	isCopyAll := false
	for sourceRows.Next() {
		var sp string
		if err := sourceRows.Scan(&sp); err == nil && sp != "" {
			existingSources = append(existingSources, sp)
			if sp == "/" {
				isCopyAll = true
			}
		}
	}

	// Detect new folders: directories on FTP root that have no matching source
	newFolders := make([]string, 0)
	if req.Path == "/" && !isCopyAll {
		sourceSet := make(map[string]bool, len(existingSources))
		for _, sp := range existingSources {
			sourceSet[sp] = true
		}
		for _, entry := range entries {
			if entry.IsDir && !sourceSet[entry.Path] {
				newFolders = append(newFolders, entry.Path)
			}
		}
	}

	mode := "selective"
	if isCopyAll {
		mode = "copy_all"
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"path":             req.Path,
		"entries":          entries,
		"existing_sources": existingSources,
		"new_folders":      newFolders,
		"mode":             mode,
	})
}

// TestConnection handles POST /api/servers/test-connection
// Tests SSH or FTP connectivity without saving the server.
func (h *ServersHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	type testRequest struct {
		Host           string `json:"host"`
		Port           int    `json:"port"`
		ConnectionType string `json:"connection_type"`
		Username       string `json:"username"`
		Password       string `json:"password"`
		SSHKeyPath     string `json:"ssh_key_path"`
		UseTLS         bool   `json:"use_tls"`
	}

	var req testRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Host == "" {
		Error(w, http.StatusBadRequest, "host is required")
		return
	}
	if req.ConnectionType != "ssh" && req.ConnectionType != "ftp" {
		Error(w, http.StatusBadRequest, "connection_type must be ssh or ftp")
		return
	}

	ctx := r.Context()

	switch req.ConnectionType {
	case "ssh":
		port := req.Port
		if port == 0 {
			port = 22
		}
		conn := connector.NewSSHConnector(connector.SSHConfig{
			Host:       req.Host,
			Port:       port,
			Username:   req.Username,
			Password:   req.Password,
			KeyPath:    req.SSHKeyPath,
			Timeout:    10 * time.Second,
		})
		if err := conn.Connect(); err != nil {
			JSON(w, http.StatusOK, map[string]interface{}{
				"success": false,
				"message": fmt.Sprintf("SSH connection failed: %v", err),
			})
			return
		}
		// Try a simple command to verify the session works
		result, err := conn.RunCommand(ctx, "echo ok")
		conn.Close()
		if err != nil || result.ExitCode != 0 {
			JSON(w, http.StatusOK, map[string]interface{}{
				"success": false,
				"message": "SSH connected but command execution failed",
			})
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("SSH connection to %s:%d successful", req.Host, port),
		})

	case "ftp":
		port := req.Port
		if port == 0 {
			port = 21
		}
		conn := connector.NewFTPConnector(connector.FTPConfig{
			Host:     req.Host,
			Port:     port,
			Username: req.Username,
			Password: req.Password,
			Timeout:  10 * time.Second,
			UseTLS:   req.UseTLS,
		})
		if err := conn.Connect(); err != nil {
			JSON(w, http.StatusOK, map[string]interface{}{
				"success": false,
				"message": fmt.Sprintf("FTP connection failed: %v", err),
			})
			return
		}
		conn.Close()
		JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("FTP connection to %s:%d successful", req.Host, port),
		})
	}
}

// DiscoverPreview handles POST /api/servers/discover-preview
// Connects via SSH using provided credentials (no saved server needed) and runs auto-discovery.
func (h *ServersHandler) DiscoverPreview(w http.ResponseWriter, r *http.Request) {
	type previewRequest struct {
		Host       string `json:"host"`
		Port       int    `json:"port"`
		Username   string `json:"username"`
		Password   string `json:"password"`
		SSHKeyPath string `json:"ssh_key_path"`
	}

	var req previewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Host == "" {
		Error(w, http.StatusBadRequest, "host is required")
		return
	}

	port := req.Port
	if port == 0 {
		port = 22
	}

	conn := connector.NewSSHConnector(connector.SSHConfig{
		Host:     req.Host,
		Port:     port,
		Username: req.Username,
		Password: req.Password,
		KeyPath:  req.SSHKeyPath,
		Timeout:  15 * time.Second,
	})
	if err := conn.Connect(); err != nil {
		Error(w, http.StatusBadGateway, fmt.Sprintf("SSH connection failed: %v", err))
		return
	}
	defer conn.Close()

	discSvc := discovery.NewDiscoveryService(h.db)
	result, err := discSvc.Discover(r.Context(), conn)
	if err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("discovery failed: %v", err))
		return
	}

	JSON(w, http.StatusOK, result)
}

// Discover handles POST /api/servers/{id}/discover
// It connects to the server via SSH, runs auto-discovery, saves results and
// returns the DiscoveryResult as JSON.
func (h *ServersHandler) Discover(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	// Fetch the server row including credentials.
	type serverRow struct {
		host           string
		port           int
		connectionType string
		username       string
		password       sql.NullString
		sshKeyPath     sql.NullString
	}
	var row serverRow
	err := h.db.DB().QueryRowContext(r.Context(),
		`SELECT host, port, connection_type, COALESCE(username,''), encrypted_password, ssh_key_path
		 FROM servers WHERE id=?`, id,
	).Scan(&row.host, &row.port, &row.connectionType, &row.username, &row.password, &row.sshKeyPath)
	if err == sql.ErrNoRows {
		Error(w, http.StatusNotFound, "server not found")
		return
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query server")
		return
	}

	if row.connectionType != "ssh" {
		Error(w, http.StatusBadRequest, "discovery is only supported for SSH servers")
		return
	}

	sshCfg := connector.SSHConfig{
		Host:     row.host,
		Port:     row.port,
		Username: row.username,
	}
	if row.password.Valid {
		decPassword, err := h.decryptCred(row.password.String)
		if err != nil {
			Error(w, http.StatusInternalServerError, "failed to decrypt server password")
			return
		}
		sshCfg.Password = decPassword
	}
	if row.sshKeyPath.Valid {
		decSSHKey, err := h.decryptCred(row.sshKeyPath.String)
		if err != nil {
			Error(w, http.StatusInternalServerError, "failed to decrypt ssh key")
			return
		}
		sshCfg.KeyPath = decSSHKey
	}

	conn := connector.NewSSHConnector(sshCfg)
	if err := conn.Connect(); err != nil {
		Error(w, http.StatusBadGateway, fmt.Sprintf("SSH connection failed: %v", err))
		return
	}
	defer conn.Close()

	discoverySvc := discovery.NewDiscoveryService(h.db)
	result, err := discoverySvc.Discover(context.Background(), conn)
	if err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("discovery failed: %v", err))
		return
	}
	result.ServerID = id

	if err := discoverySvc.SaveResults(id, result); err != nil {
		// Log the error but still return the result.
		_ = err
	}

	JSON(w, http.StatusOK, result)
}

// GetDiscovery handles GET /api/servers/{id}/discovery
// Returns the last saved discovery results for the server without running a new scan.
func (h *ServersHandler) GetDiscovery(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	// Verify the server exists.
	var exists int
	if err := h.db.DB().QueryRowContext(r.Context(),
		"SELECT COUNT(*) FROM servers WHERE id=?", id,
	).Scan(&exists); err != nil || exists == 0 {
		Error(w, http.StatusNotFound, "server not found")
		return
	}

	discSvc := discovery.NewDiscoveryService(h.db)
	result, err := discSvc.LoadResults(id)
	if err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("failed to load discovery results: %v", err))
		return
	}

	JSON(w, http.StatusOK, result)
}

// Rescan handles POST /api/servers/{id}/rescan
// Loads previous results, runs a fresh scan, compares, saves, and returns changes.
func (h *ServersHandler) Rescan(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	// Fetch server credentials.
	type serverRow struct {
		name           string
		host           string
		port           int
		connectionType string
		username       string
		password       sql.NullString
		sshKeyPath     sql.NullString
	}
	var row serverRow
	err := h.db.DB().QueryRowContext(r.Context(),
		`SELECT name, host, port, connection_type, COALESCE(username,''), encrypted_password, ssh_key_path
		 FROM servers WHERE id=?`, id,
	).Scan(&row.name, &row.host, &row.port, &row.connectionType, &row.username, &row.password, &row.sshKeyPath)
	if err == sql.ErrNoRows {
		Error(w, http.StatusNotFound, "server not found")
		return
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query server")
		return
	}

	if row.connectionType != "ssh" {
		Error(w, http.StatusBadRequest, "rescan is only supported for SSH servers")
		return
	}

	sshCfg := connector.SSHConfig{
		Host:     row.host,
		Port:     row.port,
		Username: row.username,
		Timeout:  30 * time.Second,
	}
	if row.password.Valid {
		decPassword, err := h.decryptCred(row.password.String)
		if err != nil {
			Error(w, http.StatusInternalServerError, "failed to decrypt server password")
			return
		}
		sshCfg.Password = decPassword
	}
	if row.sshKeyPath.Valid {
		decSSHKey, err := h.decryptCred(row.sshKeyPath.String)
		if err != nil {
			Error(w, http.StatusInternalServerError, "failed to decrypt ssh key")
			return
		}
		sshCfg.KeyPath = decSSHKey
	}

	discSvc := discovery.NewDiscoveryService(h.db)

	// 1. Load previous results.
	previous, err := discSvc.LoadResults(id)
	if err != nil {
		// Non-fatal: treat as empty previous scan.
		previous = &discovery.DiscoveryResult{ServerID: id, Services: []discovery.DiscoveredService{}}
	}

	// 2. Run fresh discovery.
	conn := connector.NewSSHConnector(sshCfg)
	if err := conn.Connect(); err != nil {
		Error(w, http.StatusBadGateway, fmt.Sprintf("SSH connection failed: %v", err))
		return
	}
	defer conn.Close()

	current, err := discSvc.Discover(context.Background(), conn)
	if err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("discovery failed: %v", err))
		return
	}
	current.ServerID = id

	// 3. Compare.
	changes := discovery.CompareResults(previous.Services, current.Services)

	// 4. Save new results.
	if err := discSvc.SaveResults(id, current); err != nil {
		_ = err // Non-fatal: return results anyway.
	}

	// 5. Auto-regenerate playbooks if changes found.
	if len(changes) > 0 {
		h.regeneratePlaybooks(r.Context(), id)
	}

	// 6. Notify if changes found.
	if len(changes) > 0 && h.notifyMgr != nil {
		details := make(map[string]string, len(changes))
		for i, c := range changes {
			details[fmt.Sprintf("change_%d", i+1)] = fmt.Sprintf("[%s] %s/%s: %s", c.Type, c.Category, c.Name, c.Details)
		}
		_ = h.notifyMgr.Notify(notification.NotificationEvent{
			Type:       notification.EventServiceDown, // closest existing type; reuse for discovery changes
			ServerName: row.name,
			Title:      fmt.Sprintf("Discovery changes on %s", row.name),
			Message:    fmt.Sprintf("%d change(s) detected during rescan", len(changes)),
			Details:    details,
		})
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"discovery": current,
		"changes":   changes,
	})
}

// regeneratePlaybooks deletes old playbooks for a server and generates fresh ones
// based on the current backup sources. Called automatically after a rescan detects changes.
func (h *ServersHandler) regeneratePlaybooks(ctx context.Context, serverID int) {
	// Load server info
	var srvName, srvHost string
	if err := h.db.DB().QueryRowContext(ctx,
		"SELECT name, host FROM servers WHERE id=?", serverID,
	).Scan(&srvName, &srvHost); err != nil {
		return
	}

	// Load current backup sources
	rows, err := h.db.DB().QueryContext(ctx,
		`SELECT id, name, type, COALESCE(source_path,''), COALESCE(db_name,'')
		 FROM backup_sources WHERE server_id=? AND enabled=1`, serverID)
	if err != nil {
		return
	}
	defer rows.Close()

	var sources []recovery.SourceInfo
	for rows.Next() {
		var s recovery.SourceInfo
		if err := rows.Scan(&s.ID, &s.Name, &s.Type, &s.SourcePath, &s.DBName); err == nil {
			sources = append(sources, s)
		}
	}

	// Delete old playbooks for this server
	h.db.DB().ExecContext(ctx, "DELETE FROM recovery_playbooks WHERE server_id=?", serverID)

	// Generate and save new ones
	generated := recovery.GeneratePlaybooks(recovery.ServerInfo{
		ID: serverID, Name: srvName, Host: srvHost,
	}, sources)

	now := time.Now().UTC().Format(time.RFC3339)
	for _, p := range generated {
		stepsJSON, err := json.Marshal(p.Steps)
		if err != nil {
			continue
		}
		h.db.DB().ExecContext(ctx,
			`INSERT INTO recovery_playbooks (server_id, title, scenario, steps, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			serverID, p.Title, p.Scenario, string(stepsJSON), now, now)
	}
}

// encryptCred encrypts plaintext using the handler's CredentialManager.
// If no CredentialManager is configured, plaintext is returned unchanged.
func (h *ServersHandler) encryptCred(plaintext string) (string, error) {
	if h.credMgr == nil || plaintext == "" {
		return plaintext, nil
	}
	return h.credMgr.Encrypt(plaintext)
}

// decryptCred decrypts ciphertext using the handler's CredentialManager.
// If no CredentialManager is configured, ciphertext is returned unchanged.
func (h *ServersHandler) decryptCred(ciphertext string) (string, error) {
	if h.credMgr == nil || ciphertext == "" {
		return ciphertext, nil
	}
	return h.credMgr.Decrypt(ciphertext)
}

// --- helpers ---

func (h *ServersHandler) fetchServer(r *http.Request, id int) (Server, bool) {
	var s Server
	var createdAt, updatedAt string
	var username sql.NullString
	var useTLSInt int

	err := h.db.DB().QueryRowContext(r.Context(),
		`SELECT id, name, type, host, port, connection_type, username, COALESCE(use_tls,0), status, created_at, updated_at
		 FROM servers WHERE id=?`, id,
	).Scan(&s.ID, &s.Name, &s.Type, &s.Host, &s.Port, &s.ConnectionType, &username, &useTLSInt, &s.Status, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return Server{}, false
	}
	if err != nil {
		return Server{}, false
	}
	if username.Valid {
		s.Username = username.String
	}
	s.UseTLS = useTLSInt != 0
	s.CreatedAt = parseTime(createdAt)
	s.UpdatedAt = parseTime(updatedAt)
	return s, true
}

// validateServerRequest validates the server request fields and returns an error message or empty string.
func validateServerRequest(req serverRequest) string {
	if strings.TrimSpace(req.Name) == "" {
		return "name is required"
	}
	if req.Type != "linux" && req.Type != "windows" {
		return "type must be linux or windows"
	}
	if strings.TrimSpace(req.Host) == "" {
		return "host is required"
	}
	if req.Port <= 0 {
		return "port is required and must be positive"
	}
	if req.ConnectionType != "ssh" && req.ConnectionType != "ftp" {
		return "connection_type must be ssh or ftp"
	}
	// Type/connection_type constraints
	if req.Type == "linux" && req.ConnectionType != "ssh" {
		return "linux servers must use ssh"
	}
	if req.Type == "windows" && req.ConnectionType != "ftp" {
		return "windows servers must use ftp"
	}
	return ""
}

// pathID extracts the {id} path parameter, writes an error and returns false on failure.
func pathID(w http.ResponseWriter, r *http.Request) (int, bool) {
	idStr := r.PathValue("id")
	if idStr == "" {
		Error(w, http.StatusBadRequest, "missing id")
		return 0, false
	}
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		Error(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

// nullableString converts an empty string to nil (for SQL nullable columns).
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// parseTime parses a datetime string from SQLite (tries multiple formats).
func parseTime(s string) time.Time {
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
