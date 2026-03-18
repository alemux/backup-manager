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
	serversHandler := NewServersHandler(db)
	sourcesHandler := NewSourcesHandler(db)

	// Public routes
	mux.HandleFunc("POST /api/auth/login", authHandler.Login)
	mux.HandleFunc("POST /api/auth/logout", authHandler.Logout)
	mux.HandleFunc("POST /api/auth/reset-password", authHandler.ResetPassword)
	mux.HandleFunc("POST /api/auth/reset-password/confirm", authHandler.ConfirmReset)

	// Health check
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Protected routes (require authentication)
	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/servers", serversHandler.List)
	protected.HandleFunc("POST /api/servers", serversHandler.Create)
	protected.HandleFunc("POST /api/servers/test-connection", serversHandler.TestConnection)
	protected.HandleFunc("GET /api/servers/{id}", serversHandler.Get)
	protected.HandleFunc("PUT /api/servers/{id}", serversHandler.Update)
	protected.HandleFunc("DELETE /api/servers/{id}", serversHandler.Delete)
	protected.HandleFunc("POST /api/servers/{id}/discover", serversHandler.Discover)
	protected.HandleFunc("GET /api/servers/{id}/sources", sourcesHandler.List)
	protected.HandleFunc("POST /api/servers/{id}/sources", sourcesHandler.Create)
	protected.HandleFunc("PUT /api/sources/{id}", sourcesHandler.Update)
	protected.HandleFunc("DELETE /api/sources/{id}", sourcesHandler.Delete)

	mux.Handle("/api/servers", authSvc.RequireAuth(protected))
	mux.Handle("/api/servers/", authSvc.RequireAuth(protected))
	mux.Handle("/api/sources/", authSvc.RequireAuth(protected))

	return mux
}
