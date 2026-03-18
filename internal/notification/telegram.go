// internal/notification/telegram.go
package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	defaultTelegramAPIBase = "https://api.telegram.org"
	defaultCooldown        = 30 * time.Minute
)

// TelegramNotifier sends messages via the Telegram Bot API with optional anti-flood protection.
type TelegramNotifier struct {
	botToken   string
	client     *http.Client
	apiBaseURL string // overridable for testing

	// Anti-flood tracking
	mu        sync.Mutex
	lastAlert map[string]time.Time // key: "serverID:eventType" → last alert time
}

// NewTelegramNotifier creates a new TelegramNotifier with the given bot token.
func NewTelegramNotifier(botToken string) *TelegramNotifier {
	return &TelegramNotifier{
		botToken:   botToken,
		client:     &http.Client{Timeout: 10 * time.Second},
		apiBaseURL: defaultTelegramAPIBase,
		lastAlert:  make(map[string]time.Time),
	}
}

// telegramPayload is the JSON body sent to the Telegram sendMessage endpoint.
type telegramPayload struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode"`
}

// Send sends a message to the specified chat ID.
func (t *TelegramNotifier) Send(chatID string, message string) error {
	payload := telegramPayload{
		ChatID:    chatID,
		Text:      message,
		ParseMode: "Markdown",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("telegram: marshal payload: %w", err)
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", t.apiBaseURL, t.botToken)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram: unexpected status code %d", resp.StatusCode)
	}

	return nil
}

// SendWithAntiFlood sends a message only if the same alert hasn't been sent within the cooldown period.
// Returns true if the message was sent, false if it was suppressed.
func (t *TelegramNotifier) SendWithAntiFlood(chatID string, alertKey string, message string, cooldown time.Duration) (bool, error) {
	t.mu.Lock()
	last, exists := t.lastAlert[alertKey]
	now := time.Now()
	if exists && now.Sub(last) < cooldown {
		t.mu.Unlock()
		return false, nil
	}
	t.lastAlert[alertKey] = now
	t.mu.Unlock()

	if err := t.Send(chatID, message); err != nil {
		// Roll back the timestamp so a retry can pass through.
		t.mu.Lock()
		if exists {
			t.lastAlert[alertKey] = last
		} else {
			delete(t.lastAlert, alertKey)
		}
		t.mu.Unlock()
		return false, err
	}

	return true, nil
}

// FormatBackupAlert formats a backup failure/success notification.
func FormatBackupAlert(serverName, jobName, status, errorMsg string) string {
	statusEmoji := "✅"
	if status != "success" {
		statusEmoji = "❌"
	}
	msg := fmt.Sprintf("%s *Backup Alert*\n*Server:* `%s`\n*Job:* `%s`\n*Status:* `%s`",
		statusEmoji, serverName, jobName, status)
	if errorMsg != "" {
		msg += fmt.Sprintf("\n*Error:* `%s`", errorMsg)
	}
	return msg
}

// FormatHealthAlert formats a health check status change notification.
func FormatHealthAlert(serverName, checkType, oldStatus, newStatus, message string) string {
	statusEmoji := "⚠️"
	if newStatus == "ok" {
		statusEmoji = "✅"
	} else if newStatus == "critical" {
		statusEmoji = "🔴"
	}
	msg := fmt.Sprintf("%s *Health Alert*\n*Server:* `%s`\n*Check:* `%s`\n*Status:* `%s` → `%s`",
		statusEmoji, serverName, checkType, oldStatus, newStatus)
	if message != "" {
		msg += fmt.Sprintf("\n*Detail:* `%s`", message)
	}
	return msg
}

// FormatDiskAlert formats a low disk space notification.
func FormatDiskAlert(serverName, usage string) string {
	return fmt.Sprintf("💾 *Disk Alert*\n*Server:* `%s`\n*Disk Usage:* `%s`", serverName, usage)
}
