# Umbrel: submission kit

**Status: ready to publish as a community app store once the requirements below are met. Not published.**

Umbrel has two routes:

1. **A community app store** (recommended first): a public GitHub repo that Umbrel users add by URL. You control it; no review needed.
2. **The official Umbrel App Store** ([getumbrel/umbrel-apps](https://github.com/getumbrel/umbrel-apps)): reviewed pull request, stricter rules. Worth doing once the community store has had real users.

## Files here

Laid out as the root of a community app store repo:

| File | What it is |
|---|---|
| `umbrel-app-store.yml` | Store id `rdborg`, name "Mediarium" (shown as "Mediarium App Store"). |
| `rdborg-mediarium/umbrel-app.yml` | The app manifest: id, category `media`, version `1.3.0`, tagline, description, port `8264`, gallery and icon URLs, `STORAGE_DOWNLOADS` permission. |
| `rdborg-mediarium/docker-compose.yml` | Umbrel's `app_proxy` in front of the `web` service on port 8264; `/config` in the app's data folder; Umbrel's shared Downloads folder mounted once as `/data` (with `mediarium`, `movies` and `tv` inside, so imports hardlink); torrent port 58264 TCP+UDP. |
| `rdborg-mediarium/data/config/.gitkeep` | Keeps the empty config folder in git. |

## Requirements

- [ ] The Mediarium repository is public (the icon and gallery URLs point at `raw.githubusercontent.com/rdborg/Mediarium/main/...`).
- [ ] The image `ghcr.io/rdborg/mediarium:1.3.0` is published and **public**, with both `amd64` and `arm64` (Umbrel Home is amd64, Raspberry Pi installs are arm64).
- [ ] **Pin the image by digest** (required for the official store, strongly recommended for a community store). After the release, get the multi-arch index digest with `docker buildx imagetools inspect ghcr.io/rdborg/mediarium:1.3.0` (the `Digest:` line at the top) and set `image: ghcr.io/rdborg/mediarium:1.3.0@sha256:<digest>`.
- [ ] Tested on an Umbrel device.

## Steps: community app store

1. Create a public GitHub repo, for example `rdborg/umbrel-app-store` (the name used in `submission:`), and copy this folder's contents to its root. Umbrel's template repo [getumbrel/umbrel-community-app-store](https://github.com/getumbrel/umbrel-community-app-store) shows the same layout.
2. Pin the image digest (above) and push.
3. On an Umbrel: **App Store → ⋯ (top right) → Community App Stores**, paste `https://github.com/rdborg/umbrel-app-store`, **Add**. Open the Mediarium App Store and install.
4. Check: the app opens through Umbrel, the Mediarium wizard shows `/data/mediarium`, `/data/movies` and `/data/tv` as writable, and a media server app on the same Umbrel (Jellyfin, Plex) can see the movies in the Downloads folder.
5. Update `docs/PLATFORMS.md` with the store URL.

## Steps: official store (later)

1. Fork [getumbrel/umbrel-apps](https://github.com/getumbrel/umbrel-apps) and read its current contribution rules (`AGENTS.md` and the packaging guide it points to).
2. Copy `rdborg-mediarium/` as **`mediarium/`** (no store prefix), set `id: mediarium` in `umbrel-app.yml`, `APP_HOST: mediarium_web_1` in the compose file, remove `icon:` and set `gallery: []` (Umbrel hosts the images; attach a 256x256 icon and 3 to 5 screenshots to the pull request instead), pin the image by digest, and set `submission:` to the pull request URL.
3. Run `npm run lint:apps -- mediarium --check-images` in the fork, then open the pull request with the version, the upstream repo, the image source, how you tested it, and the images.

## Notes

- Umbrel puts its own login in front of apps by default, and Mediarium has its own login too. The compose file leaves Umbrel's login on for the web page and lets `/api/*` through so API clients keep working. If the double login annoys users, `PROXY_AUTH_ADD: "false"` turns Umbrel's off for the whole app.
- Port 8264 and 58264 were free in the official Umbrel store when this kit was prepared; check again before an official submission (each app needs a unique port).

## What only the maintainer can do

- Publish the image, create the store repo, test on an Umbrel, and (later) open the official pull request.
