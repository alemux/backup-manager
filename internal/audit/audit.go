// internal/audit/audit.go
package audit

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/backupmanager/backupmanager/internal/database"
)

// AuditEntry represents a single audit log record.
type AuditEntry struct {
	ID        int       `json:"id"`
	UserID    *int      `json:"user_id"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	IPAddress string    `json:"ip_address"`
	Details   string    `json:"details"`
	CreatedAt time.Time `json:"created_at"`
}

// QueryOptions holds filter and pagination parameters for querying audit entries.
type QueryOptions struct {
	UserID   *int
	Action   string
	DateFrom *time.Time
	DateTo   *time.Time
	Page     int
	PerPage  int
}

// AuditService handles recording and querying audit log entries.
type AuditService struct {
	db *database.Database
}

// NewAuditService constructs an AuditService.
func NewAuditService(db *database.Database) *AuditService {
	return &AuditService{db: db}
}

// Log records an audit entry in the database.
func (s *AuditService) Log(userID *int, action, target, ip, details string) error {
	_, err := s.db.DB().Exec(
		`INSERT INTO audit_log (user_id, action, target, ip_address, details, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		userID, action, target, ip, details, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}

// Query returns audit entries filtered by the provided options.
// It returns the matching entries, the total count before pagination, and any error.
func (s *AuditService) Query(opts QueryOptions) ([]AuditEntry, int, error) {
	if opts.Page < 1 {
		opts.Page = 1
	}
	if opts.PerPage < 1 {
		opts.PerPage = 20
	}

	where, args := buildWhere(opts)

	// Total count
	var total int
	countQuery := "SELECT COUNT(*) FROM audit_log" + where
	if err := s.db.DB().QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit log: %w", err)
	}

	// Paginated rows
	offset := (opts.Page - 1) * opts.PerPage
	query := `SELECT id, user_id, action, COALESCE(target,''), COALESCE(ip_address,''), COALESCE(details,''), created_at
	          FROM audit_log` + where + ` ORDER BY id DESC LIMIT ? OFFSET ?`
	queryArgs := append(args, opts.PerPage, offset)

	rows, err := s.db.DB().Query(query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query audit log: %w", err)
	}
	defer rows.Close()

	entries := make([]AuditEntry, 0)
	for rows.Next() {
		var e AuditEntry
		var createdAt string
		if err := rows.Scan(&e.ID, &e.UserID, &e.Action, &e.Target, &e.IPAddress, &e.Details, &createdAt); err != nil {
			return nil, 0, fmt.Errorf("scan audit entry: %w", err)
		}
		e.CreatedAt = parseTime(createdAt)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate audit log: %w", err)
	}

	return entries, total, nil
}

// ExportCSV writes all matching audit entries as CSV to the given writer.
func (s *AuditService) ExportCSV(w io.Writer, opts QueryOptions) error {
	// Ignore pagination for export — retrieve all matching rows.
	where, args := buildWhere(opts)
	query := `SELECT id, user_id, action, COALESCE(target,''), COALESCE(ip_address,''), COALESCE(details,''), created_at
	          FROM audit_log` + where + ` ORDER BY id ASC`

	rows, err := s.db.DB().Query(query, args...)
	if err != nil {
		return fmt.Errorf("query audit log for export: %w", err)
	}
	defer rows.Close()

	cw := csv.NewWriter(w)
	// Header
	if err := cw.Write([]string{"id", "user_id", "action", "target", "ip_address", "details", "created_at"}); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}

	for rows.Next() {
		var e AuditEntry
		var createdAt string
		if err := rows.Scan(&e.ID, &e.UserID, &e.Action, &e.Target, &e.IPAddress, &e.Details, &createdAt); err != nil {
			return fmt.Errorf("scan audit entry: %w", err)
		}

		userIDStr := ""
		if e.UserID != nil {
			userIDStr = fmt.Sprintf("%d", *e.UserID)
		}

		if err := cw.Write([]string{
			fmt.Sprintf("%d", e.ID),
			userIDStr,
			e.Action,
			e.Target,
			e.IPAddress,
			e.Details,
			createdAt,
		}); err != nil {
			return fmt.Errorf("write CSV row: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate audit log rows: %w", err)
	}

	cw.Flush()
	return cw.Error()
}

// AuditMiddleware logs all state-changing requests (POST, PUT, DELETE).
// It extracts the authenticated user from the JWT claims (if present) and
// records the action and target derived from the request method and path.
func AuditMiddleware(auditSvc *AuditService, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)

		// Only log state-changing methods.
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodDelete:
		default:
			return
		}

		var userID *int
		if claims := auth.GetClaims(r); claims != nil {
			id := claims.UserID
			userID = &id
		}

		ip := extractIP(r)
		action := r.Method + " " + r.URL.Path
		target := extractTarget(r.URL.Path)

		// Best-effort logging — do not fail the request if this errors.
		_ = auditSvc.Log(userID, action, target, ip, "")
	})
}

// --- helpers ---

// buildWhere constructs the SQL WHERE clause and argument list from QueryOptions.
func buildWhere(opts QueryOptions) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	if opts.UserID != nil {
		conditions = append(conditions, "user_id = ?")
		args = append(args, *opts.UserID)
	}
	if opts.Action != "" {
		conditions = append(conditions, "action = ?")
		args = append(args, opts.Action)
	}
	if opts.DateFrom != nil {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, opts.DateFrom.UTC().Format(time.RFC3339))
	}
	if opts.DateTo != nil {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, opts.DateTo.UTC().Format(time.RFC3339))
	}

	if len(conditions) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

// extractIP returns the client IP address from the request.
// It prefers the X-Forwarded-For header (first entry) over RemoteAddr.
func extractIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	addr := r.RemoteAddr
	// Strip port if present (host:port format)
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		addr = addr[:idx]
	}
	return addr
}

// extractTarget returns a resource identifier from the URL path.
// For paths ending in a numeric segment (e.g. /api/servers/42), it returns that ID.
// Otherwise it returns the last non-empty path segment.
func extractTarget(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// parseTime parses a datetime string stored by SQLite.
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
