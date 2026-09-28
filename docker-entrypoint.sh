#!/bin/sh
set -e

# Match the app user to PUID/PGID so files written to the mounted folders have
# the ownership your NAS/host expects.
addgroup -g "${PGID}" -S appgroup 2>/dev/null || true
adduser -u "${PUID}" -S -G appgroup appuser 2>/dev/null || true

# /config is Mediarium's own private state, so it is safe to (re)own it.
mkdir -p /config
chown -R "${PUID}:${PGID}" /config 2>/dev/null || true

# Your media folders are NEVER chowned or modified recursively here: they hold
# your existing files. Only create the mount point if it is missing entirely
# (an unmapped folder) and tell you if Mediarium cannot write to it.
for dir in /downloads /movies /tv; do
  [ -d "$dir" ] || mkdir -p "$dir"
  if ! su-exec "${PUID}:${PGID}" test -w "$dir"; then
    if [ -z "$(ls -A "$dir" 2>/dev/null)" ] && [ "$(stat -c %u "$dir")" = "0" ]; then
      # Empty and root-owned: nothing from your host is mapped here (Docker made
      # a blank volume). Own just this empty folder, not recursively.
      chown "${PUID}:${PGID}" "$dir"
      echo "NOTE: $dir is not mapped to a folder on your host, so anything saved there is lost when the container is recreated. Map it in your compose file." >&2
    else
      echo "WARNING: $dir is not writable by uid ${PUID}/gid ${PGID}. Fix the folder permissions on the host or change PUID/PGID; Mediarium will not touch it." >&2
    fi
  fi
done

exec su-exec "${PUID}:${PGID}" "$@"
