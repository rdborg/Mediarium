#!/bin/sh
# Starts the "full" image and checks that both Mediarium and the built-in
# Cloudflare helper answer. Usage: docker/smoke-full.sh <image>
set -eu
image="${1:?usage: smoke-full.sh <image>}"
name="mediarium-full-smoke-$$"
cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; }
trap cleanup EXIT

docker run -d --name "$name" -p 18264:8264 -e PUID=1234 -e PGID=1234 "$image" >/dev/null

app=0
helper=0
for _ in $(seq 1 120); do
  [ "$app" = 1 ] || { curl -fsS -m 2 http://127.0.0.1:18264/api/version >/dev/null 2>&1 && app=1; }
  [ "$helper" = 1 ] || { docker exec "$name" curl -fsS -m 2 http://127.0.0.1:8191/ 2>/dev/null | grep -q "ready" && helper=1; }
  [ "$app" = 1 ] && [ "$helper" = 1 ] && break
  sleep 1
done

if [ "$app" != 1 ] || [ "$helper" != 1 ]; then
  echo "Smoke test failed (mediarium answering: $app, helper answering: $helper). Container log:" >&2
  docker logs "$name" >&2 || true
  exit 1
fi
echo "Smoke test passed: Mediarium and the Cloudflare helper both answer."
