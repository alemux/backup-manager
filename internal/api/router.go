// internal/api/router.go
package api

import (
	"net/http"

	"github.com/backupmanager/backupmanager/internal/audit"
	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/backupmanager/backupmanager/internal/database"
	"github.com/backupmanager/backupmanager/internal/health"
	"github.com/backupmanager/backupmanager/internal/integrity"
	"github.com/backupmanager/backupmanager/internal/notification"
)

// WSHub is the interface that the WebSocket hub must satisfy.
// Using an interface here avoids a direct import cycle between api and websocket packages.
type WSHub interface {
	HandleWebSocket(w http.ResponseWriter, r *http.Request)
}

// NewRouter builds and returns the main HTTP router.
// triggerFn is optional; pass nil to disable the trigger endpoint (e.g. in unit tests
// that don't exercise it).
// notificationManager is optional; pass nil to disable notification endpoints.
func NewRouter(db *database.Database, authSvc *auth.Service, triggerFn ...TriggerFunc) http.Handler {
	return newRouterInternal(db, authSvc, nil, nil, triggerFn...)
}

// NewRouterWithNotifications builds and returns the main HTTP router with a notification manager.
func NewRouterWithNotifications(db *database.Database, authSvc *auth.Service, mgr *notification.Manager, triggerFn ...TriggerFunc) http.Handler {
	return newRouterInternal(db, authSvc, mgr, nil, triggerFn...)
}

// NewRouterWithWebSocket builds and returns the main HTTP router with WebSocket support.
func NewRouterWithWebSocket(db *database.Database, authSvc *auth.Service, mgr *notification.Manager, hub WSHub, triggerFn ...TriggerFunc) http.Handler {
	return newRouterInternal(db, authSvc, mgr, hub, triggerFn...)
}

// newRouterWithNotifications is kept as an internal alias for backward compatibility.
func newRouterWithNotifications(db *database.Database, authSvc *auth.Service, mgr *notification.Manager, triggerFn ...TriggerFunc) http.Handler {
	return newRouterInternal(db, authSvc, mgr, nil, triggerFn...)
}

func newRouterInternal(db *database.Database, authSvc *auth.Service, mgr *notification.Manager, hub WSHub, triggerFn ...TriggerFunc) http.Handler {
	mux := http.NewServeMux()
	rateLimiter := NewRateLimiter(db)
	authHandler := NewAuthHandler(db, authSvc)
	serversHandler := NewServersHandlerWithKey(db, authSvc.CredentialKey())
	sourcesHandler := NewSourcesHandler(db)
	healthSvc := health.NewHealthService(db)
	healthHandler := NewHealthHandler(healthSvc)
	notificationsHandler := NewNotificationsHandler(db, mgr)

	var trigger TriggerFunc
	if len(triggerFn) > 0 {
		trigger = triggerFn[0]
	}
	jobsHandler := NewJobsHandler(db, trigger)
	dashboardHandler := NewDashboardHandler(db)
	snapshotsHandler := NewSnapshotsHandler(db)
	integritySvc := integrity.NewIntegrityService(db)
	integrityHandler := NewIntegrityHandler(integritySvc)
	auditSvc := audit.NewAuditService(db)
	auditHandler := NewAuditHandler(auditSvc)
	destinationsHandler := NewDestinationsHandler(db)
	settingsHandler := NewSettingsHandler(db)
	usersHandler := NewUsersHandler(db)
	recoveryHandler := NewRecoveryHandler(db)
	assistantHandler := NewAssistantHandler(db)
	docsHandler := NewDocsHandler()

	// Public docs routes (no auth required — documentation is publicly readable)
	mux.HandleFunc("GET /api/docs/search", docsHandler.Search)
	mux.HandleFunc("GET /api/docs/{slug}", docsHandler.Get)
	mux.HandleFunc("GET /api/docs", docsHandler.List)

	// Public routes
	mux.Handle("POST /api/auth/login", RateLimitMiddleware(rateLimiter, http.HandlerFunc(authHandler.Login)))
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

	// Dashboard endpoint (protected).
	protected.HandleFunc("GET /api/dashboard/summary", dashboardHandler.GetSummary)
	mux.Handle("/api/dashboard/", authSvc.RequireAuth(protected))

	// Notification endpoints (protected).
	protected.HandleFunc("GET /api/notifications/config", notificationsHandler.GetConfig)
	protected.HandleFunc("PUT /api/notifications/config", notificationsHandler.UpdateConfig)
	protected.HandleFunc("POST /api/notifications/test", notificationsHandler.TestNotification)
	protected.HandleFunc("GET /api/notifications/log", notificationsHandler.GetLog)
	mux.Handle("/api/notifications/", authSvc.RequireAuth(protected))

	// Integrity endpoints (protected).
	protected.HandleFunc("POST /api/integrity/verify/{snapshot_id}", integrityHandler.VerifySnapshot)
	protected.HandleFunc("POST /api/integrity/verify-all", integrityHandler.VerifyAll)
	protected.HandleFunc("GET /api/integrity/status", integrityHandler.Status)
	mux.Handle("/api/integrity/", authSvc.RequireAuth(protected))

	// Destinations endpoints (protected).
	protected.HandleFunc("GET /api/destinations", destinationsHandler.List)
	protected.HandleFunc("POST /api/destinations", destinationsHandler.Create)
	protected.HandleFunc("PUT /api/destinations/{id}", destinationsHandler.Update)
	protected.HandleFunc("DELETE /api/destinations/{id}", destinationsHandler.Delete)
	protected.HandleFunc("GET /api/destinations/status", destinationsHandler.SyncStatusMatrix)
	protected.HandleFunc("POST /api/destinations/{id}/retry/{snapshot_id}", destinationsHandler.RetrySync)
	mux.Handle("/api/destinations", authSvc.RequireAuth(protected))
	mux.Handle("/api/destinations/", authSvc.RequireAuth(protected))

	// Snapshots endpoints (protected).
	protected.HandleFunc("GET /api/snapshots/calendar", snapshotsHandler.Calendar)
	protected.HandleFunc("GET /api/snapshots", snapshotsHandler.List)
	protected.HandleFunc("GET /api/snapshots/{id}", snapshotsHandler.Get)
	protected.HandleFunc("GET /api/snapshots/{id}/download", snapshotsHandler.Download)
	mux.Handle("/api/snapshots", authSvc.RequireAuth(protected))
	mux.Handle("/api/snapshots/", authSvc.RequireAuth(protected))

	// Recovery playbook endpoints (protected).
	protected.HandleFunc("GET /api/recovery/playbooks", recoveryHandler.ListPlaybooks)
	protected.HandleFunc("GET /api/recovery/playbooks/{id}", recoveryHandler.GetPlaybook)
	protected.HandleFunc("POST /api/recovery/playbooks/generate/{server_id}", recoveryHandler.GeneratePlaybooks)
	protected.HandleFunc("PUT /api/recovery/playbooks/{id}", recoveryHandler.UpdatePlaybook)
	protected.HandleFunc("DELETE /api/recovery/playbooks/{id}", recoveryHandler.DeletePlaybook)
	mux.Handle("/api/recovery/", authSvc.RequireAuth(protected))

	// Assistant endpoints (protected).
	protected.HandleFunc("POST /api/assistant/chat", assistantHandler.Chat)
	protected.HandleFunc("GET /api/assistant/conversations", assistantHandler.GetConversations)
	protected.HandleFunc("DELETE /api/assistant/conversations", assistantHandler.ClearConversations)
	mux.Handle("/api/assistant/", authSvc.RequireAuth(protected))

	// Settings endpoints (protected).
	protected.HandleFunc("GET /api/settings", settingsHandler.Get)
	protected.HandleFunc("PUT /api/settings", settingsHandler.Update)
	mux.Handle("/api/settings", authSvc.RequireAuth(protected))

	// Users endpoints (protected).
	protected.HandleFunc("GET /api/users", usersHandler.List)
	protected.HandleFunc("POST /api/users", usersHandler.Create)
	protected.HandleFunc("PUT /api/users/{id}", usersHandler.Update)
	protected.HandleFunc("DELETE /api/users/{id}", usersHandler.Delete)
	mux.Handle("/api/users", authSvc.RequireAuth(protected))
	mux.Handle("/api/users/", authSvc.RequireAuth(protected))

	// Audit log endpoints (protected).
	protected.HandleFunc("GET /api/audit", auditHandler.List)
	protected.HandleFunc("GET /api/audit/export", auditHandler.Export)
	mux.Handle("/api/audit", authSvc.RequireAuth(audit.AuditMiddleware(auditSvc, protected)))
	mux.Handle("/api/audit/", authSvc.RequireAuth(audit.AuditMiddleware(auditSvc, protected)))

	// WebSocket endpoints — NOT behind RequireAuth middleware.
	// Auth is handled inside HandleWebSocket via the httpOnly JWT cookie.
	if hub != nil {
		mux.HandleFunc("/ws/logs", hub.HandleWebSocket)
		mux.HandleFunc("/ws/status", hub.HandleWebSocket)
	}

	// Wrap the entire mux with CSRF protection
	return CSRFMiddleware(mux)
}
