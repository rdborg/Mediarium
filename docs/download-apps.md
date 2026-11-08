# Watching downloads from your phone (nzb360 and other download apps)

Mediarium downloads with its own built-in Usenet and torrent downloaders. Phone apps such as nzb360 are made to talk to SABnzbd (Usenet) and qBittorrent (torrents), so they can show speed, time left, the queue and the history, and pause, resume or remove a download. Mediarium can answer like both of them, so you can add it in those apps as if it were SABnzbd or qBittorrent.

This is off until you switch it on: **Settings > Connections > Other apps > Phone apps for downloads**. The same card shows the addresses and a copy button for each.

This idea came from SanketBaraiya (GitHub discussion #177).

## What to fill in

| Add Mediarium as | Full address (example) | URL base |
|---|---|---|
| SABnzbd (Usenet) | `http://192.168.1.20:8264/sabnzbd` | `/sabnzbd` |
| qBittorrent (torrents) | `http://192.168.1.20:8264/qbittorrent` | `/qbittorrent` |

- **Host and port** are the ones you open Mediarium at. Inside Docker, use the container name or the NAS address, not `localhost`.
- **SABnzbd: API key.** An administrator's Mediarium API key. Make one on the Other apps page (**Make a key for other apps**) or under **Accounts > API keys**. Paste it into the app's API key box.
- **qBittorrent: password.** The same API key goes into the **password** box. The user name can be anything. The app signs in and keeps a cookie, like it does with the real qBittorrent. An app that can send the key as a bearer token (`Authorization: Bearer <key>`) or as an `X-Api-Key` header works without signing in first.
- A key of a basic account is refused. A read-only key can look at the lists but cannot pause, resume, remove or add.
- If Mediarium sits behind a reverse proxy under a sub-path (the `URL_BASE` setting), put that path in front, for example `https://home.example.com/mediarium/sabnzbd`.

In nzb360, open **Settings > Servers**, add a **SABnzbd** server and a **qBittorrent** server, and fill in the host, port, URL base and key as above. Press the test button. The Usenet and torrent tabs then show your downloads.

## What works

SABnzbd (`/sabnzbd/api`):

- Version and key check.
- The queue: each download with its name, status, size, size left, percentage, time left, category and priority, plus the total speed, the time left for everything, and the free disk space of the downloads folder.
- The history: finished and failed downloads with their name, status, size, category and finish time.
- Pause and resume, for everything or for one download. Remove one download or all of them from the queue, and remove entries from the history.
- Adding an NZB by address or as a file.
- The category list and a small config and status answer, so the app has something to read.

qBittorrent (`/qbittorrent/api/v2`):

- Signing in, the version, and the list of torrents with state (downloading, stalled, paused, uploading, error and so on), progress, size, speed, time left, seeds, category, save path and dates. The usual filters (`downloading`, `seeding`, `completed`, `paused`, `active`, `errored`), sorting, `limit` and `offset` work.
- Totals (`transfer/info`) and the `sync/maindata` call apps use for their home screen.
- Pause, resume and delete (with or without the files).
- Adding a torrent by magnet link, by address or as a `.torrent` file.
- A short properties answer for one torrent.

## What does not work

- **Adding needs a title in your library.** Mediarium only downloads releases for movies and shows it has. When you add an NZB or torrent from the app, the release name has to match a movie (name and year) or a show (name with season or episode) in your library. If it doesn't, the app gets an error and nothing is added. Add the title in Mediarium first.
- **The category sent with an add is ignored.** The title decides it. Categories in the lists are the kind of title: `movies`, `tv`, `music` and `books`, or the torrent category you set in Mediarium.
- **SABnzbd's global pause** pauses every Usenet download that is running or waiting, and "resume" puts every paused one back. There is no separate "paused for 10 minutes" timer.
- **Not available:** priority, renaming, sorting and changing a category in SABnzbd; the speed limit; file lists, trackers, tags, priorities and per-torrent limits in qBittorrent; the apps' own settings pages. These answer with an error or an empty list.
- **Speed and time left are worked out when the app asks.** Mediarium looks at how far a download has come since the last time somebody asked, so the first answer after a quiet period shows a speed of 0 and the next one shows the real number. Nothing runs while no app is connected.
- **Hashes are made up.** The torrent "hash" the app sees is worked out from the download's number in Mediarium. It stays the same, but it is not the torrent's real info hash.
- **Finished torrents.** The torrent list shows torrents that are not finished, torrents that still seed, and ones that finished in the last day. Older ones are in Mediarium's History.
- Downloads sent to an outside download client (qBittorrent, RDT-Client, Decypharr or a Usenet client) are listed by what Mediarium knows about them. The speed shown is what Mediarium can work out, not what that client reports.

## Safety

- Both addresses are off by default. Only a signed-in administrator can switch them on; an API key can switch them off but never on.
- They sign in with an API key only, never with the web page's cookie. Wrong keys are counted per address: after 20 wrong tries in 15 minutes that address is locked out of both for the rest of the time.
- The key is never written to the log.
- Switch the card off and both addresses answer "not found" again.

## For scripts

`GET /sabnzbd/api?mode=queue&output=json&apikey=KEY` answers like SABnzbd. `POST /qbittorrent/api/v2/auth/login` with `username=x&password=KEY` returns `Ok.` and sets the `SID` cookie, after which `GET /qbittorrent/api/v2/torrents/info` lists the torrents. The settings switch is `GET` and `PUT /api/system/downloader-api` (`{"enabled": true}`; turning it on needs a signed-in browser session).
