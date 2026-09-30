# One-click store kits

Ready-to-submit files for app stores and one-click installers. **Nothing here has been submitted**: every store needs a public repository, a published image and the maintainer's own accounts. Until they are live, the docs list these as "coming soon" ([docs/PLATFORMS.md](../../docs/PLATFORMS.md)).

| Store | Folder | How it gets listed | Tested |
|---|---|---|---|
| Unraid Community Applications | [`unraid/`](./unraid/) | Template repo + submit at ca.unraid.net | XML well formed; not loaded on Unraid |
| CasaOS / ZimaOS App Store | [`casaos/`](./casaos/) | Pull request to IceWhaleTech/CasaOS-AppStore | YAML parses; not on a device |
| Umbrel | [`umbrel/`](./umbrel/) | Own community store repo (later: pull request to getumbrel/umbrel-apps) | YAML parses; not on a device |
| TrueNAS SCALE | [`truenas/`](./truenas/) | Issue + pull request to truenas/apps (community train) | Rendered with the catalog's validator and run with a local image (healthy); not on TrueNAS |
| Portainer | [`portainer/`](./portainer/) | Users add the template URL; nothing to submit | JSON parses; not in Portainer |
| Synology | [`synology/`](./synology/) | No store for Docker apps; Container Manager project | Same compose as the tested install |
| Runtipi (optional) | [`runtipi/`](./runtipi/) | Own app store repo | JSON/YAML parse; not on Runtipi |
| Cosmos Cloud (optional) | [`cosmos/`](./cosmos/) | Pull request to azukaar/cosmos-servapps-official | JSON parses; not on Cosmos |

## Do these first (needed by every store)

1. **The GitHub repository must be public** (the icon, screenshots, templates and stack files are fetched from `raw.githubusercontent.com/rdborg/Mediarium/main/...`).
2. **Set the release secrets** if official images should ship with the built-in TMDB, OpenSubtitles and Trakt keys: repository secrets `TMDB_API_KEY`, `OPENSUBTITLES_API_KEY`, `TRAKT_CLIENT_ID` (see `.github/workflows/release.yml`). Without them users are asked for their own keys in the setup wizard.
3. **Push the `v1.3.0` tag** so the release workflow publishes `ghcr.io/rdborg/mediarium:1.3.0`, `:1.3` and `:latest` (and `:1.3.0-full`, `:1.3-full` and `:latest-full`) for `linux/amd64` and `linux/arm64`. Check the workflow run is green. The kits use the plain image, not `-full`.
4. **Make the container package public**: GitHub → Packages → `mediarium` → Package settings → Change visibility → Public. (New GHCR packages start private, and every store pulls anonymously.)
5. **Check from a machine that is not logged in to GitHub**:
   ```bash
   docker pull ghcr.io/rdborg/mediarium:latest
   docker buildx imagetools inspect ghcr.io/rdborg/mediarium:1.3.0   # shows amd64 + arm64, and the digest
   ```
6. **Pin digests** where a store requires it (Umbrel, TrueNAS; recommended elsewhere): use the `Digest:` from step 5 as `1.3.0@sha256:...`.

## For every new release

Stores that pin a version (CasaOS, Umbrel, TrueNAS, Runtipi) need their version, image tag and digest bumped, usually by a small pull request to the store. Unraid, Portainer, Synology and Cosmos follow `latest` and need nothing.

## When a store goes live

Update `docs/PLATFORMS.md` (move the row from "Coming soon" to "Available now"), the table in `docs/INSTALL.md` (Using a Docker app), the README's platform table, and `CHANGELOG.md`.
