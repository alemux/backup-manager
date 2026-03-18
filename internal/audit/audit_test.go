// internal/audit/audit_test.go
package audit

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/backupmanager/backupmanager/internal/database"
)

func newTestDB(t *testing.T) *database.Database {
	t.Helper()
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func intPtr(i int) *int { return &i }

// TestAuditLog verifies that Log inserts an entry that can be retrieved.
func TestAuditLog(t *testing.T) {
	db := newTestDB(t)
	svc := NewAuditService(db)

	// Insert a real user so the FK constraint is satisfied.
	hash, _ := auth.HashPassword("pass")
	res, err := db.DB().Exec(
		"INSERT INTO users (username, email, password_hash, is_admin) VALUES (?,?,?,?)",
		"loguser", "loguser@example.com", hash, 0,
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	uidInt64, _ := res.LastInsertId()
	uid := int(uidInt64)

	if err := svc.Log(&uid, "POST /api/servers", "servers", "127.0.0.1", "test details"); err != nil {
		t.Fatalf("Log: %v", err)
	}

	entries, total, err := svc.Query(QueryOptions{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.UserID == nil || *e.UserID != uid {
		t.Errorf("expected user_id=%d, got %v", uid, e.UserID)
	}
	if e.Action != "POST /api/servers" {
		t.Errorf("unexpected action: %q", e.Action)
	}
	if e.Target != "servers" {
		t.Errorf("unexpected target: %q", e.Target)
	}
	if e.IPAddress != "127.0.0.1" {
		t.Errorf("unexpected ip: %q", e.IPAddress)
	}
	if e.Details != "test details" {
		t.Errorf("unexpected details: %q", e.Details)
	}
	if e.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}
}

// TestAuditQuery_ByAction verifies filtering by action.
func TestAuditQuery_ByAction(t *testing.T) {
	db := newTestDB(t)
	svc := NewAuditService(db)

	_ = svc.Log(nil, "POST /api/servers", "servers", "127.0.0.1", "")
	_ = svc.Log(nil, "DELETE /api/servers/1", "1", "127.0.0.1", "")
	_ = svc.Log(nil, "POST /api/servers", "servers", "127.0.0.1", "")

	entries, total, err := svc.Query(QueryOptions{Action: "POST /api/servers", Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total=2, got %d", total)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Action != "POST /api/servers" {
			t.Errorf("unexpected action: %q", e.Action)
		}
	}
}

// TestAuditQuery_ByDateRange verifies filtering by date range.
func TestAuditQuery_ByDateRange(t *testing.T) {
	db := newTestDB(t)
	svc := NewAuditService(db)

	now := time.Now().UTC()
	past := now.Add(-2 * time.Hour)
	future := now.Add(2 * time.Hour)

	// Insert directly with explicit timestamps to control created_at values.
	_, err := db.DB().Exec(
		`INSERT INTO audit_log (action, target, ip_address, details, created_at) VALUES (?, ?, ?, ?, ?)`,
		"OLD_ACTION", "old", "127.0.0.1", "", now.Add(-3*time.Hour).Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert old entry: %v", err)
	}
	_, err = db.DB().Exec(
		`INSERT INTO audit_log (action, target, ip_address, details, created_at) VALUES (?, ?, ?, ?, ?)`,
		"NEW_ACTION", "new", "127.0.0.1", "", now.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert new entry: %v", err)
	}

	entries, total, err := svc.Query(QueryOptions{
		DateFrom: &past,
		DateTo:   &future,
		Page:     1,
		PerPage:  10,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total=1 within range, got %d", total)
	}
	if len(entries) != 1 || entries[0].Action != "NEW_ACTION" {
		t.Errorf("unexpected entries: %v", entries)
	}
}

// TestAuditQuery_Pagination verifies page/per_page behaviour.
func TestAuditQuery_Pagination(t *testing.T) {
	db := newTestDB(t)
	svc := NewAuditService(db)

	for i := 0; i < 5; i++ {
		if err := svc.Log(nil, "TEST_ACTION", "target", "127.0.0.1", ""); err != nil {
			t.Fatalf("Log: %v", err)
		}
	}

	// Page 1: first 3
	p1, total, err := svc.Query(QueryOptions{Page: 1, PerPage: 3})
	if err != nil {
		t.Fatalf("Query page 1: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total=5, got %d", total)
	}
	if len(p1) != 3 {
		t.Errorf("expected 3 entries on page 1, got %d", len(p1))
	}

	// Page 2: remaining 2
	p2, total2, err := svc.Query(QueryOptions{Page: 2, PerPage: 3})
	if err != nil {
		t.Fatalf("Query page 2: %v", err)
	}
	if total2 != 5 {
		t.Errorf("expected total=5 on page 2, got %d", total2)
	}
	if len(p2) != 2 {
		t.Errorf("expected 2 entries on page 2, got %d", len(p2))
	}
}

// TestAuditExportCSV verifies that ExportCSV produces valid CSV output.
func TestAuditExportCSV(t *testing.T) {
	db := newTestDB(t)
	svc := NewAuditService(db)

	// Create a real user so the FK is satisfied.
	hash, _ := auth.HashPassword("pass")
	res, err := db.DB().Exec(
		"INSERT INTO users (username, email, password_hash, is_admin) VALUES (?,?,?,?)",
		"csvuser", "csvuser@example.com", hash, 0,
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	uidInt64, _ := res.LastInsertId()
	uid := int(uidInt64)

	if err := svc.Log(&uid, "POST /api/servers", "servers", "10.0.0.1", "created server"); err != nil {
		t.Fatalf("Log 1: %v", err)
	}
	if err := svc.Log(nil, "DELETE /api/servers/3", "3", "10.0.0.2", "deleted server"); err != nil {
		t.Fatalf("Log 2: %v", err)
	}

	var buf bytes.Buffer
	if err := svc.ExportCSV(&buf, QueryOptions{}); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}

	r := csv.NewReader(strings.NewReader(buf.String()))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}

	// Header + 2 data rows
	if len(records) != 3 {
		t.Fatalf("expected 3 CSV rows (header + 2 data), got %d", len(records))
	}

	header := records[0]
	expectedHeader := []string{"id", "user_id", "action", "target", "ip_address", "details", "created_at"}
	for i, h := range expectedHeader {
		if header[i] != h {
			t.Errorf("header[%d]: expected %q, got %q", i, h, header[i])
		}
	}

	// Row 1
	row1 := records[1]
	expectedUID := fmt.Sprintf("%d", uid)
	if row1[1] != expectedUID {
		t.Errorf("expected user_id=%q, got %q", expectedUID, row1[1])
	}
	if row1[2] != "POST /api/servers" {
		t.Errorf("unexpected action: %q", row1[2])
	}

	// Row 2 — nil user_id should be empty string
	row2 := records[2]
	if row2[1] != "" {
		t.Errorf("expected empty user_id for nil, got %q", row2[1])
	}
}

// TestAuditMiddleware verifies that POST requests are logged but GET are not.
func TestAuditMiddleware(t *testing.T) {
	db := newTestDB(t)
	svc := NewAuditService(db)

	// Create a test user and generate a JWT so GetClaims works.
	authSvc := auth.NewService("test-secret-key-32-bytes-long!!")
	hash, _ := auth.HashPassword("pass")
	res, err := db.DB().Exec(
		"INSERT INTO users (username, email, password_hash, is_admin) VALUES (?,?,?,?)",
		"miduser", "mid@example.com", hash, 0,
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	userID, _ := res.LastInsertId()
	token, _ := authSvc.GenerateToken(int(userID), "miduser", false)

	// Simple handler that always returns 200.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with auth middleware so GetClaims is populated, then audit middleware.
	wrapped := authSvc.RequireAuth(AuditMiddleware(svc, inner))

	// Helper to send a request with the JWT cookie.
	makeReq := func(method, path string) {
		req := httptest.NewRequest(method, path, nil)
		req.AddCookie(&http.Cookie{Name: "token", Value: token})
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)
	}

	// GET should NOT be logged.
	makeReq(http.MethodGet, "/api/servers")

	entries, total, err := svc.Query(QueryOptions{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("Query after GET: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0 entries after GET, got %d; entries: %v", total, entries)
	}

	// POST should be logged.
	makeReq(http.MethodPost, "/api/servers")

	entries, total, err = svc.Query(QueryOptions{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("Query after POST: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1 entry after POST, got %d", total)
	}
	if len(entries) > 0 {
		e := entries[0]
		if e.Action != "POST /api/servers" {
			t.Errorf("unexpected action: %q", e.Action)
		}
		if e.UserID == nil || *e.UserID != int(userID) {
			t.Errorf("expected user_id=%d, got %v", userID, e.UserID)
		}
	}

	// DELETE should also be logged.
	makeReq(http.MethodDelete, "/api/servers/5")

	_, total, err = svc.Query(QueryOptions{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("Query after DELETE: %v", err)
	}
	if total != 2 {
		t.Errorf("expected 2 entries after POST+DELETE, got %d", total)
	}
}
