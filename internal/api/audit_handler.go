// internal/api/audit_handler.go
package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/backupmanager/backupmanager/internal/audit"
)

// AuditHandler handles /api/audit routes.
type AuditHandler struct {
	svc *audit.AuditService
}

// NewAuditHandler constructs an AuditHandler.
func NewAuditHandler(svc *audit.AuditService) *AuditHandler {
	return &AuditHandler{svc: svc}
}

// List handles GET /api/audit
// Query parameters: user_id, action, from (RFC3339), to (RFC3339), page, per_page.
func (h *AuditHandler) List(w http.ResponseWriter, r *http.Request) {
	opts, err := parseQueryOptions(r)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}

	entries, total, err := h.svc.Query(opts)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query audit log")
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"data":     entries,
		"total":    total,
		"page":     opts.Page,
		"per_page": opts.PerPage,
	})
}

// Export handles GET /api/audit/export
// Returns all matching entries as a CSV download.
func (h *AuditHandler) Export(w http.ResponseWriter, r *http.Request) {
	opts, err := parseQueryOptions(r)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	// Export always returns all rows — no pagination.
	opts.Page = 1
	opts.PerPage = 0

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="audit_log.csv"`)

	if err := h.svc.ExportCSV(w, opts); err != nil {
		// Cannot write an error JSON at this point because we may have started writing.
		// Log the error best-effort.
		_, _ = fmt.Fprintf(w, "\n# export error: %v\n", err)
	}
}

// parseQueryOptions extracts common filter/pagination parameters from query string.
func parseQueryOptions(r *http.Request) (audit.QueryOptions, error) {
	q := r.URL.Query()
	opts := audit.QueryOptions{
		Page:    1,
		PerPage: 20,
	}

	if v := q.Get("user_id"); v != "" {
		uid, err := strconv.Atoi(v)
		if err != nil || uid <= 0 {
			return opts, fmt.Errorf("invalid user_id")
		}
		opts.UserID = &uid
	}

	if v := q.Get("action"); v != "" {
		opts.Action = v
	}

	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return opts, fmt.Errorf("invalid from date (use RFC3339)")
		}
		opts.DateFrom = &t
	}

	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return opts, fmt.Errorf("invalid to date (use RFC3339)")
		}
		opts.DateTo = &t
	}

	if v := q.Get("page"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 {
			return opts, fmt.Errorf("invalid page")
		}
		opts.Page = p
	}

	if v := q.Get("per_page"); v != "" {
		pp, err := strconv.Atoi(v)
		if err != nil || pp < 1 {
			return opts, fmt.Errorf("invalid per_page")
		}
		opts.PerPage = pp
	}

	return opts, nil
}
