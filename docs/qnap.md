# Mediarium on a QNAP NAS

Mediarium runs on a QNAP NAS with **Container Station**, using the same compose file as every other Docker install. General background (folders, hardlinks, updating) is in the main [install guide](./INSTALL.md).

## What you need

- **Container Station 3** (App Center → search "Container Station" → Install).
- An Intel/AMD or 64-bit ARM QNAP (Mediarium's image is built for `amd64` and `arm64`).

## Step 1: Find your PUID and PGID

Mediarium writes files as a user and group you choose, by number.

- **Already run Radarr, Sonarr or SABnzbd in Container Station?** Open that container's details and use its `PUID` and `PGID`.
- **Otherwise, over SSH:** Control Panel → Network & File Services → Telnet / SSH → allow SSH. Connect from your computer with `ssh youradmin@<nas-ip>`, then run `id youruser` (the user that owns your media). The `uid` number is your `PUID`, the `gid` number your `PGID`.

## Step 2: Folders

Use **one shared folder** for downloads, movies and TV, so finished downloads are hardlinked (instant, no extra space). A hardlink cannot cross from one shared folder to another.

```
Shared folder "data"  (/share/data)          ->  mapped as /data
├── downloads/
├── movies/
├── tv/
└── music/                                   only if you use music
Shared folder "Container" (/share/Container)
└── mediarium/                               ->  mapped as /config
```

1. **Control Panel → Privilege → Shared Folders → Create** a shared folder named `data` (or use the one you already have), and give your user **Read/Write**.
2. In **File Station**, create a folder `mediarium` inside the `Container` shared folder (Container Station creates `Container` for you). Create `downloads`, `movies` and `tv` (and `music`) inside `data` too. **Create every folder you map before you start the application**, or Container Station stops with an error like `Bind mount failed: '/share/data/downloads' does not exist`.
3. `/share/data` is QNAP's shortcut to the shared folder, wherever it lives. If you are unsure, File Station → right-click the folder → **Properties** shows the path.

## Step 3: Create the application

1. Open **Container Station → Applications → Create**.
2. **Application name:** `mediarium`.
3. Paste this into the YAML editor and change the marked values:

```yaml
services:
  mediarium:
    image: ghcr.io/rdborg/mediarium:latest
    container_name: mediarium
    restart: unless-stopped
    ports:
      - "8264:8264"
      # Optional torrent port. Delete these two lines if you only use Usenet.
      - "58264:58264/tcp"
      - "58264:58264/udp"
    environment:
      - PUID=1000          # CHANGE: your uid from step 1
      - PGID=100           # CHANGE: your gid from step 1
      - TZ=Europe/London   # CHANGE: your timezone
      - DOWNLOADS_DIR=/data/downloads
      - MOVIES_DIR=/data/movies
      - TV_DIR=/data/tv
      # Optional folders. Take off the # to use one (see "Optional folders" below).
      # - MUSIC_DIR=/data/music
      # - EBOOKS_DIR=/data/ebooks
      # - AUDIOBOOKS_DIR=/data/audiobooks
    volumes:
      - /share/Container/mediarium:/config
      - /share/data:/data
```

   Already have a library? Point `MOVIES_DIR` and `TV_DIR` at it inside `/data` (for example `/data/media/movies`), and if other download apps use `/data/downloads`, give Mediarium its own: `DOWNLOADS_DIR=/data/downloads/mediarium`. See [Choosing your folders](./INSTALL.md#choosing-your-folders).
4. Click **Validate** to check the YAML, then **Create**. Container Station downloads the image and starts Mediarium.
5. Open `http://<nas-ip>:8264` and follow the setup wizard.

## Optional folders: music, ebooks and audiobooks

Music, ebooks and audiobooks each switch on under **Settings → Media types**.

- **Inside `data`:** create the folder (for example `music`), then take the `#` off the matching `environment` line above. Nothing else to map.
- **In another shared folder:** add a line under `volumes:`, for example `- /share/music:/music` (or `/ebooks`, `/audiobooks` on the right). No `environment` line is needed when the right side keeps those names.

To add one later, edit the application's YAML, then save it with **Update** so the container is recreated. In Mediarium, switch the type on and check the folder under **Settings → Library → Folders and file names**.

## Updating

1. Over SSH, download the newest image: `docker pull ghcr.io/rdborg/mediarium:latest`.
2. In **Container Station → Applications**, open **mediarium**'s actions menu, choose **Edit**, and save it with **Update** so the container is recreated with the new image.

If your Container Station version has no such option, delete the application (your mapped folders are kept) and create it again with the same YAML: the newest image is used. Your settings and library stay, because they live in the folders you mapped. The menu names above can differ slightly between Container Station versions.

When a new version is out, administrators see a card on the dashboard and on **Settings → System → Server and backup**. If the release is signed, the card also has an **Update now** button. See [Update now](./INSTALL.md#update-now-docker-image).

## Backing up

Back up `/share/Container/mediarium` (`app.db` and `secret.key`; keep them together) with Hybrid Backup Sync, or use **Settings → System → Server and backup** in Mediarium to download a backup zip.

## Troubleshooting

- **A folder is "not writable":** `PUID`/`PGID` do not match a user with Read/Write on the shared folder. Check the numbers again and the shared folder's permissions (Control Panel → Privilege → Shared Folders → Edit Shared Folder Permissions).
- **"Finished downloads will be copied, not moved" in the setup wizard:** downloads and library are in different shared folders or mapped as separate lines. Everything works, but each file is copied. To make it instant, use one shared folder mapped once as `/data`.
- **`Bind mount failed ... does not exist`:** a folder on the left of a `volumes` line isn't there. Create it in File Station with exactly that name, then start the application again.
- **The wizard shows `/movies`, `/tv` and `/downloads`, or "This folder isn't mapped to your server yet":** the folder lines in `environment` are missing, or you changed them after the first start. Fix them and update the application.
- **The page does not open:** check the application is running and read its log in Container Station (it should say `Mediarium listening on :8264`). QNAP's firewall (QuFirewall) may need a rule for port 8264. Another app on 8264? Change the left number, for example `"8265:8264"`.
- **Container Station rejects the YAML:** click **Validate**. The cause is usually indentation lost while pasting.

More answers: [INSTALL.md troubleshooting](./INSTALL.md#troubleshooting).
