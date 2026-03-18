// internal/encryption/keymanager.go
package encryption

// KeyManager manages the AES-256 master key used for encryption at rest.
//
// Key wrapping uses Argon2id for password-based key derivation with the
// following parameters:
//   - Time:    1 iteration
//   - Memory:  64MB (65536 KiB)
//   - Threads: 4
//   - Salt:    16 random bytes
//
// Wrapped key format: [16-byte salt][12-byte nonce][encrypted key + 16-byte GCM tag]
//
// The wrapped key is stored in the settings table as a hex-encoded string
// under the key "encryption_master_key".

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/backupmanager/backupmanager/internal/database"
	"golang.org/x/crypto/argon2"
)

const (
	argon2Time    = 1
	argon2Memory  = 64 * 1024 // 64MB in KiB
	argon2Threads = 4
	argon2KeyLen  = 32 // AES-256

	saltSize  = 16
	masterKeySize = 32
)

// KeyManager holds a reference to the database and the in-memory master key.
type KeyManager struct {
	db        *database.Database
	masterKey []byte
}

// NewKeyManager creates a KeyManager backed by the given database.
func NewKeyManager(db *database.Database) *KeyManager {
	return &KeyManager{db: db}
}

// GenerateMasterKey creates a new cryptographically random 32-byte key.
func GenerateMasterKey() ([]byte, error) {
	key := make([]byte, masterKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("keymanager: generate master key: %w", err)
	}
	return key, nil
}

// deriveKeyFromPassword uses Argon2id to derive a 32-byte AES key from the
// given password and salt.
func deriveKeyFromPassword(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
}

// WrapKey encrypts masterKey with a key derived from password using Argon2id + AES-256-GCM.
// The returned byte slice has the format: [16-byte salt][12-byte nonce][encrypted key + GCM tag].
func WrapKey(masterKey []byte, password string) ([]byte, error) {
	if len(masterKey) != masterKeySize {
		return nil, fmt.Errorf("keymanager: master key must be %d bytes, got %d", masterKeySize, len(masterKey))
	}

	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("keymanager: generate salt: %w", err)
	}

	wrappingKey := deriveKeyFromPassword(password, salt)

	encryptedKey, err := EncryptBytes(masterKey, wrappingKey)
	if err != nil {
		return nil, fmt.Errorf("keymanager: wrap key: %w", err)
	}

	// Prepend the salt to the encrypted key blob.
	wrapped := make([]byte, saltSize+len(encryptedKey))
	copy(wrapped[:saltSize], salt)
	copy(wrapped[saltSize:], encryptedKey)

	return wrapped, nil
}

// UnwrapKey decrypts a wrapped key using the provided password.
// The wrappedKey must have the format produced by WrapKey.
func UnwrapKey(wrappedKey []byte, password string) ([]byte, error) {
	// Minimum size: 16 (salt) + 12 (nonce) + 32 (key) + 16 (GCM tag) = 76 bytes
	if len(wrappedKey) < saltSize+12+masterKeySize+16 {
		return nil, fmt.Errorf("keymanager: wrapped key too short")
	}

	salt := wrappedKey[:saltSize]
	encryptedKey := wrappedKey[saltSize:]

	wrappingKey := deriveKeyFromPassword(password, salt)

	masterKey, err := DecryptBytes(encryptedKey, wrappingKey)
	if err != nil {
		return nil, fmt.Errorf("keymanager: unwrap key (wrong password?): %w", err)
	}

	return masterKey, nil
}

// StoreWrappedKey saves the wrapped key as a hex-encoded string in the settings table.
func (km *KeyManager) StoreWrappedKey(wrappedKey []byte) error {
	encoded := hex.EncodeToString(wrappedKey)
	_, err := km.db.DB().Exec(
		`INSERT INTO settings (key, value, updated_at) VALUES ('encryption_master_key', ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`,
		encoded,
	)
	if err != nil {
		return fmt.Errorf("keymanager: store wrapped key: %w", err)
	}
	return nil
}

// LoadAndUnwrap retrieves the wrapped key from the settings table and decrypts it
// into the in-memory master key using the provided password.
func (km *KeyManager) LoadAndUnwrap(password string) error {
	var encoded string
	err := km.db.DB().QueryRow(
		`SELECT value FROM settings WHERE key = 'encryption_master_key'`,
	).Scan(&encoded)
	if err != nil {
		return fmt.Errorf("keymanager: load wrapped key: %w", err)
	}

	wrappedKey, err := hex.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("keymanager: decode wrapped key: %w", err)
	}

	masterKey, err := UnwrapKey(wrappedKey, password)
	if err != nil {
		return err
	}

	km.masterKey = masterKey
	return nil
}

// ExportKeyFile writes the raw master key bytes to a file with mode 0600.
func ExportKeyFile(key []byte, path string) error {
	if err := os.WriteFile(path, key, 0600); err != nil {
		return fmt.Errorf("keymanager: export key file: %w", err)
	}
	return nil
}

// ImportKeyFile reads a raw master key from a file and validates its length.
func ImportKeyFile(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("keymanager: import key file: %w", err)
	}
	if len(key) != masterKeySize {
		return nil, fmt.Errorf("keymanager: key file must contain exactly %d bytes, got %d", masterKeySize, len(key))
	}
	return key, nil
}

// GetKey returns the in-memory master key. Returns nil if no key is loaded.
func (km *KeyManager) GetKey() []byte {
	return km.masterKey
}

// IsEnabled returns true when encryption is configured and the master key is
// loaded into memory.
func (km *KeyManager) IsEnabled() bool {
	return len(km.masterKey) == masterKeySize
}
