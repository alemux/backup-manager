// internal/api/settings_handler.go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/backupmanager/backupmanager/internal/database"
)

// SettingsHandler handles /api/settings routes.
type SettingsHandler struct {
	db *database.Database
}

// NewSettingsHandler constructs a SettingsHandler.
func NewSettingsHandler(db *database.Database) *SettingsHandler {
	return &SettingsHandler{db: db}
}

// Get handles GET /api/settings
// Returns all settings as a key-value map.
func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.DB().QueryContext(r.Context(),
		`SELECT key, value FROM settings ORDER BY key ASC`)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query settings")
		return
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan setting")
			return
		}
		// Skip internal reset token entries
		if len(k) > 12 && k[:12] == "reset_token:" {
			continue
		}
		settings[k] = v
	}
	if err := rows.Err(); err != nil {
		Error(w, http.StatusInternalServerError, "row iteration error")
		return
	}

	JSON(w, http.StatusOK, settings)
}

// Update handles PUT /api/settings
// Accepts a key-value map and upserts all entries.
func (h *SettingsHandler) Update(w http.ResponseWriter, r *http.Request) {
	var updates map[string]string
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if len(updates) == 0 {
		Error(w, http.StatusBadRequest, "at least one setting required")
		return
	}

	tx, err := h.db.DB().BeginTx(r.Context(), nil)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback()

	for k, v := range updates {
		// Prevent overwriting internal tokens
		if len(k) > 12 && k[:12] == "reset_token:" {
			continue
		}
		_, err := tx.ExecContext(r.Context(),
			`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`,
			k, v,
		)
		if err != nil {
			Error(w, http.StatusInternalServerError, "failed to upsert setting")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		Error(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	JSON(w, http.StatusOK, map[string]string{"message": "settings updated"})
}
