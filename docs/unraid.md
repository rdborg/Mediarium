# Deploying on Unraid

Part of the [master install guide](./INSTALL.md); see it for folder layout, PUID/PGID and troubleshooting.

## Community Applications template

A template exists at [`docs/unraid/mediarium.xml`](./unraid/mediarium.xml), plus a repository profile at [`docs/unraid/ca_profile.xml`](./unraid/ca_profile.xml). It has **not been submitted to Community Applications** yet: that needs a public repo, a published image and a hosted icon (none exist yet), and the current submission process was not verified. The template XML is valid XML but has not been loaded into a real Unraid server.

The template maps **two paths**: `/config` and a single `/data` share. `downloads`, `movies` and `tv` are folders inside `/data`, and the `DOWNLOADS_DIR`, `MOVIES_DIR` and `TV_DIR` variables point the app at them. (An earlier version of the template used four separate path mappings; that copies files instead of hardlinking them, because a hardlink cannot cross a container mount point even when the folders share a disk.)

Two ways to use the template in the meantime:

1. **Manual template add** (from memory of how Unraid stores user templates; verify on your version): copy `mediarium.xml` into the flash drive folder `config/plugins/dockerMan/templates-user/` (the `flash` share, or `/boot/config/plugins/dockerMan/templates-user/` over SSH). Then Docker tab, Add Container, and pick `mediarium` from the template list.
2. **Fully manual** (steps below) if you'd rather not deal with XML at all.

## Manual setup

1. Create a share for your media, for example `data`, containing the folders `downloads`, `movies` and `tv`.
2. **Docker tab, Add Container.**
3. **Repository:** `ghcr.io/rdborg/mediarium:latest` (not published yet while the repo is private; build and load the image locally instead, see [INSTALL.md](./INSTALL.md#building-the-image-yourself)).
4. **Port:** container port `8264` to a host port of your choice (`8264` unless something else uses it).
5. **Paths**, both **Read/Write**:
   - `/config` to e.g. `/mnt/user/appdata/mediarium`
   - `/data` to e.g. `/mnt/user/data`
6. **Variables:** `PUID=99`, `PGID=100` (Unraid's defaults; confirm under **Settings, User Utilities** if you've customized users), `TZ` to your timezone, and `DOWNLOADS_DIR=/data/downloads`, `MOVIES_DIR=/data/movies`, `TV_DIR=/data/tv`.
7. **Apply**, then open the WebUI on the port you chose.

If you would rather keep the old four-path layout (`/downloads`, `/movies`, `/tv` mapped separately) it still works, but imports copy instead of hardlink.

## Troubleshooting

- **Import falls back to "copy" instead of "hardlink":** the most common cause is separate path mappings. Use the single `/data` mapping above. Also keep downloads and library inside the same share.
- **Permission errors:** confirm `PUID=99`/`PGID=100` (or your customized values) match the owner of the host paths you mapped.
