# Mediarium on Unraid

Mediarium runs on Unraid as a normal Docker container. A one-click entry in **Community Applications** is coming soon; until then you add the container yourself with Unraid's **Add Container** form. It takes about five minutes.

General background (folders, hardlinks, updating) is in the main [install guide](./INSTALL.md).

## Before you start

- **Docker is enabled:** Settings → Docker → Enable Docker: Yes.
- **One share for your media.** Mediarium needs your downloads, movies and TV inside **one** share, so finished downloads can be hardlinked into the library (instant, no extra space). The common layout, also used by many Radarr and Sonarr setups, is a share named `data`:

```
/mnt/user/data                 ->  mapped as /data
├── downloads/
│   └── mediarium/             Mediarium's own downloads (created for you)
└── media/
    ├── movies/
    ├── tv/
    └── music/                 only if you use music
/mnt/user/appdata/mediarium    ->  mapped as /config
```

If you are starting fresh, create the `data` share (Shares → Add Share) and leave the rest to Mediarium: it creates the folders you name below. If you already have this layout for Radarr, Sonarr or SABnzbd, use it as is.

## Add the container

1. Open the **Docker** tab and click **Add Container** at the bottom.
2. Fill in the top of the form:
   - **Name:** `mediarium`
   - **Repository:** `ghcr.io/rdborg/mediarium:latest`
   - **Network Type:** `Bridge`
   - **WebUI** (switch on **Advanced View** at the top right to see it): `http://[IP]:[PORT:8264]/`
   - **Icon URL** (Advanced View, optional): `https://raw.githubusercontent.com/rdborg/Mediarium/main/branding/mediarium-icon-512.png`
3. Click **Add another Path, Port, Variable, Label or Device** once for each line below, choose the **Config Type**, and fill in the fields:

| Config Type | Name | Container value | Host value / Value |
|---|---|---|---|
| Port | Web UI | Container Port `8264` | Host Port `8264` (TCP) |
| Port | Torrent TCP (optional) | Container Port `58264` | Host Port `58264`, Connection Type TCP |
| Port | Torrent UDP (optional) | Container Port `58264` | Host Port `58264`, Connection Type UDP |
| Path | Config | Container Path `/config` | Host Path `/mnt/user/appdata/mediarium` |
| Path | Data | Container Path `/data` | Host Path `/mnt/user/data` |
| Variable | PUID | Key `PUID` | `99` |
| Variable | PGID | Key `PGID` | `100` |
| Variable | Timezone | Key `TZ` | your timezone, for example `Europe/London` |
| Variable | Downloads folder | Key `DOWNLOADS_DIR` | `/data/downloads/mediarium` |
| Variable | Movies folder | Key `MOVIES_DIR` | `/data/media/movies` |
| Variable | TV folder | Key `TV_DIR` | `/data/media/tv` |

4. Click **Apply**. Unraid downloads the image and starts Mediarium.
5. Click the Mediarium icon on the Docker tab → **WebUI**, or open `http://<unraid-ip>:8264`, and follow the setup wizard.

Notes on the values:

- **PUID 99 / PGID 100** are Unraid's usual `nobody` / `users`, the same most Unraid apps use. If your Radarr or Sonarr containers use other numbers, use theirs.
- **The folder variables** are where your folders are *inside* `/data`. If your movies are at `/mnt/user/data/media/movies`, that is `/data/media/movies`. For a brand-new setup you can use `/data/downloads`, `/data/movies` and `/data/tv`. Names are case sensitive.
- **Downloads folder of its own:** if SABnzbd, NZBGet or qBittorrent also use `/mnt/user/data/downloads`, keep Mediarium in its own sub-folder (`/data/downloads/mediarium`), because Mediarium tidies up leftovers in its own working folder.
- **Config on the cache:** keep `appdata` on your cache pool (SSD) so the app stays quick. Don't put it on a network share.
- **Torrent ports** are optional. Delete both torrent ports if you only use Usenet.

### Optional folders: music, ebooks and audiobooks

Add one more **Variable** for each kind of media you use. The folder must exist (Unraid creates a missing share folder when Mediarium first needs it, but it is tidier to create it yourself).

| Config Type | Name | Key | Value |
|---|---|---|---|
| Variable | Music folder | `MUSIC_DIR` | `/data/media/music` |
| Variable | Ebooks folder | `EBOOKS_DIR` | `/data/media/ebooks` |
| Variable | Audiobooks folder | `AUDIOBOOKS_DIR` | `/data/media/audiobooks` |

Music, ebooks and audiobooks each switch on under **Settings → Media types**. If a folder lives in another share, add a **Path** instead: Container Path `/music` (or `/ebooks`, `/audiobooks`) and the Host Path of that share, and skip the variable.

### Adding one later

Edit the container (Docker tab → the icon → **Edit**), add the variable or path, and **Apply**. Then switch the type on in Mediarium under **Settings → Media types** and check the folder under **Settings → Library → Folders and file names**.

## Running next to Radarr, Sonarr and SABnzbd

Mediarium runs beside your current apps. It has its own port (8264) and its own downloads folder, and shares the movies and TV folders with them. To bring your indexers, Usenet servers and library over, see [Move from Radarr, Sonarr, Prowlarr, SABnzbd and other apps](./migrate.md). The advice in the Synology guide applies here too: [if you already run other apps](./synology.md#if-you-already-run-other-apps) (on Unraid, to stop an old app: Docker tab → click the app → Stop, and turn its **Autostart** off).

## Updating

On the **Docker** tab, click **Check for Updates** at the bottom. When Mediarium shows **update ready**, click it (or the icon → **Update**). Your settings and library are kept. Read the [changelog](../CHANGELOG.md) first and back up before a big update.

When a new version is out, administrators see a card on the dashboard and on **Settings → System → Server and backup**. If the release is signed, the card also has an **Update now** button. See [Update now](./INSTALL.md#update-now-docker-image).

## Backing up

Everything that is not a media file is in `/mnt/user/appdata/mediarium` (`app.db` and `secret.key`; keep them together). The **Appdata Backup** plugin can back it up on a schedule, or use **Settings → System → Server and backup** in Mediarium to download a backup zip.

## Troubleshooting

- **A folder is "not writable":** the share's files belong to another user. Use the same PUID/PGID as your other media apps, or fix the permissions with **Tools → New Permissions** on that share (this resets the share to `nobody:users`, which matches 99/100).
- **"Finished downloads will be copied, not moved" in the setup wizard:** downloads and library are in different shares, or mapped as separate paths. Everything works, but each file is copied. To make it instant, use one share mapped once as `/data`.
- **The wizard shows `/movies`, `/tv` and `/downloads`, or "This folder isn't mapped to your server yet":** the folder variables above are missing, or you changed them after the first start. Fix them and press **Apply**. For a folder that isn't mapped, the wizard shows the line to add.
- **The page does not open:** check the container is started and look at its log (Docker tab → icon → **Logs**). It should say `Mediarium listening on :8264`. If another container already uses port 8264, change the Host Port.

More answers: [INSTALL.md troubleshooting](./INSTALL.md#troubleshooting).
