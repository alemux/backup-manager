// internal/api/servers_handler.go
package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
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
	db *database.Database
}

// NewServersHandler constructs a ServersHandler.
func NewServersHandler(db *database.Database) *ServersHandler {
	return &ServersHandler{db: db}
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

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := h.db.DB().ExecContext(r.Context(),
		`INSERT INTO servers (name, type, host, port, connection_type, username, encrypted_password, ssh_key_path, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'unknown', ?, ?)`,
		req.Name, req.Type, req.Host, req.Port, req.ConnectionType,
		nullableString(req.Username), nullableString(req.Password), nullableString(req.SSHKeyPath),
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

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := h.db.DB().ExecContext(r.Context(),
		`UPDATE servers SET name=?, type=?, host=?, port=?, connection_type=?, username=?, encrypted_password=?, ssh_key_path=?, updated_at=?
		 WHERE id=?`,
		req.Name, req.Type, req.Host, req.Port, req.ConnectionType,
		nullableString(req.Username), nullableString(req.Password), nullableString(req.SSHKeyPath),
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
// Actual SSH/FTP testing will be implemented in Task 8/9.
// For now it returns a stub response.
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

	// Stub: actual connector not implemented until Task 8/9
	JSON(w, http.StatusOK, map[string]interface{}{
		"success": false,
		"message": "connection test not yet implemented (requires Task 8/9 connectors)",
	})
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
