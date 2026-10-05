# What runs where

Docker is the one way to install Mediarium today, and it covers most home servers and NAS systems. The other ways below are coming soon.

## Available now

| Platform | How | Guide |
|---|---|---|
| Any 64-bit Linux server or NAS with Docker (Intel/AMD `amd64`, ARM `arm64`) | Docker Compose, or one `docker run` command | [INSTALL.md](./INSTALL.md) |
| Synology DSM 7.2+ | Container Manager project (paste the compose file) | [synology.md](./synology.md) |
| Unraid | Docker tab → Add Container | [unraid.md](./unraid.md) |
| QNAP | Container Station application (paste the compose file) | [qnap.md](./qnap.md) |
| Portainer, TrueNAS SCALE 24.10+, CasaOS/ZimaOS, OpenMediaVault | Paste the same compose file into their Docker screen | [Using a Docker app](./INSTALL.md#using-a-docker-app) |
| Raspberry Pi 3, 4 or 5 with a **64-bit** OS | Docker Compose | [linux.md](./linux.md#raspberry-pi) |
| Windows or Mac with Docker Desktop | Docker Compose, fine for trying it out | [INSTALL.md](./INSTALL.md#using-a-docker-app) |

The image is `ghcr.io/rdborg/mediarium:latest`, built for `linux/amd64` and `linux/arm64`. `ghcr.io/rdborg/mediarium:latest-full` is the same with a Cloudflare helper built in (see [INSTALL.md](./INSTALL.md#optional-getting-past-cloudflare-checks)). Every release also has tags with its version number, for example `:1.3.0` and `:1.3` (and `:1.3.0-full` and `:1.3-full`): see [Updating](./INSTALL.md#updating).

## Coming soon

| Platform | What it will be | Status |
|---|---|---|
| Linux without Docker | `.deb` and `.rpm` packages with a systemd service | Coming soon |
| Unraid Community Applications | One-click install from the Apps tab | Coming soon |
| TrueNAS SCALE app catalog | One-click install from Apps → Discover | Coming soon |
| CasaOS / ZimaOS App Store | One-click install from the App Store | Coming soon |
| Umbrel | Install from a community app store | Coming soon |
| Portainer | An app template you add by URL | Coming soon |
| Proxmox | A helper script that creates a ready-made LXC container | Coming soon |
| Kubernetes | A Helm chart | Coming soon |
| Raspberry Pi with a 32-bit OS (`armv7`) | An `armv7` image | Not supported yet |

Until then, anything that runs Docker can run Mediarium: see the [Docker install](./INSTALL.md).

There are no separate Windows or macOS versions: on a Windows PC or a Mac, run Mediarium in Docker Desktop. Each GitHub release also has a plain Linux program, without an installer. It's a barely tested preview and not supported yet. The notes inside the archive say what to set up. Tell us how it goes.

## Raspberry Pi notes

- You need a **64-bit** system. `uname -m` must print `aarch64`. If it prints `armv7l` you're on 32-bit, which isn't supported yet. The 64-bit Raspberry Pi OS runs on the Pi 3, 4 and 5.
- The Pi Zero, Pi 1 and Pi 2 cannot run a 64-bit system.
- Keep the database and your media on a USB SSD or hard disk formatted ext4, not on the SD card.
- A Pi unpacks and repairs large downloads slowly, but it works.

## Want to help?

To test one of the coming-soon platforms, [open an issue](https://github.com/rdborg/Mediarium/issues/new/choose) and say which.
