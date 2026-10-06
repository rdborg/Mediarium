#!/bin/sh
set -e

# ---- Installed updates ---------------------------------------------------------
# Mediarium can install a newer program file into <config>/update/app (from the
# Settings > System page, or pushed through the API; see docs/INSTALL.md). Before
# starting the app this script decides which program to run: the one inside the
# image, or the installed one. The installed one is used only when ALL of these hold:
#   - it is an executable file, and its checksum matches app.sha256 (when present);
#   - it answers --version-check as a Mediarium program for this same system;
#   - its version is not older than the image's own (the image's program decides);
#   - it has not stopped right after starting three times in a row.
# Otherwise it is ignored (one line says why) and the image's program starts, so a
# bad update can never stop Mediarium from starting. Set
# MEDIARIUM_SKIP_INSTALLED_UPDATE=1 to ignore an installed update by hand.
# The installed program is only ever run as the app user (PUID/PGID), never as root.
UPDATE_DIR="${CONFIG_DIR:-/config}/update"
IMAGE_BIN="${MEDIARIUM_IMAGE_BIN:-/app/app}"
APP_TO_RUN="$IMAGE_BIN"

say() { echo "Mediarium: $*" >&2; }

# run_as is redefined below when this script starts as root.
run_as() { "$@"; }

# probe PROGRAM prints the first line PROGRAM says for --version-check
# ("mediarium 1.3.0 linux/amd64"), or nothing when it fails or takes over 10 s.
probe() {
  if command -v timeout >/dev/null 2>&1; then
    run_as timeout 10 "$1" --version-check 2>/dev/null </dev/null | head -n 1
  else
    run_as "$1" --version-check 2>/dev/null </dev/null | head -n 1
  fi
}

# choose_app sets APP_TO_RUN and exports MEDIARIUM_IMAGE_VERSION.
choose_app() {
  APP_TO_RUN="$IMAGE_BIN"
  [ -x "$IMAGE_BIN" ] || return 0
  image_line="$(probe "$IMAGE_BIN")"
  set -f
  set -- $image_line
  set +f
  [ "$1" = "mediarium" ] && [ -n "$2" ] && [ -n "$3" ] || return 0
  img_ver="$2"
  img_plat="$3"
  # Tells the app it was started by this script, and which version the image holds.
  MEDIARIUM_IMAGE_VERSION="$img_ver"
  export MEDIARIUM_IMAGE_VERSION

  if [ -n "${MEDIARIUM_SKIP_INSTALLED_UPDATE:-}" ]; then
    say "ignoring the installed update because MEDIARIUM_SKIP_INSTALLED_UPDATE is set."
    return 0
  fi
  pushed="$UPDATE_DIR/app"
  [ -e "$pushed" ] || return 0
  if [ ! -f "$pushed" ] || [ ! -x "$pushed" ]; then
    say "ignoring the installed update: it is not an executable file."
    return 0
  fi
  if [ -f "$UPDATE_DIR/app.sha256" ]; then
    want="$(head -n 1 "$UPDATE_DIR/app.sha256" | cut -d ' ' -f 1)"
    have="$(sha256sum "$pushed" 2>/dev/null | cut -d ' ' -f 1)"
    if [ -z "$want" ] || [ "$want" != "$have" ]; then
      say "ignoring the installed update: the file does not match its checksum."
      return 0
    fi
  fi

  count="$(cat "$UPDATE_DIR/boot-count" 2>/dev/null || echo 0)"
  case "$count" in ''|*[!0-9]*) count=0 ;; esac
  if [ "$count" -ge 3 ]; then
    say "the installed update stopped 3 times in a row right after starting. It is put aside as app.failed and the version in the image ($img_ver) starts instead."
    # As the app user, not as root: the update folder is the app's own, and
    # what is in it must never make root touch a file somewhere else.
    run_as mv -f "$pushed" "$UPDATE_DIR/app.failed" 2>/dev/null || true
    run_as mv -f "$UPDATE_DIR/VERSION" "$UPDATE_DIR/VERSION.failed" 2>/dev/null || true
    run_as rm -f "$UPDATE_DIR/boot-count" "$UPDATE_DIR/app.sha256" 2>/dev/null || true
    return 0
  fi

  line="$(probe "$pushed")"
  set -f
  set -- $line
  set +f
  if [ "$1" != "mediarium" ] || [ -z "$2" ] || [ "$3" != "$img_plat" ]; then
    say "ignoring the installed update: it does not answer as a Mediarium program for this system."
    return 0
  fi
  pver="$2"
  if ! run_as "$IMAGE_BIN" --accepts-update "$pver" >/dev/null 2>&1; then
    say "ignoring the installed update ($pver): it is older than the version in the image ($img_ver), or its version is not a version number."
    return 0
  fi

  # Count this start. The app clears the counter once it has been running and
  # answering for 20 seconds; three starts without that put the update aside.
  # Written by the app user too: a link left in the update folder could
  # otherwise make root write into a file elsewhere.
  if ! run_as sh -c 'echo "$1" > "$2"' sh "$((count + 1))" "$UPDATE_DIR/boot-count" 2>/dev/null; then
    say "ignoring the installed update: the update folder can't be written, so a bad update could not be undone."
    return 0
  fi
  APP_TO_RUN="$pushed"
  say "starting the installed update $pver (the image has $img_ver)."
}

# Started as a non-root user already (for example "user: 1000:1000" in
# compose, Kubernetes, or an app platform that sets the user itself): PUID and
# PGID do not apply, and nothing can be chowned. Create missing folders if
# possible, say which ones cannot be written, and start the app as this user.
if [ "$(id -u)" != "0" ]; then
  for dir in "${CONFIG_DIR:-/config}" "${DOWNLOADS_DIR:-/downloads}" "${MOVIES_DIR:-/movies}" "${TV_DIR:-/tv}" ${MUSIC_DIR:+"$MUSIC_DIR"} ${EBOOKS_DIR:+"$EBOOKS_DIR"} ${AUDIOBOOKS_DIR:+"$AUDIOBOOKS_DIR"}; do
    [ -d "$dir" ] || mkdir -p "$dir" 2>/dev/null || true
    if [ ! -w "$dir" ]; then
      echo "WARNING: $dir is not writable by uid $(id -u)/gid $(id -g). Fix the folder permissions on the host." >&2
    fi
  done
  choose_app
  if [ "$APP_TO_RUN" != "$IMAGE_BIN" ]; then
    n=$#
    while [ "$n" -gt 0 ]; do
      a="$1"; shift
      [ "$a" = "$IMAGE_BIN" ] && a="$APP_TO_RUN"
      set -- "$@" "$a"
      n=$((n - 1))
    done
  fi
  exec "$@"
fi

# run_as runs a command as PUID:PGID. The small Alpine image has su-exec; the
# "full" image is Debian-based and uses setpriv (numeric ids need no user entry).
if command -v su-exec >/dev/null 2>&1; then
  run_as() { su-exec "${PUID}:${PGID}" "$@"; }
  # Match the app user to PUID/PGID so files written to the mounted folders
  # have the ownership your NAS/host expects.
  addgroup -g "${PGID}" -S appgroup 2>/dev/null || true
  adduser -u "${PUID}" -S -G appgroup appuser 2>/dev/null || true
else
  run_as() { setpriv --reuid="${PUID}" --regid="${PGID}" --clear-groups "$@"; }
fi

# /config is Mediarium's own private state, so it is safe to (re)own it.
mkdir -p /config
# -h: a link inside /config is changed itself, never what it points at.
chown -hR "${PUID}:${PGID}" /config 2>/dev/null || true

# is_mapped DIR: true when DIR, or a folder above it, is mounted from the host
# (for example /data/movies inside a /data mount).
is_mapped() {
  d="$1"
  while [ -n "$d" ] && [ "$d" != "/" ]; do
    if awk -v p="$d" '$5 == p { found = 1 } END { exit !found }' /proc/self/mountinfo; then
      return 0
    fi
    d="$(dirname "$d")"
  done
  return 1
}

# is_fresh DIR: an empty folder owned by root, which is what Docker creates when
# the host folder you mapped did not exist yet. Nothing of yours is in it.
is_fresh() {
  [ -d "$1" ] && [ -z "$(ls -A "$1" 2>/dev/null)" ] && [ "$(stat -c %u "$1")" = "0" ]
}

# Your media folders are NEVER chowned or modified recursively here: they hold
# your existing files. A missing folder is created as the app user, a brand-new
# empty folder Docker made for you is handed to the app user (that one folder
# only), and anything else that is not writable is reported and left alone.
# The folders checked are the ones the app uses: DOWNLOADS_DIR, MOVIES_DIR and
# TV_DIR (by default /downloads, /movies and /tv, or /data/downloads,
# /data/movies and /data/tv with one /data mount). MUSIC_DIR, EBOOKS_DIR and
# AUDIOBOOKS_DIR are checked the same way, but only when you set them.
for dir in "${DOWNLOADS_DIR:-/downloads}" "${MOVIES_DIR:-/movies}" "${TV_DIR:-/tv}" ${MUSIC_DIR:+"$MUSIC_DIR"} ${EBOOKS_DIR:+"$EBOOKS_DIR"} ${AUDIOBOOKS_DIR:+"$AUDIOBOOKS_DIR"}; do
  if [ ! -d "$dir" ]; then
    parent="$(dirname "$dir")"
    if [ "$parent" = "/" ]; then
      # A top-level folder inside the container itself (nothing mapped there).
      mkdir -p "$dir" && chown "${PUID}:${PGID}" "$dir"
    else
      if [ "${PUID}" != "0" ] && is_fresh "$parent"; then
        chown "${PUID}:${PGID}" "$parent"
      fi
      run_as mkdir -p "$dir" 2>/dev/null || true
    fi
  fi

  if [ ! -d "$dir" ]; then
    echo "WARNING: could not create $dir as uid ${PUID}/gid ${PGID}. Make sure that user can write to $(dirname "$dir") on the host, or change PUID/PGID." >&2
    continue
  fi

  if ! run_as test -w "$dir"; then
    if is_fresh "$dir"; then
      chown "${PUID}:${PGID}" "$dir"
    else
      echo "WARNING: $dir is not writable by uid ${PUID}/gid ${PGID}. Fix the folder permissions on the host or change PUID/PGID; Mediarium will not touch it." >&2
      continue
    fi
  fi

  if ! is_mapped "$dir"; then
    echo "NOTE: $dir is not mapped to a folder on your host, so anything saved there is lost when the container is recreated. Map it in your compose file." >&2
  fi
done

choose_app
if [ "$APP_TO_RUN" != "$IMAGE_BIN" ]; then
  n=$#
  while [ "$n" -gt 0 ]; do
    a="$1"; shift
    [ "$a" = "$IMAGE_BIN" ] && a="$APP_TO_RUN"
    set -- "$@" "$a"
    n=$((n - 1))
  done
fi

if command -v su-exec >/dev/null 2>&1; then
  exec su-exec "${PUID}:${PGID}" "$@"
fi
exec setpriv --reuid="${PUID}" --regid="${PGID}" --clear-groups "$@"

