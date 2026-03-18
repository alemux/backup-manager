// internal/encryption/encryption.go
package encryption

// This package implements AES-256-GCM encryption for files and byte slices.
//
// Limitation: EncryptFile and DecryptFile load the entire file into memory.
// This is acceptable for our use case (database dumps, config files) which
// are typically well under 1GB. For files approaching 1GB, consider using
// the chunked approach where each 64KB chunk gets its own nonce.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"os"
)

// EncryptBytes encrypts a byte slice using AES-256-GCM.
// The output format is: [12-byte nonce][ciphertext + 16-byte GCM tag].
func EncryptBytes(plaintext, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption: key must be 32 bytes, got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("encryption: create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("encryption: create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("encryption: generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// DecryptBytes decrypts a byte slice encrypted by EncryptBytes.
func DecryptBytes(ciphertext, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption: key must be 32 bytes, got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("encryption: create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("encryption: create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("encryption: ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("encryption: decrypt: %w", err)
	}

	return plaintext, nil
}

// EncryptFile encrypts a file using AES-256-GCM.
// Reads from inputPath and writes encrypted output to outputPath.
// Output format: [12-byte nonce][encrypted data + 16-byte GCM tag].
// The entire file is loaded into memory; avoid files approaching 1GB.
func EncryptFile(inputPath, outputPath string, key []byte) error {
	plaintext, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("encryption: read input file: %w", err)
	}

	ciphertext, err := EncryptBytes(plaintext, key)
	if err != nil {
		return err
	}

	if err := os.WriteFile(outputPath, ciphertext, 0600); err != nil {
		return fmt.Errorf("encryption: write output file: %w", err)
	}

	return nil
}

// DecryptFile decrypts a file encrypted by EncryptFile.
func DecryptFile(inputPath, outputPath string, key []byte) error {
	ciphertext, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("encryption: read input file: %w", err)
	}

	plaintext, err := DecryptBytes(ciphertext, key)
	if err != nil {
		return err
	}

	if err := os.WriteFile(outputPath, plaintext, 0600); err != nil {
		return fmt.Errorf("encryption: write output file: %w", err)
	}

	return nil
}
