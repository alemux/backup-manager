// internal/api/csrf.go
package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// GenerateCSRFToken creates a random 32-byte hex token.
func GenerateCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback: return a static value (should never happen in practice)
		return "fallback-csrf-token"
	}
	return hex.EncodeToString(b)
}

// CSRFMiddleware implements double-submit cookie CSRF protection.
// - Sets a non-httpOnly "csrf_token" cookie with a random token on every response
// - For state-changing requests (POST, PUT, DELETE), requires X-CSRF-Token header
//   to match the csrf_token cookie value
// - GET, HEAD, OPTIONS requests bypass CSRF check
// - /api/auth/login bypasses CSRF (no cookie exists yet)
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Safe methods bypass CSRF check (but still get the cookie set below)
		safeMethods := map[string]bool{
			http.MethodGet:     true,
			http.MethodHead:    true,
			http.MethodOptions: true,
		}

		// Login endpoint bypasses CSRF (no cookie exists yet on first visit)
		isLogin := r.URL.Path == "/api/auth/login"

		if !safeMethods[r.Method] && !isLogin {
			// Require the X-CSRF-Token header to match the csrf_token cookie
			cookie, err := r.Cookie("csrf_token")
			if err != nil || cookie.Value == "" {
				Error(w, http.StatusForbidden, "CSRF token missing")
				return
			}
			headerToken := r.Header.Get("X-CSRF-Token")
			if headerToken == "" || headerToken != cookie.Value {
				Error(w, http.StatusForbidden, "CSRF token invalid")
				return
			}
		}

		// Generate and set a fresh CSRF token cookie on every response.
		// Use the existing cookie value if present to avoid churn, but always
		// ensure the cookie is present so the client can read it.
		tokenValue := ""
		if cookie, err := r.Cookie("csrf_token"); err == nil && cookie.Value != "" {
			tokenValue = cookie.Value
		} else {
			tokenValue = GenerateCSRFToken()
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "csrf_token",
			Value:    tokenValue,
			HttpOnly: false, // Must be readable by JavaScript
			SameSite: http.SameSiteStrictMode,
			Path:     "/",
			MaxAge:   86400, // 24 hours
		})

		next.ServeHTTP(w, r)
	})
}
