// internal/database/crypto.go
package database

// EncryptCredential and DecryptCredential provide AES-256-GCM encryption for
// server credentials stored at rest in the database.
//
// The encryption package cannot be used here because it imports the database
// package (for KeyManager), which would create an import cycle. The AES-256-GCM
// logic is therefore implemented directly using the standard library.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// EncryptCredential encrypts a plaintext credential string using AES-256-GCM.
// Returns a hex-encoded ciphertext. If plaintext is empty, it returns an empty
// string without performing any encryption.
// Output format (before hex encoding): [12-byte nonce][ciphertext + 16-byte GCM tag].
func EncryptCredential(plaintext string, key []byte) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if len(key) != 32 {
		return "", fmt.Errorf("crypto: key must be 32 bytes, got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("crypto: create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("crypto: generate nonce: %w", err)
	}

	cipherBytes := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(cipherBytes), nil
}

// DecryptCredential decrypts a hex-encoded ciphertext back to plaintext.
// If ciphertext is empty, it returns an empty string without performing any
// decryption.
func DecryptCredential(ciphertext string, key []byte) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	if len(key) != 32 {
		return "", fmt.Errorf("crypto: key must be 32 bytes, got %d", len(key))
	}

	cipherBytes, err := hex.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("crypto: invalid hex ciphertext: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("crypto: create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(cipherBytes) < nonceSize {
		return "", fmt.Errorf("crypto: ciphertext too short")
	}

	nonce, data := cipherBytes[:nonceSize], cipherBytes[nonceSize:]
	plainBytes, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return "", fmt.Errorf("crypto: decrypt: %w", err)
	}

	return string(plainBytes), nil
}

// CredentialManager wraps a fixed encryption key for convenient credential
// encryption and decryption throughout the application.
type CredentialManager struct {
	key []byte
}

// NewCredentialManager creates a CredentialManager backed by the given 32-byte
// AES-256 key. It panics if the key is not 32 bytes, because a misconfigured
// key length is a programming error, not a runtime condition.
func NewCredentialManager(key []byte) *CredentialManager {
	if len(key) != 32 {
		panic(fmt.Sprintf("database: credential manager key must be 32 bytes, got %d", len(key)))
	}
	k := make([]byte, 32)
	copy(k, key)
	return &CredentialManager{key: k}
}

// Encrypt encrypts plaintext using the manager's key.
func (cm *CredentialManager) Encrypt(plaintext string) (string, error) {
	return EncryptCredential(plaintext, cm.key)
}

// Decrypt decrypts a hex-encoded ciphertext using the manager's key.
func (cm *CredentialManager) Decrypt(ciphertext string) (string, error) {
	return DecryptCredential(ciphertext, cm.key)
}
