# Feature-parity audit

An honest comparison of what Mediarium does **today** against the apps it aims to replace or sit beside. Written from the source code, not from the original plan's intentions.

- **Snapshot:** 2026-09-28, working tree with migrations up to `0015`. The code is changing daily (notifications, for example, grew several new targets while this was being written), so treat every "No" as "not found when this was written" and re-check `internal/` and `web/src/pages` before acting on it.
- **How each row was judged:** Yes = implemented and reachable in the UI or API. Partial = something real exists but it is narrower than the reference app. No = not found. Evidence is a package, file or endpoint you can open.
- **Reference apps** are described from general knowledge of those projects and may be out of date for their newest versions. Where a claim about another project is uncertain it is worded loosely or left out.
- Abbreviations: **Rad** Radarr, **Son** Sonarr, **Pro** Prowlarr, **Sab** SABnzbd, **qB** qBittorrent, **Baz** Bazarr.

## 1. Where Mediarium stands in one paragraph

Movies and TV both work end to end: TMDB metadata, unified search over Newznab/Torznab indexers and definition-based torrent sites (the community Cardigann definitions, downloaded on demand), grab, a built-in Usenet client (multi-server, PAR2, native RAR and ZIP unpack, `7z` only for `.7z`) and a built-in BitTorrent engine (with an embedded WireGuard VPN and kill switch), hardlink import with naming presets, quality profiles with upgrade hunting, blocklist and automatic retry, monitoring, calendar, wanted list, subtitles from OpenSubtitles, notifications, health checks and a first-run wizard. What is thin or missing: everything that makes the *arr apps "manageable at scale" (custom formats, delay profiles, tags, multiple root folders, list exclusions, recycle bin, scheduled backups), real queue control for downloads (pause, speed limits, scheduling), indexer management depth (no priorities or tags), a request portal, and a manual-match UI. Family accounts exist: an administrator can add member accounts that browse, add and follow downloads without seeing settings (see [accounts.md](./accounts.md)). The biggest bug found in the original audit, that the Docker image could not unpack RAR archives (Alpine's `7z` has no RAR codec), is fixed: RAR and ZIP are now unpacked natively in Go (see [INSTALL.md](./INSTALL.md#rar-archives)).

## 2. Matrix

### 2.1 Library, search and automation (Radarr / Sonarr territory)

| Feature | In Mediarium today | Evidence | In reference apps |
|---|---|---|---|
| Monitoring (movie, show, season, episode) | **Yes** | `PUT /api/movies/{id}/monitored`, `/api/series/{id}/monitored`, `/seasons/{season}/monitored`, `/episodes/{id}/monitored` | Rad, Son |
| Quality profiles (ordered tiers, cutoff, upgrade-until) | **Yes** | `internal/quality/profile.go` (15-tier ladder), `/api/quality-profiles` CRUD, per-title profile; four built-in presets, Any, 720p, 1080p (default) and 4K & over (see [quality-profiles.md](./quality-profiles.md)) | Rad, Son |
| Custom formats (spec-based scoring: release group, HDR, codec, language, regex) | **Partial** | Profiles only have "must contain", "must not contain" and "preferred terms" with a score (`migrations/0010`, `quality.Profile.Score`); no spec types, no import of TRaSH formats | Rad, Son (Recyclarr syncs TRaSH ones) |
| Quality definitions (min/max size per quality) | **No** | not found in `internal/quality` | Rad, Son |
| Delay profiles (wait N minutes for a preferred protocol) | **No** | not found; default preference is Usenet on ties (`preferUsenet` in `internal/api/automation.go`) and a per-title source setting (`0013`) | Rad, Son |
| Per-title protocol preference (usenet / torrent / both) | **Yes** | `PUT /api/movies/{id}/sources`, `library.default_sources` | partly (via delay profiles) |
| Indexer management (add, test, edit, enable/disable, delete) | **Yes** | `/api/indexers` GET/POST/PUT/DELETE, `/test`, `/{id}/test`, `/{id}/enabled`. Newznab and Torznab APIs, plus sites from the definition list (`kind: cardigann`) with their own settings; passwords, cookies and keys stored encrypted and never returned. A failed test of a definition-based site shows on the dashboard. No priorities | Rad, Son, Pro |
| Indexer definitions engine (Cardigann / hundreds of trackers) | **Yes (new)** | `internal/indexers/cardigann*.go`, `definitions.go`, `GET /api/indexer-definitions`. Runs the community Prowlarr/Indexers definitions (schema v11, downloaded when the site list is opened, none bundled): login by form, POST, GET or cookie with CSRF inputs and error/test selectors, HTML, JSON and XML results, the template language and the common filters, magnet and `.torrent` downloads through the signed-in session, per-site request delay, optional FlareSolverr for Cloudflare. 565 of the 568 current definitions pass validation. Not supported: CAPTCHA logins, look-around regexes in `regexp` filters (skipped in `re_replace`), a handful of invalid selectors. Tested against local fixture sites; expect some real sites to need fixes | Pro |
| Indexer priorities, per-indexer tags, seed rules | **No** | `indexers` table has no priority column | Rad, Son, Pro |
| Sync indexers to other apps | **n/a** | Mediarium is one app; can import Torznab/Newznab URLs from Prowlarr manually | Pro |
| RSS sync | **Partial** | `rss-sync` job every 15 min (`internal/api/automation.go`) runs an empty-query search on every enabled indexer and matches recent listings to monitored items. It is a listings poll, not a dedicated per-indexer RSS feed; no per-indexer toggle | Rad, Son |
| Scheduled missing / upgrade hunting | **Yes** | `hunt` job every 30 min, `series-refresh` 12 h, `genre-backfill` hourly (`internal/api/automation.go`, `automation_tv.go`, `genres_job.go`) | Rad, Son |
| Manual / interactive search with reject reasons | **Yes** | `GET /api/movies/{id}/search`, `/api/series/{id}/search`, `ReleaseTable.tsx`; "Search now" per row | Rad, Son, Pro |
| Global cross-indexer search | **Yes** | `GET /api/search`, `POST /api/search/grab`, `web/src/pages/Search.tsx` | Pro |
| Calendar | **Yes** | `GET /api/calendar`, `pages/Calendar.tsx`. No iCal/ICS feed | Rad, Son (with iCal) |
| Wanted: missing and cutoff-unmet | **Yes** | `GET /api/wanted`, `pages/Wanted.tsx` | Rad, Son |
| Rename / organize on import (token naming, presets) | **Yes** | `internal/organizer/naming.go`, presets plex/jellyfin/kodi/minimal/custom, live preview `GET /api/settings/naming-preview` | Rad, Son |
| Bulk rename of an existing library | **No** | library import registers files "in place without moving or renaming" (`internal/libimport`, `CHANGELOG.md`) | Rad, Son |
| Import an existing library | **Yes** | `POST /api/library/scan`, `/api/library/import`, `pages/ImportLibrary.tsx` | Rad, Son |
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
| Media management: hardlink then copy fallback | **Yes** (Linux/macOS) / **Partial** (Windows) | `organizer/import.go` `os.Link`, falls back to copy only on `EXDEV`; on Windows the cross-drive error is different and would fail instead (see INSTALL.md) | Rad, Son |
| Health checks | **Yes** | `GET /api/health`, `internal/api/health.go` (missing keys, no indexer, folder problems, VPN, recent failures, titles waiting for subtitles, and a shared Trakt or OpenSubtitles key reaching its limit). Does not check for `7z` / `par2` | Rad, Son, Pro |
| Shared-key usage tracking | **Yes** | `GET /api/usage` counts requests and limit hits (HTTP 429, OpenSubtitles 406) per service over 24 hours (`internal/usage`); the dashboard suggests a free personal key when the shared one is busy | none |
| Notifications | **Yes** | `internal/notify`: webhook, Discord, Telegram, email, ntfy, Gotify, Pushover, Slack, per-target event choice, test button (`POST /api/notifications/test`). Fewer targets than the reference apps' long lists | Rad, Son, Pro, Sab, Baz |
| Media server connection (Plex, Jellyfin, Emby) | **Yes** | `internal/mediaservers`, `/api/media-servers`: test, partial library scan of the imported folder after each import (debounced, with path mapping, full-scan fallback), **Refresh now**, "Watch in" links found by TMDB id (`GET /api/media-servers/links`, open to members), dashboard item when a server fails. See [media-servers.md](./media-servers.md) | Rad, Son (Connect: Plex, Emby/Jellyfin library update) |
| History | **Yes** | `GET /api/activity`, Activity page; per title `GET /api/movies/{id}/events` and `GET /api/series/{id}/events`: each search with how many releases were acceptable and why not, the grab and the profile used, download and post-processing steps, failures, blocklisting and retries (see [activity.md](./activity.md)) | Rad, Son, Pro |
| Blocklist (auto on bad release, manual, retry next best) | **Yes** | `internal/blocklist`, `/api/blocklist`, `POST /api/queue/{id}/blocklist`. **Retry** on a failed Usenet download repeats post-processing on the files it already has instead of downloading again; one download at a time per movie or episode (a grab by hand while one runs gets 409) | Rad, Son |
| Backup / restore | **Partial** | admin-only `GET /api/system/backup` (consistent `VACUUM INTO` snapshot + `secret.key` + manifest, as a zip) and `POST /api/system/restore` (strict validation, staged, applied on the next start; old files kept in `before-restore-*`), `internal/backup`. No scheduled backup | Rad, Son, Pro, Sab, Baz |
| API keys and authentication | **Yes** | local accounts, cookie sessions, rate-limited login, `X-API-Key` (`internal/auth`, `/api/auth/api-keys`) | all |
| External auth (OIDC, proxy header, basic) | **No** | planned for later | Rad, Son, Pro (forms/basic/external) |
| Prometheus metrics | **Partial** | `GET /api/metrics` (behind auth, two gauge families). The usual path would be `/metrics` | Rad, Son via exporters |
| Statistics / disk-space overview | **Partial** | dashboard with folder health, usage and quality breakdown (`internal/api/dashboard.go`) | Rad, Son |
| A title's files on disk, playing a video in the browser | **Partial** | `GET /api/movies/{id}/files` and `GET /api/series/{id}/files` list the title's folder (relative path, size, date, kind: video, subtitle, image, nfo, other); `GET /api/files/stream?movie={id}&path=...` (or `series=`) sends a video file as it is, with seeking, for a `<video>` player, and previews pictures (jpg, png, webp, gif, with their image type) and text files (nfo, srt, ass, ssa, vtt, txt, up to 2 MB, always as `text/plain`); anything else, html, svg and scripts included, is refused with 415, and every file is sent with `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox` and `Content-Disposition: inline`. Only files inside the movies and TV folders, never outside them (no `../`, no symlinks leading out). No transcoding: MP4 and WebM play in every browser, MKV only where the browser supports it (`internal/mediafiles`) | Plex, Jellyfin, Emby (with transcoding) |
| Discover, recommendations | **Yes** | `/api/discover/*`, "More like your library", browse by genre, year and sort order (`/api/discover/browse`, `/api/discover/genres`), pageable trending, popular and coming-soon rails for movies and shows (`/api/discover/list`) (`pages/Discover.tsx`) | Overseerr / Jellyseerr-style, partly Rad, Son |
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
| Pause / resume queue or item | **No** | queue endpoints are delete, retry, blocklist, resolve-conflict only (`handlers_queue.go`) | Sab, qB |
| Speed limit, scheduling | **No** | no setting or code | Sab, qB |
| Queue ordering / priority per item | **No** | no priority column in `download_queue` | Sab, qB |
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
| Seeding limits (ratio, time) | **Yes, global only** | `torrent.seed_ratio_limit`, `torrent.seed_time_limit_h`, `EnforceSeedLimits` (drops the torrent) | qB |
| Categories and per-category save paths | **No** | deliberately not built so far | qB |
| Speed limits (global, per torrent, alternative) | **No** | not found | qB |
| Queueing / max active torrents | **No** | each grab starts its own client, no queue limit (`pipeline.go` comment: "A dedicated torrentclient.Client is spun up per grab") | qB |
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
| Offer, not auto-fetch | **Yes** | `subtitles.auto_download` is off unless turned on; a health item offers the missing ones, the Wanted page fetches them on request (`POST /api/subtitles/get`) | Baz (always automatic) |
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

### 2.5 Users, requests and the wider ecosystem

| Feature | In Mediarium today | Evidence | Where it exists |
|---|---|---|---|
| Multiple users and roles | **Partly** | admin and member roles, accounts managed by an administrator (`/api/users`); one route table decides who may call what (`internal/api/server.go`, `access.go`); no read-only role, no per-user libraries or quotas | Overseerr, Jellyseerr (users, roles); *arr apps are single-login |
| Request portal (family asks, admin approves) | **No** | not found | Overseerr, Jellyseerr |
| Music (Lidarr), books (Readarr), adult (Whisparr) | **No** | movies and TV only (music and books are planned for a later phase) | Lidarr, Readarr, Whisparr |
| Play-state stats | **No** | | Tautulli |
| Archive extraction helper for other apps | **n/a** (built in) | | Unpackerr |
| Sync TRaSH profiles and custom formats | **No** | no custom-format import | Recyclarr |
| Notification hub | **Partial** | built-in senders, no Notifiarr integration | Notifiarr |
| Auto hunt for missing / upgrades | **Yes** (built into automation) | `hunt` job | Huntarr (as I understand that project) |
| Clean stalled / failed queue items | **Partial** | failed downloads are blocklisted and retried; stalled ones are not detected. Working folders are removed after import, torrent data once its seeding goal is met, and a daily clean-up (`GET/POST /api/system/cleanup`) removes orphaned, leftover and empty folders in the downloads working folder (never the library) and prunes history older than `cleanup.history_retention_days` (see [downloads.md](./downloads.md#clean-up-system--clean-up)) | Cleanuparr (as I understand that project) |
| Library cleanup rules (unwatched, old) | **No** | | Maintainerr |

## 3. What to add next

Effort: **S** = days or less, **M** = about 1 to 2 weeks, **L** = weeks. Priority reasons are about making a public v1 credible, not about copying everything.

### P0: fix before anyone else runs it

| # | Item | Effort | Why |
|---|---|---|---|
| 1 | ~~Make RAR unpack work in the image~~ **Done:** RAR is unpacked natively in Go; the health check now notes a missing `7z` (info, `.7z` only) and a missing `par2` | S | Most Usenet releases are RAR |
| 2 | Fix hardlink defaults: ship a compose file and wizard default with one `/data` mount, and make the "same drive" check try a real `os.Link` probe instead of comparing device numbers | S | Separate mounts silently copy; the dashboard can say everything is fine |
| 3 | Windows: treat `ERROR_NOT_SAME_DEVICE` like `EXDEV` (copy fallback), add a Windows same-drive check | S | Cross-drive imports currently error out on Windows |
| 4 | In-app backup and restore (SQLite `VACUUM INTO` plus `secret.key`, download, restore on start) **done at the API level**; still open: scheduled backups | M | `/config` copy while running can corrupt; the selling point is "one folder", so make it one click |
| 5 | Download queue control: pause/resume, remove-and-keep-files, speed limit, item priority (Usenet and torrent) | M | Baseline expectation coming from SABnzbd or qBittorrent |
| 6 | Manual import / manual match UI for releases that fail matching | M to L | Named in the original plan; without it failures are dead ends |
| 7 | ~~Edit indexers~~ **Done** (`PUT /api/indexers/{id}`); still open: per-indexer priority, per-indexer RSS toggle | S to M | Priorities decide which indexer wins a tie |
| 8 | ~~A real definitions engine~~ **Done:** torrent sites can be added from the community definition list without Prowlarr or Jackett (see [indexers.md](./indexers.md)); still open: proving it against more real sites, CAPTCHA logins | L | Without it torrent users needed Prowlarr or Jackett, which defeats the "no other containers" pitch |

### P1: needed to compete with *arr for daily use

| # | Item | Effort |
|---|---|---|
| 9 | Torrent management page (list, pause, speed limits, queue limit, stall detection, seeding view); reuse one long-lived client instead of one per grab | L |
| 10 | Custom formats with scoring (regex, release group, HDR, audio) plus TRaSH import | L |
| 11 | Delay profiles (prefer one protocol, wait N minutes) | M |
| 12 | Multiple root folders per media type | M |
| 13 | Tags (indexers, notifications, titles) | M |
| 14 | Recycle bin and file-permission (chmod) options | S |
| 15 | Import lists that sync on a schedule (Trakt, TMDB lists, Plex watchlist) plus exclusions | M |
| 16 | More subtitle providers and per-language profiles | M to L |
| 17 | ~~Multi-user with roles~~ **Done:** admin and member accounts (see [accounts.md](./accounts.md)); still open: a read-only role | M |
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
| 25 | Windows and macOS installers that are actually tested and signed | M |

### P3: later

| # | Item | Effort |
|---|---|---|
| 26 | Request portal (needs #17) | L |
| 27 | Play-state sync and stats from Plex, Jellyfin, Emby | L |
| 28 | Library cleanup rules | M |

## 4. Optional modules for later

Each is a separate module the schema could grow into (the original plan already sketches `/music` and `/books` as new top-level folders). Names below are projects to **study, not copy from**; check each one's license before reusing any code, and confirm each still exists before you rely on it.

| Module | What it would do | Projects to study |
|---|---|---|
| Music | MusicBrainz metadata, release matching, per-track naming and tagging, quality profiles for FLAC/MP3 | Lidarr (the *arr way), MusicBrainz and MusicBrainz Picard (metadata and tagging), beets (tagging and organizing library tool) |
| Books and audiobooks | Author/series tracking, ebook and audiobook formats, metadata from Open Library or similar | Readarr (same family), Calibre-Web (library), Audiobookshelf (audiobooks and podcasts server) |
| Comics | Volume/issue tracking with a comics metadata source | Mylar3, Kapowarr |
| Podcasts | Subscribe to feeds, download episodes, retention rules | Audiobookshelf also handles podcasts |
| Anime specifics | Absolute numbering, AniDB or similar IDs, release-group preferences, dual audio | Sonarr's anime handling, Shoko |
| Request portal | Users ask, admins approve, requests turn into monitored titles; builds on the member accounts that now exist | Overseerr, Jellyseerr |
| Watch-status sync | Import play state and ratings from Plex, Jellyfin or Emby, sync to Trakt, drive "watched" cleanup | Tautulli (Plex), Jellystat (Jellyfin), Trakt's own API |
| Library cleanup | Rules such as "unwatched for a year" with a dry-run and undo | Maintainerr |
| Stats | Downloads per month, storage over time, indexer success rates | Tautulli for the presentation ideas |
| Notifications | Already partly built; a bridge to Apprise-style URLs would cover hundreds of services | Apprise (used by other self-hosted apps), Notifiarr |

## 5. Places where the README, earlier plans or CHANGELOG do not match the code

- **Download staging:** earlier plans described staging finished downloads in `/downloads/complete/movies` and `/tv`. The pipeline downloads into `downloads/incomplete/queue-N` and imports straight from there; `complete/` is created but not used for staging (`cmd/app/main.go` creates it; nothing else in `internal/` references `DownloadsComplete`).
- **Metrics endpoint:** planned as `/metrics`, it is actually `GET /api/metrics` behind authentication.
- **README "Hardlinks":** correct that one parent mapping is needed, but the shipped `docker-compose.yml`, `docs/synology.md`, `docs/qnap.md`, `docs/linux.md` and the previous Unraid template used four separate mounts, which copy. See [INSTALL.md](./INSTALL.md#folder-layout-the-most-important-decision). (Guides now carry a correction note; `docker-compose.yml` and the README were not edited here.)
- **README backup section:** the manual method ("stop the container (or at least pause activity)") is still valid, but there is no pause control in the app (see P0 #5), so stop the container; the README now also describes the built-in backup and restore.
