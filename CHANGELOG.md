# Changelog

All notable user-facing changes to Mediarium are recorded here, following
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) conventions, and
version numbers follow [Semantic Versioning](https://semver.org/). Each
release has its own dated section; changes not released yet collect under
[Unreleased]. How releases are cut: [docs/RELEASING.md](docs/RELEASING.md).

## [Unreleased]

## [1.1.0] - 2026-09-29

The version to test: fewer pages, family accounts, media servers, clean-up, quality fallbacks and many fixes from live use.

### Added
- Adding a media server without copying keys: **Find my media servers** searches your network (type your network range if Mediarium runs in Docker), **Sign in with Plex** gets the token from plex.tv and lists your Plex servers, **Quick Connect** links Jellyfin by approving a code, and Jellyfin or Emby can be linked by signing in once with an administrator account (the password is not stored). Entering the address and key by hand is still there.
- **Find and sign in to media servers** (administrators, from Settings → Connections → Media Servers). `POST /api/media-servers/discover` (`{subnets?}`) finds Plex, Jellyfin and Emby on the local network in about ten seconds: Jellyfin/Emby broadcast discovery (UDP 7359) and Plex GDM (UDP 32414), plus a check of ports 32400 and 8096 on Mediarium's own networks and common home ones (`192.168.0.x`, `192.168.1.x`, `10.0.0.x`, `10.0.1.x`, `172.16.0.x`) or up to 8 networks you give, each at most a `/24`. Only private addresses are ever contacted; public networks are refused. It answers `{found: [{kind, name, address, version, id, alreadyAdded, via}], scanned, note}`. **Sign in with Plex** uses plex.tv's PIN flow (`POST /api/media-servers/plex/pin`, poll `GET /api/media-servers/plex/pin/{pinId}` for the account's servers, `POST /api/media-servers/plex/pin/{pinId}/add` to add one with its own access token, trying local addresses first). **Jellyfin Quick Connect** (`POST /api/media-servers/jellyfin/quickconnect`, poll `GET /api/media-servers/jellyfin/quickconnect/{id}`) and **username and password** for Jellyfin and Emby (`POST /api/media-servers/login`) save the server with the access token it gives; the password is never stored or logged, and the account must be an administrator. Account tokens and Quick Connect secrets stay in memory on the server during the sign-in and are never sent to the browser. Signing in to a server that is already added replaces its token instead of adding it twice. New setting `mediaservers.client_id` (this install's device id, made automatically). See [docs/media-servers.md](docs/media-servers.md#find-my-media-servers).
- **Activity per title**: `GET /api/movies/{id}/events` and `GET /api/series/{id}/events` (every account) list what happened to a movie or show, newest first, at most 200: `[{at, kind, message, level}]` with `level` `info`, `warn` or `error`. Each automatic search says how many releases it found, how many were acceptable and why not ("12 releases, none acceptable: 8 CAM/TeleSync, 4 wrong year"), and what it picked, from which indexer, with which profile (fallback included); then the download starting and finishing, PAR2 repair and unpacking, the import or the failure with its reason, blocklisting, retries and removal from the download list. A search with the same outcome as a recent one is not repeated. These detailed events stay on the title's own log; the global activity feed is unchanged. After a bad release is blocklisted and no other release is acceptable, the log says "No other acceptable release; will try again at the next scheduled search". See [docs/activity.md](docs/activity.md).
- **Retry without downloading again**: retrying a failed Usenet download whose files were all downloaded and are still in its working folder repeats PAR2 verify/repair, unpacking and the import on those files; it downloads again only when they are gone or fail the PAR2 check.
- **Fallback profiles**: a quality profile can name other profiles to try, in order, when an automatic search finds nothing it accepts for a title with nothing downloaded yet (`fallback`, an ordered list of profile ids, in `GET/POST /api/quality-profiles` and `PUT /api/quality-profiles/{id}`; empty for every profile until you opt in; unknown ids and the profile itself are ignored, leaving it out of an update keeps the saved list, and deleting a profile removes it from every list). The title keeps its own profile: a file that came from a fallback is always searched for a replacement, even with upgrades off, and the first release the title's own profile accepts replaces it. The activity list says when a fallback was used, and interactive search results carry `acceptedBy` (`{profileId, profileName, fallback}`) and explain "allowed only as a fallback (...)". For example, 1080p with **Cinema recordings** as its fallback gets a TeleSync copy of a film still in cinemas and then replaces it with the first 1080p release. See [docs/quality-profiles.md](docs/quality-profiles.md#fallback-profiles).
- The file browser can preview pictures and text files: `GET /api/files/stream` now also serves jpg, jpeg, png, webp and gif images with their image type, and nfo, srt, ass, ssa, vtt and txt files (up to 2 MB, larger ones get 413) as plain text. Every file is sent with `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox` and `Content-Disposition: inline`; html, svg, scripts and anything else are still refused (415).
- A fifth built-in quality preset, **Cinema recordings**, which accepts only CAM/TeleSync copies (cinema recordings and screeners). It is listed first, lowest. Existing installs get it created once on the next start; their other presets are left as they are, and a preset deleted earlier is not brought back. See [docs/quality-profiles.md](docs/quality-profiles.md).
- **Settings → Media Servers**: add Plex, Jellyfin or Emby (address and token or API key, tested before adding, optional folder mapping and browser address). Each server has Refresh after every download (on by default), Refresh now, Test, Open, Edit, Disable and Remove. Movie and show pages show **Watch in Plex / Jellyfin / Emby** once the server has the title, and the sidebar links to each server.
- Family accounts: an administrator can add **basic user** accounts (`GET/POST /api/users`, `PUT/DELETE /api/users/{id}`). Members can discover, search, add movies and shows (which starts their downloads) and follow the queue, but cannot see or change settings, indexers, servers, the VPN, notifications, backups or accounts; those routes answer them with 403. Administrators can change an account's role, set a new password for it (signing it out) or delete it; there is always at least one administrator. Accounts report `role` (`admin` or `member`) and when they last signed in. Every existing account stays an administrator. See [docs/accounts.md](docs/accounts.md).
- Settings > **Profile & Accounts** (formerly Profile) lists every account for administrators, side by side, with its role and when it last signed in, and has **Add an account** (username, name, optional email, password and a Basic user or Administrator choice), **Edit** (name, email, role, a new password) and **Remove**.
- Movies and shows record who added them (`addedBy`), and the activity list says so ("The Matrix added to library by Sam").
- Discover can browse the whole TMDB catalogue by genre, year or range of years, sorted by popularity, rating, newest or oldest (`GET /api/discover/browse`, genre list at `GET /api/discover/genres`); titles already in the library are marked.
- Library movies and shows carry their TMDB genres (`genres` in `/api/movies` and `/api/series`), for filtering the library by genre. Titles added before this are filled in gradually in the background.
- Discover shows **All**, **Movies** or **TV shows**: movies first (trending, popular, coming soon), then shows. Every section fills whole rows for your screen size and has **Load more** to add rows in place, and **Browse all** opens the list with pages. Filtered results (for example Movies + Comedy) are split into pages with Previous, page numbers and Next, centred under the list; changing page, or opening another page of the app, starts at the top. No totals are shown. Coming-soon titles show their date and can be added early.
- `GET /api/discover/list?list=trending|popular|upcoming&kind=movie|tv&page=N` pages through Discover's rails (20 titles a page, up to page 500), answering `{page, totalPages, totalResults, results}` with the same items as `/api/discover/browse`. "Upcoming" lists movies released, or shows first airing, from today on, most popular first, worldwide rather than for one country. Pages are kept for 10 minutes so scrolling back does not ask TMDB again.
- Discover items include `releaseDate` (the release or first air date, `YYYY-MM-DD`, left out when unknown), and `/api/discover/browse` also reports `totalResults`.
- See a downloaded title's files and play a video in the browser. `GET /api/movies/{id}/files` and `GET /api/series/{id}/files` answer `{folder, files: [{path, size, modified, kind, main, episodeId}]}` (paths relative to the title's folder; `kind` is video, subtitle, image, nfo or other; `main` marks the files the library tracks, `episodeId` which episode a file holds). `GET /api/files/stream?movie={id}&path=...` (or `series={id}`) sends a video file with seeking support, no transcoding. Only files inside the movies and TV folders can be read; `../` paths and symlinks leading out are refused. Members can use both.
- A loading screen with the Mediarium mark while the app starts.
- A **Files** card on a downloaded movie's page (beside Subtitles) and **Files on disk** on a show's page list what is in the title's folder, with a **Play** button that plays a video in the browser when the browser supports its format.
- Saved indexers have an **Edit** button (name, address, API key) and show their test result on its own line.
- A security policy (`SECURITY.md`), Dependabot updates, CodeQL scanning, and `govulncheck`/`npm audit` in CI.
- Media servers: connect Plex, Jellyfin or Emby (address, token or API key, optional public address and path mapping; `GET/POST /api/media-servers`, `PUT/DELETE /api/media-servers/{id}`, a blank token keeps the saved one, which is stored encrypted and never returned, only `hasToken`). **Test** works before saving (`POST /api/media-servers/test`) or on a saved server (`POST /api/media-servers/{id}/test`) and explains a wrong token, an unreachable address or the wrong kind of server. After each import the server rescans just the new title's folder (Plex partial scan, Jellyfin/Emby folder update), collected for about 15 seconds so a season pack causes one scan per folder; on by default per server (`refreshAfterImport`), in the background, never holding up the import. **Refresh now** scans every library (`POST /api/media-servers/{id}/refresh`). `GET /api/media-servers/links?tmdbId=&kind=movie|tv` lists "Watch in" links for the servers that have a title (found by TMDB id, remembered for a few minutes), and without `tmdbId` each server's home page; every account can use it. The dashboard shows a server whose last test or refresh failed. See [docs/media-servers.md](docs/media-servers.md).
- **Clean-up of the downloads folder.** A download's working folder is deleted after every import, including one finished later from a parked conflict (either choice); torrent data is kept while seeding and deleted once the seeding goal is met. A daily automatic clean-up (on by default, `cleanup.auto`) removes what is left in the working folder: folders of imported downloads that are not seeding, folders and files no download owns and failed downloads' folders once they are a day old, and empty folders. It only ever works inside `<downloads>/incomplete`: nothing in the movie or TV library is deleted, symbolic links are removed as links and never followed, and paths leading outside are refused. It also drops finished downloads and activity older than `cleanup.history_retention_days` (90 by default, 0 keeps them forever). `GET /api/system/cleanup` lists what can go (`{reclaimableBytes, items: [{path, sizeBytes, reason}], lastRunAt, auto, historyRetentionDays}`, reason `orphaned`, `imported-leftover`, `seeding-finished` or `empty-folder`) and `POST /api/system/cleanup` runs it now; administrators only. Settings: `cleanupAuto`, `historyRetentionDays`. See [docs/downloads.md](docs/downloads.md#clean-up-system--clean-up).

### Changed
- Activity: the Queue shows only what still needs attention (needs a decision, in progress, failed) and counts only those; completed downloads move to **History** under Recently downloaded, with Clear finished.
- A title waiting for a release says why: the movie page shows the last search in plain words ("100 releases, none acceptable: 98 other films or years, 2 CAM/TeleSync") with a link to set fallback qualities, and Activity lists the titles waiting for a release under the queue. Search summaries now tell other films apart from other years of the same film (`lastSearch`/`lastSearchAt` on `/api/wanted` items).
- Removing a movie or show leaves no ghost files. Its downloads are always cancelled (a running Usenet download stops, a torrent stops), their working folders deleted (failed downloads' leftovers too) and their queue entries removed, so a title that is downloading can now be removed. With `deleteFiles=true` the title's whole folder in the library goes (a show's with every season, subtitle, `.nfo` and artwork); a file loose in the library folder, or in a folder shared with other titles, goes with only the files named after it. The library folder itself is never removed, and a file outside the library folder or behind a symbolic link leading out of it is refused (409) with nothing changed. See [docs/downloads.md](docs/downloads.md#removing-a-movie-or-show).
- Fewer pages: **Upcoming** combines Calendar and Wanted as two tabs. Settings has 7 entries instead of 12, related pages sharing one entry with tabs at the top: Library & Quality (Folders & Naming, Quality), Indexers, Downloads & VPN, Lists & Subtitles, Connections (Media Servers, Notifications), Profile & Accounts, System & About. Old addresses still work.
- Finishing onboarding with only Usenet set up selects Usenet only and switches the torrent client off (only torrent indexers selects torrents only). On Downloads & VPN the unused side is blurred, with one-click buttons to use both or switch.
- The torrent engine listens on **port 58264 (TCP and UDP)** by default instead of a random port, and all torrents share one engine, so one published port serves them all. The compose files, the Unraid template and the install guides publish `58264:58264/tcp` and `58264:58264/udp` (optional: torrents work without it, but other peers can then connect to you, which finds more peers and lets you seed). The port stays changeable in Settings > Downloads & VPN (`torrent.listen_port`); a port you saved is kept, and an empty or `0` value now means 58264. `GET /api/downloads/status` also reports `torrent.listening`, `torrent.activePort`, `torrent.activeTorrents`, `torrent.incomingSeen` and `torrent.lastIncomingAt`, so the page can show whether peers can reach you. See [docs/downloads.md](docs/downloads.md).
- One download at a time per movie or episode: a grab by hand (release list, search page or **Retry**) while one is queued, downloading or post-processing is refused with 409 "Already downloading: <release>. Cancel it first to pick another."; a running season pack counts for every episode of its season. See [docs/activity.md](docs/activity.md#one-download-per-title).
- Editing an indexer (`PUT /api/indexers/{id}`) can also switch a Newznab/Torznab indexer between Usenet and torrent (`protocol`); every field is optional and a blank API key keeps the saved one. Indexers report `hasApiKey` (whether a key is saved) and never the key itself.
- Editing a Usenet server (`PUT /api/usenet-servers/{id}`) changes only the fields sent: a request with just a new name no longer turns the server off or resets its other settings. A blank or missing password still keeps the saved one.
- Quality presets are strict: **1080p** only takes 1080p and **4K & over** only takes 4K (no quietly falling back to a lower resolution). Unedited presets are updated automatically. Profiles are listed from lowest to best, with Any last.
- The sidebar has more room between items, a softer highlight, and a pastel colour per settings page. "Downloads" settings is now called **Downloaders**.
- The profile menu opens above the page instead of behind it.
- Basic users only see what they can use: under Settings just their own profile and About & Credits (other settings addresses take them to their profile), no Import, Remove or quality-profile controls in the Library, no Remove, Clear finished, Blocklist or conflict buttons in Activity, and a dashboard without setup warnings or links into settings. The profile menu reads **Your profile** for them and **Profile & Accounts** for administrators.
- Filter menus on Discover and Library open below the field in the app's own style (on macOS the system menu used to cover the field).
- About & Credits uses the full width: credits in columns, with Legal and responsible use as its own row underneath.
- Indexer test failures are explained in plain words (address not found, connection refused, no answer in time, certificate problem).

### Fixed
- **Security:** release lists (search and Choose a release) sent each release's full download link to the browser, and a Usenet indexer's link includes the indexer's API key, so any signed-in account could read it. Results now carry an opaque reference (`rel_…`) that only the server turns back into the link.
- A movie search could grab a release of a **different film** from the same year whose name shared words with it (the title search also returns those). Only releases whose name matches the movie are grabbed now; a leading The/A/An and "&" versus "and" are ignored when comparing.
- The Clean up card's message showed 0 bytes freed.
- Automatic searches could grab a season pack and then the same season's single episodes as well, when two searches (the scheduled search, the new-releases check, the retry after a bad release, the search after adding a show) ran at the same time. A grab now marks every episode it will deliver as downloading at the moment it is queued, and an automatic search never grabs an episode or movie that is already being downloaded. Single episodes are only tried after a season pack has failed and been blocklisted.
- A failed upgrade download no longer shows the movie or episode as missing: it stays downloaded with its existing file.
- Finished torrents really seed now: they upload to other peers until the seed ratio or seed time limit is reached. With no limits set they used to stop straight after downloading.
- Unpacking failed for Usenet releases whose RAR parts are numbered inconsistently (part01 … part09, then part010, part011): the parts are now renamed to one numbering before unpacking.
- Cinema recordings and screeners (TeleSync, HDTS, HDCAM, Telecine, screeners, R5) were accepted as 1080p when their name said 1080p. They are now their own **CAM/TeleSync** quality, which no preset accepts.
- Removing a title from the library asks in the app's own dialog, with **Also delete everything on disk** ticked by default. All confirmations (removing profiles, indexers, servers, notification targets, clearing the blocklist, restoring a backup) now use in-app dialogs instead of the browser's pop-up.
- A poster lifting on hover in the dashboard's Recently added row no longer loses its top border.
- On tablets and small windows (under 900px wide) pages could look empty: the hidden menu still took up the whole screen height and pushed the page below it.
- If the server is restarting (for example after an update), the loading screen now says "Reconnecting to Mediarium" and retries by itself instead of hanging; a restart no longer looks like being signed out.
- Phone layouts: settings folder fields sit under their labels, choice buttons wrap, the quality profile table shows one card per profile, toolbar filters share each row evenly, and the dashboard tiles never leave one tile alone on a row.
- The Add dialog (and the other dialogs) could open far down the page, out of sight, leaving only a dark overlay; dialogs now always appear in the middle of the screen.
- The Activity page works for basic users (it used to fail because it also asked for the blocklist, which only administrators may see).
- Discover no longer redraws every poster every few seconds.
- Mediarium removes any service worker another app left behind on the same address, which could show stale data.
- For member accounts the dashboard and `/api/health` leave out setup warnings and server folder paths.

### Security
- A member account can no longer make the server fetch an arbitrary address through a grab. `POST /api/search/grab`, `POST /api/movies/{id}/grab` and `POST /api/series/{id}/grab` accept a member's `downloadUrl` only when this server returned it in search results in the last 24 hours, or when it is on the host of a configured indexer; anything else is answered with 403 ("search again and grab from the results"). Administrators are not limited, since they can point an indexer at any address anyway.
- Built with Go 1.26.8 (standard-library security fixes); building from source now needs Go 1.26.8 or newer. Updated the torrent library's WebRTC/WebSocket dependencies (`gorilla/websocket` 1.5.3, `pion/dtls` 3.1.4, `pion/stun` 3.1.5) and OpenTelemetry (1.46.0) to versions without known vulnerabilities.

## [1.0.0] - 2026-09-28

The first numbered release.

### Changed
- The default port is now **8264** instead of 8080, which many other apps use. Set `APP_PORT` to change what the app listens on, and `HOST_PORT` in `.env` to change the port on your machine when using the compose file.
- Four clearer built-in quality profiles replace the old three: **Any**, **720p**, **1080p** (the new default) and **4K & over**. "1080p" takes 720p WEB and Bluray only as a fallback and no longer takes 1080p remuxes (very large files); "Any" stops upgrading at Bluray-1080p. Existing installs are upgraded once on start: an old preset you never changed becomes its new equivalent in place ("Up to 1080p" becomes "1080p", "Ultra-HD (up to 2160p)" becomes "4K & over"), so titles and the default keep pointing at it; a preset you edited is left alone. See [docs/quality-profiles.md](docs/quality-profiles.md).
- Docker images are tagged with the plain version number (`1.0.0`, `1.0`, `latest`) instead of `v1.0.0`, and an image built from source (`docker build`, `docker compose build`) reports the number in the `VERSION` file instead of "dev" (a plain `go build` or `go run` still says "dev").

### Added
- A `VERSION` file holds the release number, and [docs/RELEASING.md](docs/RELEASING.md) explains what the numbers mean and how a release is cut. A release fails if its git tag does not match `VERSION`.
- Torrent sites without an API can be added directly, without Prowlarr or Jackett: Settings, Indexers, Add a site lists the community-maintained site definitions (downloaded to your server when you open the list; none ship with Mediarium). Public sites work as they are; private sites sign in with your username and password, API key or browser cookie, and sign in again by themselves when the site logs them out. Passwords, cookies and keys are stored encrypted. See [docs/indexers.md](docs/indexers.md).
- Optional FlareSolverr support for sites behind a Cloudflare check: set its address in Settings (`flareSolverrUrl`). Without it such sites fail with a message saying so.
- Indexers can be edited (`PUT /api/indexers/{id}`); leaving a secret blank keeps the saved one. A definition-based site whose last test failed shows on the dashboard.
- One indexer form for both kinds: choose Usenet or torrent, pick the indexer (or Other), paste the API key and add as many as you like, in the setup wizard and in Settings, Indexers.
- The setup wizard shows the services that come with Mediarium (TMDB, OpenSubtitles, Trakt) as Connected, with their shared limits and where to add your own key.
- All three shared keys can be replaced with your own free key in Settings if you reach a limit; the dashboard warns when a shared key is refusing requests.
- Subtitles that come with a release are now used: `.srt`, `.ass`, `.ssa`, `.sub`/`.idx` and
  `.sup` files in a finished download are copied next to the movie or episode
  as `Name.en.srt` (language read from the file or folder name; episodes in a season
  pack matched by season and episode). Nothing in the download folder is changed
  or deleted, and an existing file is never overwritten.
- Subtitles are offered instead of fetched by default: Mediarium says how many
  titles have none and lets you get them from the Wanted page. Automatic download
  is now something you turn on. You can also mark a title "no subtitles wanted",
  and a warning tells you when more subtitles are wanted than OpenSubtitles' daily
  download limit allows (about 5 a day, about 20 with a free account), with how
  many days that will take.
- The dashboard warns when the shared Trakt or OpenSubtitles key that comes with
  Mediarium is at its limit, and points to using your own free key (which you can
  also clear again to go back to the shared one).
- The setup wizard has a Back button on every step after the account, hides services that ship with Mediarium, and, when the OpenSubtitles key is included, offers an optional personal account for higher subtitle download limits.
- RAR archives (single, multi-part and old-style volumes) now unpack natively, so
  the Docker image no longer needs a RAR-capable 7z. ZIP is native too; only 7z
  archives still use the 7z program.
- Backup and restore from Settings, System & Backup: download one file with your
  library, accounts and settings, and restore it later (the app restarts to load it).
- New app layout: a fixed, collapsible sidebar whose selected item joins the page,
  Settings pages listed under Settings, page title, centred search and profile in
  the header, and a redesigned sign-in page with the logo.
- Small animations throughout (page fade-in, staggered lists, button presses),
  switched off when the device asks for reduced motion.
- Reference docs generated from the code (`go run ./tools/docgen`) and checked in CI.
- A single optional Name on your profile replaces first name and surname.
- Design refresh: every section has its own colour drawn from the brand teal
  (movies teal, shows violet), a bento dashboard that greets you by name, a
  full-width calendar (dashboard and its own page), framed settings groups, and
  a header with theme, profile and sign out at the top right.
- Title pages with poster, rating, age rating, genres, summary, trailer links and
  cast; one clear action per state. Genres and ratings on Discover cards.
- One state vocabulary (Downloaded, Downloading %, Searching, Pending, Added,
  Failed, Partial) across Library, Wanted, Discover, search and title pages, with
  live download progress on library cards.
- Profile (name, surname, email, username, password), asked for in the wizard.
- Notifications: email (SMTP), ntfy, Gotify, Pushover, Slack, generic webhook
  next to Discord and Telegram, per-target event choice, test button, and a
  connection watch that notifies when your Usenet provider or an indexer stops
  working.
- Torrent client on/off switch (Usenet-only installs), and a demo-data mode with
  sample titles in every state.
- Legal notice acceptance in the wizard, plus docs: INSTALL, FEATURES, LEGAL and
  packaging for Windows, macOS, Linux, Unraid, CasaOS, Portainer and TrueNAS.
- Health warnings for a missing 7z or par2.
- Search is live as you type and looks up movies and shows first (TMDB), like
  Radarr/Sonarr. "Add" asks for quality profile, monitoring, and whether to use
  Usenet, torrents or both (or your defaults), and can search straight away.
- New dashboard: needs-attention warnings (what will not work and why, with a
  fix button), animated stats, live downloads, connected folder health with
  disk usage, recently added/downloaded, coming up, quality breakdown.
- Activity page with retry, blocklist and search again, remove, and clear
  finished; history grouped and readable.
- Built-in app-wide TMDB, OpenSubtitles and Trakt keys for official builds, and
  a "Connect services" step in the wizard that explains each service.
- Live folder checks (exists, writable, mapped, free space) in the wizard,
  Settings and dashboard.
- Toast notifications and autosaving switches; settings no longer appear to
  revert when you leave and return.
- Poster placeholders with the logo, equal-height card grids, mobile-friendly
  layouts across the app.

### Fixed
- Overwriting an existing library file is now atomic (never removes first).
- Docker entrypoint no longer recursively changes ownership of your media
  folders, and warns when a folder is not writable.
- Finished Usenet downloads clean up their own working folder.
- Settings is split into sub-pages by topic (Media Management, Quality,
  Indexers, Downloads, VPN, Subtitles, Metadata & Lists, Notifications,
  General & Security).
- Clearer downloads: Usenet servers (your provider's news-server account,
  with a provider shortcut) replace "download clients", torrents need no
  setup, and a second server can be added as a backup for missing articles.
- Missing Usenet articles are repaired with PAR2 instead of failing the
  download.
- Subtitles: choose the languages you want, and Mediarium downloads them
  automatically after each import and for anything missing (best fit for
  the release first), on movie pages, per episode, and from a Subtitles tab
  on the Wanted page.
- Test buttons for indexers and Usenet servers (before and after saving),
  and an enable/disable switch for each indexer.
- Quality profiles can require or exclude words in release titles and
  prefer terms with a score.
- Wanted page (missing items and items below their quality cutoff) with a
  Search now button on every row; Search now and Interactive search on the
  movie page, with reasons shown for releases that automation would skip.
- Monitor / unmonitor movies, shows, seasons and single episodes.
- Blocklist and automatic retry: a release that fails because it is bad
  (missing articles, failed repair, nothing to import) is blocklisted and
  the next-best release is tried automatically.
- Quality profiles: create and edit your own (accepted qualities, cutoff,
  whether to keep upgrading), choose a default, and give any movie or show
  its own profile from its page.
- Import an existing library: scan a folder of movies or TV, review the
  TMDB matches (fix any with a manual search), and register everything in
  place without moving or renaming files.
- Remove a movie or show from the library, optionally deleting its files.
- TV support: add shows from TMDB, see every season and episode with
  status, search a season or an episode across your indexers and grab a
  release (single episode, multi-episode, or a whole season pack). Files
  import into `Series (Year)/Season NN/`. Movies and TV share the
  Library, Discover (trending/popular shows), Search and Calendar pages.
- TV automation: aired missing episodes are hunted (season pack first
  when a whole season is missing), RSS-synced, and upgraded to the quality
  cutoff; episode lists refresh from TMDB every 12 hours.
- A TV folder setting (`TV_DIR`, default `/tv`).
- Real brand identity: logo, favicon, self-hosted fonts (Sora / IBM Plex
  Sans / IBM Plex Mono), and a full color token system for both dark and
  light themes.
- VPN kill switch — torrent traffic can now be required to route through
  the VPN tunnel, with no direct-connection fallback if it drops.
- VPN egress IP check and a named-provider picker (Mullvad, ProtonVPN,
  Private Internet Access, Surfshark, NordVPN) in Settings.
- Upgrade hunting: the automation loop now re-checks downloaded movies
  against the quality profile cutoff, not just missing ones.
- Import conflict policy: "overwrite if better quality" and "always ask"
  (with a real resolve UI in Activity/Queue) alongside skip/overwrite.
- Rate-limited login, password change, API keys UI, password strength
  meter on the onboarding admin-account step.
- Light/dark theme toggle, library list view with sortable columns,
  sortable search results.
- QNAP Community Applications deployment guide.
- Multi-arch (linux/amd64 + linux/arm64) Docker build support.
- Naming preview and hardlink-detection warning in onboarding/Settings.
- Curated/public list import: paste a public Trakt list URL on Discover
  to browse and add movies from it.
- "More like your library" on Discover: recommendations aggregated across
  your recently added movies, ranked by how many of them agree, never
  suggesting something you already own.

### Changed
- Docker builds now cross-compile per target platform instead of
  emulating the whole build.
- Status badge colors follow the brand guide's semantic mapping
  (success/warning/info/danger) instead of ad-hoc choices.

### Fixed
- Multi-episode release titles (e.g. `S01E01E02E03`) only ever kept the
  last episode.
- Illegal filename character handling ("replace" mode existed in the
  organizer engine but had no setting to actually choose it).
- The configured downloads path setting was silently ignored in favor of
  an env-var-derived default.
