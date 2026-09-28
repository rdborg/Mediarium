# syntax=docker/dockerfile:1

# ---- Frontend build stage ----
# Produces web/dist, which the Go build embeds via go:embed (web/embed.go)
# — the binary is not "single-binary" without this having run first.
# --platform=$BUILDPLATFORM: the frontend build output is plain JS/CSS, not
# platform-specific, so under `docker buildx build --platform
# linux/amd64,linux/arm64` this always runs natively on the builder instead
# of under arm64 QEMU emulation — same reasoning as the Go stage below.
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ---- Go build stage ----
# Matches go.mod's `go` directive — bumped repeatedly by dependencies
# added over the course of this project (torrent/VPN libraries); keep
# this in sync with go.mod's first line when that changes.
# --platform=$BUILDPLATFORM + explicit GOOS/GOARCH below: cross-compiles
# natively on the builder's own architecture for whichever target platform
# buildx is currently producing, rather than running the Go toolchain
# itself under QEMU emulation — the standard pattern for multi-platform
# builds of compiled languages (see docker/buildx's own docs), and
# meaningfully faster for a multi-arch build (linux/amd64 + linux/arm64,
# PRD §3) than emulating the whole build.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src

# Cache dependency downloads separately from source changes
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
# Overwrite the tracked placeholder web/dist/index.html with the real
# frontend build before go:embed picks it up.
COPY --from=frontend /src/web/dist ./web/dist

# Static binary, no CGO, targets set by buildx for linux/amd64 + linux/arm64
ARG TARGETOS
ARG TARGETARCH
# Release builds pass --build-arg VERSION=v0.1.0 (matching the git tag);
# local/dev builds get "dev" (PRD §8 — "version-pinned Docker tags,
# changelog per release, no forced auto-update"). Surfaced via GET
# /api/version and the About page.
ARG VERSION=dev
# App-wide API identifiers baked into an official build so users need not
# sign up for these services themselves. Leave them unset for a build from
# source: the app then asks each user for their own. They end up inside the
# binary (as with Radarr/Sonarr), so treat them as identifiers, not secrets.
#   docker build --build-arg TMDB_API_KEY=... --build-arg OPENSUBTITLES_API_KEY=... \
#                --build-arg TRAKT_CLIENT_ID=... .
ARG TMDB_API_KEY=
ARG OPENSUBTITLES_API_KEY=
ARG TRAKT_CLIENT_ID=
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION} -X main.defaultTMDBAPIKey=${TMDB_API_KEY} -X main.defaultOpenSubtitlesAPIKey=${OPENSUBTITLES_API_KEY} -X main.defaultTraktClientID=${TRAKT_CLIENT_ID}" -o /out/app ./cmd/app

# ---- Runtime stage ----
FROM alpine:3.20
ARG VERSION=dev
LABEL org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.source="https://github.com/rdborg/mediarium" \
      org.opencontainers.image.licenses="AGPL-3.0"
# par2cmdline (PAR2 verify/repair) and p7zip are the tools internal/organizer
# shells out to (PRD §4.4/§4.8). p7zip is only used for .7z archives: RAR and
# ZIP are unpacked natively in Go (Alpine's 7z has no RAR codec, so it could
# not be relied on for RAR anyway).
RUN apk add --no-cache ca-certificates tzdata su-exec par2cmdline p7zip

# Default UID/GID, overridable via PUID/PGID at runtime
ENV PUID=1000 \
    PGID=1000 \
    TZ=Etc/UTC \
    APP_PORT=8080

COPY --from=build /out/app /app/app
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

VOLUME ["/config", "/downloads", "/movies", "/tv"]
EXPOSE 8080

ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["/app/app"]
