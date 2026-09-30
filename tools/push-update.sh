#!/usr/bin/env bash
# Build Mediarium from the committed HEAD and push it to a running install.
#
# For the maintainer, and for testing. It needs Docker and curl, and the target
# install must have "Allow updates pushed through the API" switched on under
# Settings > System (see docs/RELEASING.md and docs/INSTALL.md).
#
#   MEDIARIUM_API_KEY=<admin API key> tools/push-update.sh https://mediarium.example.com
#
# What it does:
#   1. Builds the linux program from the committed HEAD with the repository's own
#      Dockerfile (frontend included), stamping the version and the app-wide
#      service keys from the git-ignored .env (TMDB_API_KEY, OPENSUBTITLES_API_KEY,
#      TRAKT_CLIENT_ID). The keys go to Docker through the environment and are never
#      printed.
#   2. Works out the file's SHA-256.
#   3. Uploads it to POST <url>/api/system/update with that checksum in the
#      X-Update-SHA256 header. The install checks the checksum and that the file
#      is a Mediarium program for its system and newer than what it runs, then
#      restarts into it.
#
# The API key is read from the MEDIARIUM_API_KEY environment variable (or typed
# at a prompt). It is never written to a file, and it is given to curl on its
# standard input, not on the command line, so it does not show in a process list.
# Only committed work is built: commit first.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: tools/push-update.sh [options] <url>

  <url>                 where Mediarium is, for example http://192.168.1.10:8264
  -a, --arch ARCH       amd64 (default) or arm64: the system of the install
  -v, --version X.Y.Z   version to stamp (default: the VERSION file at HEAD)
  -f, --force           let the install accept the same or an older version
  -n, --no-upload       only build; write the program to ./mediarium-<version>-<arch>
      --wait            after the upload, wait until the install reports the new version
  -h, --help            this text

The API key comes from MEDIARIUM_API_KEY (or is asked for). Build keys come from .env.
EOF
}

arch=amd64
version=""
force=""
upload=1
wait_for=0
url=""

while [ $# -gt 0 ]; do
  case "$1" in
    -a|--arch) arch="${2:-}"; shift 2 ;;
    -v|--version) version="${2:-}"; shift 2 ;;
    -f|--force) force="?force=true"; shift ;;
    -n|--no-upload) upload=0; shift ;;
    --wait) wait_for=1; shift ;;
    -h|--help) usage; exit 0 ;;
    -*) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
    *) url="$1"; shift ;;
  esac
done

case "$arch" in amd64|arm64) ;; *) echo "The arch must be amd64 or arm64." >&2; exit 2 ;; esac
if [ "$upload" = 1 ] && [ -z "$url" ]; then echo "Give the address of your Mediarium." >&2; usage >&2; exit 2; fi
url="${url%/}"
if [ "$upload" = 1 ]; then
  case "$url" in http://*|https://*) ;; *) echo "The address must start with http:// or https://" >&2; exit 2 ;; esac
fi

cd "$(git rev-parse --show-toplevel)"
if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
  echo "Note: you have uncommitted changes. Only the committed HEAD is built; they are not included." >&2
fi
if [ -z "$version" ]; then
  version="$(git show HEAD:VERSION | tr -d ' \r\n')"
fi
case "$version" in
  [0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "The version \"$version\" does not look like 1.2.3." >&2; exit 2 ;;
esac

# --- The keys for the build, from the git-ignored .env, never printed. -------
# The file is read line by line, not run as a script.
read_env_value() {
  local name="$1" line value
  [ -f .env ] || return 0
  line="$(grep -E "^[[:space:]]*${name}=" .env | tail -n 1 || true)"
  [ -n "$line" ] || return 0
  value="${line#*=}"
  value="${value%$'\r'}"
  value="${value#\"}"; value="${value%\"}"
  value="${value#\'}"; value="${value%\'}"
  printf '%s' "$value"
}
for name in TMDB_API_KEY OPENSUBTITLES_API_KEY TRAKT_CLIENT_ID; do
  export "$name=$(read_env_value "$name")"
done
have_keys=()
for name in TMDB_API_KEY OPENSUBTITLES_API_KEY TRAKT_CLIENT_ID; do
  [ -n "${!name}" ] && have_keys+=("$name")
done
echo "Building Mediarium $version for linux/$arch from $(git rev-parse --short HEAD)."
if [ ${#have_keys[@]} -eq 0 ]; then
  echo "No app-wide keys found in .env: this build will ask for its own keys, like a build from source." >&2
else
  echo "Built-in keys taken from .env: ${have_keys[*]} (values not shown)."
fi

# --- Build with the repository's Dockerfile, then take the program out. ------
tag="mediarium-push-build:$$"
container=""
tmp="$(mktemp -d)"
cleanup() {
  [ -n "$container" ] && docker rm -f "$container" >/dev/null 2>&1 || true
  docker rmi -f "$tag" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT

# --build-arg NAME (no value) makes Docker take the value from this
# environment, so no key appears on a command line.
git archive HEAD | docker build --quiet --target build --platform "linux/$arch" \
  --build-arg "VERSION=$version" \
  --build-arg TMDB_API_KEY --build-arg OPENSUBTITLES_API_KEY --build-arg TRAKT_CLIENT_ID \
  -t "$tag" - >/dev/null
container="$(docker create "$tag")"
# Streamed as a tar to standard output, so no host path is handed to Docker (this
# also works from Git Bash on Windows).
MSYS_NO_PATHCONV=1 docker cp "$container:/out/app" - | tar -xO -f - > "$tmp/app"
chmod 0755 "$tmp/app"

sum="$(sha256sum "$tmp/app" | cut -d ' ' -f 1)"
size="$(wc -c < "$tmp/app" | tr -d ' ')"
echo "Built: $size bytes, SHA-256 $sum"

if [ "$upload" = 0 ]; then
  out="mediarium-$version-$arch"
  cp "$tmp/app" "$out"
  echo "Wrote ./$out (not uploaded)."
  exit 0
fi

# --- The API key: from the environment, or typed (not echoed). -----------------
key="${MEDIARIUM_API_KEY:-}"
if [ -z "$key" ]; then
  if [ -t 0 ]; then
    read -r -s -p "Admin API key for $url: " key
    echo
  fi
fi
if [ -z "$key" ]; then
  echo "No API key. Set MEDIARIUM_API_KEY (create one on the Profile page of an administrator account)." >&2
  exit 2
fi

echo "Uploading to $url ..."
response="$tmp/response"
# The key goes to curl on standard input (-H @-), so it is not in the process list.
status="$(printf 'X-API-Key: %s\n' "$key" | curl -sS -o "$response" -w '%{http_code}' -X POST \
  -H @- \
  -H "X-Update-SHA256: $sum" \
  -H 'Content-Type: application/octet-stream' \
  --data-binary @"$tmp/app" \
  "$url/api/system/update$force")" || { echo "Could not reach $url." >&2; exit 1; }
unset key

echo "Answer ($status): $(tr -d '\r' < "$response" | head -c 600)"
case "$status" in 202) ;; *) echo "The install did not accept the update." >&2; exit 1 ;; esac

if [ "$wait_for" = 1 ]; then
  echo "Waiting for the install to restart into $version ..."
  for _ in $(seq 1 60); do
    sleep 2
    if curl -fsS --max-time 4 "$url/api/version" 2>/dev/null | grep -q "\"$version\""; then
      echo "Running $version."
      exit 0
    fi
  done
  echo "It did not report $version within two minutes. Check the container log." >&2
  exit 1
fi
echo "It restarts in a moment. Check the version at $url/api/version."
