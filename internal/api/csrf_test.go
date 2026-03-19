// internal/api/csrf_test.go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// okHandler is a simple 200 OK handler used in CSRF tests.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// TestCSRF_GETBypassed verifies that GET requests bypass CSRF and return 200.
func TestCSRF_GETBypassed(t *testing.T) {
	handler := CSRFMiddleware(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for GET, got %d", w.Code)
	}
}

// TestCSRF_POSTWithoutToken_Rejected verifies that POST without X-CSRF-Token returns 403.
func TestCSRF_POSTWithoutToken_Rejected(t *testing.T) {
	handler := CSRFMiddleware(okHandler)

	req := httptest.NewRequest(http.MethodPost, "/api/servers", nil)
	// Add the csrf_token cookie so the middleware can check against it
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "abc123"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for POST without X-CSRF-Token, got %d", w.Code)
	}
}

// TestCSRF_POSTWithValidToken_Accepted verifies that POST with matching token returns 200.
func TestCSRF_POSTWithValidToken_Accepted(t *testing.T) {
	handler := CSRFMiddleware(okHandler)

	req := httptest.NewRequest(http.MethodPost, "/api/servers", nil)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "abc123"})
	req.Header.Set("X-CSRF-Token", "abc123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for POST with valid CSRF token, got %d", w.Code)
	}
}

// TestCSRF_POSTWithInvalidToken_Rejected verifies that a token mismatch returns 403.
func TestCSRF_POSTWithInvalidToken_Rejected(t *testing.T) {
	handler := CSRFMiddleware(okHandler)

	req := httptest.NewRequest(http.MethodPost, "/api/servers", nil)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "abc123"})
	req.Header.Set("X-CSRF-Token", "wrong-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for POST with mismatched CSRF token, got %d", w.Code)
	}
}

// TestCSRF_LoginBypassed verifies that POST /api/auth/login bypasses CSRF.
func TestCSRF_LoginBypassed(t *testing.T) {
	handler := CSRFMiddleware(okHandler)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	// No cookie, no header — should still pass
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for POST /api/auth/login (CSRF bypass), got %d", w.Code)
	}
}

// TestCSRF_SetsCookie verifies that the response includes the csrf_token cookie.
func TestCSRF_SetsCookie(t *testing.T) {
	handler := CSRFMiddleware(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var found bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "csrf_token" {
			found = true
			if c.HttpOnly {
				t.Error("csrf_token cookie must NOT be HttpOnly (JS needs to read it)")
			}
			if c.Value == "" {
				t.Error("csrf_token cookie value must not be empty")
			}
		}
	}
	if !found {
		t.Error("expected csrf_token cookie in response")
	}
}
