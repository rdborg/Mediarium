# Mediarium documentation

Start here. The reference pages are generated from the code, and every change to the app updates its page in the same commit.

| I want to... | Read |
|---|---|
| Install it with Docker (one compose file, any Linux server or NAS) | [INSTALL.md](./INSTALL.md) |
| Install it on Synology, Unraid, QNAP or Linux, step by step | [synology.md](./synology.md), [unraid.md](./unraid.md), [qnap.md](./qnap.md), [linux.md](./linux.md) |
| See which platforms work now and which are coming soon (app stores, Proxmox, Raspberry Pi) | [PLATFORMS.md](./PLATFORMS.md) |
| See what it can and cannot do today, and what is planned | [FEATURES.md](./FEATURES.md) |
| Move over from Radarr, Sonarr, Prowlarr, SABnzbd and other apps without redoing your setup or moving files | [migrate.md](./migrate.md) |
| Add movies and shows you already have on disk (Import existing in the Library) | [import-library.md](./import-library.md) |
| Find something to add (Discover): more like your library, browse more, older titles, and the Add dialog | [discover.md](./discover.md) |
| Open the torrent port (58264), seeding, how many downloads run at once, cleaning up the downloads folder, what removing a title deletes | [downloads.md](./downloads.md) |
| Send torrent traffic through the built-in WireGuard VPN, and what the kill switch does | [vpn.md](./vpn.md) |
| Add indexers: Usenet and torrent APIs, sites from the definition list, private sites, Cloudflare and FlareSolverr | [indexers.md](./indexers.md) |
| Give family members their own login, see what members can do, reset a password | [accounts.md](./accounts.md) |
| Reach Mediarium from the internet safely (reverse proxy checklist, trusted proxies, what the app protects) | [security.md](./security.md) |
| Choose a quality profile (the built-in presets, cutoffs, upgrades, fallback profiles) | [quality-profiles.md](./quality-profiles.md) |
| See why a title has not downloaded, read the Activity page, pause, resume or stop a download, retry without downloading again | [activity.md](./activity.md) |
| Subtitles: the on/off switch, release subtitles, offers, daily limits, dismissing | [subtitles.md](./subtitles.md) |
| Switch movies, TV, music, ebooks and audiobooks on or off (Settings > Media types) | [modules.md](./modules.md) |
| Change many movies, shows or artists at once from the Library (monitor, better versions, quality profile, download from, search now, remove) | [library.md](./library.md) |
| Manage music (switch on the music module, artists and albums, audio quality, importing a collection) | [music.md](./music.md) |
| Ebooks and audiobooks (switch them on, add books, which release is picked, where files go) | [books.md](./books.md) |
| Connect Plex, Jellyfin or Emby (library refresh after imports, "Watch in" links, finding the token or API key, path mapping) | [media-servers.md](./media-servers.md) |
| Set up notifications (email, ntfy, Gotify, Pushover, Slack, Discord, Telegram, webhook), turn them on and off, test them | [notifications.md](./notifications.md) |
| Find out what went wrong (a failed download, "too many connections", a full disk) and what to try, copy a report for support | [logs-and-errors.md](./logs-and-errors.md) |
| Understand the responsible-use notice | [LEGAL.md](./LEGAL.md) |
| Look up an HTTP endpoint | [reference/api.md](./reference/api.md) |
| Look up an environment variable | [reference/environment.md](./reference/environment.md) |
| Look up a stored setting | [reference/settings-keys.md](./reference/settings-keys.md) |
| Look up a problem code from Logs and errors | [reference/problem-codes.md](./reference/problem-codes.md) |
| Know when a new version is out, update from inside the app, restart Mediarium, or push a program file to a running install | [INSTALL.md](./INSTALL.md#updating), [INSTALL.md](./INSTALL.md#updating-without-rebuilding-the-container) and [security.md](./security.md#updating-the-program) |
| Understand version numbers, sign and publish a release, or push a test build | [RELEASING.md](./RELEASING.md) |

## Keeping the docs current

- Change a feature, setting or install step: update its page here and add a line to `CHANGELOG.md` in the same pull request. CI fails a pull request that changes the app without touching the docs (add the `no-docs-needed` label if nothing visible changed).
- Change a route, an environment variable or a settings key: run `go run ./tools/docgen` and commit the regenerated `docs/reference/*.md`. CI fails if they are out of date.
- The app links here from its About and credits page. The address lives in one place, `web/src/docs.ts`, so it can move to a website later.
