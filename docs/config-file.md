# Keep your settings in a file

If you like your setup in git, or you set up Mediarium from a script, you can keep the settings in one YAML file and apply it. This is a command you run yourself. Mediarium doesn't read the file when it starts unless you switch that on (see [Settings file with a check](config-as-code.md)), so nothing changes behind your back, and you can still change anything on the Settings pages. The same file can also be downloaded, checked and applied from Settings > System, with a check first and a copy of the database before anything changes.

The command talks to your running Mediarium through its API, so every value is checked exactly as it is on the Settings pages. A value Mediarium refuses is reported, and nothing is written for that block.

## What the file can hold

One block for each Settings page that holds a single set of values:

| Block | Where it is in Mediarium |
|---|---|
| `settings` | General, Library, Downloading, Info, lists and subtitles, and the rest of Settings that is one set of values (folders, naming, schedules, languages, subtitles, retention...) |
| `downloadClient` | Settings > Downloading > Torrents > **Use your own torrent client** (qBittorrent, Transmission, RDT-Client, Decypharr) |
| `debrid` | Settings > Downloading > Torrents > **Debrid service** |
| `proxy` | The SOCKS5 or HTTP proxy for Usenet, indexers and torrents ([vpn.md](./vpn.md#a-socks5-or-http-proxy-instead)) |
| `systemOptions` | The update options under Settings > System (daily check, overnight install, restart when stuck) |

Things you add several of are **lists**. Each item has a `name`, and Mediarium finds the one you mean by that name (capital letters don't matter):

| List | Where it is in Mediarium |
|---|---|
| `indexers` | Settings > Indexers (Newznab, Torznab and the sites from the site list) |
| `usenetServers` | Settings > Downloading > Usenet servers |
| `mediaServers` | Settings > Media servers (Plex, Jellyfin, Emby, Kodi) |
| `qualityProfiles` | Settings > Profiles |
| `notifications` | Settings > Notifications (the `config:` lines are the fields of that kind of channel) |

An item Mediarium doesn't have is **added**, one it has is **changed** to match the file, and one that is not in the file is **left alone**. Nothing is ever deleted by the file: delete on the Settings page.

Custom formats, import lists and accounts are not in the file yet.

In `settings`, `defaultProfileId` is the quality profile movies use when they have none of their own, and `defaultTvProfileId` is the one shows use (`0` means shows use the movie default). Both are profile numbers, as shown by `config export`.

```yaml
usenetServers:
  - name: Main server
    host: news.example.com
    port: 563
    useSsl: true
    username: me
    password: ${NEWS_PASSWORD}
    connections: 20

indexers:
  - name: My Newznab
    kind: newznab
    baseUrl: https://indexer.example.com
    apiKey: ${NZB_KEY}
    protocol: usenet
    priority: 10         # 1 to 50, lower is tried first (25 = default)
    requestsPerDay: 25   # most requests a day (0 = none set)
    minSeeders: 5        # torrent indexers: skip releases with fewer seeders (0 = off, up to 1000)
    torrentRoute: client # torrent indexers: client, debrid or builtin (left out = the usual order)
    extraCategories:     # more category numbers to search, per kind: movies, tv, music, books, audiobooks
      tv: [105000]
      books: [7120]

qualityProfiles:
  - name: Any HD
    allowed: [WEBDL-720p, WEBDL-1080p, Bluray-1080p]
    cutoff: WEBDL-1080p
  - name: Only 1080p
    allowed: [WEBDL-1080p, Bluray-1080p]
    cutoff: WEBDL-1080p
    upgradeAllowed: true
    fallback: [Any HD]   # other profiles, by name
    formatScores:        # custom formats, by name
      - format: x265 (HD)
        score: 10
```

A server, indexer or media server added from the file is switched on, unless the file says `enabled: false`.

In a quality profile, `fallback` lists other profiles by their **name**, and each of `formatScores` names a custom format with `format:`. A profile used as a fallback has to be in Mediarium already or come earlier in the file; a custom format has to be in Mediarium already (custom formats are not in the file). A name Mediarium doesn't have is reported for that profile, and the profile is left as it is.

For an indexer from the site list, use `kind: cardigann`, the `definitionId`, and the site's own settings under `settings:` (a password or cookie there can be `${NAME}` too). The names under `settings:` are checked against the site's own settings, so a typo stops the command like any other (when Mediarium can't read the list of sites, they are passed on unchecked). Newznab and Torznab indexers have no `settings:`.

An indexer's `kind` and `definitionId`, and the `protocol` and `apiKey` of an indexer from the site list, can't be changed once it is added (remove it and add it again). The file still accepts them, but they are not compared or sent for an indexer Mediarium already has.

## Start from what you have

```bash
docker exec -e MEDIARIUM_API_KEY=your-key mediarium /app/app config export -o /config/mediarium.yml
```

An administrator makes the API key on the **Profile** page. The file is written inside the container, so use a folder that is mounted (`/config` is). Passwords and keys are never exported.

Open the file and **delete every line you don't want to control**. A line that is not in the file is left alone. A short file is a good file:

```yaml
settings:
  huntIntervalHours: 12
  trashDays: 14
  automationEnabled: true
  unsafeExtensions: [.exe, .scr, .bat]

downloadClient:
  kind: transmission
  url: http://transmission:9091
  username: admin
  password: ${TRANSMISSION_PASSWORD}
  category: mediarium

debrid:
  preferCached: true
```

## Secrets

An export writes `<secret>` where Mediarium has a password or key saved: it means "keep the one that is saved", and a `<secret>` for something the file adds means the password is still to be entered. The first line of a file, `formatVersion: 1`, is the version of the file format; a file from a newer Mediarium is refused.

To put a real one in the file, write a password or a key as `${NAME}`. It is replaced with the environment variable `NAME` when the file is applied, so the secret never has to be in the file or in git. `${NAME:-fallback}` uses `fallback` when `NAME` isn't set. A `${NAME}` with no value and no fallback stops the command before anything is changed.

These are the values Mediarium accepts but never shows back: in `settings` `tmdbApiKey`, `openSubtitlesApiKey`, `openSubtitlesUsername`, `openSubtitlesPassword`, `subdlApiKey`, `traktClientId` and `simklClientId`; `password` in `downloadClient` and `proxy`; `apiKey` in `debrid`; and in the lists the `password` of a Usenet server, the `apiKey` of an indexer (and the passwords of a site's `settings`) and the `token` of a media server. Because they can't be compared, they are sent again every time you apply.

## See what would change, then apply

```bash
docker exec -e MEDIARIUM_API_KEY=your-key -e TRANSMISSION_PASSWORD=... mediarium /app/app config diff /config/mediarium.yml
docker exec -e MEDIARIUM_API_KEY=your-key -e TRANSMISSION_PASSWORD=... mediarium /app/app config apply /config/mediarium.yml
```

`diff` (or `apply -n`) prints one line per difference, for example `settings.huntIntervalHours: 6 -> 12`, and changes nothing. `apply` makes the changes and prints what it did. Run it again and it says there is nothing to change.

- A name that isn't one of Mediarium's settings (a typo) stops the command before anything is changed, and the name is listed.
- A block Mediarium refuses is reported, and the other blocks are still applied. The exit code is then 1, so a script can tell.
- A few things can't be done with an API key, for security (for example switching on pushed updates, `systemOptions.allowPush`). Mediarium says so; do those on the Settings page.

Options for all three commands: `--url` (the address of your Mediarium; the default is `http://127.0.0.1:8264`, or `$MEDIARIUM_URL`) and `--key` (the API key; the default is `$MEDIARIUM_API_KEY`). Options go before the file name.

Outside Docker, the same command is `mediarium config apply mediarium.yml` (the program file is the `app` binary).

## Idea or missing block

This is a first version, started from what people asked for. If a setting you want in git is not in one of these blocks, say which on [GitHub](https://github.com/rdborg/Mediarium/issues) and it will be added.
