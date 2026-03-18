// internal/api/audit_handler_test.go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/backupmanager/backupmanager/internal/audit"
	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/backupmanager/backupmanager/internal/database"
)

// buildAuditRouter builds a minimal mux that exposes only the audit endpoints.
func buildAuditRouter(authSvc *auth.Service, auditSvc *audit.AuditService) http.Handler {
	mux := http.NewServeMux()
	h := NewAuditHandler(auditSvc)

	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/audit", h.List)
	protected.HandleFunc("GET /api/audit/export", h.Export)

	mux.Handle("/api/audit", authSvc.RequireAuth(protected))
	mux.Handle("/api/audit/", authSvc.RequireAuth(protected))
	return mux
}

// auditRequest sends an authenticated GET request against the audit router.
// The db passed in is the same one used by auditSvc.
func auditRequest(t *testing.T, method, path string, db *database.Database, authSvc *auth.Service, auditSvc *audit.AuditService) *httptest.ResponseRecorder {
	t.Helper()

	hash, err := auth.HashPassword("pass")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	var userID int
	row := db.DB().QueryRow("SELECT id FROM users WHERE username = 'audit_testuser'")
	if err := row.Scan(&userID); err != nil {
		res, err := db.DB().Exec(
			"INSERT INTO users (username, email, password_hash, is_admin) VALUES (?,?,?,?)",
			"audit_testuser", "audit_testuser@example.com", hash, 1,
		)
		if err != nil {
			t.Fatalf("insert test user: %v", err)
		}
		id, _ := res.LastInsertId()
		userID = int(id)
	}

	token, err := authSvc.GenerateToken(userID, "audit_testuser", true)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	router := buildAuditRouter(authSvc, auditSvc)
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: token})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestAuditHandlerList verifies that GET /api/audit returns JSON with pagination fields.
func TestAuditHandlerList(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	auditSvc := audit.NewAuditService(db)

	_ = auditSvc.Log(nil, "POST /api/servers", "servers", "127.0.0.1", "detail")
	_ = auditSvc.Log(nil, "DELETE /api/servers/2", "2", "127.0.0.1", "")

	w := auditRequest(t, http.MethodGet, "/api/audit", db, authSvc, auditSvc)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if result["total"] == nil {
		t.Error("expected 'total' field")
	}
	if result["data"] == nil {
		t.Error("expected 'data' field")
	}
	if result["page"] == nil {
		t.Error("expected 'page' field")
	}
	if result["per_page"] == nil {
		t.Error("expected 'per_page' field")
	}

	total := int(result["total"].(float64))
	if total != 2 {
		t.Errorf("expected total=2, got %d", total)
	}
}

// TestAuditHandlerListFilter verifies filtering via query params.
func TestAuditHandlerListFilter(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	auditSvc := audit.NewAuditService(db)

	_ = auditSvc.Log(nil, "POST /api/servers", "servers", "1.1.1.1", "")
	_ = auditSvc.Log(nil, "DELETE /api/servers/1", "1", "1.1.1.1", "")

	w := auditRequest(t, http.MethodGet, "/api/audit?action=DELETE+%2Fapi%2Fservers%2F1", db, authSvc, auditSvc)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]interface{}
	json.NewDecoder(w.Body).Decode(&result)
	if int(result["total"].(float64)) != 1 {
		t.Errorf("expected total=1 when filtering by action, got %v", result["total"])
	}
}

// TestAuditHandlerExport verifies that GET /api/audit/export returns CSV.
func TestAuditHandlerExport(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	auditSvc := audit.NewAuditService(db)

	_ = auditSvc.Log(nil, "POST /api/jobs", "jobs", "2.2.2.2", "")

	w := auditRequest(t, http.MethodGet, "/api/audit/export", db, authSvc, auditSvc)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("expected Content-Type text/csv, got %q", ct)
	}

	body := w.Body.String()
	if !strings.Contains(body, "id,user_id,action") {
		t.Errorf("expected CSV header, got: %q", body)
	}
	if !strings.Contains(body, "POST /api/jobs") {
		t.Errorf("expected data row in CSV, got: %q", body)
	}
}

// TestAuditHandlerRequiresAuth verifies that unauthenticated requests are rejected.
func TestAuditHandlerRequiresAuth(t *testing.T) {
	authSvc := auth.NewService(testSecret)
	auditSvc := audit.NewAuditService(newTestDB(t))

	router := buildAuditRouter(authSvc, auditSvc)
	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// TestAuditHandlerPagination verifies page/per_page query parameters.
func TestAuditHandlerPagination(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	auditSvc := audit.NewAuditService(db)

	for i := 0; i < 6; i++ {
		_ = auditSvc.Log(nil, fmt.Sprintf("ACTION_%d", i), "target", "1.1.1.1", "")
	}

	w := auditRequest(t, http.MethodGet, "/api/audit?page=2&per_page=4", db, authSvc, auditSvc)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]interface{}
	json.NewDecoder(w.Body).Decode(&result)

	if int(result["total"].(float64)) != 6 {
		t.Errorf("expected total=6, got %v", result["total"])
	}
	data := result["data"].([]interface{})
	if len(data) != 2 {
		t.Errorf("expected 2 entries on page 2, got %d", len(data))
	}
}

// TestAuditHandlerFromToFilter verifies date range filtering via API.
func TestAuditHandlerFromToFilter(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	auditSvc := audit.NewAuditService(db)

	now := time.Now().UTC()

	// Insert one entry in the past (outside range) and one recent.
	_, _ = db.DB().Exec(
		`INSERT INTO audit_log (action, target, ip_address, details, created_at) VALUES (?, ?, ?, ?, ?)`,
		"OLD", "old", "1.1.1.1", "", now.Add(-5*time.Hour).Format(time.RFC3339),
	)
	_, _ = db.DB().Exec(
		`INSERT INTO audit_log (action, target, ip_address, details, created_at) VALUES (?, ?, ?, ?, ?)`,
		"NEW", "new", "1.1.1.1", "", now.Format(time.RFC3339),
	)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)
	path := fmt.Sprintf("/api/audit?from=%s&to=%s", from, to)

	w := auditRequest(t, http.MethodGet, path, db, authSvc, auditSvc)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	json.NewDecoder(w.Body).Decode(&result)
	if int(result["total"].(float64)) != 1 {
		t.Errorf("expected total=1, got %v", result["total"])
	}
}
