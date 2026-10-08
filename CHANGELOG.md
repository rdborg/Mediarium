# Changelog

All notable user-facing changes to Mediarium are recorded here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
version numbers follow [Semantic Versioning](https://semver.org/). Each
release has its own dated section. Changes that aren't released yet collect
under [Unreleased]. How releases are cut: [docs/RELEASING.md](docs/RELEASING.md).

## [Unreleased]

## [2.1.10] - 2026-10-08

### Changed
- A new film is no longer searched for automatically while it is only in cinemas. Nearly everything posted then is a recording made in the cinema, and some are named like a normal WEB-DL, so no quality profile can tell them apart. Mediarium waits for the digital or disc release date TMDB lists, or 90 days after the cinema date when TMDB has none. It looks at films from the last five months only, a film whose profile takes CAM/TeleSync (the Cinema recordings preset) is not held, and **Search now** and choosing a release by hand still work. The title's history says what it is waiting for (asked for by u/Dry_Register_5812 on Reddit).

### Fixed
- When a finished download has no video file in it, the error now says what the folder holds: how many files, how big, and the biggest names. A support report then shows whether archives were left unpacked or the files had odd names (looked into after reports from u/Wiwer on Reddit).
- A release posted with random file names and a PAR2 file that has no `.par2` ending is handled: the PAR2 file is recognised by what is in it, so the real file names come back and repair runs (looked into after reports from u/Wiwer on Reddit).
- Cinema recordings whose names end in Rip or use a dash (`HDCAMRip`, `HQCAMRip`, `HDTSRip`, `HDTCRip`, `TSRip`, `PreDVDRip`, `Tele-Sync`) were taken for a normal 1080p WEB-DL, so a profile set to WEB-DL only still downloaded them. They are now recognised as CAM/TeleSync like the others (reported by u/Dry_Register_5812 on Reddit).
- A WireGuard config whose server is a host name (Surfshark and several other providers write it that way, like `us-slc.prod.surfshark.com:51820`) failed with "The VPN server address isn't right". The name is now looked up each time the connection starts, the way `wg-quick` does, and the name stays in the settings so a provider that changes the address keeps working. A name that can't be found says so (reported by goneturbo on GitHub, #33).
- Importing from Sonarr, Radarr and the others through an address behind Cloudflare, like a Cloudflare Tunnel, ended with a bare "530". It now says that the address is behind Cloudflare, which can't reach the app, and suggests the app's address on your own network (reported by Hoopes80 on GitHub, #32).
- A torrent whose magnet link lists a UDP tracker crashed Mediarium while the VPN was on. UDP trackers can't go through the tunnel, so they are skipped now and the torrent carries on with its web (HTTP) trackers and peers (reported by u/Big_Dragonfruit9719 on Reddit).

## [2.1.9] - 2026-10-07

### Fixed
- Adding a media server by pasting the address from your browser (`https://host/web/#/home` for Jellyfin, `.../web/index.html#!/home` for Emby and Plex) failed. The part that only opens the server's web app is now dropped, so it becomes `https://host`; an address with a folder before it, like `https://host/jellyfin/web/#/home`, becomes `https://host/jellyfin` (suggested by msholly on GitHub, #29).

## [2.1.8] - 2026-10-07

### Fixed
- Torrent sites added through Prowlarr or Jackett as a **Usenet** indexer (an easy mix-up, since both kinds of feed look the same) failed with "couldn't read the NZB file". Results now say for themselves that they are torrents, from what the feed sends, so they download as torrents whichever tab the indexer was added on. If a torrent file or magnet link still reaches the Usenet downloader, the error now says to add that indexer again on the Torrent tab, and the release is no longer blocklisted for it (reported by Apostol6 on GitHub, #25).

## [2.1.7] - 2026-10-07

### Added
- **Import existing** can show just one kind of row in the review table: Unmatched, Check match, Matched or Already in library, with a count on each (asked for by PauloJf on GitHub, #24).

### Fixed
- Scanning a folder again after importing it showed titles you had matched by hand as unmatched, and never recognised shows as already imported. A scan now recognises files already in the library by their location and lists them as **Already in library** under the title they belong to (reported by PauloJf on GitHub, #24).
- The check for folders Mediarium can't write to inside a library folder no longer looks inside the filesystem root or system folders, and remembers fewer results at once.
- In the setup wizard, a site setting with a checkbox (1337x's "Disable sorting", for example) stretched the checkbox across the form and squeezed its label into a column one letter wide (reported by PauloJf on GitHub, #23).

## [2.1.6] - 2026-10-07

### Added
- Paste your WireGuard `.conf` file (or choose it) under **Settings > Downloading > VPN protection** and every field is filled in for you, including the preshared key, DNS servers and allowed IPs. Windscribe is in the provider list (asked for by PauloJf on GitHub, #21).

### Fixed
- The VPN form had no field for a WireGuard preshared key, so configs that use one (Windscribe's, some self-hosted servers) never connected. It's there now, and stored encrypted like the private key (reported by PauloJf on GitHub, #21).

## [2.1.5] - 2026-10-07

### Added
- File names understand Radarr's and Sonarr's tokens, so a format copied from either works as it is, for example `{Movie CleanTitle} ({Release Year}) - {Custom Formats}{ - Edition Tags}`. That includes text inside the braces that only shows when there's a value (`{ - Edition Tags}`, `{[Quality Full]}`), dotted names (`{Movie.CleanTitle}`), Plex's `{edition-{Edition Tags}}`, quality, media info and ids (asked for by u/Wiwer on Reddit).
- A file name builder under **Custom** naming: start from a ready-made format (including *Detailed, like Radarr and Sonarr* and *Plex with editions*) or paste your own, click tokens to add them, and see a preview of both a movie and an episode as you type. Episodes now have their own custom format.
- The preview says what's wrong with a format (an unknown token, a missing brace, no season or episode) while you type.

### Fixed
- Usenet releases posted with only PAR2 recovery volumes (`.vol01.par2` and so on) and no main `.par2` failed with "no PAR2 files to repair it" and were blocklisted, though they could be repaired. Mediarium now repairs from one of the volumes (reported by TryToTilt on GitHub, #20).

## [2.1.4] - 2026-10-07

### Fixed
- Obfuscated Usenet releases (random file names) failed PAR2 repair and were blocklisted, even when nothing was wrong with them. Mediarium now gives the files their real names back from the PAR2 data before repairing, and hands par2 every file in the download (reported by u/Wiwer on Reddit).
- A failed repair now says in one line why ("it needs 12 more recovery blocks than the release has") instead of pages of par2 progress output.
- A download is no longer started when Mediarium can't write to the folder the file will go in; the reason names the folder. Before, the file was downloaded in full and then failed to import (reported by u/Wiwer on Reddit).
- Setup, Settings and the dashboard warn about folders inside Movies and TV that Mediarium can't write to, such as ones another app created under a different user, with an example path.
- Obfuscated releases with many PAR2 files are verified once, not once per file.

## [2.1.3] - 2026-10-06

### Fixed
- A Torznab or Newznab address pasted with `/api` on the end, as Prowlarr and Jackett show it (for example `http://192.168.1.10:9696/1/api`), didn't work: Mediarium added its own `/api` and got Prowlarr's web page back. Both forms work now, and so does an address pasted with `?t=...&apikey=...` after it.

## [2.1.2] - 2026-10-06

### Fixed
- Sites from the definition list (UTOPIA, for example) that write their dates differently from what their definition expects failed every search with "could not read the results … dateparse". Such a date is now read as any common date format, as Prowlarr does.
- Adding Prowlarr's main address (or any web page) as a Torznab indexer gave a puzzling "parse newznab response … `<html>`" error. It now says the address gave back a web page, and how to copy an indexer's feed address from Prowlarr.

## [2.1.1] - 2026-10-06

### Fixed
- Outside your download hours, Activity said "Started downloading" for a grab that was in fact waiting for the hours to come round.
- Scripts after imports: a script stopped at its time limit now takes everything it started with it, and anything it leaves running in the background is stopped when it ends. **Try it** answers straight away when a run after an import is still going instead of waiting behind it.
- Subtitle timing: "Put the original back" could bring back an older, different subtitle after a new one was downloaded; and checking for the original left a file open.
- Books that aren't out yet no longer use up the automatic searches meant for missing books, a book announced for a later year is no longer searched for straight away, and a followed series keeps its books' release dates up to date when one is postponed.
- Dates such as "out on" and "unaired" use your own time zone, not UTC, so they no longer flip a day early or late in the evening.
- Audiobooks: chapters of very long books (over about 60 hours) are read again, and a damaged M4B can no longer keep the server busy.
- A Kindle book is no longer converted twice when the library folder is reached through a link.
- Kindle books: a damaged or hostile MOBI/AZW3 file could make the converter use up all memory and stop Mediarium; sizes are now capped. Books with many internal links (footnotes, big omnibus editions) convert many times faster.
- Daily shows were almost never found: the year in an air date ("Show.2024.03.15") was taken for the show's year and the release was turned down.
- Anime and daily shows: an automatic grab of one episode was queued as a whole season, which blocked the rest of the season. "Show S2 - 05" names are now read as one episode, a "12.5" recap is no longer taken for episode 12, and a site tag in front of a release name no longer replaces the real release group.
- Shows moved over from Sonarr keep Sonarr's series type, and shows from Overseerr or Jellyseerr requests get theirs guessed like any other.
- Specials: 2.1.0 let you switch a special on, but Mediarium couldn't download it, so it stayed missing and used up searches. Specials are now listed for reference only, with no Search button or Monitored box, until they can be looked for properly.
- Family accounts that have to ask could still add titles without a request: by picking a release in Search for something not in the library, or by following a book series or an author. Both now need "Add titles themselves" (the Follow switches are hidden without it).
- Requests: Approve, Decline and taking a request back no longer step on each other when clicked at the same time; two titles with the same name (Dune 1984 and 2021) are no longer treated as one request; a book request needs ebook or audiobook; a request for a media type that has since been switched off waits instead of being marked approved; and a music request for "Only this album" now adds and looks for that album when approved.
- Cleanup rules (off unless you switched them on) could remove things you still wanted. Now: "nobody watched" counts from when a movie arrived, not from when you first asked for it; a title downloaded again after it was watched is kept; nothing runs unless every media server answered; a media server that doesn't know when something was played no longer makes it look watched in 1970; cleaning an episode removes only that file, never the show's folder; a file with several episodes goes only when all of them match; a problem reading tags stops the run; the oldest go first; and "Remove these now" removes exactly what the preview showed.
- Security: sign-in through a reverse proxy believed the user-name header from any address on your home network, so another device could sign in as any account by sending it. It now needs your proxy's own address and believes the header only from there. If you use it, open Settings > Accounts through your proxy and save it once more.
- The Kindle converter ignores impossible positions inside a book instead of trusting them (found by GitHub's code scanning; no effect on 64-bit systems).

### Changed
- The standard Docker image runs a tiny init process (tini) first, like the full image already did, so processes a post-import script leaves behind are always cleaned up.
- Updated source-map-js, a library used only while building the web interface, for a security fix. The app itself is unchanged.

## [2.1.0] - 2026-10-06

### Added
- Run a script after each import: put your own script in the config folder's `scripts` folder and pick it under Settings > System. It is told what was imported in `MEDIARIUM_*` variables, runs one at a time with a time limit, and can be tried from the page. Off until you pick one, and it can't be chosen or run with an API key.
- Subtitles made for your exact file: searches send the video's fingerprint, so a subtitle timed against that very file is marked and picked first. With automatic downloading on, Mediarium also looks again for a month and swaps an earlier pick for one made for the file when it turns up. Hand-picked or edited subtitles are never touched.
- Watch statistics: with What's been watched on, the Statistics page shows how much of your movies and episodes get played, the space held by what nobody has watched, and the most watched and latest watched titles.
- Specials: a show's season 0 is listed as Specials, unmonitored to start with, so you can ask for the ones you want. Existing shows get theirs at the next refresh.
- Anime and daily shows: a show's page has Episode numbering (seasons, anime episode numbers counted from the start, or air dates), set by itself when the show is added. Releases like "[Group] Show - 105" and "Show.2024.03.15" are searched for, picked and imported to the right episode.
- Subtitle timing: move a subtitle earlier or later, or line it up automatically with another subtitle of the same title that is in time (frame-rate drift included), from the Files panel. The original is kept, and Put the original back undoes it.
- Download hours: let new downloads start only between two hours (for example at night). Outside them they wait in line and start by themselves; a running download finishes. Settings > Downloading > Usenet and torrents.
- Permissions for basic users: choose, when adding or editing the account, which types of media they can add, and whether they can add titles themselves, pick releases, start searches and change monitoring, retry downloads, get subtitles and play, read and listen. Everything is on by default.
- Requests: a basic user who can't add titles themselves sends a request instead. Admins approve (the title is added for them, as they chose) or decline it with a note under Activity > Requests, and can be told about new ones by notification.
- What's been watched: Mediarium can read play counts from Plex, Jellyfin and Emby every six hours and show them on movie and show pages. Off until you switch it on under Settings > Connections > Media servers.
- Cleanup rules: remove movies or episodes watched a while ago, or movies nobody watched long after they were added, with a preview first, tags that keep a title safe, the recycle bin, and every removal in Activity. Off by default.
- Book series. A book's page shows the series it belongs to, every book in it in reading order, and a **Follow this series** switch that adds the missing books now and new ones as they appear. Books that aren't out yet show their release date, and Mediarium waits for that day before looking for them.
- Books on the calendar: books with a release date show up on Upcoming > Calendar and in the calendar feed, with a Books filter. On Wanted, a book that isn't out yet shows the day it comes out instead of Search now.
- Audiobook details: who reads it and how long it runs, from Audnexus (Audible's catalogue), on the book page and in the player. The full unabridged reading is preferred over translations and dramatisations.
- Chapters inside one-file audiobooks: an M4B's own chapter marks show in the player's list, with previous and next chapter, a slider per chapter and End of chapter on the sleep timer.
- Kindle books (MOBI, AZW, AZW3) open in the built-in reader: Mediarium's own converter turns them into EPUB the first time, in well under a second, and keeps the copy in its cache folder without touching your library.
- An optional Hardcover token (Settings > Info, lists and subtitles) for more book series and release dates. Without one, Open Library is used, as before.
- Sign in through your reverse proxy: a proxy that does the login itself (Authelia, Authentik, Cloudflare Access) can pass the user name on in a header, and Mediarium signs that account in. Off by default, only believed from a trusted proxy, and only for accounts that exist. Set it under Settings > Accounts.
- Email notifications have a **From name** box: the name your emails show as coming from. It starts as Mediarium, so emails no longer show the part before the @ (such as "nas").

### Changed
- The repository is tidier: the compose files, the container start script and `.env.example` are in `docker/`, and the contributing, security and conduct guides are in `.github/`. The install command now downloads `docker/docker-compose.yml`; the file itself is unchanged.
- Updated the libraries Mediarium is built with (SQLite driver, Go time package, Vite, oxlint).
- The Synology guide has a step-by-step section for reaching Mediarium from outside your home with DSM's reverse proxy, and the Synology, Unraid and QNAP guides no longer call ebooks and audiobooks unfinished.

### Fixed
- Tick boxes in settings forms (such as the subtitle languages) sit beside their names again instead of above them.

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
