# What runs where

Mediarium is one Docker image, and every platform below runs that same image with the same settings. There is nothing separate to wait for: the install is the same compose file or the same few fields everywhere.

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

The image is `ghcr.io/rdborg/mediarium:latest` (also on Docker Hub as `rdborg/mediarium:latest`), built for `linux/amd64` and `linux/arm64`. `ghcr.io/rdborg/mediarium:latest-full` is the same with a Cloudflare helper built in (see [INSTALL.md](./INSTALL.md#optional-getting-past-cloudflare-checks)). Every release also has tags with its version number, for example `:4.1.0` and `:4.1` (and `:4.1.0-full` and `:4.1-full`): see [Updating](./INSTALL.md#updating).

## Not available

| What | Where it stands |
|---|---|
| Linux without Docker (`.deb`, `.rpm`, systemd) | Not planned for now |
| One-click entries in app stores (Unraid Community Applications, TrueNAS, CasaOS/ZimaOS, Umbrel, Proxmox helper scripts, Helm) | Not planned for now: the Docker install above does the same job. [Tell us](https://github.com/rdborg/Mediarium/issues/new/choose) which one you would use |
| Synology Package Center (`.spk`) | Not possible with a Docker app: use Container Manager ([synology.md](./synology.md)) |
| Raspberry Pi with a 32-bit OS (`armv7`) | Not supported |

There are no separate Windows or macOS versions: on a Windows PC or a Mac, run Mediarium in Docker Desktop. Each GitHub release also has a plain Linux program, without an installer. It's a barely tested preview and not supported. The notes inside the archive say what to set up.

## Raspberry Pi notes

- You need a **64-bit** system. `uname -m` must print `aarch64`. If it prints `armv7l` you're on 32-bit, which isn't supported yet. The 64-bit Raspberry Pi OS runs on the Pi 3, 4 and 5.
- The Pi Zero, Pi 1 and Pi 2 cannot run a 64-bit system.
- Keep the database and your media on a USB SSD or hard disk formatted ext4, not on the SD card.
- A Pi unpacks and repairs large downloads slowly, but it works.

## Want to help?

To test the install on a platform nobody has tried yet, or to ask for a store listing, [open an issue](https://github.com/rdborg/Mediarium/issues/new/choose) and say which.
