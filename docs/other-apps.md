# Connecting apps made for Radarr, Sonarr and Lidarr

Many apps were made to work with Radarr, Sonarr and Lidarr: request apps (Overseerr, Jellyseerr), dashboards (Homepage, Homarr) and phone apps (nzb360, LunaSea, Ruddarr). Mediarium answers the same requests, so you can add it in those apps as if it were Radarr (for movies), Sonarr (for TV) or Lidarr (for music).

The page **Settings > Connections > Other apps** shows the addresses to use and makes the key.

Chat programs and voice assistants that support MCP connect in a different way, with their own address and switch. See [mcp.md](./mcp.md).

To see the speed and queue of the built-in downloaders in nzb360 and similar apps, Mediarium can also answer like SABnzbd and qBittorrent. See [download-apps.md](./download-apps.md).

## What to fill in

Each app gets its own address, because Radarr and Sonarr use the same paths:

| Add Mediarium as | URL base | Full address (example) |
|---|---|---|
| Radarr (movies) | `/radarr` | `http://192.168.1.20:8264/radarr` |
| Sonarr (TV) | `/sonarr` | `http://192.168.1.20:8264/sonarr` |
| Lidarr (music) | `/lidarr` | `http://192.168.1.20:8264/lidarr` |

- **Host and port** are the ones you open Mediarium at. Inside Docker, use the container name or the NAS address, not `localhost`.
- **URL base** is `/radarr`, `/sonarr` or `/lidarr`. Apps that ask for one full address take the full address instead.
- **API key**: an administrator's Mediarium API key. Press **Make a key for other apps** on the Other apps page, or make one under **Accounts > API keys**. The apps send it as the `X-Api-Key` header or the `apikey` parameter; both work. A key of a basic account is refused.

Each part only answers while its media type is switched on (**Settings > Media types**). Lidarr, for example, answers once music is on.

If Mediarium sits behind a reverse proxy under a sub-path (the `URL_BASE` setting, see [INSTALL.md](./INSTALL.md#running-under-a-sub-path-url-base)), put that path in front: the URL base becomes `/mediarium/radarr` and the full address `https://home.example.com/mediarium/radarr`. The Other apps page shows them that way.

## Overseerr and Jellyseerr

1. In Overseerr go to **Settings > Services > Add Radarr Server**. Fill in the host, port, URL base `/radarr` and the API key, then press **Test**.
2. Pick a **Quality Profile** (Mediarium's quality profiles are listed) and any **Root Folder**. Mediarium decides the folder itself.
3. Do the same under **Add Sonarr Server** with URL base `/sonarr`. Choose **English** as the language profile when it asks.

From then on, an approved request adds the title to your library, with the seasons that were asked for monitored and the others not. When Overseerr asks for a search, Mediarium searches straight away, like **Search now**. Overseerr's tags (for example who asked) become Mediarium tags on the title. Overseerr shows what's downloading from Mediarium's queue, and marks a title available once it's downloaded.

## Dashboards and phone apps

- **Homepage**: add a `radarr`, `sonarr` or `lidarr` widget with `url: http://<mediarium>:8264/radarr` (or `/sonarr`, `/lidarr`) and the key. It shows what's wanted, missing and downloading.
- **Homarr**: add a Radarr, Sonarr or Lidarr integration with the full address and the key. The calendar and the download list work.
- **matcharr** (group movie picking): add Radarr with the address `http://<mediarium>:8264/radarr` and the key. A match is added to your library and searched for; a movie you already have counts as done.
- **nzb360, LunaSea, Ruddarr**: add a Radarr or Sonarr instance with the full address and the key. You can browse the library, add movies and shows, change monitoring and quality profile, search, and follow the download queue and the calendar. The interactive release search works too: the app lists what your indexers have for a movie, an episode or a season, and tapping one downloads it. Each release comes with what Radarr and Sonarr report: its quality and revision (repack or proper), the custom formats from your quality profile that it matches and their total score, the edition, seeders and leechers for torrents, and the reasons an automatic search would skip it. The best match is listed first, as in the web app. If an app shows an error, check the log: a request Mediarium does not answer is written there with its address (never the key).

## Tdarr and other apps that change your files

Tdarr and similar apps change a file after it was imported (a new codec, sometimes a new extension) and then tell Radarr or Sonarr to look again, so that the media server finds the new file. Add Mediarium to them like Radarr or Sonarr (the address and key above). When they send **RescanMovie**, **RefreshMovie**, **RescanSeries** or **RefreshSeries**, Mediarium checks each downloaded file of that title: a file that is still there stays as it is, a file that only changed its extension (`.mp4` became `.mkv`, same name) is found under its new name, and then your media servers are asked to scan the folder. A file that can't be found is left alone (it is never marked missing or deleted). Tdarr itself works on the files in your library folders, so mount the same folders into it.

## Add-ons

**Settings > Connections > Add-ons** keeps links to other programs you run next to Mediarium: a channel guide, a dashboard, a library tool of your own. Each has a name, an address and a way to open: **In a new tab**, or **Inside Mediarium**, which shows the program in a frame on its own page (`/apps/...`). Everyone who can sign in sees the ones that are switched on in the players menu (the play button at the bottom of the sidebar); only an administrator edits the list.

- Mediarium doesn't run these programs or load their code. They are separate projects with their own licences and rules, and Mediarium is not responsible for what they do.
- For **Inside Mediarium**, Mediarium allows exactly the addresses of the add-ons you set to open that way to be shown in a frame, and nothing else. An add-on on the same address as Mediarium itself (for example behind the same reverse proxy under `/tdarr/`) cannot be set to open inside Mediarium, because a frame would not keep it apart from your signed-in session: open it in a new tab. A program that refuses to be framed (many send `X-Frame-Options`) has to open in a new tab. An add-on's address must be a plain `http://` or `https://` address with a host name or IP address and maybe a port: spaces, quotes and semicolons are refused.
- An add-on that wants to read your library should get its own **read-only API key** from the Profile page (a read-only key can't change anything), so you can switch it off with one click by revoking the key.
- Up to 20. Stored as `apps.links`; scripts use `GET /api/apps` and `PUT /api/apps` (administrators).

## What works and what doesn't

| Works | Notes |
|---|---|
| Library lists, details, lookup by TMDB id, TVDB id or title | A show's TVDB id is read from TMDB when the show is added or refreshed; older shows get theirs the first time an app lists them. |
| Adding a movie, show or artist | The quality profile is used. Root folder, minimum availability and language profile are accepted and ignored. |
| Monitoring a title, season or episode; quality profile; tags; show type | Also through the "editor" endpoints that change many titles at once. |
| Searching (one movie, a show, a season, episodes, everything missing) | Show and season searches only look for monitored episodes, as in Sonarr. A search for everything missing searches up to 25 titles; the scheduled search does the rest. |
| Removing a title | Files go to the recycle bin when the app asks to delete them. |
| History (also since a date, and per movie or show), episode and movie files (with `mediaInfo`: codecs, bit depth, HDR, resolution, audio channels and languages, subtitles), parsing a release name | History is made from your downloads: each one is a "grabbed" line, and one that finished or failed gets a second line (imported or failed). Filter by event type (a number or a name such as `downloadFolderImported`, one or several, repeated or separated by commas), by movie or show ids, and page and sort it, as the apps do. Every line carries its movie (with the poster), show and episode. For Radarr an import is answered as both `downloadFolderImported` (3) and `movieFolderImported` (6), once under the id the app asked for, and a movie or an episode that is in your library with a file but never came through a download of Mediarium's gets one import line, dated by when it was added or when its file arrived, so "recently downloaded" cards are not empty. In a list that is not about one movie or show and has no start date, those lines go back 30 days only, so a card does not fill up with your whole library; a start date (`date`, `since`, `start`) or a movie or show id brings them all. A plain list such as `/history/since` keeps a `pageSize` or `limit` the app sends, newest first. What an app asks the history for is written to the log at debug level (switch on **Detailed logging** under Settings > Logs; see [Logs and errors](logs-and-errors.md#detailed-logging)). |
| Download queue, queue counts, wanted and missing, calendar | A season pack is one entry per episode, as Sonarr shows it. Removing an entry stops the download. Missing movies are the monitored ones without a file that are out; a movie still waiting for a release is not on the list, so nzb360 does not show it as missing (`monitored=false` adds the unmonitored ones) and can be sorted as the app asks. The calendar, the missing list and the episode lists carry each show with its pictures, and the episode file when there is one. |
| Pictures and credits | Every movie and show carries its poster, fanart (and banner, for a show) with the local address (`/MediaCover/<id>/poster.jpg`) and the TMDB address. The local address works under the app's own prefix and under its API path (`/radarr/MediaCover/...`, `/radarr/api/v3/mediacover/...`), with a size such as `poster-250.jpg`, and sends the app on to the TMDB picture. Radarr's `/credit?movieId=` lists the cast and crew TMDB has for a movie (an empty list without a TMDB key). The recommended movies (`/importlist/movie`) are what is trending on TMDB without what you own, with `isRecommendation` set. |
| Cutoff unmet (monitored movies and episodes whose file is below what the profile is after), custom formats, saved filters | The custom formats are listed by name only, without their conditions. Saved filters are always empty. |
| Root folders, disk space, quality profiles, tags | |
| Folder browsing (`/filesystem`, also the type of a path and the video files in a folder) | Lists your library folders and the downloads folder and what is inside them, folders only unless the app asks for files. Anything outside those folders, and a link that leads out of them, is left out. |
| Removing several queue entries at once, health check (`/ping`), log file list | The log file list is always empty. |

Not available: manual imports and renaming (an empty list is answered), deleted or renamed file lines in history, the app's own settings pages (the lists and pages an app loads at start, such as notifications, import lists, remote path mappings and display settings, are answered with empty or fixed values), releases pushed from other tools (always turned down), and changing quality profiles from the app. Anything an app asks that has no meaning in Mediarium (such as a "refresh downloads" command) is accepted and does nothing.

## For scripts

The endpoints are Radarr's and Sonarr's API v3 and Lidarr's API v1 under the prefixes above, for example `GET /radarr/api/v3/movie` or `POST /sonarr/api/v3/command` with `{"name": "SeriesSearch", "seriesId": 3}`. Commands run one after another in the background, so a long list never holds up a search you start yourself; one command may name at most 200 movies, episodes or albums, and when 20 commands are already waiting a new one is turned away until they have run. They follow the shapes of the real apps closely enough for the apps listed here. For anything else, Mediarium's own API ([reference/api.md](./reference/api.md)) covers more and is the one to build on.
