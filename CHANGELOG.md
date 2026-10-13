# Changelog

All notable user-facing changes to Mediarium are recorded here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
version numbers follow [Semantic Versioning](https://semver.org/). Each
release has its own dated section. Changes that aren't released yet collect
under [Unreleased]. How releases are cut: [docs/RELEASING.md](docs/RELEASING.md).

## [Unreleased]

### Added
- **Send indexer traffic through the VPN**: a new switch under the kill switch in Settings > Downloading > VPN and proxy. Searches, RSS, release downloads, IRC announces and debrid then go through the built-in VPN, and wait while it isn't connected instead of going without it. Indexers on your own network still connect directly. Off by default. See `docs/vpn.md`.

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
