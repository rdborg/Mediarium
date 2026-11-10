<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="branding/mediarium-lockup-dark.svg" />
    <img src="branding/mediarium-lockup-light.svg" alt="Mediarium" height="64" />
  </picture>
</p>

<p align="center"><b>Your movies, TV, music and books, from search to library, in one app.</b></p>

<p align="center">
  <a href="https://github.com/rdborg/Mediarium/releases/latest"><img src="https://img.shields.io/github/v/release/rdborg/Mediarium?label=release&color=2dd4bf" alt="Latest release" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/licence-AGPL--3.0-f5a524" alt="Licence: AGPL-3.0" /></a>
  <img src="https://img.shields.io/badge/runs%20on-Docker%20(amd64%20%2B%20arm64)-38bdf8" alt="Runs on Docker, amd64 and arm64" />
  <img src="https://img.shields.io/badge/languages-12-e879f9" alt="12 app languages" />
</p>

<p align="center">
  <a href="docs/INSTALL.md">Install</a> ·
  <a href="docs/README.md">Documentation</a> ·
  <a href="docs/FEATURES.md">Features</a> ·
  <a href="CHANGELOG.md">Changelog</a> ·
  <a href="https://github.com/rdborg/Mediarium/issues">Report a bug</a> ·
  <a href="https://github.com/rdborg/Mediarium/discussions">Ideas and questions</a> ·
  <a href="https://www.reddit.com/r/Mediarium/">Reddit</a>
</p>

# Mediarium

Mediarium is a self-hosted media manager. Find a movie or show, and it searches your Usenet and torrent indexers, downloads the best release, unpacks it, names it and puts it in your library, ready for Plex, Jellyfin or Emby.

It does the jobs of Radarr, Sonarr, Prowlarr, SABnzbd, a torrent client and Bazarr in **one app**: one container, one web page, one search bar, one thing to update and back up.

- **Movies and TV** (anime, daily shows and specials included), plus **music**, **ebooks** and **audiobooks** if you switch them on.
- **Mediarium Books**, a built-in reader and audiobook player that opens as an app of its own and remembers your place on every device. It reads Kindle books too, jumps between audiobook chapters, and follows book series.
- **Tags** such as Kids or 4K, which also show up as collections in Plex, Jellyfin and Emby.
- **Discover and search** everything from one place, across all your indexers. Music has one box for artists, albums and songs, and Soulseek can be added through your own slskd.
- **Search for the complete series** on a show that has ended: season packs first, then one pack of the whole series.
- **Ratings** from IMDb, Rotten Tomatoes and Metacritic on film and show pages, with your own free OMDb key.
- **Built-in downloaders** for Usenet and torrents, with repair and unpacking. Each torrent indexer can send its torrents to your client, to debrid or to the built-in one.
- **Works with your phone apps.** Mediarium can answer like SABnzbd and qBittorrent, so nzb360 and similar apps show speed, time left and the queue (off until you switch it on), and Radarr and Sonarr apps such as LunaSea and Ruddarr connect as before.
- **Built-in VPN** for torrents (WireGuard), with no special container permissions.
- **Quality profiles** with upgrades and fallbacks, automatic searching and retries.
- **Family accounts with permissions and requests**: choose what each person can do, and anyone who can't add titles sends a request for you to approve.
- **Subtitles** picked to match your exact file, with a timing fix when one is out of sync.
- **What's been watched**, read from Plex, Jellyfin and Emby, with watch statistics and optional cleanup rules (all off until you switch them on).
- **Twelve app languages**, including Simplified Chinese, and a layout that fits phones, foldable phones and tablets, with a bottom bar on a phone.
- **Statistics** per Usenet provider and indexer, a **Long wanted** list for what is still missing long after release, a calendar with a month view, and import lists that can read an RSS feed.
- **Signed releases** with a build attestation and a list of everything the program is built from (SBOM).
- Notifications, media-server refresh (Plex, Jellyfin, Emby, Audiobookshelf, Kavita), download hours, backups, and your own script after each import.
- **Move over from Radarr, Sonarr, Prowlarr, SABnzbd** and eight other apps without redoing your setup. They are only read, never changed.
- Runs on **Synology, Unraid, QNAP, any Linux server** and 64-bit Raspberry Pi.

**Privacy:** there are no ads, and Mediarium sends nothing about you or your library anywhere. It asks once whether you want to share a small anonymous message a day (a random ID, the version and rough device sizes) so the project can see which devices are popular: it is off until you say Yes, the numbers are public at [mediarium.app/stats](https://mediarium.app/stats/), and the code is open. [What exactly is sent](docs/usage-stats.md).

Mediarium 4.0 is out. Movies and TV work end to end, and so do music, ebooks and audiobooks once you switch them on. It's used daily, but expect some rough edges. [FEATURES.md](docs/FEATURES.md) says what works and what doesn't yet.

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
| ![One music search with an artist, albums and songs in one list](docs/images/music-search.png) | ![Statistics with the Usenet providers and a month by month table](docs/images/statistics.png) |
| **Music search**: artists, albums and songs in one list, each marked with what it is | **Statistics**: what each Usenet provider delivered, month by month |
| ![Other apps: connect Overseerr, nzb360 and other apps](docs/images/other-apps.png) | |
| **Other apps**: let Overseerr, Homepage and phone apps such as nzb360 talk to Mediarium | |

<p align="center"><img src="docs/images/phone-dashboard.png" alt="Mediarium on a phone" width="260" /></p>

> **About these screenshots.** They were taken in demo mode, so every movie, show, artist, book, poster and cover in them is made up. That's on purpose: the pictures don't use anyone else's titles or artwork. In your own copy, Mediarium shows the real posters, artwork and details for whatever you search for and add.

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

### One image for every platform

Mediarium comes as one Docker image, and everything that can run Docker runs the same image with the same settings. There is no separate build or app-store version to wait for.

| Platform | How |
|---|---|
| Any 64-bit Linux server, NAS or Raspberry Pi (Intel/AMD or ARM64) | Docker Compose, or one `docker run` command (the image is on GitHub and on Docker Hub as `rdborg/mediarium`) |
| Synology, QNAP, Unraid, TrueNAS SCALE, CasaOS/ZimaOS, OpenMediaVault, Portainer | Paste the same compose file into their Docker screen: [guides](docs/PLATFORMS.md) |
| Windows PC or Mac | Docker Desktop, fine for trying it out |

There are no one-click app-store entries for now, and no separate Windows or macOS versions.

Details: [What runs where](docs/PLATFORMS.md).

## Documentation

Everything is in [`docs/`](docs/README.md): installing, indexers, downloads and the torrent port, quality profiles, subtitles, media servers, family accounts, and reference pages for the API, environment variables and settings.

## Getting help

- **Something not working?** Check [troubleshooting](docs/INSTALL.md#troubleshooting), then [open an issue](https://github.com/rdborg/Mediarium/issues/new/choose). Include your version (Settings > System > About and credits), or better, the text that **Copy for support** puts on your clipboard (Settings > System > Server and updates > Help and support). It has no passwords or keys in it.
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

### The name and logo

The licence covers the code, not the name. "Mediarium" and the Mediarium logo are the names I use for this project. You're welcome to fork it, change it and share your version under the AGPL, but if you share a changed version, please give it a different name and logo so people can tell it apart from this one. Saying it's based on Mediarium is fine, and appreciated.
