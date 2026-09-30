# TrueNAS SCALE apps catalog: submission kit

**Status: prepared and render-tested, not submitted.**

TrueNAS SCALE 24.10 and newer install apps from the [truenas/apps](https://github.com/truenas/apps) catalog. Community apps live in the `community` train and are added by pull request.

Until Mediarium is in the catalog, TrueNAS users can paste the normal compose file: Apps → Discover Apps → the menu next to **Custom App** → **Install via YAML** (see [INSTALL.md](../../../docs/INSTALL.md#using-a-docker-app)). Use one dataset for downloads, movies and TV, because every dataset is its own filesystem and hardlinks cannot cross datasets.

## Files here

`ix-dev/community/mediarium/` is laid out as it goes into the catalog repo:

| File | What it is |
|---|---|
| `app.yaml` | Catalog metadata: `app_version: 1.3.0`, catalog `version: 1.0.0`, category `media`, icon/screenshot URLs on `media.sys.truenas.net`, run-as 568:568. |
| `ix_values.yaml` | The image (`ghcr.io/rdborg/mediarium`, tag to be pinned with a digest) and the permissions helper image. |
| `questions.yaml` | The install form, based on the catalog's Sonarr app: timezone; the downloads, movies and TV folders (inside `/data`); user and group (568 by default); web port 8264 and torrent port 58264 (TCP+UDP); **Config** storage (ixVolume or host path) and **Data** storage (a host path, the one dataset holding downloads, movies and TV) plus additional storage; labels; resources. |
| `templates/docker-compose.yaml` | Renders the app with the catalog's library: runs as the chosen user (the image supports starting as a non-root user), sets `DOWNLOADS_DIR`/`MOVIES_DIR`/`TV_DIR`, health check on `/api/version`, ports and storage. |
| `templates/test_values/basic-values.yaml` | Values for the catalog's CI. |
| `README.md` | The one-paragraph description the catalog shows. |

### What was tested

The template was rendered with the catalog's own validation image (`ghcr.io/truenas/apps_validation`, `apps_render_app render`) against library `2.3.15` and `basic-values.yaml`, and the rendered compose was then run with a locally built Mediarium image: it started as user 568 with all capabilities dropped, created `/data/downloads/mediarium`, `/data/media/movies` and `/data/media/tv`, answered `/api/onboarding/status`, and the health check reported **healthy**. It has not been run on a real TrueNAS machine.

## Requirements

- [ ] The Mediarium repository is public, and `ghcr.io/rdborg/mediarium:1.3.0` is published and **public**, for `amd64` (TrueNAS SCALE is amd64 only).
- [ ] Pin the image in `ix_values.yaml`: `tag: "1.3.0@sha256:<digest>"` (get it with `docker buildx imagetools inspect ghcr.io/rdborg/mediarium:1.3.0`). Renovate keeps it updated afterwards.
- [ ] An icon (PNG, square) and 3 screenshots to attach to the pull request; the TrueNAS team uploads them to `media.sys.truenas.net` and the URLs in `app.yaml` become real. `branding/mediarium-icon-512.png` and `docs/images/*.png` are suitable.

## Steps

1. **Open an issue** in [truenas/apps](https://github.com/truenas/apps/issues) proposing the app (their pull request checklist asks for one), then fork the repo.
2. Copy `ix-dev/community/mediarium/` into the fork.
3. In the fork, following its `CONTRIBUTIONS.md`:
   - `devbox run copy-lib` (or their documented equivalent) to vendor the library into `templates/library/` and fill `lib_version_hash` (currently a placeholder) and `lib_version`.
   - `./.github/scripts/port_validation.py`: the catalog wants a **unique default web port**. If 8264 is taken or refused, use the port it suggests in `questions.yaml` and `basic-values.yaml` (the app listens on 8264 inside the container either way).
   - `./.github/scripts/ci.py --app mediarium --train community --test-file basic-values.yaml` (render, then a real start).
   - `./.github/scripts/generate_metadata.py --app mediarium --train community`.
   - Update `date_added` in `app.yaml` to the submission date.
4. Open a draft pull request early, fill in their template (including the AI-assisted checkbox: these files were prepared with AI assistance), attach the icon and screenshots.
5. After it is merged and released to the train, update `docs/PLATFORMS.md` and the TrueNAS row in `docs/INSTALL.md`.

## Things to verify during review

- The `maintainers:` block: the catalog currently lists TrueNAS itself for community apps.
- `min_scale_version`: copied from the Sonarr app (24.10.2.2).
- The questions for the torrent port: if reviewers prefer, drop it (torrents work without it).

## What only the maintainer can do

- Publish the image, open the issue and pull request with your GitHub account, and answer the reviewers.
