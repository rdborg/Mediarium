# Runtipi: app store kit (optional)

**Status: prepared, not published.**

Runtipi's official app store no longer accepts new apps; its README asks developers to publish their own store instead. Users add a store by URL (Settings → App Stores → Add App Store).

## Files here

`apps/mediarium/` in Runtipi's current format:

| File | What it is |
|---|---|
| `config.json` | Store metadata: id, name, version, port 8264, category `media`, `amd64`/`arm64`. |
| `docker-compose.yml` | The app (schema version 2): `/config` in the app's data folder, Runtipi's media folder mounted once as `/data`, torrent port 58264 TCP+UDP; Runtipi maps the web port itself. |
| `metadata/logo.jpg` | Square logo. |
| `metadata/description.md` | The text shown in the store. |

## Steps

1. Create a store repo from Runtipi's template [runtipi/example-appstore](https://github.com/runtipi/example-appstore) (for example `rdborg/runtipi-appstore`) and copy `apps/mediarium/` into its `apps/` folder. The template contains the `app-info-schema.json` that `config.json` refers to; run the template's checks if it has any.
2. Set `created_at` and `updated_at` in `config.json` to the real publish time (milliseconds since 1970).
3. On a Runtipi server: Settings → App Stores → add `https://github.com/rdborg/runtipi-appstore`, install Mediarium, check the wizard shows `/data/downloads/mediarium`, `/data/movies` and `/data/tv` as writable.
4. Check where Runtipi's other media apps (Jellyfin, Sonarr) keep movies and TV in the media folder, and change `MOVIES_DIR`/`TV_DIR` to match so they share the library.
5. For each release: bump `version`, `tipi_version` (+1), `updated_at` and the image tag.

## Requirements

- [ ] Image `ghcr.io/rdborg/mediarium:2.1.0` published and public (Runtipi discourages `latest`).
- [ ] A Runtipi install to test on.

## What only the maintainer can do

- Create and maintain the store repo, test on Runtipi.
