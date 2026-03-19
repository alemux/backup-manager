// internal/api/destinations_handler_test.go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/backupmanager/backupmanager/internal/database"
)

// destAuthRequest sends an authenticated request through the full router.
func destAuthRequest(t *testing.T, method, path string, body []byte, db *database.Database, authSvc *auth.Service) *httptest.ResponseRecorder {
	t.Helper()

	hash, err := auth.HashPassword("testpass")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	var userID int
	row := db.DB().QueryRow("SELECT id FROM users WHERE username = 'dest_testuser'")
	if err := row.Scan(&userID); err != nil {
		res, err := db.DB().Exec(
			"INSERT INTO users (username, email, password_hash, is_admin) VALUES (?, ?, ?, ?)",
			"dest_testuser", "dest_testuser@example.com", hash, 1,
		)
		if err != nil {
			t.Fatalf("insert test user: %v", err)
		}
		id, _ := res.LastInsertId()
		userID = int(id)
	}

	token, err := authSvc.GenerateToken(userID, "dest_testuser", true)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	var bodyReader *bytes.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	var req *http.Request
	if bodyReader != nil {
		req = httptest.NewRequest(method, path, bodyReader)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.AddCookie(&http.Cookie{Name: "token", Value: token})
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	csrfSafeMethods := map[string]bool{
		http.MethodGet:     true,
		http.MethodHead:    true,
		http.MethodOptions: true,
	}
	if !csrfSafeMethods[method] {
		const testCSRFToken = "test-csrf-token-for-unit-tests"
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: testCSRFToken})
		req.Header.Set("X-CSRF-Token", testCSRFToken)
	}

	router := NewRouter(db, authSvc)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func destPayload(t *testing.T, name, destType, path string) []byte {
	t.Helper()
	b, _ := json.Marshal(map[string]interface{}{
		"name": name,
		"type": destType,
		"path": path,
	})
	return b
}

// insertTestDestination directly inserts a destination row for test setup.
func insertTestDestination(t *testing.T, db *database.Database, name, path string) int {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.DB().Exec(
		`INSERT INTO destinations (name, type, path, is_primary, retention_daily, retention_weekly, retention_monthly, enabled, created_at)
		 VALUES (?, 'local', ?, 0, 7, 4, 3, 1, ?)`,
		name, path, now,
	)
	if err != nil {
		t.Fatalf("insert destination: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

// insertTestSnapshotForDest creates a full snapshot fixture for destination sync tests.
func insertTestSnapshotForDest(t *testing.T, db *database.Database) int {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)

	srvRes, err := db.DB().Exec(
		`INSERT INTO servers (name, type, host, port, connection_type, status, created_at, updated_at)
		 VALUES ('srv', 'linux', 'localhost', 22, 'ssh', 'online', ?, ?)`, now, now,
	)
	if err != nil {
		t.Fatalf("insert server: %v", err)
	}
	srvID, _ := srvRes.LastInsertId()

	srcRes, err := db.DB().Exec(
		`INSERT INTO backup_sources (server_id, name, type, priority, enabled, created_at)
		 VALUES (?, 'src', 'web', 0, 1, ?)`, srvID, now,
	)
	if err != nil {
		t.Fatalf("insert source: %v", err)
	}
	srcID, _ := srcRes.LastInsertId()

	jobRes, err := db.DB().Exec(
		`INSERT INTO backup_jobs (name, server_id, schedule, retention_daily, retention_weekly, retention_monthly, timeout_minutes, enabled, created_at, updated_at)
		 VALUES ('job', ?, '0 3 * * *', 7, 4, 3, 120, 1, ?, ?)`, srvID, now, now,
	)
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}
	jobID, _ := jobRes.LastInsertId()

	runRes, err := db.DB().Exec(
		`INSERT INTO backup_runs (job_id, status, created_at) VALUES (?, 'success', ?)`, jobID, now,
	)
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}
	runID, _ := runRes.LastInsertId()

	snapRes, err := db.DB().Exec(
		`INSERT INTO backup_snapshots (run_id, source_id, snapshot_path, size_bytes, created_at)
		 VALUES (?, ?, '/tmp/snap.tar.gz', 0, ?)`,
		runID, srcID, now,
	)
	if err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	snapID, _ := snapRes.LastInsertId()
	return int(snapID)
}

func TestCreateDestination(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	w := destAuthRequest(t, http.MethodPost, "/api/destinations",
		destPayload(t, "NAS Backup", "nas", "/mnt/nas"),
		db, authSvc)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["id"] == nil {
		t.Error("expected id in response")
	}
	if result["name"] != "NAS Backup" {
		t.Errorf("expected name 'NAS Backup', got %v", result["name"])
	}
	if result["type"] != "nas" {
		t.Errorf("expected type 'nas', got %v", result["type"])
	}
	if result["path"] != "/mnt/nas" {
		t.Errorf("expected path '/mnt/nas', got %v", result["path"])
	}
	if result["retention_daily"] != float64(7) {
		t.Errorf("expected retention_daily=7, got %v", result["retention_daily"])
	}
	if result["enabled"] != true {
		t.Errorf("expected enabled=true, got %v", result["enabled"])
	}
}

func TestCreateDestinationValidation(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	tests := []struct {
		name    string
		payload map[string]interface{}
	}{
		{
			name:    "missing name",
			payload: map[string]interface{}{"type": "local", "path": "/tmp"},
		},
		{
			name:    "missing path",
			payload: map[string]interface{}{"name": "Test", "type": "local"},
		},
		{
			name:    "invalid type",
			payload: map[string]interface{}{"name": "Test", "type": "ftp", "path": "/tmp"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(tc.payload)
			w := destAuthRequest(t, http.MethodPost, "/api/destinations", payload, db, authSvc)
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d; body: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestListDestinations(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	// Create two destinations via API.
	destAuthRequest(t, http.MethodPost, "/api/destinations",
		destPayload(t, "Dest One", "local", "/backups/1"), db, authSvc)
	destAuthRequest(t, http.MethodPost, "/api/destinations",
		destPayload(t, "Dest Two", "nas", "/backups/2"), db, authSvc)

	w := destAuthRequest(t, http.MethodGet, "/api/destinations", nil, db, authSvc)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result []interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 destinations, got %d", len(result))
	}
}

func TestDeleteDestination(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	destID := insertTestDestination(t, db, "To Delete", "/tmp/del")

	wDel := destAuthRequest(t, http.MethodDelete, "/api/destinations/"+itoa(destID), nil, db, authSvc)
	if wDel.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d; body: %s", wDel.Code, wDel.Body.String())
	}

	// Confirm it no longer exists.
	var count int
	db.DB().QueryRow("SELECT COUNT(*) FROM destinations WHERE id=?", destID).Scan(&count)
	if count != 0 {
		t.Error("expected destination to be deleted from DB")
	}
}

func TestDeleteDestinationNotFound(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	w := destAuthRequest(t, http.MethodDelete, "/api/destinations/99999", nil, db, authSvc)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestUpdateDestination(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	destID := insertTestDestination(t, db, "Original", "/tmp/orig")

	payload, _ := json.Marshal(map[string]interface{}{
		"name": "Updated",
		"type": "usb",
		"path": "/mnt/usb",
	})
	w := destAuthRequest(t, http.MethodPut, "/api/destinations/"+itoa(destID), payload, db, authSvc)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["name"] != "Updated" {
		t.Errorf("expected name 'Updated', got %v", result["name"])
	}
	if result["type"] != "usb" {
		t.Errorf("expected type 'usb', got %v", result["type"])
	}
}

func TestRetrySync(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	destID := insertTestDestination(t, db, "NAS", "/mnt/nas")
	snapID := insertTestSnapshotForDest(t, db)

	// Insert a failed sync status entry.
	db.DB().Exec(
		`INSERT INTO destination_sync_status (snapshot_id, destination_id, status, retry_count, last_error)
		 VALUES (?, ?, 'failed', 5, 'timeout')`,
		snapID, destID,
	)

	// Reset retry_count to allow RetryFailed to find it as failed.
	db.DB().Exec(
		`UPDATE destination_sync_status SET retry_count = 3 WHERE snapshot_id=? AND destination_id=?`,
		snapID, destID,
	)

	w := destAuthRequest(t, http.MethodPost,
		"/api/destinations/"+itoa(destID)+"/retry/"+itoa(snapID),
		nil, db, authSvc)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify the status was reset to pending.
	var status string
	db.DB().QueryRow(
		`SELECT status FROM destination_sync_status WHERE snapshot_id=? AND destination_id=?`,
		snapID, destID,
	).Scan(&status)

	if status != "pending" {
		t.Errorf("expected status 'pending' after retry, got %q", status)
	}
}

func TestRetrySyncNotFound(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	w := destAuthRequest(t, http.MethodPost, "/api/destinations/999/retry/888", nil, db, authSvc)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
