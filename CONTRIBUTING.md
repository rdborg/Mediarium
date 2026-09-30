# Contributing to Mediarium

Thanks for thinking about helping. Bug reports, testing on your NAS, documentation fixes and code are all welcome.

Mediarium is licensed under AGPL-3.0. By submitting a change you agree that it is licensed under the same terms as the rest of the project.

## Ways to help without writing code

- **Report a bug** with the [bug report form](https://github.com/rdborg/Mediarium/issues/new/choose). Your version (Settings > System > About and credits), how you installed it and the **Copy for support** text (Settings > System > Server and backup > Help and support) make it much quicker to fix.
- **Test on your hardware.** The install guides for Synology, Unraid and QNAP need people with real devices. If a step or a menu name is different on yours, tell us.
- **Try a "coming soon" platform** from [PLATFORMS.md](docs/PLATFORMS.md) and report what happens.
- **Improve the docs.** Plain, friendly wording for people who are not developers is the goal.

## Before you start on code

- For anything bigger than a small fix, open an issue first so we can agree on the approach. It saves everyone rework.
- Check [docs/FEATURES.md](docs/FEATURES.md) for what exists and what is planned.
- Indexer problems: use the "Indexer problem" issue form. Torrent site definitions come from the community Cardigann definitions (the same ones Prowlarr uses), so a broken site often needs a fix there rather than here.

## Development setup

You need **Go** (the minimum version is the `go` line in [`go.mod`](go.mod)) and **Node.js 22 or newer** for the web interface.

```bash
# build the web interface (it is embedded into the Go program)
cd web && npm ci && npm run build && cd ..

# build and run the server
go build -o mediarium ./cmd/app
CONFIG_DIR=./config DOWNLOADS_DIR=./downloads MOVIES_DIR=./movies TV_DIR=./tv ./mediarium
```

Open `http://localhost:8264`. For live-reloading frontend work, run `API_TARGET=http://localhost:8264 npm run dev` in `web/` alongside the server (Vite otherwise sends API calls to port 8080).

**Or with Docker**, with nothing else installed:

```bash
cp .env.example .env     # optional: port, PUID/PGID, build keys
docker compose -f docker-compose.dev.yml up -d --build
```

### Before opening a pull request

```bash
gofmt -l .            # should print nothing
go vet ./...
go test ./...
cd web && npm run lint && npm test && npm run build
```

If you changed an HTTP route, an environment variable or a stored setting, regenerate the reference pages and commit them (CI checks they are current):

```bash
go run ./tools/docgen
```

### Building multi-arch images

The Dockerfile builds `linux/amd64` and `linux/arm64`, cross-compiling Go natively for each:

```bash
docker buildx create --use --name mediarium-builder   # once
docker buildx build --platform linux/amd64,linux/arm64 -t mediarium:test .
```

Add `--load` with a single `--platform` to get an image in your local `docker images`.

## Conventions

- **Formatting:** `gofmt`/`goimports`.
- **Errors:** wrap with context (`fmt.Errorf("doing x: %w", err)`); never swallow them silently.
- **Logging:** structured key/value logs. Never log API keys, passwords or VPN keys, not even at debug level.
- **Tests:** table-driven tests, especially for release-name parsing and matching, where edge cases live.
- **Settings:** container-level things (PUID, PGID, TZ, ports) are environment variables; everything else belongs in the app's Settings, not in new environment variables.

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/): `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`, `build:`. For example: `fix(downloads): keep seeding after a restart`.

## Pull requests

- One feature or fix per pull request.
- Add or update tests for the code you change.
- **Documentation is part of the change:** update the matching page in `docs/` and add a line to `CHANGELOG.md` under "Unreleased" in the same pull request. CI flags app changes without a docs change (maintainers can add the `no-docs-needed` label when nothing user-visible changed).
- Fill in the pull request template.

## Versions and releases

Mediarium uses [Semantic Versioning](https://semver.org/). The current version is in [`VERSION`](VERSION); contributors do not change it, maintainers do when they cut a release. See [docs/RELEASING.md](docs/RELEASING.md).

## Code of conduct

Everyone taking part is expected to follow the [Code of Conduct](CODE_OF_CONDUCT.md).
