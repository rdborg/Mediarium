# Mediarium documentation

Start here. Pages are kept in step with the code: the reference pages are generated from it, and every change to the app updates the matching page in the same commit.

| I want to... | Read |
|---|---|
| Install it (Docker, Synology, Unraid, QNAP, TrueNAS, Portainer, CasaOS, Windows, macOS, Linux) | [INSTALL.md](./INSTALL.md) |
| See what it can and cannot do today, and what is planned | [FEATURES.md](./FEATURES.md) |
| Open the torrent port (58264), understand seeding, clean up the downloads folder, know what removing a title deletes | [downloads.md](./downloads.md) |
| Add indexers: Usenet and torrent APIs, sites from the definition list, private sites, Cloudflare and FlareSolverr | [indexers.md](./indexers.md) |
| Give family members their own login, see what members can do, reset a password | [accounts.md](./accounts.md) |
| Choose a quality profile (the built-in presets, cutoffs, upgrades, fallback profiles) | [quality-profiles.md](./quality-profiles.md) |
| See why a title has not downloaded (its own activity log), the one-download-per-title rule, retrying without downloading again | [activity.md](./activity.md) |
| Understand how subtitles work (release subtitles, offers, daily limits, dismissing) | [subtitles.md](./subtitles.md) |
| Connect Plex, Jellyfin or Emby (library refresh after imports, "Watch in" links, finding the token or API key, path mapping) | [media-servers.md](./media-servers.md) |
| Understand the responsible-use notice | [LEGAL.md](./LEGAL.md) |
| Look up an HTTP endpoint | [reference/api.md](./reference/api.md) |
| Look up an environment variable | [reference/environment.md](./reference/environment.md) |
| Look up a stored setting | [reference/settings-keys.md](./reference/settings-keys.md) |
| Understand version numbers, or publish a release | [RELEASING.md](./RELEASING.md) |
| Platform notes | [synology.md](./synology.md), [unraid.md](./unraid.md), [qnap.md](./qnap.md), [linux.md](./linux.md) |

## Keeping the docs current

- Change a feature, setting or install step: update its page here and add a line to `CHANGELOG.md` in the same pull request. CI fails a pull request that changes the app without touching the docs (add the `no-docs-needed` label if nothing user-visible changed).
- Change a route, an environment variable or a settings key: run `go run ./tools/docgen` and commit the regenerated `docs/reference/*.md`. CI fails if they are out of date.
- The app links here from its About & Credits page. The address lives in one place, `web/src/docs.ts`, so it can move to a website later.
