// internal/api/assistant_handler.go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/backupmanager/backupmanager/internal/assistant"
	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/backupmanager/backupmanager/internal/database"
)

// AssistantHandler handles all /api/assistant/* routes.
type AssistantHandler struct {
	svc *assistant.AssistantService
}

// NewAssistantHandler constructs an AssistantHandler.
func NewAssistantHandler(db *database.Database) *AssistantHandler {
	return &AssistantHandler{svc: assistant.NewAssistantService(db)}
}

// chatRequest is the JSON body for POST /api/assistant/chat.
type chatRequest struct {
	Message string `json:"message"`
}

// Chat handles POST /api/assistant/chat.
func (h *AssistantHandler) Chat(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r)
	if claims == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Message == "" {
		Error(w, http.StatusBadRequest, "message is required")
		return
	}

	msg, err := h.svc.Chat(r.Context(), claims.UserID, req.Message)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	JSON(w, http.StatusOK, msg)
}

// GetConversations handles GET /api/assistant/conversations.
func (h *AssistantHandler) GetConversations(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r)
	if claims == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	history, err := h.svc.GetHistory(claims.UserID, 50)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	if history == nil {
		history = []assistant.Message{}
	}

	JSON(w, http.StatusOK, history)
}

// ClearConversations handles DELETE /api/assistant/conversations.
func (h *AssistantHandler) ClearConversations(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r)
	if claims == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := h.svc.ClearHistory(claims.UserID); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
