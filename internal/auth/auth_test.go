// internal/auth/auth_test.go
package auth

import (
	"testing"
	"time"
)

func TestHashAndVerifyPassword(t *testing.T) {
	password := "testPassword123!"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("hash failed: %v", err)
	}
	if !CheckPassword(password, hash) {
		t.Error("password should match")
	}
	if CheckPassword("wrongPassword", hash) {
		t.Error("wrong password should not match")
	}
}

func TestGenerateAndValidateJWT(t *testing.T) {
	secret := "test-secret-key-32-bytes-long!!"
	svc := NewService(secret)

	token, err := svc.GenerateToken(1, "admin", true)
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("validate token failed: %v", err)
	}
	if claims.UserID != 1 {
		t.Errorf("expected user_id 1, got %d", claims.UserID)
	}
	if claims.Username != "admin" {
		t.Errorf("expected username admin, got %s", claims.Username)
	}
	if !claims.IsAdmin {
		t.Error("expected is_admin true")
	}
}

func TestExpiredJWT(t *testing.T) {
	secret := "test-secret-key-32-bytes-long!!"
	svc := NewService(secret)
	svc.tokenDuration = -1 * time.Hour // Force expired

	token, err := svc.GenerateToken(1, "admin", true)
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}

	_, err = svc.ValidateToken(token)
	if err == nil {
		t.Error("expired token should fail validation")
	}
}
