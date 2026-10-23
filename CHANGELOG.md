# Changelog

All notable user-facing changes to Mediarium are recorded here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
version numbers follow [Semantic Versioning](https://semver.org/). Each
release has its own dated section. Changes that aren't released yet collect
under [Unreleased]. How releases are cut: [docs/RELEASING.md](docs/RELEASING.md).

## [Unreleased]

### Fixed

- **Add a download by hand** refused a magnet link with spaces in it, saying "Paste a web address that starts with http:// or https://, or a magnet link." Many sites copy the name part (`&dn=`) with plain spaces. The spaces are now written as `%20` before the link is checked and sent, so it is added like any other.

## [3.7.1] - 2026-10-23

In short: Premiumize as a third debrid service, a Music tab in the search results, a Retry button for picked downloads, and fixes for the calendar views, the Simkl Client ID test, the nzb360 dashboard and a lost library marker file.

### Added

- Activity's bar for picked downloads has a **Retry** button, next to To the front, Pause, Resume, Stop and Remove. It retries the picked downloads that failed or were stopped and leaves the others alone. Contributed by WassawRoki on GitHub, #118.
- Premiumize as a third debrid service next to Real-Debrid and TorBox (Settings > Downloading > Torrents > Debrid service). Paste the API key from premiumize.me/account, press Test to see your customer number, when premium ends and how much of the monthly fair-use limit is used. Torrents Premiumize already has are read at once; others become a transfer and the files are brought home like with the other services. Releases Premiumize has cached get the same "Cached" tag and ranking as TorBox. Thanks to Hoopes80 on GitHub, #120.
- The search results page has a Music tab next to Movies and shows, Books and Releases when music is on. It finds artists the same way the search box does, shows how many it found, and lets you add one from the card (thanks to mirisbowring on GitHub, #112).

### Fixed

- Picking more than 200 downloads in Activity made every action in the bar fail with "Pick at most 200 downloads at a time". The page now sends a bigger pick in batches of 200, and "To the front" keeps the order of the whole pick. Contributed by WassawRoki on GitHub, #118.
- The calendar's Month, Posters, Backdrops and Agenda tabs now each show something different on a phone. Month is a small grid with a dot for each release and the picked day's list underneath, Posters and Backdrops give each day its own block of posters or wide stills, and Agenda is the plain list. The choice is remembered in the browser. Reported by Bayharbourbutcher1 on GitHub, #119.
- The Test button next to the Simkl Client ID answered "This client_id is an OAuth 2.0 app: use POST /oauth2/device instead of /oauth/pin" for apps that sign in fine. It now asks the same way Connect does (the newer way first, the older PIN way for older apps) and says "Simkl accepted this Client ID." without starting a sign-in. When Simkl turns a Client ID test or a sign-in start down, a warning line (with only the first four characters of the Client ID) is written to the log. Reported by Bayharbourbutcher1 on GitHub, #115.
- The nzb360 app's "From Radarr: Recently Downloaded" card showed "No results found". The history now answers every way an app can ask for imports: the event type as a number, a name, a list or repeated, imports as both `downloadFolderImported` (3) and `movieFolderImported` (6) for Radarr, every line with its movie and poster, sorting by title or date, `movieIds` and big pages. A movie that is in the library with a file but never came through a Mediarium download gets one import line, so it shows up too. What the app asks the history for is written to the log at debug level. Reported by Bayharbourbutcher1 on GitHub, #94.
- A library folder that lost its `.mediarium-root` file (a new drive or volume at the same path, a moved library) refused every import with no way to fix it from the app. Administrators now get **This is the right folder: create the marker** on the failed import and next to the folder in Settings > Library > Folders, with a warning that explains the risk, a stronger one (and a tick to say the drive is connected) when titles are recorded under the folder but none of their folders is there, and **Retry the failed imports** afterwards. It is written to Activity. A network folder that cannot be read for a moment is no longer reported as having lost its file. Reported by Hoopes80 on GitHub, #121 and #45.
- On a phone, long release and file names without spaces ran off the right edge of a download's row in Activity (the error text, the Details box, the history steps). They now wrap, and the Details box lines up. Reported by Hoopes80 on GitHub, #121 and #45.

## [3.7.0] - 2026-10-22

In short: the calendar shows episodes on the day and hour they air in your own time zone.

### Added

- The calendar now shows an episode on the day and hour it airs in your own time zone. Mediarium asks TVmaze for the exact air time of each episode; a show that airs Monday 9 pm in the US now sits on Tuesday morning for someone in the UK. Episodes TVmaze has no time for stay on their date, as before. Idea from wivaku on GitHub, discussion #99.
- New under the calendar: "Show times in" lets you read the calendar in this device's time zone (the default), UTC or another zone. It is kept in the browser only.
- The calendar feed (.ics) writes timed events (in UTC) for episodes with a known time, and all-day events for the rest.
- An episode whose date has come but whose air time is still hours away is no longer counted as missing or searched for.
- New switch Settings > Metadata > Air times > "Look up air times on TVmaze" (on by default). Off means TVmaze is never contacted.
- The Sonarr-style API now sends the exact air time in `airDateUtc` and the TVmaze id in `tvMazeId` when known.

## [3.6.1] - 2026-10-22

In short: Simkl's Test connection works for accounts whose account call is refused, and a notification hint reads clearer.

### Fixed

- The Simkl *Test connection* button no longer says "Simkl rejected the client ID" for an account that is connected and whose lists sync fine. When Simkl refuses the account-name call, it checks the sign-in with the same call the list sync uses. Reported by nexus4004 on GitHub, #115.
- The hint under the *TRaSH Guides sync applied* notification no longer says nothing is sent when nothing changed (the box below it does the opposite). Pointed out by Bayharbourbutcher1 on GitHub, #109.

## [3.6.0] - 2026-10-22

In short: Simkl sign-in works with new Simkl apps, you can cancel a running download right from the release list, and the statistics no longer disappear when you clear finished downloads.

### Added

- Radarr and Sonarr apps such as nzb360 can now browse folders: Mediarium answers `/filesystem` (with `includeFiles`), `/filesystem/type` and `/filesystem/mediafiles` for your library folders and the downloads folder, `/ping`, `/log/file`, removing several queue entries at once (`DELETE /queue/bulk`) and `POST /queue/grab/{id}`. Anything outside those folders is left out. Thanks to Bayharbourbutcher1 on GitHub, #94.
- New option *Also tell me when nothing changed* under the *TRaSH Guides sync applied* notification: you then also get "TRaSH Guides checked: nothing needed updating", and a check that failed always sends "TRaSH Guides check failed: ...". Thanks to Bayharbourbutcher1 on GitHub, #109.
- When a release in the interactive list is refused because that movie or episode is already downloading, the message now shows the running download (progress and size) and a *Cancel it and grab this one* button, with a question first. It stops that download, deletes its partial files and grabs the release you picked in one step. Thanks to nexus4004 on GitHub, #116.
- The Simkl card shows the sign-in code in big letters with a Copy button, an "Open Simkl and enter this code" button, a countdown, and "Connected as <name>" when it is done. If you decline or the code runs out it says so and offers "Start again".
- If Simkl gives a refresh token, Mediarium keeps it (encrypted) and renews the sign-in when it runs out, so lists keep working.

### Fixed

- Connecting a Simkl account with an app made today ("This client_id is an OAuth 2.0 app: use POST /oauth2/device instead of /oauth/pin"). Mediarium now signs in with Simkl's OAuth 2.0 device code sign-in and falls back to the older PIN sign-in by itself for older apps. Thanks to Bayharbourbutcher1 and nexus4004 on GitHub, #115.
- The download numbers on the Statistics page and the dashboard (totals, the months, the Usenet share) are now counted as downloads happen and no longer disappear when you clear finished downloads from Activity or the history runs out. They are filled once from what the history held. New *Reset statistics* button on the Statistics page (administrators, with a question). Thanks to SanketBaraiya on GitHub, #117.

## [3.5.0] - 2026-10-22

In short: shows that ended are marked as ended, the show page tells what is monitored and how much space it uses, Simkl sign-in is fixed, and the assistant search is more robust.

### Added

- A show's page now says what is being monitored, in a small tag by the name: Monitoring: all episodes, new episodes only, only episodes that haven't aired yet, season 2 only, some episodes, or Not monitoring. The Monitored switch adds the count ("Episodes monitored: 12 of 40"). Thanks to Bayharbourbutcher1 on GitHub, #114.
- Size on disk on a show's and a movie's page, the size of each season, and the total of the Movies or Shows tab at the end of the Library's status chips (Shown: x of y when a filter is on). Thanks to Bayharbourbutcher1 on GitHub, #114.
- Seasons on this page (Collapsed, Latest season open, All open) on a show's page, kept in this browser; Open all and Close all were already there. Thanks to Bayharbourbutcher1 on GitHub, #114.
- The assistant search (`library_search`) also matches genres and the description, and has a `genre` argument (Action, Sci-Fi...). An empty search says "No titles matched" and how many titles the library holds. Thanks to mirisbowring on GitHub, #97.
- Notification event **TRaSH Guides sync applied**, off until you tick it. It sends one message when the automatic or manual TRaSH update changed custom formats (names, then "and N more") and profile scores, and nothing when nothing changed. Thanks to Bayharbourbutcher1 on GitHub, #109.

### Fixed

- Shows that had ended were not marked as ended in the Library. The status is now read from TMDB for every show, also the ones that are not monitored, once a day for running shows and once a week for finished ones. Thanks to SanketBaraiya on GitHub, #113.
- A show set to "Only episodes that haven't aired yet" now has its Monitored switch on (also through the Sonarr style API), ticking an episode or a season on a switched-off show switches the show on, and shows that were left off after that choice are switched back on when Mediarium starts. A show you switched off yourself is left alone. Thanks to Bayharbourbutcher1 on GitHub, #114.
- Asking the assistant for titles through MCP (library_search) no longer comes back as an empty "error communicating with the MCP server". Every failure is now a normal answer with a plain explanation, the search no longer scans the disks for subtitle files or reads every file's size unless the question needs it, and the searches accept movie/movies/film, tv/series/shows and the like. Thanks to mirisbowring on GitHub, #97.
- Connecting a Simkl account. The sign-in now sends the redirect address the guide tells you to enter (`urn:ietf:wg:oauth:2.0:oob`) and the app name and version Simkl asks for, and when Simkl refuses (for example with status 400) the card shows Simkl's own words and what to check. The badge on the Simkl key card says "Client ID saved" instead of "Connected", and the account card has a **Test connection** button that shows your Simkl name. Thanks to Bayharbourbutcher1 on GitHub, #115 (and nexus4004 on GitHub, #115).

## [3.4.0] - 2026-10-22

In short: Home Assistant can sign in to MCP, Discover has language and rating filters, richer notifications, import from any folder, and a number of fixes found by users.

### Added

- Apps like nzb360 that ask Radarr for recommended movies on their home screen now get an answer: what is trending on TMDB without the movies you already have (up to 20, refreshed hourly). Several settings pages those apps load (naming, media management, download client and indexer options, delay profiles, exclusions, backups, auto tagging) are answered too, so they no longer end up as unanswered requests in the log. Thanks to Bayharbourbutcher1 on GitHub, #94.
- Library filters have a new **Missing subtitles** choice next to Missing audio: pick a language and see the titles that have no subtitles in it, neither inside the video nor as a subtitle file next to it (like Movie.nl.srt). Subtitle files next to the video now also count for the existing Subtitle language filter. The assistant tool library_search has a matching missing_subtitle_language. Thanks to nexus4004 on GitHub, #89.
- The "Torrenting without a VPN" notice on the dashboard has a button **My download client is already behind a VPN** that hides it for good (administrators; the same option is under Settings > VPN, where switching it off shows the notice again). The notice is also left out when the built-in torrent client is off and torrents go to an external client. Thanks to Bayharbourbutcher1 on GitHub, #108.
- The "download started" and "ready to watch" messages now say the custom format score, the custom formats it matched (the top five), the release group and the indexer, whenever they are known. Webhooks get the same as `customFormatScore`, `customFormats`, `releaseGroup`, `indexer` and `size`, and a custom JSON body can use `{{customFormatScore}}`, `{{customFormats}}`, `{{releaseGroup}}`, `{{indexer}}`, `{{size}}` and `{{sizeText}}`. (Bayharbourbutcher1 on GitHub, #109)
- Two new events, **Download client down** and **Download client back**, for the torrent client and the Usenet downloader you connected and for the built-in Usenet downloader. One message per outage, off until you tick them. (Bayharbourbutcher1 on GitHub, #109)
- Import a file by hand now looks in any folder you choose (type it or pick it), not only the downloads folder, and a file can come in as a hardlink, a copy or a move. Incomplete download folders (like qBittorrent's `incomplete`), samples and unfinished files are left out unless you turn on Show incomplete downloads. (lazy-adams on GitHub, #110)
- The search results page has a Books tab, next to Movies and shows and Releases, when ebooks or audiobooks are on. It shows the same books as the search box, with the same Add button and the number found. (Deemer15 on GitHub, #112)
- Discover (Movies and TV shows tabs) has a **More filters** row: **Original language** (English, Korean, Japanese and the other common ones first, then every other language, named in your own language), **Lowest rating** (6+, 7+, 8+; titles with very few votes are left out) and **Runtime** (under 90, 90 to 120, over 120 minutes; for shows, per episode). The heading says what is filtered, for example "Shows in Korean from 2020 to 2026, rated 7+", and the choices stay in the address so a reload or a shared link shows the same list. Thanks to an idea sent from inside the app.
- Discover: **Hide owned** now keeps reading further pages until the grid is full of titles you don't have, instead of leaving holes where your titles were.
- MCP assistants that look for a sign-in by themselves, such as Home Assistant, can now connect. Mediarium answers the standard OAuth discovery addresses (`/.well-known/oauth-protected-resource`, `/.well-known/oauth-authorization-server`), lets an app register itself, and shows an Allow page where an administrator signs in and approves. API keys keep working as before. Thanks to mirisbowring on GitHub, #97.
- Under Settings > Other apps, the MCP card lists connected apps (name, account, access, when connected, last used) with a Revoke button, and a form to register a client for apps that ask for a client ID and secret (for example Home Assistant's application credentials).

### Changed

- The 401 from the MCP address now says where the sign-in information is (`WWW-Authenticate: Bearer resource_metadata=...`).

### Fixed

- Pressing Pause on a download that runs in an external torrent client (qBittorrent, Transmission and the others) now pauses the torrent in the client too; before, only Mediarium stopped watching and the client carried on. Resume starts it again. A torrent you pause in the client yourself is left alone and is no longer mistaken for a stalled download. Thanks to Bayharbourbutcher1 on GitHub, #108.
- Searching for the releases of a book no longer asks movie and TV sites, and the second, title-only search leaves out indexers that already failed. The search stops after 25 seconds and shows what the quick indexers found instead of failing, and the list says which indexers were left out. (Deemer15 on GitHub, #111)
- Unknown `/.well-known/...` and other discovery addresses answered the web page's HTML, which made some MCP clients stop with a JSON error. They now answer a JSON 404.

## [3.3.0] - 2026-10-21

In short: connect AI assistants to your library, use SABnzbd or NZBGet for Usenet, see what is inside your video files, order and group qualities, and rescan music albums.

### Added

- Ask your library questions from a chat program or a voice assistant. Mediarium now has an MCP address (`/mcp`) that programs supporting MCP, and Home Assistant's voice assistant, can use to answer things like "which movies are not FuN releases?", "how many movies are over 15 GB?" or "which downloads failed?". It is off until you switch on Let AI assistants connect (MCP) under Settings > Other apps, and it only works with an administrator's API key. A read-only key can only ask questions. Adding titles, switching a profile, retrying a failed download and adding a preferred term are shown first and only happen after a yes. Idea from mirisbowring on GitHub, #97. See docs/mcp.md.
- Usenet releases can now go to SABnzbd or NZBGet instead of the built-in downloader. Settings > Downloading > Usenet > "Use SABnzbd or NZBGet": pick the downloader, enter its address and API key (SABnzbd) or login (NZBGet), a category (plus one per movies, TV, music and books) and a priority, press Test, then switch on "Send Usenet releases to ...". The built-in downloader stays the default. Progress, pause, resume and remove show under Activity > Download clients, a release the downloader gives up on is blocklisted and the next one is tried, and finished files are imported from the downloader's folder (with path mapping) without being moved or deleted. Thanks to xrandallx1 on GitHub, #92.
- Music: a **Rescan album folder** button on an album (administrators) re-reads the audio files in the album's folder, matches them to the tracks by tags, track numbers and titles, and updates which tracks have a file, the album's quality and its status. It works for albums already in your library, which the import scan skips, and for an album that has no folder recorded yet (it looks in the folder Mediarium would use, or one of the artist's folders with the album's name). If the album is still incomplete and monitored it says so, lists the missing tracks and offers **Search for missing tracks**. Idea from u/troelsdk on Reddit.
- Music: **Delete file** in an album's Files tab (administrators) removes one file from the album folder after you confirm (recycle bin when it is on). The track loses its link and the album's status and quality are worked out again; with no audio left the album is wanted again so a better release can be searched. Only files inside the album's folder, inside the music folder, can be deleted. Idea from u/troelsdk on Reddit.
- Music: **Search for missing tracks** (and "Look for better") now also works for a downloaded album with tracks missing, and takes any release the profile accepts, not only an upgrade. Idea from u/troelsdk on Reddit.
- Quality profiles can have their own quality order and groups, like the quality list in Radarr and Sonarr. In a profile's editor (under Show advanced options) unfold "Quality order and groups" and press "Customise order and groups": drag the qualities, use the arrows (or the arrow keys on a row's handle) to rank them your way, and tick "Pick" on several to put them in a group that counts as one rank. The cutoff can be a whole group. "Use TRaSH layout", "Treat web and TV as equal" and "Reset to default order" help you start. Automatic search, the best match in the search list, upgrades, the "below cutoff" lists and the delay profile all follow the order. Profiles without a custom order behave exactly as before. Importing a TRaSH Guides profile sets the order when the guide groups or ranks qualities differently. The Radarr/Sonarr compatible quality profile answers show groups the way those apps do. Thanks to Bayharbourbutcher1 on GitHub, #87.
- Mediarium now reads what is inside your video files: the video codec, picture size, 8 or 10 bit, SDR, HDR10, HDR10+, Dolby Vision or HLG, every audio track (codec, channels, language, Atmos when the track says so) and every subtitle track. A movie's page and each downloaded episode show it as a short line of chips (`HEVC 10-bit HDR10`, `2160p`, `TrueHD 7.1 Atmos`, `EN, DE audio`, `EN, NL subs`). It reads only the headers of MKV, WebM, MP4, M4V and MOV files, needs no extra program, and goes through your existing library slowly in the background. Thanks to nexus4004 (GitHub) for the idea (#89).
- New Library filters: video codec, HDR, resolution, audio language, audio codec, subtitle language, and "missing audio" for a language (for example 4K films with no German track). They are kept in the address like the other filters.
- The Radarr and Sonarr style API now fills `mediaInfo` on movie and episode files (`videoCodec`, `videoBitDepth`, `videoDynamicRangeType`, `audioCodec`, `audioChannels`, `audioLanguages`, `subtitles`, `resolution`, `runTime`), and the assistant (MCP) `library_search` tool can filter by `audio_language` and `video_codec`.
- WebP covers. A book cover that is a WebP picture (in an EPUB, from Open Library, from the Cover Art Archive, as comic pages or uploaded) is now read and kept as a JPEG instead of being refused. A `cover.webp` or `folder.webp` in an album folder is kept and shown too. (No issue found for this, so no credit line.)

### Changed

- The search box popup keeps settings out of the way. The All tab shows at most three settings matches (a "Show all" row opens the rest) and they come after the titles unless what you typed is a setting's name; All | Media | Settings tabs appear when settings match, and the last tab you picked is remembered in this browser. Thanks to mirisbowring on GitHub, #93.
- Mediarium now requires Go 1.26.10 to build, after a security scan reported a certificate-handling problem in 1.26.9 (the Docker images were already built with a newer Go).

### Fixed

- Book search: Open Library answered "422" for text its search syntax can't read (an unclosed bracket or quote, a lone colon). Mediarium now retries with just the words, and when Open Library still refuses it says "Open Library couldn't read that search. Try fewer or simpler words." The search box in the header showed an empty list in that case; it now shows the message.
- Interactive search: the **Score** column showed only the custom-format part of the score. It now shows the whole score the pick uses (formats + preferred terms + the small bonus of a PROPER/REPACK), equal to the total in the "Why this score" row, and sorting by Score uses it. A profile with preferred terms but no custom formats now gets the column too.
- Checking a site that needs FlareSolverr no longer shows "the check timed out" while FlareSolverr is still solving the Cloudflare challenge. The background check gave up after 30 seconds, the Test button waited 100; both now wait up to 100 seconds when FlareSolverr is set up, searches up to 150. The Test button says "Solving the Cloudflare check, this can take up to a minute" while it waits, and a real timeout now names FlareSolverr, how long it waited and what to try. Thanks to Bayharbourbutcher1 on GitHub, #86.

## [3.2.20] - 2026-10-21

In short: phone apps get a real history, notifications mention subtitles, optional automatic single sign-on, up to 500 downloads at once, and several fixes.

### Added

- Apps like nzb360 now get a real history from Mediarium, so their "Recently downloaded" cards fill up instead of saying "No results found". Each download shows as grabbed, and as imported or failed once it ends. Also answered: history since a date, history of one movie or show, episode and movie files, the calendar with the episode file, parsing a release name, and empty lists for rename and manual import. Thanks to Bayharbourbutcher1 on GitHub, #94.
- The "ready to watch" notification now says where the subtitles stand: "Already there (en, nl)", "None yet, searching for en" and so on. It appears in every channel (Discord, Telegram, email, push, webhook) and only while subtitles are switched on. Webhooks get a new `subtitles` field, and custom webhook bodies get `{{subtitles}}`. (SanketBaraiya on GitHub, #106)
- New setting "Sign in automatically with" (Settings > Account & Access > All accounts). With single sign-on chosen, visitors who are not signed in go straight to the provider and come back to the page they asked for. Open the address with `?no-redirect=1` (or `?disable-auth-redirect`) for the normal sign-in page. Off by default. (mirisbowring on GitHub, #105)
- A switch "Built-in minimum size check" (Settings > Library > Quality > Size limits). On by default. Off, automatic searches no longer skip a release for being too small for its quality, and the release list stops showing "smaller than a real ... release is likely to be". Reported from inside the app (Mediarium reference 20261020-200637-1fa7f504).
- **Downloads at the same time** is a number box from 1 to 500 instead of five buttons, so a download client such as qBittorrent can run as many torrents at once as it did for Radarr and Sonarr. It saves by itself a moment after you stop typing. The built-in Usenet and torrent downloaders keep their own limit (Usenet: your provider's connections, 5 to 50; torrents: 20), the rest wait in line. Thanks to WassawRoki on GitHub, #102.

### Changed

- The built-in minimum for a TV episode now shrinks with the episode length when TMDB says it is under 30 minutes (table value times runtime over 30, never under 15 MB), so real releases of short shows (a 5 minute WEB-DL 1080p episode needs about 25 MB instead of 150 MB) are no longer skipped. Movies and normal episodes are unchanged.
- Development: vite 8.3.5 (Dependabot, #103).

### Fixed

- Adding custom formats that a quality profile already has no longer re-scores them silently. They are skipped and keep their score, and the message says per profile how many were added and how many were already there. A new box, "Replace the score if the profile already has the format", gives them the new score on purpose. The Add formats list in a profile says when every format is already in it. Thanks to Bayharbourbutcher1 on GitHub, #87.
- The "More" menu on a torrent card in Activity > Download clients no longer opens behind the next card. An open menu now sits above the cards around it, and it scrolls into view when it opens near the bottom of the window. Thanks to nexus4004 on GitHub, #107.

## [3.2.19] - 2026-10-20

In short: nzb360 and other phone apps show scores, naming has folder formats and TRaSH presets that save, size limits are separate for movies and TV, and long cards fold away.

### Added

- Separate Movie folder and Show folder formats under Settings > Library > File names and importing > Custom, with ready-made choices (Mediarium default, TRaSH recommended, and TMDb id versions for Plex, Emby and Jellyfin) and a live preview. They only name the folder of a new title; a movie or show that already has files keeps its folder. (Bayharbourbutcher1 on GitHub, #95)
- Release lists in apps like nzb360 now show what Radarr and Sonarr show: the custom formats a release matches and their score, the quality revision (repack or proper), the edition, seeders and leechers, and the reasons an automatic search would skip it. The best match is listed first. Reported by Bayharbourbutcher1 on GitHub, #94.
- The apps' "cutoff unmet" list, saved filters, custom format list and the settings lists they load at start (tags in detail, display and host settings, notifications, import lists, remote path mappings, updates, tasks, log, metadata) now get a proper answer instead of "not found". Releases pushed from other tools are answered with a clear refusal. Reported by Bayharbourbutcher1 on GitHub, #94.

### Changed

- Size limits are now separate for Movies and TV, each with its own tab, switch and Apply TRaSH sizes. Radarr's TRaSH lists fill the Movies table and Sonarr's the TV table. On upgrade the TV table starts as a copy of your current table, so no check changes. (Bayharbourbutcher1 on GitHub, #95)
- The Custom formats card, the Size limits card and the Music card on the Quality page are folded away until opened, with a short summary and your choice remembered. Custom format scores in the profile editor are folded too, and open by themselves when you add formats or a score has a mistake. (Bayharbourbutcher1 on GitHub, #95)

### Fixed

- The TRaSH file name formats (Plex, Emby, Jellyfin with TMDb id) no longer fail to save with "at most 200 characters". A format can now be up to 500 characters. (Bayharbourbutcher1 on GitHub, #95)

## [3.2.17] - 2026-10-20

In short: phone apps can search for releases, the search box finds settings, and more TRaSH help.

### Added

- Each row in a profile's Custom format scores list has a bin button that takes the format out of the profile. (Bayharbourbutcher1 on GitHub, #87)
- New file name choices under Settings > File names and importing > Custom > Start from: "TRaSH recommended", plus Plex, Emby and Jellyfin versions with the TMDb id. Nothing existing changes. (Bayharbourbutcher1 on GitHub, #95)
- "Apply TRaSH sizes" on the Size limits card fills the table with the sizes the TRaSH Guides recommend (movies, streaming and 4K SQP, anime, series, anime series). It shows what changed first and saves nothing until you press Save. (Bayharbourbutcher1 on GitHub, #95)
- Phone apps built for Radarr and Sonarr (nzb360 and others) can now run an interactive release search: pick a movie, episode or season, see what your indexers have, and tap one to download it (Bayharbourbutcher1 on GitHub, #94).
- Radarr and Sonarr apps also get the list of your indexers and a download client entry, and a request Mediarium does not answer is written to the log with its address instead of failing without a trace (Bayharbourbutcher1 on GitHub, #94).
- The search box at the top also finds settings. Type "folders" or "temp folder" and a Settings group shows the page or the setting; picking one opens the page, scrolls to the card and lights it up for a moment (mirisbowring on GitHub, #93).

### Fixed

- Importing TRaSH custom formats that score 0 now adds them to the quality profile with a score of 0 instead of skipping them, from the TRaSH tab, "Add to profiles" and "Add formats" on a profile. They show in the profile's Custom format scores list. A 0 changes no release's score. (Bayharbourbutcher1 on GitHub, #87)

## [3.2.16] - 2026-10-20

In short: Usenet downloads with a split 7z archive are unpacked.

### Fixed

- Usenet downloads where the video is inside a 7z archive cut into pieces (`name.7z.001`, `.002`, ...) are now unpacked. Before, only a plain `.7z` was recognised, so these ended as "No video in a download". Reported by xrandallx1 on GitHub (#92) and u/troelsdk on Reddit.

## [3.2.15] - 2026-10-20

In short: multi-language releases rank better when you list several languages, and the TRaSH update tells you what it did.

### Fixed

- A German release with `DL` after the source (`German.BDRip.DL`) is now read as dual language, the same as `German.DL.AC3`, so both rank the same. Reported by mirisbowring on GitHub, #91.
- Formats imported from the TRaSH tab, or added with "Add to profiles...", now show up in a profile's edit form that is already open, instead of leaving it out of date (and saving it would drop the new scores). The form picks up the new scores and keeps anything you typed. Reported by Bayharbourbutcher1 on GitHub, #87.

### Changed

- When you list several audio languages (for example German, English), a multi-language release that has the first one now ranks above a plain release in that language, because it also carries the others. With one language nothing changes. Reported by mirisbowring on GitHub, #91.

### Added

- "Update from TRaSH" now leaves a result on the Custom formats card ("Checked the TRaSH Guides: 3 formats updated, 12 already up to date, 2 profile scores changed", or nothing to update). The same line goes to Activity and the server log, for the automatic check too. Suggested by Bayharbourbutcher1 on GitHub, #87.
- Under the "Keep TRaSH formats up to date" switch the card shows when the guides were last checked and about when the next check is, and you can choose every day or every week. Suggested by Bayharbourbutcher1 on GitHub, #87.

## [3.2.14] - 2026-10-19

In short: the search list says why a release was not the best match.

### Changed
- The details row under a release in interactive search now says why a release that was not rejected was not the best match: your preferred language, a higher quality, a cached release, a higher score (with the matched terms and formats of both releases), a torrent with no seeders, or, between two equal releases, the indexer's priority, the protocol or the seeders. Thanks to mirisbowring on GitHub, #91.
- The details row also lists the preferred terms a release matched, with their scores, next to the custom formats. Thanks to mirisbowring on GitHub, #91.

## [3.2.13] - 2026-10-19

In short: subtitle results get a Fit even when your video file's name says nothing about its release.

### Changed
- In the subtitle list, Fit now also compares each result with the name of the release that was downloaded for the video (Mediarium remembers it) and with the stored quality, and takes the best score, so a movie that came from a YTS release can match a YTS subtitle even though the file is named `Title (Year).mp4`. Rating stays empty for SubDL, which sends none. The same comparison is used when Mediarium picks a subtitle by itself (asked for by SanketBaraiya on GitHub, #83).

## [3.2.12] - 2026-10-19

In short: the address of a site on your own network could not be edited yet.

### Fixed
- The address of a site on your own network (BitMagnet, for example) stayed a fixed choice of its two listed links, with no Address field when editing. The site list your server had saved came from an older version and didn't say which sites run on your network; it is worked out when the list is read now, so the field is there without pressing Update list (reported by userkbmsr-png on GitHub, #90).

## [3.2.11] - 2026-10-19

In short: see why a release got its score, and use your own address for a site on your network.

### Added
- A site from the list that runs on your own network, such as BitMagnet (Local DHT), can now be pointed at your own server. When you add it (or edit it later) there is an Address field filled in with the example address, and you can type something like http://192.168.1.10:3333. Test then asks that server, and a refused connection on a 127.0.0.1 address now explains that inside Docker 127.0.0.1 is the Mediarium container itself. Sites on the public internet keep to the addresses in their definition. Thanks to userkbmsr-png on GitHub, #90.
- The search list can now say why a release got its score. A small arrow after the score opens a row under the release with each matched custom format and its score in the chosen profile, the total, the profile's minimum score and the score upgrades stop at, and every reason an automatic search would skip it (quality or language not taken, size outside the limits, score under the minimum, blocklisted, too few seeders, a file you already have that is as good). It works by touch and keyboard, no hovering. "Show why" above the list opens all of them, and your choice is remembered. Asked for by Bayharbourbutcher1 on GitHub, #87.

## [3.2.10] - 2026-10-19

In short: SubDL subtitles that were found but still thrown away, and a cropped arrow on the indexer card.

### Fixed
- SubDL subtitles were still thrown away as having "no usable download link": SubDL's link carries more after the file name (a query or a fragment), which Mediarium counted as an invalid path. Only the path is used now, and the Details line says when there was more after it (reported by SanketBaraiya on GitHub, #83).
- The little arrow that opens the reason of a failed test on an indexer card was cropped. It has room now (SanketBaraiya on GitHub, #80).

## [3.2.9] - 2026-10-19

In short: a minimum number of seeders per torrent indexer, and tidier indexer cards.

### Changed
- The indexer cards in a row are the same height with the buttons at the bottom, and each card shows its test result and limits in at most two short lines with the full text on hover (a failure opens its reason when you tap it). Nothing is hidden: a passed test older than a day is just shown dimmer. Asked for by SanketBaraiya on GitHub, #80.

### Added
- A **Minimum seeders** box on every torrent indexer card. Releases from that indexer with fewer seeders than the number are skipped by the automatic searches, the check for new releases, upgrades and season packs, so downloads stall less often. 0 turns it off (the default). Releases your debrid service already has cached, and releases that don't say how many seeders they have, are never skipped. In a search by hand every release is still listed; the ones under the minimum get a "Below minimum seeders" label and you can still grab them. Also in the settings file (`minSeeders`) and the API, and a minimum set in Prowlarr comes along when you import. Thanks to Bayharbourbutcher1 on GitHub, #88.

## [3.2.8] - 2026-10-19

In short: custom formats from the TRaSH Guides are easier to score, add and keep up to date.

### Added
- Custom formats added from the TRaSH Guides are now scored with TRaSH's default score in your default profile unless you choose "Don't score them", and the list shows the score each format will get before you press Add. Thanks to Bayharbourbutcher1 on GitHub (#87).
- You can tick several custom formats and add them to one or more profiles in one go (with TRaSH's score for each, or one score you type), and the profile edit form has an "Add formats" button for the same. Thanks to Bayharbourbutcher1 on GitHub (#87).
- New "Keep TRaSH formats up to date" switch (off by default) and an "Update from TRaSH" button that shows what would change first. Conditions of TRaSH formats follow the guides, and a profile's score follows only while you haven't changed it yourself. Thanks to Bayharbourbutcher1 on GitHub (#87).
- In the release list of a manual search, the release an automatic search would take is marked "Best match" and put first, followed by the others from the highest custom format score down. Automatic searches already took the highest score within the same quality. Thanks to Bayharbourbutcher1 on GitHub (#87).

## [3.2.7] - 2026-10-19

In short: a fix for tools like Maintainerr that act on a season, SubDL subtitles that were found but thrown away, and the indexer card icon.

### Fixed
- The Sonarr-compatible episode list ignored `seasonNumber`, so a tool that asked for one season (Maintainerr, for example) got every season of the show and could act on episodes that were never meant, such as unmonitoring season 7 because of a rule about season 1. It now answers with only the season asked for (also `episodeIds` and `episodeFileId`). If your episodes were unmonitored like this, open the show and choose which episodes to monitor again (reported by u/troelsdk on Reddit).
- SubDL subtitles were found and then all thrown away as having "no usable download link" because Mediarium only knew one shape of SubDL's download path. Any plain path under /subtitle/ is accepted now, and when a link is still turned down the Details line shows what it looked like (reported by SanketBaraiya on GitHub, #83).
- The icon on an indexer card (the magnet or server symbol) sat in the top-left corner of a square box with sharp corners. It is now a rounded tile with the symbol centred (SanketBaraiya on GitHub, #80).

## [3.2.6] - 2026-10-19

In short: a tidier Indexers & Search page, and a way to see why a subtitle search finds nothing.
- Every section of the Indexers & Search page is one full-width card, and your indexers sit in up to three columns, with no card overlapping another at any width.
- When a subtitle search finds nothing, a Details line shows what each site was asked and what it answered, and SubDL is asked in more forgiving ways.

### Fixed
- On Settings > Indexers & Search the "Your indexers" card no longer overlaps the "Add an indexer" card at medium widths. Every section is one full-width card, stacked (reported by SanketBaraiya on GitHub, #80).
- SubDL lookups are more forgiving: if every lookup finds nothing it is repeated with fewer filters, the language is accepted in any spelling, full download links are accepted, and hearing-impaired results are ranked lower instead of dropped (SanketBaraiya on GitHub, #83).

### Changed
- The indexer list is a grid of up to three columns (two on medium screens, one on a phone). On a wide screen the add card shows the search and filters beside the list of sites (SanketBaraiya on GitHub, #80).
- When a subtitle search finds nothing, a collapsed Details line under the result lists, for each site and lookup, what was asked and what came back (never keys). The search answer of the movie and episode subtitle endpoints is now an object with `results` and `diagnostics` instead of a bare list.

## [3.2.5] - 2026-10-19

In short: release dates for your own country, and the Indexers & Search page fixed.
- A new setting picks the country for a movie's release dates, and a date is never taken from some other country any more.
- The Add an indexer card no longer slides over the sections below it when you scroll.

### Added
- **Country for release dates** under Settings > Info, lists and subtitles. Pick your own country (India, say, while reading in English); left on "Same as the info language" it works as before. Asked for by SanketBaraiya on GitHub, #82.

### Changed
- A movie's cinema and digital dates now come from your country, then from the US (shown with "(US)"), and otherwise say "Not announced". They are never taken from some other country any more, so no more stray (BR) or (IN). The calendar follows the same rule and re-reads its saved dates when you change the country (SanketBaraiya on GitHub, #82).

### Fixed
- On Settings > Indexers & Search the "Add an indexer" card no longer sticks to the screen or scrolls inside itself, so it can't slide over "Cloudflare sites" and "IRC announce" any more. The page scrolls as one again (reported by SanketBaraiya on GitHub, #80).

## [3.2.4] - 2026-10-19

In short: fixes for outside torrent clients, from your reports.
- An indexer's seeding rules now reach qBittorrent (they never did), and the Category box stays empty when you clear it.
- Stopping or removing a download also removes the torrent Mediarium sent to qBittorrent or Transmission, unless it has finished (a switch in the download client card).

### Added
- **When I stop or remove a download, remove it from the client too** in the download client card (on by default). Stopping a download, removing it from the list, removing its movie or show, or pressing Blocklist & search again now also removes the torrent Mediarium sent, with what it downloaded, from qBittorrent or Transmission. A finished torrent, an imported download and a torrent that was in the client before Mediarium sent it are left alone (Blocklist & search again also removes a finished one). If the client is down or has lost the torrent, the download is removed from Mediarium anyway. Asked for by Bayharbourbutcher1 on GitHub, #85.

### Fixed
- The Category field in the download client card no longer refills with "mediarium" after you clear it. If you fill in a category for some kinds and leave the main one empty, the other kinds are now added with no category (the `mediarium-qN` tag still lets Mediarium find its torrents). Only when everything is empty is "mediarium" used. Reported by Bayharbourbutcher1 on GitHub, #79.
- An indexer's seeding rules never reached the download client. Mediarium didn't remember which indexer a release came from, so only the global limit was ever sent. It now records the indexer when a release is grabbed (from a search, the feed check or a scheduled search) and sends that indexer's ratio and time to qBittorrent when the torrent is added. Reported by Bayharbourbutcher1 on GitHub, #79.

## [3.2.3] - 2026-10-18

In short: release dates on the movie page, and fixes for second copies and for subtitles.
- A movie's page shows when it is in cinemas and when it comes out digitally, and the calendar shows both days.
- A release that may not replace your file is no longer imported next to it under another name (this left second copies with TRaSH-style names).
- The subtitle search asks each site by TMDB id, IMDb id and title, reads more pages, and shows an error instead of "No subtitles found".

### Fixed
- A release that is not allowed to replace the file you have is no longer imported next to it when its file name differs (for example with TRaSH-style names). Before, a worse release, or any release for a title set to keep its file, landed beside the good file and became the one Mediarium pointed at, leaving two copies. It is now turned away with "Kept the file you already have." (reported by dxtrlws on GitHub, #84)
- A file whose quality is not stored is now read from its name, including bracketed names like `[Bluray-1080p]` and `[Remux-2160p]`; a file whose quality cannot be told is never counted as worse than a new release.
- When a new version replaces a file under another name, the old file goes to the recycle bin (when it is on) together with the subtitles and other files named after it, instead of being deleted and leaving them behind. Choosing "Overwrite" on a parked "Ask me each time" conflict now also removes the old file that had another name.
- Searching for subtitles could say "no subtitles" for well-known titles that OpenSubtitles and SubDL do have. Both sites are now asked by TMDB id first, then by IMDb id, then by title and year, and up to three pages of results are read. A site that fails to answer is now shown as an error instead of "No subtitles found", and a failed search is no longer remembered for days (reported by SanketBaraiya on GitHub, #83).
- Subtitle results from an add-on show the add-on's own name in the From column, and Rating and Fit are left empty when there is nothing to base them on, instead of a dash or 0.

### Added
- A movie's page now shows when it is in cinemas and when it comes out digitally (with how long until then when it is still ahead), for your country, and the calendar shows a monitored movie on both days, marked Cinema and Digital. The calendar learns the digital dates a few movies at a time in the background, so right after the update it shows only the cinema dates. Thanks to SanketBaraiya on GitHub, #82.

## [3.2.2] - 2026-10-18

In short: a tidier indexer page and a round of fixes from a full check of the 3.2 changes.
- The indexer list is sorted by priority, in two columns, and the Add card stays in view.
- The problem report form tells you when a newer version is out, and an indexer error that an update fixed says so.
- Members no longer see server folder paths, a disconnected library folder is better protected, and settings save all or nothing.
- Fixes for rescan, same-name shows, season packs and resuming downloads after a restart.

### Changed
- The indexer list is sorted by priority (lowest number first) and shown as compact cards in two columns, the Add card stays in view while you scroll, and a filter box appears when you have more than eight indexers. Asked for by SanketBaraiya on GitHub, #80.

### Added
- The problem report form tells you when a newer version is out (it says which version you are on and which is newest, and that many problems are fixed by updating) before you send it, and says to leave an email or GitHub name if you would like an answer.
- An indexer that shows a known error from an older version says which version fixed it and to update and press Try again.

### Fixed
- Shows with the same name: a show you are searching for (monitored) now always gets the releases without a country tag before one you are not searching for. The American-first rule only decides between shows that are all monitored (or all unmonitored).
- Rescan files on a show: when the show's folder also holds files of other shows, a file is only given to this show when the title in its name is the show's. Files named for another show stay where they are and are listed as not matched.
- Rescan files on a show: a smaller file for two episodes that you added by hand is no longer dropped as a copy of a bigger file that an episode already has.
- Season packs: a season that keeps finding no pack is searched for six times (once a week) and then left alone until an episode is added to it, you change the season pack setting, or 60 days have passed.
- Pause while streaming: the list of paused downloads can no longer lose entries when the restart resume and the stream check change it at the same time.
- Settings > Downloading: the download client card keeps what you typed when it reloads because another card changed, and says so when its settings can't be loaded.
- Members no longer see server folder paths in the downloads list (failed-import messages, conflict paths) or the second-copies folder.
- Imports into a library folder that sits inside another library folder are now checked against that inner folder's own marker.
- Season packs and the resume-interrupted option are now saved together with the rest of a settings update, so a failed save no longer leaves them half-changed.
- When a restart interrupted downloads that could not carry on by themselves, Activity now says so and that they stay paused.
- A settings file that names an indexer priority with an old word ("preferred", "normal", "last resort") or a number in quotes no longer shows a change on every check.
- Simkl connect: the "waiting for the code" wait is no longer cut short when the client ID box changes, and it stops if you leave the page.
- Person page: moving from one person to another can no longer show the previous person's details.
- Settings > Indexers: a leftover filter word can no longer hide a short indexer list.

## [3.2.1] - 2026-10-18

In short: new indexers and lists, and fixes for what you reported after 3.2.0.
- Indexer priority is a number from 1 to 50 like Prowlarr, Knaben is a built-in site, and Simkl lists can be imported (Simkl is new and has not been tried with a real account yet).
- Anime numbered by TheTVDB seasons (One Piece, Jujutsu Kaisen) is placed on the right episodes, and Mazepa is selectable again.
- The e-reader catalogue loads much faster on big libraries.
- Better on a desktop with wide fonts, and a few more fixes below.

### Added
- **Simkl lists**: import your own Simkl lists (Plan to watch, Watching, Completed, On hold, Dropped; movies, shows and anime). Connect your Simkl account with a short code at simkl.com/pin under Settings > Info, lists and subtitles > Import lists, then add a Simkl list. An idea from nexus4004 on GitHub, through the idea form.
- **Knaben** is a built-in torrent site now: add it from Settings > Indexers & Search > Torrent > Pick a site from the list. It's public, needs no login, and works with the VPN switch like the other sites. Asked for by SanketBaraiya on GitHub, #73.
- **Indexer priority is a number from 1 to 50** (like Prowlarr; lower is tried first, 25 by default) instead of Preferred, Normal and Last resort. Existing indexers keep their order (Preferred became 10, Normal 25, Last resort 40), the Prowlarr import carries its numbers over, and the settings file and the API still accept the three old words. Thanks to SanketBaraiya on GitHub, #80.

### Fixed
- A title with a `+` or a `%` in it (`C++ Primer`) is no longer changed when Sonarr or Radarr style tools look it up through Mediarium: the search term was decoded twice.
- Names with `&` are handled better: a folder named with an HTML-escaped `&amp;` still matches its title, a search for a title like `Law & Order` no longer misses releases because of the `&`, and the Library, Wanted and Bookshelf search boxes treat `&` and "and" as the same and ignore accents (reported by franciscojnneto on GitHub, #81; if folders with `&` still don't show up for you, send me one example).
- Anime files named by TheTVDB season (One Piece) were placed three episodes too late because TheTVDB starts the season earlier than the movie database does. Placement now checks the episode titles in the file names, and places a file by its title when the title matches exactly one episode. The import note lists the files that were left out (reported by SanketBaraiya on GitHub, #69).
- A site marked "Not supported yet" in the site list is checked again after an update, so one that an update made possible (like Mazepa) no longer stays grey until you press Update list (reported by Apostol6 on GitHub, #16).
- The e-reader catalogue (OPDS) and the Kobo sync no longer load and sort the whole book list for every request. Built pages are kept in memory and rebuilt only when a book is added, removed or changed; saving a reading place does not count. Pages also carry an ETag, so an app that sends If-None-Match gets a 304.
- On a phone, long button labels and hints in Settings (Russian, German, French, Polish) now wrap instead of pushing the page wider than the screen.

## [3.2.0] - 2026-10-18

In short: a feature release, built mostly from your reports and ideas.
- Click an actor, director or writer to see their page, and add their other work from there.
- Big downloads unpack much faster (a 75 GB movie took hours on an Unraid share), with progress shown in Activity.
- New options: a temporary download folder on a fast disk, a category per kind of download and seeding limits for qBittorrent and Transmission, prefer season packs, only watch episodes that haven't aired yet, and resume downloads after a restart.
- Anime named by TheTVDB seasons (One Piece, Jujutsu Kaisen) is placed on the right episodes, and YTS, The Pirate Bay, nekoBT and Mazepa work again.
- Safer: an unmounted library drive is never written into, settings save all or nothing, and `app reset-key` helps when the key file is lost.
- Better on a phone and in long languages, with about fifty more server messages translated.

### Added
- **Shows with the same name are kept apart** (Shameless US and UK, The Office, Top Gear, Doctor Who 1963 and 2005): Mediarium now remembers the country each show comes from. A release tagged `US`, `UK`, `AU` and so on (`Shameless.UK.S02E03`, `Shameless (US) S01E01`) only goes to a show from that country, and when two shows share a name, a release without a tag goes to the American one (or the one you added first), not to both. Lists, search results and the import review show "Shameless (UK)" when two shows have the same name, and a folder called `Shameless (UK)` is matched to the UK show when you import a library. The country is read on the next refresh of each show.
- **Sign out browsers that sit unused** (optional): Settings > Account & Access > Security has a new choice for administrators, 1 to 7 days. A browser not used for that long is signed out and the sign-in page says why. Off by default. API keys, calendar feeds and e-reader passwords are not affected.
- `app reset-key`: for when `secret.key` is lost. It lists every saved password and key that can't be read, asks you to type yes (or `--yes`), keeps a copy of the database and the old key, clears only those (the things they belong to are switched off or removed until you enter them again), makes a new `secret.key` when there is none that works, and prints what has to be entered again. It never prints a secret. The startup message for a missing `secret.key` now points to it.
- **Resume after a restart**: downloads that were running when Mediarium stopped (an update, a restart, a crash) now carry on by themselves, a few seconds apart, once the VPN is up. Anything you paused stays paused. The switch is under Settings > Downloading > Download rules and is on by default.
- **Prefer season packs** (an idea from nexus4004 on GitHub, through the idea form): an optional switch under Settings > Quality > Automation, and a choice on each show. Once a season has ended, Mediarium looks for the season pack and takes it only if its quality is the same or better than the episodes you have; until then episodes are grabbed one by one as before. Off by default.
- **Temporary download folder** (an idea from mirisbowring on GitHub, #76): an optional setting under Settings > Downloading > Download rules that keeps the half-finished download, repair and unpack work on another disk (a fast cache drive, for example), while the finished files still go to your library. Empty by default, so nothing changes. A download keeps the folder it started in if you change the setting later. If the folders are on different drives the file is copied across and checked.
- **Rescan files** on a show now also looks in the show's folder and adds video files you put there by hand to the right episodes (season and episode, the number from the first episode for anime, or the air date for daily shows). Nothing is moved, renamed or downloaded, and the message says how many it found. Files it can't match are listed by name instead of guessed. The button now shows on shows that have no files yet too.
- A new show added by an import whose files are named by air date (talk shows, news) now switches itself to the Daily numbering type. A show already in your library, or one whose type you set yourself, keeps its type.
- A show that is already in your library can now be switched to **Only episodes that haven't aired yet** (or all, or none) on its page, with a count of what stops being monitored first. Nothing is deleted. Thanks to Bayharbourbutcher1 on GitHub, #77.
- An outside download client (qBittorrent, Transmission) can now get a category of its own for movies, shows, music and books, next to the single category. An empty one uses the single category as before. Thanks to Bayharbourbutcher1 on GitHub, #79.
- The seeding limits (global, or the per-indexer seeding rules) are now sent to an external torrent client when a torrent is added, so qBittorrent seeds for the time or ratio you set instead of you setting it by hand. Transmission gets the ratio only (it has no total seeding time). What the client does at the limit (pause, stop, remove) is set in the client itself. Thanks to Bayharbourbutcher1 on GitHub, #79.
- **People pages** (asked for by franciscojnneto on GitHub, discussion #72): cast, directors, writers and show creators on a movie or show page are now links to a person page with their photo, biography and the movies and TV shows they worked on, which you can add to your library like any Discover title.

### Fixed
- Anime files that Sonarr names by TheTVDB season and episode (`One Piece - S22E41 - Title.mkv`, Jujutsu Kaisen's seasons 1 to 3) are now placed on the right episodes when you import an existing library, move from Sonarr or import by hand, even when the name has no absolute number; the import note says how many were placed this way. Reported by SanketBaraiya on GitHub, #69.
- YTS and The Pirate Bay (and other sites that save a suggested API address with the form) were refused again after 3.1.3 with "the site definition wants to connect to ...". The address a site suggests itself is no longer taken for a login, so they work again (reported by SanketBaraiya on GitHub, #73).
- A library folder on a network drive that isn't connected is no longer written into. Before it imports, moves or renames anything under a library folder, Mediarium checks the folder is there, and a folder that was known to be the real one (it keeps a small hidden `.mediarium-root` file) but has lost that file is treated as a disconnected drive: the import fails with a plain message, nothing lands on the machine's own disk, and the dashboard's Health list names the folder. A folder that never got the file keeps working as before.
- Saving settings is now all or nothing: every value is checked first and then stored together in one database transaction, keys and passwords included. A save that is refused or fails part way no longer leaves half the changes behind.
- Members no longer see where the server keeps the library: the folder choice in the Add dialog lists the folders by name (movies, kids) instead of by path, and a title's folder is shown the same way. Administrators still see the full paths.
- A saved Usenet password, VPN key, media server token or notification password that can't be read (a `secret.key` that doesn't belong to the database) no longer makes its whole list fail or quietly turn into an empty value. The one that can't be read is marked on its Settings page and named on Health, it isn't used, everything else keeps working, and saving something else never overwrites it with an empty value. The log names them at start (never their values).
- A blank OpenSubtitles password next to an unchanged user name keeps the saved one exactly as stored, even when it can't be read.
- Regular new-release checks (RSS) no longer use up an indexer's daily request limit before your searches: they take about a third of it, and the indexer card says how often it checks.
- Releases whose title contains a number that looks like a year (Blade Runner 2049, 1917, 2012, Wonder Woman 1984 with its release year) are read with the right title and year, and a year in an episode title (an Olympics special from 2012) no longer confuses the show's name or year.
- Searches read each release name once instead of many times, which saves work on big runs.
- Mazepa and other sites whose definition cleans titles with many filters are no longer refused as "not supported yet" (reported by Apostol6 on GitHub, #16).
- Many more messages from the server (cloud backups, directory and single sign-on, subtitles, add-ons, indexers, importing, moving files, restart and update notes) are now translated.
- On a phone, the Size limits table in Settings > Quality fits the screen, and the release name hint in Add a download by hand is no longer cut off.
- What's new now names the version you are running and lists the changes of 3.1.2 and 3.1.3.
- The calendar's month arrows, the theme button in the menu and the Browse more links are bigger to tap on a phone.
- On a phone, the episode list of a show no longer lets the whole page slide sideways.
- Long button labels (Russian, German, Polish, French) now wrap inside the settings cards instead of running off the screen or widening the page.
- The message about the one-time setup code on the sign-up page is now translated.
- The avatar button in the header is big enough to tap comfortably on a phone.
- Unpacking a big RAR download is much faster on network shares and on file systems that run in a separate program, such as Unraid user shares (a 75 GB movie took hours there; reported by mirisbowring on GitHub, #76). The Activity page now shows how much has been unpacked, and pausing or stopping a download also stops its unpacking.
- Indexers that answer with a normal RSS feed, like nekoBT, no longer fail with "XML syntax error ... unexpected end element </link>". The feed's link elements were read as HTML and broke the parse (reported by SanketBaraiya on GitHub, #73).
- A request whose body was cut off or refused before it was fully sent (for example an oversized program update) was only answered after the upload time limit ran out, and the connection stayed open. It is now answered at once and the connection is closed.

## [3.1.3] - 2026-10-17

In short: a big round of fixes from a full review and from testing Mediarium with real libraries.
- Titles with accents or in other alphabets (Pokémon, Amélie, Japanese, Russian, Greek) are now found, matched and imported correctly.
- Password-protected RAR releases now open with their password, and a release whose link gives a login page is no longer grabbed again and again.
- Big libraries are much faster: the dashboard, Wanted (now in pages) and the Library tabs load in a fraction of the time.
- Security: the setup code can't be skipped any more, sign-in limits hold however fast the guesses come, and a site entry can't be pointed at your home network.
- Indexers with a daily request limit are no longer used up by Mediarium's own checks, and a learned limit can't lock an indexer out for good.
- Keep the original file names if you like, locate a renamed file, change a show's folder, set your own book cover, see the folder of a title's files, and see and sign out your other devices.
- Clearer messages in every language, a calmer layout on wide and small screens, and many smaller fixes listed below.

### Added
- Account changes (new accounts, role changes, turning accounts off or on, password resets, deleting accounts) and API keys made or removed are written to the log and the Activity page for administrators.
- **Signed-in devices**: Settings > Account & Access > Security has a "Signed-in devices" list and a "Sign out all other devices" button.
- **Keep the original file names** (asked for by Jisevind on GitHub, #75): a new switch under Settings > Library > File names and importing. Files keep the name they came with, while folders are still named after the movie or show; a file with a random name is named by the preset instead. Custom naming also gets the tokens `{Original Filename}` and `{Original Title}`.

### Fixed
- An indexer that once reported a small daily limit (or a wrong one) could stay held back for good. A limit is now forgotten after a week, dropped when a search works past it, and nonsense numbers or waits longer than 48 hours are ignored.
- The connection check of an indexer is no longer hidden for hours after a real search failed, or by an out-of-date daily limit.
- Security: a site list entry that needs no login can no longer be pointed at your own network through a web name that secretly points to it; any filled-in text or password setting now counts as a sign-in for this.
- Change folder and Locate the folder no longer take files that belong to a movie or to another show, or links that lead out of your library. Change folder waits while the show is downloading and uses the bigger file when two files are the same episode.
- Only administrators are shown the folder of a title's files.
- Stopping Mediarium now keeps to one time limit for all steps, so the database closes before Docker's 10 seconds are over.
- When secret.key is missing, the start-up message now says how to start fresh, and more kinds of saved secrets (VPN connections, notification secrets, media server tokens) are recognised.
- Movies and shows with accents in their name (Pokémon, Amélie, Élite) and titles in other alphabets (Japanese, Russian, Greek) are now found and matched; before, they were never matched to their releases.
- A release no longer gets taken for a title with no letters it can compare, so a Russian or Japanese release can't be downloaded for a different film from the same year.
- A TV episode with a word like French, Dutch, Greek or Dual in its title is no longer refused as a foreign-language release.
- The scheduled search for shows starts with the shows searched longest ago, and the book and movie searches carry on where the last run stopped, so a long list no longer keeps the same titles at the front forever. The movie search takes 50 movies a run.
- Adding a show with "future episodes only" or "none", changing its profile, or blocklisting one of its releases no longer searches for old episodes you didn't monitor.
- Torrents the indexer says have no seeders are only picked when nothing else is available, and more seeders win between equal releases.
- "Quiet for a while" no longer skips the searches that were due; they run when the quiet time is over.
- Picking a release from the search bar no longer adds a different film or show when TMDB has no title that matches; you get "couldn't find a match" instead.
- When every indexer is paused or at its daily limit, titles no longer get a "no releases found" note.
- Prowlarr and Jackett behind a reverse proxy folder now get the longer wait for slow feeds.
- The default quality profile keeps its custom formats when no default is chosen.
- a torrent whose import failed (for example a folder that wasn't writable) kept seeding and then couldn't be retried; it now stops, so Retry works.
- removing a download that was waiting for a decision about a file that already exists left its movie stuck on "downloading" until the next restart.
- audiobooks that come in several folders with the same file names (CD1, CD2) lost files on import; every part is kept now.
- archives inside the folder of a torrent or debrid download weren't unpacked, and the release was blocklisted for having no video.
- torrents that were already running when a VPN was connected, or the kill switch was switched on, kept sending from your home address; they are now stopped within about a minute and can be retried through the VPN.
- picking a season pack when only one episode was missing imported the biggest file instead of the missing episode.
- a full or read-only disk, or PAR2 running out of time, no longer puts a good release on the blocklist; the same for an error reported by qBittorrent or Transmission, whose torrent now stays where it is.
- Real-Debrid and TorBox downloads fetched every file, samples and notes included, without checking that the disk had room.
- choosing to keep the existing file removed the torrent from your own torrent client; it stays now.
- one failed connection to the main Usenet server sent that connection to the backup server for the rest of the download.
- the progress file of a Usenet download could get ahead of the data on disk after a power cut.
- a release can no longer plant the files the app keeps its notes in (this could make Mediarium remove an unrelated torrent from your own client).
- A saved indexer key that can't be read (usually after copying only `app.db` to a new install) now only switches off that indexer, and Health names it, instead of silently stopping every automatic search and saying "No indexer is set up".
- At start, Mediarium now says plainly when `secret.key` does not belong to the database.
- Saving settings with one wrong value now saves nothing, instead of keeping the values that came before it.
- Before an update changes the database, a copy of it is saved next to it as `pre-migration-<number>-<date>-<time>.db` (the newest two are kept), so restoring an older backup never replaces the copy from the real update.
- An older version started on a database that a newer version already changed now warns about it in the log and on Health.
- Half-written backups and locked cloud copies left behind when the container was stopped are cleaned up at the next start.
- The temporary copy of the database made for a backup is no longer readable by other users on the machine.
- Recycle bin folders left by a removal that was cut short now show up in the list and are cleaned up after the usual days instead of using disk space for ever.
- Several people opening the dashboard right after a restart no longer start several library scans at once.
- Databases on storage that can't use the faster journal now sync fully, which protects them better against a power cut.
- The Docker start script now says when `/config` can't be written and rejects a `PUID` or `PGID` that isn't a number.
- The example compose files now keep Docker's log file for Mediarium to three files of 10 MB.
- Importing a library whose folders are named in Japanese, Chinese, Russian, Greek or another non-Latin script no longer lumps every title of one year into a single entry, so none of your films or episodes are dropped.
- Cleanup no longer removes a movie as "never watched" unless one of your Plex, Jellyfin or Emby servers actually lists it, and a server that answers with no libraries (for example Plex before its drive is mounted) is no longer taken as nobody having watched anything.
- The Statistics page loads quickly on a big library, and the statistics snapshot no longer reads every file again each time.
- A Plex watchlist of an account that was turned off (or is read-only) no longer adds titles.
- One person searching the music database in a burst can no longer hold up everyone else's music lookups.
- The Subtitles wanted page is much quicker on a big library.
- Episodes that TMDB removes or renumbers no longer stay behind as missing; episodes you already have are never removed.
- Other people's accounts see a file's name instead of its full folder path on movies, episodes and music.
- The calendar no longer lists every old movie release, and the Backdrops pictures are fetched for the nearest dates first.
- Subtitles from add-ons in Chinese, Japanese, Korean and Thai are converted to UTF-8 correctly instead of showing garbled text.
- A subtitle add-on can no longer make Mediarium read files from other machines on your home network.
- An add-on on the same address as Mediarium can no longer be shown inside a frame; it opens in a new tab.
- Music and book folders that are links to other folders are now scanned, and one unreadable music folder no longer stops the whole scan.
- Notifications go to all your targets at the same time, a service that says "slow down" gets the message again a moment later, and messages held back during quiet hours go out one by one.
- "Remove these now" in the cleanup rules removes exactly the titles shown in the preview.
- A bad TMDB key no longer produces one failed request and one log line for every show on each refresh.
- Looking up which movie or show owns a file is faster on big libraries.
- Book release matching and Kavita/Audiobookshelf lookups now work for titles in non-Latin scripts.
- sign-in limits could be passed by sending many guesses at once; now the five tries per address and 25 per account count however fast they arrive.
- on a new Docker install, anyone reaching the sign-up page through Docker's port forwarding could skip the setup code by sending a made-up address; the code is now always asked for on those visits (a visit from the same computer too). It is in the log: `docker logs mediarium`.
- if Mediarium could not read whether an account has two-step sign-in, a right password signed in without the code; it now refuses and asks to try again.
- turning an account off now also stops its Plex watchlist from adding titles and stops request messages going to it.
- people who are not administrators no longer see folder paths on the server in the Activity page and in a title's history, only file names.
- a flood of single sign-on or passkey sign-in starts can no longer lock real sign-ins out.
- typing a name like `code:1` as a user name can no longer lock another account's two-step or password check.
- Accounts that are not administrators no longer see the folder and tag controls on a title's page unless they may manage the library, and the "Download from" default now shows the right choice for them.
- Typing a tag that starts with an existing tag (for example "Kids movies" when "Kids" exists) no longer adds the shorter tag half way through.
- On phones and tablets the closed menu can no longer be reached with the Tab key or a screen reader, and the open menu closes with Escape and keeps the keyboard inside it.
- The Copy buttons for the two-step recovery codes and the share dialog now work when Mediarium is opened over plain http.
- The "site definitions" link in the indexer picker opens the community list instead of a not-found page.
- Years and port numbers no longer show a thousands separator (2,020 or 52,100).
- Playing music no longer slows down artist and album pages with many tracks.
- Tab, filter and view buttons now tell screen readers which one is chosen.
- The search box's arrow keys follow the order the results are shown in, and screen readers hear the highlighted result.
- A refresh of the file list no longer stops a video that is playing if the refresh fails once.
- The migration page keeps updating after one slow answer.
- Search for all missing albums on an artist asks first for long lists and carries on if one search fails.
- The Activity page no longer fetches the history and blocklist every 3 seconds while you are on another tab.
- Activity messages, download errors and blocklist reasons are translated in every case.
- Messages pop-ups stay while you point at them.
- Opening a shared Activity link to a tab you cannot see now shows the queue.
- The red line under a form's button ("A field above needs fixing before you can ...") is now a proper sentence in every language, instead of a broken one in Italian, Dutch, Russian, Ukrainian, Polish and Portuguese (Portugal).
- The dashboard's "Needs your attention" messages (missing TMDB key, no indexer, folder problems and so on) are shown in your language once translated.
- The "Edit profile" form under Settings > Quality is no longer right-aligned.
- The version badge in the sidebar no longer hides the last menu items on a short screen.
- "What's new" opens at the top on a phone, even after skipping several versions.
- The memory line in the dashboard's server card wraps instead of being cut off on a phone.
- Activity now says it cannot reach the server, with a Try again button, instead of showing zeros and "Nothing is downloading".
- The "slow to answer" message no longer shows twice at the same time.
- Several settings fields now have a name for screen readers.
- The music playlists page and the bookshelf no longer ask the server while music or books are switched off.
- Typing wrong passwords to confirm a change (turning off two-step sign-in, adding a passkey, linking single sign-on) no longer stops the administrator from signing in from the same address.
- Choosing keep or replace on a download that has already gone from the list now says so instead of showing a server error.
- A connection that starts sending a request and then goes silent is now closed after a minute instead of being held open; backup, cover and NZB uploads still get up to half an hour.
- with a proxy on a port from 40700 to 40799, connection problems were always reported as a wrong user name or password; the real reason is shown now.
- Connecting to a VPN server whose address is IPv6 from a Docker container without IPv6 now says so straight away and what to do about it, and a server name that has both IPv4 and IPv6 addresses uses IPv4 (and tries IPv6 if IPv4 doesn't answer) instead of hanging.
- The release list and other wide tables no longer squeeze short columns so that "Age" breaks into two lines or sizes like "12.1 GB" wrap. The release name takes the remaining room, and the table scrolls inside its own box only when the window is really too narrow (reported by SanketBaraiya on GitHub, #74).
- Folder names no longer overlap the free-space text in the Folders list of the Dashboard's server card (reported by SanketBaraiya on GitHub, #74).
- The Dashboard's Connected folders now lists only the media types that are switched on, with one line naming the ones that are off.
- Scrollbars now match the app's look, the sidebar menu scrolls above the version badge so it never covers an entry, and long page names in the top bar end in an ellipsis instead of running under the search box.
- Password-protected RAR downloads now open with the password from the NZB or the `{{password}}` after the release name. Before, every encrypted RAR was refused, even with the right password. A wrong password now says so instead of just "password protected".
- When an indexer answers a download link with a sign-in or maintenance page, Mediarium no longer grabs that release again on every automatic search. It leaves the release alone on that indexer for six hours, the Activity row says so, and you get the "failed" notification once instead of every time.
- With "If a file already exists" set to keep the existing file (or Skip in Activity), the download now ends as "Kept the file you already have." instead of Failed, and no "Download failed" notification is sent.
- Downloads waiting because the disk is nearly full now say so on their Activity row.
- The disk-full message during a download no longer starts with a technical article id.
- Testing a news server with a self-signed SSL certificate now explains the problem in plain words.
- "Blocklist & search again" keeps the reason the release was bad and adds that you blocklisted it, instead of replacing the reason.
- A download interrupted by a restart, and one you resume, now leave a line in the title's history.
- The harmless "database is closed" warning when Mediarium stops during a download is gone.
- Logs and errors files an indexer's sign-in or maintenance page as its own problem with matching advice, instead of "A download failed".
- The "file already exists" help and its link now point to Settings > Library > File names and importing, where the setting is.
- The API reference now lists the routes for adding movies, shows, books and artists, following authors and book series, and Retry.
- The API key box in the add-indexer form has its own label for screen readers.
- Big TV libraries: the Dashboard no longer takes seconds or fails with "Mediarium is busy right now", and its database queries stop when a request gives up.
- Big TV libraries: Wanted and Activity load the wanted list a page at a time, one line per show, instead of every missing episode at once (93 MB in one report). Scripts that call `GET /api/wanted` without paging get the same full list as before.
- Big TV libraries: the Library reads only the tab you have open, and the TV list answers quickly when you open it again.
- Look for missing episodes on a big TV import no longer blocks other saves for half a minute.
- A problem that happens while the database is busy is saved a few seconds later instead of being lost.
- Music import: a damaged or zero-filled audio file no longer stalls the scan for hours. Its tags are given up on after a few seconds, it's matched by its name, and the album says so. You can stop a scan, and it shows again when you come back to the page.
- Library import: Check match rows now start on the film with the folder's year (and the film's original title counts), and adult titles never come first.
- Library import: daily shows named by air date (`Show - 2024-01-15 - Guest.mkv`) are no longer skipped. Each file goes on the episode that aired that day, in the TV import and in Change folder.
- Books import: importing audiobooks no longer clears the ebook import's list, so Find it myself stays available for the ebooks that weren't found (and the other way round).
- Books import: `Author - 1984.epub` and titles with accents or apostrophes such as `L'Étranger` are found on Open Library, and Find it myself has separate title and author boxes and lists that author's books first.
- Translations: the import report's notes, the Change folder message, the area names and problem texts in Settings > Logs, and the dashboard's folder cards (folder names, counts, file sizes) now follow the chosen language.
- Library import: the TV report no longer counts shows that were already in your library as added.
- Library import: two folders of the same film in one import are reported as the same film with the copy that's in use, a second copy next to it is named, and a release by a group called OTHER is no longer dropped as an extra.
- Import banners no longer stack up after several imports: only the newest finished one of each kind shows.
- Locate the file and Look in a different folder read the new file's quality instead of keeping the old one's.
- The Monitored switch, the dashboard greeting and the Rescan message no longer contradict what's on screen for titles that aren't monitored or have things to fix.
- After an update, What's new now opens first and the question about anonymous usage numbers waits until it is closed, instead of both opening on top of each other.
- After restoring a backup from Settings, the sign-in page now says the backup was restored.
- Members and viewers no longer get "not allowed" errors in the browser console from the dashboard asking for settings and server numbers only administrators can see.
- A notification that can't be delivered is now logged as a warning that names the notification and its type.
- If secret.key is missing, the start-up message now names the folder Mediarium is actually using instead of always saying /config.

## [3.1.2] - 2026-10-16

### Added
- **Daily limits for indexers** (reported by anguyen1776 on GitHub, #61): Mediarium now learns an indexer's daily limit from its "Request limit reached" answer, stops automatic searches two requests before it, shows "12 of 25 requests today" on the indexer's card and uses the indexer again after midnight (UTC). A search you start yourself still asks the indexer and warns you. A new optional **Requests per day** setting on each indexer (also `requestsPerDay` in the settings file) is there for a limit you know.

### Fixed
- Free indexers with a daily request cap (such as Drunken Slug and Althub) are no longer used up by Mediarium's own connection checks: the check no longer runs a search, skips an indexer that has just answered a real search, and asks at most every 6 hours (reported by anguyen1776 on GitHub, #61).

## [3.1.1] - 2026-10-16

### Added
- **More subtitle languages** (asked for by nexus4004 on GitHub, through the website idea form): Croatian, Serbian, Bosnian, Slovenian, Slovak, Bulgarian, Ukrainian, Catalan, Indonesian, Thai, Vietnamese and Hindi. A Serbian subtitle is read correctly whether it is written in Latin or Cyrillic letters. You can also choose Croatian, Serbian, Bosnian, Slovenian or Bulgarian as an audio language, and releases tagged for them are recognised.
- **Rescan files on a movie that has no file** (asked for by nexus4004 on GitHub, through the website idea form): if you put the video in the movie's folder yourself, Rescan files finds it and adds it, and tells you which file it found or which folder it looked in.
- **Library in the left menu** (asked for by nexus4004 on GitHub, through the website idea form): Movies, TV shows, Music, Ebooks and Audiobooks are listed under Library, so you can open a tab directly. Only the kinds that are switched on are listed.
- **Change cover for books** (asked for by varnakube on GitHub, #68): a book with no cover, or the wrong one, can get a cover of your own from its page. Upload a picture, paste a link, or pick the cover of another edition when Open Library has one. **Remove my cover** brings the automatic one back.
- **Only take releases in these languages** (reported by mirisbowring on GitHub, #67): a new switch under Settings > Library > Quality > Audio language, off by default. With it on, releases with no language in the name, releases tagged English only, and MULTi or DUAL releases that don't name one of your languages are skipped, so someone who wants only German no longer gets an English release as a last choice.
- **Search under other titles** (asked for by mirisbowring on GitHub, #67): if you want audio in another language than English (German, Italian, French, Spanish and so on), Mediarium also searches your indexers under the title's other names from TMDB, such as "Der Herr der Ringe", and recognises a release named by one of them. The release list says which title it was found under. Nothing changes when English is the only language you want.
- **Folder for its files** (asked for by nexus4004 on GitHub, #71): a movie's and a show's page now show the full folder where its files are, or will go, with a Copy button, so you can find it to put files there by hand. Under Docker it is the path inside the container, and the hint says so.
- **Locate the file / Change folder** (reported by dxtrlws on GitHub, #71): if you renamed or moved a movie's file, **Locate the file** on the movie page lets you pick it from your library folders. If you renamed a show's folder, **Change folder** on the show page reads the episodes in the new folder and tells you how many it found and how many are still missing. Nothing is moved, renamed or deleted.

### Fixed
- When Mediarium was slow to answer, a bar kept appearing and disappearing at the top and pushed the page down. It is now a small notice in the bottom right corner that only shows after the slowness has lasted a few seconds, stays while it lasts, and goes away by itself. (Reported by nexus4004 on GitHub, through the website idea form.)
- **Anime numbered by TheTVDB seasons** (reported by SanketBaraiya on GitHub, #69): anime that Sonarr numbers by TheTVDB seasons (such as One Piece, `S23E10 - 1100`) is now imported from an existing library, from a Sonarr move and by hand. The file is placed by the episode number counted from the first episode in its name, and a file that can't be placed says why and what to do. Import the folder again to pick up files that were left out before.
- If the encryption key file (secret.key) is missing but the database already has saved passwords or keys, Mediarium now stops with a clear message telling you to put your old secret.key back, instead of writing a new key that cannot open anything. A saved TMDB, OpenSubtitles or Trakt key it cannot read no longer stops the app from starting.
- Book covers under "Other editions" now show up when Mediarium is served under a sub-path (URL_BASE).
- The calendar's backdrops and the other-language titles no longer get stuck behind titles that TMDB has removed or keeps failing on.
- Stopping or restarting Mediarium now stops downloads and the built-in torrent client cleanly and never waits longer than a few seconds on background jobs, so downloads that were running come back as interrupted rather than failed.
- If saving the first-run legal acknowledgement failed right after creating the admin account, setup now carries on instead of getting stuck on "An admin account already exists".
- After closing a confirmation or notice dialog, keyboard focus goes back to the button that opened it.
- Restore errors on the Backup page are now translated.
- An APP_PORT that is not a valid port is now reported in the log, and `app config` finds the port from APP_PORT_FILE. URL_BASE can no longer start with help, apps, opds or kobo.
- In the release list, the "Matched on the title" line wraps on narrow screens, and the age tooltip uses the app language. Screen readers announce error messages as alerts, and each other-edition cover button has its own label.
- Two manual book matches of the same file at the same moment no longer leave the book pointing at a file that is not there, and Rescan files on a movie only takes a folder whose year fits the movie.
- Fixed the YTS site failing its Test with a message about "movies-api.accel.li": public sites where you have entered no login, cookie or key can now search on another public address, while sites with a login or key still only talk to their own addresses (reported by SanketBaraiya on GitHub, #73).

## [3.1.0] - 2026-10-14

### Added
- **The settings file now holds your lists too** (asked for by jdamner on GitHub, #43): indexers, Usenet servers, media servers and quality profiles can be in the YAML file, each item matched by its name. A missing one is added, a different one is changed, and one that is not in the file is left alone. `config export` writes them, `config diff` and `config apply` use them. Passwords and keys stay `${NAME}` from the environment.
- **Speed schedule** (inspired by TryToTilt's fork on GitHub): Settings > Downloading > Download rules has a new box where you add lines of "on these days, from this hour to that hour, limit to this speed". The first line that fits the moment sets the limit, so downloads can be slow on weekday evenings and unlimited at night.
- **Weekly summary** (an idea from TryToTilt's fork on GitHub): once a week Mediarium can send one short message with what arrived, what failed and what is still missing, to the notification targets that have **Weekly summary** ticked. It is off until you pick a day under Settings > Notifications, and **Send one now** shows what it looks like.
- **"Start newer releases first"** (asked for by 12nick12 on GitHub, #49): a switch under Downloads at the same time. With it on, among the downloads Mediarium finds by itself, the newest movie or episode starts first. Downloads you add by hand, or move to the front, still go first.
- **Run the books that were not found again** (asked for by thegliffy on GitHub, #42): after a books import, a button on the import card looks again only at the books that were not found, without reading the folders again, for when Open Library or Audible did not answer the first time.
- **Password-protected ZIP downloads** (a follow-up to the password support for RAR and 7z, asked for by 12nick12 on GitHub, #44): a ZIP with a password in the NZB or after the release name (`{{password}}`) is now opened with the same password. Without the password, or with a wrong one, the download fails as "password protected" like before.
- **Page-turn animation in the ebook reader** (an idea from TryToTilt's fork on GitHub): under **Aa** you can choose how a page turn looks (slide, fade or none) and how fast it is. It is off by itself when your device is set to reduce motion.
- **Backdrops view in the calendar** (an idea from TryToTilt's fork on GitHub): next to Posters, each release can be a wide still with the title's logo over it. The pictures are fetched from TMDB in the background, a few at a time, and a title shows its poster until its picture is there.
- **Quiet for a while** (an idea from TryToTilt's fork on GitHub): three buttons under the automation numbers (1 hour, 4 hours, until morning) hold the heavy background work, the searches, refreshes, subtitle sweep and picture fetching, so a small server isn't busy while something plays. Downloads, imports and backups carry on.
- **Add-ons**: Settings > Connections > Add-ons keeps links to other programs you run next to Mediarium (a channel guide, a dashboard). They show in the players menu and open in a new tab or inside Mediarium. Mediarium doesn't run them or load their code; give one a read-only API key if it should read your library.
- **A better installed app and tablet layouts**: the app has shortcuts (long-press the icon for Activity, Calendar, Library and Discover), a proper icon for round and squircle masks, and a "Mediarium can't be reached" page (in your language) with a Try again button when the server is down, instead of the browser's own error. Tablets get a narrower menu in landscape, a wider menu drawer in portrait and bigger touch targets; foldables open flat keep clear of the hinge. See [Using it as an app](docs/INSTALL.md#using-it-as-an-app-on-a-phone-tablet-or-computer).
- **Comics**: `.cbz`, `.cbr` and `.cb7` files go in the ebooks folder like any book, are named from their `ComicInfo.xml` (series, number, writer, year), open in the reader page by page (one page or two, fit to the page or the width, left to right or right to left for manga, with the page-turn animation) and keep your place. See [Comics](docs/books.md#comics).
- **More screen languages**: Italian, Dutch, Polish, Russian and Ukrainian, next to Spanish, German, French and the two Portuguese. Polish, Russian and Ukrainian get their own plural forms (2 to 4 differs from 5 or more).
- **Messages from the server in your language**: error messages, the notes in the Activity list and the results of checks now appear in the screen language (about a thousand sentences, in all eleven languages), where before they were always English. A sentence Mediarium has no translation for is shown as it came.
- **Send indexer traffic through the VPN** (asked for and written by spamoom on GitHub, #63 and pull request #62): a new switch under the kill switch in Settings > Downloading > VPN and proxy. Searches, RSS, release downloads, IRC announces and debrid then go through the built-in VPN, and wait while it isn't connected instead of going without it. Indexers on your own network still connect directly. Off by default. See `docs/vpn.md`.
- **Separate default quality profile for TV** (asked for by anguyen1776 on GitHub, #60, and a Reddit user): under Settings > Library > Quality, **Make default for TV** gives shows a default of their own while movies keep theirs. Nothing changes until you set one. The library import and the migration lists also tag each title as Movie or TV, and each season on a show's page has a **Find missing** button that searches only that season's missing episodes.
- **Find it myself for books** (asked for by varnakube on GitHub, #64): each book a books import could not find now has a button to search Open Library by hand and pick the right one, with an optional **Rename the file to match**.
- **Replace with a smaller version** (asked about by mirisbowring on GitHub, discussion #65): if you moved a movie to a lower quality profile (4K to 1080p, say), press it on the movie's page, or use **Replace with smaller** in Library > Select. Mediarium finds a smaller release your profile accepts and only removes the big file once the new one is in your library.

### Fixed
- **Indexers saying "too many requests"** (reported by anguyen1776 on GitHub, #61): the automatic search for missing movies and episodes asked every indexer for every title one right after the other, hundreds of requests in a minute with a big library, which Newznab indexers answer with "too many requests" and a pause. Searches on one indexer are now spaced at least a second and a half apart (six seconds for an indexer that has already complained), and a search by hand still starts at once when the indexer hasn't been asked just before. A big library takes longer to go through, never gets the indexer paused.
- Opening Mediarium by the server's public IP address now always asks for the setup code before the first account is made, also when Docker or Podman hand the visit on from a private address.
- Deleting an account, switching off someone's two-step sign-in, and making an e-reader password, Kobo address or calendar feed now need you signed in on the web page; an API key can't do them.
- Wrong tries at your own password when adding a passkey, linking single sign-on or changing two-step sign-in now count per account as well as per address.
- With password sign-in turned off and directory sign-in on, the sign-in form no longer shows which user names have an account here.
- Single sign-on only links a first sign-in by email to an address an administrator set on the account, never one a person typed on their own profile. If you use this, save those addresses again under Edit.
- A new password set by an administrator now also revokes that person's API keys, e-reader password, Kobo address and calendar feed.
- Changing the Not interested list now needs the right to add that kind of title yourself.
- `reset-password` accepts the user name in any letter case and works with a piped-in password.
- Many identical requests sent at once by one person now make just one request, and can't get past the request limit.
- The torrent engine's own log lines are now cleaned of passkeys and other secrets like the rest of the log.
- Cloud backup settings no longer reuse the saved key, password or passphrase when you point them at another storage address or bucket; you type them again.
- An unlocked copy of a passphrase-protected backup left behind by a crash during a restore is now removed at start-up.
- The encryption key file is now written safely on first start, so a crash can't leave half a key.
- Picking a release no longer reads the whole download history while other grabs wait.
- Searching Activity for text with % or _ now finds exactly that text.
- One person can no longer use up the problem-report limit for everyone; administrators can always send theirs.
- Reset my ID no longer sends a second usage message on the same day.
- Push notifications no longer cut a letter in half, and very long titles are shortened.
- Indexers: a broken or hostile site definition can no longer crash Mediarium or tie up a search for minutes. Rows, templates, regular expressions and nested pages are now kept within limits, and a crash in one indexer's search only costs that indexer's results.
- Indexers: when searches for one indexer are already lined up for more than 10 minutes, a new search skips that indexer instead of waiting for hours, and cancelled searches no longer hold back the ones after them.
- Indexers: a site from the definition list only gets your login or key at the address you chose (or its own domain), not at other addresses the definition lists. Links and redirects from a site on the internet can't point Mediarium at your own machine or home network.
- Indexers: very large Newznab/Torznab replies are read item by item and stop at 5,000 releases. Dates far in the future and impossible sizes are ignored, and a site reply over 16 MB now shows an error instead of quietly giving fewer results.
- Torrents: a magnet link nobody shares now gives up after 10 minutes and the next release is tried, instead of holding a download place for good.
- Torrents: a full disk or a VPN change no longer gets a good release blocklisted as "no one is sharing it"; the download stops at once with the real reason.
- Usenet: an indexer's "request limit reached" answer, or a login or maintenance page, no longer blocklists the release you grabbed.
- Usenet: with a single Usenet server, one connection that drops no longer fails the whole download while the other connections work.
- Usenet: an article that arrives damaged is fetched from your next server instead of being saved as it is.
- Activity: pausing or stopping a download that had just finished no longer turns it into a paused or stopped one that downloads again.
- Activity: Blocklist & search again on a paused download, or one waiting for your decision, now frees the title and deletes the partial files.
- Activity: choosing to keep the existing file on a conflict leaves the movie as downloaded instead of missing (which made it download again and again).
- Library: a download never replaces a file that belongs to another movie or episode (such as a remake with the same title and year), and two files of one season pack can't overwrite each other.
- Media servers: downloads paused while someone was watching now carry on if the media server goes away or is removed.
- Other apps: searches sent by Radarr/Sonarr/Lidarr-style apps run one after another, so a long list no longer holds up your own searches; one command may name at most 200 titles.
- Downloads: with "start newer releases first", season packs, albums and books take their turn, and a resumed download goes back to the front of the line.
- Downloads: files brought home from Real-Debrid or TorBox keep to the speed limit; a brief hiccup talking to a download client or debrid service no longer fails the download.
- Downloads: the speed schedule applies within a minute of the hour, and safe mode still starts a download you started yourself when the download hours begin.
- Books: automatic retries after bad releases have the same daily limit as movies and shows.
- Activity: Retry on a failed download that keeps its files no longer loses them if Mediarium restarts while the retry waits in line.
- After you changed the folder of a movie or show, deleting, removing, renaming or rescanning the files it already had now works on the folder they are in, instead of being refused or proposing a move to the new folder.
- Renaming files to match the naming setting no longer leaves the library pointing at the old name when a subtitle or other side file can't be renamed: what was renamed is put back.
- Rescan files no longer marks a file missing when the disk gives an error or when two files with the same name and different extensions are there.
- With the proxy in Settings switched on, links from indexers can no longer reach the cloud metadata address through the proxy; pages FlareSolverr opens are checked the same way.
- A saved token or password is no longer kept when a notification, indexer or media server address changes from https to http, when an ntfy server is emptied (it then falls back to ntfy.sh), or when an email server's encryption is switched off.
- More ways of writing a local or metadata address inside an IPv6 address (local-use NAT64, Teredo) are now refused.
- Debrid downloads stop as soon as the service sends more than the file's size.
- An add-on address with spaces, quotes or semicolons is refused.
- Settings file: a Usenet server (or indexer or media server) added from the file is now switched on unless the file says `enabled: false`.
- Settings file: quality profiles name their fallback profiles and custom formats, so a file works on another install.
- Settings file: a typo under an indexer's `settings:` is now reported, and values Mediarium can't change on an existing indexer no longer show as a change on every run.
- Pressing Send one now for the weekly summary no longer cancels that week's scheduled summary.
- Security: a .7z download can no longer hide a link or an oversized file after the first 64 KB of its file list, or behind a file name with a line break in it. Such archives are refused before anything is unpacked.
- Unpacking a download again no longer damages a file that was already imported into the library as a hard link.
- Comics: .cbr and .cb7 comics now open in the reader and get their cover. Mediarium goes by what the file really is, so a .cbr that is really a ZIP works too.
- Comics: unpacked comics now take at most 2 GB of cache in total (the ones opened longest ago go first), and pages load with less memory.
- File names: a title with no name now gets a `tmdb-<id>` folder and file instead of an empty folder and a hidden file, and names no longer start with a dot (which hid them).
- File names: switching the naming style to Custom now checks the custom format you saved earlier, and an episode format must include {Season} and {Episode}.
- Imports: two imports to the same place at the same time no longer delete each other's half-copied file, and a broken link where the file should go is now handled by the "if a file already exists" setting.
- Importing a books folder laid out the usual comic way (one folder per series run, one file per issue) now adds every issue, with the series and issue number read from the file name. Before, only one issue per folder was added.
- An odd year in a comic's ComicInfo.xml (like "2021-05") no longer throws away the rest of its details.
- Lining up a subtitle with another one no longer runs for minutes on very long or dense subtitle files. It stops with a message instead, and stops when you leave the page.
- Covers are resized with less memory, so very large cover pictures can't use up the server's memory.
- A library scan now follows symlinked folders, and a library folder that is itself a symlink.
- Moving over from Radarr, Sonarr, Medusa or SickChill no longer links a whole library folder's files to one title when the other app reports a library folder as a title's folder. That title is skipped with a reason.
- Moving over from other apps checks indexers and Usenet servers the same way the settings pages do, skips one SickChill show that can't be read instead of failing the whole import, and refuses a redirect that switches between http and https.
- A TMDB show that lists the same season twice no longer gets its episodes twice.
- TV releases named "Show S01 E01" or "Show.S1.E1" are now read as one episode instead of a whole season, so they no longer satisfy a season search or fail the size check as a "pack".
- Packs of several seasons ("S01-S03", "S01-03") are no longer grabbed as if they were season 1 only.
- Release names like "S01E01.E02", "1x01-1x03" and year-numbered seasons ("S2024E01") are now read correctly.
- The release group is now read correctly when a posting tool added "-postbot", "-Obfuscated", "-xpost" and the like, or a site tag such as "[rarbg]" after it, so preferred and blocked group rules work on these releases.
- A release name that starts with its season ("Season.2.Show.1080p") now gets its show title.
- Book, comic and music covers now show when Mediarium runs under a URL base (a sub-path behind a reverse proxy).
- The single sign-on button on the sign-in page works under a URL base.
- Opening Mediarium no longer removes the service workers of other apps on the same domain, such as Jellyfin under its own path.
- Reopening a comic and leaving without turning a page no longer resets your place to page 1.
- Translations: Russian and Ukrainian no longer use the "this one" sentence for 21, 31, 101 and so on, French no longer uses it for 0, and more counts use the right plural form (in English too: "Search for 1 title now?").
- A very long error from the server can no longer freeze the page for readers using a language other than English.
- When your session ends (it expired, or you were signed out elsewhere), Mediarium shows the sign-in page and takes you back to the same page after you sign in.
- In the book reader, the arrow keys on the place slider and Space on a button no longer turn the page as well.
- The audiobook player keeps your place when you leave it with the Back link.
- A settings switch no longer flips back after a save when you click it right after coming back to the tab.
- A push notification without a link opens Mediarium, not the root of the domain, under a URL base. The error page's link does the same.
- The "can't be reached" page is now in Italian, Dutch, Polish, Russian and Ukrainian too, and follows the language you chose in Mediarium.
- Report on GitHub no longer puts the support report in the link. It is copied for you to paste instead.
- Connecting Plex (or Jellyfin Quick Connect) stops waiting when the code runs out and shows the success message only once.
- Signing out clears the music queue kept in the browser.

## [3.0.5] - 2026-10-13

### Fixed
- **A 7z archive with a password was refused even with the right password** (found while testing 12nick12's report on GitHub, #44, with a real archive): the check that runs before unpacking stopped at "password protected" for any 7z whose files are encrypted, before the password was ever tried. It now tries the password you gave (from the NZB or `{{password}}` after the release name), and an archive that also hides its file names fails as "password protected" when there is no password, as it should.

## [3.0.4] - 2026-10-13

### Added
- **Pause downloads while someone is watching** (asked for by yannicschuller on GitHub, #57): on the media servers page, **While someone is watching** pauses running and waiting downloads when someone is watching on Plex, Jellyfin or Emby, and lets them carry on a minute after the last person stops. It only resumes what it paused itself. Off by default.
- **Rescan files** (reported by blind0wl on GitHub, #59), on a show's and a movie's page: looks at the files on your disk again, follows a renamed file, and makes a title wanted again when its file was deleted by hand. It never believes an empty or missing library folder.
- **Remove several downloads at once** in Activity (#59): tick downloads (also failed and stopped ones), **Pick paused** picks all the paused ones, and **Remove** takes them off the list.
- **Delete one file, an episode's file or a season's files, and keep the title** (asked for by blind0wl on GitHub, #59): **Delete file** on a movie and on an episode, and **Delete files** on a season. The files go to the recycle bin and the movie or episodes are wanted again, so a new release can take their place. A season pack you pick yourself for a season you already have is no longer refused with "no episodes to fill": it goes through the import settings, and the message that explains a skipped import now says how to replace the files.
- **Activity shows what each download is** (suggested by yannicschuller on GitHub, #56): under a download's name there are small tags for the picture size, the source, the video codec and the audio language, read from the release name.
- **Tdarr and similar apps can tell Mediarium a file changed** (asked about by neoblitz on Reddit): the Radarr and Sonarr "rescan" and "refresh" requests are no longer ignored. Mediarium looks at the title's files again, follows a file that only changed its extension, and asks your media servers to scan the folder. See `docs/other-apps.md`.

### Fixed
- **Giving a quality profile to many titles at once no longer queues a download for every one** (reported by blind0wl on GitHub, #59): every title whose file was below the new profile was searched for at the same moment, and the limit of 10 an hour didn't apply. Now the first 10 are searched for right away and the scheduled search does the rest.
- **A better version found by "Look for better versions" now replaces the old file** (reported from inside the app): with the default setting for "If a file already exists" the better file was downloaded and then refused with "files for this release already exist in your library, so nothing was imported". A title that may be upgraded now takes a better file in place of the old one; a file that is not better, or a title set to leave its file alone, is still skipped.
- **A fix to IPTorrents (or Toloka) now reaches installs that already have the site list** (reported by xrandallx1 on GitHub, #28): the definitions that ship with Mediarium were only added to the saved site list when it was made, so an install that had saved it before kept the old copy of IPTorrents even after updating. The saved list is now fetched once again when the shipped definitions have changed.
- **The Updates card no longer says an old installed update is "waiting for the next restart"** when the Docker image is newer: Mediarium ignores such an update at start-up, and the card now says so.

## [3.0.3] - 2026-10-12

### Added
- **Keep your settings in a YAML file** (asked for by jdamner on GitHub, #43): `app config export` writes your settings as YAML, `app config diff FILE` shows what a file would change and `app config apply FILE` makes it so, through the same checks as the Settings pages. It covers the settings that are one set of values (general, library, downloading, the torrent client, the debrid service, the proxy and the update options); passwords and keys are written as `${NAME}` and read from the environment. Lists such as indexers and quality profiles are not in it yet. Nothing is read at start-up. See `docs/config-file.md`.
- **Transmission as a download client** (asked for by acortespr06 on GitHub, #53): Settings > Downloading > Torrents > Use your own torrent client now lists Transmission (version 3 or newer). Mediarium sends torrents with a label, follows them, pauses, resumes and re-checks them and removes them after the import, like it does with qBittorrent. Tested against a copy of Transmission's RPC, not yet against a real Transmission.
- **Ideas for the movie and show pages** (suggested by MrBanditt on GitHub, #54): a show's page says whether it is **Ended**, **Continuing** or **Upcoming**, and an ended show has a red **Ended** ribbon in the Library; every episode shows the size of its file and each season the total; the release list has an **Age** column you can sort; the release list scrolls into view when a search answers; the **TMDB** link (and IMDb and the site) stay on a title's page after it is in your library; and the Library sorts titles without a leading "The", "A" or "An".

### Fixed
- **IPTorrents: a signed-in member is recognised** (reported by xrandallx1 on GitHub, #28): the site sends a signed-in member from the front page on to the torrents page, and its "Log out" tooltip is not always inside a link, so the sign-in check found nothing. It now also accepts the tooltip and the torrents table.

## [3.0.2] - 2026-10-11

### Fixed
- **The explanations in "Move from other apps" are in your language** (reported by DFlexy on GitHub, #17): lines such as "Mediarium's site list has no ..., so it stays on Prowlarr", "already in your library" or "switched off in Prowlarr" were always English. The common ones are now shown in Spanish, German, French and both Portuguese; any line the list doesn't know stays as the server wrote it.
- **Two artists with the same name no longer get mixed up when you scan your music folder** (reported by barnetfan on Reddit): when the music database has two artists called the same (two bands named Blue Ruin), the one that has the albums found in your folder is now chosen, instead of whichever the search lists first. An artist already in your library counts too.
- **An album you don't monitor no longer says its tracks are "missing"**: they say "not monitored" instead, and "missing" is kept for albums you want.

## [3.0.1] - 2026-10-11

### Added
- **Send a download to the front, and pick several downloads at once** (asked for by 12nick12 on GitHub, #49 and #51): in Activity, a waiting download has **To the front**, and the small box on each download's picture lets you pick several and **Pause**, **Resume**, **Stop** or send them to the front together. A download that can't be changed is named and the rest still go through.

### Fixed
- **IPTorrents sign-in now works with the site's real pages** (reported by xrandallx1 on GitHub, #28): the site's sign-out link is an icon with a "Log out" tooltip, not a plain /lout.php link, so a good cookie was refused. Mediarium now recognises the link by its words as well as its address.
- **A rejected sign-in cookie now says what the site showed instead** (IPTorrents, reported by xrandallx1 on GitHub, #28): a browser check, its own sign-in page, or another page, so you can tell what to fix.
- **Slow Prowlarr and Jackett feeds no longer time out** (reported by DFlexy on GitHub, #18): a feed that Prowlarr or Jackett serves for a site they read page by page can take 20 to 40 seconds to answer. Mediarium gave up after 15, so the Test button and searches failed even though the feed worked in Prowlarr. Those feeds now get up to 60 seconds; indexers' own APIs keep the short wait.

## [3.0.0] - 2026-10-11

### Requested features
- **More than one folder per kind of media**, as in Radarr and Sonarr (asked for by u/joshpennington on Reddit): add more movie and TV folders under Settings > Library > Folders and file names (another drive, or kids, documentaries, concerts). The Add dialog and each title's page choose which folder a title is kept in; imports, renaming, the recycle bin, disk checks and the Radarr/Sonarr compatible root folder list all know about them.
- **A Download clients tab in Activity** (asked for by blind0wl on GitHub, #38): one place to see and control every downloader. The built-in Usenet downloader and the built-in torrent client come first as two cards you can show side by side or one at a time, each with its state, what it is doing and every download with the same buttons as the queue (pause, resume, stop, retry, remove), plus Pause all and Resume all for that downloader alone. Under them come the clients you connected: what is in qBittorrent, RDT-Client or Decypharr and on Real-Debrid or TorBox, with state, speed, peers, time left, ratio and how long a torrent has sat idle, and buttons to pause, resume, check, remove (with or without files) or give up on one and search again. Torrents the built-in client runs show their peers and seeds under Details in the queue.
- **Choose when to give up on a stalled torrent** (GitHub #39, blind0wl): Settings > Downloading > Usenet and torrents > Speed and space, from 1 hour to 72 hours or never. Until now it was a fixed 2 hours (built-in client) or 6 hours (download client and debrid).
- **Album pages look like music**: the cover sits on a record that turns slowly and slides out from the sleeve, each track has a small play button that searches YouTube for that song (and a **YouTube** button for the album). The page now shows the album at once and fills in the tracks a moment later, with a loading state (the Mediarium mark) while it waits; opening an album takes about half the requests it did.
- **Book pages**: the Open Library link sits beside the Add buttons instead of on its own line, subjects such as "series:Harry_Potter" read "Series: Harry Potter", and the note at the bottom no longer leaves one word on its own line. The page looks up the book and its author details at the same time.
- **Sharper trailer behind a title page**: the video is laid out as a full 1080p screen and scaled to fit, so YouTube sends its best picture, and the highest quality is asked for.
- **A page for every album on Discover**, also the ones you don't have: cover, artist, year, genres, the full tracklist and an **Add to library** button. Until now an album card only opened once the album was downloaded. The covers of a Discover page are also fetched in the background as the list is built, so they appear faster and fewer are left broken, and an album without cover art is remembered for an hour instead of being asked for again on every visit.
- **Trailer settings in one place**: under Settings > Info, lists and subtitles > Movie info and lists > Trailers, an administrator chooses whether the trailer plays behind title pages and whether it starts with sound. The two buttons on the title page are now small icons (film strip, speaker) that only change it for that browser, and the speaker choice is remembered.
- **Subtitle add-ons** (Settings > Info, lists and subtitles > Subtitles): paste the address of a subtitle add-on (the Stremio kind) and Mediarium searches that site too, for automatic downloads and when you pick a subtitle by hand. That brings in subs.ro, and any other site that has an add-on, such as Hebrew and Arabic ones. Subtitles saved in an older Windows code page (Romanian, Polish, Greek, Hebrew and so on) are converted to UTF-8 so players don't show garbage, and WebVTT is converted to SRT. The address can hold your own key for the site, so it is stored encrypted and only the host is shown (asked for by [@userkbmsr-png](https://github.com/userkbmsr-png) on GitHub, #27, and u/SarSha on Reddit)
- **Titles, descriptions, genres and posters in your language** (Settings > Info, lists and subtitles > Movie info and lists, and in setup): about thirty languages, separate from the screen language, on Discover, search and title pages. Files and folders always keep the English title, so nothing is renamed or searched differently (asked for by [@Apostol6](https://github.com/Apostol6) on GitHub, #26)
- **Download clients**: torrent releases can go to a torrent client you already run: qBittorrent, or a program that works like it, such as RDT-Client or Decypharr. The client downloads and keeps seeding in its own folder, and Mediarium imports the finished files from there without moving or deleting them. Set it under Settings > Downloading > Download client, with a **Test** button, a category, a path mapping for clients in another container, and an optional **Remove from the client after import**. Decypharr's symlinks are added to the library as symlinks once you set the folder they may point into. Choosing a download client turns the debrid service off and the other way round, so torrents go to one place only (asked for by [@PauloJf](https://github.com/PauloJf) on GitHub, #22)
- **Real-Debrid and TorBox**: torrent releases can go to a debrid service, which downloads them on its own servers; Mediarium then downloads the finished files over https and imports them as usual. No torrent traffic leaves your home, nothing is seeded, and the built-in torrent client can stay off. Nothing is mounted, so no special container permissions. Set it under Settings > Downloading > Debrid service (asked for by u/paulojf and u/evilpig on Reddit)
- **Silo** as a media server (Settings > Connections > Media servers): new downloads are scanned straight away, title pages get **Watch in Silo** buttons, and **Refresh now** scans every library. It uses Silo's own API at the address you open it at, with an administrator's API key (asked for by [@xrandallx1](https://github.com/xrandallx1) on GitHub, #19)
- **Add your own indexer definition file** (Settings > Indexers & Search > Pick a site from the list > Add a definition file), for a site the community list doesn't have, such as an indexer you run yourself that ships a `.yml` file for Prowlarr. The file is checked before it's kept, survives list updates, can point at any address on your own network, and takes the place of a community definition with the same id (asked for by [@DFlexy](https://github.com/DFlexy) on GitHub, #18)
- Mediarium in **Brazilian Portuguese**, next to Spanish, German and French (asked for by [@DFlexy](https://github.com/DFlexy) on GitHub, #17)
- Mediarium in **European Portuguese** (Português de Portugal), as its own language next to the Brazilian one: ficheiro, transferir, palavra-passe, Definições and so on. A browser set to Portuguese from Portugal picks it by itself (asked for by [@FACM-PF](https://github.com/FACM-PF) on GitHub, #17)
- Moving a title to a quality profile its file falls short of (a 1080p movie put on the 4K profile) now looks for a release in that quality straight away, even when the profile has better versions switched off or the title was set to leave what it has alone. A file better than the new profile is never swapped for a worse one (idea from u/Wiwer on Reddit)
- **Toloka.to** built in: the Ukrainian tracker shows in the site list under Settings > Indexers & Search, ready for your Toloka login. Its Ukrainian titles and season markers are turned into normal release names so episodes match. (asked for by [@Apostol6](https://github.com/Apostol6) on GitHub, #16)
- Keep a movie or show in two qualities, such as a 4K copy next to the 1080p one. Choose a folder for second copies in Settings > Library > Folders and file names, then press Add a second copy on the title's page. The copy follows its own quality profile, is downloaded and upgraded on its own, and goes in its own folder, ready to add to your media server as a separate library. (idea from u/Embyverse on Reddit)
- Custom formats, as in Radarr and Sonarr. A format spots something in a release name (HDR, a codec, a streaming service, a release group) and each quality profile gives it a score: the higher total wins between releases of the same quality, a minimum score keeps poor releases out, and an upgrade-until score swaps a file for a better-scoring release. Import the TRaSH Guides formats and ready-made profiles straight from Settings > Library > Quality, or paste the JSON from Radarr or Sonarr. Their patterns work unchanged. Test any release name to see what it matches and what it scores. Interactive search shows each release's score. (idea from u/Embyverse on Reddit)
- **Never download season packs**, per quality profile: episodes are fetched one by one even when a whole season is out. (idea from u/12_nick_12 on Reddit)
- A SOCKS5 or HTTP proxy for torrents, indexers and/or Usenet (Settings > Downloading > VPN protection > Proxy), for people who already run one, such as a VPN container's SOCKS5 port. Torrents follow the same rules as through the VPN, and a proxy for torrents counts for the kill switch. **Test** checks it before you save. (idea from u/this_for_loona on Reddit)
- Apps made for Radarr, Sonarr and Lidarr can connect to Mediarium as if it were those apps: Overseerr and Jellyseerr for requests, Homepage and Homarr dashboards, and phone apps such as nzb360, LunaSea and Ruddarr. Add Mediarium in them with the address `/radarr`, `/sonarr` or `/lidarr` and an administrator's API key (Settings > Connections > Other apps). (idea from u/12_nick_12 on Reddit)
- **Webhook** as a media server, for servers and tools without their own support. After each download it gets a JSON message with the folders that changed, path-mapped and batched like the scans of Plex, Jellyfin and Emby. An optional token is sent as a Bearer header. (idea from u/12_nick_12 on Reddit)
- **Skip bad releases**, on in every quality profile without any setup: whole Blu-ray discs, 3D, upscaled copies, burned-in subtitles and groups known for poor re-encodes are left out, and a fixed release (PROPER, REPACK) wins over the broken one. Custom formats are now an advanced option, hidden until you switch on **Show advanced options** (on already if you have custom formats). (idea from u/FilesFromTheVoid on Reddit)
- Two switches under Settings > Library > Quality > Automation: **Stop monitoring a movie once it's downloaded**, and **Stop monitoring a show once it has ended and every episode is in**. Both are off unless you switch them on. (idea from u/Wolfspider_79 on Reddit)

### Added
- **Wait for a better protocol** (delay profiles): automatic searches can hold back the protocol you like less (Usenet or torrent) for a number of minutes, like Radarr and Sonarr. Held releases show on Wanted and on the title page. Settings > Library > Languages and automation. Off by default.
- **An indexer that keeps failing is paused** with a growing wait, up to 6 hours, and says why and until when on the indexer list and in Health, with a Try again now button.
- **Size limits per quality** in MB per minute (Settings > Library > Quality), with reset to defaults. Releases outside them are skipped and marked in the release list. Off by default.
- **Indexer statistics** on the Statistics page: searches, failures, how often each worked, releases found and grabs.
- **Usenet retention**: tell Mediarium how many days your provider keeps posts; older posts are skipped by automatic searches and marked in the release list.
- **Releases with programs or scripts** (.exe, .scr, .bat and so on) are rejected before download and again after unpacking, put on the blocklist, and another release is searched for. On by default, with a switch under Settings > Downloading.
- **Skip duplicate downloads** (optional): a release that was already downloaded, or is already in the queue, is not downloaded again.
- **Add a download by hand** from Activity: an NZB or torrent file, an address or a magnet link, for a movie or episode you pick.
- **IRC announce**: Mediarium can listen to a tracker's IRC announce channel and download a wanted release the moment it is announced. Settings > Indexers & Search, off until you add a connection and switch it on. Four built-in announcement styles, a template editor and a test box.
- **More notification channels**: Matrix, Home Assistant and Apprise.
- **Run under a sub-path** of your domain (`URL_BASE=/mediarium`), and read any container variable from a file (`NAME_FILE=/run/secrets/x`) for Docker secrets.
- **Prometheus metrics** now cover wanted counts, library sizes, queue, disk space, health and indexer status. They are also at `/metrics` and accept a bearer API key.
- **Import lists**: Trakt, TMDB, MDBList, IMDb and Letterboxd (exported file) and the Plex watchlist add movies and shows by themselves on a schedule, with a quality profile, folder, tags and a minimum rating or year (Settings > Info, lists and subtitles > Import lists).
- **Request portal**: people are told by email and on their devices when a request is approved, declined or ready to watch; per-person request limits; 4K requests; and "Report a problem" on a movie or show, answered under Activity > Reports.
- **Music**: playlists (private or shared), a queue panel, gapless playback with an optional crossfade, synced lyrics (from a .lrc file, the file's tags or LRCLIB), ReplayGain, and sending your plays to your own ListenBrainz account.
- **Read on an e-reader**: an OPDS catalogue (KOReader, Moon+ Reader, Librera, Marvin, Foliate) with its own e-reader password, and Kobo sync with KEPUB conversion and reading position kept in step with Mediarium Books (asked for by PauloJf on GitHub, #34; not yet tested on a real Kobo).
- **Built-in torrent client controls**: choose which files to download (samples and notes are left out by default), download in order, speed limits per torrent and an upload limit of its own, categories with their own folders, seeding rules per indexer, and a UPnP/NAT-PMP switch that shows the result. Optionally only the wanted season of a season pack.
- **File permissions**: set the mode of the files and folders Mediarium creates, and the container honours `UMASK`.
- **Move many titles to another library folder** at once, either only for new files or with the files already there (copied and checked first when it is another drive).
- **Movie collections**: a movie's page shows its collection with "Add the whole collection" and an option to add new films that join it; the Library can be filtered by collection.
- **NFO and artwork files** (poster, background, logo, season posters) for Kodi, Jellyfin and Emby, written after imports and for the library you already have. Off by default.
- **Subtitles by kind**: per language choose normal or SDH, normal only, SDH only, forced only, or any of these with a forced track alongside. Files are named `.sdh` and `.forced` so Plex, Jellyfin and Emby understand them.
- **Anime and scene numbering**: TheXEM scene numbers, anime named by part of a season and the show's other names are read, with AniList, AniDB and MyAnimeList links and a Refresh mapping button on the show page. An "Add anime groups" button adds starter release-group terms to a quality profile.
- **Single sign-on** with an OpenID Connect provider (Authelia, Authentik, Keycloak, Pocket ID, Google), **directory sign-in** (LDAP and Active Directory), and **passkeys**. All are off until you turn them on; local sign-in stays available, and `MEDIARIUM_DISABLE_SSO=1` is the way back in.
- **A user guide inside Mediarium** (`/help`, also in the profile menu): the first steps, how searching and downloading work, and one page per area, in every language Mediarium speaks. About 100 settings now have a small (i) with a short explanation, and many have a "Read the guide" link to the right page.
- **VPN provider presets**: pick Mullvad, Proton VPN, AirVPN, IVPN, NordVPN, Surfshark, PureVPN, Private Internet Access, TorGuard and others from a list under Settings > Downloading > VPN protection, and the address fields are filled in for you. Cloudflare WARP has its own card.
- **Turn each subtitle source on or off**: OpenSubtitles, SubDL, your subtitle add-ons and, for Hebrew, **Wizdom** each have a switch under Settings > Info, lists and subtitles > Subtitles.
- **TorBox: prefer cached releases** (Settings > Downloading > Download clients): torrents TorBox already has are tried first, so they finish in seconds.
- **Check Usenet articles before downloading** (optional, Settings > Downloading > Usenet): Mediarium asks the provider whether a sample of an NZB's parts still exist, and gives up at once on a release that could not be repaired.
- **IPTorrents** built in to the site list under Settings > Indexers & Search (asked for by xrandallx1).
- **App language** is the first thing on the new General page under Settings, and also in the profile menu.
- **A Viewer role** that can look and play but never change anything, **read-only API keys**, and accounts that can be turned off without deleting them.
- **Release and subtitle lists sort and filter**: click a column name to sort (size, seeders, quality, score, indexer, release, language; rating and fit for subtitles), narrow the releases by indexer, quality or words, and press **Close** to put a results card away instead of reloading the page (asked for by [@PauloJf](https://github.com/PauloJf) on GitHub, #35)
- **Trailers open in a pop-up** on the page instead of a new YouTube tab (Ctrl-click still opens YouTube). Nothing is asked of YouTube until you press a trailer (asked for by [@PauloJf](https://github.com/PauloJf) on GitHub, #35)
- **Subtitles in the in-app video player**: SRT and VTT files saved next to a video are offered in the player's subtitle menu (asked for by [@PauloJf](https://github.com/PauloJf) on GitHub, #35)
- **More on each download** on the Activity page: speed and time left while it downloads, and a **Details** box with the indexer, release, size, when it was added and finished, where it goes and what happened to it (asked for by [@PauloJf](https://github.com/PauloJf) on GitHub, #35)
- **Find the right number** of connections for a Usenet provider (Settings > Downloading): it tries connections one by one, tells you how many your provider really allows, and sets it for you with one click. If the provider keeps saying "too many connections", the dashboard now says so and points to the test, instead of the downloads just slowing down (looked into after u/Wiwer's reports on Reddit)
- **Trailer behind the page** on movie and show pages: the button next to the trailer links plays the first trailer, muted, as a moving backdrop, with a **Sound on** button. On out of the box, so a title page starts its trailer by itself (the video comes from YouTube, so YouTube sees the visit): an administrator can switch it off for everyone under Settings > Info, lists and subtitles > Movie info and lists, and each browser can switch it off or on with the button on the page. It stays off with reduced motion or data saving, plays once at full strength across the whole width of the page (with a dark wash from the left so the text stays readable), with the still image fading out while it plays and back in as the trailer nears its end, shows only once the video is really playing, asks a player that is ready but hasn't started to play again, tries the next trailer when one can't be embedded, and says in a short note why when none will play (asked for by the owner)
- **Several audio languages, and the title's original language** (Settings > Library > Quality > Audio languages): list the languages you'd watch in, best first, and add **Original language of the title** to get a Japanese anime in Japanese and a French film in French while everything else stays in your language. A release with two or more audio tracks counts when one of them is on your list
- **The music player can pop out into a window of its own**, like the audiobook player, with the cover, controls and queue; it carries on from where it was, and **Back to the app** gives it back to the bar
- **Play and Shuffle for a whole artist** on the artist page, and a **Play** button on the artists in the music Library
- **Music player** built in: a **Play** button on every downloaded album and a play button on each track on the artist page start the music in a bar at the bottom of the app. It keeps playing while you move around, and has previous, next, a seek bar, shuffle, repeat (queue or one track), volume that is remembered, and works with the media keys and the lock screen. A track the browser can't play (ALAC outside Safari, for example) is skipped with a short note
- **Your Plex watchlist**: connect your Plex account under Your profile, and what you add to your watchlist in any Plex app is added to Mediarium and searched for within 20 minutes (or sent as a request, for accounts that have to ask). What was already on the list shows on Discover with Add buttons. Each account connects its own.
- **Third-party licences** in one place: `THIRD_PARTY_NOTICES.md` lists every library, font and tool Mediarium ships with and their licence texts. It's in the Docker images (`/usr/share/doc/mediarium/`), the download archives, and linked from the About page.
- **Report a problem or share an idea** from inside Mediarium: the bug button at the bottom of the sidebar (also in the profile menu and Settings > System) opens a window for a title, a description, a screenshot and, for administrators, the support report. **Send privately** sends it to the developer through mediarium.app, where nobody else sees it; **Open on GitHub** opens a new issue with everything filled in. Screenshots are shrunk and saved again as plain JPEGs before they leave the browser, and private reports are limited to a few an hour.
- A thank-you list on the About and credits page, and in the documentation, of the people whose ideas and bug reports shaped Mediarium.
- Links to the website, the documentation and GitHub in the profile menu, a GitHub button, and **Share Mediarium**: ready-made posts for Reddit, X, Bluesky, Facebook, WhatsApp, Instagram and forums, with pictures to download. After two weeks, a small card asks once in a while for a star, an idea or a share; **Don't ask again** stops it.
- Mediarium in Spanish, German and French. Pick a language under Profile > Language, on the sign-in page or at the start of setup; until then it follows your browser. Dates and numbers follow the language too. Messages from the server itself stay in English for now.
- Statistics over time: charts of your library's size, the number of movies and shows, and the downloads each week, over 3 months, a year or everything (Statistics > Over time). A small snapshot is kept each day, so the charts reach back further than the download history.
- Push notifications to your phone or computer straight from the browser, with no app or account: Settings > Connections > Notifications > **Push to this device**. Messages are encrypted for each device before they leave your server. Needs Mediarium opened over https; on an iPhone or iPad, add it to the Home Screen first.
- Two-step sign-in: a six-digit code from an authenticator app after the password, with recovery codes for a lost phone. Off until each person switches it on in their profile; suggested once Mediarium can be reached from outside your home.
- An empty movie or TV library shows a row of popular titles you can add straight away.
- Library: a slider to make posters bigger or smaller, and two more ways to sort: by release date and by size on disk.
- After an update, a short "What's new" note shows the highlights of the new version, once in each browser.
- Specials can be downloaded. Tick a special on the show's page and it's looked for like any episode, by its S00E number and by its title, since many specials are posted under their own name. Search finds one straight away. They go in the show's Season 00 folder, where media servers expect them.
- Subtitles inside the video: subtitle tracks in MKV and MP4 files count as there, so a language the video already has isn't downloaded again. Forced tracks don't count. Can be switched off.
- SubDL as a second place to find subtitles (needs a free key). Searches ask both sites and the best fit wins, and automatic downloads carry on with SubDL when the OpenSubtitles daily limit is used up.
- Cloud backups: a copy of each nightly backup in S3-style storage (Backblaze B2, Wasabi, Cloudflare R2, Amazon S3, MinIO) or a WebDAV folder (Nextcloud, NAS), locked with your passphrase before it leaves the server. Settings and database only, never media. Restore a copy by choosing it under Restore with the passphrase.

### Changed
- **Help opens in a window**: the small "?" buttons and "Learn more" links show the user guide in a window over the page, so what you were doing stays where it is. The info and help buttons are teal and sit in the middle of the line.
- **Usenet and Torrents each have their own on/off switch** on their pages (at least one stays on). The Cloudflare card sits under the indexers. The Accounts group is now called **Account & Access**. The sidebar has a slim scroll rail, and the drifting shapes in the background are a little fainter.
- **Password-protected downloads**: RAR and 7z releases with a password now unpack. The password is read from the NZB or from `{{password}}` at the end of the release name, as NZBGet does (asked for by 12nick12, #44).
- **Audiobooks that are on Audible but not on Open Library** now import: the books import asks Audnexus before giving up and adds the book with its narrator, length and series (a contribution by thegliffy, #41 and #42).
- **Discover is already there when you scroll**: every list on the page loads by itself within a few seconds of opening it (movies and shows first, music and books a little later because those services ask for patience), and the posters are fetched quietly in the background, so the cards are ready before you reach them. Posters are sent at the size a card needs (342 pixels wide, 500 on a sharp screen), and your browser keeps a Discover list for five minutes, so going back to the page costs nothing.
- **Books with accents in their names** (Le Père Goriot, Honoré de Balzac) now match releases written without them, and the other way round, so they no longer wait in Wanted for ever (a contribution by thegliffy, #40).
- **Shows and films with an apostrophe in their name are found**: Hell's Kitchen, Marvel's Agents of S.H.I.E.L.D. and the like. Indexers often answered "Hell's Kitchen" with nothing, because release names are written without the apostrophe, so the search now leaves it out. A release that adds a country tag after the name ("Hells.Kitchen.US.S24E01") also counts as that show (reported by Hoopes80).
- **A Posters view in the calendar**: big posters fill each day, with the name in a frosted bubble, and a day with several releases opens as a window of posters. Month and Agenda are still there (inspired by TryToTilt's fork).
- **Anonymous usage numbers, only if you say Yes**: the first time an administrator opens Mediarium (a new install or an update) it asks whether to share one small anonymous message a day, so the project knows how many installs are in use and which devices and setups are popular. Nothing is chosen for you and nothing is sent before Yes. The message is a random ID, the version, how it runs and rough bands for size and hardware, and yes/no for features, and you can see it exactly in the window and under Settings > General, switch it off, make a new ID, or set `MEDIARIUM_DISABLE_STATS=1`. The totals are public at mediarium.app/stats, and the code that makes the message and the code that receives it are open. Prefer not to share from the app? There is an anonymous form on mediarium.app/survey. See [usage-stats.md](docs/usage-stats.md).
- **Settings sidebar**: the Settings group stays open and shows every page, and opening a page opens only that group's pages. **General** is the first page (language, light or dark, time zone, address, updates, legal notice).
- **System logs and the blocklist** load a page at a time with Previous and Next, and the log filters sit on one row.
- **About and credits**: the six steps sit on one row, and the thank-you list is grouped by kind of help in cards.
- **Settings, reorganised**: the long pages are split so each has one job and a name that says it. **Library** now has Folders and file names, Quality, and **Languages and automation** (audio languages and the automatic searching, moved out of Quality). **Accounts** has **Your profile** (everyone) and **All accounts** (administrators: the list, adding people, sign-in through a proxy). **System** has **Server and updates** (was Server and backup), **Backup and restore**, Statistics, Logs and errors, Move from other apps, and About and credits. **VPN protection** is now **VPN and proxy**. Every settings page has a one-line description under its name, the header shows the group in smaller type before the page (and the page name alone on a phone), and the old addresses still work. The profile menu says **Your profile** for everyone, and **Show demo data** is for administrators only (it only changes what this browser shows).
- **Faster loading**: the web interface is sent compressed (about a quarter of the size on the first load or after an update), the rarely used pages (all of Settings, the setup wizard, Statistics, imports) load when you first open them instead of with the first page, and the big lists (queue, dashboard, activity, calendar, books, artists and Discover lists) answer "not changed" when nothing has. The Library reads its lists at the same time instead of one after the other and only redraws when something changed, Discover polls less, and a title that isn't in your library no longer re-reads its details every five seconds. The dashboard and the health check look at your folders at most every 30 seconds.
- The media cards on the dashboard have a crisp outline of the same width all the way round, and the bite out of the top corner is the shape of the kind's own icon (ticket, television, record, book, headphones)
- Pages about one title (movie, show, artist, book) no longer show the drifting shapes through the glass; they keep the plain dark ground
- **One menu for everything that plays**: the play button in the header now lists the Ebook reader, the Audiobook player, the Music player (with what is playing) and your media servers, instead of the book button and the media server menu being separate. The book button is gone from the header
- **Statistics** has its own place under Settings > System (members can open it too), not only the link on the dashboard
- **Search shows the newest first**: results in the search box, Discover and the Search page are ordered by year with the newest on top (a 2026 film above the 2022 one with the same name); titles that start with what you typed come before the rest. Books are ordered the same way, and artists with the same name and score are listed newest first
- The media server menu (the play button in the sidebar) shows each server's logo instead of a stretched letter
- A release named `HDRip` counts as an HDTV quality instead of a WEB-DL, so a profile that wants WEB-DL leaves it out
- In the release list of a film that isn't out digitally or on disc yet, every release carries the note that it may be a recording from a cinema even when its name says WEB-DL
- Posters that are waiting for a release keep their normal picture, with a thin still dashed outline. Before, the poster was dimmed and the outline marched, which made dark posters hard to read. On a movie ticket the outline steps aside while the ticket tilts on hover. "Admit one" on the ticket is darker, so it can be read.
- On a show's poster the speaker lines and knobs are lighter, so they show in the dark theme.
- The search box in the top bar uses the free space, up to about 780 pixels wide, and is taller.
- Discover's toolbar fits on one row: "From year", "To year" and "Hide owned" are shorter, and the Trakt button shows just its mark on screens under 1700 pixels.
- On a movie or show page the tags sit at the end of the row with the quality profile, instead of on a row of their own.
- **A new calendar.** It reads like a cinema programme: the month in big type, round arrows and Today to move between months, and a switch between **Month** and **Agenda**. In the month grid every release is a small poster in its kind's frame and colour, today is lit up, and past days are dimmed. The agenda lists the days with something on, each with a ticket stub for the date and cards with the poster, episode code and status. Phones always get the agenda. Status works as on the posters: solid when you have it, a bright line while it downloads, a dashed outline while it's wanted, with a glow when it's late. The legend now shows each kind's symbol and the three states.
- **A new look.** New fonts (Wix Madefor Display for headings, Host Grotesk for text, Reddit Mono for numbers and codes), a deeper night-blue background, and one colour per kind of media that follows it everywhere: movies amber, TV cyan, music pink, books lilac, audiobooks orange. Every poster has the shape of what it is: a movie is a cinema ticket with a tear line, a show is a TV set with a glass screen, speaker grille and knobs, an album has its record peeking out of the sleeve, a book has a ribbon bookmark and page edges, and an audiobook wears headphones. Something you want but don't have yet gets a moving dashed outline in its colour, and a download shows a bright line along the bottom of its poster. Title pages show the film's or show's own backdrop on the right, slowly drifting, with a much bigger title, a ticket stub under movie posters (or a TV set around a show's poster) that tilts towards the pointer and catches the light, and the main button in the title's colour. The sidebar is a floating panel where the open section lights up in its colour, the buttons at the bottom each have their own colour, and the header shows the page name in large type next to a glass search field. The dashboard greeting is a cinema marquee with chasing bulbs, the media counts use each kind's shape as their icon, wanted rows are dashed in their colour instead of having a bar down the side, and a book's own page shows it as a real book with page edges and a ribbon. Activity opens with a row of counts (downloading, in line, problems, waiting, downloaded), each download shows its percentage large next to a progress line in its colour, and titles still waiting for a release are tiles with the moving dashed outline. Every settings page now uses the same panels with the section name inside, the quality profiles table gives each column room, Media types shows each kind's own shape, and About lists what Mediarium does as numbered steps.
- The sidebar has no section labels, no status line under the logo and no collapse button; the version is a small bubble with the logo at the bottom. A group of settings pages opens only when you press it and closes when you press anywhere else. The books player, reporting a problem, GitHub and the support heart sit in the top bar, in colour, next to the light/dark switch and your profile (point at one to see what it does), with a play button that opens a menu of your media servers. On a phone the top bar has the page name, a search icon and your profile, the buttons and the light/dark switch are in the menu, and the filters in Library and Discover fold behind a Filters button.
- Mediarium is dark unless you pick light. The button in the top bar switches between dark and light; it no longer follows your device's setting.
- The README says how the name and logo may be used: forks are welcome, and a changed version that's shared should go by another name.
- Dashboard: Recently added is split into two cards side by side. **Just added to your library** shows what arrived, the latest download first, with when it came in. **Added, still to come** shows what's still waiting, the latest added first. On a phone they sit one under the other. The separate Recently downloaded card is gone, since the first card now covers it.
- Dashboard: **Connected folders** is a wide card with every kind of media (movies, TV, music, ebooks, audiobooks) and Downloads; kinds that are switched off are shown dimmed as Off.
- Discover: the filters fit on one row on more screens. The Ebooks & Audiobooks tab is now called Books, and Trakt list import is the list button at the end of the row.

### Fixed
- The built-in torrent client no longer follows a tracker, web seed or magnet address that points at your home network or this computer (a hostile torrent could use it to poke at things there), and reads at most 1 MB from a tracker. If you run a tracker or web seed at home, turn on **Let torrents use trackers on my own network** under Torrent client options.
- A crafted torrent file, or magnet metadata from a hostile peer, can no longer crash Mediarium.
- A huge page number in an OPDS address no longer crashes the catalogue.
- Only the Kobo's own headers are sent on to the Kobo store, and ebooks that unpack to gigabytes are not converted for a Kobo.
- The calendar feed and Kobo addresses are kept out of the log, and OPDS and Kobo addresses work under `URL_BASE`.
- A turned-off account loses its e-reader and calendar access as well, a read-only API key can no longer read other credentials, linking single sign-on to an account asks for the password, and a push address has to be a public one.
- Single sign-on works under `URL_BASE`; a domain limit needs a verified email; an LDAP administrator group filter has to name the person.
- IRC announces follow the size limits and the delay profile; the follow-up search for a held release waits for room under the hourly download limit; held releases are forgotten when a title is removed, grabbed or downloaded; and a title's search note counts size limits, the delay profile and scene or anime numbering.
- Titles added by an import list are searched for like any automatic search, and a new list adds 10 titles at a time.
- A music playlist page is no longer restyled by the logs page, and viewers are no longer shown buttons that always fail.
- A download can no longer be skipped as a duplicate of an identical one that started at the same moment, and a paused download keeps its title from being moved.
- The migrate page and the subtitle kind settings no longer overflow on a phone.
- Importing from Prowlarr no longer leaves indexers behind. A site Mediarium's list has no definition for (or can't run yet), one of Prowlarr's own built-in indexers, or a Newznab indexer with an unusual API path is now added as Prowlarr's own Torznab or Newznab feed for it, with Prowlarr's API key, instead of being skipped with "add it by hand" (reported by DFlexy on GitHub, #18).
- Searches and NZB downloads from Usenet indexers now say they come from Mediarium instead of using Go's default name, which some indexers' Cloudflare rules turn away with a 403 page (reported by Hoopes80 on GitHub, #37 with NZBFinder). A 403 that comes back as a web page is now reported as "An indexer is turning Mediarium away" instead of "refused the key".
- On Activity, the titles waiting for a release now have their poster and year, and a show is one tile ("6 episodes waiting") instead of one per missing episode. The search note on a tile no longer runs on for lines: it is cut short and reads in full when you point at it.
- Picking a music genre on Discover no longer ends in "MusicBrainz is slow to answer": the genre details are fetched four at a time, the listening statistics get 18 seconds, and when they are slow, down or know no genres for the list, the music database's own genre tags fill the page. A **Try again** button sits next to any error there.
- Popular artists on Discover now have a picture (the cover of the release of theirs people listen to most), and a missing cover is a small record instead of a music note.
- Opening an artist from the library (or an album from Discover or Activity) no longer scrolls the page down to the albums; the album opens in place and the page starts at the top.
- The version at the bottom of the sidebar stays pinned there, below the menu, even when a Settings group is open. The green dot beside the page name in the header no longer has its glow cut into a box.
- On a phone a page could be dragged sideways (seen on Downloading in iOS Safari), pages sometimes did not start at the top after moving between them, and a few inputs on Settings > Migrate were wider than the screen. Sideways panning is switched off on phones, the jump to the top is repeated once the finger's momentum and the loading page have settled, and the browser's own scroll restoring no longer fights the app's.
- **Start the trailer with its sound on** did nothing on a page opened straight from a link or a refresh, because browsers refuse sound until you tap something. The trailer now plays muted, the small speaker button says it is waiting, and the first tap or key press anywhere on the page turns the sound on. After you open a title from inside Mediarium it still starts with sound at once.
- On the book player's shelf, the active tab (All, Reading, Listening) had an empty space in front of its name.
- Security: two-step sign-in could be guessed at without limit by signing in again with the password after every few wrong codes. Wrong codes are now counted per account too, and the count is no longer wiped by the password step. Changing a password from a stolen session is slowed down the same way.
- Security: the saved proxy password could be sent to a different proxy address by an API key that left the password out. It now only goes with the address it was saved for.
- Security: a definition file added by an API key could replace the definition an indexer runs on and redirect its saved login. Adding or removing definition files now needs a signed-in administrator, and a file with the id of a definition in use is refused.
- A Plex watchlist could add movies or shows to an account that has no permission for that kind. Music playback now follows the Play permission like books and video.
- Posters and covers that fail to load no longer show the browser's broken-image icon, and a poster that fails once is tried again when its address changes.
- Back now puts a long list at the spot you left it, instead of the top, and typing in a search box or picking a filter no longer jumps the page to the top. The Queue and Wanted tabs, the Music and Books library filters, Discover pages (Browse all) and music Browse all pages are kept in the address too.
- Opening another book from a rail on a book page showed the last book's details until the new one loaded. Series search answers that arrive late no longer land under the wrong episode, only one release can be grabbed at a time, the Add dialog can't be closed while it is adding, and the Discover rows and pages have a Try again button when a page failed.
- Security: cloud backups (S3 and WebDAV) now go through the same guard as indexers, so an address pointing at a cloud metadata service is refused.
- The Music and Books library toolbars fold their filters behind a Filters button on a phone and sit in the same panel as Movies and TV, so their buttons no longer wrap onto lines of their own. The Calendar's kind, "not downloaded" tick and month are kept for Back too.
- Several buttons could be pressed twice (Search now on Wanted while another search runs, Refresh episodes, Clear finished and the blocklist buttons, asking for a book twice), and a slow answer could land after you had switched tabs on Wanted or typed on in the Queue history search or the book search. The Wanted > Upgrades tab no longer shows a search box above an empty list, the file player and viewer keep the keyboard inside them, and deleting a search down to one letter clears it from the address.
- On a phone, the Subtitles list under Wanted no longer squeezes its language badges into the thumbnail column.
- Back from a title, an album or a book now returns to the Discover tab and filters you were on, and to the Library tab, status chip, search and filters you had, instead of starting again on All or Movies. They live in the address now, so a link keeps them too.
- The **Downloading** chip in the Library moved its blue line even when nothing was downloading. It now moves only while something is.
- Posters waiting for a release (dashed outline) no longer have a darker ticket and different text colours than the rest. The dashed outline alone marks them.
- The **Select** and **Trakt list** buttons on Discover are icons and sit in the filter row instead of on a line of their own.
- A title page's poster no longer touches the left edge of the page.
- Book pages have a background now: the cover, blurred, behind the title.
- Subtitle results that came from a subtitle add-on were labelled "OpenSubtitles" in the results list; they now say Add-on, and show no rating they don't have
- Custom formats with a language condition set to **Original** (as in Radarr and Sonarr, and several TRaSH formats) never matched. They now use the language the film or show was made in, from TMDB (suggested by a question from u/DerGlordon on Reddit)
- Book covers load from a copy kept on your server. Open Library answers every cover with a redirect and takes one to two seconds each time, so a page of books filled in slowly. Each cover is fetched once, kept in the config folder (`book-covers`) and sent with a month-long browser cache, and Library and Discover ask for the next covers in the background.
- An artist with one album added showed "Partial 1/48", counting every release the artist has. It now counts what you asked for ("Partial 1/3", or Downloaded when all of it is in), and the hint says albums, not episodes.
- On an artist page the open album's track list is titled "Tracks on <album>", and a line "More by <artist>" marks where the artist's other albums start.
- **Skip bad releases** also leaves out upscales that only write "Ai" after the resolution (like `2160p.Ai.DTS-HD`), not just those that say "upscale" (seen in a report from u/Wiwer on Reddit).
- The Statistics page has its own name in the browser tab instead of just "Mediarium".
- Accessibility: every page has a proper main heading for screen readers again (they were hidden along with the visible title), headings no longer skip levels, links inside sentences are underlined, status labels on posters are readable in the light theme, empty table columns are named, the backup file picker has a label, and the calendar no longer confuses screen readers. Checked on every page in both themes with an automated checker, and by keyboard.
- Daily shows with two episodes on the same day: a release dated that day went to the first episode, whichever it really was. The guest or title after the date, or "Part 2", now picks the right one, and a release that can't be told apart is left for you to choose.

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
