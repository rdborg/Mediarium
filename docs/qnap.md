# Deploying on QNAP (Container Station)

> **Correction (see [INSTALL.md](./INSTALL.md#folder-layout-the-most-important-decision)):** mapping `/downloads`, `/movies` and `/tv` as separate volumes, as below, imports by copying, not hardlinking: a hardlink cannot cross a container mount point even when the folders share a volume (verified in a container). Prefer one shared folder containing `downloads`, `movies` and `tv`, mapped once as `/data` with `DOWNLOADS_DIR`/`MOVIES_DIR`/`TV_DIR` set. The steps below still work if you accept copies.

> Screenshots pending real QNAP hardware access (platform guides are meant to have screenshots) — the steps below are accurate and complete, just text-only for now.

## Prerequisites
- **Container Station 3** installed (App Center → search "Container Station"). Older Container Station 2.x has a different UI for the same underlying steps — look for "Create → Application (docker-compose)" instead of the flow described below.
- A user with a known UID/GID for `PUID`/`PGID`. SSH into the NAS and run `id <username>` (same approach as Synology/Unraid — QNAP doesn't expose this in the GUI).

## 1. Create shared folders

In **Control Panel → Privilege → Shared Folders**, create (or reuse existing) shared folders for:
- `mediarium-config` — will map to `/config`
- `mediarium-downloads` — will map to `/downloads`
- `mediarium-movies` — will map to `/movies`
- `mediarium-tv` — will map to `/tv`

**Put `mediarium-downloads`, `mediarium-movies` and `mediarium-tv` on the same storage pool/volume.** This is the same hardlinking constraint as every other platform in these docs: if they're on different volumes, imports silently fall back to copying instead of hardlinking, roughly doubling the storage you'd expect to use. Container Station doesn't warn you about this — it's a filesystem-level constraint on how hardlinks work, not something the app or QNAP's UI can detect for you ahead of time.

## 2. Create the application in Container Station

1. Open **Container Station → Applications → Create**.
2. Give it a name (e.g. `mediarium`) and paste this repo's `docker-compose.yml` into the YAML editor — Container Station 3 accepts a compose file directly, with a **Validate** button to catch syntax errors before creating anything.
3. Edit the compose file's volume lines to use your actual QNAP paths, e.g.:
   ```yaml
   volumes:
     - /share/CACHEDEV1_DATA/mediarium-config:/config
     - /share/CACHEDEV1_DATA/mediarium-downloads:/downloads
     - /share/CACHEDEV1_DATA/mediarium-movies:/movies
     - /share/CACHEDEV1_DATA/mediarium-tv:/tv
   ```
   (The exact `/share/CACHEDEV*_DATA/...` prefix depends on which storage pool you created the shared folders on in step 1 — check **Control Panel → Privilege → Shared Folders** for the real path, or browse to it from Container Station's own path picker instead of typing it by hand.)
4. Under environment variables, set `PUID`/`PGID` to the values from the prerequisites section, and `TZ` to your timezone (e.g. `Europe/Malta`).
5. Click **Create** to build and start it.

## 3. First run

Open `http://<qnap-ip>:8080` and complete the first-run wizard (create admin account → library path → indexer → Usenet server (optional if you only use torrents) → naming preset).

## Troubleshooting

- **Permission errors on import:** the owning user of your shared folders needs to match `PUID`/`PGID`. Re-check with `id <username>` over SSH.
- **Import falls back to "copy" instead of "hardlink":** confirm `mediarium-downloads` and `mediarium-movies` are on the same storage pool/volume (see step 1).
- **Can't reach the web UI:** QNAP's firewall (**Control Panel → Security → Firewall**) or the **myQNAPcloud** access control settings may block the port — add an allow rule for `8080` (or whatever `APP_PORT` you set).
- **Container Station won't accept the compose file:** use the **Validate** button in the YAML editor before creating — it's usually a leftover `${VAR}` placeholder or a volume path that doesn't exist yet.
