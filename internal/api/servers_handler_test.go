// internal/api/servers_handler_test.go
package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/backupmanager/backupmanager/internal/database"
)

const testSecret = "test-secret-key-32-bytes-long!!"

// authenticatedRequest creates an HTTP request with a valid JWT cookie for a test user.
// For state-changing methods (POST, PUT, DELETE), it also sets the CSRF cookie and header
// so that requests pass the CSRFMiddleware added to the router.
func authenticatedRequest(t *testing.T, method, path string, body io.Reader, db *database.Database, authSvc *auth.Service) *httptest.ResponseRecorder {
	t.Helper()

	// Create a test user if none exists yet (ignore duplicate errors)
	hash, err := auth.HashPassword("testpass")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	var userID int
	row := db.DB().QueryRow("SELECT id FROM users WHERE username = 'testuser'")
	if err := row.Scan(&userID); err != nil {
		res, err := db.DB().Exec(
			"INSERT INTO users (username, email, password_hash, is_admin) VALUES (?, ?, ?, ?)",
			"testuser", "testuser@example.com", hash, 1,
		)
		if err != nil {
			t.Fatalf("insert test user: %v", err)
		}
		id, _ := res.LastInsertId()
		userID = int(id)
	}

	token, err := authSvc.GenerateToken(userID, "testuser", true)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	req := httptest.NewRequest(method, path, body)
	req.AddCookie(&http.Cookie{Name: "token", Value: token})
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Add CSRF token for state-changing methods so the CSRFMiddleware passes them through.
	csrfSafeMethods := map[string]bool{
		http.MethodGet:     true,
		http.MethodHead:    true,
		http.MethodOptions: true,
	}
	if !csrfSafeMethods[method] && path != "/api/auth/login" {
		const testCSRFToken = "test-csrf-token-for-unit-tests"
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: testCSRFToken})
		req.Header.Set("X-CSRF-Token", testCSRFToken)
	}

	router := NewRouter(db, authSvc)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestListServersEmpty(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	w := authenticatedRequest(t, http.MethodGet, "/api/servers", nil, db, authSvc)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result []interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result == nil || len(result) != 0 {
		t.Errorf("expected empty array [], got %v", result)
	}
}

func TestCreateServerLinux(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	payload, _ := json.Marshal(map[string]interface{}{
		"name":            "Production Linux",
		"type":            "linux",
		"host":            "192.168.1.100",
		"port":            22,
		"connection_type": "ssh",
		"username":        "backup-user",
		"password":        "secret",
	})

	w := authenticatedRequest(t, http.MethodPost, "/api/servers", bytes.NewReader(payload), db, authSvc)

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
	if result["name"] != "Production Linux" {
		t.Errorf("expected name 'Production Linux', got %v", result["name"])
	}
	if result["type"] != "linux" {
		t.Errorf("expected type 'linux', got %v", result["type"])
	}
	if result["connection_type"] != "ssh" {
		t.Errorf("expected connection_type 'ssh', got %v", result["connection_type"])
	}
	// Sensitive fields must not be returned
	if _, ok := result["encrypted_password"]; ok {
		t.Error("encrypted_password must not be in response")
	}
	if _, ok := result["password"]; ok {
		t.Error("password must not be in response")
	}
}

func TestCreateServerWindows(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	payload, _ := json.Marshal(map[string]interface{}{
		"name":            "Windows Server",
		"type":            "windows",
		"host":            "10.0.0.5",
		"port":            21,
		"connection_type": "ftp",
		"username":        "ftpuser",
		"password":        "ftppass",
	})

	w := authenticatedRequest(t, http.MethodPost, "/api/servers", bytes.NewReader(payload), db, authSvc)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["type"] != "windows" {
		t.Errorf("expected type 'windows', got %v", result["type"])
	}
	if result["connection_type"] != "ftp" {
		t.Errorf("expected connection_type 'ftp', got %v", result["connection_type"])
	}
}

func TestCreateServerValidation(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	tests := []struct {
		name    string
		payload map[string]interface{}
	}{
		{
			name: "missing name",
			payload: map[string]interface{}{
				"type":            "linux",
				"host":            "1.2.3.4",
				"port":            22,
				"connection_type": "ssh",
			},
		},
		{
			name: "invalid type",
			payload: map[string]interface{}{
				"name":            "Test",
				"type":            "macos",
				"host":            "1.2.3.4",
				"port":            22,
				"connection_type": "ssh",
			},
		},
		{
			name: "linux with ftp",
			payload: map[string]interface{}{
				"name":            "Bad Server",
				"type":            "linux",
				"host":            "1.2.3.4",
				"port":            21,
				"connection_type": "ftp",
			},
		},
		{
			name: "windows with ssh",
			payload: map[string]interface{}{
				"name":            "Bad Windows",
				"type":            "windows",
				"host":            "1.2.3.4",
				"port":            22,
				"connection_type": "ssh",
			},
		},
		{
			name: "missing host",
			payload: map[string]interface{}{
				"name":            "Test",
				"type":            "linux",
				"port":            22,
				"connection_type": "ssh",
			},
		},
		{
			name: "missing port",
			payload: map[string]interface{}{
				"name":            "Test",
				"type":            "linux",
				"host":            "1.2.3.4",
				"connection_type": "ssh",
			},
		},
		{
			name: "missing connection_type",
			payload: map[string]interface{}{
				"name": "Test",
				"type": "linux",
				"host": "1.2.3.4",
				"port": 22,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(tc.payload)
			w := authenticatedRequest(t, http.MethodPost, "/api/servers", bytes.NewReader(payload), db, authSvc)
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d; body: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestGetServer(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	// Create a server
	payload, _ := json.Marshal(map[string]interface{}{
		"name":            "My Linux Server",
		"type":            "linux",
		"host":            "192.168.1.50",
		"port":            22,
		"connection_type": "ssh",
		"username":        "admin",
	})
	wCreate := authenticatedRequest(t, http.MethodPost, "/api/servers", bytes.NewReader(payload), db, authSvc)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create failed: %d %s", wCreate.Code, wCreate.Body.String())
	}

	var created map[string]interface{}
	json.NewDecoder(wCreate.Body).Decode(&created)
	id := int(created["id"].(float64))

	// Get it back
	w := authenticatedRequest(t, http.MethodGet, "/api/servers/"+itoa(id), nil, db, authSvc)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if int(result["id"].(float64)) != id {
		t.Errorf("expected id %d, got %v", id, result["id"])
	}
	if result["name"] != "My Linux Server" {
		t.Errorf("expected name 'My Linux Server', got %v", result["name"])
	}
	if result["host"] != "192.168.1.50" {
		t.Errorf("expected host '192.168.1.50', got %v", result["host"])
	}
	if result["status"] != "unknown" {
		t.Errorf("expected status 'unknown', got %v", result["status"])
	}
	if result["created_at"] == nil {
		t.Error("expected created_at in response")
	}
	if result["updated_at"] == nil {
		t.Error("expected updated_at in response")
	}
	// Sensitive fields must not be returned
	if _, ok := result["encrypted_password"]; ok {
		t.Error("encrypted_password must not be in response")
	}
	if _, ok := result["ssh_key_path"]; ok {
		t.Error("ssh_key_path must not be in response")
	}
}

func TestGetServerNotFound(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	w := authenticatedRequest(t, http.MethodGet, "/api/servers/99999", nil, db, authSvc)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestUpdateServer(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	// Create
	payload, _ := json.Marshal(map[string]interface{}{
		"name":            "Original Name",
		"type":            "linux",
		"host":            "10.0.0.1",
		"port":            22,
		"connection_type": "ssh",
		"username":        "user1",
	})
	wCreate := authenticatedRequest(t, http.MethodPost, "/api/servers", bytes.NewReader(payload), db, authSvc)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create failed: %d", wCreate.Code)
	}
	var created map[string]interface{}
	json.NewDecoder(wCreate.Body).Decode(&created)
	id := int(created["id"].(float64))

	// Update
	update, _ := json.Marshal(map[string]interface{}{
		"name":            "Updated Name",
		"type":            "linux",
		"host":            "10.0.0.99",
		"port":            22,
		"connection_type": "ssh",
		"username":        "user2",
	})
	w := authenticatedRequest(t, http.MethodPut, "/api/servers/"+itoa(id), bytes.NewReader(update), db, authSvc)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["name"] != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got %v", result["name"])
	}
	if result["host"] != "10.0.0.99" {
		t.Errorf("expected host '10.0.0.99', got %v", result["host"])
	}
	if result["username"] != "user2" {
		t.Errorf("expected username 'user2', got %v", result["username"])
	}
}

func TestDeleteServer(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	// Create
	payload, _ := json.Marshal(map[string]interface{}{
		"name":            "To Delete",
		"type":            "linux",
		"host":            "10.0.0.1",
		"port":            22,
		"connection_type": "ssh",
	})
	wCreate := authenticatedRequest(t, http.MethodPost, "/api/servers", bytes.NewReader(payload), db, authSvc)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create failed: %d", wCreate.Code)
	}
	var created map[string]interface{}
	json.NewDecoder(wCreate.Body).Decode(&created)
	id := int(created["id"].(float64))

	// Delete
	wDel := authenticatedRequest(t, http.MethodDelete, "/api/servers/"+itoa(id), nil, db, authSvc)
	if wDel.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d; body: %s", wDel.Code, wDel.Body.String())
	}

	// Get should return 404
	wGet := authenticatedRequest(t, http.MethodGet, "/api/servers/"+itoa(id), nil, db, authSvc)
	if wGet.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", wGet.Code)
	}
}

func TestTestConnection(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	payload, _ := json.Marshal(map[string]interface{}{
		"host":            "192.168.1.1",
		"port":            22,
		"connection_type": "ssh",
		"username":        "admin",
		"password":        "pass",
	})

	w := authenticatedRequest(t, http.MethodPost, "/api/servers/test-connection", bytes.NewReader(payload), db, authSvc)

	// Endpoint must exist and return a proper JSON structure
	if w.Code == http.StatusNotFound {
		t.Fatalf("endpoint not found (404); body: %s", w.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if _, ok := result["success"]; !ok {
		t.Error("expected 'success' field in response")
	}
	if _, ok := result["message"]; !ok {
		t.Error("expected 'message' field in response")
	}
}

func TestServersRequireAuth(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	router := NewRouter(db, authSvc)

	// Request without token
	req := httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", w.Code)
	}
}

// itoa converts an int to string (avoids importing strconv everywhere).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	result := []byte{}
	for n > 0 {
		result = append([]byte{byte('0' + n%10)}, result...)
		n /= 10
	}
	return string(result)
}
