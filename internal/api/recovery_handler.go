// internal/api/recovery_handler.go
package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
	"github.com/backupmanager/backupmanager/internal/recovery"
)

// RecoveryHandler handles all /api/recovery/* routes.
type RecoveryHandler struct {
	db *database.Database
}

// NewRecoveryHandler constructs a RecoveryHandler.
func NewRecoveryHandler(db *database.Database) *RecoveryHandler {
	return &RecoveryHandler{db: db}
}

// playbookRow is used to scan rows from the recovery_playbooks table.
type playbookRow struct {
	id        int
	serverID  sql.NullInt64
	title     string
	scenario  string
	stepsJSON string
	createdAt string
	updatedAt string
}

func (h *RecoveryHandler) rowToPlaybook(row playbookRow) (recovery.Playbook, error) {
	var steps []recovery.Step
	if err := json.Unmarshal([]byte(row.stepsJSON), &steps); err != nil {
		return recovery.Playbook{}, err
	}
	p := recovery.Playbook{
		ID:        row.id,
		Title:     row.title,
		Scenario:  row.scenario,
		Steps:     steps,
		CreatedAt: parseTime(row.createdAt),
		UpdatedAt: parseTime(row.updatedAt),
	}
	if row.serverID.Valid {
		sid := int(row.serverID.Int64)
		p.ServerID = &sid
	}
	return p, nil
}

// ListPlaybooks handles GET /api/recovery/playbooks
func (h *RecoveryHandler) ListPlaybooks(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.DB().QueryContext(r.Context(),
		`SELECT id, server_id, title, scenario, steps, created_at, COALESCE(updated_at, created_at)
		 FROM recovery_playbooks ORDER BY id ASC`)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query playbooks")
		return
	}
	defer rows.Close()

	playbooks := make([]recovery.Playbook, 0)
	for rows.Next() {
		var row playbookRow
		if err := rows.Scan(&row.id, &row.serverID, &row.title, &row.scenario, &row.stepsJSON, &row.createdAt, &row.updatedAt); err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan playbook")
			return
		}
		p, err := h.rowToPlaybook(row)
		if err != nil {
			Error(w, http.StatusInternalServerError, "failed to parse playbook steps")
			return
		}
		playbooks = append(playbooks, p)
	}
	if err := rows.Err(); err != nil {
		Error(w, http.StatusInternalServerError, "row iteration error")
		return
	}

	JSON(w, http.StatusOK, playbooks)
}

// GetPlaybook handles GET /api/recovery/playbooks/{id}
func (h *RecoveryHandler) GetPlaybook(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	var row playbookRow
	err := h.db.DB().QueryRowContext(r.Context(),
		`SELECT id, server_id, title, scenario, steps, created_at, COALESCE(updated_at, created_at)
		 FROM recovery_playbooks WHERE id=?`, id,
	).Scan(&row.id, &row.serverID, &row.title, &row.scenario, &row.stepsJSON, &row.createdAt, &row.updatedAt)
	if err == sql.ErrNoRows {
		Error(w, http.StatusNotFound, "playbook not found")
		return
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query playbook")
		return
	}

	p, err := h.rowToPlaybook(row)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to parse playbook steps")
		return
	}

	JSON(w, http.StatusOK, p)
}

// GeneratePlaybooks handles POST /api/recovery/playbooks/generate/{server_id}
// It auto-generates playbooks for all backup sources of the given server and persists them.
func (h *RecoveryHandler) GeneratePlaybooks(w http.ResponseWriter, r *http.Request) {
	serverIDStr := r.PathValue("server_id")
	serverID, err := strconv.Atoi(serverIDStr)
	if err != nil || serverID <= 0 {
		Error(w, http.StatusBadRequest, "invalid server_id")
		return
	}

	// Load server info
	var srv recovery.ServerInfo
	err = h.db.DB().QueryRowContext(r.Context(),
		`SELECT id, name, host FROM servers WHERE id=?`, serverID,
	).Scan(&srv.ID, &srv.Name, &srv.Host)
	if err == sql.ErrNoRows {
		Error(w, http.StatusNotFound, "server not found")
		return
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query server")
		return
	}

	// Load backup sources for this server
	rows, err := h.db.DB().QueryContext(r.Context(),
		`SELECT id, name, type, COALESCE(source_path,''), COALESCE(db_name,'')
		 FROM backup_sources WHERE server_id=? AND enabled=1`, serverID,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query sources")
		return
	}
	defer rows.Close()

	var sources []recovery.SourceInfo
	for rows.Next() {
		var s recovery.SourceInfo
		if err := rows.Scan(&s.ID, &s.Name, &s.Type, &s.SourcePath, &s.DBName); err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan source")
			return
		}
		sources = append(sources, s)
	}
	if err := rows.Err(); err != nil {
		Error(w, http.StatusInternalServerError, "row iteration error")
		return
	}

	// Generate playbooks in memory
	generated := recovery.GeneratePlaybooks(srv, sources)

	now := time.Now().UTC().Format(time.RFC3339)

	// Persist each playbook
	var saved []recovery.Playbook
	for _, p := range generated {
		stepsJSON, err := json.Marshal(p.Steps)
		if err != nil {
			Error(w, http.StatusInternalServerError, "failed to marshal steps")
			return
		}

		result, err := h.db.DB().ExecContext(r.Context(),
			`INSERT INTO recovery_playbooks (server_id, title, scenario, steps, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			serverID, p.Title, p.Scenario, string(stepsJSON), now, now,
		)
		if err != nil {
			Error(w, http.StatusInternalServerError, "failed to save playbook")
			return
		}

		lastID, _ := result.LastInsertId()
		p.ID = int(lastID)
		p.ServerID = &serverID
		p.CreatedAt = parseTime(now)
		saved = append(saved, p)
	}

	JSON(w, http.StatusCreated, saved)
}

// UpdatePlaybook handles PUT /api/recovery/playbooks/{id}
func (h *RecoveryHandler) UpdatePlaybook(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	var req recovery.Playbook
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Title == "" {
		Error(w, http.StatusBadRequest, "title is required")
		return
	}

	stepsJSON, err := json.Marshal(req.Steps)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to marshal steps")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := h.db.DB().ExecContext(r.Context(),
		`UPDATE recovery_playbooks SET title=?, scenario=?, steps=?, updated_at=? WHERE id=?`,
		req.Title, req.Scenario, string(stepsJSON), now, id,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to update playbook")
		return
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		Error(w, http.StatusNotFound, "playbook not found")
		return
	}

	// Return the updated playbook
	var row playbookRow
	err = h.db.DB().QueryRowContext(r.Context(),
		`SELECT id, server_id, title, scenario, steps, created_at, COALESCE(updated_at, created_at) FROM recovery_playbooks WHERE id=?`, id,
	).Scan(&row.id, &row.serverID, &row.title, &row.scenario, &row.stepsJSON, &row.createdAt, &row.updatedAt)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to retrieve updated playbook")
		return
	}

	p, err := h.rowToPlaybook(row)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to parse playbook steps")
		return
	}

	JSON(w, http.StatusOK, p)
}

// DeletePlaybook handles DELETE /api/recovery/playbooks/{id}
func (h *RecoveryHandler) DeletePlaybook(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	res, err := h.db.DB().ExecContext(r.Context(),
		`DELETE FROM recovery_playbooks WHERE id=?`, id,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to delete playbook")
		return
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		Error(w, http.StatusNotFound, "playbook not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
