// internal/notification/telegram_test.go
package notification

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------- Format helpers ----------

func TestFormatBackupAlert(t *testing.T) {
	msg := FormatBackupAlert("web-01", "daily-db", "success", "")
	if !strings.Contains(msg, "web-01") {
		t.Error("expected server name in message")
	}
	if !strings.Contains(msg, "daily-db") {
		t.Error("expected job name in message")
	}
	if !strings.Contains(msg, "success") {
		t.Error("expected status in message")
	}
}

func TestFormatBackupAlert_WithError(t *testing.T) {
	msg := FormatBackupAlert("web-01", "daily-db", "failed", "connection refused")
	if !strings.Contains(msg, "connection refused") {
		t.Error("expected error message to be included")
	}
	if !strings.Contains(msg, "failed") {
		t.Error("expected failed status in message")
	}
}

func TestFormatHealthAlert(t *testing.T) {
	msg := FormatHealthAlert("app-server", "disk", "ok", "critical", "disk at 95%")
	if !strings.Contains(msg, "app-server") {
		t.Error("expected server name in message")
	}
	if !strings.Contains(msg, "disk") {
		t.Error("expected check type in message")
	}
	if !strings.Contains(msg, "ok") {
		t.Error("expected old status in message")
	}
	if !strings.Contains(msg, "critical") {
		t.Error("expected new status in message")
	}
	if !strings.Contains(msg, "disk at 95%") {
		t.Error("expected detail message included")
	}
}

func TestFormatDiskAlert(t *testing.T) {
	msg := FormatDiskAlert("storage-01", "92%")
	if !strings.Contains(msg, "storage-01") {
		t.Error("expected server name in message")
	}
	if !strings.Contains(msg, "92%") {
		t.Error("expected disk usage in message")
	}
}

// ---------- Anti-flood ----------

// newTestNotifier creates a notifier pointing at the given test server URL.
func newTestNotifier(serverURL string) *TelegramNotifier {
	n := NewTelegramNotifier("test-token")
	n.apiBaseURL = serverURL
	return n
}

// okServer returns an httptest.Server that always responds with {"ok": true}.
func okServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok": true}`))
	}))
}

func TestAntiFlood_FirstMessageSent(t *testing.T) {
	srv := okServer()
	defer srv.Close()

	n := newTestNotifier(srv.URL)
	sent, err := n.SendWithAntiFlood("chat1", "server1:backup", "hello", 30*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sent {
		t.Error("expected first message to be sent")
	}
}

func TestAntiFlood_DuplicateSuppressed(t *testing.T) {
	srv := okServer()
	defer srv.Close()

	n := newTestNotifier(srv.URL)
	key := "server1:backup"

	_, err := n.SendWithAntiFlood("chat1", key, "first", 30*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error on first send: %v", err)
	}

	sent, err := n.SendWithAntiFlood("chat1", key, "duplicate", 30*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error on second send: %v", err)
	}
	if sent {
		t.Error("expected duplicate message within cooldown to be suppressed")
	}
}

func TestAntiFlood_DifferentKeyAllowed(t *testing.T) {
	srv := okServer()
	defer srv.Close()

	n := newTestNotifier(srv.URL)

	_, err := n.SendWithAntiFlood("chat1", "server1:backup", "first", 30*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sent, err := n.SendWithAntiFlood("chat1", "server1:disk", "different key", 30*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sent {
		t.Error("expected different alert key to pass through")
	}
}

func TestAntiFlood_AfterCooldown(t *testing.T) {
	srv := okServer()
	defer srv.Close()

	n := newTestNotifier(srv.URL)
	key := "server1:backup"

	// Send first message.
	_, err := n.SendWithAntiFlood("chat1", key, "first", 30*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Back-date the last alert timestamp so it appears beyond the cooldown.
	n.mu.Lock()
	n.lastAlert[key] = time.Now().Add(-31 * time.Minute)
	n.mu.Unlock()

	sent, err := n.SendWithAntiFlood("chat1", key, "after cooldown", 30*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sent {
		t.Error("expected message to be sent after cooldown expired")
	}
}

// ---------- HTTP layer ----------

func TestSend_InvalidToken(t *testing.T) {
	// Server that returns 401 Unauthorized.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"ok": false, "description": "Unauthorized"}`))
	}))
	defer srv.Close()

	n := newTestNotifier(srv.URL)
	err := n.Send("chat1", "hello")
	if err == nil {
		t.Error("expected error for non-2xx status code")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 in error message, got: %v", err)
	}
}

func TestSend_BuildsCorrectRequest(t *testing.T) {
	var capturedPath string
	var capturedBody []byte
	var capturedContentType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedContentType = r.Header.Get("Content-Type")
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()

	n := newTestNotifier(srv.URL)
	err := n.Send("my-chat-id", "test message")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify URL path contains the token and method.
	if !strings.Contains(capturedPath, "test-token") {
		t.Errorf("expected path to contain bot token, got: %s", capturedPath)
	}
	if !strings.Contains(capturedPath, "sendMessage") {
		t.Errorf("expected path to contain sendMessage, got: %s", capturedPath)
	}

	// Verify Content-Type.
	if capturedContentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got: %s", capturedContentType)
	}

	// Verify JSON body fields.
	var payload telegramPayload
	if err := json.Unmarshal(capturedBody, &payload); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if payload.ChatID != "my-chat-id" {
		t.Errorf("expected chat_id 'my-chat-id', got: %s", payload.ChatID)
	}
	if payload.Text != "test message" {
		t.Errorf("expected text 'test message', got: %s", payload.Text)
	}
	if payload.ParseMode != "Markdown" {
		t.Errorf("expected parse_mode 'Markdown', got: %s", payload.ParseMode)
	}
}
