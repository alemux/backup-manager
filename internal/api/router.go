// internal/api/router.go
package api

import (
	"net/http"

	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/backupmanager/backupmanager/internal/database"
	"github.com/backupmanager/backupmanager/internal/health"
)

// NewRouter builds and returns the main HTTP router.
// triggerFn is optional; pass nil to disable the trigger endpoint (e.g. in unit tests
// that don't exercise it).
func NewRouter(db *database.Database, authSvc *auth.Service, triggerFn ...TriggerFunc) http.Handler {
	mux := http.NewServeMux()
	authHandler := NewAuthHandler(db, authSvc)
	serversHandler := NewServersHandler(db)
	sourcesHandler := NewSourcesHandler(db)
	healthSvc := health.NewHealthService(db)
	healthHandler := NewHealthHandler(healthSvc)

	var trigger TriggerFunc
	if len(triggerFn) > 0 {
		trigger = triggerFn[0]
	}
	jobsHandler := NewJobsHandler(db, trigger)

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

	protected.HandleFunc("GET /api/jobs", jobsHandler.List)
	protected.HandleFunc("POST /api/jobs", jobsHandler.Create)
	protected.HandleFunc("GET /api/jobs/{id}", jobsHandler.Get)
	protected.HandleFunc("PUT /api/jobs/{id}", jobsHandler.Update)
	protected.HandleFunc("DELETE /api/jobs/{id}", jobsHandler.Delete)
	protected.HandleFunc("POST /api/jobs/{id}/trigger", jobsHandler.Trigger)
	protected.HandleFunc("GET /api/runs", jobsHandler.ListRuns)
	protected.HandleFunc("GET /api/runs/{id}/logs", jobsHandler.GetRunLogs)

	mux.Handle("/api/servers", authSvc.RequireAuth(protected))
	mux.Handle("/api/servers/", authSvc.RequireAuth(protected))
	mux.Handle("/api/sources/", authSvc.RequireAuth(protected))
	mux.Handle("/api/jobs", authSvc.RequireAuth(protected))
	mux.Handle("/api/jobs/", authSvc.RequireAuth(protected))
	mux.Handle("/api/runs", authSvc.RequireAuth(protected))
	mux.Handle("/api/runs/", authSvc.RequireAuth(protected))

	// Health monitoring endpoints (protected).
	protected.HandleFunc("GET /api/health/servers", healthHandler.GetAllHealth)
	protected.HandleFunc("GET /api/health/servers/{id}/history", healthHandler.GetServerHistory)
	mux.Handle("/api/health/", authSvc.RequireAuth(protected))

	return mux
}
