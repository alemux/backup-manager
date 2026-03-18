// internal/auth/middleware.go
package auth

import (
	"context"
	"net/http"
	"time"
)

type contextKey string

const claimsKey contextKey = "claims"

func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("token")
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}

		claims, err := s.ValidateToken(cookie.Value)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"invalid token"}`))
			return
		}

		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetClaims(r *http.Request) *Claims {
	claims, _ := r.Context().Value(claimsKey).(*Claims)
	return claims
}

// RefreshMiddleware transparently refreshes the JWT cookie when it expires within 1 hour.
// Requests without a token (or with an invalid token) pass through to the next handler unchanged.
func (s *Service) RefreshMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("token")
		if err != nil {
			// No token — not our concern, pass through
			next.ServeHTTP(w, r)
			return
		}

		claims, err := s.ValidateToken(cookie.Value)
		if err != nil {
			// Invalid token — pass through (RequireAuth will handle rejection)
			next.ServeHTTP(w, r)
			return
		}

		// If the token expires within 1 hour, issue a new one
		if time.Until(claims.ExpiresAt.Time) < time.Hour {
			newToken, err := s.GenerateToken(claims.UserID, claims.Username, claims.IsAdmin)
			if err == nil {
				http.SetCookie(w, &http.Cookie{
					Name:     "token",
					Value:    newToken,
					HttpOnly: true,
					SameSite: http.SameSiteStrictMode,
					Path:     "/",
					MaxAge:   86400, // 24 hours
				})
			}
		}

		next.ServeHTTP(w, r)
	})
}
