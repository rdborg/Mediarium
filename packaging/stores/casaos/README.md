# CasaOS / ZimaOS App Store: submission kit

**Status: ready to submit once the requirements below are met. Not submitted.**

The CasaOS App Store ([IceWhaleTech/CasaOS-AppStore](https://github.com/IceWhaleTech/CasaOS-AppStore)) is also the ZimaOS store. Apps are added by pull request.

## Files here

`Apps/Mediarium/` is laid out exactly as it goes into the store repo:

| File | What it is |
|---|---|
| `docker-compose.yml` | The app: image `ghcr.io/rdborg/mediarium:2.1.0`, ports 8264 and 58264 (TCP+UDP), `/DATA/AppData/$AppID/config` as `/config`, `/DATA/Media` as `/data`, and the top-level `x-casaos` block (id, title, tagline, description, category `Media`, version, icon, screenshots, tips). |
| `icon.svg` | The Mediarium icon (from `branding/mediarium-icon.svg`). |
| `thumbnail.png` | 784 x 442. |
| `screenshot-1.png` ... `screenshot-3.png` | 1280 x 720 (Dashboard, Discover, Library, from the demo data). |

The icon and screenshot URLs point at the store's own jsDelivr mirror (`cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/Mediarium/...`), so they work once the pull request is merged.

## Requirements

- [ ] The Mediarium repository is public and the image `ghcr.io/rdborg/mediarium:2.1.0` is published and **public** (GitHub package visibility). The store does not accept `latest`: use a version tag, and update `image`, `version` and `update_at` in the compose file for each release you want the store to carry.
- [ ] Both architectures in `architectures` (`amd64`, `arm64`) exist in the image (the release workflow builds both).
- [ ] Tested on a real CasaOS or ZimaOS device: App Store → **+** → **Install a customized app** → import this compose file.
- [ ] Check the default media folder names on your device. This file assumes `/DATA/Media/Movies` and `/DATA/Media/TV Shows` (created if missing) and downloads in `/DATA/Media/Downloads/mediarium`. If your CasaOS uses other names, adjust `MOVIES_DIR`/`TV_DIR` to match what Jellyfin's CasaOS app expects.

## Steps

1. Fork [IceWhaleTech/CasaOS-AppStore](https://github.com/IceWhaleTech/CasaOS-AppStore) and create a branch.
2. Copy this `Apps/Mediarium/` folder into the fork's `Apps/`.
3. Read the repo's current `CONTRIBUTING.md` and `docs/specs/` (they change) and run its checks locally: `docker compose -f Apps/Mediarium/docker-compose.yml config -q`, then `./scripts/build_dist.sh`; it must produce `dist/apps/io.github.rdborg.mediarium/`.
4. Open a pull request: "Add Mediarium". Say it is a new app, what it does, and how you tested it (device, CasaOS/ZimaOS version). Attach a screenshot of it running.
5. Answer review comments. After merge it shows in the App Store after the store refreshes.
6. Then update `docs/PLATFORMS.md` (and the CasaOS row in `docs/INSTALL.md`).

## What only the maintainer can do

- Publish the image and make it public; fork the store repo with your GitHub account, test on a CasaOS/ZimaOS device and open the pull request.
