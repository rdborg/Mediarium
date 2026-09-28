# Deploying on Synology DSM

> **Correction (see [INSTALL.md](./INSTALL.md#folder-layout-the-most-important-decision)):** the four-separate-mounts layout below imports by copying, not hardlinking. Two separate Docker mounts cannot hardlink to each other even on the same volume (verified in a container), and on Btrfs each Synology shared folder is a separate subvolume, which cannot hardlink across either (widely reported, not tested on DSM here). Prefer ONE shared folder with `downloads`, `movies` and `tv` inside, mapped once as `/data` with `DOWNLOADS_DIR`/`MOVIES_DIR`/`TV_DIR` set; the DSM section of INSTALL.md shows this. The steps below still work if you accept copies.

> Screenshots pending real DSM hardware access (they are planned) — the steps below are accurate and complete, just text-only for now.

## Prerequisites
- DSM 7.x with **Container Manager** installed (Package Center → search "Container Manager"). Older DSM versions use the "Docker" package instead — the steps are the same, just under a different app name.
- A user with a known UID/GID for `PUID`/`PGID`. Find yours: **Control Panel → User & Group → your user → Edit**, or SSH in and run `id <username>`.

## 1. Create shared folders

In **Control Panel → Shared Folder**, create (or reuse existing) shared folders for:
- `mediarium-config` — will map to `/config`
- `mediarium-downloads` — will map to `/downloads`
- `mediarium-movies` — will map to `/movies`
- `mediarium-tv` — will map to `/tv`

**Put `mediarium-downloads`, `mediarium-movies` and `mediarium-tv` on the same DSM volume** (e.g. both on `Volume 1`, not one on `Volume 1` and the other on `Volume 2`). This is the single most common Synology misconfiguration: if they're on different volumes, the app silently falls back to copying files instead of hardlinking them, and you'll use roughly double the storage you expect. Container Manager doesn't warn you about this — it's a DSM-level constraint on how hardlinks work.

## 2. Create the project in Container Manager

1. Open **Container Manager → Project → Create**.
2. Name it `mediarium`, and point "Path" at a folder containing a copy of this repo's `docker-compose.yml` (or paste its contents directly into the "docker-compose.yml" editor Container Manager provides).
3. Edit the compose file's volume lines to use your actual DSM paths, e.g.:
   ```yaml
   volumes:
     - /volume1/mediarium-config:/config
     - /volume1/mediarium-downloads:/downloads
     - /volume1/mediarium-movies:/movies
     - /volume1/mediarium-tv:/tv
   ```
4. Under environment variables, set `PUID`/`PGID` to the values from step 1's prerequisites, and `TZ` to your timezone (e.g. `Europe/Malta`).
5. Build and start the project.

## 3. First run

Open `http://<synology-ip>:8080` and complete the first-run wizard (create admin account → library path → indexer → Usenet server (optional if you only use torrents) → naming preset).

## Troubleshooting

- **Permission errors on import:** the owning user of your shared folders needs to match `PUID`/`PGID`. Re-check with `id <username>` over SSH.
- **Import falls back to "copy" instead of "hardlink":** confirm `mediarium-downloads` and `mediarium-movies` are on the same Synology volume (see step 1).
- **Can't reach the web UI:** Synology's firewall (Control Panel → Security → Firewall) may block the port — add an allow rule for `8080` (or whatever `APP_PORT` you set), or reach it through Synology's reverse proxy instead.
