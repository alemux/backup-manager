// internal/assistant/assistant_test.go
package assistant

import (
	"context"
	"os"
	"testing"

	"github.com/backupmanager/backupmanager/internal/database"
)

func setupTestDB(t *testing.T) *database.Database {
	t.Helper()
	f, err := os.CreateTemp("", "assistant_test_*.db")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	db, err := database.Open(f.Name())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{
			input:    "why did backup fail",
			expected: []string{"backup", "fail"},
		},
		{
			input:    "what is the status of my servers?",
			expected: []string{"status", "servers"},
		},
		{
			input:    "show me the last backup run logs",
			expected: []string{"backup", "run", "logs"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractKeywords(tt.input)
			// Check that all expected keywords are present
			gotSet := make(map[string]bool, len(got))
			for _, k := range got {
				gotSet[k] = true
			}
			for _, want := range tt.expected {
				if !gotSet[want] {
					t.Errorf("extractKeywords(%q): expected keyword %q, got %v", tt.input, want, got)
				}
			}
		})
	}
}

func TestBuildContext_WithinBudget(t *testing.T) {
	db := setupTestDB(t)
	// Insert a test user required for foreign key
	_, err := db.DB().Exec("INSERT INTO users (id, username, email, password_hash, is_admin) VALUES (1, 'admin', 'admin@example.com', 'hash', 1)")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	ctx := BuildContext(db, "why did backup fail last night?", nil)
	const maxChars = 16000
	if len(ctx) > maxChars {
		t.Errorf("context length %d exceeds budget of %d chars (~4000 tokens)", len(ctx), maxChars)
	}
	if len(ctx) == 0 {
		t.Error("context is empty")
	}
}

func TestBuildServerSummary(t *testing.T) {
	db := setupTestDB(t)

	// Insert test servers
	_, err := db.DB().Exec(`INSERT INTO servers (name, host, type, port, connection_type, username) VALUES ('web-01', '10.0.0.1', 'linux', 22, 'ssh', 'root')`)
	if err != nil {
		t.Fatalf("insert server: %v", err)
	}
	_, err = db.DB().Exec(`INSERT INTO servers (name, host, type, port, connection_type, username) VALUES ('db-01', '10.0.0.2', 'linux', 22, 'ssh', 'root')`)
	if err != nil {
		t.Fatalf("insert server 2: %v", err)
	}

	summary := buildServerSummary(db)
	if summary == "" {
		t.Error("expected non-empty server summary")
	}
	if !containsStr(summary, "web-01") {
		t.Error("expected server summary to include 'web-01'")
	}
	if !containsStr(summary, "db-01") {
		t.Error("expected server summary to include 'db-01'")
	}
}

func TestChat_NoAPIKey(t *testing.T) {
	db := setupTestDB(t)
	// Insert user
	_, err := db.DB().Exec("INSERT INTO users (id, username, email, password_hash, is_admin) VALUES (1, 'admin', 'admin@example.com', 'hash', 1)")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	svc := NewAssistantService(db)
	// No API key configured

	msg, err := svc.Chat(context.Background(), 1, "hello")
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	if msg == nil {
		t.Fatal("expected a message, got nil")
	}
	if !containsStr(msg.Content, "not configured") {
		t.Errorf("expected helpful error message, got: %s", msg.Content)
	}
}

func TestGetHistory(t *testing.T) {
	db := setupTestDB(t)
	// Insert user
	_, err := db.DB().Exec("INSERT INTO users (id, username, email, password_hash, is_admin) VALUES (1, 'admin', 'admin@example.com', 'hash', 1)")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	svc := NewAssistantService(db)

	// Manually store messages
	_, err = svc.storeMessage(1, "user", "hello")
	if err != nil {
		t.Fatalf("storeMessage: %v", err)
	}
	_, err = svc.storeMessage(1, "assistant", "hi there")
	if err != nil {
		t.Fatalf("storeMessage: %v", err)
	}

	history, err := svc.GetHistory(1, 10)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(history) != 2 {
		t.Errorf("expected 2 messages, got %d", len(history))
	}
	if history[0].Role != "user" {
		t.Errorf("expected first message role 'user', got %q", history[0].Role)
	}
	if history[1].Role != "assistant" {
		t.Errorf("expected second message role 'assistant', got %q", history[1].Role)
	}
}

func TestClearHistory(t *testing.T) {
	db := setupTestDB(t)
	// Insert user
	_, err := db.DB().Exec("INSERT INTO users (id, username, email, password_hash, is_admin) VALUES (1, 'admin', 'admin@example.com', 'hash', 1)")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	svc := NewAssistantService(db)

	// Store messages
	for i := 0; i < 3; i++ {
		if _, err := svc.storeMessage(1, "user", "message"); err != nil {
			t.Fatalf("storeMessage: %v", err)
		}
	}

	if err := svc.ClearHistory(1); err != nil {
		t.Fatalf("ClearHistory: %v", err)
	}

	history, err := svc.GetHistory(1, 10)
	if err != nil {
		t.Fatalf("GetHistory after clear: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("expected 0 messages after clear, got %d", len(history))
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
