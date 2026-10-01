# What Mediarium does today

A plain comparison of what Mediarium does **today** against the apps it can replace or sit beside, written from the source code.

- **Snapshot:** 2026-10-01, version 1.3.1 (database migrations up to `0029`; see the [changelog](../CHANGELOG.md)). Read every "No" as "not found when this was written". If it matters to you, look in `internal/` and `web/src/pages`.
- **How each row was judged:** Yes = implemented and reachable in the UI or API. Partial = something real exists but it is narrower than the reference app. No = not found. The evidence is a package, file or endpoint you can open.
- **Reference apps** are described from general knowledge of those projects and may be out of date for their newest versions. Uncertain claims are worded loosely or left out.
- Abbreviations: **Rad** Radarr, **Son** Sonarr, **Lid** Lidarr, **Pro** Prowlarr, **Sab** SABnzbd, **qB** qBittorrent, **Baz** Bazarr.
- In Mediarium's screens, indexers are called **Indexers**.

## 1. Where Mediarium stands in one paragraph

Movies and TV both work end to end. That covers TMDB metadata; one search across Newznab/Torznab indexers and definition-based torrent sites (the community Cardigann definitions, downloaded on demand); grabbing; a built-in Usenet client (multiple servers, PAR2, native RAR and ZIP unpacking, `7z` only for `.7z`); and a built-in BitTorrent engine (with an embedded WireGuard VPN and kill switch). It also has hardlink import with naming presets, quality profiles with upgrade hunting, a blocklist with automatic retry, monitoring, a calendar, a wanted list, subtitles from OpenSubtitles (optional), notifications, media server refresh, family accounts, backups, a clean-up of the downloads folder, health checks and a first-run wizard. Music works too once you switch it on (see 2.4b). Audiobooks and ebooks are not built; they show as "coming soon".

What is thin or missing: the things that make the *arr apps manageable at scale (custom formats, delay profiles, tags, multiple root folders, list exclusions, a recycle bin, scheduled backups); more queue control for downloads (speed limits, scheduling, dragging to reorder; pause, resume and stop exist); depth in indexer management (no priorities or tags); a request portal; and a screen for matching releases by hand. Family accounts exist. An administrator can add basic-user accounts that browse, add and follow downloads without seeing the server settings (see [accounts.md](./accounts.md)). RAR (RAR4 and RAR5, including multi-part sets) and ZIP are unpacked natively in Go, so they work the same everywhere. Only the rare `.7z` format uses the `7z` tool, which the Docker image includes.

## 2. Matrix

### 2.1 Library, search and automation (Radarr / Sonarr territory)

| Feature | In Mediarium today | Evidence | In reference apps |
|---|---|---|---|
| Monitoring (movie, show, season, episode) | **Yes** | `PUT /api/movies/{id}/monitored`, `/api/series/{id}/monitored`, `/seasons/{season}/monitored`, `/episodes/{id}/monitored` | Rad, Son |
| Quality profiles (ordered tiers, cutoff, upgrade-until) | **Yes** | `internal/quality/profile.go` (15-tier ladder), `/api/quality-profiles` CRUD, per-title profile; five built-in presets: Cinema recordings, Any, 720p, 1080p (default) and 4K & over, all starting with upgrades off (see [quality-profiles.md](./quality-profiles.md)) | Rad, Son |
| Custom formats (spec-based scoring: release group, HDR, codec, language, regex) | **Partial** | Profiles only have "must contain", "must not contain" and "preferred terms" with a score (`migrations/0010`, `quality.Profile.Score`); no spec types, no import of TRaSH formats | Rad, Son (Recyclarr syncs TRaSH ones) |
| Quality definitions (min/max size per quality) | **No** | not found in `internal/quality` | Rad, Son |
| Delay profiles (wait N minutes for a preferred protocol) | **No** | not found; default preference is Usenet on ties (`preferUsenet` in `internal/api/automation.go`) and a per-title source setting (`0013`) | Rad, Son |
| Per-title protocol preference (usenet / torrent / both) | **Yes** | `PUT /api/movies/{id}/sources`, `library.default_sources` | partly (via delay profiles) |
| Indexer management (add, test, edit, enable/disable, delete) | **Yes** | `/api/indexers` GET/POST/PUT/DELETE, `/test`, `/{id}/test`, `/{id}/enabled`. Newznab and Torznab APIs, plus sites from the definition list (`kind: cardigann`) with their own settings; passwords, cookies and keys stored encrypted and never returned. A failed test of a definition-based site shows on the dashboard. No priorities | Rad, Son, Pro |
| Indexer definitions engine (Cardigann / hundreds of trackers) | **Yes** | `internal/indexers/cardigann*.go`, `definitions.go`, `GET /api/indexer-definitions`. Runs the community Prowlarr/Indexers definitions (schema v11, downloaded when the site list is opened, none bundled): login by form, POST, GET or cookie with CSRF inputs and error/test selectors, HTML, JSON and XML results, the template language and the common filters, magnet and `.torrent` downloads through the signed-in session, per-site request delay, optional FlareSolverr for Cloudflare. Almost all of the current definitions pass validation, and the site list marks the rest as unsupported, with the reason. Not supported: CAPTCHA logins, look-around regexes in `regexp` filters (skipped in `re_replace`), a handful of invalid selectors. Tested against local fixture sites; expect some real sites to need fixes | Pro |
| Indexer priorities, per-indexer tags, seed rules | **No** | `indexers` table has no priority column | Rad, Son, Pro |
| Sync indexers to other apps | **n/a** | Mediarium is one app; can import Torznab/Newznab URLs from Prowlarr manually | Pro |
| RSS sync | **Partial** | `rss-sync` job (every 15 min by default, a setting; `internal/api/automation.go`) runs an empty-query search on every enabled indexer and matches recent listings to monitored items. It is a listings poll, not a dedicated per-indexer RSS feed; no per-indexer toggle | Rad, Son |
| Scheduled missing / upgrade hunting | **Yes** | `hunt` job (every 6 h by default, a setting), `series-refresh` 12 h, `genre-backfill` hourly (`internal/api/automation.go`, `automation_tv.go`, `genres_job.go`) | Rad, Son |
| Manual / interactive search with reject reasons | **Yes** | `GET /api/movies/{id}/search`, `/api/series/{id}/search`, `ReleaseTable.tsx`; "Search now" per row | Rad, Son, Pro |
| Global cross-indexer search | **Yes** | `GET /api/search`, `POST /api/search/grab`, `web/src/pages/Search.tsx` | Pro |
| Calendar | **Yes** | `GET /api/calendar`, `pages/Calendar.tsx`. No iCal/ICS feed | Rad, Son (with iCal) |
| Wanted: missing and cutoff-unmet | **Yes** | `GET /api/wanted`, `pages/Wanted.tsx` | Rad, Son |
| Rename / organize on import (token naming, presets) | **Yes** | `internal/organizer/naming.go`, presets plex/jellyfin/kodi/minimal/custom, live preview `GET /api/settings/naming-preview` | Rad, Son |
| Bulk rename of an existing library | **No** | library import registers files "in place without moving or renaming" (`internal/libimport`, `CHANGELOG.md`) | Rad, Son |
| Import an existing library | **Yes** | `POST /api/library/scan`, `/api/library/import`, `GET /api/library/import/active`, `pages/ImportLibrary.tsx`. Confirming registers every title at once in one transaction (no lookups); a background worker (`internal/api/import_worker.go`) then fills in details, resumes after a restart and retries failures. Imported titles are added unmonitored and set to leave what you have alone; two switches on the review page (both off) start monitoring, and for shows also look for missing episodes ([import-library.md](./import-library.md)) | Rad, Son |
| Change many titles at once (Library select mode) | **Yes** | administrators pick titles (Shift-click for a range, "Select all N in this view") and monitor, switch better versions, set the quality profile or downloaders, search now (monitored and missing only, 25 at a time) or remove them, each with one request in one transaction: `PUT /api/library/bulk/{monitored,no-upgrade,profile,sources}`, `POST /api/library/bulk/{search-now,remove}`, and `/api/music/bulk/{follow,profile,remove}` for artists; files are deleted only when the request says so ([library.md](./library.md)) | Rad, Son |
| Move over from other apps | **Yes** | `internal/migrate`, `POST /api/migrate/preview`, `/api/migrate/run`, `GET /api/migrate/status`: twelve apps (Radarr, Sonarr, Prowlarr, SABnzbd, NZBGet, Jackett, NZBHydra2, Overseerr/Jellyseerr, Ombi, Bazarr, Medusa, SickChill), read-only on their side. Library (files registered in place), monitored flags, quality profile matching, indexers, Usenet servers, requests and subtitle languages ([migrate.md](./migrate.md)). Screen: Settings → System → Move from other apps. | Rad, Son, Pro, Sab |
| Manual import / manual match for unmatched releases | **No** | not built; only a naming-collision resolver exists (`POST /api/queue/{id}/resolve-conflict`) | Rad, Son |
| Multiple root folders | **No** | one `library.movies_path` and one `library.tv_path` (`internal/settings/settings.go`) | Rad, Son |
| Tags | **No** | no tag tables or endpoints | Rad, Son, Pro, qB |
| Import lists (automatic add from a list) | **Partial** | `GET /api/discover/import-list` browses a public Trakt list you paste, then you add titles by hand; nothing syncs on a schedule (`internal/trakt`) | Rad, Son |
| List exclusions | **No** | not found | Rad, Son |
| Season packs, multi-episode releases | **Yes** | `internal/parser`, `pipeline_tv.go`, `automation_tv.go`; a grab marks the episodes it will deliver as downloading when it is queued, and automatic searches never grab an episode that is already downloading, so a pack's episodes are only tried singly after the pack failed (`grab_claim.go`) | Son |
| Anime / absolute numbering / daily shows / series types | **No** | parser only handles `SxxEyy` forms; TV specials are excluded (`metadata/tv.go`) | Son |
| Minimum availability / release-date gating for movies | **Partial** | unreleased movies are skipped using the TMDB release date (`unreleased()` in `automation.go`); not configurable | Rad |
| Editions, movie collections | **No** | not found | Rad |
| Media management: recycle bin | **No** | deleting a title with "delete files" removes its folder (or, for a file loose in the library folder or in a shared folder, the file and the files named after it) and always cancels its downloads and deletes their working folders (`DELETE /api/movies/{id}`, `DELETE /api/series/{id}`, see [downloads.md](./downloads.md#removing-a-movie-or-show)) | Rad, Son |
| Media management: file permissions / chmod / chown | **No** | files are created 0644 / dirs 0755 (`pipeline.go`, `organizer/import.go`); ownership comes from `PUID`/`PGID` only | Rad, Son |
| Media management: existing-file conflict policy | **Yes** | `library.import_conflict_policy` skip / overwrite / overwrite-if-better / ask, atomic replace (`organizer/import.go`) | Rad, Son (differently) |
| Media management: hardlink then copy fallback | **Yes** | `organizer/import.go` tries `os.Link` and copies when the link fails for any reason (another drive or mount, or a filesystem without hardlinks). The "same drive" check on the dashboard compares device numbers (`organizer/samefs_unix.go`) and is not available on Windows; two Docker mounts of one disk can pass it and still copy, which is why the compose file uses one `/data` mount ([INSTALL.md](./INSTALL.md#why-one-data-folder-hardlinks)) | Rad, Son |
| Health checks | **Yes** | `GET /api/health`, `internal/api/health.go` (missing keys, no indexer, folder problems, VPN, recent failures, titles waiting for subtitles (only while subtitles are switched on), a missing `par2` or `7z`, and a shared Trakt or OpenSubtitles key reaching its limit) | Rad, Son, Pro |
| Shared-key usage tracking | **Yes** | `GET /api/usage` counts requests and limit hits (HTTP 429, OpenSubtitles 406) per service over 24 hours (`internal/usage`); the dashboard suggests a free personal key when the shared one is busy | none |
| Logs and errors | **Yes** | Settings > System > Logs and errors: real problems only (failed downloads, Usenet, indexers, media servers, import, disk and folders, database, notifications), each with a plain explanation and what to try, repeats counted on one row, kept 30 days, copy for support and a text file, optional message on new errors (`internal/problems`, `GET /api/system/problems`; see [logs-and-errors.md](./logs-and-errors.md)) | Rad, Son, Pro (their log pages), Sab (warnings) |
| Notifications | **Yes** | `internal/notify`: webhook, Discord, Telegram, email, ntfy, Gotify, Pushover, Slack, per-target event choice, test button (`POST /api/notifications/test`). Fewer targets than the reference apps' long lists | Rad, Son, Pro, Sab, Baz |
| Media server connection (Plex, Jellyfin, Emby) | **Yes** | `internal/mediaservers`, `/api/media-servers`: find servers on the network, sign in with Plex, Jellyfin Quick Connect or a Jellyfin/Emby login (or add one with a token or API key), test, partial library scan of the imported folder after each import (debounced, with path mapping, full-scan fallback), **Refresh now**, "Watch in" links found by TMDB id (`GET /api/media-servers/links`, open to members), dashboard item when a server fails. See [media-servers.md](./media-servers.md) | Rad, Son (Connect: Plex, Emby/Jellyfin library update) |
| History | **Yes** | `GET /api/activity`, Activity page; per title `GET /api/movies/{id}/events` and `GET /api/series/{id}/events`: each search with how many releases were acceptable and why not, the grab and the profile used, download and post-processing steps, failures, blocklisting and retries (see [activity.md](./activity.md)) | Rad, Son, Pro |
| Blocklist (auto on bad release, manual, retry next best) | **Yes** | `internal/blocklist`, `/api/blocklist`, `POST /api/queue/{id}/blocklist`. **Retry** on a failed Usenet download repeats post-processing on the files it already has instead of downloading again; one download at a time per movie or episode (a grab by hand while one runs gets 409) | Rad, Son |
| Updates and restarts | **Yes** | new-version notice (daily GitHub check, notes, steps for Docker and native), signed **Update now** and overnight install, pushed updates (opt-in, `POST /api/system/update`), restart and safe restart, self-restart when stuck, Docker health check (`internal/updatecheck`, `internal/selfupdate`, `docker-entrypoint.sh`); see [INSTALL.md](./INSTALL.md#updating) and [security.md](./security.md#updating-the-program) | Rad, Son (built-in updater) |
| Backup / restore | **Partial** | admin-only `GET /api/system/backup` (consistent `VACUUM INTO` snapshot + `secret.key` + manifest, as a zip) and `POST /api/system/restore` (strict validation, staged, applied on the next start; old files kept in `before-restore-*`), `internal/backup`. No scheduled backup | Rad, Son, Pro, Sab, Baz |
| API keys and authentication | **Yes** | local accounts, cookie sessions, rate-limited login, `X-API-Key` (`internal/auth`, `/api/auth/api-keys`) | all |
| External auth (OIDC, proxy header, basic) | **No** | not built | Rad, Son, Pro (forms/basic/external) |
| Prometheus metrics | **Partial** | `GET /api/metrics` (behind auth, three gauge families: `mediarium_up`, movies by status, queue items by status). The usual path would be `/metrics` | Rad, Son via exporters |
| Statistics / disk-space overview | **Partial** | dashboard with folder health, usage and quality breakdown (`internal/api/dashboard.go`) | Rad, Son |
| A title's files on disk, playing a video in the browser | **Partial** | `GET /api/movies/{id}/files` and `GET /api/series/{id}/files` list the title's folder (relative path, size, date, kind: video, subtitle, image, nfo, other). `GET /api/files/stream?movie={id}&path=...` (or `series=`) sends a video file as it is, with seeking, for a `<video>` player. It also previews pictures (jpg, png, webp, gif, with their image type) and text files (nfo, srt, ass, ssa, vtt, txt, up to 2 MB, always as `text/plain`). Anything else, html, svg and scripts included, is refused with 415, and every file is sent with `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox` and `Content-Disposition: inline`. Only files inside the movies and TV folders can be reached (no `../`, no symlinks leading out). No transcoding: MP4 and WebM play in every browser, MKV only where the browser supports it (`internal/mediafiles`) | Plex, Jellyfin, Emby (with transcoding) |
| Discover, recommendations | **Yes** | `/api/discover/*`, "More like your library" (recent, popular titles that several of your titles point to; a paged "Browse more" list with a switch for older titles, `list=similar` on `/api/discover/list`), browse by genre, year and sort order (`/api/discover/browse`, `/api/discover/genres`), pageable trending, popular and coming-soon rails for movies and shows (`/api/discover/list`) (`pages/Discover.tsx`) | Overseerr / Jellyseerr-style, partly Rad, Son |
| First-run wizard with live folder checks | **Yes** | `pages/Onboarding.tsx`, `/api/settings/folder-check` | none of them as such |
| Mobile-friendly UI, light/dark theme | **Yes** | `web/src/index.css`, `ThemeToggle.tsx` | Rad, Son (mobile partly) |

### 2.2 Usenet downloader (SABnzbd territory)

| Feature | In Mediarium today | Evidence | In reference apps |
|---|---|---|---|
| NZB download via NNTP, SSL, yEnc | **Yes** | `internal/download/nntp.go`, `yenc.go`, `nzb.go`, `downloader.go` | Sab |
| Multiple servers, primary and backup with priority | **Yes** | `DownloadFromServers` (falls through per article), `usenet_servers` UI, `migrations/0012` | Sab |
| Connections per server, test button | **Yes** | `Connections` in `ClientConfig`, `POST /api/usenet-servers/test` | Sab |
| PAR2 verify and repair | **Yes, needs `par2` binary** | `internal/organizer/par2.go`; skipped silently if `par2` is missing unless articles are missing | Sab |
| Unpack (RAR, ZIP, 7z) | **Yes** | `internal/organizer/archive.go`: RAR (single, `.partNN.rar`, `.rar`+`.r00`) via pure-Go `rardecode`, ZIP via `archive/zip`, both with path-traversal and size-bomb protection; password-protected archives fail as a bad release. Only `.7z` shells out to `7z` | Sab |
| Pause / resume queue or item | **Yes** | `POST /api/queue/{id}/pause`, `/resume`, `/stop`, `POST /api/queue/pause-all`, `/resume-all` (`queue_control.go`); a paused download keeps its files and stays paused after a restart | Sab, qB |
| Speed limit, scheduling | **No** | no setting or code | Sab, qB |
| Queue ordering / priority per item | **Partly** | downloads wait in one line: what a person started goes before what the automatic searches added, then first come, first served; a resumed download goes to the front of its group (`priority` and `line_seq` on `download_queue`, `internal/queue/dispatch.go`). There is no drag to reorder | Sab, qB |
| Categories | **No** | movies vs TV is tracked in the queue row, but there is no user-visible category concept and no per-category folder or script | Sab, qB |
| Post-processing scripts | **No** | none | Sab |
| Retention / article-age handling, duplicate detection, direct unpack | **No** | none | Sab |
| Failed download handling (blocklist and pick next release) | **Yes** | `internal/api/pipeline.go`, `blocklist.go` | Rad, Son (Sab reports failure) |
| Add an NZB by upload or URL directly | **No** | grabs only come from indexer results (`POST /api/search/grab`, `/api/movies/{id}/grab`) | Sab |
| RSS feeds in the downloader | **No** (handled by automation, above) | | Sab |
| Live queue with progress | **Yes** | `GET /api/queue`, `GET /api/downloads/status` | Sab |

### 2.3 Torrent client (qBittorrent territory)

| Feature | In Mediarium today | Evidence | In reference apps |
|---|---|---|---|
| Magnet and .torrent grabs | **Yes** | `internal/torrentclient` (`anacrolix/torrent`), `pipeline.go` `downloadTorrent` | qB |
| Seeding limits (ratio, time) | **Yes, global only** | `torrent.seed_ratio_limit`, `torrent.seed_time_limit_h`, `SeedUntilGoal` in `internal/torrentclient` (stops the torrent and, once imported, deletes its data) | qB |
| Categories and per-category save paths | **No** | deliberately not built so far | qB |
| Speed limits (global, per torrent, alternative) | **No** | not found | qB |
| Queueing / max active torrents | **Yes** | one shared limit, **Downloads at the same time** (1 to 5, one by default, `downloads.concurrent`), for Usenet and torrents together; a download keeps its place until it is imported, a seeding torrent does not (`internal/queue/dispatch.go`, `internal/api/dispatch.go`) | qB |
| Stalled-download detection | **No** | none | qB (Cleanuparr adds it for *arr queues) |
| Per-torrent priority, file selection, sequential download | **No** | `DownloadAll()` only | qB |
| A torrent list UI (peers, trackers, pause, recheck) | **No** | torrents appear as queue rows only | qB |
| Listen port setting | **Yes** | `torrent.listen_port`, default 58264 (TCP and UDP), published in the compose files; one shared engine for all torrents; `GET /api/downloads/status` reports whether incoming connections have been seen | qB |
| Port forwarding | **No** | not configurable; disabled when the VPN tunnel is used (`torrentclient.New`) | qB (UPnP); VPN providers' own port forwarding needs external glue |
| VPN with kill switch, no `NET_ADMIN` | **Yes** | `internal/vpn` (embedded userspace WireGuard), `vpn.require_for_torrents`, `/api/vpn/*`, egress IP check | none built in (Gluetun sidecar is the usual answer) |
| Peer discovery when tunneled | **Partial** | with a tunnel, DHT, uTP and incoming peers are off (`torrentclient.New`), so magnets rely on trackers | qB |

### 2.4 Subtitles (Bazarr territory)

| Feature | In Mediarium today | Evidence | In reference apps |
|---|---|---|---|
| Providers | **Partial: one** | OpenSubtitles only (`internal/subtitles/opensubtitles.go`) | Baz (many) |
| Subtitles that come with the release | **Yes** | `.srt .ass .ssa .sub .idx .sup` files in the finished download are copied next to the video as `<video>.<lang>.<ext>`; language read from the file or folder name, episodes in a pack matched by `SxxEyy` (`internal/subtitles/sidecar.go`, `langdetect.go`) | Baz (reads existing files) |
| Master switch, off by default | **Yes** | `subtitles.enabled` (`subtitlesEnabled` in `/api/settings` and `/api/modules`). While off there are no searches or downloads, no subtitle health items, no Wanted tab or count, no subtitle tools on title pages, and the subtitle routes answer `409`. Subtitles inside a download are still imported | Baz (always on) |
| Offer, not auto-fetch | **Yes** | with the switch on, `subtitles.auto_download` is off unless turned on; a health item offers the missing ones, the Wanted page fetches them on request (`POST /api/subtitles/get`) | Baz (always automatic) |
| Auto download after import and missing sweep | **Yes, opt in** | `subtitles_auto.go`, `subtitle-sweep` job, Wanted page tab; only when `subtitles.auto_download` is `1` | Baz |
| Daily download limit awareness | **Yes** | downloads and OpenSubtitles' reported `remaining` are tracked; `GET /api/subtitles/quota` warns when more is wanted than the day allows | Baz |
| "No subtitles wanted" per title | **Yes** | `POST` / `DELETE /api/subtitles/dismiss`, left out of Wanted, the offer and every fetch | Baz (per-series profiles) |
| Manual search and choose per movie or episode | **Yes** | `/api/movies/{id}/subtitles`, `/api/episodes/{id}/subtitles` | Baz |
| Best-fit ranking (release group, source, codec) | **Yes** | `internal/subtitles/pick.go` | Baz (scored) |
| Multiple languages | **Yes, global list** | `subtitles.languages` | Baz (per-language profiles) |
| Per-language profiles (forced, hearing impaired, per series) | **No** | none. Existing and imported `.forced` files are kept but do not count as a full subtitle in that language; `.sdh` files do | Baz |
| Upgrade existing subtitles | **No** | none | Baz |
| Sync / re-time subtitles | **No** | none | Baz |
| Embedded-subtitle detection | **No** | not found | Baz |

### 2.4b Music (Lidarr territory)

Switch it on in Settings > Media types ([modules.md](./modules.md), [music.md](./music.md)). Until then every `/api/music/` route answers 404 and nothing runs in the background.

| Feature | In Mediarium today | Evidence | In reference apps |
|---|---|---|---|
| Module switchboard (movies, TV, music; audiobooks and ebooks "coming soon") | **Yes** | `GET`/`PUT /api/modules`, settings `modules.*`, `internal/api/modules.go`; switching a module off stops its automation and refuses new titles (409), deleting nothing | none |
| Artist search and metadata | **Yes** | metadata client (`internal/music*`): identifying User-Agent, one request per second from a shared bucket, retry with backoff on 503, in-memory cache; `GET /api/music/search?q=` | Lid |
| Cover art | **Yes** | the front cover (500 px) from the cover-art service is fetched once per album, cached under the config folder and served by `GET /api/music/albums/{id}/cover` and `/api/music/artists/{id}/cover` (cache headers, plain 404 when there is none); saved as `cover.jpg` in the album folder on import when none exists, never overwriting (`internal/api/music_cover.go`) | Lid |
| Artists, albums, EPs, singles, tracklists | **Yes** | `POST /api/music/artists` with monitor all / future / none, `GET /api/music/artists[/{id}]`, `GET /api/music/albums/{id}`; the canonical release (earliest official, most common country) supplies the tracklist; live albums, compilations and remixes are left out | Lid |
| Monitoring (artist, album) | **Yes** | `PUT /api/music/albums/{id}/monitored` decides what is searched; an artist is followed or not (`PUT /api/music/artists/{id}` with `monitored`, `profileId`): a followed artist is checked every 12 hours and new releases are added monitored (`music-refresh` job, `internal/api/music_follow.go`) | Lid |
| Audio quality profiles | **Yes** | tiers MP3-192, MP3-256, AAC-256, MP3-320/V0, FLAC, FLAC 24bit; presets Lossy (MP3 320), the default, and Lossless (FLAC) with a Lossy fallback; cutoff, upgrades, fallback chain (`internal/music/quality.go`); profiles can be made, edited and deleted and the default chosen (`/api/music/profiles`, `musicDefaultProfileId`), with the same rules as video profiles; edited in Settings > Library > Quality (`MusicProfiles.tsx`, reusing the fallback editor), per-artist profile on the artist page | Lid |
| Release-name parser for music | **Yes** | artist, album, year, format, bitrate, bit depth, source, discography (`internal/music/parse.go`, table-driven tests with P2P and scene names) | Lid |
| Search and grab (Usenet and torrent) | **Yes** | Newznab/Torznab categories 3000, 3010, 3040, 3050; interactive search with rejection reasons `POST /api/music/albums/{id}/search`, `/grab`, `/search-now`; scheduled hunt (25 albums per run) and retry after a bad release; same queue, downloaders and blocklist as movies | Lid |
| Import and naming | **Yes** | audio files matched to the tracklist (number, then title), `Artist/Album (Year)/NN - Title.ext` (`D-NN` on multi-disc), hardlink then copy, `cover.jpg`/`folder.jpg` kept, old files replaced on an upgrade; per-album activity `GET /api/music/albums/{id}/events` | Lid |
| Files of an album and playing a track in the browser | **Yes** | `GET /api/music/albums/{id}/files` (like the movie listing, tracks marked with `trackId`) and `GET /api/files/stream?album={id}&path=` with the right audio types (`audio/flac`, `audio/mpeg`, `audio/mp4`, `audio/aac`, `audio/ogg`, `audio/opus`), ranges, the same sandbox headers and path safety through `internal/mediafiles` (`ForDir`, inside the music folder only). No transcoding | Lid |
| Import an existing music folder | **Yes** | `POST /api/music/import/scan`, `GET /api/music/import/scan/{id}` (admin): matches Artist/Album folders in the music database, registers files in place, reports the unmatched | Lid |
| Reading embedded audio tags and real quality | **Yes** | `github.com/dhowden/tag` (BSD-2-Clause) for artist, album artist, album, title, track and disc, year and picture (`internal/music/tags.go`); in-house header readers for FLAC, MP3 (Xing/VBRI/Info), M4A (AAC, ALAC), ADTS and Ogg Vorbis/Opus give bit depth and bitrate (`internal/music/audioinfo.go`). Tags identify an album when scanning and match files to the tracklist after a download; the quality of files on disk is read from them, and a file that cannot be read stays Unknown. Files and tags are never rewritten | Lid |
| Media server refresh after a music import | **Yes** | the same refresher as movies and TV (`mediaservers.MediaMusic`): Plex scans the album folder in its music library (type artist; all music libraries when no folder matches), Jellyfin and Emby get the folder through `Library/Media/Updated`; 15 second debounce, path mapping | Lid |
| Music screens: Media types page, Library Music tab, artist page, collection import | **Yes** | `web/src/pages/settings/ModulesSettings.tsx`, `MusicLibrary.tsx`, `MusicArtist.tsx`, `MusicImport.tsx`; the app-wide modules state (`ModulesContext.tsx`) hides the Movies, TV and Music tabs of modules that are off, for every account; artist search with an add dialog in Discover (`MusicDiscover.tsx`, `AddMusicDialog.tsx`, the same dialog in both) and in the header search; follow switch and profile picker on the artist page; album Files tab with an in-app audio player (`FilesPanel.tsx`); local cover art | Lid |
| Music in the calendar, dashboard tiles, Wanted page | **Partial** | Wanted (missing and upgrades, with a kind filter and Search now), Activity (queue rows with cover, retry, blocklist, waiting list) and the dashboard's recent lists show albums (`Wanted.tsx`, `Queue.tsx`, `Dashboard.tsx`); albums are in `/api/calendar` as `kind: "album"` (`albumCalendarEntries`) and on the Upcoming calendar with a music badge (`CalendarGrid.tsx`), but not in the dashboard's coming-up list, and its folder tiles are movies and TV only (`library.music` carries the music counts, see [music.md](./music.md)) | Lid |

### 2.5 Users, requests and the wider ecosystem

| Feature | In Mediarium today | Evidence | Where it exists |
|---|---|---|---|
| Multiple users and roles | **Partly** | admin and basic-user roles (`member` in the API), accounts managed by an administrator (`/api/users`); one route table decides who may call what (`internal/api/server.go`, `access.go`); no read-only role, no per-user libraries or quotas | Overseerr, Jellyseerr (users, roles); *arr apps are single-login |
| Request portal (family asks, admin approves) | **No** | not found | Overseerr, Jellyseerr |
| Books (Readarr), adult (Whisparr) | **No** | music is covered in 2.4b; audiobooks and ebooks show as "coming soon" in Settings and are not built | Readarr, Whisparr |
| Play-state stats | **No** | | Tautulli |
| Archive extraction helper for other apps | **n/a** (built in) | | Unpackerr |
| Sync TRaSH profiles and custom formats | **No** | no custom-format import | Recyclarr |
| Notification hub | **Partial** | built-in senders, no Notifiarr integration | Notifiarr |
| Auto hunt for missing / upgrades | **Yes** (built into automation) | `hunt` job | Huntarr |
| Clean stalled / failed queue items | **Partial** | failed downloads are blocklisted and retried; stalled ones are not detected. Working folders are removed after import, torrent data once its seeding goal is met, and a daily clean-up (`GET/POST /api/system/cleanup`) removes orphaned, leftover and empty folders in the downloads working folder (never the library) and prunes history older than `cleanup.history_retention_days` (see [downloads.md](./downloads.md#clean-up)) | Cleanuparr |
| Library cleanup rules (unwatched, old) | **No** | | Maintainerr |

## 3. What to add next

Effort: **S** = days or less, **M** = about 1 to 2 weeks, **L** = weeks. Priority reasons are about making a public v1 credible, not about copying everything.

### P0: the most important gaps

| # | Item | Effort | Why |
|---|---|---|---|
| 1 | ~~Make RAR unpack work in the image~~ **Done:** RAR is unpacked natively in Go; the health check now notes a missing `7z` (info, `.7z` only) and a missing `par2` | S | Most Usenet releases are RAR |
| 2 | ~~Hardlink defaults~~ **Partly done:** the compose file and guides use one `/data` mount. Still open: make the "same drive" check try a real `os.Link` probe instead of comparing device numbers | S | Separate mounts silently copy; the dashboard can say everything is fine |
| 3 | ~~Windows: copy when a link fails~~ **Done:** any link failure falls back to a copy. Still open: a Windows same-drive check | S | The dashboard cannot warn about separate drives on Windows |
| 4 | In-app backup and restore (SQLite `VACUUM INTO` plus `secret.key`, download, restore on start) **done** (Settings > System > Server and backup); still open: scheduled backups | M | `/config` copy while running can corrupt; the selling point is "one folder", so make it one click |
| 5 | Download queue control: ~~pause/resume~~ **done** (Pause, Resume, Stop, Pause all, Resume all, and a download line); still open: speed limit, dragging to reorder (Usenet and torrent) | M | Baseline expectation coming from SABnzbd or qBittorrent |
| 6 | Manual import / manual match UI for releases that fail matching | M to L | Named in the original plan; without it failures are dead ends |
| 7 | ~~Edit indexers~~ **Done** (`PUT /api/indexers/{id}`); still open: per-indexer priority, per-indexer RSS toggle | S to M | Priorities decide which indexer wins a tie |
| 8 | ~~A real definitions engine~~ **Done:** torrent sites can be added from the community definition list without Prowlarr or Jackett (see [indexers.md](./indexers.md)); still open: proving it against more real sites, CAPTCHA logins | L | Without it torrent users needed Prowlarr or Jackett, which defeats the "no other containers" pitch |

### P1: needed to compete with *arr for daily use

| # | Item | Effort |
|---|---|---|
| 9 | Torrent management page (list, speed limits, stall detection, seeding view); reuse one long-lived client instead of one per grab | L |
| 10 | Custom formats with scoring (regex, release group, HDR, audio) plus TRaSH import | L |
| 11 | Delay profiles (prefer one protocol, wait N minutes) | M |
| 12 | Multiple root folders per media type | M |
| 13 | Tags (indexers, notifications, titles) | M |
| 14 | Recycle bin and file-permission (chmod) options | S |
| 15 | Import lists that sync on a schedule (Trakt, TMDB lists, Plex watchlist) plus exclusions | M |
| 16 | More subtitle providers and per-language profiles | M to L |
| 17 | ~~Multi-user with roles~~ **Done:** admin and basic-user accounts (see [accounts.md](./accounts.md)); still open: a read-only role | M |
| 18 | iCal feed for the calendar; expose `/metrics` outside the auth wall optionally | S |

### P2: polish

| # | Item | Effort |
|---|---|---|
| 19 | Usenet scheduling and categories, post-processing scripts | M |
| 20 | OIDC / reverse-proxy header auth | M |
| 21 | Anime handling (absolute numbering, series types, daily shows), specials | M to L |
| 22 | Subtitle upgrade and sync | M |
| 23 | Quality size limits, editions, movie collections | M |
| 24 | Bulk rename of an existing library | M |
| 25 | Windows and macOS installers that are tested and signed | M |

### P3: later

| # | Item | Effort |
|---|---|---|
| 26 | Request portal (needs #17) | L |
| 27 | Play-state sync and stats from Plex, Jellyfin, Emby | L |
| 28 | Library cleanup rules | M |

## 4. Optional modules for later

Each of these could become a module of its own. The projects named are for ideas only; check a project's licence before reusing any of its code.

| Module | What it would do | Projects to study |
|---|---|---|
| Music | Metadata, release matching, per-track naming and tagging, quality profiles for FLAC/MP3. Everything except writing tags is built (see 2.4b) | Lidarr (the *arr way), Picard (metadata and tagging), beets (tagging and organizing library tool) |
| Books and audiobooks | Author/series tracking, ebook and audiobook formats, metadata from Open Library or similar | Readarr (same family), Calibre-Web (library), Audiobookshelf (audiobooks and podcasts server) |
| Comics | Volume/issue tracking with a comics metadata source | Mylar3, Kapowarr |
| Podcasts | Subscribe to feeds, download episodes, retention rules | Audiobookshelf also handles podcasts |
| Anime specifics | Absolute numbering, AniDB or similar IDs, release-group preferences, dual audio | Sonarr's anime handling, Shoko |
| Request portal | Users ask, admins approve, requests turn into monitored titles; builds on the basic-user accounts that now exist | Overseerr, Jellyseerr |
| Watch-status sync | Import play state and ratings from Plex, Jellyfin or Emby, sync to Trakt, drive "watched" cleanup | Tautulli (Plex), Jellystat (Jellyfin), Trakt's own API |
| Library cleanup | Rules such as "unwatched for a year" with a dry-run and undo | Maintainerr |
| Stats | Downloads per month, storage over time, indexer success rates | Tautulli for the presentation ideas |
| Notifications | Already partly built; a bridge to Apprise-style URLs would cover hundreds of services | Apprise (used by other self-hosted apps), Notifiarr |

## 5. Things that work differently than you might expect

- **Download folders:** each download works in `downloads/incomplete/queue-N` and is imported straight from there. The `downloads/complete` folder is created but not used.
- **Metrics endpoint:** it is `GET /api/metrics`, behind sign-in, not the usual `/metrics`.
- **Hardlinks and folder mounts:** hardlinks only work inside one mounted folder, so the compose file and every platform guide use one `/data` mount for downloads, movies and TV. Separate mounts copy files instead. See [INSTALL.md](./INSTALL.md#why-one-data-folder-hardlinks).
- **Install methods:** Docker is the only supported install for now. Native Windows, macOS and Linux programs are attached to each release as a preview, and the app-store entries are prepared but not submitted ([PLATFORMS.md](./PLATFORMS.md)).
- **Backups:** the in-app backup is the easy way (Settings > System > Server and backup). By hand, stop the container first, so no download is writing files while you copy them.
