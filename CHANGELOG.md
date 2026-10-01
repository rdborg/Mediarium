# Changelog

All notable user-facing changes to Mediarium are recorded here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
version numbers follow [Semantic Versioning](https://semver.org/). Each
release has its own dated section. Changes that aren't released yet collect
under [Unreleased]. How releases are cut: [docs/RELEASING.md](docs/RELEASING.md).

## [Unreleased]

## [1.3.1] - 2026-10-01

### Fixed
- A release that can never be completed no longer wastes a whole download. If articles are missing on every Usenet server and the release has too few PAR2 repair files to rebuild them (or none at all), Mediarium stops straight away, blocklists that release and moves on to the next one. Before, it downloaded everything and only then found out it could not be repaired.
- Logs and errors groups the same problem for the same movie, show or album on one line for 24 hours, even when different downloads caused it. A title that keeps failing is one line with a count, not a page of lines. The log keeps at most 2000 problems now (it was 5000).
- Emails no longer end with "This message is about...". They end with one plain line that says where to choose which emails you get.
- On Discover and on a title's page, the rows of posters no longer leave a big empty gap before "Load more".

## [1.3.0] - 2026-09-30

First public release.

### Movies and TV
- Add a movie or a show from search or Discover. Mediarium finds the best release on your indexers, downloads it, unpacks it, names it and files it in your library.
- Shows come with their full episode list. You can monitor a whole show, a season or a single episode, and season packs and multi-episode releases are understood.
- Every title has its own activity log that says what was searched, what was skipped and why, and what happened to the download.
- The Upcoming page has a calendar of releases and episodes, and a Wanted list of what is still missing.
- Discover has trending, popular and coming-soon rows, "More like your library" with a switch for older titles, browsing by genre, year and order, and importing a public Trakt list.
- In the Library, administrators can press Select and change many titles at once: monitor, better versions, quality profile, where to download from, search now, or remove.

### Music
- Music is off until you switch it on under Settings > Media types.
- Add artists from MusicBrainz, follow them, and download their albums. Music has its own quality profiles and its own Discover page.
- Import an existing music folder without moving or renaming anything.

### Downloading and the download line
- Mediarium has its own Usenet downloader (several providers, backup providers, PAR2 repair, unpacking) and its own torrent client. There is no download program to connect.
- Downloads wait in a line. **Downloads at the same time** sets how many run together (1 to 5, one by default), and what you start yourself goes before what the automatic searches add.
- Downloads can be paused, resumed and stopped, one at a time or all at once. A paused download stays paused after a restart.
- Torrents can go through a built-in WireGuard VPN with a kill switch. It needs no special container permissions. The VPN comes back by itself after a restart, says plainly whether it is really connected, and torrents wait for it instead of going out without it.
- Settings shows whether the torrent port is open and whether another peer has reached you.
- Indexers can be Newznab or Torznab sites, or sites from the community definition list, with support for the FlareSolverr helper for Cloudflare checks. The `-full` image has the helper built in.
- A daily clean-up removes leftovers from the downloads folder. Your library is never touched. You choose how many days finished downloads and activity are kept.

### Importing an existing library
- **Import existing** finds movies, shows and music you already have and adds them without moving, renaming or deleting anything.
- Imported titles start unmonitored and set to leave your files alone, so nothing is downloaded by surprise. The report has buttons to start monitoring afterwards.
- Details fill in on the server in the background, so you can leave the page. A banner shows the progress.
- **Move from other apps** reads Radarr, Sonarr, Prowlarr, SABnzbd, NZBGet, Jackett, NZBHydra2, Overseerr or Jellyseerr, Ombi, Bazarr, Medusa and SickChill, and only reads them.

### Quality and languages
- Quality profiles say which releases are acceptable, where to stop upgrading, and which other profiles to try if nothing is found. Five profiles come built in.
- Looking for better versions after a download is off unless you switch it on.
- An audio language setting decides which language tags in release names are taken (English unless you change it).
- Subtitles are off until you switch them on under Settings > Info, lists and subtitles > Subtitles. Subtitles that come inside a download are always kept.
- File names follow a preset for Plex, Jellyfin or Emby, Kodi, a short style, or your own pattern.

### Media servers
- Connect Plex, Jellyfin or Emby. Mediarium can find servers on your network, help you sign in, and ask the server to scan just the new folder after each import (or the whole library, when the server does not know that folder).
- Title pages get a "Watch in" link to your server.

### Notifications
- Send messages by email, ntfy, Gotify, Pushover, Slack, Discord, Telegram or a webhook, and choose which events each one is told about.

### Accounts and security
- Passwords, API keys and tokens are only ever sent to the address they were saved with, stored encrypted, and removed from the log before you see or copy it.
- Downloaded archives are checked before anything is unpacked, and update files are checked against a signature before they are installed.
- The first account is the administrator. Family members can get basic accounts that can find and add titles but cannot change settings.
- Every account can make API keys. Saved passwords and keys are stored encrypted, and sign-in attempts are rate limited.
- [docs/security.md](docs/security.md) has a checklist and examples for putting Mediarium behind a reverse proxy.
- A backup and restore of your settings and database is built in (Settings > System > Server and backup).

### Updates and restart
- Mediarium checks GitHub once a day for a new version and shows a notice with what is new. You can turn the check off.
- **Update now** installs a new release in the Docker image after checking its signature. Installing overnight and pushing an update through the API are both off unless you switch them on.
- **Restart Mediarium** and **Restart with automation paused** are on the Server and backup page. Mediarium also restarts itself if it stops answering.
- Setting `MEDIARIUM_PAUSE_AUTOMATION` starts it in safe mode, with no automatic searching, downloading or refreshing.

### Logs and errors
- Settings > System > Logs and errors lists what went wrong in plain words, with what to try. Repeats are counted on one line and problems are kept for 30 days.
- **Copy for support** puts a report on your clipboard, with passwords and keys left out.

### Install and platforms
- Runs in Docker on 64-bit Linux, Synology, Unraid, QNAP and Raspberry Pi (`amd64` and `arm64`). Images are published as `ghcr.io/rdborg/mediarium` with the tags `latest`, `1.3.0` and `1.3`, and the same with `-full`.
- A first-run wizard asks what you want to manage (movies, TV, music), checks your folders live, and helps you connect your indexers, Usenet provider, media player and media server.
- Audiobooks and ebooks are not ready yet. Windows and macOS installers and one-click app store entries are still to come.

### Support the project
- A small "Support Mediarium" link sits at the bottom of the sidebar for administrators. One click hides it.

### Documentation
- Step-by-step guides for Synology, Unraid, QNAP and Linux, and pages for each part of the app, are in [docs/](docs/README.md).
- The pages for the HTTP API, environment variables, stored settings and problem codes are generated from the code.
