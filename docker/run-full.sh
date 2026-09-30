#!/bin/bash
# Starts the Cloudflare helper (FlareSolverr) and Mediarium side by side in the
# "full" image. The helper only listens on 127.0.0.1, so nothing outside the
# container can reach it. If either one stops, this script stops too, and Docker
# restarts the container.
set -u

# Chromium and its driver need a writable home folder, whichever user id the
# container runs as.
export HOME=/tmp/flaresolverr-home
mkdir -p "$HOME"

HOST=127.0.0.1 PORT=8191 /usr/local/bin/python -u /app/flaresolverr.py &
helper=$!

# Give the helper up to a minute to come up, so Mediarium does not start by
# reporting it as down.
for _ in $(seq 1 60); do
  if curl -fsS -o /dev/null --max-time 2 http://127.0.0.1:8191/ 2>/dev/null; then
    break
  fi
  if ! kill -0 "$helper" 2>/dev/null; then
    echo "The Cloudflare helper stopped while starting." >&2
    exit 1
  fi
  sleep 1
done

"$@" &
app=$!

asked=0
stop() {
  kill -TERM "$app" "$helper" 2>/dev/null
}
# docker stop: shut both down and exit cleanly (0), not as a failure.
trap 'asked=1; stop' TERM INT

wait -n "$app" "$helper"
code=$?
stop
wait
[ "$asked" = 1 ] && exit 0
exit "$code"
