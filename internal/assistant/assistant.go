// internal/assistant/assistant.go
package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
)

// Message represents a single conversation turn.
type Message struct {
	ID        int       `json:"id"`
	Role      string    `json:"role"` // "user" or "assistant"
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// AssistantService handles LLM chat interactions.
type AssistantService struct {
	db       *database.Database
	provider string // "openai" or "anthropic"
	apiKey   string
	model    string
}

// NewAssistantService creates a new AssistantService, loading settings from DB.
func NewAssistantService(db *database.Database) *AssistantService {
	svc := &AssistantService{db: db}
	svc.loadSettings()
	return svc
}

// loadSettings reads assistant config from the settings table.
func (s *AssistantService) loadSettings() {
	rows, err := s.db.DB().Query("SELECT key, value FROM settings WHERE key IN ('assistant_provider','assistant_api_key','assistant_model')")
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		switch key {
		case "assistant_provider":
			s.provider = value
		case "assistant_api_key":
			s.apiKey = value
		case "assistant_model":
			s.model = value
		}
	}
}

// Configure sets the LLM provider settings.
func (s *AssistantService) Configure(provider, apiKey, model string) {
	s.provider = provider
	s.apiKey = apiKey
	s.model = model
}

// Chat sends a user message and returns the assistant response.
func (s *AssistantService) Chat(ctx context.Context, userID int, message string) (*Message, error) {
	// Reload settings in case they changed
	s.loadSettings()

	if s.apiKey == "" {
		resp := &Message{
			Role:      "assistant",
			Content:   "AI assistant is not configured. Go to Settings → Assistant to add your API key.",
			CreatedAt: time.Now(),
		}
		return resp, nil
	}

	// Load conversation history (last 10 messages)
	history, err := s.GetHistory(userID, 10)
	if err != nil {
		return nil, fmt.Errorf("load history: %w", err)
	}

	// Build system context
	systemCtx := BuildContext(s.db, message, history)

	// Store user message
	userMsg, err := s.storeMessage(userID, "user", message)
	if err != nil {
		return nil, fmt.Errorf("store user message: %w", err)
	}
	_ = userMsg

	// Call LLM
	var assistantContent string
	switch s.provider {
	case "anthropic":
		assistantContent, err = s.callAnthropic(ctx, systemCtx, history, message)
	default: // "openai" or empty defaults to openai
		assistantContent, err = s.callOpenAI(ctx, systemCtx, history, message)
	}
	if err != nil {
		return nil, fmt.Errorf("LLM call: %w", err)
	}

	// Store assistant response
	assistantMsg, err := s.storeMessage(userID, "assistant", assistantContent)
	if err != nil {
		return nil, fmt.Errorf("store assistant message: %w", err)
	}

	return assistantMsg, nil
}

// storeMessage persists a message in the database and returns it with its ID.
func (s *AssistantService) storeMessage(userID int, role, content string) (*Message, error) {
	res, err := s.db.DB().Exec(
		"INSERT INTO llm_conversations (user_id, role, content) VALUES (?, ?, ?)",
		userID, role, content,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Message{
		ID:        int(id),
		Role:      role,
		Content:   content,
		CreatedAt: time.Now(),
	}, nil
}

// GetHistory returns the last `limit` messages for a user.
func (s *AssistantService) GetHistory(userID int, limit int) ([]Message, error) {
	rows, err := s.db.DB().Query(`
		SELECT id, role, content, created_at FROM (
			SELECT id, role, content, created_at FROM llm_conversations
			WHERE user_id = ?
			ORDER BY created_at DESC
			LIMIT ?
		) ORDER BY created_at ASC`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		var createdAt string
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &createdAt); err != nil {
			return nil, err
		}
		m.CreatedAt = parseTime(createdAt)
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// ClearHistory deletes all conversation messages for a user.
func (s *AssistantService) ClearHistory(userID int) error {
	_, err := s.db.DB().Exec("DELETE FROM llm_conversations WHERE user_id = ?", userID)
	return err
}

// --- OpenAI ---

type openAIRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (s *AssistantService) callOpenAI(ctx context.Context, systemCtx string, history []Message, userMessage string) (string, error) {
	model := s.model
	if model == "" {
		model = "gpt-4o-mini"
	}

	messages := []openAIMessage{
		{Role: "system", Content: systemCtx},
	}
	for _, h := range history {
		messages = append(messages, openAIMessage{Role: h.Role, Content: h.Content})
	}
	messages = append(messages, openAIMessage{Role: "user", Content: userMessage})

	body, _ := json.Marshal(openAIRequest{Model: model, Messages: messages})

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var result openAIResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("openai error: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	return result.Choices[0].Message.Content, nil
}

// --- Anthropic ---

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (s *AssistantService) callAnthropic(ctx context.Context, systemCtx string, history []Message, userMessage string) (string, error) {
	model := s.model
	if model == "" {
		model = "claude-3-5-haiku-20241022"
	}

	var messages []anthropicMessage
	for _, h := range history {
		messages = append(messages, anthropicMessage{Role: h.Role, Content: h.Content})
	}
	messages = append(messages, anthropicMessage{Role: "user", Content: userMessage})

	body, _ := json.Marshal(anthropicRequest{
		Model:     model,
		MaxTokens: 1024,
		System:    systemCtx,
		Messages:  messages,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", s.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var result anthropicResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("anthropic error: %s", result.Error.Message)
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("no content in response")
	}
	return result.Content[0].Text, nil
}

// parseTime parses a datetime string from SQLite.
func parseTime(s string) time.Time {
	formats := []string{
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05",
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

