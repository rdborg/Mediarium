# Mediarium documentation

Start here. Pages are kept in step with the code: the reference pages are generated from it, and every change to the app updates the matching page in the same commit.

| I want to... | Read |
|---|---|
| Install it (Docker, Synology, Unraid, QNAP, TrueNAS, Portainer, CasaOS, Windows, macOS, Linux) | [INSTALL.md](./INSTALL.md) |
| See what it can and cannot do today, and what is planned | [FEATURES.md](./FEATURES.md) |
| Understand the responsible-use notice | [LEGAL.md](./LEGAL.md) |
| Look up an HTTP endpoint | [reference/api.md](./reference/api.md) |
| Look up an environment variable | [reference/environment.md](./reference/environment.md) |
| Look up a stored setting | [reference/settings-keys.md](./reference/settings-keys.md) |
| Platform notes | [synology.md](./synology.md), [unraid.md](./unraid.md), [qnap.md](./qnap.md), [linux.md](./linux.md) |

## Keeping the docs current

- Change a feature, setting or install step: update its page here and add a line to `CHANGELOG.md` in the same pull request. CI fails a pull request that changes the app without touching the docs (add the `no-docs-needed` label if nothing user-visible changed).
- Change a route, an environment variable or a settings key: run `go run ./tools/docgen` and commit the regenerated `docs/reference/*.md`. CI fails if they are out of date.
- The app links here from its About & Credits page. The address lives in one place, `web/src/docs.ts`, so it can move to a website later.
