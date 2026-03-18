// internal/api/sources_handler.go
package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
)

// BackupSource represents a backup source record returned in API responses.
type BackupSource struct {
	ID         int        `json:"id"`
	ServerID   int        `json:"server_id"`
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	SourcePath *string    `json:"source_path"`
	DBName     *string    `json:"db_name"`
	DependsOn  *int       `json:"depends_on"`
	Priority   int        `json:"priority"`
	Enabled    bool       `json:"enabled"`
	CreatedAt  time.Time  `json:"created_at"`
}

// sourceRequest is the incoming payload for create/update.
type sourceRequest struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	SourcePath string  `json:"source_path"`
	DBName     string  `json:"db_name"`
	DependsOn  *int    `json:"depends_on"`
	Priority   int     `json:"priority"`
	Enabled    *bool   `json:"enabled"`
}

// SourcesHandler handles all backup source routes.
type SourcesHandler struct {
	db *database.Database
}

// NewSourcesHandler constructs a SourcesHandler.
func NewSourcesHandler(db *database.Database) *SourcesHandler {
	return &SourcesHandler{db: db}
}

// List handles GET /api/servers/{id}/sources
func (h *SourcesHandler) List(w http.ResponseWriter, r *http.Request) {
	serverID, ok := pathID(w, r)
	if !ok {
		return
	}

	rows, err := h.db.DB().QueryContext(r.Context(),
		`SELECT id, server_id, name, type, source_path, db_name, depends_on, priority, enabled, created_at
		 FROM backup_sources WHERE server_id=? ORDER BY priority ASC, id ASC`, serverID)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query sources")
		return
	}
	defer rows.Close()

	sources := make([]BackupSource, 0)
	for rows.Next() {
		s, err := scanSource(rows)
		if err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan source")
			return
		}
		sources = append(sources, s)
	}
	if err := rows.Err(); err != nil {
		Error(w, http.StatusInternalServerError, "row iteration error")
		return
	}

	JSON(w, http.StatusOK, sources)
}

// Create handles POST /api/servers/{id}/sources
func (h *SourcesHandler) Create(w http.ResponseWriter, r *http.Request) {
	serverID, ok := pathID(w, r)
	if !ok {
		return
	}

	// Verify server exists
	var exists int
	err := h.db.DB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM servers WHERE id=?", serverID).Scan(&exists)
	if err != nil || exists == 0 {
		Error(w, http.StatusNotFound, "server not found")
		return
	}

	var req sourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if errMsg := validateSourceRequest(req); errMsg != "" {
		Error(w, http.StatusBadRequest, errMsg)
		return
	}

	// Validate depends_on if set
	if req.DependsOn != nil {
		if errMsg := h.validateDependsOn(r, serverID, 0, *req.DependsOn); errMsg != "" {
			Error(w, http.StatusBadRequest, errMsg)
			return
		}
	}

	enabled := 1
	if req.Enabled != nil && !*req.Enabled {
		enabled = 0
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := h.db.DB().ExecContext(r.Context(),
		`INSERT INTO backup_sources (server_id, name, type, source_path, db_name, depends_on, priority, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		serverID, req.Name, req.Type,
		nullableString(req.SourcePath), nullableString(req.DBName),
		req.DependsOn, req.Priority, enabled, now,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to create source")
		return
	}

	id, _ := result.LastInsertId()
	source, found := h.fetchSource(r, int(id))
	if !found {
		Error(w, http.StatusInternalServerError, "failed to retrieve created source")
		return
	}

	JSON(w, http.StatusCreated, source)
}

// Update handles PUT /api/sources/{id}
func (h *SourcesHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	// Fetch existing source to get server_id
	var serverID int
	err := h.db.DB().QueryRowContext(r.Context(), "SELECT server_id FROM backup_sources WHERE id=?", id).Scan(&serverID)
	if err == sql.ErrNoRows {
		Error(w, http.StatusNotFound, "source not found")
		return
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query source")
		return
	}

	var req sourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if errMsg := validateSourceRequest(req); errMsg != "" {
		Error(w, http.StatusBadRequest, errMsg)
		return
	}

	// Validate depends_on if set
	if req.DependsOn != nil {
		if errMsg := h.validateDependsOn(r, serverID, id, *req.DependsOn); errMsg != "" {
			Error(w, http.StatusBadRequest, errMsg)
			return
		}
	}

	enabled := 1
	if req.Enabled != nil && !*req.Enabled {
		enabled = 0
	}

	res, err := h.db.DB().ExecContext(r.Context(),
		`UPDATE backup_sources SET name=?, type=?, source_path=?, db_name=?, depends_on=?, priority=?, enabled=?
		 WHERE id=?`,
		req.Name, req.Type,
		nullableString(req.SourcePath), nullableString(req.DBName),
		req.DependsOn, req.Priority, enabled, id,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to update source")
		return
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		Error(w, http.StatusNotFound, "source not found")
		return
	}

	source, found := h.fetchSource(r, id)
	if !found {
		Error(w, http.StatusInternalServerError, "failed to retrieve updated source")
		return
	}

	JSON(w, http.StatusOK, source)
}

// Delete handles DELETE /api/sources/{id}
func (h *SourcesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	res, err := h.db.DB().ExecContext(r.Context(), "DELETE FROM backup_sources WHERE id=?", id)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to delete source")
		return
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		Error(w, http.StatusNotFound, "source not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func (h *SourcesHandler) fetchSource(r *http.Request, id int) (BackupSource, bool) {
	row := h.db.DB().QueryRowContext(r.Context(),
		`SELECT id, server_id, name, type, source_path, db_name, depends_on, priority, enabled, created_at
		 FROM backup_sources WHERE id=?`, id)
	s, err := scanSourceRow(row)
	if err == sql.ErrNoRows {
		return BackupSource{}, false
	}
	if err != nil {
		return BackupSource{}, false
	}
	return s, true
}

// validateDependsOn checks that:
// 1. The referenced source exists and belongs to the same server.
// 2. Setting the dependency does not create a cycle.
// sourceID is the ID of the source being created/updated (0 for new sources).
func (h *SourcesHandler) validateDependsOn(r *http.Request, serverID, sourceID, dependsOnID int) string {
	// Verify the referenced source exists and belongs to the same server
	var refServerID int
	err := h.db.DB().QueryRowContext(r.Context(),
		"SELECT server_id FROM backup_sources WHERE id=?", dependsOnID,
	).Scan(&refServerID)
	if err == sql.ErrNoRows {
		return "depends_on references a non-existent source"
	}
	if err != nil {
		return "failed to validate depends_on"
	}
	if refServerID != serverID {
		return "depends_on must reference a source belonging to the same server"
	}

	// Cycle detection: walk the dependency chain from dependsOnID
	// If we encounter sourceID (or if sourceID == 0, just check for cycles in chain),
	// then setting this dependency would create a cycle.
	visited := map[int]bool{}
	current := dependsOnID
	for current != 0 {
		if current == sourceID && sourceID != 0 {
			return "dependency cycle detected"
		}
		if visited[current] {
			// Already a cycle in the chain itself (shouldn't happen with valid data)
			break
		}
		visited[current] = true

		var nextDep sql.NullInt64
		err := h.db.DB().QueryRowContext(r.Context(),
			"SELECT depends_on FROM backup_sources WHERE id=?", current,
		).Scan(&nextDep)
		if err != nil {
			break
		}
		if !nextDep.Valid {
			break
		}
		current = int(nextDep.Int64)
	}

	return ""
}

// validateSourceRequest validates source request fields.
func validateSourceRequest(req sourceRequest) string {
	if strings.TrimSpace(req.Name) == "" {
		return "name is required"
	}
	if req.Type != "web" && req.Type != "database" && req.Type != "config" {
		return "type must be web, database, or config"
	}
	if (req.Type == "web" || req.Type == "config") && strings.TrimSpace(req.SourcePath) == "" {
		return "source_path is required for type " + req.Type
	}
	if req.Type == "database" && strings.TrimSpace(req.DBName) == "" {
		return "db_name is required for type database"
	}
	return ""
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanSourceRow(row rowScanner) (BackupSource, error) {
	var s BackupSource
	var sourcePath sql.NullString
	var dbName sql.NullString
	var dependsOn sql.NullInt64
	var enabledInt int
	var createdAt string

	err := row.Scan(
		&s.ID, &s.ServerID, &s.Name, &s.Type,
		&sourcePath, &dbName, &dependsOn,
		&s.Priority, &enabledInt, &createdAt,
	)
	if err != nil {
		return BackupSource{}, err
	}

	if sourcePath.Valid {
		s.SourcePath = &sourcePath.String
	}
	if dbName.Valid {
		s.DBName = &dbName.String
	}
	if dependsOn.Valid {
		v := int(dependsOn.Int64)
		s.DependsOn = &v
	}
	s.Enabled = enabledInt != 0
	s.CreatedAt = parseTime(createdAt)

	return s, nil
}

func scanSource(rows *sql.Rows) (BackupSource, error) {
	return scanSourceRow(rows)
}
