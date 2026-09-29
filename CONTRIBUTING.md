# Contributing

Thanks for considering contributing. This project is AGPL-3.0 — by submitting a change, you agree it's licensed under the same terms as the rest of the project.

## Before you start

- For anything beyond a small fix, open an issue first to discuss the approach — saves everyone rework.
- Check `docs/FEATURES.md` for what exists and what is planned. Large features outside that list are best discussed in an issue first.
- New indexer requests: open an issue using the "New indexer request" template rather than a PR — indexer definitions are consumed from the community-maintained Cardigann format, so most of the time nothing needs to change in this repo at all.

## Development setup

```bash
go build ./cmd/app
./app
```

Go 1.23+, `gofmt`/`goimports` formatting expected. Run tests with `go test ./...` before opening a PR.

## Commit style

[Conventional Commits](https://www.conventionalcommits.org/): `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`.

## Pull requests

- Keep PRs focused — one feature or fix per PR.
- Include or update tests for the code you touch, especially anything in the parser/matching logic.
- Update the matching page in `docs/` (and `CHANGELOG.md`) in the same pull request. Documentation is part of the change.

## Versions and releases

Mediarium uses [Semantic Versioning](https://semver.org/). The current version is in the [`VERSION`](./VERSION) file; contributors do not change it, maintainers do when they cut a release. How version numbers are chosen and how a release is published: [`docs/RELEASING.md`](./docs/RELEASING.md).

## Code of conduct

See [`CODE_OF_CONDUCT.md`](./CODE_OF_CONDUCT.md).
