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
	KeyMoviesPath          = "library.movies_path"
	KeyTVPath              = "library.tv_path"                  // falls back to the TV_DIR env default when unset
	KeyPushVAPID           = "notify.vapid_key"                 // this server's key for push notifications (encrypted; made on first start)
	KeyTrailerSound        = "ui.trailer_sound"                 // "1" starts the trailer behind a title page with its sound on, where the browser allows it; unset or "0" starts it muted
	KeyTrailerBackground   = "ui.trailer_background"            // unset or "1" plays the trailer behind movie and show pages for everyone, unless a browser chose otherwise; "0" switches it off for everyone
	KeyShowAdvanced        = "ui.show_advanced"                 // "1" shows custom formats and format scores on the Quality page; unset follows whether any custom format exists
	KeyTrashFormatsAuto    = "formats.trash_auto_update"        // "1" re-reads the TRaSH Guides once a day and updates the custom formats that came from them; unset or "0" only updates on request
	KeyTrashFormatsLast    = "formats.trash_last_update"        // when the TRaSH Guides were last read for the daily update (RFC 3339)
	KeyTrashFormatsEvery   = "formats.trash_every_days"         // how often the daily update reads the guides: "1" every day (the default) or "7" every week
	KeyTrashFormatsCheck   = "formats.trash_last_check"         // what the last check of the TRaSH Guides found, as JSON (shown under the switch on the Custom formats card)
	KeyUnmonitorDownloaded = "library.unmonitor_downloaded"     // "1" stops monitoring a movie once it is downloaded; unset or "0" keeps it monitored
	KeyUnmonitorFinished   = "library.unmonitor_finished_shows" // "1" stops monitoring a show once it has ended and every monitored episode is in; unset or "0" keeps it monitored
	KeyRootsConfirmed      = "library.roots_confirmed"          // library folders Mediarium has seen with their .mediarium-root marker (a JSON list); one that loses its marker is refused until it is back
	KeyMoviesExtraPaths    = "library.movies_extra_paths"       // more movie folders next to the main one (a JSON list of paths); each movie says which one its files go to
	KeyTVExtraPaths        = "library.tv_extra_paths"           // the same for shows
	KeyMoviesNewFolder     = "library.movies_new_folder"        // where a movie added without a folder choice is kept: unset = the main folder, a path = that one of the extra folders, "most-room" = whichever movie folder has the most free space at the time
	KeyTVNewFolder         = "library.tv_new_folder"            // the same for shows
	KeyMovieCopiesPath     = "library.movie_copies_path"        // where second copies of movies go (a 4K copy next to the 1080p one); unset = second copies of movies are off
	KeyTVCopiesPath        = "library.tv_copies_path"           // the same for shows
	KeyDownloadsPath       = "library.downloads_path"
	KeyDownloadsTempDir    = "downloads.temp_dir"          // a folder for the unfinished part of downloads (a fast cache drive); unset = they stay in the downloads folder
	KeyDownloadsTempPrev   = "downloads.temp_dir_previous" // earlier temporary folders (a JSON list), kept so downloads that started there are still found
	KeyNamingPreset        = "library.naming_preset"       // plex|jellyfin|kodi|minimal|custom
	KeyMovieNameFormat     = "library.movie_name_format"   // token string, used when preset=custom
	KeyEpisodeNameFormat   = "library.episode_name_format" // token string, used when preset=custom
	KeyTVmazeAirTimes      = "metadata.tvmaze_air_times"   // "0" stops Mediarium looking up the time episodes air on TVmaze (the calendar then only knows days); anything else (including unset) leaves it on
	KeyKeepOriginalNames   = "library.keep_original_names" // "1" keeps the file name an imported video came with (folders are still named by the preset; a random, obfuscated name is replaced by the release's name for a movie or single episode, else by the preset); unset or "0" renames as the preset says
	KeyTMDBAPIKey          = "metadata.tmdb_api_key"       // encrypted
	KeyOnboardingDone      = "onboarding.done"             // "1" once the first-run wizard completes

	// Phase 2 — torrent engine settings. There's no remote
	// "download client" to configure for torrents (the engine is embedded
	// via anacrolix/torrent), so these live here rather than in
	// the download_clients table.
	KeyTorrentEnabled        = "torrent.enabled"                 // "0" turns torrents off entirely; anything else (including unset) leaves them on
	KeyTorrentListenPort     = "torrent.listen_port"             // TCP+UDP port for incoming peers; unset or "0" = 58264
	KeyTorrentSeedRatioLimit = "torrent.seed_ratio_limit"        // e.g. "2.0"; empty or unset = unlimited, "0" = do not seed
	KeyTorrentSeedTimeLimitH = "torrent.seed_time_limit_h"       // hours; empty or unset = unlimited, "0" = do not seed
	KeyTorrentSequential     = "torrent.sequential_default"      // "1" downloads every new built-in torrent in order (first to last piece) so a video can be previewed while it downloads; unset or "0" = off. A torrent can override it
	KeyTorrentSkipJunk       = "torrent.skip_junk"               // "0" downloads every file of a torrent; anything else (including unset) skips samples and .nfo, .txt, .exe, .url files
	KeyTorrentPackOnlyWanted = "torrent.pack_only_wanted"        // "1" downloads only the episodes of the wanted show and season from a pack; unset or "0" = the whole pack
	KeyTorrentUploadLimitKB  = "torrent.upload_limit_kb"         // upload speed limit of the built-in client in KB/s; empty or unset = no limit, "0" = no uploading at all. Applies in the same hours as the download limit
	KeyTorrentAllowLocal     = "torrent.allow_local_addresses"   // "1" lets trackers, web seeds and magnet metadata addresses of the built-in client be on the home network or this machine; anything else (including unset) refuses them, because those addresses come from torrents anyone can write. Read when the torrent client starts
	KeyTorrentPortMapping    = "torrent.port_mapping"            // "0" stops the built-in client asking the router (UPnP, NAT-PMP) to open its port; anything else (including unset) leaves it on. Never used with a VPN or proxy
	KeyTorrentCategories     = "torrent.categories"              // JSON list of {name, subfolder}: categories for built-in torrents; the subfolder is made inside the downloads working folder. Unset = none
	KeyTorrentCategoryMap    = "torrent.category_map"            // JSON {movie, tv, music, book: category name}: the category each kind of grab gets; "" = no category
	KeyDebridService         = "debrid.service"                  // "" (off), "realdebrid", "torbox" or "premiumize": torrent releases go to that service instead of the built-in client
	KeyDebridAPIKey          = "debrid.api_key"                  // encrypted; the debrid service's API key
	KeyPauseWhileStreaming   = "downloads.pause_while_streaming" // "1" pauses downloads while someone watches on Plex, Jellyfin or Emby
	KeyResumeInterrupted     = "downloads.resume_interrupted"    // "0" leaves downloads that a restart interrupted paused; unset or "1" lets them carry on by themselves
	KeyPausedForStreaming    = "downloads.paused_for_streaming"  // the ids of the downloads paused for a stream, as JSON
	KeyDebridPreferCached    = "debrid.prefer_cached"            // "0" stops asking TorBox or Premiumize which releases it already has (no tag, no ranking); "1" or unset (with TorBox or Premiumize as the service) asks and ranks a cached release ahead of an equal one that is not. Real-Debrid has no such check
	KeyDebridCachedLower     = "debrid.cached_lower_score"       // "1" lets a cached release win over an uncached one of the same quality even when the profile's preferred terms score it lower; unset or "0" only breaks ties between equal scores

	// An external download client that speaks the qBittorrent Web API
	// (qBittorrent, RDT-Client, Decypharr). While one is set, torrent
	// releases go to it instead of the built-in client or a debrid service.
	KeyDownloadClientKind           = "downloadclient.kind"                // "" (off), "qbittorrent", "rdtclient" or "decypharr"
	KeyDownloadClientURL            = "downloadclient.url"                 // the client's web address, e.g. http://192.168.1.10:8080
	KeyDownloadClientUsername       = "downloadclient.username"            // the client's web login
	KeyDownloadClientPassword       = "downloadclient.password"            // encrypted; never shown or logged
	KeyDownloadClientCategory       = "downloadclient.category"            // the category torrents are added with; unset = "mediarium" (when no kind has a category of its own either)
	KeyDownloadClientCategoryMovies = "downloadclient.category_movies"     // category for movies; unset = the main category
	KeyDownloadClientCategoryTV     = "downloadclient.category_tv"         // category for TV shows; unset = the main category
	KeyDownloadClientCategoryMusic  = "downloadclient.category_music"      // category for music; unset = the main category
	KeyDownloadClientCategoryBooks  = "downloadclient.category_books"      // category for books; unset = the main category
	KeyDownloadClientRemotePath     = "downloadclient.remote_path"         // path mapping: a folder as the download client reports it, e.g. /downloads; unset = no mapping
	KeyDownloadClientLocalPath      = "downloadclient.local_path"          // path mapping: the same folder as Mediarium sees it, e.g. /data/torrents
	KeyDownloadClientRemoveImport   = "downloadclient.remove_after_import" // "1" removes a torrent from the client once it's imported; unset or "0" leaves it seeding
	KeyDownloadClientRemoveOnStop   = "downloadclient.remove_on_stop"      // "0" leaves a stopped or removed download's torrent in the client; unset or "1" removes it too (only an unfinished torrent Mediarium added itself)
	KeyDownloadClientLinkRoot       = "downloadclient.link_root"           // symlinks from the client may be imported only when they lead into this folder (Decypharr's mount); unset = no symlinks

	// An external Usenet downloader (SABnzbd or NZBGet) Usenet releases can go to instead of the built-in one.
	KeyUsenetClientKind           = "usenetclient.kind"                // "" (off), "sabnzbd" or "nzbget"
	KeyUsenetClientUse            = "usenetclient.use"                 // "1" sends Usenet releases to it; unset or "0" keeps the built-in downloader
	KeyUsenetClientURL            = "usenetclient.url"                 // the client's web address, e.g. http://192.168.1.10:8080
	KeyUsenetClientUsername       = "usenetclient.username"            // NZBGet's login (ControlUsername)
	KeyUsenetClientPassword       = "usenetclient.password"            // encrypted; NZBGet's password (ControlPassword)
	KeyUsenetClientAPIKey         = "usenetclient.api_key"             // encrypted; SABnzbd's API key
	KeyUsenetClientCategory       = "usenetclient.category"            // the category NZBs are added with; unset = "mediarium" (when no kind has a category of its own either)
	KeyUsenetClientCategoryMovies = "usenetclient.category_movies"     // category for movies; unset = the main category
	KeyUsenetClientCategoryTV     = "usenetclient.category_tv"         // category for TV shows; unset = the main category
	KeyUsenetClientCategoryMusic  = "usenetclient.category_music"      // category for music; unset = the main category
	KeyUsenetClientCategoryBooks  = "usenetclient.category_books"      // category for books; unset = the main category
	KeyUsenetClientPriority       = "usenetclient.priority"            // "low", "normal" (default), "high" or "force"
	KeyUsenetClientSendURL        = "usenetclient.send_url"            // "1" lets the client fetch the NZB from the indexer itself; unset or "0" uploads the file
	KeyUsenetClientRemotePath     = "usenetclient.remote_path"         // path mapping: a folder as the client reports it, e.g. /downloads/complete
	KeyUsenetClientLocalPath      = "usenetclient.local_path"          // path mapping: the same folder as Mediarium sees it
	KeyUsenetClientRemoveImport   = "usenetclient.remove_after_import" // "1" removes the job and its files from the client once imported; unset or "0" leaves them there
	KeyUsenetClientRemoveOnStop   = "usenetclient.remove_on_stop"      // "0" leaves a stopped or removed download's job in the client; unset or "1" removes it too (only an unfinished job Mediarium added)

	// Automation (RSS sync, scheduled search, hunting).
	KeyAutomationEnabled  = "automation.enabled"            // "0" disables; anything else (including unset) enables
	KeyQualityProfile     = "library.quality_profile"       // legacy: an old preset key (any-1080p, ultra-hd, any); only read once to pick the initial default profile
	KeyQualityLanguage    = "quality.language"              // the audio language wanted, as a name like "English" (the default when unset); a release clearly in another language only is not picked automatically
	KeyInfoLanguage       = "info.language"                 // the language titles, plots, genres and posters are read in, as a TMDB tag like "de-DE" (en-US when unset); with naming.folders_in_info_language on, new movie and show folders and file names use that language's title too
	KeyReleaseCountry     = "info.release_country"          // the country release dates are read for, as a code like "IN"; unset = the country of the info language
	KeySubtitleAddons     = "subtitles.addons"              // subtitle add-ons (JSON list of {id, name, url}; the url can hold the person's own key for a site, so it is stored encrypted)
	KeyDefaultSources     = "library.default_sources"       // usenet | torrent | both (default both)
	KeySeasonPacks        = "library.season_packs"          // "1" looks for a season pack once a season has ended (a show can say otherwise for itself); unset or "0" leaves it off
	KeyDefaultProfileID   = "library.default_profile_id"    // id of the stored quality profile used by items with no profile of their own
	KeyDefaultTVProfileID = "library.default_profile_id_tv" // id of the quality profile shows with none of their own use; unset = the same default as movies
	KeyPresetsVersion     = "library.presets_version"       // revision of the built-in quality presets last applied; unset means never (set at start, not user-editable)

	// Folder and file names.
	KeyFoldersInInfoLanguage = "naming.folders_in_info_language" // "1" names new movie and show folders and files with the title in the info language; unset or "0" keeps the English title

	// "Only take releases in these languages", next to quality.language.
	KeyQualityLanguageOnly = "quality.language_only" // "1" takes only releases that say they are in a wanted audio language (no untagged, English-only or bare multi releases unless English is wanted); unset or "0" keeps those as a lower-ranked fallback

	// Phase 4 — subtitles module.
	KeySubtitlesEnabled      = "subtitles.enabled"                // the master switch: "1" turns downloading subtitles on; unset or "0" leaves it off (upgrades from a version that had subtitles in use are switched on once, by a migration)
	KeyOpenSubtitlesUsername = "subtitles.opensubtitles_username" // optional account for a higher download quota
	KeyOpenSubtitlesPassword = "subtitles.opensubtitles_password" // encrypted
	KeySubtitleLanguages     = "subtitles.languages"              // comma-separated OpenSubtitles codes; default "en"
	KeySubtitleAutoDownload  = "subtitles.auto_download"          // "1" fetches subtitles automatically; unset or "0" only offers them ("ask me")
	KeySubtitleUpgrade       = "subtitles.upgrade"                // "0" stops swapping a subtitle for one made for the exact video file later; on by default, and only while subtitles download automatically
	KeyOpenSubtitlesAPIKey   = "subtitles.opensubtitles_api_key"  // encrypted
	KeySubDLAPIKey           = "subtitles.subdl_api_key"          // encrypted; SubDL, a second place to find subtitles. Unset leaves it out
	KeySubtitleWizdom        = "subtitles.wizdom"                 // "1" also looks for Hebrew subtitles on Wizdom (wizdom.xyz, no key needed); off when unset
	KeyOpenSubtitlesEnabled  = "subtitles.opensubtitles_enabled"  // "0" switches OpenSubtitles off as a subtitle source (never searched, no downloads counted); on by default, and it only runs with a key
	KeySubDLEnabled          = "subtitles.subdl_enabled"          // "0" switches SubDL off as a subtitle source; on by default, and it only runs with a key
	KeySubtitleAddonsEnabled = "subtitles.addons_enabled"         // "0" switches every subtitle add-on off at once; on by default, and it only runs while at least one add-on is added
	KeySubtitleCountEmbedded = "subtitles.count_embedded"         // "0" stops counting subtitle tracks inside the video file (MKV, MP4) as already there; on by default
	KeyNumberingLookup       = "numbering.lookup"                 // "0" stops Mediarium looking up scene and anime numbering (TheXEM, the anime lists, AniList) for anime on its own; on by default. The Refresh mapping button on a show's page still works
	KeySubtitleProfiles      = "subtitles.profiles"               // per language, which kind of subtitle to look for (JSON such as {"en":{"full":"normal","forced":true}}); a language without an entry wants a normal or SDH subtitle

	// Phase 3 — curated/public list import (Discover). Trakt's
	// public list-items endpoint only needs a client ID (an app
	// registration on trakt.tv), no OAuth user login — see internal/trakt.
	KeyTraktClientID  = "metadata.trakt_client_id" // encrypted
	KeySimklClientID  = "metadata.simkl_client_id" // encrypted; the Client ID of a Simkl app (own, or the one a release ships with)
	KeySimklToken     = "metadata.simkl_token"     // encrypted; the access token of the connected Simkl account
	KeySimklRefresh   = "metadata.simkl_refresh"   // encrypted; the refresh token Simkl gave with the access token, when it gave one
	KeySimklExpires   = "metadata.simkl_expires"   // when the access token runs out (Unix seconds), when Simkl said; unset for a token that does not expire
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

	// "1" once the person said their torrent client is already behind a VPN:
	// the dashboard no longer warns about torrenting without one.
	KeyVPNNoticeAcknowledged = "vpn.notice_acknowledged"

	// Indexer traffic through the VPN. Off when unset.
	KeyVPNRouteIndexers = "vpn.route_indexers" // "1" sends searches, RSS, release downloads, IRC announces and debrid through the VPN, and they wait while it isn't up. Home-network addresses still connect directly

	// UDP tracker announces and DHT queries through the built-in VPN. Off
	// when unset: those are skipped, and only HTTP trackers are used.
	KeyVPNDiscoverThroughTunnel = "vpn.discover_through_tunnel" // "1" sends UDP tracker announces and DHT through the VPN and looks their names up through the VPN's DNS. A failed lookup, or a tunnel socket that will not open, sends nothing from the home address. Unset or "0" skips them

	// A SOCKS5 or HTTP proxy for some of Mediarium's traffic. The address
	// (with any user name and password) is stored encrypted; "" means no
	// proxy. proxy_for lists what goes through it: torrents, indexers and/or
	// usenet, comma-separated.
	KeyProxy    = "network.proxy"     // e.g. "socks5://user:pass@10.0.0.2:1080"
	KeyProxyFor = "network.proxy_for" // e.g. "torrents,indexers"

	// Manual-import naming-collision conflict policy ("skip /
	// overwrite if better quality / always ask"). "skip" (default) matches
	// the historical behavior before this setting existed, so upgrading
	// doesn't change anyone's existing import behavior underneath them.
	KeyImportConflictPolicy = "library.import_conflict_policy" // "skip" | "overwrite" | "overwrite_if_better" | "ask"
	KeyImportCheckDuration  = "import.check_duration"          // "0" turns off the check that a video is about as long as its episode or movie before it is imported (Settings > Library > File names and importing); anything else, including unset, leaves it on

	// Connectivity monitor: minutes between checks of every Usenet server and
	// indexer. Unset means 30; 0 turns it off; anything under 5 is raised to 5.
	KeyMonitorIntervalMinutes = "monitor.interval_minutes" // minutes between connection checks of your Usenet providers and indexers; unset = 30, "0" = off, under 5 is raised to 5

	// How often the automatic loops run.
	KeyHuntIntervalHours   = "automation.hunt_interval_hours"   // hours between searches for missing items and better versions: 1 to 168; unset = 6
	KeyReleaseCheckMinutes = "automation.release_check_minutes" // minutes between checks of each indexer's newest releases: 5 to 1440; unset = 15

	KeyPublicURL = "server.public_url" // the address you open Mediarium at from your own devices (like https://mediarium.example.com); links in notification messages use it; unset = no links

	KeyNotifyOnProblems = "notify.on_problems" // "1" sends a notification (to the targets that listen for Health) when a new error is added to the problem log; unset or "0" sends none

	KeyDebugLogging = "log.debug" // "1" writes the extra detail (debug lines) to the log, taking effect at once and after a restart; unset or "0" keeps the log to normal detail

	// A download keeps its place until it has been repaired, unpacked and
	// imported; the others wait in line.
	KeyAppLinks            = "apps.links"             // JSON list of add-ons: other programs next to Mediarium with a link to each (name, address, how it opens)
	KeyQuietUntil          = "automation.quiet_until" // RFC 3339: until then the heavy background jobs do nothing ("Quiet for a while"); unset = not quiet
	KeyNewestFirst         = "downloads.newest_first" // "1": among the downloads the automatic searches added, newer releases start first; unset = in the order they were added
	KeyDownloadsConcurrent = "downloads.concurrent"   // how many downloads run at the same time, Usenet and torrents together: 1 to 500; unset = 1

	// When the person accepted the legal notice (RFC 3339); unset until then.
	KeyLegalAcknowledgedAt = "legal.acknowledged_at"

	// Address of a FlareSolverr the person runs (e.g. http://flaresolverr:8191),
	// used to pass Cloudflare checks on definition-based indexer sites. Unset
	// means none: such sites fail with an explanation instead.
	KeyFlareSolverrURL = "flaresolverr.url" // e.g. http://flaresolverr:8191; unset = none

	// Clean-up of the downloads working folder and of old history.
	KeySpeedLimitMB         = "downloads.speed_limit_mb"       // download speed limit in MB/s for Usenet and torrents together; unset or "0" = no limit
	KeySpeedLimitHours      = "downloads.speed_limit_hours"    // when the speed limit applies, "8-23" (from 8:00 to 23:00, server time); unset = all day
	KeySpeedSchedule        = "downloads.speed_schedule"       // JSON list of speed rules (days, hours, limit); the first one that fits the moment sets the limit
	KeyDownloadHours        = "downloads.hours"                // when new downloads may start, "1-7" (from 1:00 to 7:00, server time); outside it they wait in line. Unset = any time
	KeyStallHours           = "downloads.stall_hours"          // hours a torrent may get no data before it is given up on (blocklisted and searched for again): 1 to 72, "0" = never; unset = 2 for the built-in client and 6 for a download client
	KeyMinFreeGB            = "downloads.min_free_gb"          // no new download starts while the downloads folder has less free space than this many GB; unset = 5, "0" = off
	KeyRetentionDays        = "downloads.retention_days"       // Usenet releases posted longer ago than this many days are not taken (the provider no longer has them): 1 to 10000, "0" or unset = off
	KeyRejectExecutables    = "downloads.reject_executables"   // "0" lets releases that hold programs or scripts (.exe, .scr, .bat...) through; anything else (including unset) rejects them
	KeySkipDuplicates       = "downloads.skip_duplicates"      // "1" does not download a release twice (same NZB, or same name and size, for the same title); unset or "0" leaves it off
	KeyNZBLinkHolds         = "downloads.nzb_link_holds"       // kept by Mediarium itself: JSON of releases whose NZB link answered with a web page or another answer that is not an NZB, and until when automatic searches leave each one alone
	KeyPrecheck             = "downloads.precheck"             // "1" asks the news servers whether a sample of an NZB's articles exist before downloading it, and gives up at once on a release the PAR2 files could not repair; unset or "0" leaves it off
	KeyDigestDay            = "notify.digest_day"              // the day of the weekly summary, "0" (Sunday) to "6"; unset = no summary
	KeyDigestHour           = "notify.digest_hour"             // the hour it is sent (server time), "0" to "23"; unset = 9
	KeyTrashSyncAlways      = "notify.trashsync_always"        // "1" also sends a message when a check of the TRaSH Guides changed nothing (to the targets that listen for that event); unset or "0" sends only when something changed. A check that failed is always sent
	KeyDigestSent           = "notify.digest_sent"             // when the last one was sent, RFC 3339 (kept by Mediarium, not a setting to edit)
	KeyNotifyQuietHours     = "notify.quiet_hours"             // hours when everyday messages wait, "23-7" (server time); unset = none. Problems are always sent at once
	KeyBackupAuto           = "backup.auto"                    // "0" turns the nightly backup into /config/backups off; anything else (including unset) leaves it on
	KeyBackupKeep           = "backup.keep"                    // how many saved backups are kept in /config/backups; unset = 7
	KeyBackupCloud          = "backup.cloud"                   // encrypted; copies of the nightly backup in S3-style storage or a WebDAV folder (JSON: enabled, kind, storage details, passphrase, keep). Off unless enabled is true. Only a signed-in browser session can read or set it, not an API key
	KeyBackupCloudStatus    = "backup.cloud_status"            // what the last cloud copy did (JSON; set by the app, not user-editable)
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
	KeyMusicLyricsOnline     = "music.lyrics_online"      // "0" stops the player asking LRCLIB for lyrics a file doesn't have; anything else (including unset) leaves it on
	KeyMusicShowTypes        = "music.show_types"         // release types the artist pages list and following adds, a comma separated list of album, ep, single; unset = all three
	KeyMusicHideSecondary    = "music.hide_secondary"     // secondary types (live, remix, compilation...) left out, comma separated keys, "none" = hide none; unset = hide all of them
	KeyMusicHideBootlegs     = "music.hide_bootlegs"      // "1" also leaves out release groups whose releases are all bootlegs, promotions or pseudo-releases (one MusicBrainz request per group); unset = list them

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
	KeyStatsChoice        = "stats.choice"                   // the administrator's answer about sharing anonymous usage numbers: "yes", "no", or unset when not asked yet. Nothing is sent unless it is "yes" (see docs/usage-stats.md)
	KeyStatsInstallID     = "stats.install_id"               // a random number made when the answer is yes, sent with the numbers so one install counts once; set by the app, cleared on "no" and by "Reset my ID"
	KeyStatsLastSent      = "stats.last_sent"                // the day (UTC) the numbers were last sent; set by the app, not user-editable
	KeyUpdatesCheck       = "updates.check"                  // "0" turns the daily check for a new version off; anything else (including unset) leaves it on
	KeyUpdatesAutoInstall = "updates.auto_install"           // "1" installs a new, signed release overnight when nothing is downloading; unset or "0" leaves updating to you
	KeyUpdatesAllowPush   = "updates.allow_push"             // "1" lets an administrator replace the program by uploading a file through the API; unset or "0" refuses. Only a signed-in browser session can switch it on, not an API key
	KeyUpdatesLatest      = "updates.latest"                 // the newest release the last check found (JSON; set by the app, not user-editable)
	KeyUpdatesCheckedAt   = "updates.last_checked_at"        // when the last check ran (RFC 3339; set by the app, not user-editable)
	KeyUpdatesNotified    = "updates.notified_version"       // the version the "new version" message was last sent for (set by the app, not user-editable)
	KeyAutoRestartStuck   = "system.auto_restart_when_stuck" // "0" stops Mediarium restarting itself when it has not answered for three minutes; anything else (including unset) leaves it on
	KeyMCPEnabled         = "mcp.enabled"                    // "1" lets AI assistants connect to the MCP address (POST /mcp) with an administrator's API key; unset or "0" keeps it off. Only a signed-in browser session can switch it on, not an API key

	// Watched status and cleanup rules (Settings > Connections > Media servers). Both off by default.
	KeyWatchedSync   = "watched.sync"   // "1" reads what's been watched from Plex, Jellyfin and Emby every six hours; unset or "0" leaves it off
	KeyWatchedStatus = "watched.status" // the last read and cleanup (JSON; set by the app, not user-editable)
	KeyCleanupRules  = "cleanup.rules"  // the cleanup rules (JSON: enabled, moviesWatchedDays, moviesUnwatchedDays, episodesWatchedDays, keepTags); off unless enabled is true

	// Sign-in through a reverse proxy (Settings > Accounts). See docs/security.md.
	KeyAuthProxyHeader    = "auth.proxy_header"    // the header a trusted reverse proxy puts the signed-in user name in (for example Remote-User); unset or empty leaves it off. Only a signed-in browser session can set it, not an API key
	KeyAuthProxyAddresses = "auth.proxy_addresses" // where that proxy connects from: IP addresses or CIDR ranges, comma-separated ("private" is refused). The header is only believed from these
	KeyAuthOutsideSignIn  = "auth.outside_signin"  // when someone last signed in from outside the home network (RFC 3339; set by the app). Two-step sign-in is suggested for 30 days after

	// Other ways to sign in (Settings > Accounts). All off until switched on. See docs/security.md.
	KeyAuthOIDC        = "auth.oidc"         // encrypted; single sign-on with an OpenID Connect provider (JSON: enabled, name, issuer, client id and secret, scopes, claim names, account rules). Off unless enabled is true. Only a signed-in browser session can read or set it, not an API key. MEDIARIUM_DISABLE_SSO=1 overrides it
	KeyAuthLDAP        = "auth.ldap"         // encrypted; sign-in with a directory (LDAP or Active Directory) (JSON: enabled, server, bind account and password, filters, attribute names, account rules). Off unless enabled is true. Only a signed-in browser session can read or set it, not an API key. MEDIARIUM_DISABLE_SSO=1 overrides it
	KeyAuthIdleDays    = "auth.idle_days"    // browser sessions not used for this many days (1 to 7) end and the person signs in again. Unset or "0" leaves it off. API keys, calendar feeds and e-reader passwords are not affected
	KeyAuthPasskeys    = "auth.passkeys"     // "1" lets people sign in with a passkey; needs the public address (server.public_url). Unset or "0" leaves it off
	KeyAuthAutoSignIn  = "auth.auto_signin"  // "oidc" sends visitors who are not signed in straight to the single sign-on provider from the sign-in page (add ?no-redirect=1 to the address to see the normal page); unset or empty leaves it off. Only a signed-in browser session can set it, and only while single sign-on works
	KeyAuthLocalSignIn = "auth.local_signin" // "0" turns the username and password form off (only possible once an administrator has signed in with another method); anything else (including unset) leaves it on

	// A script to run after each import (Settings > System > Server and updates). See docs/scripts.md.
	KeyScriptAfterImport = "scripts.after_import" // JSON: script (a file name in /config/scripts; empty = off) and timeoutSec (default 300). Only a signed-in browser session can set it, not an API key
	KeyScriptLastRun     = "scripts.last_run"     // what the last run did (JSON; set by the app, not user-editable)

	// Search and quality (Settings > Library). Both off until switched on.
	KeySearchDelay  = "search.delay_profile"  // delay profile (JSON: preferredProtocol, usenetMinutes, torrentMinutes, bypassIfHighestQuality): automatic searches hold back releases of the less liked protocol for that many minutes; unset or 0 minutes = no wait
	KeyMinSizeCheck = "search.min_size_check" // "0" turns the built-in minimum size per quality off (Settings > Library > Quality > Size limits); anything else, including unset, leaves it on
	KeySizeLimits   = "quality.size_limits"   // size limits per quality for movies in MB per minute of runtime (JSON: enabled, limits); applied only while enabled is true

	// The same table for shows (it started as a copy of the movie table).
	KeySizeLimitsTV = "quality.size_limits_tv"

	// The folder a new movie or show gets, used when the naming style is Custom.
	KeyMovieFolderFormat  = "library.movie_folder_format"  // token string; unset = "{Movie Title} ({Year})"
	KeySeriesFolderFormat = "library.series_folder_format" // the same for a new show; unset = "{Series Title} ({Year})"

	// The request portal (Settings > Accounts > All accounts). Everything is off or unlimited until set.
	KeyRequestOptions = "requests.options" // JSON: notifyRequester (tell people when their request or report is answered), quotaCount (requests per period, 0 = no limit), quotaPeriod (day or week), fourK (let people ask for a 4K copy), fourKProfileId (the quality profile of that copy), reports (let people report a problem with a title)
	// Permissions of what Mediarium creates in the library (Settings > Library > Folders).
	KeyFileMode   = "library.file_mode"   // three octal digits like "664" applied to files Mediarium puts in the library; unset = leave it to the container's umask
	KeyFolderMode = "library.folder_mode" // the same for the folders it creates, like "775"

	// NFO and artwork files for media servers (Settings > Library > Folders). All off by default.
	KeyMetadataFiles = "library.metadata_files" // JSON: movies and tv, each {nfo, artwork}, and replace (true replaces files Mediarium wrote earlier); unset = nothing is written
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
