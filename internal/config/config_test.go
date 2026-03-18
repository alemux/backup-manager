// internal/config/config_test.go
package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg := LoadDefaults()
	if cfg.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Port)
	}
	if cfg.DataDir != "./data" {
		t.Errorf("expected default data dir ./data, got %s", cfg.DataDir)
	}
	if cfg.DBPath != "./data/backupmanager.db" {
		t.Errorf("expected default db path, got %s", cfg.DBPath)
	}
}

func TestLoadFromEnv(t *testing.T) {
	os.Setenv("BM_PORT", "9090")
	os.Setenv("BM_DATA_DIR", "/tmp/bm-test")
	defer os.Unsetenv("BM_PORT")
	defer os.Unsetenv("BM_DATA_DIR")

	cfg := Load()
	if cfg.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Port)
	}
	if cfg.DataDir != "/tmp/bm-test" {
		t.Errorf("expected data dir /tmp/bm-test, got %s", cfg.DataDir)
	}
}
