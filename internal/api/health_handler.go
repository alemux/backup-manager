// internal/api/health_handler.go
package api

import (
	"net/http"
	"strconv"

	"github.com/backupmanager/backupmanager/internal/health"
)

// HealthHandler handles health-check API endpoints.
type HealthHandler struct {
	svc *health.HealthService
}

// NewHealthHandler constructs a HealthHandler.
func NewHealthHandler(svc *health.HealthService) *HealthHandler {
	return &HealthHandler{svc: svc}
}

// GetAllHealth handles GET /api/health/servers
// Returns the current (most recent) health status for all servers.
func (h *HealthHandler) GetAllHealth(w http.ResponseWriter, r *http.Request) {
	results, err := h.svc.GetCurrentHealth(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to retrieve health data")
		return
	}
	JSON(w, http.StatusOK, results)
}

// GetServerHistory handles GET /api/health/servers/{id}/history
// Returns historical health check results for a single server.
// Query param: limit (default 50).
func (h *HealthHandler) GetServerHistory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}

	history, err := h.svc.GetServerHistory(id, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to retrieve health history")
		return
	}
	JSON(w, http.StatusOK, history)
}
