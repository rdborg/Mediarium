// Package config loads container-level configuration from environment
// variables. Everything else (indexers, download clients, naming, VPN)
// lives in the settings table, not here.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	EbooksDir           string // /ebooks (the ebooks module)
	AudiobooksDir       string // /audiobooks (the audiobooks module)
	OtherDir            string // /other (the Other media type)

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

	// URLBase is the sub-path Mediarium is served under behind a reverse proxy,
	// for example "/mediarium" (URL_BASE). It starts with a slash and has none
	// at the end; empty means the root, which is how it always worked. A value
	// that cannot be used is reported by Validate.
	URLBase    string
	urlBaseErr error

	// BundledFlareSolverr is true in the "-full" image, which runs FlareSolverr
	// next to Mediarium on 127.0.0.1:8191. The image sets it; users do not.
	BundledFlareSolverr bool

	// PauseAutomation is the safe mode: MEDIARIUM_PAUSE_AUTOMATION=1 starts the
	// app with automatic searching, downloading, refreshing and the
	// connectivity checks switched off, so the pages always load and a person can
	// change settings. Nothing is saved; take the variable away and they run again.
	PauseAutomation bool

	// DisableSSO is the way back in when single sign-on or the directory
	// sign-in has locked people out: MEDIARIUM_DISABLE_SSO=1 switches OpenID
	// Connect and LDAP sign-in off and turns password sign-in on again, for as
	// long as it is set. Nothing is saved; take the variable away and the
	// settings apply again.
	DisableSSO bool

	// DisableStats is MEDIARIUM_DISABLE_STATS=1: no anonymous usage numbers are
	// ever sent, whatever the saved answer is, and the question is not asked.
	DisableStats bool

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
	fileMu.Lock()
	fileProblems = nil
	fileMu.Unlock()
	cfg := Config{
		Port:          envPort("APP_PORT", 8264),
		ConfigDir:     envStr("CONFIG_DIR", "/config"),
		DownloadsDir:  envStr("DOWNLOADS_DIR", "/downloads"),
		MoviesDir:     envStr("MOVIES_DIR", "/movies"),
		TVDir:         envStr("TV_DIR", "/tv"),
		MusicDir:      envStr("MUSIC_DIR", "/music"),
		EbooksDir:     envStr("EBOOKS_DIR", "/ebooks"),
		AudiobooksDir: envStr("AUDIOBOOKS_DIR", "/audiobooks"),
		OtherDir:      envStr("OTHER_DIR", "/other"),

		TrustedProxies: envStr("TRUSTED_PROXIES", "private"),
		AllowedOrigins: envStr("ALLOWED_ORIGINS", ""),

		BundledFlareSolverr: truthy(envStr("BUNDLED_FLARESOLVERR", "")),
		PauseAutomation:     truthy(envStr("MEDIARIUM_PAUSE_AUTOMATION", "")),

		DisableSSO:   truthy(envStr("MEDIARIUM_DISABLE_SSO", "")),
		DisableStats: truthy(envStr("MEDIARIUM_DISABLE_STATS", "")),

		ImageVersion: envStr("MEDIARIUM_IMAGE_VERSION", ""),
		Supervised:   truthy(envStr("MEDIARIUM_SUPERVISED", "")),
	}
	cfg.URLBase, cfg.urlBaseErr = NormalizeURLBase(envStr("URL_BASE", ""))
	cfg.DownloadsIncomplete = filepath.Join(cfg.DownloadsDir, "incomplete")
	cfg.DownloadsComplete = filepath.Join(cfg.DownloadsDir, "complete")
	cfg.DBPath = filepath.Join(cfg.ConfigDir, "app.db")
	cfg.SecretKeyPath = filepath.Join(cfg.ConfigDir, "secret.key")
	return cfg
}

// Validate reports a setting that cannot work, as a sentence a person can act
// on. The app refuses to start with one rather than run in a way that looks
// right but is not.
func (c Config) Validate() error {
	return c.urlBaseErr
}

var (
	fileMu       sync.Mutex
	fileProblems []string
)

// Problems lists what the last Load could not do, such as a NAME_FILE that
// points at a file that cannot be read. Each is a sentence for the log.
func Problems() []string {
	fileMu.Lock()
	defer fileMu.Unlock()
	return append([]string(nil), fileProblems...)
}

func noteProblem(format string, args ...any) {
	fileMu.Lock()
	defer fileMu.Unlock()
	fileProblems = append(fileProblems, fmt.Sprintf(format, args...))
}

// lookup returns the value of an environment variable. Following the Docker
// secrets convention, NAME_FILE=/run/secrets/x holds the path of a file whose
// contents (with the whitespace around them removed) are the value, so a
// secret need not sit in the container's environment. The plain NAME wins
// when both are set. A file that cannot be read is noted (see Problems) and
// counts as not set. The contents are never written to a message.
func lookup(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	path := strings.TrimSpace(os.Getenv(key + "_FILE"))
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		reason := err.Error()
		if pe, ok := err.(*os.PathError); ok {
			reason = pe.Err.Error()
		}
		noteProblem("%s_FILE is set to %q but that file cannot be read (%s), so %s is not set.", key, path, reason, key)
		return ""
	}
	return strings.TrimSpace(string(b))
}

func envStr(key, def string) string {
	if v := lookup(key); v != "" {
		return v
	}
	return def
}

// envPort reads a port number. A value that is not a number from 1 to 65535 is
// noted (see Problems) and the default is used.
func envPort(key string, def int) int {
	v := lookup(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 65535 {
		noteProblem("%s is %q, which is not a port number from 1 to 65535, so port %d is used instead.", key, v, def)
		return def
	}
	return n
}

func envInt(key string, def int) int {
	if v := lookup(key); v != "" {
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
