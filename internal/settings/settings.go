// Package settings stores app-wide configuration that belongs in the UI,
// not in environment variables: library
// paths, naming preset/tokens, TMDB API key, etc.
package settings

import (
	"database/sql"
	"fmt"

	"github.com/rdborg/mediarium/internal/crypto"
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

	// Phase 2 — torrent engine settings. There's no remote
	// "download client" to configure for torrents (the engine is embedded
	// via anacrolix/torrent), so these live here rather than in
	// the download_clients table.
	KeyTorrentEnabled        = "torrent.enabled"           // "0" turns torrents off entirely; anything else (including unset) leaves them on
	KeyTorrentListenPort     = "torrent.listen_port"       // TCP+UDP port for incoming peers; unset or "0" = 58264
	KeyTorrentSeedRatioLimit = "torrent.seed_ratio_limit"  // e.g. "2.0"; "0" = unlimited
	KeyTorrentSeedTimeLimitH = "torrent.seed_time_limit_h" // hours; "0" = unlimited

	// Automation (RSS sync, scheduled search, hunting).
	KeyAutomationEnabled = "automation.enabled"         // "0" disables; anything else (including unset) enables
	KeyQualityProfile    = "library.quality_profile"    // legacy: an old preset key (any-1080p, ultra-hd, any); only read once to pick the initial default profile
	KeyQualityLanguage   = "quality.language"           // the audio language wanted, as a name like "English" (the default when unset); a release clearly in another language only is not picked automatically
	KeyDefaultSources    = "library.default_sources"    // usenet | torrent | both (default both)
	KeyDefaultProfileID  = "library.default_profile_id" // id of the stored quality profile used by items with no profile of their own
	KeyPresetsVersion    = "library.presets_version"    // revision of the built-in quality presets last applied; unset means never (set at start, not user-editable)

	// Phase 4 — subtitles module.
	KeySubtitlesEnabled      = "subtitles.enabled"                // the master switch: "1" turns downloading subtitles on; unset or "0" leaves it off (upgrades from a version that had subtitles in use are switched on once, by a migration)
	KeyOpenSubtitlesUsername = "subtitles.opensubtitles_username" // optional account for a higher download quota
	KeyOpenSubtitlesPassword = "subtitles.opensubtitles_password" // encrypted
	KeySubtitleLanguages     = "subtitles.languages"              // comma-separated OpenSubtitles codes; default "en"
	KeySubtitleAutoDownload  = "subtitles.auto_download"          // "1" fetches subtitles automatically; unset or "0" only offers them ("ask me")
	KeySubtitleUpgrade       = "subtitles.upgrade"                // "0" stops swapping a subtitle for one made for the exact video file later; on by default, and only while subtitles download automatically
	KeyOpenSubtitlesAPIKey   = "subtitles.opensubtitles_api_key"  // encrypted

	// Phase 3 — curated/public list import (Discover). Trakt's
	// public list-items endpoint only needs a client ID (an app
	// registration on trakt.tv), no OAuth user login — see internal/trakt.
	KeyTraktClientID  = "metadata.trakt_client_id" // encrypted
	KeyHardcoverToken = "books.hardcover_token"    // encrypted; a personal Hardcover API token for better book series and release dates. Unset = Open Library only

	// Illegal filename character handling ("configurable
	// (replace vs. strip)"). The engine (internal/organizer.Sanitize) has
	// always supported both modes; this was the only way to actually
	// choose one, found missing during a full feature-vs-code audit.
	KeyIllegalCharMode        = "library.illegal_char_mode"        // "strip" (default) | "replace"
	KeyIllegalCharReplacement = "library.illegal_char_replacement" // used when mode=replace, e.g. "-"

	// VPN kill switch. Defaults off ("" reads back false via
	// GetBool) rather than on, since flipping it on by default would
	// silently block every torrent grab for anyone who hasn't configured a
	// VPN at all yet — an opt-in kill switch, not an opt-out one.
	KeyVPNRequireForTorrents = "vpn.require_for_torrents"

	// Manual-import naming-collision conflict policy ("skip /
	// overwrite if better quality / always ask"). "skip" (default) matches
	// the historical behavior before this setting existed, so upgrading
	// doesn't change anyone's existing import behavior underneath them.
	KeyImportConflictPolicy = "library.import_conflict_policy" // "skip" | "overwrite" | "overwrite_if_better" | "ask"

	// Connectivity monitor: minutes between checks of every Usenet server and
	// indexer. Unset means 30; 0 turns it off; anything under 5 is raised to 5.
	KeyMonitorIntervalMinutes = "monitor.interval_minutes" // minutes between connection checks of your Usenet providers and indexers; unset = 30, "0" = off, under 5 is raised to 5

	// How often the automatic loops run.
	KeyHuntIntervalHours   = "automation.hunt_interval_hours"   // hours between searches for missing items and better versions: 1 to 168; unset = 6
	KeyReleaseCheckMinutes = "automation.release_check_minutes" // minutes between checks of each indexer's newest releases: 5 to 1440; unset = 15

	KeyPublicURL = "server.public_url" // the address you open Mediarium at from your own devices (like https://mediarium.example.com); links in notification messages use it; unset = no links

	KeyNotifyOnProblems = "notify.on_problems" // "1" sends a notification (to the targets that listen for Health) when a new error is added to the problem log; unset or "0" sends none

	// A download keeps its place until it has been repaired, unpacked and
	// imported; the others wait in line.
	KeyDownloadsConcurrent = "downloads.concurrent" // how many downloads run at the same time, Usenet and torrents together: 1 to 5; unset = 1

	// When the person accepted the legal notice (RFC 3339); unset until then.
	KeyLegalAcknowledgedAt = "legal.acknowledged_at"

	// Address of a FlareSolverr the person runs (e.g. http://flaresolverr:8191),
	// used to pass Cloudflare checks on definition-based indexer sites. Unset
	// means none: such sites fail with an explanation instead.
	KeyFlareSolverrURL = "flaresolverr.url" // e.g. http://flaresolverr:8191; unset = none

	// Clean-up of the downloads working folder and of old history.
	KeySpeedLimitMB         = "downloads.speed_limit_mb"       // download speed limit in MB/s for Usenet and torrents together; unset or "0" = no limit
	KeySpeedLimitHours      = "downloads.speed_limit_hours"    // when the speed limit applies, "8-23" (from 8:00 to 23:00, server time); unset = all day
	KeyDownloadHours        = "downloads.hours"                // when new downloads may start, "1-7" (from 1:00 to 7:00, server time); outside it they wait in line. Unset = any time
	KeyMinFreeGB            = "downloads.min_free_gb"          // no new download starts while the downloads folder has less free space than this many GB; unset = 5, "0" = off
	KeyNotifyQuietHours     = "notify.quiet_hours"             // hours when everyday messages wait, "23-7" (server time); unset = none. Problems are always sent at once
	KeyBackupAuto           = "backup.auto"                    // "0" turns the nightly backup into /config/backups off; anything else (including unset) leaves it on
	KeyBackupKeep           = "backup.keep"                    // how many saved backups are kept in /config/backups; unset = 7
	KeyCleanupAuto          = "cleanup.auto"                   // "0" turns the daily automatic clean-up off; anything else (including unset) leaves it on
	KeyTrashDays            = "library.trash_days"             // days removed titles' files stay in the recycle bin (.mediarium-trash in the library folder); unset = 7, "0" = delete straight away
	KeyHistoryRetentionDays = "cleanup.history_retention_days" // days finished downloads and activity are kept; unset = 90, "0" = forever
	KeyCleanupLastRunAt     = "cleanup.last_run_at"            // when clean-up last ran (RFC 3339; set by the app, not user-editable)

	// Signing in to Plex, Jellyfin and Emby: the id this install presents as
	// its device (X-Plex-Client-Identifier, MediaBrowser DeviceId). Random,
	// made on first use, kept so the servers see the same device every time.
	KeyMediaServersClientID = "mediaservers.client_id" // set by the app, not user-editable

	// One-off fix that gives titles imported by an older version the age of
	// their files as the date they were added.
	KeyImportAddedBackfill = "library.import_added_backfill" // "1" once it has run (set by the app, not user-editable)

	// One-off fix that reads the quality of episodes an older import left
	// as Unknown from their file names.
	KeyEpisodeQualityBackfill = "library.episode_quality_backfill" // "1" once it has run (set by the app, not user-editable)

	// Music module (artists, albums, tracks). Off until switched on.
	KeyMusicEnabled          = "music.enabled"            // older name of modules.music; still honoured while modules.music is unset, and kept in step with it
	KeyMusicDefaultProfileID = "music.default_profile_id" // id of the music profile artists without one of their own use; unset = the first
	KeyMusicPath             = "music.path"               // music library folder; falls back to the MUSIC_DIR env default (/music) when unset

	// Folders for the ebooks and audiobooks modules.
	KeyEbooksPath     = "ebooks.path"     // ebooks library folder; falls back to the EBOOKS_DIR env default (/ebooks) when unset
	KeyAudiobooksPath = "audiobooks.path" // audiobooks library folder; falls back to the AUDIOBOOKS_DIR env default (/audiobooks) when unset

	// Modules switchboard (Settings > Media types): which kinds of media are on.
	KeyModulesMovies     = "modules.movies"     // "0" switches movies off; anything else (including unset) leaves them on
	KeyModulesTV         = "modules.tv"         // "0" switches TV off; anything else (including unset) leaves it on
	KeyModulesMusic      = "modules.music"      // "1" switches music on; unset or "0" leaves it off (unset falls back to music.enabled)
	KeyModulesAudiobooks = "modules.audiobooks" // "1" switches it on; unset or "0" leaves it off
	KeyModulesEbooks     = "modules.ebooks"     // "1" switches it on; unset or "0" leaves it off

	// Updates and restarts (Settings > System). See docs/security.md.
	KeyUpdatesCheck       = "updates.check"                  // "0" turns the daily check for a new version off; anything else (including unset) leaves it on
	KeyUpdatesAutoInstall = "updates.auto_install"           // "1" installs a new, signed release overnight when nothing is downloading; unset or "0" leaves updating to you
	KeyUpdatesAllowPush   = "updates.allow_push"             // "1" lets an administrator replace the program by uploading a file through the API; unset or "0" refuses. Only a signed-in browser session can switch it on, not an API key
	KeyUpdatesLatest      = "updates.latest"                 // the newest release the last check found (JSON; set by the app, not user-editable)
	KeyUpdatesCheckedAt   = "updates.last_checked_at"        // when the last check ran (RFC 3339; set by the app, not user-editable)
	KeyUpdatesNotified    = "updates.notified_version"       // the version the "new version" message was last sent for (set by the app, not user-editable)
	KeyAutoRestartStuck   = "system.auto_restart_when_stuck" // "0" stops Mediarium restarting itself when it has not answered for three minutes; anything else (including unset) leaves it on

	// Watched status and cleanup rules (Settings > Connections > Media servers). Both off by default.
	KeyWatchedSync   = "watched.sync"   // "1" reads what's been watched from Plex, Jellyfin and Emby every six hours; unset or "0" leaves it off
	KeyWatchedStatus = "watched.status" // the last read and cleanup (JSON; set by the app, not user-editable)
	KeyCleanupRules  = "cleanup.rules"  // the cleanup rules (JSON: enabled, moviesWatchedDays, moviesUnwatchedDays, episodesWatchedDays, keepTags); off unless enabled is true

	// Sign-in through a reverse proxy (Settings > Accounts). See docs/security.md.
	KeyAuthProxyHeader = "auth.proxy_header" // the header a trusted reverse proxy puts the signed-in user name in (for example Remote-User); unset or empty leaves it off. Only a signed-in browser session can set it, not an API key

	// A script to run after each import (Settings > System > Scripts). See docs/scripts.md.
	KeyScriptAfterImport = "scripts.after_import" // JSON: script (a file name in /config/scripts; empty = off) and timeoutSec (default 300). Only a signed-in browser session can set it, not an API key
	KeyScriptLastRun     = "scripts.last_run"     // what the last run did (JSON; set by the app, not user-editable)
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
