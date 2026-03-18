// internal/api/router.go
package api

import (
	"net/http"

	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/backupmanager/backupmanager/internal/database"
)

// NewRouter builds and returns the main HTTP router.
func NewRouter(db *database.Database, authSvc *auth.Service) http.Handler {
	mux := http.NewServeMux()
	authHandler := NewAuthHandler(db, authSvc)

	// Public routes
	mux.HandleFunc("POST /api/auth/login", authHandler.Login)
	mux.HandleFunc("POST /api/auth/logout", authHandler.Logout)
	mux.HandleFunc("POST /api/auth/reset-password", authHandler.ResetPassword)
	mux.HandleFunc("POST /api/auth/reset-password/confirm", authHandler.ConfirmReset)

	// Health check
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return mux
}
