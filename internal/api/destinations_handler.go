// internal/api/destinations_handler.go
package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
	syncdest "github.com/backupmanager/backupmanager/internal/sync"
)

// DestinationResponse is the destination JSON structure returned by the API.
type DestinationResponse struct {
	ID               int       `json:"id"`
	Name             string    `json:"name"`
	Type             string    `json:"type"`
	Path             string    `json:"path"`
	IsPrimary        bool      `json:"is_primary"`
	RetentionDaily   int       `json:"retention_daily"`
	RetentionWeekly  int       `json:"retention_weekly"`
	RetentionMonthly int       `json:"retention_monthly"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"created_at"`
}

// destinationRequest is the incoming payload for create/update.
type destinationRequest struct {
	Name             string `json:"name"`
	Type             string `json:"type"`
	Path             string `json:"path"`
	IsPrimary        *bool  `json:"is_primary"`
	RetentionDaily   *int   `json:"retention_daily"`
	RetentionWeekly  *int   `json:"retention_weekly"`
	RetentionMonthly *int   `json:"retention_monthly"`
	Enabled          *bool  `json:"enabled"`
}

// DestinationsHandler handles all /api/destinations routes.
type DestinationsHandler struct {
	db     *database.Database
	syncer *syncdest.DestinationSyncer
}

// NewDestinationsHandler constructs a DestinationsHandler.
func NewDestinationsHandler(db *database.Database) *DestinationsHandler {
	return &DestinationsHandler{
		db:     db,
		syncer: syncdest.NewDestinationSyncer(db),
	}
}

// List handles GET /api/destinations
func (h *DestinationsHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.DB().QueryContext(r.Context(),
		`SELECT id, name, type, path, is_primary,
		        retention_daily, retention_weekly, retention_monthly,
		        enabled, created_at
		 FROM destinations ORDER BY id ASC`,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query destinations")
		return
	}
	defer rows.Close()

	dests := make([]DestinationResponse, 0)
	for rows.Next() {
		d, err := scanDestination(rows)
		if err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan destination")
			return
		}
		dests = append(dests, d)
	}
	if err := rows.Err(); err != nil {
		Error(w, http.StatusInternalServerError, "row iteration error")
		return
	}

	JSON(w, http.StatusOK, dests)
}

// Create handles POST /api/destinations
func (h *DestinationsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req destinationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if errMsg := validateDestinationRequest(req); errMsg != "" {
		Error(w, http.StatusBadRequest, errMsg)
		return
	}

	isPrimary, retDaily, retWeekly, retMonthly, enabled := destinationDefaults(req)
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := h.db.DB().ExecContext(r.Context(),
		`INSERT INTO destinations (name, type, path, is_primary,
		  retention_daily, retention_weekly, retention_monthly, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Name, req.Type, req.Path, isPrimary,
		retDaily, retWeekly, retMonthly, enabled, now,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to create destination")
		return
	}

	id, _ := res.LastInsertId()
	dest, ok := h.fetchDestination(r, int(id))
	if !ok {
		Error(w, http.StatusInternalServerError, "failed to retrieve created destination")
		return
	}

	JSON(w, http.StatusCreated, dest)
}

// Update handles PUT /api/destinations/{id}
func (h *DestinationsHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	var exists int
	if err := h.db.DB().QueryRowContext(r.Context(),
		"SELECT COUNT(*) FROM destinations WHERE id=?", id,
	).Scan(&exists); err != nil || exists == 0 {
		Error(w, http.StatusNotFound, "destination not found")
		return
	}

	var req destinationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if errMsg := validateDestinationRequest(req); errMsg != "" {
		Error(w, http.StatusBadRequest, errMsg)
		return
	}

	isPrimary, retDaily, retWeekly, retMonthly, enabled := destinationDefaults(req)

	_, err := h.db.DB().ExecContext(r.Context(),
		`UPDATE destinations SET name=?, type=?, path=?, is_primary=?,
		  retention_daily=?, retention_weekly=?, retention_monthly=?, enabled=?
		 WHERE id=?`,
		req.Name, req.Type, req.Path, isPrimary,
		retDaily, retWeekly, retMonthly, enabled, id,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to update destination")
		return
	}

	dest, found := h.fetchDestination(r, id)
	if !found {
		Error(w, http.StatusInternalServerError, "failed to retrieve updated destination")
		return
	}

	JSON(w, http.StatusOK, dest)
}

// Delete handles DELETE /api/destinations/{id}
func (h *DestinationsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	res, err := h.db.DB().ExecContext(r.Context(),
		"DELETE FROM destinations WHERE id=?", id,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to delete destination")
		return
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		Error(w, http.StatusNotFound, "destination not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// SyncStatusMatrix handles GET /api/destinations/status
// Returns a matrix of all snapshots × destinations sync status.
func (h *DestinationsHandler) SyncStatusMatrix(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.DB().QueryContext(r.Context(),
		`SELECT dss.id, dss.snapshot_id, dss.destination_id, d.name,
		        dss.status, dss.retry_count, dss.last_error, dss.synced_at
		 FROM destination_sync_status dss
		 INNER JOIN destinations d ON d.id = dss.destination_id
		 ORDER BY dss.snapshot_id ASC, dss.destination_id ASC`,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query sync status")
		return
	}
	defer rows.Close()

	type entry struct {
		ID            int     `json:"id"`
		SnapshotID    int     `json:"snapshot_id"`
		DestinationID int     `json:"destination_id"`
		DestName      string  `json:"destination_name"`
		Status        string  `json:"status"`
		RetryCount    int     `json:"retry_count"`
		LastError     *string `json:"last_error,omitempty"`
		SyncedAt      *string `json:"synced_at,omitempty"`
	}

	entries := make([]entry, 0)
	for rows.Next() {
		var e entry
		var lastErr sql.NullString
		var syncedAt sql.NullString
		if err := rows.Scan(
			&e.ID, &e.SnapshotID, &e.DestinationID, &e.DestName,
			&e.Status, &e.RetryCount, &lastErr, &syncedAt,
		); err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan sync status")
			return
		}
		if lastErr.Valid {
			e.LastError = &lastErr.String
		}
		if syncedAt.Valid {
			e.SyncedAt = &syncedAt.String
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		Error(w, http.StatusInternalServerError, "row iteration error")
		return
	}

	JSON(w, http.StatusOK, entries)
}

// RetrySync handles POST /api/destinations/{id}/retry/{snapshot_id}
// Manually retries a failed sync for a specific destination × snapshot.
func (h *DestinationsHandler) RetrySync(w http.ResponseWriter, r *http.Request) {
	destID, ok := pathID(w, r)
	if !ok {
		return
	}

	// Extract snapshot_id from URL path segment.
	snapshotIDStr := r.PathValue("snapshot_id")
	snapshotID, err := strconv.Atoi(snapshotIDStr)
	if err != nil || snapshotID <= 0 {
		Error(w, http.StatusBadRequest, "invalid snapshot_id")
		return
	}

	// Find the sync status entry.
	var syncStatusID int
	err = h.db.DB().QueryRowContext(r.Context(),
		`SELECT id FROM destination_sync_status
		 WHERE destination_id=? AND snapshot_id=?`,
		destID, snapshotID,
	).Scan(&syncStatusID)
	if err == sql.ErrNoRows {
		Error(w, http.StatusNotFound, "sync status not found")
		return
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query sync status")
		return
	}

	if err := h.syncer.RetryFailed(syncStatusID); err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}

	JSON(w, http.StatusOK, map[string]string{"status": "queued"})
}

// --- helpers ---

func (h *DestinationsHandler) fetchDestination(r *http.Request, id int) (DestinationResponse, bool) {
	row := h.db.DB().QueryRowContext(r.Context(),
		`SELECT id, name, type, path, is_primary,
		        retention_daily, retention_weekly, retention_monthly,
		        enabled, created_at
		 FROM destinations WHERE id=?`, id,
	)
	d, err := scanDestination(row)
	if err == sql.ErrNoRows {
		return DestinationResponse{}, false
	}
	if err != nil {
		return DestinationResponse{}, false
	}
	return d, true
}

func scanDestination(row rowScanner) (DestinationResponse, error) {
	var d DestinationResponse
	var isPrimaryInt, enabledInt int
	var createdAt string

	err := row.Scan(
		&d.ID, &d.Name, &d.Type, &d.Path, &isPrimaryInt,
		&d.RetentionDaily, &d.RetentionWeekly, &d.RetentionMonthly,
		&enabledInt, &createdAt,
	)
	if err != nil {
		return DestinationResponse{}, err
	}

	d.IsPrimary = isPrimaryInt != 0
	d.Enabled = enabledInt != 0
	d.CreatedAt = parseTime(createdAt)
	return d, nil
}

func validateDestinationRequest(req destinationRequest) string {
	if strings.TrimSpace(req.Name) == "" {
		return "name is required"
	}
	switch req.Type {
	case "local", "nas", "usb", "s3":
	default:
		return "type must be one of: local, nas, usb, s3"
	}
	if strings.TrimSpace(req.Path) == "" {
		return "path is required"
	}
	return ""
}

func destinationDefaults(req destinationRequest) (isPrimary, retDaily, retWeekly, retMonthly, enabled int) {
	retDaily = 7
	if req.RetentionDaily != nil {
		retDaily = *req.RetentionDaily
	}
	retWeekly = 4
	if req.RetentionWeekly != nil {
		retWeekly = *req.RetentionWeekly
	}
	retMonthly = 3
	if req.RetentionMonthly != nil {
		retMonthly = *req.RetentionMonthly
	}
	enabled = 1
	if req.Enabled != nil && !*req.Enabled {
		enabled = 0
	}
	isPrimary = 0
	if req.IsPrimary != nil && *req.IsPrimary {
		isPrimary = 1
	}
	return
}
