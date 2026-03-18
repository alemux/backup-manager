// internal/auth/reset_token_test.go
package auth

import (
	"testing"
)

func TestGenerateResetTokenNonEmpty(t *testing.T) {
	token, err := GenerateResetToken()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}
	// 32 bytes hex = 64 characters
	if len(token) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(token))
	}
}

func TestGenerateResetTokenUnique(t *testing.T) {
	token1, err := GenerateResetToken()
	if err != nil {
		t.Fatal(err)
	}
	token2, err := GenerateResetToken()
	if err != nil {
		t.Fatal(err)
	}
	if token1 == token2 {
		t.Error("expected unique tokens, got duplicates")
	}
}

func TestHashResetTokenDeterministic(t *testing.T) {
	token := "abc123testtoken"
	hash1 := HashResetToken(token)
	hash2 := HashResetToken(token)
	if hash1 != hash2 {
		t.Errorf("expected same hash, got %q and %q", hash1, hash2)
	}
	if hash1 == "" {
		t.Error("expected non-empty hash")
	}
}

func TestHashResetTokenDifferentInputs(t *testing.T) {
	hash1 := HashResetToken("token1")
	hash2 := HashResetToken("token2")
	if hash1 == hash2 {
		t.Error("expected different hashes for different inputs")
	}
}
