# Mediarium on TrueNAS SCALE (notes, UNTESTED)

Nobody has run this on a TrueNAS box yet. It is written from general knowledge of
how TrueNAS SCALE handles Docker apps, and the menu names below may differ between
TrueNAS versions. Corrections welcome.

## What is known

- TrueNAS SCALE moved its "Apps" from Kubernetes to Docker in the 24.10 release.
  Newer versions let you install a custom app from a pasted Docker Compose (YAML)
  definition. Older Kubernetes-based releases (24.04 and earlier) have a different
  "Launch Docker Image" form and are not covered here.
- There is no Mediarium entry in the TrueNAS app catalog. Getting one would mean a
  submission to the TrueNAS apps catalog project, which was not investigated.
  Until then use the custom-app route.

## Datasets and hardlinks (the part that matters)

Hardlinks only work inside ONE filesystem. On TrueNAS every dataset is its own
filesystem, so `tank/downloads` and `tank/movies` as two datasets cannot hardlink to
each other even though they are on the same pool. Make ONE dataset and put plain
folders inside it:

```
tank/media                (one dataset)
  downloads/
  movies/
  tv/
tank/apps/mediarium       (a second dataset for /config)
```

Give the app permission to write: create the folders, then set the dataset's
owner/ACL to the user and group you will use as `PUID` / `PGID`. TrueNAS's built-in
apps user is commonly uid/gid 568, but check under Credentials > Local Users rather
than trusting this note.

## Compose to paste

Adjust the two host paths (`/mnt/tank/...`), `PUID`, `PGID` and `TZ`.

```yaml
services:
  mediarium:
    image: ghcr.io/rdborg/mediarium:latest   # not published yet, see docs/INSTALL.md
    container_name: mediarium
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      PUID: "568"
      PGID: "568"
      TZ: "Etc/UTC"
      DOWNLOADS_DIR: /data/downloads
      MOVIES_DIR: /data/movies
      TV_DIR: /data/tv
    volumes:
      - /mnt/tank/apps/mediarium:/config
      - /mnt/tank/media:/data
```

Then in TrueNAS: Apps, choose the option to install a custom app, pick the YAML
option, paste the above, and deploy. Open `http://<truenas-ip>:8080`.

## Things to check if it does not work

- The container log says a folder "is not writable": fix the dataset owner/ACL or
  change `PUID`/`PGID`. Mediarium never changes ownership of your media folders.
- Port 8080 may already be used by another app; change the left side of `8080:8080`.
- If TrueNAS rejects the YAML, remove the `container_name` line first, some
  versions manage names themselves (unverified).
