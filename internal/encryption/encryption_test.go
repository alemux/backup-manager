// internal/encryption/encryption_test.go
package encryption

import (
	"bytes"
	"crypto/rand"
	"io"
	"os"
	"testing"
)

func newTestKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	return key
}

func TestEncryptDecryptBytes_RoundTrip(t *testing.T) {
	key := newTestKey(t)
	plaintext := []byte("hello, AES-256-GCM world!")

	ciphertext, err := EncryptBytes(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptBytes: %v", err)
	}

	if bytes.Equal(ciphertext, plaintext) {
		t.Fatal("ciphertext must differ from plaintext")
	}

	decrypted, err := DecryptBytes(ciphertext, key)
	if err != nil {
		t.Fatalf("DecryptBytes: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted != plaintext: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptDecryptBytes_WrongKey(t *testing.T) {
	key := newTestKey(t)
	wrongKey := newTestKey(t)
	plaintext := []byte("secret data")

	ciphertext, err := EncryptBytes(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptBytes: %v", err)
	}

	_, err = DecryptBytes(ciphertext, wrongKey)
	if err == nil {
		t.Fatal("expected error when decrypting with wrong key, got nil")
	}
}

func TestEncryptDecryptBytes_EmptyInput(t *testing.T) {
	key := newTestKey(t)
	plaintext := []byte{}

	ciphertext, err := EncryptBytes(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptBytes on empty input: %v", err)
	}

	decrypted, err := DecryptBytes(ciphertext, key)
	if err != nil {
		t.Fatalf("DecryptBytes on empty input: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("empty round-trip failed: got %v, want %v", decrypted, plaintext)
	}
}

func TestEncryptDecryptFile_RoundTrip(t *testing.T) {
	key := newTestKey(t)
	dir := t.TempDir()

	inputPath := dir + "/input.txt"
	encPath := dir + "/input.txt.enc"
	decPath := dir + "/input.txt.dec"

	original := []byte("file contents for encryption round-trip test\nline 2\nline 3")
	if err := os.WriteFile(inputPath, original, 0600); err != nil {
		t.Fatalf("write input file: %v", err)
	}

	if err := EncryptFile(inputPath, encPath, key); err != nil {
		t.Fatalf("EncryptFile: %v", err)
	}

	// Encrypted file must differ from original.
	encData, _ := os.ReadFile(encPath)
	if bytes.Equal(encData, original) {
		t.Fatal("encrypted file must differ from plaintext")
	}

	if err := DecryptFile(encPath, decPath, key); err != nil {
		t.Fatalf("DecryptFile: %v", err)
	}

	decData, err := os.ReadFile(decPath)
	if err != nil {
		t.Fatalf("read decrypted file: %v", err)
	}

	if !bytes.Equal(decData, original) {
		t.Fatalf("decrypted file != original: got %q, want %q", decData, original)
	}
}

func TestEncryptDecryptFile_WrongKey(t *testing.T) {
	key := newTestKey(t)
	wrongKey := newTestKey(t)
	dir := t.TempDir()

	inputPath := dir + "/input.txt"
	encPath := dir + "/input.txt.enc"
	decPath := dir + "/input.txt.dec"

	if err := os.WriteFile(inputPath, []byte("sensitive data"), 0600); err != nil {
		t.Fatalf("write input file: %v", err)
	}

	if err := EncryptFile(inputPath, encPath, key); err != nil {
		t.Fatalf("EncryptFile: %v", err)
	}

	if err := DecryptFile(encPath, decPath, wrongKey); err == nil {
		t.Fatal("expected error decrypting with wrong key, got nil")
	}
}

func TestEncryptFile_LargeFile(t *testing.T) {
	key := newTestKey(t)
	dir := t.TempDir()

	inputPath := dir + "/large.bin"
	encPath := dir + "/large.bin.enc"
	decPath := dir + "/large.bin.dec"

	// Generate 1MB of random data.
	original := make([]byte, 1*1024*1024)
	if _, err := io.ReadFull(rand.Reader, original); err != nil {
		t.Fatalf("generate random data: %v", err)
	}
	if err := os.WriteFile(inputPath, original, 0600); err != nil {
		t.Fatalf("write large input file: %v", err)
	}

	if err := EncryptFile(inputPath, encPath, key); err != nil {
		t.Fatalf("EncryptFile (large): %v", err)
	}

	if err := DecryptFile(encPath, decPath, key); err != nil {
		t.Fatalf("DecryptFile (large): %v", err)
	}

	decData, err := os.ReadFile(decPath)
	if err != nil {
		t.Fatalf("read decrypted large file: %v", err)
	}

	if !bytes.Equal(decData, original) {
		t.Fatal("large file round-trip failed: decrypted data does not match original")
	}
}
