# Mediarium on a Synology NAS

This guide installs Mediarium on a Synology with **Container Manager**. You don't need to know Docker. You copy some numbers, make a few folders, paste one block of text and press Next.

Already run Radarr, Sonarr, Prowlarr or SABnzbd on the same NAS? Mediarium runs next to them on its own port, so you can try it without switching anything off. See [If you already run other apps](#if-you-already-run-other-apps) at the end.

General background (folders, hardlinks, updating) is in the main [install guide](./INSTALL.md).

## Contents

1. [What you need](#1-what-you-need)
2. [Find your user numbers](#2-find-your-user-numbers)
3. [Pick your media folders](#3-pick-your-media-folders)
4. [Create the folders first](#4-create-the-folders-first)
5. [Create the project](#5-create-the-project)
6. [First run](#6-first-run)
7. [Troubleshooting](#7-troubleshooting)
8. [Reach it from outside your home](#reach-it-from-outside-your-home)
9. [If you already run other apps](#if-you-already-run-other-apps)
10. [Updating](#updating)
11. [Backing up](#backing-up)

## 1. What you need

- **DSM 7.2 or newer** with **Container Manager**. If it isn't installed: Package Center, search "Container Manager", Install. It is only offered on models that support it. If you run Radarr or Sonarr in Docker today, yours does.
- An Intel/AMD or 64-bit ARM Synology. The Mediarium image comes in both kinds.
- An administrator account on the NAS.
- About ten minutes.

## 2. Find your user numbers

Mediarium saves files as a user and group you choose, by number. You want the same user your other apps use, so they can all read and write the same files. The two numbers are called **PUID** and **PGID**.

**The easy way: copy them from an app you already run.**

1. Open **Container Manager**, then **Container**.
2. Click your **radarr** (or sonarr, or sabnzbd) container, then **Details**.
3. Open the **Environment** tab. Find `PUID` and `PGID` and write both numbers down. On the test NAS they were `1026` and `101`. Use what you see on yours.

If you made your apps as a Container Manager **project**, open **Project**, click your project, then **YAML**. The numbers are on the `PUID=` and `PGID=` lines.

**No other apps?** Turn on SSH (Control Panel, Terminal & SNMP, Enable SSH service), connect with `ssh youradmin@your-nas-address`, and run `id youruser`, using the account that owns your media. The `uid=` number is your PUID and the `gid=` number is your PGID. You can turn SSH off again afterwards.

## 3. Pick your media folders

This is the one choice that matters. There are two ways to set it up.

### Recommended: one shared folder for everything

Put downloads, movies, TV and music inside **one shared folder**, and give Mediarium that one folder. Mediarium then moves finished downloads into your library instantly, without copying and without using extra space.

Here the shared folder is called `Media`. It is your own folder, so any name works:

```
Media                     (/volume1/Media)   <- Mediarium sees this as /data
├── Movies                                   <- /data/Movies
├── tv                                       <- /data/tv
├── Music                                    <- /data/Music   (only if you use music)
└── downloads                                <- /data/downloads

docker
└── mediarium             (/volume1/docker/mediarium)   <- Mediarium's settings, seen as /config
```

Already have a shared folder with your movies and TV in it? Use that one and add a `downloads` folder inside it.

Folder names are case sensitive. `Movies` and `movies` are different folders, and the name in the compose text below has to match the real one exactly.

### The other way: separate shared folders

If your movies, TV and downloads already live in different shared folders (for example `video` and `downloads`), you can map each one on its own. It works, with one catch: every shared folder on a Synology is its own disk area, so Mediarium can't move files between them and **copies** each finished download into the library instead.

- Usenet downloads are deleted after the copy, so all you lose is a little time.
- Torrents keep seeding from the downloads folder, so the file takes double the space until it is done seeding.

If that is fine with you, use the second compose text in step 5. You can move to one shared folder later.

## 4. Create the folders first

Do this before you create the project. If a folder you map doesn't exist, Container Manager refuses to start the project and shows an error like this:

```
Bind mount failed: '/volume1/Media/downloads' does not exist
```

In **File Station**:

1. Open the `docker` shared folder and create a folder named `mediarium`. This holds Mediarium's settings and database.
2. Open your `Media` shared folder (or whichever one you picked in step 3) and create `downloads`. Create `Movies`, `tv` and `Music` too if they aren't there yet.
3. Check that your user has **Read/Write** on that shared folder: Control Panel, Shared Folder, select it, Edit, Permissions. If Radarr and Sonarr already work there, it does.

Not sure of a folder's path? Right-click it in File Station and choose **Properties**. `/volume1` is your first storage volume. Yours may be `/volume2`.

## 5. Create the project

1. Open **Container Manager**, then **Project**, then **Create**.
2. **Project name:** `mediarium`.
3. **Path:** click **Set Path** and choose the `docker/mediarium` folder from step 4.
4. **Source:** choose **Create docker-compose.yml** and paste the text below.

**One shared folder (recommended):**

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
      # CHANGE: the numbers from step 2, and your timezone.
      - PUID=1026
      - PGID=101
      - TZ=Europe/London
      # Where things are inside the Media folder. Match your real folder names.
      - DOWNLOADS_DIR=/data/downloads
      - MOVIES_DIR=/data/Movies
      - TV_DIR=/data/tv
      # Only if you use music. Delete the line if you don't.
      - MUSIC_DIR=/data/Music
    volumes:
      # CHANGE the left side of each line if your folders are somewhere else.
      - /volume1/docker/mediarium:/config
      - /volume1/Media:/data
```

**Separate shared folders (copies instead of moves):**

```yaml
services:
  mediarium:
    image: ghcr.io/rdborg/mediarium:latest
    container_name: mediarium
    restart: unless-stopped
    ports:
      - "8264:8264"
      - "58264:58264/tcp"
      - "58264:58264/udp"
    environment:
      - PUID=1026
      - PGID=101
      - TZ=Europe/London
    volumes:
      # The names on the right (/movies, /tv, /downloads, /music) are what
      # Mediarium expects, so no folder lines are needed above.
      - /volume1/docker/mediarium:/config
      - /volume1/video/Movies:/movies
      - /volume1/video/tv:/tv
      - /volume1/downloads:/downloads
      # Only if you use music:
      - /volume1/music:/music
```

5. Change the numbers, timezone and `/volume1/...` paths to match your NAS. If you made a music folder, keep the `Music` lines. Otherwise delete them.
6. Click **Next**. On the **Web portal settings** page leave **Set up web portal via Web Station** unticked. You don't need it.
7. Click **Next**, make sure **Start the project once it is created** is ticked, then **Done**. Container Manager downloads the image and starts Mediarium. The project should show **Running**.

## 6. First run

1. In your browser open `http://<your-nas-address>:8264`, for example `http://192.168.1.20:8264`.
2. Create your administrator account.
3. **Media types:** leave Movies and TV shows on. Tick Music if you want it.
4. **Library paths:** the boxes show the folders from your compose text, for example `/data/Movies`, `/data/tv` and `/data/downloads`. Each one should say **writable** and **Mapped to your device**, and you should see a green **Same filesystem. Hardlinking will work.** below the boxes. A folder that isn't mapped shows the exact line to add to your compose text.
5. **Connect services** needs nothing from you. On **Indexers** and **Usenet provider** you can choose **Skip for now** if you plan to bring them over from Prowlarr and SABnzbd ([Move from other apps](./migrate.md)).
6. **Quality & naming** asks which media player you use, so your files are named the way it likes. **Media server** is optional. It connects Plex, Jellyfin or Emby, so "Watch in" links work from day one.

Downloads run **one at a time**. Each is downloaded, repaired, unpacked and moved into your library before the next starts. To allow more, use **Settings > Downloading > Usenet and torrents > Downloads at the same time**. See [Downloads](./downloads.md#downloads-at-the-same-time-the-download-line).

## 7. Troubleshooting

**"Bind mount failed: '/volume1/Media/downloads' does not exist"**
A folder in the compose text isn't there. Create it in File Station with exactly that name (capital letters count), then start the project again. If it still fails, stop the project and choose **Action, Build**, then **Start**.

**The wizard shows `/movies`, `/tv` and `/downloads`, or says "not a mapped folder"**
Mediarium shows the folders your compose text gives it. If you see the short names above, the `DOWNLOADS_DIR`, `MOVIES_DIR` and `TV_DIR` lines are missing or you changed the text after the first start. Fix the text (**Project, mediarium, YAML**), stop the project, choose **Action, Build**, then **Start**. If the wizard offers to switch a saved folder to the one your compose text maps, accept it.

**"This folder isn't mapped to your server yet"**
Docker only sees folders you map. The message shows the line to add under `volumes:`, for example `- /volume1/Media/Music:/music`. The part before the colon is the folder on your NAS. The part after it is the name Mediarium uses. Create the NAS folder first, add the line, then Build and Start the project.

**"Finished downloads will be copied, not moved"**
Your downloads and your library are in different shared folders. Everything still works. To make it instant, use one shared folder as in [step 3](#recommended-one-shared-folder-for-everything).

**A folder is "read-only" or not writable**
`PUID` and `PGID` don't match a user with write access. Check the numbers ([step 2](#2-find-your-user-numbers)) and give that user **Read/Write** on the shared folder in Control Panel, Shared Folder, Edit, Permissions. Synology permissions are set there, not with `chown`.

**The page doesn't open**
Check the project is **Running**, then open the container's **Log** tab. It should say `Mediarium listening on :8264`. If the Synology firewall is on (Control Panel, Security, Firewall), allow port 8264. If another app already uses 8264, change the left number to something else, for example `"8265:8264"`, and open that port.

**"Project creation failed" or the image doesn't download**
Check that the NAS can reach the internet. Also check that the text was pasted with its spaces intact. Compose text is sensitive to indentation.

More answers: [install guide troubleshooting](./INSTALL.md#troubleshooting). Still stuck? [Open an issue](https://github.com/rdborg/Mediarium/issues/new/choose) and say you are on a Synology, with your DSM version and NAS model.

## Adding music, ebooks or audiobooks later

Music, ebooks and audiobooks each switch on under **Settings, Media types**. If your movies, TV and downloads share one folder (like `/data`), Mediarium offers to create the new folder inside it for you, so often you don't need to change the project at all.

- **Inside your one shared folder:** create the folder (for example `Music`), add `MUSIC_DIR=/data/Music`, `EBOOKS_DIR=/data/Ebooks` or `AUDIOBOOKS_DIR=/data/Audiobooks` to the `environment:` lines, then stop the project, choose **Action, Build** and **Start**. Nothing else to map.
- **A folder somewhere else:** add a line under `volumes:`, for example `- /volume1/music:/music`, and rebuild the same way. No `environment:` line is needed when the right side is `/music`, `/ebooks` or `/audiobooks`.
- Then switch the type on in **Settings, Media types** and check the folder in **Settings, Library, Folders and file names**.

## Reach it from outside your home

To open Mediarium on your phone away from home, or to share it with family, use the reverse proxy built into DSM. It gives Mediarium a proper web address with HTTPS (for example `https://mediarium.example.com`). Mediarium needs nothing special from it: no WebSocket option and no extra headers beyond the one below.

**Before you start:** you need a host name that points at your home internet address. Synology's free DDNS (**Control Panel, External Access, DDNS**, for example `yourname.synology.me`) works, or a name from your own domain.

1. **Open port 443 on your router** and forward it to the NAS. Forward only 443 (and 80 if you want Let's Encrypt to renew over HTTP). **Never forward port 8264**: Mediarium should only be reachable through the proxy.
2. **Get a certificate.** **Control Panel, Security, Certificate, Add, Add a new certificate, Get a certificate from Let's Encrypt.** Use your host name (for example `mediarium.yourname.synology.me`).
3. **Create the proxy rule.** **Control Panel, Login Portal, Advanced, Reverse Proxy, Create.**
   - **Source:** Protocol `HTTPS`, Hostname `mediarium.yourname.synology.me`, Port `443`.
   - **Destination:** Protocol `HTTP`, Hostname `localhost`, Port `8264` (the port on the left of `8264:8264` in your project).
4. **Add one header.** In the same window, open **Custom Header, Create** and add `X-Forwarded-Proto` with the value `$scheme`. If your DSM version lists only the WebSocket option, choose **Create** and type the name and value yourself. Mediarium works without this header, but with it your sign-in cookie is marked HTTPS-only.
5. **Give the rule the certificate.** **Control Panel, Security, Certificate, Settings**, find your proxy entry and pick the certificate from step 2.
6. Open `https://mediarium.yourname.synology.me` and sign in.

**You don't need to change Mediarium's settings for this.** The DSM proxy runs on the NAS itself, so Mediarium already trusts it to pass on your visitors' real addresses (`TRUSTED_PROXIES` covers it by default). Sign-in limits, HTTPS-only cookies and the cross-site checks all keep working.

**If something isn't right:**
- **"Blocked: this request came from another web site" when you save something:** the proxy changed the `Host` header. Leave the Destination host name as `localhost` and don't add a `Host` custom header. See [ALLOWED_ORIGINS](./security.md#allowed_origins) if you must.
- **Updates pushed through the API or large backup restores fail:** DSM's proxy allows fairly big uploads, but if a restore stops part way, restore from your home network instead (`http://<NAS-IP>:8264`).
- **Firewall:** if the Synology firewall is on, allow 443 from anywhere and 8264 only from your home network.

The [security guide](./security.md) has the full checklist, and other proxies (Nginx Proxy Manager, Traefik, Caddy, Cloudflare Tunnel) if you use one of those instead. The **Sign in through your reverse proxy** option under Settings, Accounts is only for proxies that do the login themselves (Authelia, Authentik); the DSM proxy doesn't, so leave it off.

## If you already run other apps

You can keep Radarr, Sonarr and SABnzbd running while you try Mediarium. They share the library folders, and each app keeps its own port and its own downloads.

- **Bring your settings over:** [Move from Radarr, Sonarr, Prowlarr, SABnzbd and other apps](./migrate.md) imports your indexers, Usenet servers and library, so you don't type everything again.
- **Let only one app download each title.** If Radarr and Mediarium both search for the same movie, you can download it twice. While you try Mediarium, add only new titles to it, or turn off monitoring in Radarr and Sonarr for the titles Mediarium looks after.
- **Give Mediarium its own downloads folder.** Mediarium tidies up leftovers in its own download folder, so it must not share one with SABnzbd or qBittorrent. If your other apps use `/volume1/Media/downloads`, use `DOWNLOADS_DIR=/data/downloads/mediarium` instead. Mediarium creates that folder.
- **Plex, Jellyfin and Emby** keep working as before. Connect them under **Settings, Connections, Media servers** so they refresh when Mediarium adds something ([media-servers.md](./media-servers.md)).
- **Usenet connections:** Mediarium and SABnzbd both count against your provider's connection limit. If a download stops with "too many connections", stop SABnzbd or lower the number in one of them. Mediarium lowers its own number by itself when the provider says so (see [downloads.md](./downloads.md#usenet-connections)).

When Mediarium does everything you need, stop the old containers (**Container Manager, Container, select them, Action, Stop**) and leave them stopped for a few weeks before you delete them. Your library isn't touched: the movies and TV folders belong to you, not to the apps.

## Updating

**Over SSH (quickest):**

```bash
cd /volume1/docker/mediarium
sudo docker compose pull
sudo docker compose up -d
```

**From Container Manager only:**

1. **Project, mediarium, Action, Stop**, then **Action, Clean**. This removes the container, not your folders.
2. **Image:** select `ghcr.io/rdborg/mediarium` and **Delete** it. Newer versions of Container Manager may show **Update available** here with an **Update** button. If so, use that and skip the Build in the next step.
3. **Project, mediarium, Action, Build**, then **Start** if it doesn't start by itself. Container Manager downloads the newest image because the old one is gone.

Your settings and library are kept, and Mediarium updates its database when it starts. Read the [changelog](../CHANGELOG.md) first and back up before a big update.

When a new version is out, administrators see a card on the dashboard and on **Settings > System > Server and backup**. If the release is signed, the card also has an **Update now** button, which installs it without using Container Manager. See [Update now](./INSTALL.md#update-now-docker-image).

## Backing up

- **Hyper Backup:** include the `docker/mediarium` folder in a backup task. It holds everything that isn't a media file (`app.db` and `secret.key`; keep the two together).
- **From the app:** **Settings, System, Server and backup** can download a backup zip. It contains your encryption key, so keep it private.

---

**About one-click installs:** Synology's Package Center only installs `.spk` packages (for example from SynoCommunity), and Mediarium isn't packaged that way. A Container Manager project, as above, is the practical one-click route on a Synology. See [What runs where](./PLATFORMS.md).
