# Changelog

All notable user-facing changes to Mediarium are recorded here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
version numbers follow [Semantic Versioning](https://semver.org/). Each
release has its own dated section. Changes that aren't released yet collect
under [Unreleased]. How releases are cut: [docs/RELEASING.md](docs/RELEASING.md).

## [Unreleased]

## [2.0.0] - 2026-10-05

Version 2 adds ebooks and audiobooks, with a reader and player built in, and tags for movies and shows.

### Upgrading

- **More movie and TV folders are gone.** If you added extra folders under Settings > Library, the titles in them stay in your library, but Mediarium no longer renames, replaces or deletes their files there. Move them into your main movies or TV folder and use Import to pick them up. Tags (for example Kids or 4K) are the way to keep them apart now.
- For scripts using the API: adding a movie or show no longer takes a `rootPath`, and the settings `library.movies_extra_paths` and `library.tv_extra_paths` are removed.
- Nothing else needs doing. The database updates itself on the first start, and your compose file keeps working. To use books, switch them on under Settings > Media types; Mediarium suggests a folder inside the one you already map and can create it.

### Added
- Ebooks and audiobooks. Switch them on under Settings > Media types, find a book by title or author (book details come from Open Library), pick ebook, audiobook or both, and Mediarium searches your indexers, downloads the best release (EPUB first for ebooks, M4B first for audiobooks) and files it as Author/Title (Year).
- Discover's eBooks & Audiobooks tab: trending and popular books from Open Library, the most read classics, fantasy, science fiction and mysteries, and a browse by genre, year and order. A banner on each cover says whether an eBook, an audiobook or both have been published, and Add lights up when you point at a book.
- Every book has an info page (description, subjects, editions, Add, more by the author), also from the search box.
- Import the ebooks and audiobooks you already have: Mediarium reads the folder, recognises Author/Title folders (and Calibre's and Audiobookshelf's layouts), and adds them where they are.
- Mediarium Books: a built-in reader and audiobook player that opens in its own window and installs as an app of its own. Read EPUBs a page at a time (contents, text size, light, sepia and dark pages, swipe and keys), open PDFs, and listen with chapters, 30-second skips, speed, a sleep timer and lock-screen controls. Your place is saved and carries on across devices.
- A book's page shows more by its author, and you can follow an author so their new books are added by themselves.
- Connect Audiobookshelf and Kavita under Media servers: new books show up there straight away, and a book's page links to it ("Listen in Audiobookshelf", "Read in Kavita").
- The search box at the top finds books, and Upcoming > Wanted lists the books still missing, with Search now.
- Tags on movies and shows (Kids, 4K, anything): set them on a title's page, when adding, or on a whole selection; filter the Library by tag; and every tag shows up as a collection on Plex, Jellyfin and Emby. A tidy way to keep kids' films or 4K apart in one folder.
- A library folder that doesn't exist yet can be created from Settings and from the setup wizard, and when your other folders share one mapped folder (like /data) Mediarium offers to use and create /data/Ebooks, /data/Audiobooks or /data/Music for you, so no compose change is needed.

### Changed
- The header holds the small links that were at the bottom of the sidebar: the eBooks/Audiobooks Player, your media servers (named Plex, Jellyfin and so on) and, for administrators, the support heart. The search box sits next to the page title. On narrower screens the links show as icons.
- Settings > Media types has bigger cards, each with a short list of what that kind of media does.
- The dashboard's server details now sit in a slim panel inside the greeting card (the greeting takes a third, the server two thirds), instead of a separate strip.

### Removed
- More movie and TV folders. Each kind of media has one folder again, and tags sort it instead (see Upgrading above).
- The Windows and macOS programs. Releases now carry the Linux program only, next to the Docker image. On a Windows PC or a Mac, run Mediarium in Docker Desktop.

### Fixed
- The "Mediarium is slow to answer" notice no longer appears when the wait is for an outside service (TMDB, Open Library, MusicBrainz, your indexers). Those lists show their own loading placeholders, and a service that doesn't answer is named in the message.
- A book with a short title is no longer matched to a longer one that contains it ("It" picked up "If It Bleeds"): the title has to stand on its own in the release name.
- Once a book has downloaded, its earlier failed tries leave the queue, as they already did for movies.
- Ebook and Audiobook in the add-book window show a tick when chosen.
- The reader and the audiobook player no longer leave an empty strip down the right edge of the window, and the search box hint is shorter so it fits next to long page titles.

## [1.4.1] - 2026-10-04

### Fixed
- The "Mediarium is slow to answer" notice no longer appears after the phone or the tab was asleep: a request that waited while the page was out of sight does not count as slow.
- A failed download in Activity has **Why did this happen, and what can I do?**: a plain explanation of the failure (for example a release that is no longer complete on your Usenet provider) and what to try, such as another release, a quality profile that accepts more qualities, or a second Usenet provider.
- Failed tries of a movie or episode clear themselves from Activity once it has been downloaded another way.
- On the dashboard, each recently added title says where it stands: In library, Downloading, Some episodes, Waiting for a release or Not monitored.
- The dashboard's server details are a full-width strip under the greeting with a steady height, so the Movies, TV and Music cards have the whole row and the page no longer jumps as the numbers change.
- The lines under "Coming up" on the dashboard open their movie, show or album.
- In the Library, **Import** and **Add** (shorter names) sit together at the end of the toolbar row instead of Add wrapping onto its own line.

## [1.4.0] - 2026-10-04

A big release about control and safety, with everything kept simple: a recycle bin, nightly backups, speed and space limits, more ways to find and import titles, and many everyday pages reworked.

### Added
- **More than one library folder**: add extra movie and TV folders in Settings > Library, and choose which one a title goes in when you add it.
- **Rename existing files** (Settings > Library) renames files already in the library to match the naming preset, after showing every change.
- A **Statistics** page: library size, titles per quality, and downloads per month.
- Discover has **Select** to pick many titles and add them all at once, and **Not interested** (the x on a poster) to never see a title there again, with a list to undo it.
- **Quiet hours** for notifications: everyday messages wait until the morning; problems are still sent at once.
- **Search releases** (from the Search page) asks all your indexers for a release by name and downloads the one you pick; the movie or show is added to your library if needed.
- **Import a file by hand** (from Activity) lists the video files in the downloads folder with a guess of what each is, and puts the one you choose into your library as a movie or an episode.
- The series page folds its seasons (the one that needs attention stays open), has a row of season buttons to jump around, a **Missing episodes only** switch, and shows the next episode and when it airs.
- The Wanted page folds episodes of the same show into one line, has a search box, **Search all**, and shows 50 at a time.
- The calendar can be filtered (movies, TV, music, not downloaded yet), and **Add to your calendar app** gives a private address to subscribe to in Google, Apple or Outlook calendars.
- The History tab has a search box, **Show older lines**, and links each line to its title.
- The Library has a button to reverse the sort order, and remembers the sort.
- Indexers have a priority (Preferred, Normal, Last resort): between two equally good releases the preferred indexer wins. Each card shows how its last test went, and **Test all** tests them all at once.
- Releases far too small for the quality they claim (fakes and samples) are skipped by automatic searches, and a quality profile can set the **Largest download** in GB. Profiles can be duplicated.
- A torrent that gets no data for 2 hours is treated as stalled: it is stopped, blocklisted, and another release is tried.
- A download speed limit for Usenet and torrents together, optionally only between two hours (for example full speed at night), and a free-space floor: with less than 5 GB free in the downloads folder no new download starts until there is room. Both are in the new **Speed and space** box under Settings > Downloading.
- Automatic backups. Every night, and before every update, Mediarium saves a backup in `/config/backups` and keeps the newest 7. Settings > System > Backup lists them with a Download button, has **Back up now**, and lets you switch the nightly backup off or keep more.
- A recycle bin. Removing a title with its files now moves them to a hidden folder in the same library folder for 7 days. **Activity > Recycle bin** shows them, with **Put back** and **Delete now**. The number of days is under Settings > System > Clean up (0 deletes straight away, as before).

### Changed
- Notification emails have a new look: wider, with the full Mediarium logo on top, a big poster on one side, and the facts stacked on the other with bold titles. The logo travels inside the email, so nothing is loaded from the web except the poster, which is now sharper.
- You can delete an API key after you have revoked it. Revoked keys no longer pile up in the list.
- The test button on an email notification sends a real-looking email, using a title from your library, so you see the design you will get.
- When the update check fails, the Updates box now says why (for example that GitHub is limiting requests).
- An address that does not exist in the app shows a "That page doesn't exist" page with links, instead of jumping to the dashboard.
- Mediarium can be added to a phone's home screen and opens like an app.
- Finished library imports have a "Dismiss all" button when there is more than one.
- The Audiobooks and Ebooks tiles are gone from the dashboard until they are ready.
- The dashboard numbers no longer count up when your system asks for reduced motion.

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
