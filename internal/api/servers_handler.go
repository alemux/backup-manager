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
		`SELECT id, name, type, host, port, connection_type, COALESCE(username,''), status, created_at, updated_at
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
		if err := rows.Scan(&s.ID, &s.Name, &s.Type, &s.Host, &s.Port, &s.ConnectionType, &s.Username, &s.Status, &createdAt, &updatedAt); err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan server")
			return
		}
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

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := h.db.DB().ExecContext(r.Context(),
		`INSERT INTO servers (name, type, host, port, connection_type, username, encrypted_password, ssh_key_path, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'unknown', ?, ?)`,
		req.Name, req.Type, req.Host, req.Port, req.ConnectionType,
		nullableString(req.Username), nullableString(encPassword), nullableString(encSSHKey),
		now, now,
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

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := h.db.DB().ExecContext(r.Context(),
		`UPDATE servers SET name=?, type=?, host=?, port=?, connection_type=?, username=?, encrypted_password=?, ssh_key_path=?, updated_at=?
		 WHERE id=?`,
		req.Name, req.Type, req.Host, req.Port, req.ConnectionType,
		nullableString(req.Username), nullableString(encPassword), nullableString(encSSHKey),
		now, id,
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

	// 5. Notify if changes found.
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

	err := h.db.DB().QueryRowContext(r.Context(),
		`SELECT id, name, type, host, port, connection_type, username, status, created_at, updated_at
		 FROM servers WHERE id=?`, id,
	).Scan(&s.ID, &s.Name, &s.Type, &s.Host, &s.Port, &s.ConnectionType, &username, &s.Status, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return Server{}, false
	}
	if err != nil {
		return Server{}, false
	}
	if username.Valid {
		s.Username = username.String
	}
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
