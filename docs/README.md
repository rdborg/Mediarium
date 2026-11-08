# Mediarium documentation

Start here. The reference pages are generated from the code, and every change to the app updates its page in the same commit.

The screenshots in these pages come from demo mode: the movies, shows, artists, posters and covers are all made up (the books are public-domain classics), so no one else's titles or artwork are used. Your own copy shows the real posters and details for what you search for and add.

| I want to... | Read |
|---|---|
| Read these pages inside Mediarium, without leaving the app or going online (the user guide, opened with the **?** key or the **?** button at the top) | [The guide inside Mediarium](#the-guide-inside-mediarium) |
| Install it with Docker (one compose file, any Linux server or NAS) | [INSTALL.md](./INSTALL.md) |
| Install it on Synology, Unraid, QNAP or Linux, step by step | [synology.md](./synology.md), [unraid.md](./unraid.md), [qnap.md](./qnap.md), [linux.md](./linux.md) |
| See which platforms Mediarium runs on (one Docker image for all of them, Raspberry Pi notes) | [PLATFORMS.md](./PLATFORMS.md) |
| See what it can and cannot do today, and what is planned | [FEATURES.md](./FEATURES.md) |
| Move over from Radarr, Sonarr, Prowlarr, SABnzbd and other apps without redoing your setup or moving files | [migrate.md](./migrate.md) |
| Add movies and shows you already have on disk (Import existing in the Library) | [import-library.md](./import-library.md) |
| Use the whole screen of a foldable or dual-screen phone, and tune it | [foldable-phones.md](./foldable-phones.md) |
| Know where posters and backdrops come from, why your browsers no longer contact TMDB, the sizes and the cache, and how to switch it off | [artwork.md](./artwork.md) |
| Find something to add (Discover): more like your library, browse more, older titles, and the Add dialog | [discover.md](./discover.md) |
| Open the torrent port (58264), seeding, Real-Debrid, TorBox and Premiumize, qBittorrent, RDT-Client and Decypharr, how many downloads run at once, cleaning up the downloads folder, what removing a title deletes | [downloads.md](./downloads.md) |
| Send torrent traffic through the built-in WireGuard VPN, and what the kill switch does | [vpn.md](./vpn.md) |
| Add indexers: Usenet and torrent APIs, sites from the definition list, private sites, Cloudflare and FlareSolverr | [indexers.md](./indexers.md) |
| Grab a release the moment a tracker announces it in IRC (connections, announcement templates, the test box, limits) | [irc-announce.md](./irc-announce.md) |
| Give family members their own login, see what members can do, reset a password | [accounts.md](./accounts.md) |
| Add movies and shows by themselves from lists on Trakt, TMDB, IMDb, MDBList, Simkl and Letterboxd (import lists) | [lists.md](./lists.md) |
| Let people ask for titles: messages when a request is answered, a limit on requests, 4K requests, problem reports | [accounts.md](./accounts.md#requests) |
| Reach Mediarium from the internet safely (reverse proxy checklist, trusted proxies, what the app protects) | [security.md](./security.md) |
| Seed the same files on more than one tracker (cross-seeding) next to Mediarium, with Qui or cross-seed | [cross-seeding.md](./cross-seeding.md) |
| Choose a quality profile (the built-in presets, cutoffs, upgrades, fallback profiles) | [quality-profiles.md](./quality-profiles.md) |
| Use Mediarium in Spanish, German, French, Italian, Dutch, Polish, Russian, Ukrainian, Simplified Chinese or Portuguese (Brazil or Portugal), or help with a translation | [languages.md](./languages.md) |
| Report a problem or share an idea from inside Mediarium, privately or on GitHub, with a screenshot and the support report | [report-a-problem.md](./report-a-problem.md) |
| Know what the optional anonymous usage numbers are, what is sent, where it goes, and how to switch it off or use the survey instead | [usage-stats.md](./usage-stats.md) |
| See who suggested what, and how to suggest something yourself | [community.md](./community.md) |
| Tell others about Mediarium, star it or send an idea (the profile menu has **Share Mediarium** with ready-made posts and pictures) | [README.md](#sharing-and-feedback) |
| See why a title has not downloaded, read the Activity page, pause, resume or stop a download, retry without downloading again | [activity.md](./activity.md) |
| Subtitles: the on/off switch, release subtitles, offers, daily limits, dismissing | [subtitles.md](./subtitles.md) |
| Switch movies, TV, music, ebooks and audiobooks on or off (Settings > Media types) | [modules.md](./modules.md) |
| Remove what nobody watches, with a dry run, a Leaving soon shelf and a Keep button | [lifecycle.md](./lifecycle.md) |
| Follow YouTube channels and playlists and keep their videos or just the audio, with a yt-dlp you supply yourself (off by default, with a warning) | [archiver.md](./archiver.md) |
| Change many movies, shows or artists at once from the Library (monitor, better versions, quality profile, download from, search now, remove) | [library.md](./library.md) |
| Manage music (switch on the music module, artists and albums, audio quality, importing a collection) | [music.md](./music.md) |
| Ebooks and audiobooks (switch them on, add books, which release is picked, where files go) | [books.md](./books.md) |
| Connect Plex, Jellyfin or Emby (library refresh after imports, "Watch in" links, finding the token or API key, path mapping) | [media-servers.md](./media-servers.md) |
| Run a script of your own after each import (what it is told, how to try it) | [scripts.md](./scripts.md) |
| Keep your settings in a YAML file (for git or a deploy script) and apply it | [config-file.md](./config-file.md) |
| Download your settings as one file, check a file and see what it would change, then apply it (or apply it at start) | [config-as-code.md](./config-as-code.md) |
| Connect Overseerr, Jellyseerr, Homepage, Homarr, nzb360, LunaSea or Ruddarr (apps made for Radarr, Sonarr and Lidarr) | [other-apps.md](./other-apps.md) |
| Watch downloads from nzb360 and other phone apps (the built-in downloaders answer like SABnzbd and qBittorrent) | [download-apps.md](./download-apps.md) |
| Ask your library questions from a chat program or a voice assistant (MCP) | [mcp.md](./mcp.md) |
| Set up notifications (email, ntfy, Gotify, Pushover, Slack, Discord, Telegram, Matrix, Home Assistant, Apprise, webhook), turn them on and off, test them | [notifications.md](./notifications.md) |
| Find out what went wrong (a failed download, "too many connections", a full disk) and what to try, copy a report for support | [logs-and-errors.md](./logs-and-errors.md) |
| Understand the responsible-use notice | [LEGAL.md](./LEGAL.md) |
| Serve Mediarium under a sub-path of your domain (`URL_BASE`), keep a value in a Docker secret file (`NAME_FILE`), or graph it with Prometheus | [INSTALL.md](./INSTALL.md#running-under-a-sub-path-url-base), [INSTALL.md](./INSTALL.md#secrets-from-files-docker-secrets), [INSTALL.md](./INSTALL.md#monitoring-with-prometheus) |
| Look up an HTTP endpoint | [reference/api.md](./reference/api.md) |
| Look up an environment variable | [reference/environment.md](./reference/environment.md) |
| Look up a stored setting | [reference/settings-keys.md](./reference/settings-keys.md) |
| Look up a problem code from Logs and errors | [reference/problem-codes.md](./reference/problem-codes.md) |
| Know when a new version is out, update from inside the app, restart Mediarium, or push a program file to a running install | [INSTALL.md](./INSTALL.md#updating), [INSTALL.md](./INSTALL.md#updating-without-rebuilding-the-container) and [security.md](./security.md#updating-the-program) |
| Understand version numbers, sign and publish a release, or push a test build | [RELEASING.md](./RELEASING.md) |

## Where to find a setting

Settings has one entry in the sidebar. It always lists its groups, and one group at a time shows its pages. Older addresses such as `/settings/downloads` still open, on the page that now holds that setting.

| Group | Pages | What is there |
|---|---|---|
| General | General | The language of the app, light or dark, the time zone note, the address you open Mediarium at, updates and the legal notice. Everyone sees the first three. |
| Media types | Media types | Switch movies, shows, music, ebooks and audiobooks on or off. |
| Library | Folders, File names and importing, Quality, Automatic searching | Where files live; how they are named, NFO and artwork files and the script after an import; quality profiles, size limits and the audio language you want; when Mediarium searches by itself. |
| Indexers & Search | Indexers & Search | Indexers, FlareSolverr and IRC announce. |
| Downloading | Usenet, Torrents, VPN and proxy, Download rules | Your Usenet provider; the torrent client, debrid and your own client; the VPN; where to download from, speed, hours and checks. |
| Info, lists and subtitles | Movie and show info, Import lists, Subtitles | TMDB and the info language, trailers; lists, the Trakt key and the Simkl account; subtitles. |
| Connections | Media servers, Notifications, Other apps | Plex, Jellyfin and Emby; messages; Overseerr and phone apps. |
| Accounts | Your profile, Security, Linked services, All accounts, Requests and reports, Ways to sign in | Your own details, passkeys, two-step sign-in and API keys, Plex watchlist, ListenBrainz and e-reader; for administrators the accounts, requests and every way to sign in. |
| System | Server, Backup and restore, Statistics, Logs and errors, Move from other apps, About and credits | The server and its clean-up, backups, numbers, errors and the report for support. |

The app language is only about the app itself. The audio language you want is under Settings > Library > Quality, and the subtitle languages are under Settings > Info, lists and subtitles > Subtitles.

## Keeping the docs current

- Change a feature, setting or install step: update its page here and add a line to `CHANGELOG.md` in the same pull request. CI fails a pull request that changes the app without touching the docs (add the `no-docs-needed` label if nothing visible changed).
- Change a route, an environment variable or a settings key: run `go run ./tools/docgen` and commit the regenerated `docs/reference/*.md`. CI fails if they are out of date.
- The in-app user guide is built from the pages here (see [The guide inside Mediarium](#the-guide-inside-mediarium)), so an edited page shows up in the next build with nothing else to do. A new page has to be added to `web/src/guide/topicDefs.ts`, or the web tests fail.
- The app links here from its About and credits page. The address lives in one place, `web/src/docs.ts`, so it can move to a website later.

## The guide inside Mediarium

Mediarium has these pages built in. Open **User guide** from the profile menu (your initial, top right), press the **?** button next to the bug in the top bar (it is in the menu on a phone), or press **?** on your keyboard anywhere you are not typing. The guide lists the topics (getting started, adding movies and shows, music, books, downloads, quality profiles, subtitles, accounts, media servers, notifications, backups, fixing problems and the install guides), has a search box over all of them, and shows a list of sections for the topic you are reading. Links between pages stay inside the guide, and code blocks have a **Copy** button.

- It is made from the pages in this folder when the app is built, so it always matches the version you run and works without an internet connection. It leaves out the notes for people who script against the API, and the screenshots. **Read the full documentation** at the top of every topic opens the complete pages online.
- It is in English only for now. When Mediarium is set to another language, the guide says so at the top.
- The small round **i** and **?** buttons next to settings and headings open a short explanation. Hover over one, tab to it or tap it; Escape closes it. A **?** also has a **Learn more** link that opens the matching part of the guide. Most settings pages have a **?** after the line under their name that opens the part of the guide about that page, and the fields and switches that need an explanation have an **i** with one sentence on what they do and what changes when you set them.
- A page in this folder shows up in the guide once it is listed in `web/src/guide/topicDefs.ts`. `npm run build` in `web/` stops with a message when a listed page or section no longer exists.

## Sharing and feedback

The profile menu (your initial, top right) links to the website, this documentation and the GitHub page, where a star helps others find Mediarium. **Report a problem or share an idea** opens the [report window](report-a-problem.md), and **Questions and discussions** opens GitHub Discussions. **Share Mediarium** has a ready-made post for Reddit, X, Bluesky, Facebook, WhatsApp, Instagram and forums, with buttons to copy it or open the site, and three pictures to download (1200 × 630 for links, 1080 × 1080 square, 1080 × 1920 for stories). The pictures use made-up titles.

After two weeks of use, a small card in the corner asks once whether you'd like to star, send an idea or share. It comes back at most every three months, never in demo mode, and **Don't ask again** stops it for that browser. Nothing is sent anywhere unless you click a link.
