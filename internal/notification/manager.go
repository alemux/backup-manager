// internal/notification/manager.go
package notification

import (
	"fmt"
	"strings"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
)

// EventType identifies the kind of notification event.
type EventType string

const (
	EventBackupSuccess     EventType = "backup_success"
	EventBackupFailed      EventType = "backup_failed"
	EventServerUnreachable EventType = "server_unreachable"
	EventServiceDown       EventType = "service_down"
	EventDiskSpaceLow      EventType = "disk_space_low"
	EventIntegrityFailed   EventType = "integrity_failed"
	EventMissedBackup      EventType = "missed_backup"
	EventRecoveryActivated EventType = "recovery_activated"
	EventUserLogin         EventType = "user_login"
)

// NotificationEvent is the payload dispatched to the Manager.
type NotificationEvent struct {
	Type       EventType
	ServerName string
	Title      string
	Message    string
	Details    map[string]string
}

// notificationConfig holds a row from the notifications_config table.
type notificationConfig struct {
	TelegramEnabled  bool
	EmailEnabled     bool
	TelegramChatID   string
	EmailRecipients  string
}

// Manager is the central dispatcher that routes events to Telegram and/or Email.
type Manager struct {
	db       *database.Database
	telegram *TelegramNotifier
	email    *EmailNotifier
	cooldown time.Duration // anti-flood cooldown, default 30 min
}

// NewManager creates a new Manager. telegram and email may be nil if the
// respective notifier is not configured.
func NewManager(db *database.Database, telegram *TelegramNotifier, email *EmailNotifier) *Manager {
	return &Manager{
		db:       db,
		telegram: telegram,
		email:    email,
		cooldown: defaultCooldown,
	}
}

// Notify dispatches an event to configured channels.
// Returns nil if at least one channel succeeded (or none were enabled).
// Returns an error only if all enabled channels failed.
func (m *Manager) Notify(event NotificationEvent) error {
	cfg, err := m.loadConfig(string(event.Type))
	if err != nil {
		// No config row means the event type is not configured — treat as no-op.
		return nil
	}

	var telegramErr, emailErr error
	telegramAttempted := false
	emailAttempted := false

	// --- Telegram ---
	if cfg.TelegramEnabled && cfg.TelegramChatID != "" && m.telegram != nil {
		telegramAttempted = true
		msg := m.formatMessage(event)
		alertKey := fmt.Sprintf("%s:%s", event.ServerName, string(event.Type))
		sent, err := m.telegram.SendWithAntiFlood(cfg.TelegramChatID, alertKey, msg, m.cooldown)
		if err != nil {
			telegramErr = err
			m.logNotification(event, "telegram", cfg.TelegramChatID, msg, "failed", err.Error())
		} else if sent {
			m.logNotification(event, "telegram", cfg.TelegramChatID, msg, "sent", "")
		}
		// if suppressed by anti-flood, we don't log at all (not a failure)
	}

	// --- Email ---
	if cfg.EmailEnabled && cfg.EmailRecipients != "" && m.email != nil {
		emailAttempted = true
		recipients := splitRecipients(cfg.EmailRecipients)
		subject := fmt.Sprintf("[BackupManager] %s", event.Title)
		htmlBody := m.formatEmailBody(event)
		alertKey := fmt.Sprintf("%s:%s:email", event.ServerName, string(event.Type))
		sent, err := m.email.SendWithAntiFlood(recipients, alertKey, subject, htmlBody, m.cooldown)
		if err != nil {
			emailErr = err
			m.logNotification(event, "email", cfg.EmailRecipients, subject, "failed", err.Error())
		} else if sent {
			m.logNotification(event, "email", cfg.EmailRecipients, subject, "sent", "")
		}
	}

	// If neither channel was attempted, that's fine — return nil.
	if !telegramAttempted && !emailAttempted {
		return nil
	}

	// Return nil if at least one channel succeeded (no error).
	if telegramAttempted && telegramErr == nil {
		return nil
	}
	if emailAttempted && emailErr == nil {
		return nil
	}

	// All attempted channels failed.
	var errs []string
	if telegramErr != nil {
		errs = append(errs, fmt.Sprintf("telegram: %v", telegramErr))
	}
	if emailErr != nil {
		errs = append(errs, fmt.Sprintf("email: %v", emailErr))
	}
	return fmt.Errorf("all notification channels failed: %s", strings.Join(errs, "; "))
}

// TestTelegram sends a test message to verify Telegram configuration.
func (m *Manager) TestTelegram(chatID string) error {
	if m.telegram == nil {
		return fmt.Errorf("telegram notifier not configured")
	}
	return m.telegram.Send(chatID, "✅ *BackupManager* — Telegram test message. Configuration is working correctly.")
}

// TestEmail sends a test email to verify SMTP configuration.
func (m *Manager) TestEmail(to string) error {
	if m.email == nil {
		return fmt.Errorf("email notifier not configured")
	}
	subject := "[BackupManager] Test Email"
	body := emailHTML(
		"Test Email",
		"Configuration Test",
		`<p style="color:#222;">This is a test email from <strong>BackupManager</strong>. Your SMTP configuration is working correctly.</p>`,
	)
	return m.email.Send([]string{to}, subject, body)
}

// --- internal helpers ---

func (m *Manager) loadConfig(eventType string) (notificationConfig, error) {
	var cfg notificationConfig
	var telegramEnabled, emailEnabled int
	var chatID, recipients *string

	err := m.db.DB().QueryRow(
		`SELECT telegram_enabled, email_enabled, telegram_chat_id, email_recipients
		 FROM notifications_config WHERE event_type = ?`, eventType,
	).Scan(&telegramEnabled, &emailEnabled, &chatID, &recipients)
	if err != nil {
		return notificationConfig{}, err
	}

	cfg.TelegramEnabled = telegramEnabled != 0
	cfg.EmailEnabled = emailEnabled != 0
	if chatID != nil {
		cfg.TelegramChatID = *chatID
	}
	if recipients != nil {
		cfg.EmailRecipients = *recipients
	}
	return cfg, nil
}

func (m *Manager) logNotification(event NotificationEvent, channel, recipient, message, status, errMsg string) {
	_, _ = m.db.DB().Exec(
		`INSERT INTO notifications_log (event_type, channel, recipient, message, status, error_message, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, datetime('now'))`,
		string(event.Type), channel, recipient, message, status, nullableString(errMsg),
	)
}

func (m *Manager) formatMessage(event NotificationEvent) string {
	msg := fmt.Sprintf("🔔 *%s*\n", event.Title)
	if event.ServerName != "" {
		msg += fmt.Sprintf("*Server:* `%s`\n", event.ServerName)
	}
	if event.Message != "" {
		msg += fmt.Sprintf("*Message:* %s\n", event.Message)
	}
	for k, v := range event.Details {
		msg += fmt.Sprintf("*%s:* `%s`\n", k, v)
	}
	return strings.TrimRight(msg, "\n")
}

func (m *Manager) formatEmailBody(event NotificationEvent) string {
	var rows string
	if event.ServerName != "" {
		rows += fmt.Sprintf(`<tr><td style="padding:8px 0;color:#555;font-weight:bold;width:120px;">Server</td><td style="padding:8px 0;color:#222;">%s</td></tr>`, event.ServerName)
	}
	if event.Message != "" {
		rows += fmt.Sprintf(`<tr><td style="padding:8px 0;color:#555;font-weight:bold;">Message</td><td style="padding:8px 0;color:#222;">%s</td></tr>`, event.Message)
	}
	for k, v := range event.Details {
		rows += fmt.Sprintf(`<tr><td style="padding:8px 0;color:#555;font-weight:bold;">%s</td><td style="padding:8px 0;color:#222;">%s</td></tr>`, k, v)
	}
	content := fmt.Sprintf(`<table width="100%%" cellpadding="0" cellspacing="0" style="border-collapse:collapse;">%s</table>`, rows)
	return emailHTML(event.Title, event.Title, content)
}

func splitRecipients(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// nullableString returns nil interface for empty strings (for SQL NULL).
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
