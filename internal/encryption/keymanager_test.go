// internal/encryption/keymanager_test.go
package encryption

import (
	"bytes"
	"os"
	"testing"

	"github.com/backupmanager/backupmanager/internal/database"
)

// openTestDB opens an in-memory SQLite database with the settings table created.
func openTestDB(t *testing.T) *database.Database {
	t.Helper()
	dbPath := t.TempDir() + "/test.db"
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}

	return db
}

func TestGenerateMasterKey(t *testing.T) {
	key, err := GenerateMasterKey()
	if err != nil {
		t.Fatalf("GenerateMasterKey: %v", err)
	}

	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d bytes", len(key))
	}

	// Two consecutive calls must produce different keys.
	key2, err := GenerateMasterKey()
	if err != nil {
		t.Fatalf("GenerateMasterKey (second call): %v", err)
	}

	if bytes.Equal(key, key2) {
		t.Fatal("two generated keys must not be identical")
	}
}

func TestWrapUnwrapKey_RoundTrip(t *testing.T) {
	masterKey, err := GenerateMasterKey()
	if err != nil {
		t.Fatalf("GenerateMasterKey: %v", err)
	}

	password := "s3cr3t-p@ssw0rd"

	wrapped, err := WrapKey(masterKey, password)
	if err != nil {
		t.Fatalf("WrapKey: %v", err)
	}

	// Minimum expected length: 16 (salt) + 12 (nonce) + 32 (key) + 16 (GCM tag)
	if len(wrapped) < 76 {
		t.Fatalf("wrapped key too short: %d bytes", len(wrapped))
	}

	unwrapped, err := UnwrapKey(wrapped, password)
	if err != nil {
		t.Fatalf("UnwrapKey: %v", err)
	}

	if !bytes.Equal(unwrapped, masterKey) {
		t.Fatal("unwrapped key does not match original master key")
	}
}

func TestUnwrapKey_WrongPassword(t *testing.T) {
	masterKey, err := GenerateMasterKey()
	if err != nil {
		t.Fatalf("GenerateMasterKey: %v", err)
	}

	wrapped, err := WrapKey(masterKey, "correct-password")
	if err != nil {
		t.Fatalf("WrapKey: %v", err)
	}

	_, err = UnwrapKey(wrapped, "wrong-password")
	if err == nil {
		t.Fatal("expected error when unwrapping with wrong password, got nil")
	}
}

func TestExportImportKeyFile_RoundTrip(t *testing.T) {
	masterKey, err := GenerateMasterKey()
	if err != nil {
		t.Fatalf("GenerateMasterKey: %v", err)
	}

	dir := t.TempDir()
	keyPath := dir + "/master.key"

	if err := ExportKeyFile(masterKey, keyPath); err != nil {
		t.Fatalf("ExportKeyFile: %v", err)
	}

	// Verify file permissions are restrictive.
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat key file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("expected file mode 0600, got %o", info.Mode().Perm())
	}

	imported, err := ImportKeyFile(keyPath)
	if err != nil {
		t.Fatalf("ImportKeyFile: %v", err)
	}

	if !bytes.Equal(imported, masterKey) {
		t.Fatal("imported key does not match exported key")
	}
}

func TestKeyManagerStoreAndLoad(t *testing.T) {
	db := openTestDB(t)
	password := "test-password-123"

	masterKey, err := GenerateMasterKey()
	if err != nil {
		t.Fatalf("GenerateMasterKey: %v", err)
	}

	wrapped, err := WrapKey(masterKey, password)
	if err != nil {
		t.Fatalf("WrapKey: %v", err)
	}

	km := NewKeyManager(db)

	if err := km.StoreWrappedKey(wrapped); err != nil {
		t.Fatalf("StoreWrappedKey: %v", err)
	}

	km2 := NewKeyManager(db)

	if err := km2.LoadAndUnwrap(password); err != nil {
		t.Fatalf("LoadAndUnwrap: %v", err)
	}

	if !bytes.Equal(km2.GetKey(), masterKey) {
		t.Fatal("loaded key does not match original master key")
	}
}

func TestKeyManagerIsEnabled(t *testing.T) {
	db := openTestDB(t)
	password := "enable-test-password"

	km := NewKeyManager(db)

	// Before loading: must be disabled.
	if km.IsEnabled() {
		t.Fatal("IsEnabled must be false before loading a key")
	}

	masterKey, err := GenerateMasterKey()
	if err != nil {
		t.Fatalf("GenerateMasterKey: %v", err)
	}

	wrapped, err := WrapKey(masterKey, password)
	if err != nil {
		t.Fatalf("WrapKey: %v", err)
	}

	if err := km.StoreWrappedKey(wrapped); err != nil {
		t.Fatalf("StoreWrappedKey: %v", err)
	}

	if err := km.LoadAndUnwrap(password); err != nil {
		t.Fatalf("LoadAndUnwrap: %v", err)
	}

	// After loading: must be enabled.
	if !km.IsEnabled() {
		t.Fatal("IsEnabled must be true after successfully loading a key")
	}
}
