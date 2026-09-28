// Package config loads container-level configuration from environment
// variables (PRD.md §4.9, CLAUDE.md "Config" convention). Everything else
// (indexers, download clients, naming, VPN) lives in the settings table,
// not here.
package config

import (
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	Port int

	ConfigDir           string // /config
	DownloadsDir        string // /downloads
	DownloadsIncomplete string // /downloads/incomplete
	DownloadsComplete   string // /downloads/complete
	MoviesDir           string // /movies
	TVDir               string // /tv

	DBPath        string
	SecretKeyPath string
}

func Load() Config {
	cfg := Config{
		Port:         envInt("APP_PORT", 8080),
		ConfigDir:    envStr("CONFIG_DIR", "/config"),
		DownloadsDir: envStr("DOWNLOADS_DIR", "/downloads"),
		MoviesDir:    envStr("MOVIES_DIR", "/movies"),
		TVDir:        envStr("TV_DIR", "/tv"),
	}
	cfg.DownloadsIncomplete = filepath.Join(cfg.DownloadsDir, "incomplete")
	cfg.DownloadsComplete = filepath.Join(cfg.DownloadsDir, "complete")
	cfg.DBPath = filepath.Join(cfg.ConfigDir, "app.db")
	cfg.SecretKeyPath = filepath.Join(cfg.ConfigDir, "secret.key")
	return cfg
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
