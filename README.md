<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="branding/mediarium-lockup-dark.svg" />
    <img src="branding/mediarium-lockup-light.svg" alt="Mediarium" height="64" />
  </picture>
</p>

<p align="center"><b>Your movies, TV, music and books, from search to library, in one app.</b></p>

<p align="center">
  <a href="docs/INSTALL.md">Install</a> ·
  <a href="docs/README.md">Documentation</a> ·
  <a href="docs/FEATURES.md">Features</a> ·
  <a href="CHANGELOG.md">Changelog</a> ·
  <a href="https://github.com/rdborg/Mediarium/issues">Report a bug</a> ·
  <a href="https://github.com/rdborg/Mediarium/discussions">Ideas and questions</a>
</p>

# Mediarium

Mediarium is a self-hosted media manager. Find a movie or show, and it searches your Usenet and torrent indexers, downloads the best release, unpacks it, names it and puts it in your library, ready for Plex, Jellyfin or Emby.

It does the jobs of Radarr, Sonarr, Prowlarr, SABnzbd, a torrent client and Bazarr in **one app**: one container, one web page, one search bar, one thing to update and back up.

- **Movies and TV** (anime, daily shows and specials included), plus **music**, **ebooks** and **audiobooks** if you switch them on.
- **Mediarium Books**, a built-in reader and audiobook player that opens as an app of its own and remembers your place on every device. It reads Kindle books too, jumps between audiobook chapters, and follows book series.
- **Tags** such as Kids or 4K, which also show up as collections in Plex, Jellyfin and Emby.
- **Discover and search** everything from one place, across all your indexers.
- **Built-in downloaders** for Usenet and torrents, with repair and unpacking.
- **Built-in VPN** for torrents (WireGuard), with no special container permissions.
- **Quality profiles** with upgrades and fallbacks, automatic searching and retries.
- **Family accounts with permissions and requests**: choose what each person can do, and anyone who can't add titles sends a request for you to approve.
- **Subtitles** picked to match your exact file, with a timing fix when one is out of sync.
- **What's been watched**, read from Plex, Jellyfin and Emby, with watch statistics and optional cleanup rules (all off until you switch them on).
- Notifications, media-server refresh (Plex, Jellyfin, Emby, Audiobookshelf, Kavita), download hours, backups, and your own script after each import.
- **Move over from Radarr, Sonarr, Prowlarr, SABnzbd** and eight other apps without redoing your setup. They are only read, never changed.
- Runs on **Synology, Unraid, QNAP, any Linux server** and 64-bit Raspberry Pi.

Mediarium is new. Movies and TV work end to end, and so do music, ebooks and audiobooks once you switch them on. It's used daily, but expect some rough edges. [FEATURES.md](docs/FEATURES.md) says what works and what doesn't yet.

## Screenshots

| | |
|---|---|
| ![Dashboard with a card for each kind of media and a Server card](docs/images/dashboard.png) | ![Discover with rows of movies and shows like the ones in your library](docs/images/discover.png) |
| **Dashboard**: what needs attention, a card per kind of media and the Server card | **Discover**: more like your library, trending, popular and coming soon |
| ![Library of movies with a status badge on each poster](docs/images/library.png) | ![Activity page with downloads in progress, waiting in line and failed](docs/images/activity.png) |
| **Library**: every title with its status, and Select to change many at once | **Activity**: what is downloading, waiting in line or failed, plus history and blocklist |
| ![Upcoming calendar for the month](docs/images/upcoming-calendar.png) | ![An artist page in the music library](docs/images/music-artist.png) |
| **Upcoming**: the calendar of releases and episodes, and what is still wanted | **Music**: artists and albums, off until you switch it on |
| ![Discover on the eBooks and Audiobooks tab, with a corner banner on each cover saying eBook, Audiobook or both](docs/images/discover-books.png) | ![The Mediarium Books shelf with the books you are reading or listening to at the top](docs/images/bookshelf.png) |
| **Books**: trending and classic books, each marked eBook, Audiobook or both | **Mediarium Books**: your shelf, with the reader and the audiobook player |
| ![The Media types page with cards for movies, TV shows, music, audiobooks and ebooks](docs/images/settings-modules.png) | ![The Quality page with the quality profiles table](docs/images/settings-quality.png) |
| **Media types**: switch movies, TV, music, ebooks and audiobooks on or off | **Quality**: profiles and the fallback order |

<p align="center"><img src="docs/images/phone-dashboard.png" alt="Mediarium on a phone" width="260" /></p>

> **About these screenshots.** They were taken in demo mode, so every movie, show, artist, poster and cover in them is made up (the books are old public-domain classics). That's on purpose: the pictures don't use anyone else's titles or artwork. In your own copy, Mediarium shows the real posters, artwork and details for whatever you search for and add.

## Install with Docker

Mediarium runs in Docker on any 64-bit Linux server or NAS (`amd64` or `arm64`).

```bash
mkdir mediarium && cd mediarium
curl -fsSLO https://raw.githubusercontent.com/rdborg/Mediarium/main/docker/docker-compose.yml
# open docker-compose.yml and set PUID, PGID, TZ and your folders
docker compose up -d
```

Then open `http://<your-server-ip>:8264` and follow the setup wizard.

The [install guide](docs/INSTALL.md) covers every line of [`docker-compose.yml`](docker/docker-compose.yml): folders, PUID/PGID, why one `/data` folder makes imports instant, updating, backups and a `docker run` alternative.

Step-by-step guides: **[Synology](docs/synology.md)** (including running next to Radarr, Sonarr and SABnzbd) · **[Unraid](docs/unraid.md)** · **[QNAP](docs/qnap.md)** · **[Linux](docs/linux.md)**. Coming from the *arr apps? See [Move from Radarr, Sonarr, Prowlarr and SABnzbd](docs/migrate.md).

### Other ways to install: coming soon

| Platform | Status |
|---|---|
| Docker on Linux, NAS or Raspberry Pi (64-bit) | **Available now** |
| Linux without Docker (`.deb`, `.rpm`, systemd) | Coming soon |
| One-click apps: Unraid, TrueNAS, CasaOS/ZimaOS, Umbrel, Portainer | Coming soon |
| Proxmox helper script, Helm chart | Coming soon |

There are no separate Windows or macOS versions. On a Windows PC or a Mac, Mediarium runs in Docker Desktop.

Details: [What runs where](docs/PLATFORMS.md).

## Documentation

Everything is in [`docs/`](docs/README.md): installing, indexers, downloads and the torrent port, quality profiles, subtitles, media servers, family accounts, and reference pages for the API, environment variables and settings.

## Getting help

- **Something not working?** Check [troubleshooting](docs/INSTALL.md#troubleshooting), then [open an issue](https://github.com/rdborg/Mediarium/issues/new/choose). Include your version (Settings > System > About and credits), or better, the text that **Copy for support** puts on your clipboard (Settings > System > Server and backup > Help and support). It has no passwords or keys in it.
- **Security problem?** Report it privately, as described in [SECURITY.md](.github/SECURITY.md).
- **Ideas and feature requests** are welcome as [issues](https://github.com/rdborg/Mediarium/issues/new/choose).

## Support the project

Mediarium is free, open source and has no ads. If it saves you time, you can [buy a coffee on Ko-fi](https://ko-fi.com/ryanborg). The app works exactly the same either way.

## Contributing

Contributions are welcome. Start with [CONTRIBUTING.md](.github/CONTRIBUTING.md) and follow the [Code of Conduct](.github/CODE_OF_CONDUCT.md).

## Responsible use

Mediarium does not host, provide or link to any content. It is a tool for managing media you have the right to access. You are responsible for the indexers you add and what you download: read [LEGAL.md](docs/LEGAL.md).

## Licence

Mediarium is free software under the [GNU Affero General Public License v3.0](LICENSE) (AGPL-3.0). You can use it, change it and share it. If you offer a changed version to others as a network service, you must share your source code under the same licence.
