// internal/websocket/hub_test.go
package websocket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/gorilla/websocket"
)

// testDialer connects to a test server and returns a *websocket.Conn.
func testDialer(t *testing.T, srv *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(srv.URL, "http")
	header := http.Header{}
	if token != "" {
		header.Set("Cookie", "token="+token)
	}
	conn, _, err := websocket.DefaultDialer.Dial(u, header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

// newTestHubAndServer creates a Hub (already running) and a test HTTP server.
func newTestHubAndServer(t *testing.T) (*Hub, *httptest.Server, *auth.Service) {
	t.Helper()
	authSvc := auth.NewService("test-secret-key-32-bytes-long!!")
	hub := NewHub(authSvc)
	go hub.Run()

	srv := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
	t.Cleanup(srv.Close)
	return hub, srv, authSvc
}

// validToken generates a JWT for test use.
func validToken(t *testing.T, authSvc *auth.Service) string {
	t.Helper()
	tok, err := authSvc.GenerateToken(1, "admin", true)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return tok
}

// waitForCount polls hub.ClientCount() until it matches n or the deadline passes.
func waitForCount(t *testing.T, hub *Hub, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.ClientCount() == n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for client count %d, got %d", n, hub.ClientCount())
}

// TestHubBroadcast starts a hub, registers one client, broadcasts a message, and
// verifies the client receives it.
func TestHubBroadcast(t *testing.T) {
	hub, srv, authSvc := newTestHubAndServer(t)
	tok := validToken(t, authSvc)

	conn := testDialer(t, srv, tok)
	defer conn.Close()

	waitForCount(t, hub, 1)

	msg := Message{
		Type:      MessageLog,
		ServerID:  42,
		Data:      "hello",
		Timestamp: time.Now(),
	}
	hub.Broadcast(msg)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read message: %v", err)
	}

	var got Message
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != MessageLog {
		t.Errorf("expected type %q, got %q", MessageLog, got.Type)
	}
	if got.ServerID != 42 {
		t.Errorf("expected server_id 42, got %d", got.ServerID)
	}
}

// TestHubClientDisconnect verifies that the hub removes a client after it disconnects.
func TestHubClientDisconnect(t *testing.T) {
	hub, srv, authSvc := newTestHubAndServer(t)
	tok := validToken(t, authSvc)

	conn := testDialer(t, srv, tok)
	waitForCount(t, hub, 1)

	conn.Close()
	waitForCount(t, hub, 0)
}

// TestHubMultipleClients verifies that all connected clients receive a broadcast.
func TestHubMultipleClients(t *testing.T) {
	hub, srv, authSvc := newTestHubAndServer(t)
	tok := validToken(t, authSvc)

	const numClients = 3
	conns := make([]*websocket.Conn, numClients)
	for i := range conns {
		conns[i] = testDialer(t, srv, tok)
		defer conns[i].Close()
	}

	waitForCount(t, hub, numClients)

	hub.Broadcast(Message{
		Type:      MessageStatus,
		Data:      "ping",
		Timestamp: time.Now(),
	})

	for i, conn := range conns {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("client %d read: %v", i, err)
		}
		var got Message
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("client %d unmarshal: %v", i, err)
		}
		if got.Type != MessageStatus {
			t.Errorf("client %d: expected type %q, got %q", i, MessageStatus, got.Type)
		}
	}
}

// TestHubClientCount verifies that ClientCount reflects registrations and unregistrations.
func TestHubClientCount(t *testing.T) {
	hub, srv, authSvc := newTestHubAndServer(t)
	tok := validToken(t, authSvc)

	if hub.ClientCount() != 0 {
		t.Fatalf("expected 0 clients initially, got %d", hub.ClientCount())
	}

	conn1 := testDialer(t, srv, tok)
	defer conn1.Close()
	waitForCount(t, hub, 1)

	conn2 := testDialer(t, srv, tok)
	defer conn2.Close()
	waitForCount(t, hub, 2)

	conn1.Close()
	waitForCount(t, hub, 1)

	conn2.Close()
	waitForCount(t, hub, 0)
}

// TestBroadcastMessageFormat verifies that the JSON message structure is correct.
func TestBroadcastMessageFormat(t *testing.T) {
	hub, srv, authSvc := newTestHubAndServer(t)
	tok := validToken(t, authSvc)

	conn := testDialer(t, srv, tok)
	defer conn.Close()

	waitForCount(t, hub, 1)

	now := time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC)
	hub.Broadcast(Message{
		Type:      MessageHealth,
		ServerID:  7,
		Data:      map[string]string{"status": "healthy"},
		Timestamp: now,
	})

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Unmarshal into a generic map to inspect raw field names.
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	requiredFields := []string{"type", "server_id", "data", "timestamp"}
	for _, field := range requiredFields {
		if _, ok := payload[field]; !ok {
			t.Errorf("missing field %q in JSON payload", field)
		}
	}

	if payload["type"] != string(MessageHealth) {
		t.Errorf("expected type %q, got %v", MessageHealth, payload["type"])
	}
	if int(payload["server_id"].(float64)) != 7 {
		t.Errorf("expected server_id 7, got %v", payload["server_id"])
	}

	// Verify timestamp is a parseable RFC3339 string.
	tsStr, ok := payload["timestamp"].(string)
	if !ok {
		t.Fatalf("timestamp is not a string: %T", payload["timestamp"])
	}
	if _, err := time.Parse(time.RFC3339Nano, tsStr); err != nil {
		t.Errorf("timestamp %q is not valid RFC3339: %v", tsStr, err)
	}
}

// TestHandleWebSocketRejectsNoToken verifies that requests without a JWT cookie
// receive a 401 Unauthorized response and are not upgraded.
func TestHandleWebSocketRejectsNoToken(t *testing.T) {
	_, srv, _ := newTestHubAndServer(t)

	u := "ws" + strings.TrimPrefix(srv.URL, "http")
	_, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if err == nil {
		t.Fatal("expected dial to fail for unauthenticated request")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		var code int
		if resp != nil {
			code = resp.StatusCode
		}
		t.Errorf("expected 401, got %d", code)
	}
}
