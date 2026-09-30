# Synology

**Status: nothing to submit. The Container Manager project below is the one-click route on Synology.**

## Why there is no Package Center entry

DSM's **Package Center only installs `.spk` packages**: Synology's own, packages from a third-party package source such as [SynoCommunity](https://synocommunity.com), or an `.spk` file installed by hand. Docker images cannot be listed there.

A native `.spk` is possible in principle (Mediarium is a single program), but it means cross-compiling for every Synology CPU family with the SynoCommunity [spksrc](https://github.com/SynoCommunity/spksrc) framework, submitting it there for review, and maintaining it. That is a "coming soon" item at best. Whether SynoCommunity would accept a package that merely wraps a Docker container was not verified.

## The practical one-click path: a Container Manager project

On DSM 7.2 and newer, **Container Manager → Project → Create** takes a compose file. `docker-compose.yml` in this folder is ready to paste; it matches [docs/synology.md](../../../docs/synology.md), which walks through finding PUID/PGID, choosing and creating the folders, the first run, running next to Radarr/Sonarr/SABnzbd, updating and backups.

## Checklist before announcing it for Synology

- [ ] The image `ghcr.io/rdborg/mediarium:latest` is published and public (Container Manager pulls it anonymously).
- [ ] Walk through `docs/synology.md` on a real DSM 7.2 NAS once, and correct any menu names that differ.
- [ ] Optional: screenshots of the Container Manager steps for the guide.

## What only the maintainer can do

- Test on a real Synology (or ask a user to), and, if a native package is ever wanted, set up and maintain a SynoCommunity submission.
