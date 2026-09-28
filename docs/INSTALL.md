# Installing Mediarium

This is the master install guide. Pick the row in the table that matches how you want to run Mediarium, then jump to its section.

**Read this first: honest status**

- Mediarium is pre-release. No release has been tagged, so **the Docker image `ghcr.io/rdborg/mediarium` and the downloadable binaries do not exist yet** (the repository is private). The release pipeline that will publish both is written (`.github/workflows/release.yml`) but has not run yet. Until then you build the image yourself, see [Building the image yourself](#building-the-image-yourself).
- Every section below is marked with what was actually tested. **"Tested" means someone ran it in this project; "untested" means it was written from documentation or memory and may need small fixes.** If something here does not match reality on your device, please open an issue.
- Mediarium does not host or provide any content. Read [LEGAL.md](./LEGAL.md).

## Contents

1. [Which method should I use?](#which-method-should-i-use)
2. [Before you start (folders, PUID/PGID, ports)](#before-you-start)
3. [Docker Compose (any Linux or NAS)](#docker-compose-any-linux-or-nas)
4. [docker run](#docker-run)
5. [Synology DSM 7](#synology-dsm-7)
6. [Unraid](#unraid)
7. [QNAP Container Station](#qnap-container-station)
8. [TrueNAS SCALE](#truenas-scale)
9. [Portainer](#portainer)
10. [CasaOS / ZimaOS](#casaos--zimaos)
11. [Umbrel](#umbrel)
12. [OpenMediaVault](#openmediavault)
13. [Proxmox](#proxmox)
14. [Raspberry Pi](#raspberry-pi)
15. [Windows](#windows)
16. [macOS](#macos)
17. [Plain Linux binary with systemd](#plain-linux-binary-with-systemd)
18. [Building the image yourself](#building-the-image-yourself)
19. [Cross-compilation check (what was actually verified)](#cross-compilation-check)
20. [First run, updating, backup and restore](#first-run-updating-backup-and-restore)
21. [Troubleshooting](#troubleshooting)

---

## Which method should I use?

| Your setup | Method | Section | Status |
|---|---|---|---|
| Any Linux box or NAS with Docker | Docker Compose | [link](#docker-compose-any-linux-or-nas) | Compose syntax is standard; the single-mount layout was verified for hardlinks in a container (see [Folder layout](#folder-layout-the-most-important-decision)); full app not run on every platform |
| Any Docker host, one command | `docker run` | [link](#docker-run) | same as above |
| Synology DSM 7.2+ | Container Manager project | [link](#synology-dsm-7) | Untested on DSM hardware |
| Unraid | Template from `docs/unraid/mediarium.xml` | [link](#unraid) | XML parses; not loaded into a real Unraid, not in Community Apps |
| QNAP | Container Station application | [link](#qnap-container-station) | Untested on QNAP hardware |
| TrueNAS SCALE | Custom app (YAML) | [link](#truenas-scale) | Untested |
| You already use Portainer | Stack | [link](#portainer) | Untested in Portainer |
| CasaOS / ZimaOS | Custom install (compose import) | [link](#casaos--zimaos) | Untested |
| Umbrel | Not installable in one click; needs an app-store package | [link](#umbrel) | Not started |
| OpenMediaVault | Compose plugin | [link](#openmediavault) | Untested |
| Proxmox | VM with Docker (recommended), or LXC | [link](#proxmox) | Untested |
| Raspberry Pi 3/4/5 (64-bit OS) | Docker Compose or native binary | [link](#raspberry-pi) | Untested on a Pi; arm64 binary cross-compiles |
| Windows, with Docker | Docker Desktop + WSL2 | [link](#windows-docker-desktop-with-wsl2) | Untested |
| Windows, no Docker | Native `mediarium.exe` | [link](#windows-native-binary) | The exe was run on Windows 11 and served its API; the helper script was run once; installer untested |
| macOS, with Docker | Docker Desktop or OrbStack | [link](#macos-docker-desktop-or-orbstack) | Untested |
| macOS, no Docker | Native binary | [link](#macos-native-binary) | darwin builds compile; never run on a Mac |
| Any Linux, no Docker | Binary + systemd | [link](#plain-linux-binary-with-systemd) | Untested |

If you have no strong preference: **Docker Compose** is the best-supported path. It also gives you everything Mediarium needs for unpacking and repairing downloads (RAR and ZIP are unpacked by Mediarium itself, so there is no extra tool to worry about).

### RAR archives

Most Usenet releases arrive as RAR archives. Mediarium unpacks RAR (single files, `.part01.rar`-style sets and old `.rar` + `.r00`/`.r01` sets, RAR4 and RAR5) and ZIP **itself**, in pure Go, so it works the same in the Docker image and on native installs with nothing extra installed. (Earlier builds relied on the `7z` tool, and Alpine's `p7zip` package has no RAR support, which is why RAR releases failed to unpack in the Docker image; that is fixed.) Password-protected archives cannot be unpacked; such a release is marked as bad and blocklisted so the next-best release is tried. Archives are also protected against malicious paths and against expanding to an absurd size (limit: 200 GB per archive). Only the rarer `.7z` format still needs the external `7z` tool, which the Docker image includes.

---

## Before you start

### Folder layout: the most important decision

Mediarium downloads into `downloads/`, then **hardlinks** the finished file into your library (`movies/` or `tv/`). A hardlink is a second name for the same data: instant, and no extra disk space. If a hardlink is not possible it copies instead (double the space until the download is deleted), and on Windows it may fail outright (see [Windows](#windows-native-binary)).

A hardlink only works **inside one mounted filesystem**. This has a consequence that surprises almost everyone:

> **Do not map `/downloads`, `/movies` and `/tv` as three separate Docker volumes, even if all three folders sit on the same disk.** Each mapping is its own mount point inside the container, and a hardlink cannot cross a mount point.

This was checked while writing this guide: in a container, two mounts of the same volume (`-v v:/a -v v:/b`) gave `ln: /b/g: Cross-device link`, while one mount (`-v v:/data`) with `downloads/` and `movies/` inside it linked fine.

Mediarium's own dashboard check compares device numbers, which are identical for two bind mounts of the same disk, so it can say "same drive" while imports still copy. Verify for real after your first import (see [Troubleshooting](#files-are-copied-instead-of-hardlinked)).

**Recommended layout: one parent folder, mapped once as `/data`:**

```
/your/storage/media/          <- mapped into the container as /data
  downloads/                  <- incomplete/ and complete/ are created inside
  movies/
  tv/
/your/storage/mediarium/config   <- mapped as /config (small, separate is fine)
```

The container is told where these live with three environment variables, so the first-run wizard already shows the right folders:

```
DOWNLOADS_DIR=/data/downloads
MOVIES_DIR=/data/movies
TV_DIR=/data/tv
```

Notes:

- The repo's own `docker-compose.yml` and the older platform guides use four separate mounts (`/config`, `/downloads`, `/movies`, `/tv`). That still **works**, it just copies instead of hardlinking. The compose files in `packaging/` use the single `/data` layout.
- When you use one `/data` mount, the container log may still print `NOTE: /downloads is not mapped ...` and the same for `/movies` and `/tv`. In this layout that message is harmless: the entrypoint only checks the three default folder names, not the `*_DIR` variables. It creates those empty folders inside the container and they are never used.
- Already have a library elsewhere? Put `downloads/` next to it under the same parent, or map your existing parent as `/data`.
- The filesystem must support hardlinks: ext4, XFS, Btrfs, ZFS (within one dataset), APFS, NTFS all do. exFAT and FAT do not. On TrueNAS, each dataset is its own filesystem, so use one dataset. On Synology with Btrfs, each shared folder is a separate subvolume, so use one shared folder with sub-folders (reported widely; not tested on DSM here).
- **Never** put `/config` on a network share (NFS/SMB): it holds a SQLite database.

### PUID and PGID

Files Mediarium writes are owned by the numeric user and group in `PUID` and `PGID` (defaults 1000 and 1000). Set them to the owner of your media folders:

- On any Linux over SSH: `id yourusername` prints `uid=1000(you) gid=1000(you) ...`. The `uid` is your PUID, the `gid` is your PGID. `id -u` and `id -g` print them alone.
- Unraid's usual values are `99` and `100`. Synology and QNAP values are whatever `id` says for the user that owns your shared folders.
- The container only changes ownership of `/config`. It **never** changes ownership or permissions of your media folders; if it cannot write to one, the container log says `WARNING: /movies is not writable by uid ...` and the dashboard shows it.
- PUID/PGID/TZ only exist in the Docker image. A native install runs as whichever user starts it.

### Ports

| Port | Used for | Notes |
|---|---|---|
| `8080` (TCP) | Web interface and API | Change the **left** side of `8080:8080` to use another host port. Natively, set `APP_PORT`. |
| Torrent listen port | BitTorrent peers | Setting: Settings > Downloads > Torrent settings, `0` means the operating system picks a random port each start. Downloading works without incoming connections. If you want incoming peers in Docker you would need to set a fixed port there and also publish it (TCP and UDP) in your compose file. Not tested. |

Mediarium serves plain HTTP. Do not expose port 8080 directly to the internet. Put a reverse proxy with TLS in front, or use a VPN into your home network.

---

## Docker Compose (any Linux or NAS)

**Status:** compose syntax is standard; not run on every host.

1. Install Docker and the Compose plugin from [docs.docker.com/engine/install](https://docs.docker.com/engine/install/). Check with `docker compose version`.
2. Make the folders and note your PUID/PGID (`id youruser`):
   ```bash
   sudo mkdir -p /srv/media/downloads /srv/media/movies /srv/media/tv /srv/mediarium/config
   sudo chown -R 1000:1000 /srv/media /srv/mediarium   # use YOUR uid:gid
   ```
3. Create `docker-compose.yml` (this is `packaging/portainer/docker-compose.yml` with the defaults filled in):
   ```yaml
   services:
     mediarium:
       image: ghcr.io/rdborg/mediarium:latest   # not published yet, see "Building the image yourself"
       container_name: mediarium
       restart: unless-stopped
       ports:
         - "8080:8080"
       environment:
         PUID: "1000"
         PGID: "1000"
         TZ: "Etc/UTC"           # for example Europe/Malta
         DOWNLOADS_DIR: /data/downloads
         MOVIES_DIR: /data/movies
         TV_DIR: /data/tv
       volumes:
         - /srv/mediarium/config:/config
         - /srv/media:/data
   ```
4. Start it: `docker compose up -d`. Check the log: `docker compose logs -f`.
5. Open `http://<host-ip>:8080` and follow the [first-run wizard](#first-run-updating-backup-and-restore).

More Linux notes (including a systemd unit that wraps compose) are in [linux.md](./linux.md).

## docker run

**Status:** same as Compose.

```bash
docker run -d \
  --name mediarium \
  --restart unless-stopped \
  -p 8080:8080 \
  -e PUID=1000 -e PGID=1000 -e TZ=Etc/UTC \
  -e DOWNLOADS_DIR=/data/downloads -e MOVIES_DIR=/data/movies -e TV_DIR=/data/tv \
  -v /srv/mediarium/config:/config \
  -v /srv/media:/data \
  ghcr.io/rdborg/mediarium:latest
```

Use absolute host paths. (The README's `./config` relative form also works with a recent Docker, but absolute paths are clearer.)

---

## Synology DSM 7

**Status:** untested on DSM hardware. Steps follow the existing [synology.md](./synology.md), adjusted for the single-mount layout.

Needs **Container Manager** (Package Center). Container Manager replaced the older "Docker" package in DSM 7.2; on DSM 7.0/7.1 the old package has no "Project" feature, so upgrade DSM or use another method.

1. **Find your PUID/PGID.** Turn on SSH: Control Panel > Terminal & SNMP > Enable SSH service. Then from your computer: `ssh youradminuser@nas-ip` and run `id youruser`. Use the `uid` and `gid` numbers. Turn SSH off again afterwards if you do not need it.
2. **Create ONE shared folder for media**, for example `data` (Control Panel > Shared Folder > Create), and inside it (File Station) create folders `downloads`, `movies` and `tv`. Give the user whose uid you will use **Read/Write** permission on the shared folder (Shared Folder > Edit > Permissions). Create another small shared folder, for example `docker`, and a sub-folder `mediarium-config` inside it. Note: the existing synology.md suggests separate shared folders on one volume; because hardlinks do not cross Btrfs shared folders (see the caveat in [Folder layout](#folder-layout-the-most-important-decision)), one shared folder is the safer choice.
3. **Create the project.** Container Manager > Project > Create. Name it `mediarium`, choose a path for the project folder (for example `/docker/mediarium`), pick "Create docker-compose.yml" and paste:
   ```yaml
   services:
     mediarium:
       image: ghcr.io/rdborg/mediarium:latest
       container_name: mediarium
       restart: unless-stopped
       ports:
         - "8080:8080"
       environment:
         PUID: "1026"          # from step 1
         PGID: "100"           # from step 1
         TZ: "Europe/Malta"
         DOWNLOADS_DIR: /data/downloads
         MOVIES_DIR: /data/movies
         TV_DIR: /data/tv
       volumes:
         - /volume1/docker/mediarium-config:/config
         - /volume1/data:/data
   ```
   `/volume1` is the first storage volume; use the real volume name if yours differs. Replace the `1026`/`100` example numbers with yours.
4. Build/start the project. Open `http://<nas-ip>:8080`.
5. If you cannot reach it: Control Panel > Security > Firewall may block the port.

Backups: include `/volume1/docker/mediarium-config` in Hyper Backup or copy it.

## Unraid

**Status:** the template XML is valid XML and follows Unraid's template format; it has not been loaded into a real Unraid server and is not in Community Applications (needs a public repo, a published image and a hosted icon; the current CA submission process was not verified).

Template: [`docs/unraid/mediarium.xml`](./unraid/mediarium.xml). It uses **two paths**: `/config` and one `/data` share, plus `PUID=99`, `PGID=100`, `TZ` and the three `*_DIR` variables (under "Show more settings").

Manual way, from memory of how Unraid stores user templates (verify on your version):

1. Create a share called `data` with folders `downloads`, `movies`, `tv` inside it, and use the same share for your media server. Keep them in one share so hardlinks work.
2. Copy `mediarium.xml` to the flash drive folder `config/plugins/dockerMan/templates-user/` (visible as the `flash` network share, or `/boot/config/plugins/dockerMan/templates-user/` over SSH).
3. Docker tab > Add Container > choose `mediarium` from the template list > check paths > Apply.
4. Open the WebUI from the container's menu.

Or skip the template and fill the form by hand as described in [unraid.md](./unraid.md).

Unraid notes: `PUID=99`/`PGID=100` are Unraid's default `nobody`/`users`. Keep `/config` on a cache or appdata location (SSD), not on a spinning array disk, for responsiveness.

## QNAP Container Station

**Status:** untested on QNAP hardware. Follow [qnap.md](./qnap.md) with these differences:

- Create **one** shared folder for media (with `downloads`, `movies`, `tv` inside) instead of three, and map it once as `/data` with the three `*_DIR` variables from the compose above.
- Container Station 3: Applications > Create, paste the compose, Validate, Create.
- Find PUID/PGID by SSH: `id youruser`.

## TrueNAS SCALE

**Status:** untested. Full notes and a paste-ready YAML: [`packaging/truenas/README.md`](../packaging/truenas/README.md).

Key points: newer SCALE versions (Docker-based apps, 24.10 and later) can install a custom app from pasted YAML; older Kubernetes-based versions cannot use this. Use **one dataset** for downloads/movies/tv (each dataset is its own filesystem, so hardlinks do not cross datasets) and set that dataset's owner to the uid/gid you use as PUID/PGID. There is no Mediarium entry in the TrueNAS app catalog.

## Portainer

**Status:** untested in Portainer; it is ordinary compose.

1. Portainer > Stacks > Add stack.
2. Name it `mediarium`, choose **Web editor**, paste [`packaging/portainer/docker-compose.yml`](../packaging/portainer/docker-compose.yml).
3. Scroll to **Environment variables** and set `PUID`, `PGID`, `TZ`, `DATA_DIR` (host folder containing `downloads`, `movies`, `tv`) and `CONFIG_DIR_HOST`. The file has sensible defaults if you skip them.
4. Deploy the stack. Update later by opening the stack's Editor, turning on the re-pull-image-and-redeploy option and choosing Update the stack (option name from memory; it may differ in your Portainer version).

If the image is not published yet you will need to build it on the Docker host first; Portainer's Images > Build can do it from a Dockerfile, or use the command line ([Building the image yourself](#building-the-image-yourself)).

## CasaOS / ZimaOS

**Status:** untested. File: [`packaging/casaos/docker-compose.yml`](../packaging/casaos/docker-compose.yml). It has ordinary compose plus CasaOS `x-casaos` display metadata written from memory; if the importer rejects it, delete the two `x-casaos` blocks.

1. In the CasaOS dashboard open the App Store and use the custom-install option to import a Docker Compose file (exact button label not verified; it is in the top corner of the App Store page in the versions I know of). ZimaOS uses the same approach.
2. Paste the file. It maps `/DATA/Media` as `/data` and expects `downloads`, `movies` and `tv` folders inside it. If you already have `/DATA/Media/Movies` and `/DATA/Media/TV Shows`, edit the three `*_DIR` lines to match.
3. Check `PUID`/`PGID` against `id` in the CasaOS terminal, then install.

## Umbrel

**Status:** not started. Honest answer: **yes, a one-click Umbrel install needs an app-store package, and none exists.**

What Umbrel needs (from memory of Umbrel's community app store format; verify against Umbrel's current developer docs before building):

- A git repository acting as a **community app store**: an `umbrel-app-store.yml` at the root and one folder per app.
- In the app folder: `umbrel-app.yml` (manifest: id, name, tagline, category, version, port, description, developer, website, repo, support, and so on) and a `docker-compose.yml` that includes Umbrel's `app_proxy` service pointing at Mediarium's port `8080`, with data under Umbrel's app data folder and shared media under Umbrel's storage folder.
- The image must be published and reachable, ideally pinned by digest.
- Umbrel puts its own login in front of apps by default; Mediarium has its own login too, so you would decide whether to disable one.

Effort: small (a day or less) once an image is published, plus testing on a real Umbrel. Until then the only route is running Docker by hand over SSH on the Umbrel machine, which is unsupported and untested here.

## OpenMediaVault

**Status:** untested.

1. Install Docker and the Compose plugin: OpenMediaVault's Docker support comes from the **omv-extras** plugin repository (a community add-on, separate from OMV itself), with the **compose** plugin providing a Services > Compose section. Follow omv-extras' own instructions for your OMV version.
2. Create **one shared folder** for media (for example `media`, with `downloads`, `movies`, `tv` inside) and one for app config. Note their absolute paths (Storage > Shared Folders shows the device; the path is `/srv/dev-disk-by-uuid-.../media`).
3. Find the PUID/PGID: OMV's Users page lists the user; `id user` over SSH prints the numbers. Give that user read/write on the shared folder (shared folder permissions or ACL).
4. In Services > Compose, add a file using the [Docker Compose](#docker-compose-any-linux-or-nas) YAML above with your real paths, then bring it up.

## Proxmox

**Status:** untested. Two routes.

**Recommended: a small VM (Debian or Ubuntu) with Docker inside**, then follow [Docker Compose](#docker-compose-any-linux-or-nas). Proxmox's own documentation recommends against running Docker inside an LXC container; a VM avoids the permission and nesting problems below. Pass your media through as a disk, or mount an NFS/SMB share **for media only** (never for `/config`) on the VM.

**LXC with Docker (works for many people, more fiddly):**

1. Create a Debian 12 container. Under Options > Features enable **Nesting** (and, if Docker complains, **keyctl**).
2. Install Docker inside it per the Docker docs.
3. Bind-mount your media into the container from the Proxmox host, for example `pct set 101 -mp0 /tank/media,mp=/data`. Use one mount for the whole media tree.
4. **User mapping:** in an unprivileged container, uid 1000 inside is uid 101000 on the host. Files owned by 1000 on the host will look owned by "nobody" inside. Either `chown -R 101000:101000 /tank/media` on the host (for PUID/PGID 1000) or set up an `idmap` for the container. This is the usual source of "permission denied" here.

**LXC without Docker (often simpler):** run the native binary with the [systemd unit](#plain-linux-binary-with-systemd) inside the container. No nesting is needed. The same bind-mount and uid-mapping notes apply.

## Raspberry Pi

**Status:** untested on a Pi. The `linux/arm64` binary cross-compiles cleanly (see [the check](#cross-compilation-check)) and the Docker image is built for `linux/arm64`.

- You need a **64-bit** OS. Check with `uname -m`: it must print `aarch64`. If it prints `armv7l` you are on 32-bit Raspberry Pi OS; neither the image nor the binaries are built for that. Pi 3, 4 and 5 can run 64-bit; Pi Zero, 1 and 2 (32-bit only) cannot.
- Do not keep `/config` (the database) on a cheap SD card if you can avoid it; use a USB SSD, and keep the media there too.
- The USB drive should be ext4 (or another hardlink-capable filesystem). exFAT/FAT cannot hardlink.
- Then follow [Docker Compose](#docker-compose-any-linux-or-nas) (Docker install docs cover Raspberry Pi OS and Debian), or the [native binary](#plain-linux-binary-with-systemd) with `mediarium_..._linux_arm64.tar.gz` and `apt install p7zip-full par2`.
- Expect a small Pi to be slow at unpacking large releases and PAR2 repair.

---

## Windows

Two routes: Docker Desktop (Linux container) or the native `mediarium.exe`.

### Windows: Docker Desktop with WSL2

**Status:** untested end to end (Docker is installed on the machine this guide was written on, but Mediarium's image was not run there).

1. Install Docker Desktop with the WSL2 backend (docs.docker.com/desktop/setup/install/windows-install/).
2. Prefer keeping `/config` and `/data` on the **Linux side** for speed and correct file behaviour: use Docker named volumes, or folders inside a WSL distro, rather than bind-mounting `C:\...` paths. How well hardlinks behave on bind-mounted Windows drives was not verified. For a quick trial, this works with named volumes:
   ```powershell
   docker volume create mediarium-config
   docker volume create mediarium-data
   docker run -d --name mediarium --restart unless-stopped -p 8080:8080 `
     -e PUID=1000 -e PGID=1000 -e TZ=Etc/UTC `
     -e DOWNLOADS_DIR=/data/downloads -e MOVIES_DIR=/data/movies -e TV_DIR=/data/tv `
     -v mediarium-config:/config -v mediarium-data:/data `
     ghcr.io/rdborg/mediarium:latest
   ```
   Then point Plex/Jellyfin/Emby at the same data through your usual means. Volumes are hard to reach from Windows Explorer; if you need the files visible on Windows, the native route below is simpler.
3. Open `http://localhost:8080`.

### Windows: native binary

**Status:** partly tested. A CGO-free `windows/amd64` build was run on Windows 11: it started, created `app.db`, `secret.key` and the download/library folders, and answered `/api/version` and `/api/onboarding/status`. The helper script `run-mediarium.ps1` was run once (see the script header for exactly what it exercised). The Inno Setup installer was **never compiled**.

**What is bundled and what is not**

| Needed for | Bundled in the exe? | What to install |
|---|---|---|
| The app, web UI, database (SQLite), Usenet client, torrent client, VPN | **Yes**, all inside `mediarium.exe` | nothing |
| Unpacking RAR and ZIP downloads | **Yes**, built in | nothing |
| Unpacking `.7z` downloads (rare) | **No.** Mediarium runs a program named `7z` | 7-Zip (optional) |
| PAR2 verify and repair | **No.** Mediarium runs a program named `par2` | par2cmdline |

Mediarium finds them by the names `7z` and `par2` on your `PATH` (`internal/organizer/archive.go`, `par2.go`). If either is missing, the app still runs: without `7z` only `.7z` archives fail to unpack (RAR and ZIP are unaffected, and the dashboard shows an informational note); without `par2` a download is only accepted if no articles were missing, otherwise it fails.

**Step by step**

1. Download and unzip `mediarium_<version>_windows_amd64.zip` somewhere permanent, for example `C:\Mediarium\app`. (No release exists yet: build it, see [Building](#building-the-image-yourself), or `go build ./cmd/app` after building the frontend.)
2. **Install 7-Zip:** in PowerShell, `winget install 7zip.7zip`. **Important, checked here:** the 7-Zip installer puts `7z.exe` in `C:\Program Files\7-Zip` but does **not** add that folder to `PATH`, so `7z` is not found until you do. Add it: Start menu > "Edit environment variables for your account" > Path > Edit > New > `C:\Program Files\7-Zip`. Open a new PowerShell and check `7z` prints a version banner. (The 7-Zip that winget installs here lists Rar and Rar5 support.)
3. **Install par2:** no winget package for `par2cmdline` was found when this was written (a winget search for "par2" returned only MultiPar and another unrelated tool, which are not drop-in). Download a Windows build of **par2cmdline** (the `par2cmdline-turbo` fork at github.com/animetosho/par2cmdline-turbo has releases; I could not read its asset list to confirm a Windows build, so check the releases page yourself), unzip it, and put the folder containing `par2.exe` on your `PATH` the same way. Check with `par2 --version`. It must be a command-line `par2.exe` compatible with `par2 verify` / `par2 repair`; the GUI tool MultiPar is not.
4. **Run it.** From the unzipped folder:
   ```powershell
   powershell -ExecutionPolicy Bypass -File .\run-mediarium.ps1
   ```
   The script downloads nothing. It looks for `7z` and `par2`, prints what to do if one is missing, creates `%USERPROFILE%\Mediarium\{config,downloads,movies,tv}`, sets the environment variables Mediarium reads, and starts the exe. Choose other locations with `-DataRoot D:\Media`, another port with `-Port 8181`.
5. Open `http://localhost:8080`. Windows Firewall may ask whether to allow it on your network; that only matters if you want to reach it from other devices.

**Windows-specific things to know**

- Without the environment variables, the defaults are `/config`, `/downloads`, `/movies` and `/tv`, which on Windows mean folders at the root of the current drive (for example `C:\config`). Use the script (or set `CONFIG_DIR`, `DOWNLOADS_DIR`, `MOVIES_DIR`, `TV_DIR`, `APP_PORT` yourself).
- **Keep downloads, movies and tv on the same drive (NTFS).** Checked here on Windows with Go: a hardlink on the same drive works; across drives it fails with "The system cannot move the file to a different disk drive", and Mediarium's fallback-to-copy only recognises the Linux error code (`internal/organizer/import.go` `isCrossDeviceErr` checks `syscall.EXDEV`; the test showed that check is false for the Windows error). So on Windows, **a cross-drive import will probably fail instead of copying**. Not tested through the full import path.
- The Windows build has no "same drive" warning (`internal/organizer/samefs_windows.go` reports "unsupported").
- `mediarium.exe` is not a Windows service, so closing the window stops it. Options: a Scheduled Task at logon (built into Windows; the installer sketch can create one), or wrap it with **NSSM** (`winget install NSSM.NSSM`, confirmed to exist in winget). Example NSSM commands are at the bottom of `packaging/windows/mediarium.iss`. If you run it as a service, `7z.exe` and `par2.exe` must be on the **system** `PATH` and the service account must be able to write your folders.
- `mediarium reset-password <user>` hides typing using `stty`, which Windows does not have; it falls back to showing the password as you type.
- The installer sketch: [`packaging/windows/mediarium.iss`](../packaging/windows/mediarium.iss) (UNTESTED). Compile with Inno Setup (`winget install JRSoftware.InnoSetup`, exists in winget).

## macOS

### macOS: Docker Desktop or OrbStack

**Status:** untested. Docker Desktop for Mac and OrbStack both run Linux containers; on Apple silicon the `linux/arm64` image runs natively. Use the [Docker Compose](#docker-compose-any-linux-or-nas) YAML with host paths like `/Users/you/Media`. Use the single `/data` mount. Bind mounts from macOS go through a virtualization layer; hardlink behaviour and speed there were not verified. PUID/PGID mostly do not matter for files on macOS folders, keep 1000.

### macOS: native binary

**Status:** the `darwin/amd64` (Intel) and `darwin/arm64` (Apple silicon) builds compile with CGO disabled (see [the check](#cross-compilation-check)); they were **never run on a Mac**. `install.sh` and the launchd plist are UNTESTED.

1. Download and extract `mediarium_<version>_darwin_arm64.tar.gz` (Apple silicon) or `..._darwin_amd64.tar.gz` (Intel). (No release exists yet.)
2. Install helpers with Homebrew:
   ```bash
   brew install p7zip par2
   ```
   Caveats not verified here: Homebrew has been moving away from the `p7zip` formula in favour of `sevenzip`, whose command is named `7zz`. Mediarium looks for a program named exactly `7z`. If `7z` is not found after installing, either `brew install sevenzip` and add a link (`ln -s "$(command -v 7zz)" /opt/homebrew/bin/7z`, `/usr/local/bin` on Intel), or use whichever formula Homebrew currently provides that installs `7z`. The formula for `par2` is named `par2`. Check `7z` and `par2 --version` both print something.
3. Run it once by hand to try it:
   ```bash
   CONFIG_DIR=$HOME/Mediarium/config DOWNLOADS_DIR=$HOME/Mediarium/downloads \
   MOVIES_DIR=$HOME/Mediarium/movies TV_DIR=$HOME/Mediarium/tv ./mediarium
   ```
   The first launch of a downloaded, unsigned program may be blocked by Gatekeeper ("cannot verify the developer"). Approve it in System Settings > Privacy & Security, or run `xattr -d com.apple.quarantine ./mediarium`.
4. To start it at login, run `sh install.sh` from the extracted folder. It copies the binary to `~/.local/share/mediarium/`, creates `~/Mediarium/...`, installs a per-user launchd agent (`io.github.ryanborg.mediarium`) with a `PATH` that includes Homebrew, and starts it. Logs: `~/Library/Logs/Mediarium/mediarium.log`. Remove with `sh install.sh uninstall`. Files: [`packaging/macos/`](../packaging/macos/).

## Plain Linux binary with systemd

**Status:** untested (no systemd host used). `linux/amd64` and `linux/arm64` builds compile with CGO disabled.

1. Install helpers: Debian/Ubuntu `sudo apt install p7zip-full par2`; Fedora `sudo dnf install p7zip p7zip-plugins par2cmdline`; Arch `sudo pacman -S p7zip par2cmdline`. `p7zip` is optional: it is only used for `.7z` archives, because Mediarium unpacks RAR and ZIP itself.
2. Extract `mediarium_<version>_linux_amd64.tar.gz` (or `arm64`). It contains `mediarium`, `mediarium.service`, `mediarium.env.example`.
3. Install (as root):
   ```bash
   useradd --system --home-dir /var/lib/mediarium --create-home --shell /usr/sbin/nologin mediarium
   install -m 0755 mediarium /usr/local/bin/mediarium
   install -d -o mediarium -g mediarium /var/lib/mediarium/config
   install -d /etc/mediarium
   install -m 0640 -g mediarium mediarium.env.example /etc/mediarium/mediarium.env
   install -m 0644 mediarium.service /etc/systemd/system/mediarium.service
   ```
4. Edit `/etc/mediarium/mediarium.env` (media folders on one filesystem) and make sure the `mediarium` user can write them (`chown`, or run the service as your own user by changing `User=`).
5. `systemctl daemon-reload && systemctl enable --now mediarium`, then `journalctl -u mediarium -f`.

Files: [`packaging/systemd/`](../packaging/systemd/). Native runs ignore `PUID`, `PGID` and `TZ`; the process runs as the systemd `User=` and uses the system timezone.

---

## Building the image yourself

Until the image is published (private repo):

```bash
git clone <your clone URL> mediarium && cd mediarium
docker build -t mediarium:local .
```

Then use `image: mediarium:local` in the compose file instead of the `ghcr.io/...` name. The Dockerfile builds the frontend and the Go binary itself (Node 22 and Go are used inside the build, you do not need them on the host). Optional build arguments `TMDB_API_KEY`, `OPENSUBTITLES_API_KEY` and `TRAKT_CLIENT_ID` bake in app-wide keys; without them the app asks you for your own in the wizard. On a NAS you may prefer to build on your PC and move the image: `docker save mediarium:local -o mediarium.tar` then `docker load -i mediarium.tar` on the NAS.

Native binary from source:

```bash
cd web && npm ci && npm run build && cd ..     # writes web/dist, embedded by web/embed.go
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=v0.0.0-local" -o mediarium ./cmd/app
```

Skip the npm step and the binary embeds only the tiny placeholder page that is checked into `web/dist/index.html`. The API works, but there is no real web interface.

## Cross-compilation check

Checked on Windows 11 with Go 1.27.0 (repo `go.mod` says `go 1.26.0`), building the committed source (`git archive HEAD`, with the placeholder frontend, so this tests Go compilation, not the frontend) with `CGO_ENABLED=0 go build -trimpath ./cmd/app`:

| Target | Result |
|---|---|
| linux/amd64 | builds (about 44 MB) |
| linux/arm64 | builds (about 41 MB) |
| windows/amd64 | builds (about 44 MB), and **runs** on Windows 11 |
| windows/arm64 | builds (not needed by the release workflow) |
| darwin/amd64 | builds (about 44 MB), not run |
| darwin/arm64 | builds (about 42 MB), not run |

**Conclusion: nothing in the dependencies (the torrent library, the pure-Go SQLite driver, WireGuard) blocks cross-compiling with CGO disabled.** The repository already has separate `_windows.go` files for the two OS-specific pieces (`internal/organizer/samefs_windows.go` and `internal/fsinfo/usage_windows.go`).

One caveat: while checking the live working tree (not the committed source), other people's half-finished edits in `internal/api` briefly broke the build with syntax errors. Those were unrelated to cross-compilation and are why the check above uses the committed source.

The release workflow uses exactly these commands. It has not been run on GitHub.

---

## First run, updating, backup and restore

**First run.** Open `http://<host>:8080`. There is no default password: the wizard creates the admin account (the first account is the admin). It then checks your folders live (exists, writable, really mapped, free space), and asks about indexers, your Usenet provider (optional if you only use torrents) and naming. Everything is changeable later in Settings. Forgotten password: `docker exec -it -u 1000:1000 mediarium /app/app reset-password <username>` (use your PUID:PGID; running as root could leave root-owned database files). Natively: `mediarium reset-password <username>` with the same environment variables set.

**Updating (Docker).** Releases are meant to be version-pinned. Change the tag in your compose file (for example `:v0.2.0`), then `docker compose pull && docker compose up -d`. Portainer: see the Portainer section. Synology: Container Manager > Project > Action > Build. Nothing updates itself. The database schema migrates automatically on start. **Back up `/config` before updating**, and note there is no downgrade path documented.

**Updating (native).** Stop it, replace the binary, start it.

**Backup.** Everything that is not a media file lives in `/config`: `app.db` (settings, library, history, accounts) and `secret.key`. **Keep them together**: `secret.key` decrypts the API keys, Usenet passwords and VPN keys stored in `app.db`; with `app.db` alone those fields are lost. Stop the container (or at least quiet it) and copy the folder:

```bash
docker compose stop mediarium
cp -a /srv/mediarium/config /srv/backups/mediarium-config-$(date +%F)
docker compose start mediarium
```

**Built-in backup and restore (administrators only).** Mediarium can also make the backup for you while it runs: it downloads a zip named `mediarium-backup-YYYYMMDD-HHMMSS.zip` containing a consistent snapshot of `app.db` (taken with SQLite `VACUUM INTO`, so it is safe while downloads are active), `secret.key` and a small `manifest.json`. The API is `GET /api/system/backup`. **The zip contains your encryption key and therefore every stored credential: keep it as private as a password.** To restore, upload that zip (`POST /api/system/restore`, field `file`, up to 1 GB). Mediarium checks it thoroughly (only those three files, a healthy database from this or an older version, a valid key), stages it, and restarts itself; on the next start it swaps the files in and moves your previous `app.db`, `secret.key` and any WAL files into a `before-restore-<timestamp>` folder inside `/config`, which it never deletes (remove it yourself once you are happy). If the staged files turn out to be unusable, your current data is left untouched and they are moved to `restore-failed-<timestamp>`. The restart relies on the container's restart policy (`restart: unless-stopped` in the shipped compose file); if yours has none, start Mediarium again yourself and the restore is applied then. After a restore you may need to sign in again, with the accounts from the backup.

The manual method still works. **Manual restore:** stop, put the folder back, start.

---

## Troubleshooting

### Permission denied / folder not writable

- Container log says `WARNING: /data ... not writable` or the dashboard flags a folder: `PUID`/`PGID` do not match the owner of the host folder. Find the owner with `ls -ln /path` (numbers) or `id user`, set `PUID`/`PGID`, `docker compose up -d`. Mediarium never chowns your media folders; you fix them on the host.
- On NAS systems, also grant that user Read/Write on the shared folder in the NAS's own permission screen.
- Proxmox LXC: see the uid mapping note ([Proxmox](#proxmox)).
- Windows service: the service account cannot see your mapped drives; use folders it can write.

### Files are copied instead of hardlinked

Symptoms: disk use doubles after each import.

1. Are you using **separate mounts** for downloads and library? That is the usual cause. Use one `/data` mount ([Folder layout](#folder-layout-the-most-important-decision)).
2. Verify on the host after an import: `stat -c '%h %n' /srv/media/movies/*/*.mkv`. A link count (`%h`) of `2` means it is hardlinked; `1` means it is a separate copy (the download was cleaned up, or it copied). Compare inode numbers with `ls -li` on the download and the library file if both still exist.
3. Different filesystems (two disks, two ZFS datasets, two Btrfs subvolumes or Synology shared folders, or a network share) cannot hardlink. Move both under one filesystem.
4. The dashboard's "same drive" indicator can say "OK" for two bind mounts of one disk, as explained above. Trust the link count.

### Unmapped volumes / settings reset after restart

- If the wizard or the log says a folder is **not mapped**, or your onboarding wizard appears again after every restart, that folder or `/config` is not mapped to a host folder, so it lives inside the container and is lost when the container is recreated. Add the volume line and recreate the container.
- With the single `/data` layout, ignore log lines about `/downloads`, `/movies`, `/tv` being unmapped (see the note above), but make sure `/data` and `/config` are real host folders.
- `docker compose down` then `up -d` keeps mapped folders; `docker compose down -v` deletes **named volumes**.

### Cannot open the web page

- Check the container is running: `docker ps`, `docker logs mediarium`.
- Host firewall or NAS firewall blocking `8080`; port already used by another app (change the left side of `8080:8080`).
- Native install: check `APP_PORT` and that nothing else listens there.

### Search finds nothing / downloads never start

- Add at least one indexer that is enabled in Settings, and press its Test button. Add a Usenet provider (Settings > Downloads) and test it, unless you only use torrents. See the [README troubleshooting](../README.md#troubleshooting) for more.

### Downloads finish but nothing is imported, or "unpack" / "repair" fails

- Usenet releases are usually RAR + PAR2. RAR and ZIP are unpacked by Mediarium itself; `par2` must exist for repairs (`docker exec mediarium sh -c 'command -v par2'`; natively `par2 --version`), and `7z` only matters for `.7z` archives.
- A release that is password protected, or whose archive is damaged, fails at the unpack step and is blocklisted automatically (see [RAR archives](#rar-archives)); if you see "password protected" in the failure message, that release simply cannot be used.

### Timezone or clock is wrong

Set `TZ` (for example `Europe/Malta`) in the environment and recreate the container.

### Reporting a problem

Open an issue with: your platform, how you installed it, the container log (`docker logs mediarium`), and never paste API keys, Usenet passwords or VPN keys.

---

See also: [synology.md](./synology.md), [unraid.md](./unraid.md), [qnap.md](./qnap.md), [linux.md](./linux.md), [FEATURES.md](./FEATURES.md) (what is and is not built yet), [LEGAL.md](./LEGAL.md).
