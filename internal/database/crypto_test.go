// internal/database/crypto_test.go
package database

import (
	"strings"
	"testing"
)

// testKey is a 32-byte key used across all crypto tests.
var testKey = []byte("test-key-32-bytes-long-padding!!")

func TestEncryptDecryptCredential_RoundTrip(t *testing.T) {
	plaintext := "super-secret-password"

	ciphertext, err := EncryptCredential(plaintext, testKey)
	if err != nil {
		t.Fatalf("EncryptCredential failed: %v", err)
	}
	if ciphertext == "" {
		t.Fatal("expected non-empty ciphertext")
	}
	if ciphertext == plaintext {
		t.Error("ciphertext should not equal plaintext")
	}

	decrypted, err := DecryptCredential(ciphertext, testKey)
	if err != nil {
		t.Fatalf("DecryptCredential failed: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("expected %q, got %q", plaintext, decrypted)
	}
}

func TestEncryptCredential_DifferentOutputs(t *testing.T) {
	// Same input must produce different ciphertext each time due to random nonce.
	plaintext := "same-password"

	ct1, err := EncryptCredential(plaintext, testKey)
	if err != nil {
		t.Fatalf("first encrypt: %v", err)
	}
	ct2, err := EncryptCredential(plaintext, testKey)
	if err != nil {
		t.Fatalf("second encrypt: %v", err)
	}

	if ct1 == ct2 {
		t.Error("expected different ciphertexts for the same input (random nonce)")
	}

	// Both must still decrypt to the same plaintext.
	d1, err := DecryptCredential(ct1, testKey)
	if err != nil {
		t.Fatalf("decrypt ct1: %v", err)
	}
	d2, err := DecryptCredential(ct2, testKey)
	if err != nil {
		t.Fatalf("decrypt ct2: %v", err)
	}
	if d1 != plaintext || d2 != plaintext {
		t.Errorf("expected both to decrypt to %q, got %q and %q", plaintext, d1, d2)
	}
}

func TestDecryptCredential_WrongKey(t *testing.T) {
	plaintext := "secret"
	ciphertext, err := EncryptCredential(plaintext, testKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	wrongKey := []byte("wrong-key-32-bytes-long-padding!")
	_, err = DecryptCredential(ciphertext, wrongKey)
	if err == nil {
		t.Error("expected error when decrypting with wrong key, got nil")
	}
}

func TestDecryptCredential_InvalidHex(t *testing.T) {
	_, err := DecryptCredential("not-valid-hex!!", testKey)
	if err == nil {
		t.Error("expected error for invalid hex input, got nil")
	}
	if !strings.Contains(err.Error(), "invalid hex") {
		t.Errorf("expected 'invalid hex' in error message, got: %v", err)
	}
}

func TestDecryptCredential_EmptyString(t *testing.T) {
	// Empty string should pass through without encryption or error.
	ciphertext, err := EncryptCredential("", testKey)
	if err != nil {
		t.Fatalf("EncryptCredential empty: %v", err)
	}
	if ciphertext != "" {
		t.Errorf("expected empty ciphertext for empty plaintext, got %q", ciphertext)
	}

	decrypted, err := DecryptCredential("", testKey)
	if err != nil {
		t.Fatalf("DecryptCredential empty: %v", err)
	}
	if decrypted != "" {
		t.Errorf("expected empty decrypted for empty ciphertext, got %q", decrypted)
	}
}
