# Mediarium on Linux

On a Linux server (Debian, Ubuntu, Fedora, Raspberry Pi OS 64-bit, and so on) Mediarium runs in Docker. A native install without Docker (a `.deb`/`.rpm` package with a systemd service) is coming soon: see [What runs where](./PLATFORMS.md).

This is the short version, with Linux tips. The full walk-through is in the [install guide](./INSTALL.md).

## 1. Install Docker

Follow [docs.docker.com/engine/install](https://docs.docker.com/engine/install/) for your distribution. Then check:

```bash
docker compose version
```

To run `docker` without `sudo`, add yourself to the `docker` group (`sudo usermod -aG docker $USER`, then log out and in again).

## 2. Make the folders

Use one parent folder for downloads, movies and TV so finished downloads are hardlinked (instant, no extra space), and a small folder for Mediarium's settings. For example:

```bash
sudo mkdir -p /srv/data/downloads /srv/data/movies /srv/data/tv /opt/mediarium/config
sudo chown -R $(id -u):$(id -g) /srv/data /opt/mediarium
id
```

The last command prints your `uid` and `gid`: those are your `PUID` and `PGID`.

Want music too? Make the folder inside `/srv/data`, for example `sudo mkdir -p /srv/data/music` and `sudo chown $(id -u):$(id -g) /srv/data/music`, then take the `#` off `MUSIC_DIR=/data/music` in the compose file. Ebooks and audiobooks (`EBOOKS_DIR`, `AUDIOBOOKS_DIR`) work the same way. Nothing more needs mapping. To add one later, create the folder, add the line and run `docker compose up -d` again.

## 3. Create the compose file and start

```bash
cd /opt/mediarium
nano docker-compose.yml
```

Paste [the compose file from the install guide](./INSTALL.md#the-compose-file), then change:

- `PUID` and `PGID` to the numbers from `id`, and `TZ` to your timezone;
- the two `volumes` lines to `/opt/mediarium/config:/config` and `/srv/data:/data`. Folders in the compose file that don't exist are created by Docker as `root`, so make yours first, as above.

Save (Ctrl+O, Enter, Ctrl+X in nano) and start it:

```bash
docker compose up -d
docker compose logs -f mediarium
```

When the log says `Mediarium listening on :8264`, open `http://<server-ip>:8264` and follow the setup wizard.

## Updating

```bash
cd /opt/mediarium
docker compose pull
docker compose up -d
```

When a new version is out, administrators see a card on the dashboard and on **Settings → System → Server and backup**. If the release is signed, the card also has an **Update now** button. See [Update now](./INSTALL.md#update-now-docker-image).

## Starting at boot

Nothing extra is needed: Docker starts at boot, and `restart: unless-stopped` in the compose file starts Mediarium with it.

## Raspberry Pi

- Use a **64-bit** system: `uname -m` must print `aarch64`. Raspberry Pi 3, 4 and 5 can run the 64-bit Raspberry Pi OS. 32-bit systems (`armv7l`) are not supported yet.
- Keep `/config` and your media on a USB SSD or hard disk formatted **ext4**, not on the SD card, and not on exFAT/FAT (they cannot hardlink).
- A small Pi unpacks and repairs large downloads slowly, but it works.

## Firewall

If you use `ufw`: `sudo ufw allow 8264/tcp`, plus `sudo ufw allow 58264` if you publish the torrent port. Do not open port 8264 on your router: to reach Mediarium from outside, use a VPN or a reverse proxy with HTTPS.

## Troubleshooting

See [INSTALL.md troubleshooting](./INSTALL.md#troubleshooting). The most common Linux issue is ownership: `ls -ln /srv/data` shows the owner numbers, and they must match `PUID`/`PGID`.
