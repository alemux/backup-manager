// internal/api/jobs_handler.go
package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
	"github.com/robfig/cron/v3"
)

// TriggerFunc is a function that triggers a job by ID and returns the run ID.
// It is kept as a simple type to avoid tight coupling to the scheduler.
type TriggerFunc func(jobID int) (int, error)

// AnalyzeFunc is a function that runs a dry-run analysis for a job and returns estimated sizes.
type AnalyzeFunc func(jobID int) (interface{}, error)

// JobsHandler handles all /api/jobs and /api/runs routes.
type JobsHandler struct {
	db      *database.Database
	trigger TriggerFunc
	analyze AnalyzeFunc
}

// NewJobsHandler constructs a JobsHandler.
func NewJobsHandler(db *database.Database, trigger TriggerFunc) *JobsHandler {
	return &JobsHandler{db: db, trigger: trigger}
}

// SetAnalyzeFunc sets the function used to perform dry-run analysis.
func (h *JobsHandler) SetAnalyzeFunc(fn AnalyzeFunc) {
	h.analyze = fn
}

// --- response types ---

// lastRunInfo represents the most recent backup run for a job.
type lastRunInfo struct {
	ID             int        `json:"id"`
	Status         string     `json:"status"`
	StartedAt      *time.Time `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	TotalSizeBytes int64      `json:"total_size_bytes"`
}

// JobResponse is the job JSON structure returned by the API.
type JobResponse struct {
	ID                 int          `json:"id"`
	Name               string       `json:"name"`
	ServerID           int          `json:"server_id"`
	ServerName         string       `json:"server_name"`
	Schedule           string       `json:"schedule"`
	RetentionDaily     int          `json:"retention_daily"`
	RetentionWeekly    int          `json:"retention_weekly"`
	RetentionMonthly   int          `json:"retention_monthly"`
	BandwidthLimitMbps *int         `json:"bandwidth_limit_mbps"`
	TimeoutMinutes     int          `json:"timeout_minutes"`
	Enabled            bool         `json:"enabled"`
	SourceIDs          []int        `json:"source_ids"`
	LastRun            *lastRunInfo `json:"last_run"`
	CreatedAt          time.Time    `json:"created_at"`
}

// jobRequest is the incoming payload for create/update.
type jobRequest struct {
	Name               string `json:"name"`
	ServerID           int    `json:"server_id"`
	Schedule           string `json:"schedule"`
	RetentionDaily     *int   `json:"retention_daily"`
	RetentionWeekly    *int   `json:"retention_weekly"`
	RetentionMonthly   *int   `json:"retention_monthly"`
	BandwidthLimitMbps *int   `json:"bandwidth_limit_mbps"`
	TimeoutMinutes     *int   `json:"timeout_minutes"`
	Enabled            *bool  `json:"enabled"`
	SourceIDs          []int  `json:"source_ids"`
}

// RunResponse is the run JSON structure returned by the API.
type RunResponse struct {
	ID             int        `json:"id"`
	JobID          int        `json:"job_id"`
	Status         string     `json:"status"`
	StartedAt      *time.Time `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	TotalSizeBytes int64      `json:"total_size_bytes"`
	FilesCopied    int        `json:"files_copied"`
	ErrorMessage   *string    `json:"error_message"`
	CreatedAt      time.Time  `json:"created_at"`
}

// --- List ---

// List handles GET /api/jobs
func (h *JobsHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.DB().QueryContext(r.Context(), `
		SELECT
			bj.id, bj.name, bj.server_id, s.name,
			bj.schedule,
			bj.retention_daily, bj.retention_weekly, bj.retention_monthly,
			bj.bandwidth_limit_mbps, bj.timeout_minutes, bj.enabled,
			bj.created_at,
			br.id, br.status, br.started_at, br.finished_at, br.total_size_bytes
		FROM backup_jobs bj
		INNER JOIN servers s ON s.id = bj.server_id
		LEFT JOIN backup_runs br ON br.id = (
			SELECT id FROM backup_runs WHERE job_id = bj.id ORDER BY id DESC LIMIT 1
		)
		ORDER BY bj.id ASC
	`)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query jobs")
		return
	}
	defer rows.Close()

	jobs := make([]JobResponse, 0)
	for rows.Next() {
		job, err := h.scanJobRow(rows)
		if err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan job")
			return
		}
		job.SourceIDs = h.loadSourceIDs(r, job.ID)
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		Error(w, http.StatusInternalServerError, "row iteration error")
		return
	}

	JSON(w, http.StatusOK, jobs)
}

// Create handles POST /api/jobs
func (h *JobsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if errMsg := h.validateJobRequest(r, req, 0); errMsg != "" {
		Error(w, http.StatusBadRequest, errMsg)
		return
	}

	// Set defaults
	retDaily := 7
	if req.RetentionDaily != nil {
		retDaily = *req.RetentionDaily
	}
	retWeekly := 4
	if req.RetentionWeekly != nil {
		retWeekly = *req.RetentionWeekly
	}
	retMonthly := 3
	if req.RetentionMonthly != nil {
		retMonthly = *req.RetentionMonthly
	}
	timeout := 120
	if req.TimeoutMinutes != nil {
		timeout = *req.TimeoutMinutes
	}
	enabled := 1
	if req.Enabled != nil && !*req.Enabled {
		enabled = 0
	}

	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := h.db.DB().BeginTx(r.Context(), nil)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(r.Context(),
		`INSERT INTO backup_jobs (name, server_id, schedule, retention_daily, retention_weekly, retention_monthly,
		  bandwidth_limit_mbps, timeout_minutes, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Name, req.ServerID, req.Schedule,
		retDaily, retWeekly, retMonthly,
		req.BandwidthLimitMbps, timeout, enabled,
		now, now,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to create job")
		return
	}

	jobID, _ := result.LastInsertId()

	for _, srcID := range req.SourceIDs {
		if _, err := tx.ExecContext(r.Context(),
			`INSERT INTO backup_job_sources (job_id, source_id) VALUES (?, ?)`,
			jobID, srcID,
		); err != nil {
			Error(w, http.StatusInternalServerError, "failed to associate source")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		Error(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	job, found := h.fetchJob(r, int(jobID))
	if !found {
		Error(w, http.StatusInternalServerError, "failed to retrieve created job")
		return
	}

	JSON(w, http.StatusCreated, job)
}

// Get handles GET /api/jobs/{id}
func (h *JobsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	job, found := h.fetchJob(r, id)
	if !found {
		Error(w, http.StatusNotFound, "job not found")
		return
	}

	JSON(w, http.StatusOK, job)
}

// Update handles PUT /api/jobs/{id}
func (h *JobsHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	// Verify job exists
	var exists int
	err := h.db.DB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM backup_jobs WHERE id=?", id).Scan(&exists)
	if err != nil || exists == 0 {
		Error(w, http.StatusNotFound, "job not found")
		return
	}

	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if errMsg := h.validateJobRequest(r, req, id); errMsg != "" {
		Error(w, http.StatusBadRequest, errMsg)
		return
	}

	retDaily := 7
	if req.RetentionDaily != nil {
		retDaily = *req.RetentionDaily
	}
	retWeekly := 4
	if req.RetentionWeekly != nil {
		retWeekly = *req.RetentionWeekly
	}
	retMonthly := 3
	if req.RetentionMonthly != nil {
		retMonthly = *req.RetentionMonthly
	}
	timeout := 120
	if req.TimeoutMinutes != nil {
		timeout = *req.TimeoutMinutes
	}
	enabled := 1
	if req.Enabled != nil && !*req.Enabled {
		enabled = 0
	}

	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := h.db.DB().BeginTx(r.Context(), nil)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(r.Context(),
		`UPDATE backup_jobs SET name=?, server_id=?, schedule=?,
		  retention_daily=?, retention_weekly=?, retention_monthly=?,
		  bandwidth_limit_mbps=?, timeout_minutes=?, enabled=?, updated_at=?
		 WHERE id=?`,
		req.Name, req.ServerID, req.Schedule,
		retDaily, retWeekly, retMonthly,
		req.BandwidthLimitMbps, timeout, enabled,
		now, id,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to update job")
		return
	}

	// Replace sources
	if _, err := tx.ExecContext(r.Context(), "DELETE FROM backup_job_sources WHERE job_id=?", id); err != nil {
		Error(w, http.StatusInternalServerError, "failed to update job sources")
		return
	}
	for _, srcID := range req.SourceIDs {
		if _, err := tx.ExecContext(r.Context(),
			`INSERT INTO backup_job_sources (job_id, source_id) VALUES (?, ?)`,
			id, srcID,
		); err != nil {
			Error(w, http.StatusInternalServerError, "failed to associate source")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		Error(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	job, found := h.fetchJob(r, id)
	if !found {
		Error(w, http.StatusInternalServerError, "failed to retrieve updated job")
		return
	}

	JSON(w, http.StatusOK, job)
}

// Delete handles DELETE /api/jobs/{id}
func (h *JobsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	res, err := h.db.DB().ExecContext(r.Context(), "DELETE FROM backup_jobs WHERE id=?", id)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to delete job")
		return
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		Error(w, http.StatusNotFound, "job not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Trigger handles POST /api/jobs/{id}/trigger
func (h *JobsHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	// Verify job exists
	var exists int
	err := h.db.DB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM backup_jobs WHERE id=?", id).Scan(&exists)
	if err != nil || exists == 0 {
		Error(w, http.StatusNotFound, "job not found")
		return
	}

	if h.trigger == nil {
		Error(w, http.StatusInternalServerError, "trigger not configured")
		return
	}

	runID, err := h.trigger(id)
	if err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("failed to trigger job: %v", err))
		return
	}

	JSON(w, http.StatusAccepted, map[string]int{"run_id": runID})
}

// Analyze handles POST /api/jobs/{id}/analyze
// Runs rsync --dry-run for each source and returns estimated transfer sizes.
func (h *JobsHandler) Analyze(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	// Verify job exists
	var exists int
	err := h.db.DB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM backup_jobs WHERE id=?", id).Scan(&exists)
	if err != nil || exists == 0 {
		Error(w, http.StatusNotFound, "job not found")
		return
	}

	if h.analyze == nil {
		Error(w, http.StatusInternalServerError, "analyze not configured")
		return
	}

	result, err := h.analyze(id)
	if err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("analysis failed: %v", err))
		return
	}

	JSON(w, http.StatusOK, result)
}

// --- ListRuns ---

// ListRuns handles GET /api/runs
func (h *JobsHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page := 1
	perPage := 20

	if v := q.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := q.Get("per_page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			perPage = n
		}
	}

	// Build WHERE clause
	where := []string{"1=1"}
	args := []interface{}{}

	if v := q.Get("job_id"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			where = append(where, "br.job_id = ?")
			args = append(args, n)
		}
	}
	if v := q.Get("server_id"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			where = append(where, "bj.server_id = ?")
			args = append(args, n)
		}
	}
	if v := q.Get("status"); v != "" {
		where = append(where, "br.status = ?")
		args = append(args, v)
	}
	if v := q.Get("from"); v != "" {
		where = append(where, "br.started_at >= ?")
		args = append(args, v)
	}
	if v := q.Get("to"); v != "" {
		where = append(where, "br.started_at <= ?")
		args = append(args, v)
	}

	whereClause := strings.Join(where, " AND ")

	// Count total
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	var total int
	err := h.db.DB().QueryRowContext(r.Context(),
		fmt.Sprintf(`SELECT COUNT(*) FROM backup_runs br
		 INNER JOIN backup_jobs bj ON bj.id = br.job_id
		 WHERE %s`, whereClause),
		countArgs...,
	).Scan(&total)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to count runs")
		return
	}

	offset := (page - 1) * perPage
	args = append(args, perPage, offset)

	rows, err := h.db.DB().QueryContext(r.Context(),
		fmt.Sprintf(`SELECT br.id, br.job_id, br.status, br.started_at, br.finished_at,
		  br.total_size_bytes, br.files_copied, br.error_message, br.created_at
		 FROM backup_runs br
		 INNER JOIN backup_jobs bj ON bj.id = br.job_id
		 WHERE %s
		 ORDER BY br.id DESC
		 LIMIT ? OFFSET ?`, whereClause),
		args...,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query runs")
		return
	}
	defer rows.Close()

	runs := make([]RunResponse, 0)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan run")
			return
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		Error(w, http.StatusInternalServerError, "row iteration error")
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"runs":     runs,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

// GetRunLogs handles GET /api/runs/{id}/logs
func (h *JobsHandler) GetRunLogs(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	row := h.db.DB().QueryRowContext(r.Context(),
		`SELECT id, job_id, status, started_at, finished_at,
		  total_size_bytes, files_copied, error_message, created_at
		 FROM backup_runs WHERE id=?`, id)

	run, err := scanRunRow(row)
	if err == sql.ErrNoRows {
		Error(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query run")
		return
	}

	// Load snapshot results for each source
	type snapshotResult struct {
		ID              int       `json:"id"`
		SourceID        int       `json:"source_id"`
		SnapshotPath    string    `json:"snapshot_path"`
		SizeBytes       int64     `json:"size_bytes"`
		ChecksumSHA256  *string   `json:"checksum_sha256"`
		CreatedAt       time.Time `json:"created_at"`
	}

	snapRows, err := h.db.DB().QueryContext(r.Context(),
		`SELECT id, source_id, snapshot_path, size_bytes, checksum_sha256, created_at
		 FROM backup_snapshots WHERE run_id=? ORDER BY id ASC`, id)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query snapshots")
		return
	}
	defer snapRows.Close()

	snapshots := make([]snapshotResult, 0)
	for snapRows.Next() {
		var sn snapshotResult
		var checksum sql.NullString
		var createdAt string
		if err := snapRows.Scan(&sn.ID, &sn.SourceID, &sn.SnapshotPath, &sn.SizeBytes, &checksum, &createdAt); err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan snapshot")
			return
		}
		if checksum.Valid {
			sn.ChecksumSHA256 = &checksum.String
		}
		sn.CreatedAt = parseTime(createdAt)
		snapshots = append(snapshots, sn)
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"run":       run,
		"snapshots": snapshots,
	})
}

// --- helpers ---

func (h *JobsHandler) fetchJob(r *http.Request, id int) (JobResponse, bool) {
	row := h.db.DB().QueryRowContext(r.Context(), `
		SELECT
			bj.id, bj.name, bj.server_id, s.name,
			bj.schedule,
			bj.retention_daily, bj.retention_weekly, bj.retention_monthly,
			bj.bandwidth_limit_mbps, bj.timeout_minutes, bj.enabled,
			bj.created_at,
			br.id, br.status, br.started_at, br.finished_at, br.total_size_bytes
		FROM backup_jobs bj
		INNER JOIN servers s ON s.id = bj.server_id
		LEFT JOIN backup_runs br ON br.id = (
			SELECT id FROM backup_runs WHERE job_id = bj.id ORDER BY id DESC LIMIT 1
		)
		WHERE bj.id = ?
	`, id)

	job, err := h.scanJobRow(row)
	if err == sql.ErrNoRows {
		return JobResponse{}, false
	}
	if err != nil {
		return JobResponse{}, false
	}
	job.SourceIDs = h.loadSourceIDs(r, job.ID)
	return job, true
}

// scanJobRow scans a job row from either *sql.Row or *sql.Rows.
func (h *JobsHandler) scanJobRow(row rowScanner) (JobResponse, error) {
	var job JobResponse
	var bandwidth sql.NullInt64
	var enabledInt int
	var createdAt string

	// last_run nullable fields
	var lrID sql.NullInt64
	var lrStatus sql.NullString
	var lrStartedAt sql.NullString
	var lrFinishedAt sql.NullString
	var lrTotalSize sql.NullInt64

	err := row.Scan(
		&job.ID, &job.Name, &job.ServerID, &job.ServerName,
		&job.Schedule,
		&job.RetentionDaily, &job.RetentionWeekly, &job.RetentionMonthly,
		&bandwidth, &job.TimeoutMinutes, &enabledInt,
		&createdAt,
		&lrID, &lrStatus, &lrStartedAt, &lrFinishedAt, &lrTotalSize,
	)
	if err != nil {
		return JobResponse{}, err
	}

	if bandwidth.Valid {
		v := int(bandwidth.Int64)
		job.BandwidthLimitMbps = &v
	}
	job.Enabled = enabledInt != 0
	job.CreatedAt = parseTime(createdAt)
	job.SourceIDs = []int{}

	if lrID.Valid {
		lr := &lastRunInfo{
			ID:     int(lrID.Int64),
			Status: lrStatus.String,
		}
		if lrStartedAt.Valid {
			t := parseTime(lrStartedAt.String)
			lr.StartedAt = &t
		}
		if lrFinishedAt.Valid {
			t := parseTime(lrFinishedAt.String)
			lr.FinishedAt = &t
		}
		if lrTotalSize.Valid {
			lr.TotalSizeBytes = lrTotalSize.Int64
		}
		job.LastRun = lr
	}

	return job, nil
}

func (h *JobsHandler) loadSourceIDs(r *http.Request, jobID int) []int {
	rows, err := h.db.DB().QueryContext(r.Context(),
		`SELECT source_id FROM backup_job_sources WHERE job_id=? ORDER BY source_id ASC`, jobID)
	if err != nil {
		return []int{}
	}
	defer rows.Close()

	ids := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func (h *JobsHandler) validateJobRequest(r *http.Request, req jobRequest, jobID int) string {
	if strings.TrimSpace(req.Name) == "" {
		return "name is required"
	}
	if req.ServerID <= 0 {
		return "server_id is required"
	}
	if strings.TrimSpace(req.Schedule) == "" {
		return "schedule is required"
	}

	// Validate cron schedule using robfig/cron
	if _, err := cron.ParseStandard(req.Schedule); err != nil {
		return fmt.Sprintf("invalid cron schedule: %v", err)
	}

	// Verify server exists
	var exists int
	err := h.db.DB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM servers WHERE id=?", req.ServerID).Scan(&exists)
	if err != nil || exists == 0 {
		return "server not found"
	}

	// Verify each source_id belongs to the server
	for _, srcID := range req.SourceIDs {
		var serverID int
		err := h.db.DB().QueryRowContext(r.Context(),
			"SELECT server_id FROM backup_sources WHERE id=?", srcID,
		).Scan(&serverID)
		if err == sql.ErrNoRows {
			return fmt.Sprintf("source %d not found", srcID)
		}
		if err != nil {
			return fmt.Sprintf("failed to validate source %d", srcID)
		}
		if serverID != req.ServerID {
			return fmt.Sprintf("source %d does not belong to server %d", srcID, req.ServerID)
		}
	}

	return ""
}

func scanRunRow(row rowScanner) (RunResponse, error) {
	var run RunResponse
	var startedAt sql.NullString
	var finishedAt sql.NullString
	var errMsg sql.NullString
	var createdAt string

	err := row.Scan(
		&run.ID, &run.JobID, &run.Status,
		&startedAt, &finishedAt,
		&run.TotalSizeBytes, &run.FilesCopied,
		&errMsg, &createdAt,
	)
	if err != nil {
		return RunResponse{}, err
	}

	if startedAt.Valid {
		t := parseTime(startedAt.String)
		run.StartedAt = &t
	}
	if finishedAt.Valid {
		t := parseTime(finishedAt.String)
		run.FinishedAt = &t
	}
	if errMsg.Valid {
		run.ErrorMessage = &errMsg.String
	}
	run.CreatedAt = parseTime(createdAt)

	return run, nil
}

func scanRun(rows *sql.Rows) (RunResponse, error) {
	return scanRunRow(rows)
}
