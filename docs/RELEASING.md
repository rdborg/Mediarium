# Releasing Mediarium

This page is for whoever publishes a new version. It assumes you have never cut a release before. Nothing here happens by itself: a release is published only when you push a version tag.

## The short version

1. Put the new number in `VERSION`.
2. Move the `[Unreleased]` notes in `CHANGELOG.md` under a new heading with that number and today's date.
3. Commit, tag the commit `v` + the number, push the tag.
4. GitHub builds everything and publishes it. Check the result.

The rest of the page explains each step.

## What the version numbers mean

Mediarium uses [Semantic Versioning](https://semver.org/): three numbers, **MAJOR.MINOR.PATCH**, for example `2.1.2`. The number tells people upgrading how careful they need to be.

| Bump | When | Mediarium examples | Example |
|---|---|---|---|
| **PATCH** (third number) | Bug fixes only. Nothing new to learn, nothing to change on the user's side. | A release name that was parsed wrongly; a crash when an indexer returns an empty page; a subtitle saved with the wrong language code; a typo in the interface. | `2.1.2` to `2.1.3` |
| **MINOR** (second number) | New features, added in a way that keeps everything that already works working. Existing settings, API calls and databases carry on untouched. Reset PATCH to 0. | A new notification service; a new quality preset; a new optional setting; a new API endpoint; a new page in the app; a database migration that the app runs by itself on start. | `2.1.3` to `2.2.0` |
| **MAJOR** (first number) | A breaking change: something that needs the user to act, or that breaks something they built on top of Mediarium. Reset MINOR and PATCH to 0. | Renaming or removing an environment variable or a volume path in the container; removing or changing the shape of an API endpoint that scripts may use; a database change that cannot be undone or needs a manual step; dropping support for a platform (for example linux/arm64). | `2.2.0` to `3.0.0` |

Rules of thumb:

- When in doubt between two, pick the bigger one. An unnecessary minor bump costs nothing; a breaking change hidden in a patch release breaks people's setups without warning.
- A release with both new features and bug fixes is a MINOR release. The biggest change decides.
- A MAJOR release needs an "Upgrading" note at the top of its changelog section that says exactly what users must do.
- The number is never reused. If `2.1.3` went out broken, the fix is `2.1.4`, not a second `2.1.3`.

## Where the number lives

The `VERSION` file at the root of the repository holds the current version on one line, without a leading `v`:

```
2.1.0
```

Everything else reads it:

- **Docker builds** (the `Dockerfile`) stamp it into the app when no `--build-arg VERSION=...` is given (the release workflow does pass it, taken from `VERSION`).
- **The release workflow** (`.github/workflows/release.yml`) checks that the tag you pushed is exactly `v` + `VERSION`, and stops the whole release if not. It also uses `VERSION` for the file names and the image tags.
- **A test** (`cmd/app/version_test.go`) fails if `VERSION` is not a valid semantic version, so a typo is caught by CI before you ever tag.

A plain `go build` or `go run` without extra flags reports the version `dev`, so a developer's build never claims to be a release. The running version shows on the About page and at `GET /api/version`.

The git tag is the only place with a `v` in front: file `2.1.0`, tag `v2.1.0`, Docker image `2.1.0`.

## Cutting a release, step by step

The example releases version `2.1.0`. Replace it with your number.

1. **Make sure `main` is ready.** Everything you want in the release is merged, and the CI checks on `main` are green (build, tests, reference docs).

2. **Pick the number** using the table above. Look through the `[Unreleased]` section of `CHANGELOG.md`: anything under "Removed" or anything that makes users change their setup means MAJOR; anything under "Added" means at least MINOR; only "Fixed" means PATCH.

3. **Update `VERSION`** so it contains exactly `2.1.0` (one line).

4. **Update `CHANGELOG.md`.** Turn the unreleased notes into a release section and leave an empty `[Unreleased]` heading above it for the next round:

   ```markdown
   ## [Unreleased]

   ## [2.1.0] - 2026-10-15

   ### Added
   - ...everything that was under [Unreleased]...
   ```

   Read the notes once as a user would. They are what people see before upgrading.

5. **Commit** both files together:

   ```bash
   git add VERSION CHANGELOG.md
   git commit -m "chore: release 2.1.0"
   ```

   Push the commit to `main` (or merge it through a pull request) and wait for CI to pass on it.

6. **Tag that commit and push the tag.** The tag must be `v` + `VERSION`:

   ```bash
   git tag -a v2.1.0 -m "Mediarium 2.1.0"
   git push origin v2.1.0
   ```

   The tag has to point at a commit that is on GitHub. If you work in a separate private repository and publish to GitHub from it, tag the published commit, not your private one.

7. **Watch the release run** under the repository's Actions tab, workflow "Release". It:
   - checks that the tag matches `VERSION` (and stops everything if not);
   - builds the web interface once;
   - builds the app for Linux (amd64, arm64), each packed with the licence, the install notes and the systemd files, plus a `sha256sums.txt`;
   - signs `sha256sums.txt` and adds `sha256sums.txt.sig`, if the `UPDATE_SIGNING_KEY` secret is set (see [Signing releases](#signing-releases));
   - creates a GitHub Release named "Mediarium v2.1.0" with those files and notes generated from the commits;
   - builds the Docker image for linux/amd64 and linux/arm64 and pushes it to `ghcr.io` with the tags `2.1.0`, `2.1` and `latest`;
   - builds the "full" image (with the Cloudflare helper), starts it as a test, and pushes it with the tags `2.1.0-full`, `2.1-full` and `latest-full` (see [The full image](#the-full-image-with-the-cloudflare-helper)).

8. **Check the result.** Open the GitHub Release page and see the files are there. Pull the image and look at the version:

   ```bash
   docker pull ghcr.io/rdborg/mediarium:2.1.0
   docker run --rm -p 8264:8264 ghcr.io/rdborg/mediarium:2.1.0
   # then open http://localhost:8264/api/version
   ```

   You can paste the changelog section into the GitHub Release description so both say the same.

### If something goes wrong

- **"The tag is v2.1.0 but VERSION says 2.0.0."** You tagged a commit without the `VERSION` change. Delete the tag (`git tag -d v2.1.0` and `git push origin :refs/tags/v2.1.0`), commit the `VERSION` change, and tag again. Nothing was published, because the check runs first.
- **A build step failed after the check.** Fix the problem on `main`, then either delete and re-push the same tag (only if nothing was published yet) or release the next PATCH number.
- **A broken release went out.** Do not delete or replace it. Fix it and release the next PATCH number (`2.1.1`), with a "Fixed" note that says what was wrong.

## How users get the update

Nothing updates unless the person switches on an overnight install. Each release publishes three Docker tags, and the same three again with `-full` added for the image with the Cloudflare helper:

| Tag | Moves when | For people who want |
|---|---|---|
| `2.1.0` (`2.1.0-full`) | never | exactly this version, updating by hand |
| `2.1` (`2.1-full`) | a PATCH release of 2.1 comes out (`2.1.1`, `2.1.2`...) | bug fixes automatically, features when they choose |
| `latest` (`latest-full`) | any release, including MAJOR ones | always the newest (reading the changelog first) |

To update, a user changes the tag in their compose file if needed, then runs `docker compose pull && docker compose up -d`. The database migrates by itself on start; they should back up `/config` first. See [INSTALL.md](./INSTALL.md#updating). (Native binaries are attached to each GitHub Release too, but they are not a supported install yet: see [PLATFORMS.md](./PLATFORMS.md).)

Administrators are also told about a new release by the app itself. Once a day it reads the latest releases of this repository from the GitHub API and shows the first ten lines of the release notes, so write them for users. A signed release also gets an **Update now** button in the Docker images. A release must therefore carry its signature: see [Signing releases](#signing-releases). A pre-release is only offered to installs that already run a pre-release.

## Signing releases

Mediarium's **Update now** button installs a release only if the release carries a signature. The release workflow signs the checksum list (`sha256sums.txt`) and attaches the signature (`sha256sums.txt.sig`). The button only exists in the Docker images, on Linux. The app has the matching public key built in (`updatePublicKey` in `cmd/app/main.go`), so a changed download, or a release somebody else published, is refused.

One-time setup:

1. Make a key pair: `go run ./tools/signsums -generate`. It prints the private **seed** and the **public key**.
2. Put the seed in the repository secret `UPDATE_SIGNING_KEY` (GitHub: Settings, Secrets and variables, Actions). Keep a copy in your password manager. It is never written to a file in the repository and the workflow only passes it to the signing step's environment.
3. Put the public key in `updatePublicKey` in `cmd/app/main.go` (a fork does the same, or builds with `-ldflags "-X main.updatePublicKey=..."`).

What the workflow does: after it creates `sha256sums.txt` it runs `tools/signsums`, which refuses to sign when the key does not match the public key in `cmd/app/main.go` (so a wrong secret fails the release instead of publishing signatures nobody can verify). If `UPDATE_SIGNING_KEY` is not set, the workflow prints a warning and publishes the release **unsigned**: people can still update the normal way, the button just is not offered.

Losing or replacing the key: releases signed with the old key stop verifying against apps that carry the new public key, and the other way round. To rotate, publish a release whose app carries the new public key, signed by the old key (so existing installs accept it), then sign later releases with the new one. If the private key leaks, remove the secret, make a new pair and release a new version by hand, telling people to update the normal way.

### Protecting the signing key

Whoever can get the signing key can publish an update that every Mediarium install with **Update now** will run as code. So the key is guarded on the GitHub side too. Do this once, before the first release:

1. **Settings > Environments > New environment**, named `release` (the release job of the workflow already uses it). Move the `UPDATE_SIGNING_KEY` secret there from the repository secrets, so it is only handed to that job.
2. In the environment, under **Deployment branches and tags**, allow only tags that match `v*`, and add **Required reviewers** (you) so nothing signs without an approval click.
3. **Settings > Rules > Rulesets**: protect the tag pattern `v*` so that only you can create, move or delete such tags, and protect `main` (pull request required, no force pushes).
4. Give collaborators the *Triage* or *Write* role only when you must; anyone who can change the workflow files on a tag you approve can read the key.
5. Leave "Allow GitHub Actions to create and approve pull requests" off, and keep the default workflow permissions on *Read repository contents*.

The workflows also pin every action to a full commit (with the version as a comment) and Dependabot updates those pins, so a moved tag of a third-party action cannot change what runs.

Check a release by hand: `sha256sum -c sha256sums.txt` verifies the archives against the list. The signature is base64 of an ed25519 signature over the exact bytes of `sha256sums.txt`.

## Pushing a test build

`tools/push-update.sh` builds the committed `HEAD` in Docker (the repository's own `Dockerfile`, so the interface is included), stamps the version and the app-wide keys from the git-ignored `.env` (`TMDB_API_KEY`, `OPENSUBTITLES_API_KEY`, `TRAKT_CLIENT_ID`; they are passed to Docker through the environment and never printed), computes the SHA-256 and uploads it to an install that has **Allow updates pushed through the API** switched on. It is for the maintainer, for testing a build on a real install before a release, and for installs that cannot rebuild their container.

```bash
MEDIARIUM_API_KEY=<administrator API key> tools/push-update.sh http://192.168.1.10:8264
tools/push-update.sh --arch arm64 --version 2.1.1 --wait https://mediarium.example.com
tools/push-update.sh --no-upload          # only build; writes ./mediarium-<version>-<arch>
```

The API key comes from the environment (or a prompt), is never stored in a file, and is given to `curl` on its standard input so it does not show in a process list. `--force` lets the install accept the same or an older version. Only committed work is built.

The install must have been started by the Docker image (its entrypoint starts the pushed program), on Linux. See [INSTALL.md](./INSTALL.md#updating-without-rebuilding-the-container) for what the install checks and how to go back.

## Pre-releases

To let people try a version before it is final, release it with a suffix: `2.1.0-beta.1`, then `2.1.0-beta.2`, then `2.1.0-rc.1` ("release candidate"), and finally `2.1.0`. The steps are the same: `VERSION` holds `2.1.0-beta.1` and the tag is `v2.1.0-beta.1`.

A pre-release is handled differently in two ways:

- The GitHub Release is marked as a pre-release.
- The Docker images get **only** their exact tags (`2.1.0-beta.1` and `2.1.0-beta.1-full`). The `2.1`, `latest` and `-full` short tags are left alone, so nobody is moved onto a beta without asking for it.

In `CHANGELOG.md`, you can give a pre-release its own section or leave the notes under `[Unreleased]` until the final release. Either is fine, as long as the final release's section lists everything.

## Checklist

Copy this into the release pull request or an issue:

```markdown
- [ ] `main` has everything for this release and CI is green
- [ ] Version picked: PATCH (fixes only) / MINOR (new features) / MAJOR (users must act)
- [ ] `VERSION` holds the new number, no leading "v"
- [ ] `CHANGELOG.md`: `[Unreleased]` notes moved under `## [X.Y.Z] - YYYY-MM-DD`, empty `[Unreleased]` left above
- [ ] MAJOR only: an "Upgrading" note says exactly what users must do
- [ ] Release commit pushed to `main`, CI green on it
- [ ] Tag `vX.Y.Z` created on that commit and pushed
- [ ] Release workflow finished: GitHub Release has all archives and `sha256sums.txt` and `sha256sums.txt.sig` (no warning about a missing signing key)
- [ ] Image `ghcr.io/rdborg/mediarium:X.Y.Z` pulls and `/api/version` reports `X.Y.Z`
- [ ] Stable release only: `X.Y` and `latest` point at the new image, and `X.Y.Z-full`, `X.Y-full` and `latest-full` exist too
```

## The full image (with the Cloudflare helper)

Every release also publishes `ghcr.io/rdborg/mediarium:<version>-full`, `:<major.minor>-full` and `:latest-full` (a pre-release only gets its exact `-full` tag). It comes from the `full` stage of the `Dockerfile` and is built on the official FlareSolverr image.

- The FlareSolverr version is pinned in the `FLARESOLVERR_IMAGE` line at the top of the `Dockerfile`, by version and digest. To update it, pull the new image, copy its digest into that line, run `docker/smoke-full.sh` on a local build (`docker build --target full -t mediarium-full-test .`), and put the change in the changelog. Cloudflare changes its checks all the time, so check the FlareSolverr release notes first.
- The release job builds the image for amd64 first, runs `docker/smoke-full.sh` (both Mediarium and the helper must answer) and only then pushes amd64 and arm64.
- `.github/workflows/rebuild-full.yml` rebuilds the latest release's `-full` image every Monday with no cache, so browser and system security fixes get picked up without a new Mediarium version.
- The plain image (no `-full`) is unchanged and stays the default in the compose files.
