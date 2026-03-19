// internal/api/notifications_handler_test.go
package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/backupmanager/backupmanager/internal/database"
	"github.com/backupmanager/backupmanager/internal/notification"
)

// --- helpers ---

// notifAuthRequest sends an authenticated request using a router with a notification manager.
func notifAuthRequest(t *testing.T, method, path string, body io.Reader, db *database.Database, authSvc *auth.Service, mgr *notification.Manager) *httptest.ResponseRecorder {
	t.Helper()

	hash, err := auth.HashPassword("testpass")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	var userID int
	row := db.DB().QueryRow("SELECT id FROM users WHERE username = 'notif_testuser'")
	if err := row.Scan(&userID); err != nil {
		res, err := db.DB().Exec(
			"INSERT INTO users (username, email, password_hash, is_admin) VALUES (?, ?, ?, ?)",
			"notif_testuser", "notif_testuser@example.com", hash, 1,
		)
		if err != nil {
			t.Fatalf("insert test user: %v", err)
		}
		id, _ := res.LastInsertId()
		userID = int(id)
	}

	token, err := authSvc.GenerateToken(userID, "notif_testuser", true)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	req := httptest.NewRequest(method, path, body)
	req.AddCookie(&http.Cookie{Name: "token", Value: token})
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Add CSRF token for state-changing methods.
	csrfSafeMethods := map[string]bool{
		http.MethodGet:     true,
		http.MethodHead:    true,
		http.MethodOptions: true,
	}
	if !csrfSafeMethods[method] && path != "/api/auth/login" {
		const testCSRFToken = "test-csrf-token-for-unit-tests"
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: testCSRFToken})
		req.Header.Set("X-CSRF-Token", testCSRFToken)
	}

	router := NewRouterWithNotifications(db, authSvc, mgr)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// newMockTgServer returns an httptest.Server simulating a Telegram API and a call counter.
func newMockTgServer(t *testing.T) (*httptest.Server, *int) {
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

// newManager returns a Manager backed by the mock Telegram server.
func newManager(db *database.Database, tgURL string) *notification.Manager {
	tg := notification.NewTelegramNotifier("test-token")
	tg.SetAPIBaseURL(tgURL) // exposed for testing
	return notification.NewManager(db, tg, nil)
}

// insertNotifConfig inserts a notifications_config row for tests.
func insertNotifConfig(t *testing.T, db *database.Database, eventType string, tgEnabled, emEnabled bool, chatID, recipients string) {
	t.Helper()
	tg := 0
	if tgEnabled {
		tg = 1
	}
	em := 0
	if emEnabled {
		em = 1
	}
	_, err := db.DB().Exec(
		`INSERT OR REPLACE INTO notifications_config
		 (event_type, telegram_enabled, email_enabled, telegram_chat_id, email_recipients)
		 VALUES (?, ?, ?, ?, ?)`,
		eventType, tg, em, chatID, recipients,
	)
	if err != nil {
		t.Fatalf("insertNotifConfig: %v", err)
	}
}

// --- Tests ---

func TestGetConfig(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	srv, _ := newMockTgServer(t)
	mgr := newManager(db, srv.URL)

	// Insert some config rows.
	insertNotifConfig(t, db, "backup_failed", true, false, "-100chat1", "")
	insertNotifConfig(t, db, "backup_success", false, true, "", "admin@example.com")

	w := notifAuthRequest(t, http.MethodGet, "/api/notifications/config", nil, db, authSvc, mgr)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 config entries, got %d", len(result))
	}

	// Results are ordered by event_type ASC: backup_failed, backup_success
	if result[0]["event_type"] != "backup_failed" {
		t.Errorf("expected event_type 'backup_failed', got %v", result[0]["event_type"])
	}
	if result[0]["telegram_enabled"] != true {
		t.Errorf("expected telegram_enabled true, got %v", result[0]["telegram_enabled"])
	}
	if result[0]["email_enabled"] != false {
		t.Errorf("expected email_enabled false, got %v", result[0]["email_enabled"])
	}
}

func TestGetConfig_Empty(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	mgr := notification.NewManager(db, nil, nil)

	w := notifAuthRequest(t, http.MethodGet, "/api/notifications/config", nil, db, authSvc, mgr)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result []interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty array, got %d entries", len(result))
	}
}

func TestUpdateConfig(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	mgr := notification.NewManager(db, nil, nil)

	payload, _ := json.Marshal([]map[string]interface{}{
		{
			"event_type":       "backup_failed",
			"telegram_enabled": true,
			"email_enabled":    false,
			"telegram_chat_id": "-100newchat",
			"email_recipients": "",
		},
		{
			"event_type":       "disk_space_low",
			"telegram_enabled": false,
			"email_enabled":    true,
			"telegram_chat_id": "",
			"email_recipients": "ops@company.com",
		},
	})

	w := notifAuthRequest(t, http.MethodPut, "/api/notifications/config", bytes.NewReader(payload), db, authSvc, mgr)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 config entries, got %d", len(result))
	}
}

func TestUpdateConfig_Upsert(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	mgr := notification.NewManager(db, nil, nil)

	// Insert initial config.
	insertNotifConfig(t, db, "backup_failed", true, false, "-100old", "")

	// Upsert with updated values.
	newTrue := true
	payload, _ := json.Marshal([]map[string]interface{}{
		{
			"event_type":       "backup_failed",
			"telegram_enabled": newTrue,
			"email_enabled":    true,
			"telegram_chat_id": "-100new",
			"email_recipients": "admin@example.com",
		},
	})

	w := notifAuthRequest(t, http.MethodPut, "/api/notifications/config", bytes.NewReader(payload), db, authSvc, mgr)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify via GET.
	w2 := notifAuthRequest(t, http.MethodGet, "/api/notifications/config", nil, db, authSvc, mgr)
	var configs []map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&configs)
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}
	if configs[0]["telegram_chat_id"] != "-100new" {
		t.Errorf("expected updated chat_id '-100new', got %v", configs[0]["telegram_chat_id"])
	}
	if configs[0]["email_enabled"] != true {
		t.Errorf("expected email_enabled true after upsert, got %v", configs[0]["email_enabled"])
	}
}

func TestUpdateConfig_InvalidBody(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	mgr := notification.NewManager(db, nil, nil)

	w := notifAuthRequest(t, http.MethodPut, "/api/notifications/config", bytes.NewReader([]byte("not json")), db, authSvc, mgr)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestUpdateConfig_EmptyArray(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	mgr := notification.NewManager(db, nil, nil)

	payload, _ := json.Marshal([]interface{}{})
	w := notifAuthRequest(t, http.MethodPut, "/api/notifications/config", bytes.NewReader(payload), db, authSvc, mgr)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty array, got %d", w.Code)
	}
}

func TestTestNotification_Telegram(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	srv, count := newMockTgServer(t)
	mgr := newManager(db, srv.URL)

	payload, _ := json.Marshal(map[string]string{
		"channel": "telegram",
		"target":  "-100testchat",
	})

	w := notifAuthRequest(t, http.MethodPost, "/api/notifications/test", bytes.NewReader(payload), db, authSvc, mgr)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if *count != 1 {
		t.Errorf("expected 1 telegram call, got %d", *count)
	}

	var result map[string]string
	json.NewDecoder(w.Body).Decode(&result)
	if result["status"] != "sent" {
		t.Errorf("expected status 'sent', got %v", result["status"])
	}
	if result["channel"] != "telegram" {
		t.Errorf("expected channel 'telegram', got %v", result["channel"])
	}
}

func TestTestNotification_InvalidChannel(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	mgr := notification.NewManager(db, nil, nil)

	payload, _ := json.Marshal(map[string]string{
		"channel": "sms",
		"target":  "+123456",
	})

	w := notifAuthRequest(t, http.MethodPost, "/api/notifications/test", bytes.NewReader(payload), db, authSvc, mgr)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestTestNotification_MissingFields(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	mgr := notification.NewManager(db, nil, nil)

	payload, _ := json.Marshal(map[string]string{})
	w := notifAuthRequest(t, http.MethodPost, "/api/notifications/test", bytes.NewReader(payload), db, authSvc, mgr)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestGetLog_Empty(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	mgr := notification.NewManager(db, nil, nil)

	w := notifAuthRequest(t, http.MethodGet, "/api/notifications/log", nil, db, authSvc, mgr)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	json.NewDecoder(w.Body).Decode(&result)
	if result["total"].(float64) != 0 {
		t.Errorf("expected total 0, got %v", result["total"])
	}
}

func TestGetLog_WithEntries(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	srv, _ := newMockTgServer(t)
	mgr := newManager(db, srv.URL)

	// Insert a config and trigger a notify to populate the log.
	insertNotifConfig(t, db, "backup_failed", true, false, "-100chat", "")
	evt := notification.NotificationEvent{
		Type:       notification.EventBackupFailed,
		ServerName: "web-01",
		Title:      "Backup Failed",
		Message:    "error during backup",
	}
	if err := mgr.Notify(evt); err != nil {
		t.Fatalf("notify: %v", err)
	}

	w := notifAuthRequest(t, http.MethodGet, "/api/notifications/log", nil, db, authSvc, mgr)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	json.NewDecoder(w.Body).Decode(&result)
	if result["total"].(float64) != 1 {
		t.Errorf("expected total 1, got %v", result["total"])
	}

	logs := result["logs"].([]interface{})
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}
	entry := logs[0].(map[string]interface{})
	if entry["event_type"] != "backup_failed" {
		t.Errorf("expected event_type 'backup_failed', got %v", entry["event_type"])
	}
	if entry["status"] != "sent" {
		t.Errorf("expected status 'sent', got %v", entry["status"])
	}
}

func TestGetLog_FilterByEventType(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	srv, _ := newMockTgServer(t)
	mgr := newManager(db, srv.URL)

	insertNotifConfig(t, db, "backup_failed", true, false, "-100chat", "")
	insertNotifConfig(t, db, "disk_space_low", true, false, "-100chat", "")

	mgr.Notify(notification.NotificationEvent{Type: notification.EventBackupFailed, ServerName: "s1", Title: "T1"})
	// Reset anti-flood for different server name
	mgr.Notify(notification.NotificationEvent{Type: notification.EventDiskSpaceLow, ServerName: "s1", Title: "T2"})

	w := notifAuthRequest(t, http.MethodGet, "/api/notifications/log?event_type=backup_failed", nil, db, authSvc, mgr)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]interface{}
	json.NewDecoder(w.Body).Decode(&result)
	if result["total"].(float64) != 1 {
		t.Errorf("expected total 1 when filtered by event_type, got %v", result["total"])
	}
}

func TestGetLog_Pagination(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	mgr := notification.NewManager(db, nil, nil)

	// Insert 5 log entries directly.
	for i := 0; i < 5; i++ {
		db.DB().Exec(
			`INSERT INTO notifications_log (event_type, channel, recipient, message, status, created_at)
			 VALUES (?, ?, ?, ?, ?, datetime('now'))`,
			"backup_failed", "telegram", "-100chat", "msg", "sent",
		)
	}

	w := notifAuthRequest(t, http.MethodGet, "/api/notifications/log?page=1&per_page=3", nil, db, authSvc, mgr)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]interface{}
	json.NewDecoder(w.Body).Decode(&result)
	if result["total"].(float64) != 5 {
		t.Errorf("expected total 5, got %v", result["total"])
	}
	logs := result["logs"].([]interface{})
	if len(logs) != 3 {
		t.Errorf("expected 3 entries per page, got %d", len(logs))
	}
	if result["page"].(float64) != 1 {
		t.Errorf("expected page 1, got %v", result["page"])
	}
}

func TestNotifications_RequiresAuth(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	mgr := notification.NewManager(db, nil, nil)

	router := NewRouterWithNotifications(db, authSvc, mgr)

	req := httptest.NewRequest(http.MethodGet, "/api/notifications/config", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", w.Code)
	}
}
