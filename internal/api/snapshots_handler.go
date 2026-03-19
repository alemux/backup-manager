// internal/api/snapshots_handler.go
package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
)

// SnapshotsHandler handles all /api/snapshots routes.
type SnapshotsHandler struct {
	db *database.Database
}

// NewSnapshotsHandler constructs a SnapshotsHandler.
func NewSnapshotsHandler(db *database.Database) *SnapshotsHandler {
	return &SnapshotsHandler{db: db}
}

// SnapshotResponse is the snapshot JSON structure returned by the API.
type SnapshotResponse struct {
	ID                 int                  `json:"id"`
	RunID              int                  `json:"run_id"`
	SourceID           int                  `json:"source_id"`
	SourceName         string               `json:"source_name"`
	SourceType         string               `json:"source_type"`
	SourcePath         *string              `json:"source_path,omitempty"`
	DBName             *string              `json:"db_name,omitempty"`
	ServerID           int                  `json:"server_id"`
	ServerName         string               `json:"server_name"`
	SnapshotPath       string               `json:"snapshot_path"`
	SizeBytes          int64                `json:"size_bytes"`
	ChecksumSHA256     *string              `json:"checksum_sha256,omitempty"`
	IsEncrypted        bool                 `json:"is_encrypted"`
	IntegrityStatus    *string              `json:"integrity_status,omitempty"`
	RetentionExpiresAt *time.Time           `json:"retention_expires_at,omitempty"`
	CreatedAt          time.Time            `json:"created_at"`
	SyncStatuses       []SnapshotSyncStatus `json:"sync_statuses"`
}

// SnapshotSyncStatus represents one destination sync status for a snapshot.
type SnapshotSyncStatus struct {
	DestinationID   int     `json:"destination_id"`
	DestinationName string  `json:"destination_name"`
	Status          string  `json:"status"`
	RetryCount      int     `json:"retry_count"`
	LastError       *string `json:"last_error,omitempty"`
	SyncedAt        *string `json:"synced_at,omitempty"`
}

// SnapshotListResponse is the paginated snapshot list response.
type SnapshotListResponse struct {
	Snapshots  []SnapshotResponse `json:"snapshots"`
	Total      int                `json:"total"`
	Page       int                `json:"page"`
	PerPage    int                `json:"per_page"`
	TotalPages int                `json:"total_pages"`
}

// CalendarDay holds snapshot counts for a single day.
type CalendarDay struct {
	Date         string `json:"date"`
	Count        int    `json:"count"`
	SuccessCount int    `json:"success_count"`
	FailedCount  int    `json:"failed_count"`
}

// List handles GET /api/snapshots
// Filters: date_from, date_to, server_id, source_type, page, per_page
func (h *SnapshotsHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := 1
	perPage := 20

	if p := q.Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if pp := q.Get("per_page"); pp != "" {
		if v, err := strconv.Atoi(pp); err == nil && v > 0 && v <= 100 {
			perPage = v
		}
	}

	dateFrom := q.Get("date_from")
	dateTo := q.Get("date_to")
	serverID := q.Get("server_id")
	sourceType := q.Get("source_type")

	// Build WHERE clauses
	where := "WHERE 1=1"
	args := []interface{}{}

	if dateFrom != "" {
		where += " AND bs.created_at >= ?"
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		// Include end of day
		where += " AND bs.created_at < date(?, '+1 day')"
		args = append(args, dateTo)
	}
	if serverID != "" {
		if id, err := strconv.Atoi(serverID); err == nil {
			where += " AND s.id = ?"
			args = append(args, id)
		}
	}
	if sourceType != "" && sourceType != "all" {
		where += " AND bsrc.type = ?"
		args = append(args, sourceType)
	}

	// Count query
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM backup_snapshots bs
		INNER JOIN backup_sources bsrc ON bsrc.id = bs.source_id
		INNER JOIN servers s ON s.id = bsrc.server_id
		%s`, where)

	var total int
	if err := h.db.DB().QueryRowContext(r.Context(), countQuery, args...).Scan(&total); err != nil {
		Error(w, http.StatusInternalServerError, "failed to count snapshots")
		return
	}

	offset := (page - 1) * perPage
	totalPages := (total + perPage - 1) / perPage
	if totalPages == 0 {
		totalPages = 1
	}

	// Data query
	dataQuery := fmt.Sprintf(`
		SELECT bs.id, bs.run_id, bs.source_id,
		       bsrc.name, bsrc.type, bsrc.source_path, bsrc.db_name,
		       s.id, s.name,
		       bs.snapshot_path, bs.size_bytes, bs.checksum_sha256,
		       bs.is_encrypted, bs.retention_expires_at, bs.created_at
		FROM backup_snapshots bs
		INNER JOIN backup_sources bsrc ON bsrc.id = bs.source_id
		INNER JOIN servers s ON s.id = bsrc.server_id
		%s
		ORDER BY bs.created_at DESC
		LIMIT ? OFFSET ?`, where)

	args = append(args, perPage, offset)
	rows, err := h.db.DB().QueryContext(r.Context(), dataQuery, args...)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query snapshots")
		return
	}
	defer rows.Close()

	snapshots := make([]SnapshotResponse, 0)
	for rows.Next() {
		sn, err := h.scanSnapshotRow(rows)
		if err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan snapshot")
			return
		}
		snapshots = append(snapshots, sn)
	}
	if err := rows.Err(); err != nil {
		Error(w, http.StatusInternalServerError, "row iteration error")
		return
	}

	// Load sync statuses for each snapshot
	if len(snapshots) > 0 {
		ids := make([]interface{}, len(snapshots))
		for i, sn := range snapshots {
			ids[i] = sn.ID
		}
		h.loadSyncStatuses(r, snapshots, ids)
	}

	JSON(w, http.StatusOK, SnapshotListResponse{
		Snapshots:  snapshots,
		Total:      total,
		Page:       page,
		PerPage:    perPage,
		TotalPages: totalPages,
	})
}

// Get handles GET /api/snapshots/{id}
func (h *SnapshotsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathSnapshotID(w, r)
	if !ok {
		return
	}

	row := h.db.DB().QueryRowContext(r.Context(), `
		SELECT bs.id, bs.run_id, bs.source_id,
		       bsrc.name, bsrc.type, bsrc.source_path, bsrc.db_name,
		       s.id, s.name,
		       bs.snapshot_path, bs.size_bytes, bs.checksum_sha256,
		       bs.is_encrypted, bs.retention_expires_at, bs.created_at
		FROM backup_snapshots bs
		INNER JOIN backup_sources bsrc ON bsrc.id = bs.source_id
		INNER JOIN servers s ON s.id = bsrc.server_id
		WHERE bs.id = ?`, id)

	sn, err := h.scanSnapshotRow(row)
	if err == sql.ErrNoRows {
		Error(w, http.StatusNotFound, "snapshot not found")
		return
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to scan snapshot")
		return
	}

	// Load sync statuses
	syncRows, err := h.db.DB().QueryContext(r.Context(), `
		SELECT dss.destination_id, d.name, dss.status, dss.retry_count, dss.last_error, dss.synced_at
		FROM destination_sync_status dss
		INNER JOIN destinations d ON d.id = dss.destination_id
		WHERE dss.snapshot_id = ?
		ORDER BY dss.destination_id ASC`, id)
	if err == nil {
		defer syncRows.Close()
		for syncRows.Next() {
			var ss SnapshotSyncStatus
			var lastErr, syncedAt sql.NullString
			if scanErr := syncRows.Scan(
				&ss.DestinationID, &ss.DestinationName,
				&ss.Status, &ss.RetryCount, &lastErr, &syncedAt,
			); scanErr == nil {
				if lastErr.Valid {
					ss.LastError = &lastErr.String
				}
				if syncedAt.Valid {
					ss.SyncedAt = &syncedAt.String
				}
				sn.SyncStatuses = append(sn.SyncStatuses, ss)
			}
		}
	}

	JSON(w, http.StatusOK, sn)
}

// Download handles GET /api/snapshots/{id}/download
func (h *SnapshotsHandler) Download(w http.ResponseWriter, r *http.Request) {
	id, ok := pathSnapshotID(w, r)
	if !ok {
		return
	}

	var snapshotPath string
	var isEncryptedInt int
	err := h.db.DB().QueryRowContext(r.Context(),
		`SELECT snapshot_path, is_encrypted FROM backup_snapshots WHERE id = ?`, id,
	).Scan(&snapshotPath, &isEncryptedInt)
	if err == sql.ErrNoRows {
		Error(w, http.StatusNotFound, "snapshot not found")
		return
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query snapshot")
		return
	}

	// Check file exists
	if _, err := os.Stat(snapshotPath); os.IsNotExist(err) {
		Error(w, http.StatusNotFound, "snapshot file not found on disk")
		return
	}

	filename := filepath.Base(snapshotPath)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, snapshotPath)
}

// Calendar handles GET /api/snapshots/calendar
// Query params: month (1-12), year (e.g. 2025)
func (h *SnapshotsHandler) Calendar(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	now := time.Now()
	month := int(now.Month())
	year := now.Year()

	if m := q.Get("month"); m != "" {
		if v, err := strconv.Atoi(m); err == nil && v >= 1 && v <= 12 {
			month = v
		}
	}
	if y := q.Get("year"); y != "" {
		if v, err := strconv.Atoi(y); err == nil && v > 2000 {
			year = v
		}
	}

	// Build date range for the month
	startDate := fmt.Sprintf("%04d-%02d-01", year, month)
	// Last day of month: first day of next month
	endDate := fmt.Sprintf("%04d-%02d-01", year, month+1)
	if month == 12 {
		endDate = fmt.Sprintf("%04d-01-01", year+1)
	}

	rows, err := h.db.DB().QueryContext(r.Context(), `
		SELECT date(bs.created_at) as day,
		       COUNT(*) as total,
		       SUM(CASE WHEN br.status = 'success' THEN 1 ELSE 0 END) as success_count,
		       SUM(CASE WHEN br.status = 'failed' THEN 1 ELSE 0 END) as failed_count
		FROM backup_snapshots bs
		INNER JOIN backup_runs br ON br.id = bs.run_id
		WHERE bs.created_at >= ? AND bs.created_at < ?
		GROUP BY day
		ORDER BY day ASC`,
		startDate, endDate,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query calendar data")
		return
	}
	defer rows.Close()

	days := make([]CalendarDay, 0)
	for rows.Next() {
		var d CalendarDay
		if err := rows.Scan(&d.Date, &d.Count, &d.SuccessCount, &d.FailedCount); err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan calendar row")
			return
		}
		days = append(days, d)
	}
	if err := rows.Err(); err != nil {
		Error(w, http.StatusInternalServerError, "row iteration error")
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"year":  year,
		"month": month,
		"days":  days,
	})
}

// --- helpers ---

// scanSnapshotRow scans a snapshot row from either *sql.Row or *sql.Rows.
func (h *SnapshotsHandler) scanSnapshotRow(row rowScanner) (SnapshotResponse, error) {
	var sn SnapshotResponse
	var sourcePath, dbName sql.NullString
	var checksumSHA256 sql.NullString
	var isEncryptedInt int
	var retentionExpiresAt sql.NullString
	var createdAt string

	err := row.Scan(
		&sn.ID, &sn.RunID, &sn.SourceID,
		&sn.SourceName, &sn.SourceType, &sourcePath, &dbName,
		&sn.ServerID, &sn.ServerName,
		&sn.SnapshotPath, &sn.SizeBytes, &checksumSHA256,
		&isEncryptedInt, &retentionExpiresAt, &createdAt,
	)
	if err != nil {
		return SnapshotResponse{}, err
	}

	sn.IsEncrypted = isEncryptedInt != 0
	sn.CreatedAt = parseTime(createdAt)
	sn.SyncStatuses = []SnapshotSyncStatus{}

	if sourcePath.Valid {
		sn.SourcePath = &sourcePath.String
	}
	if dbName.Valid {
		sn.DBName = &dbName.String
	}
	if checksumSHA256.Valid {
		sn.ChecksumSHA256 = &checksumSHA256.String
	}
	if retentionExpiresAt.Valid && retentionExpiresAt.String != "" {
		t := parseTime(retentionExpiresAt.String)
		sn.RetentionExpiresAt = &t
	}

	return sn, nil
}

// loadSyncStatuses populates SyncStatuses for the given snapshots.
func (h *SnapshotsHandler) loadSyncStatuses(r *http.Request, snapshots []SnapshotResponse, ids []interface{}) {
	if len(ids) == 0 {
		return
	}

	placeholder := "?"
	for i := 1; i < len(ids); i++ {
		placeholder += ",?"
	}

	syncRows, err := h.db.DB().QueryContext(r.Context(), fmt.Sprintf(`
		SELECT dss.snapshot_id, dss.destination_id, d.name, dss.status,
		       dss.retry_count, dss.last_error, dss.synced_at
		FROM destination_sync_status dss
		INNER JOIN destinations d ON d.id = dss.destination_id
		WHERE dss.snapshot_id IN (%s)
		ORDER BY dss.snapshot_id ASC, dss.destination_id ASC`, placeholder), ids...)
	if err != nil {
		return
	}
	defer syncRows.Close()

	// Build a map for quick lookup
	snMap := make(map[int]int, len(snapshots))
	for i, sn := range snapshots {
		snMap[sn.ID] = i
	}

	for syncRows.Next() {
		var snapshotID int
		var ss SnapshotSyncStatus
		var lastErr, syncedAt sql.NullString
		if scanErr := syncRows.Scan(
			&snapshotID, &ss.DestinationID, &ss.DestinationName,
			&ss.Status, &ss.RetryCount, &lastErr, &syncedAt,
		); scanErr != nil {
			continue
		}
		if lastErr.Valid {
			ss.LastError = &lastErr.String
		}
		if syncedAt.Valid {
			ss.SyncedAt = &syncedAt.String
		}
		if idx, ok := snMap[snapshotID]; ok {
			snapshots[idx].SyncStatuses = append(snapshots[idx].SyncStatuses, ss)
		}
	}
}

// pathSnapshotID extracts the snapshot ID from URL path value "id".
func pathSnapshotID(w http.ResponseWriter, r *http.Request) (int, bool) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		Error(w, http.StatusBadRequest, "invalid snapshot id")
		return 0, false
	}
	return id, true
}
