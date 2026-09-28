# Changelog

All notable user-facing changes to Mediarium are recorded here, following
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) conventions. This
project has not cut a tagged release yet. Once the first version-pinned
Docker image is published, each release gets its own dated section here.

## [Unreleased]

Pre-release development. No version-pinned image has been published yet;
building from source or `docker compose build` produces a "dev" build
(see `GET /api/version` / the About page).

### Added
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
