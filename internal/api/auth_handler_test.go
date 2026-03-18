// internal/api/auth_handler_test.go
package api

import (
	"bytes"
	"encoding/json"
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

func createTestUser(t *testing.T, db *database.Database, username, email, password string, isAdmin bool) int {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	adminInt := 0
	if isAdmin {
		adminInt = 1
	}
	res, err := db.DB().Exec(
		"INSERT INTO users (username, email, password_hash, is_admin) VALUES (?, ?, ?, ?)",
		username, email, hash, adminInt,
	)
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

// TestLoginSuccess verifies 200 OK, httpOnly cookie, SameSite=Strict.
func TestLoginSuccess(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService("test-secret-key-32-bytes-long!!")
	createTestUser(t, db, "alice", "alice@example.com", "password123", false)

	handler := NewAuthHandler(db, authSvc)

	body, _ := json.Marshal(map[string]string{
		"username": "alice",
		"password": "password123",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.Login(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", resp.StatusCode, w.Body.String())
	}

	var found bool
	for _, c := range resp.Cookies() {
		if c.Name == "token" {
			found = true
			if !c.HttpOnly {
				t.Error("cookie should be HttpOnly")
			}
			if c.SameSite != http.SameSiteStrictMode {
				t.Errorf("expected SameSite=Strict, got %v", c.SameSite)
			}
			if c.Value == "" {
				t.Error("cookie value should not be empty")
			}
		}
	}
	if !found {
		t.Error("expected token cookie to be set")
	}

	var respBody map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&respBody); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if respBody["username"] != "alice" {
		t.Errorf("expected username alice in response, got %v", respBody["username"])
	}
}

// TestLoginWrongPassword verifies 401 on bad password.
func TestLoginWrongPassword(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService("test-secret-key-32-bytes-long!!")
	createTestUser(t, db, "bob", "bob@example.com", "correctpass", false)

	handler := NewAuthHandler(db, authSvc)

	body, _ := json.Marshal(map[string]string{
		"username": "bob",
		"password": "wrongpass",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.Login(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// TestLoginUnknownUser verifies 401 for nonexistent user.
func TestLoginUnknownUser(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService("test-secret-key-32-bytes-long!!")

	handler := NewAuthHandler(db, authSvc)

	body, _ := json.Marshal(map[string]string{
		"username": "nobody",
		"password": "anypass",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.Login(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// TestLogout verifies the cookie is cleared.
func TestLogout(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService("test-secret-key-32-bytes-long!!")

	handler := NewAuthHandler(db, authSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: "sometoken"})
	w := httptest.NewRecorder()
	handler.Logout(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var cleared bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "token" {
			cleared = true
			if c.MaxAge != -1 {
				t.Errorf("expected MaxAge=-1 to clear cookie, got %d", c.MaxAge)
			}
		}
	}
	if !cleared {
		t.Error("expected token cookie to be set (with MaxAge=-1) in response")
	}
}

// TestResetPasswordAlwaysReturns200 verifies no email enumeration.
func TestResetPasswordAlwaysReturns200(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService("test-secret-key-32-bytes-long!!")
	handler := NewAuthHandler(db, authSvc)

	// Unknown email still returns 200
	body, _ := json.Marshal(map[string]string{"email": "unknown@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ResetPassword(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (no enumeration), got %d", w.Code)
	}
}

// TestResetPasswordKnownEmailStoresToken verifies token is stored in settings.
func TestResetPasswordKnownEmailStoresToken(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService("test-secret-key-32-bytes-long!!")
	createTestUser(t, db, "carol", "carol@example.com", "pass", false)
	handler := NewAuthHandler(db, authSvc)

	body, _ := json.Marshal(map[string]string{"email": "carol@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ResetPassword(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Verify at least one reset_token entry in settings
	var count int
	err := db.DB().QueryRow("SELECT COUNT(*) FROM settings WHERE key LIKE 'reset_token:%'").Scan(&count)
	if err != nil {
		t.Fatalf("query settings: %v", err)
	}
	if count == 0 {
		t.Error("expected reset token entry in settings table")
	}
}

// TestConfirmResetSuccess verifies password is updated with valid token.
func TestConfirmResetSuccess(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService("test-secret-key-32-bytes-long!!")
	userID := createTestUser(t, db, "dave", "dave@example.com", "oldpass", false)
	handler := NewAuthHandler(db, authSvc)

	// Manually insert a fresh token
	rawToken := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hash := auth.HashResetToken(rawToken)
	expiry := time.Now().Add(1 * time.Hour).Unix()
	settingVal := fmt.Sprintf("%d:%d", userID, expiry)
	_, err := db.DB().Exec("INSERT INTO settings (key, value) VALUES (?, ?)",
		"reset_token:"+hash, settingVal)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}

	body, _ := json.Marshal(map[string]string{
		"token":        rawToken,
		"new_password": "newpass456",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password/confirm", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ConfirmReset(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify new password works
	var storedHash string
	err = db.DB().QueryRow("SELECT password_hash FROM users WHERE id = ?", userID).Scan(&storedHash)
	if err != nil {
		t.Fatalf("query user: %v", err)
	}
	if !auth.CheckPassword("newpass456", storedHash) {
		t.Error("new password does not match stored hash")
	}

	// Token should be deleted
	var count int
	db.DB().QueryRow("SELECT COUNT(*) FROM settings WHERE key = ?", "reset_token:"+hash).Scan(&count)
	if count != 0 {
		t.Error("expected reset token to be deleted after use")
	}
}

// TestConfirmResetExpired verifies expired tokens are rejected.
func TestConfirmResetExpired(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService("test-secret-key-32-bytes-long!!")
	userID := createTestUser(t, db, "eve", "eve@example.com", "oldpass", false)
	handler := NewAuthHandler(db, authSvc)

	rawToken := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hash := auth.HashResetToken(rawToken)
	expiry := time.Now().Add(-2 * time.Hour).Unix() // already expired
	settingVal := fmt.Sprintf("%d:%d", userID, expiry)
	db.DB().Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "reset_token:"+hash, settingVal)

	body, _ := json.Marshal(map[string]string{
		"token":        rawToken,
		"new_password": "newpass456",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password/confirm", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ConfirmReset(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestConfirmResetInvalidToken verifies invalid tokens return 400.
func TestConfirmResetInvalidToken(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService("test-secret-key-32-bytes-long!!")
	handler := NewAuthHandler(db, authSvc)

	body, _ := json.Marshal(map[string]string{
		"token":        "nonexistenttoken",
		"new_password": "newpass",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password/confirm", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ConfirmReset(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestRefreshMiddlewareRefreshesNearExpiry tests that a JWT expiring within 1 hour gets a new cookie.
func TestRefreshMiddlewareRefreshesNearExpiry(t *testing.T) {
	authSvc := auth.NewService("test-secret-key-32-bytes-long!!")

	// Generate a token that will expire in 30 minutes (within 1h window)
	shortSvc := auth.NewServiceWithDuration("test-secret-key-32-bytes-long!!", 30*time.Minute)
	token, err := shortSvc.GenerateToken(1, "testuser", false)
	if err != nil {
		t.Fatal(err)
	}

	innerCalled := false
	handler := authSvc.RefreshMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !innerCalled {
		t.Error("inner handler should have been called")
	}

	var refreshed bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "token" {
			refreshed = true
			if c.Value == token {
				t.Error("expected a new token value, got same token")
			}
		}
	}
	if !refreshed {
		t.Error("expected refreshed token cookie")
	}
}

// TestRefreshMiddlewareNoRefreshFarExpiry tests that a JWT expiring in >1h is NOT refreshed.
func TestRefreshMiddlewareNoRefreshFarExpiry(t *testing.T) {
	authSvc := auth.NewService("test-secret-key-32-bytes-long!!")

	// Generate a token that will expire in 12 hours (outside 1h window)
	token, err := authSvc.GenerateToken(1, "testuser", false)
	if err != nil {
		t.Fatal(err)
	}

	handler := authSvc.RefreshMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	for _, c := range w.Result().Cookies() {
		if c.Name == "token" {
			t.Error("expected no token cookie refresh for long-lived token")
		}
	}
}

// TestRefreshMiddlewareNoTokenPassesThrough tests that requests without token pass through unchanged.
func TestRefreshMiddlewareNoTokenPassesThrough(t *testing.T) {
	authSvc := auth.NewService("test-secret-key-32-bytes-long!!")

	innerCalled := false
	handler := authSvc.RefreshMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !innerCalled {
		t.Error("inner handler should be called even without token")
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "token" {
			t.Error("expected no token cookie when request has no token")
		}
	}
}

// TestRouterHealthCheck verifies the /api/health endpoint.
func TestRouterHealthCheck(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService("test-secret-key-32-bytes-long!!")
	router := NewRouter(db, authSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ok") {
		t.Errorf("expected ok in body, got: %s", w.Body.String())
	}
}
