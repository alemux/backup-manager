// internal/config/config.go
package config

import (
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	Port      int
	DataDir   string
	DBPath    string
	BackupDir string
	LogLevel  string
	JWTSecret string
	Timezone  string
}

func LoadDefaults() Config {
	dataDir := "./data"
	return Config{
		Port:      8080,
		DataDir:   dataDir,
		DBPath:    dataDir + "/backupmanager.db",
		BackupDir: dataDir + "/backups",
		LogLevel:  "info",
		JWTSecret: "",
		Timezone:  "Local",
	}
}

func Load() Config {
	cfg := LoadDefaults()

	if v := os.Getenv("BM_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Port = p
		}
	}
	if v := os.Getenv("BM_DATA_DIR"); v != "" {
		cfg.DataDir = v
		cfg.DBPath = filepath.Join(v, "backupmanager.db")
		cfg.BackupDir = filepath.Join(v, "backups")
	}
	if v := os.Getenv("BM_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("BM_JWT_SECRET"); v != "" {
		cfg.JWTSecret = v
	}
	if v := os.Getenv("BM_TIMEZONE"); v != "" {
		cfg.Timezone = v
	}
	return cfg
}
