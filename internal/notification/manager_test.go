// internal/notification/manager_test.go
package notification

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
)

// --- test helpers ---

// newTestDB opens an in-memory SQLite database and applies the schema needed
// for notification tests.
func newTestDB(t *testing.T) *database.Database {
	t.Helper()
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// insertConfig upserts a notifications_config row for the given event type.
func insertConfig(t *testing.T, db *database.Database, eventType string, telegramEnabled, emailEnabled bool, chatID, recipients string) {
	t.Helper()
	tg := 0
	if telegramEnabled {
		tg = 1
	}
	em := 0
	if emailEnabled {
		em = 1
	}
	_, err := db.DB().Exec(
		`INSERT OR REPLACE INTO notifications_config
		 (event_type, telegram_enabled, email_enabled, telegram_chat_id, email_recipients)
		 VALUES (?, ?, ?, ?, ?)`,
		eventType, tg, em, nullableString(chatID), nullableString(recipients),
	)
	if err != nil {
		t.Fatalf("insertConfig: %v", err)
	}
}

// newMockTelegramServer returns a test HTTP server that always responds with
// {"ok": true} and records how many times it was called.
func newMockTelegramServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &count
}

// newFailingTelegramServer returns a server that always returns 500.
func newFailingTelegramServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newTelegramNotifierWithURL creates a TelegramNotifier pointing at the given URL.
func newTelegramNotifierWithURL(url string) *TelegramNotifier {
	n := NewTelegramNotifier("test-token")
	n.apiBaseURL = url
	return n
}

// countLogs returns the number of rows in notifications_log for the given event_type.
func countLogs(t *testing.T, db *database.Database, eventType string) int {
	t.Helper()
	var count int
	if err := db.DB().QueryRow(
		"SELECT COUNT(*) FROM notifications_log WHERE event_type = ?", eventType,
	).Scan(&count); err != nil {
		t.Fatalf("countLogs: %v", err)
	}
	return count
}

// countLogsByStatus returns number of log rows with given status.
func countLogsByStatus(t *testing.T, db *database.Database, status string) int {
	t.Helper()
	var count int
	if err := db.DB().QueryRow(
		"SELECT COUNT(*) FROM notifications_log WHERE status = ?", status,
	).Scan(&count); err != nil {
		t.Fatalf("countLogsByStatus: %v", err)
	}
	return count
}

// testEvent builds a simple NotificationEvent.
func testEvent(eventType EventType) NotificationEvent {
	return NotificationEvent{
		Type:       eventType,
		ServerName: "web-01",
		Title:      "Test Notification",
		Message:    "This is a test",
		Details:    map[string]string{"key": "value"},
	}
}

// --- Tests ---

func TestNotify_TelegramOnly(t *testing.T) {
	db := newTestDB(t)
	srv, count := newMockTelegramServer(t)

	insertConfig(t, db, string(EventBackupFailed), true, false, "-100chat", "")

	tg := newTelegramNotifierWithURL(srv.URL)
	mgr := NewManager(db, tg, nil)

	if err := mgr.Notify(testEvent(EventBackupFailed)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *count != 1 {
		t.Errorf("expected 1 telegram call, got %d", *count)
	}
}

func TestNotify_EmailOnly(t *testing.T) {
	db := newTestDB(t)

	// Use a nil email notifier but config email enabled — this will fail.
	// Instead, track that telegram is NOT called.
	srv, tgCount := newMockTelegramServer(t)
	insertConfig(t, db, string(EventBackupSuccess), false, true, "", "admin@example.com")

	tg := newTelegramNotifierWithURL(srv.URL)
	// No real email server; pass a dummy EmailNotifier — Send will fail because
	// localhost is not reachable. We just verify telegram was NOT called.
	email := NewEmailNotifier(SMTPConfig{Host: "127.0.0.1", Port: 65535})
	mgr := NewManager(db, tg, email)

	// The email will fail but telegram should not be called.
	_ = mgr.Notify(testEvent(EventBackupSuccess))

	if *tgCount != 0 {
		t.Errorf("expected 0 telegram calls when only email is enabled, got %d", *tgCount)
	}
}

func TestNotify_Both(t *testing.T) {
	db := newTestDB(t)
	srv, tgCount := newMockTelegramServer(t)

	insertConfig(t, db, string(EventDiskSpaceLow), true, true, "-100chat", "admin@example.com")

	tg := newTelegramNotifierWithURL(srv.URL)
	// Email notifier will fail, but telegram should still succeed.
	email := NewEmailNotifier(SMTPConfig{Host: "127.0.0.1", Port: 65535})
	mgr := NewManager(db, tg, email)

	// Should return nil because telegram succeeded.
	if err := mgr.Notify(testEvent(EventDiskSpaceLow)); err != nil {
		t.Fatalf("expected success because telegram succeeded, got: %v", err)
	}
	if *tgCount != 1 {
		t.Errorf("expected 1 telegram call, got %d", *tgCount)
	}
}

func TestNotify_NoneConfigured(t *testing.T) {
	db := newTestDB(t)
	srv, tgCount := newMockTelegramServer(t)

	insertConfig(t, db, string(EventServiceDown), false, false, "", "")

	tg := newTelegramNotifierWithURL(srv.URL)
	mgr := NewManager(db, tg, nil)

	if err := mgr.Notify(testEvent(EventServiceDown)); err != nil {
		t.Fatalf("unexpected error when both disabled: %v", err)
	}
	if *tgCount != 0 {
		t.Errorf("expected 0 calls when both disabled, got %d", *tgCount)
	}
}

func TestNotify_NoConfig(t *testing.T) {
	// Event type not in notifications_config at all → treated as no-op.
	db := newTestDB(t)
	mgr := NewManager(db, nil, nil)

	if err := mgr.Notify(testEvent(EventMissedBackup)); err != nil {
		t.Fatalf("unexpected error for unconfigured event type: %v", err)
	}
}

func TestNotify_LogsToDatabase(t *testing.T) {
	db := newTestDB(t)
	srv, _ := newMockTelegramServer(t)

	insertConfig(t, db, string(EventIntegrityFailed), true, false, "-100chat", "")

	tg := newTelegramNotifierWithURL(srv.URL)
	mgr := NewManager(db, tg, nil)

	if err := mgr.Notify(testEvent(EventIntegrityFailed)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	n := countLogs(t, db, string(EventIntegrityFailed))
	if n != 1 {
		t.Errorf("expected 1 log entry, got %d", n)
	}
	sentCount := countLogsByStatus(t, db, "sent")
	if sentCount != 1 {
		t.Errorf("expected status 'sent', got %d rows with status 'sent'", sentCount)
	}
}

func TestNotify_LogsFailedToDatabase(t *testing.T) {
	db := newTestDB(t)
	failSrv := newFailingTelegramServer(t)

	insertConfig(t, db, string(EventBackupFailed), true, false, "-100chat", "")

	tg := newTelegramNotifierWithURL(failSrv.URL)
	mgr := NewManager(db, tg, nil)

	// Should return error because all channels failed.
	_ = mgr.Notify(testEvent(EventBackupFailed))

	failedCount := countLogsByStatus(t, db, "failed")
	if failedCount != 1 {
		t.Errorf("expected 1 failed log entry, got %d", failedCount)
	}
}

func TestNotify_AntiFlood(t *testing.T) {
	db := newTestDB(t)
	srv, count := newMockTelegramServer(t)

	insertConfig(t, db, string(EventServerUnreachable), true, false, "-100chat", "")

	tg := newTelegramNotifierWithURL(srv.URL)
	mgr := NewManager(db, tg, nil)
	// Use a very long cooldown so the second send is guaranteed to be suppressed.
	mgr.cooldown = 24 * time.Hour

	evt := testEvent(EventServerUnreachable)

	if err := mgr.Notify(evt); err != nil {
		t.Fatalf("first notify: unexpected error: %v", err)
	}
	if *count != 1 {
		t.Errorf("expected 1 call after first notify, got %d", *count)
	}

	// Second notify with same server+event — should be suppressed.
	if err := mgr.Notify(evt); err != nil {
		t.Fatalf("second notify: unexpected error: %v", err)
	}
	if *count != 1 {
		t.Errorf("expected call count to remain 1 after anti-flood, got %d", *count)
	}
}

func TestTestTelegram(t *testing.T) {
	srv, count := newMockTelegramServer(t)
	tg := newTelegramNotifierWithURL(srv.URL)
	db := newTestDB(t)
	mgr := NewManager(db, tg, nil)

	if err := mgr.TestTelegram("-100chat"); err != nil {
		t.Fatalf("TestTelegram: unexpected error: %v", err)
	}
	if *count != 1 {
		t.Errorf("expected 1 telegram call, got %d", *count)
	}
}

func TestTestTelegram_NotConfigured(t *testing.T) {
	db := newTestDB(t)
	mgr := NewManager(db, nil, nil)
	if err := mgr.TestTelegram("-100chat"); err == nil {
		t.Error("expected error when telegram notifier is nil")
	}
}

func TestTestEmail_NotConfigured(t *testing.T) {
	db := newTestDB(t)
	mgr := NewManager(db, nil, nil)
	if err := mgr.TestEmail("admin@example.com"); err == nil {
		t.Error("expected error when email notifier is nil")
	}
}
