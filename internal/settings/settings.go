// Package settings stores app-wide configuration that belongs in the UI,
// not in environment variables (CLAUDE.md "Config" convention): library
// paths, naming preset/tokens, TMDB API key, etc.
package settings

import (
	"database/sql"
	"fmt"

	"github.com/ryanborg/mediarium/internal/crypto"
)

// Known setting keys. Using constants keeps callers from typo-ing a
// string key that silently reads back empty.
const (
	KeyMoviesPath        = "library.movies_path"
	KeyTVPath            = "library.tv_path" // falls back to the TV_DIR env default when unset
	KeyDownloadsPath     = "library.downloads_path"
	KeyNamingPreset      = "library.naming_preset"       // plex|jellyfin|kodi|minimal|custom
	KeyMovieNameFormat   = "library.movie_name_format"   // token string, used when preset=custom
	KeyEpisodeNameFormat = "library.episode_name_format" // token string, used when preset=custom
	KeyTMDBAPIKey        = "metadata.tmdb_api_key"       // encrypted
	KeyOnboardingDone    = "onboarding.done"             // "1" once the first-run wizard completes

	// Phase 2 (PRD §7) — torrent engine settings. There's no remote
	// "download client" to configure for torrents (the engine is embedded
	// via anacrolix/torrent, PRD §4.4), so these live here rather than in
	// the download_clients table.
	KeyTorrentEnabled        = "torrent.enabled"           // "0" turns torrents off entirely; anything else (including unset) leaves them on
	KeyTorrentListenPort     = "torrent.listen_port"       // "0" = let the OS pick
	KeyTorrentSeedRatioLimit = "torrent.seed_ratio_limit"  // e.g. "2.0"; "0" = unlimited
	KeyTorrentSeedTimeLimitH = "torrent.seed_time_limit_h" // hours; "0" = unlimited

	// Automation (PRD §7 Phase 2 — RSS sync, scheduled search, hunting).
	KeyAutomationEnabled = "automation.enabled"         // "0" disables; anything else (including unset) enables
	KeyQualityProfile    = "library.quality_profile"    // legacy: a quality.Presets() key; only read once to pick the initial default profile
	KeyDefaultSources    = "library.default_sources"    // usenet | torrent | both (default both)
	KeyDefaultProfileID  = "library.default_profile_id" // id of the stored quality profile used by items with no profile of their own

	// Phase 4 (PRD §7) — subtitles module.
	KeyOpenSubtitlesUsername = "subtitles.opensubtitles_username" // optional account for a higher download quota
	KeyOpenSubtitlesPassword = "subtitles.opensubtitles_password" // encrypted
	KeySubtitleLanguages     = "subtitles.languages"              // comma-separated OpenSubtitles codes; default "en"
	KeySubtitleAutoDownload  = "subtitles.auto_download"          // "0" disables; anything else enables
	KeyOpenSubtitlesAPIKey   = "subtitles.opensubtitles_api_key"  // encrypted

	// Phase 3 (PRD §7) — curated/public list import (Discover). Trakt's
	// public list-items endpoint only needs a client ID (an app
	// registration on trakt.tv), no OAuth user login — see internal/trakt.
	KeyTraktClientID = "metadata.trakt_client_id" // encrypted

	// Illegal filename character handling (PRD §4.8 — "configurable
	// (replace vs. strip)"). The engine (internal/organizer.Sanitize) has
	// always supported both modes; this was the only way to actually
	// choose one, found missing during a full PRD-vs-code audit.
	KeyIllegalCharMode        = "library.illegal_char_mode"        // "strip" (default) | "replace"
	KeyIllegalCharReplacement = "library.illegal_char_replacement" // used when mode=replace, e.g. "-"

	// VPN kill switch (PRD §4.7). Defaults off ("" reads back false via
	// GetBool) rather than on, since flipping it on by default would
	// silently block every torrent grab for anyone who hasn't configured a
	// VPN at all yet — an opt-in kill switch, not an opt-out one.
	KeyVPNRequireForTorrents = "vpn.require_for_torrents"

	// Manual-import naming-collision conflict policy (PRD §4.8 — "skip /
	// overwrite if better quality / always ask"). "skip" (default) matches
	// the historical behavior before this setting existed, so upgrading
	// doesn't change anyone's existing import behavior underneath them.
	KeyImportConflictPolicy = "library.import_conflict_policy" // "skip" | "overwrite" | "overwrite_if_better" | "ask"

	// Connectivity monitor: minutes between checks of every Usenet server and
	// indexer. Unset means 30; 0 turns it off; anything under 5 is raised to 5.
	KeyMonitorIntervalMinutes = "monitor.interval_minutes"

	// When the person accepted the legal notice (RFC 3339); unset until then.
	KeyLegalAcknowledgedAt = "legal.acknowledged_at"
)

type Store struct {
	db  *sql.DB
	box *crypto.Box
}

func New(db *sql.DB, box *crypto.Box) *Store {
	return &Store{db: db, box: box}
}

// Get returns the raw (decrypted, if applicable) value, or "" if unset.
func (s *Store) Get(key string) (string, error) {
	var value string
	var encrypted bool
	err := s.db.QueryRow(`SELECT value, encrypted FROM settings WHERE key = ?`, key).Scan(&value, &encrypted)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get setting %s: %w", key, err)
	}
	if !encrypted {
		return value, nil
	}
	plain, err := s.box.Decrypt(value)
	if err != nil {
		return "", fmt.Errorf("decrypt setting %s: %w", key, err)
	}
	return plain, nil
}

// Set stores value, encrypting it at rest when encrypt is true.
func (s *Store) Set(key, value string, encrypt bool) error {
	stored := value
	if encrypt {
		enc, err := s.box.Encrypt(value)
		if err != nil {
			return fmt.Errorf("encrypt setting %s: %w", key, err)
		}
		stored = enc
	}
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value, encrypted) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, encrypted = excluded.encrypted`,
		key, stored, encrypt,
	)
	if err != nil {
		return fmt.Errorf("set setting %s: %w", key, err)
	}
	return nil
}

// GetBool is a convenience for flag-style settings ("1"/"" ).
func (s *Store) GetBool(key string) (bool, error) {
	v, err := s.Get(key)
	if err != nil {
		return false, err
	}
	return v == "1", nil
}
