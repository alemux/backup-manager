// internal/config/config.go
package config

import (
	"log"
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
		DBPath:    filepath.Join(dataDir, "backupmanager.db"),
		BackupDir: filepath.Join(dataDir, "backups"),
		LogLevel:  "info",
		JWTSecret: "",
		Timezone:  "Local",
	}
}

func Load() Config {
	cfg := LoadDefaults()

	if v := os.Getenv("BM_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			log.Printf("WARNING: invalid BM_PORT value '%s', using default: %v", v, err)
		} else {
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
