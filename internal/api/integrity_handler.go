// internal/api/integrity_handler.go
package api

import (
	"net/http"
	"strconv"

	"github.com/backupmanager/backupmanager/internal/integrity"
)

// IntegrityHandler handles /api/integrity routes.
type IntegrityHandler struct {
	svc *integrity.IntegrityService
}

// NewIntegrityHandler constructs an IntegrityHandler.
func NewIntegrityHandler(svc *integrity.IntegrityService) *IntegrityHandler {
	return &IntegrityHandler{svc: svc}
}

// VerifySnapshot handles POST /api/integrity/verify/{snapshot_id}
// It verifies the integrity of a single snapshot and returns the result.
func (h *IntegrityHandler) VerifySnapshot(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("snapshot_id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		Error(w, http.StatusBadRequest, "invalid snapshot_id")
		return
	}

	result, err := h.svc.VerifySnapshot(id)
	if err != nil {
		Error(w, http.StatusNotFound, err.Error())
		return
	}

	JSON(w, http.StatusOK, result)
}

// VerifyAll handles POST /api/integrity/verify-all
// It starts a background verification of all snapshots and returns immediately.
func (h *IntegrityHandler) VerifyAll(w http.ResponseWriter, r *http.Request) {
	started := h.svc.StartBackgroundVerify(r.Context())
	if !started {
		JSON(w, http.StatusConflict, map[string]string{
			"message": "verification already in progress",
		})
		return
	}

	JSON(w, http.StatusAccepted, map[string]string{
		"message": "verification started in background",
	})
}

// Status handles GET /api/integrity/status
// It returns the latest verification results and whether a run is in progress.
func (h *IntegrityHandler) Status(w http.ResponseWriter, r *http.Request) {
	results := h.svc.LatestResults()
	running := h.svc.IsRunning()

	if results == nil {
		results = []integrity.IntegrityResult{}
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"running": running,
		"results": results,
	})
}
