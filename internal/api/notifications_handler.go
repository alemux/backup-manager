// internal/api/notifications_handler.go
package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
	"github.com/backupmanager/backupmanager/internal/notification"
)

// NotificationsHandler handles all /api/notifications routes.
type NotificationsHandler struct {
	manager *notification.Manager
	db      *database.Database
}

// NewNotificationsHandler constructs a NotificationsHandler.
func NewNotificationsHandler(db *database.Database, manager *notification.Manager) *NotificationsHandler {
	return &NotificationsHandler{db: db, manager: manager}
}

// --- response / request types ---

// NotificationConfigResponse is the JSON shape for a notifications_config row.
type NotificationConfigResponse struct {
	ID               int    `json:"id"`
	EventType        string `json:"event_type"`
	TelegramEnabled  bool   `json:"telegram_enabled"`
	EmailEnabled     bool   `json:"email_enabled"`
	TelegramChatID   string `json:"telegram_chat_id"`
	EmailRecipients  string `json:"email_recipients"`
}

// notificationConfigRequest is the incoming payload for a single config update.
type notificationConfigRequest struct {
	EventType       string `json:"event_type"`
	TelegramEnabled *bool  `json:"telegram_enabled"`
	EmailEnabled    *bool  `json:"email_enabled"`
	TelegramChatID  string `json:"telegram_chat_id"`
	EmailRecipients string `json:"email_recipients"`
}

// testNotificationRequest is the payload for POST /api/notifications/test.
type testNotificationRequest struct {
	Channel string `json:"channel"` // "telegram" or "email"
	Target  string `json:"target"`  // chat ID or email address
}

// NotificationLogResponse is the JSON shape for a notifications_log row.
type NotificationLogResponse struct {
	ID           int        `json:"id"`
	EventType    string     `json:"event_type"`
	Channel      string     `json:"channel"`
	Recipient    string     `json:"recipient"`
	Message      string     `json:"message"`
	Status       string     `json:"status"`
	ErrorMessage *string    `json:"error_message"`
	CreatedAt    time.Time  `json:"created_at"`
}

// --- Handlers ---

// GetConfig handles GET /api/notifications/config
// Returns all notification configuration rows.
func (h *NotificationsHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.DB().QueryContext(r.Context(),
		`SELECT id, event_type, telegram_enabled, email_enabled,
		        COALESCE(telegram_chat_id, ''), COALESCE(email_recipients, '')
		 FROM notifications_config ORDER BY event_type ASC`)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query notification configs")
		return
	}
	defer rows.Close()

	configs := make([]NotificationConfigResponse, 0)
	for rows.Next() {
		var cfg NotificationConfigResponse
		var tgEnabled, emEnabled int
		if err := rows.Scan(&cfg.ID, &cfg.EventType, &tgEnabled, &emEnabled, &cfg.TelegramChatID, &cfg.EmailRecipients); err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan config")
			return
		}
		cfg.TelegramEnabled = tgEnabled != 0
		cfg.EmailEnabled = emEnabled != 0
		configs = append(configs, cfg)
	}
	if err := rows.Err(); err != nil {
		Error(w, http.StatusInternalServerError, "row iteration error")
		return
	}

	JSON(w, http.StatusOK, configs)
}

// UpdateConfig handles PUT /api/notifications/config
// Accepts an array of config objects and upserts each one.
func (h *NotificationsHandler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	var reqs []notificationConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if len(reqs) == 0 {
		Error(w, http.StatusBadRequest, "at least one config entry required")
		return
	}

	tx, err := h.db.DB().BeginTx(r.Context(), nil)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback()

	for _, req := range reqs {
		if strings.TrimSpace(req.EventType) == "" {
			Error(w, http.StatusBadRequest, "event_type is required for each config")
			return
		}

		tgEnabled := 0
		if req.TelegramEnabled != nil && *req.TelegramEnabled {
			tgEnabled = 1
		}
		emEnabled := 0
		if req.EmailEnabled != nil && *req.EmailEnabled {
			emEnabled = 1
		}

		_, err := tx.ExecContext(r.Context(),
			`INSERT INTO notifications_config (event_type, telegram_enabled, email_enabled, telegram_chat_id, email_recipients)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(event_type) DO UPDATE SET
			   telegram_enabled = excluded.telegram_enabled,
			   email_enabled = excluded.email_enabled,
			   telegram_chat_id = excluded.telegram_chat_id,
			   email_recipients = excluded.email_recipients`,
			req.EventType, tgEnabled, emEnabled,
			nullableString(req.TelegramChatID), nullableString(req.EmailRecipients),
		)
		if err != nil {
			Error(w, http.StatusInternalServerError, fmt.Sprintf("failed to upsert config for %s: %v", req.EventType, err))
			return
		}
	}

	if err := tx.Commit(); err != nil {
		Error(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	// Return updated configs.
	rows, err := h.db.DB().QueryContext(r.Context(),
		`SELECT id, event_type, telegram_enabled, email_enabled,
		        COALESCE(telegram_chat_id, ''), COALESCE(email_recipients, '')
		 FROM notifications_config ORDER BY event_type ASC`)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query updated configs")
		return
	}
	defer rows.Close()

	configs := make([]NotificationConfigResponse, 0)
	for rows.Next() {
		var cfg NotificationConfigResponse
		var tgEnabled, emEnabled int
		if err := rows.Scan(&cfg.ID, &cfg.EventType, &tgEnabled, &emEnabled, &cfg.TelegramChatID, &cfg.EmailRecipients); err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan config")
			return
		}
		cfg.TelegramEnabled = tgEnabled != 0
		cfg.EmailEnabled = emEnabled != 0
		configs = append(configs, cfg)
	}

	JSON(w, http.StatusOK, configs)
}

// TestNotification handles POST /api/notifications/test
// Sends a test notification via the specified channel.
func (h *NotificationsHandler) TestNotification(w http.ResponseWriter, r *http.Request) {
	var req testNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Channel == "" {
		Error(w, http.StatusBadRequest, "channel is required (telegram or email)")
		return
	}
	if req.Target == "" {
		Error(w, http.StatusBadRequest, "target is required")
		return
	}

	var sendErr error
	switch req.Channel {
	case "telegram":
		sendErr = h.manager.TestTelegram(req.Target)
	case "email":
		sendErr = h.manager.TestEmail(req.Target)
	default:
		Error(w, http.StatusBadRequest, "channel must be 'telegram' or 'email'")
		return
	}

	if sendErr != nil {
		Error(w, http.StatusBadGateway, fmt.Sprintf("test notification failed: %v", sendErr))
		return
	}

	JSON(w, http.StatusOK, map[string]string{"status": "sent", "channel": req.Channel, "target": req.Target})
}

// GetLog handles GET /api/notifications/log
// Returns paginated notification history, filterable by event_type, channel, date range.
func (h *NotificationsHandler) GetLog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page := 1
	perPage := 20

	if v := q.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := q.Get("per_page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			perPage = n
		}
	}

	where := []string{"1=1"}
	args := []interface{}{}

	if v := q.Get("event_type"); v != "" {
		where = append(where, "event_type = ?")
		args = append(args, v)
	}
	if v := q.Get("channel"); v != "" {
		where = append(where, "channel = ?")
		args = append(args, v)
	}
	if v := q.Get("status"); v != "" {
		where = append(where, "status = ?")
		args = append(args, v)
	}
	if v := q.Get("from"); v != "" {
		where = append(where, "created_at >= ?")
		args = append(args, v)
	}
	if v := q.Get("to"); v != "" {
		where = append(where, "created_at <= ?")
		args = append(args, v)
	}

	whereClause := strings.Join(where, " AND ")

	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	var total int
	if err := h.db.DB().QueryRowContext(r.Context(),
		fmt.Sprintf("SELECT COUNT(*) FROM notifications_log WHERE %s", whereClause),
		countArgs...,
	).Scan(&total); err != nil {
		Error(w, http.StatusInternalServerError, "failed to count log entries")
		return
	}

	offset := (page - 1) * perPage
	args = append(args, perPage, offset)

	rows, err := h.db.DB().QueryContext(r.Context(),
		fmt.Sprintf(`SELECT id, event_type, channel, recipient, message, status, error_message, created_at
		 FROM notifications_log WHERE %s ORDER BY id DESC LIMIT ? OFFSET ?`, whereClause),
		args...,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query notification log")
		return
	}
	defer rows.Close()

	entries := make([]NotificationLogResponse, 0)
	for rows.Next() {
		var entry NotificationLogResponse
		var errMsg sql.NullString
		var createdAt string
		if err := rows.Scan(
			&entry.ID, &entry.EventType, &entry.Channel, &entry.Recipient,
			&entry.Message, &entry.Status, &errMsg, &createdAt,
		); err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan log entry")
			return
		}
		if errMsg.Valid {
			entry.ErrorMessage = &errMsg.String
		}
		entry.CreatedAt = parseTime(createdAt)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		Error(w, http.StatusInternalServerError, "row iteration error")
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"logs":     entries,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

