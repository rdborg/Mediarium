// Package config loads container-level configuration from environment
// variables. Everything else (indexers, download clients, naming, VPN)
// lives in the settings table, not here.
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Port int

	ConfigDir           string // /config
	DownloadsDir        string // /downloads
	DownloadsIncomplete string // /downloads/incomplete
	DownloadsComplete   string // /downloads/complete
	MoviesDir           string // /movies
	TVDir               string // /tv
	MusicDir            string // /music (only used when the music module is switched on)
	EbooksDir           string // /ebooks (kept for the ebooks module, which is not built yet)
	AudiobooksDir       string // /audiobooks (kept for the audiobooks module, which is not built yet)

	DBPath        string
	SecretKeyPath string

	// TrustedProxies lists the reverse proxies whose X-Forwarded-For /
	// X-Forwarded-Proto / X-Forwarded-Host headers are believed (see
	// internal/httpsec). "private" (default) trusts loopback and private
	// network ranges, "none" trusts no proxy, or give addresses and CIDR
	// ranges separated by commas.
	TrustedProxies string
	// AllowedOrigins are extra host names accepted as the origin of
	// state-changing browser requests, for a proxy that rewrites the Host
	// header (comma-separated; usually empty).
	AllowedOrigins string

	// BundledFlareSolverr is true in the "-full" image, which runs FlareSolverr
	// next to Mediarium on 127.0.0.1:8191. The image sets it; users do not.
	BundledFlareSolverr bool

	// PauseAutomation is the safe mode: MEDIARIUM_PAUSE_AUTOMATION=1 starts the
	// app with automatic searching, downloading, refreshing and the
	// connectivity checks switched off, so the pages always load and a person can
	// change settings. Nothing is saved; take the variable away and they run again.
	PauseAutomation bool

	// ImageVersion is the version of the program inside the container image.
	// The image's entrypoint sets it before starting the app, which is how the
	// app knows it was started by that entrypoint and so can restart into an
	// installed update (see internal/selfupdate). It is empty on a native
	// install. The image sets it; users do not.
	ImageVersion string

	// Supervised is true when something restarts Mediarium after it exits, so
	// the Restart button can work. Docker (with a restart policy), systemd and
	// launchd are recognised by themselves; MEDIARIUM_SUPERVISED=1 says so for
	// any other service manager.
	Supervised bool
}

func Load() Config {
	cfg := Config{
		Port:          envInt("APP_PORT", 8264),
		ConfigDir:     envStr("CONFIG_DIR", "/config"),
		DownloadsDir:  envStr("DOWNLOADS_DIR", "/downloads"),
		MoviesDir:     envStr("MOVIES_DIR", "/movies"),
		TVDir:         envStr("TV_DIR", "/tv"),
		MusicDir:      envStr("MUSIC_DIR", "/music"),
		EbooksDir:     envStr("EBOOKS_DIR", "/ebooks"),
		AudiobooksDir: envStr("AUDIOBOOKS_DIR", "/audiobooks"),

		TrustedProxies: envStr("TRUSTED_PROXIES", "private"),
		AllowedOrigins: envStr("ALLOWED_ORIGINS", ""),

		BundledFlareSolverr: truthy(envStr("BUNDLED_FLARESOLVERR", "")),
		PauseAutomation:     truthy(envStr("MEDIARIUM_PAUSE_AUTOMATION", "")),

		ImageVersion: envStr("MEDIARIUM_IMAGE_VERSION", ""),
		Supervised:   truthy(envStr("MEDIARIUM_SUPERVISED", "")),
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

// truthy is true for 1, true, yes or on (any case); anything else is false.
func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
