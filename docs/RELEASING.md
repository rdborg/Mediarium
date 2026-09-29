# Releasing Mediarium

This page is for whoever publishes a new version. It assumes you have never cut a release before. Nothing here happens by itself: a release is published only when you push a version tag.

## The short version

1. Put the new number in `VERSION`.
2. Move the `[Unreleased]` notes in `CHANGELOG.md` under a new heading with that number and today's date.
3. Commit, tag the commit `v` + the number, push the tag.
4. GitHub builds everything and publishes it. Check the result.

The rest of the page explains each step.

## What the version numbers mean

Mediarium uses [Semantic Versioning](https://semver.org/): three numbers, **MAJOR.MINOR.PATCH**, for example `1.4.2`. The number tells people upgrading how careful they need to be.

| Bump | When | Mediarium examples | Example |
|---|---|---|---|
| **PATCH** (third number) | Bug fixes only. Nothing new to learn, nothing to change on the user's side. | A release name that was parsed wrongly; a crash when an indexer returns an empty page; a subtitle saved with the wrong language code; a typo in the interface. | `1.4.2` to `1.4.3` |
| **MINOR** (second number) | New features, added in a way that keeps everything that already works working. Existing settings, API calls and databases carry on untouched. Reset PATCH to 0. | A new notification service; a new quality preset; a new optional setting; a new API endpoint; a new page in the app; a database migration that the app runs by itself on start. | `1.4.3` to `1.5.0` |
| **MAJOR** (first number) | A breaking change: something that needs the user to act, or that breaks something they built on top of Mediarium. Reset MINOR and PATCH to 0. | Renaming or removing an environment variable or a volume path in the container; removing or changing the shape of an API endpoint that scripts may use; a database change that cannot be undone or needs a manual step; dropping support for a platform (for example linux/arm64). | `1.5.0` to `2.0.0` |

Rules of thumb:

- When in doubt between two, pick the bigger one. An unnecessary minor bump costs nothing; a breaking change hidden in a patch release breaks people's setups without warning.
- A release with both new features and bug fixes is a MINOR release. The biggest change decides.
- A MAJOR release needs an "Upgrading" note at the top of its changelog section that says exactly what users must do.
- The number is never reused. If `1.4.3` went out broken, the fix is `1.4.4`, not a second `1.4.3`.

## Where the number lives

The `VERSION` file at the root of the repository holds the current version on one line, without a leading `v`:

```
1.0.0
```

Everything else reads it:

- **Docker builds** (the `Dockerfile`) stamp it into the app. You can still override it with `--build-arg VERSION=...`, which is rarely needed.
- **The release workflow** (`.github/workflows/release.yml`) checks that the tag you pushed is exactly `v` + `VERSION`, and stops the whole release if not. It also uses `VERSION` for the file names and the image tags.
- **A test** (`cmd/app/version_test.go`) fails if `VERSION` is not a valid semantic version, so a typo is caught by CI before you ever tag.

A plain `go build` or `go run` without extra flags reports the version `dev`, so a developer's build never claims to be a release. The running version shows on the About page and at `GET /api/version`.

The git tag is the only place with a `v` in front: file `1.2.0`, tag `v1.2.0`, Docker image `1.2.0`.

## Cutting a release, step by step

The example releases version `1.1.0`. Replace it with your number.

1. **Make sure `main` is ready.** Everything you want in the release is merged, and the CI checks on `main` are green (build, tests, reference docs).

2. **Pick the number** using the table above. Look through the `[Unreleased]` section of `CHANGELOG.md`: anything under "Removed" or anything that makes users change their setup means MAJOR; anything under "Added" means at least MINOR; only "Fixed" means PATCH.

3. **Update `VERSION`** so it contains exactly `1.1.0` (one line).

4. **Update `CHANGELOG.md`.** Turn the unreleased notes into a release section and leave an empty `[Unreleased]` heading above it for the next round:

   ```markdown
   ## [Unreleased]

   ## [1.1.0] - 2026-10-15

   ### Added
   - ...everything that was under [Unreleased]...
   ```

   Read the notes once as a user would. They are what people see before upgrading.

5. **Commit** both files together:

   ```bash
   git add VERSION CHANGELOG.md
   git commit -m "chore: release 1.1.0"
   ```

   Push the commit to `main` (or merge it through a pull request) and wait for CI to pass on it.

6. **Tag that commit and push the tag.** The tag must be `v` + `VERSION`:

   ```bash
   git tag -a v1.1.0 -m "Mediarium 1.1.0"
   git push origin v1.1.0
   ```

   The tag has to point at a commit that is on GitHub. If you work in a separate private repository and publish to GitHub from it, tag the published commit, not your private one.

7. **Watch the release run** under the repository's Actions tab, workflow "Release". It:
   - checks that the tag matches `VERSION` (and stops everything if not);
   - builds the web interface once;
   - builds the app for Linux (amd64, arm64), Windows (amd64) and macOS (Intel and Apple silicon), each packed with the licence and install notes, plus a `sha256sums.txt`;
   - creates a GitHub Release named "Mediarium v1.1.0" with those files and notes generated from the commits;
   - builds the Docker image for linux/amd64 and linux/arm64 and pushes it to `ghcr.io` with the tags `1.1.0`, `1.1` and `latest`.

8. **Check the result.** Open the GitHub Release page and see the files are there. Pull the image and look at the version:

   ```bash
   docker pull ghcr.io/rdborg/mediarium:1.1.0
   docker run --rm -p 8264:8264 ghcr.io/rdborg/mediarium:1.1.0
   # then open http://localhost:8264/api/version
   ```

   You can paste the changelog section into the GitHub Release description so both say the same.

### If something goes wrong

- **"The tag is v1.1.0 but VERSION says 1.0.0."** You tagged a commit without the `VERSION` change. Delete the tag (`git tag -d v1.1.0` and `git push origin :refs/tags/v1.1.0`), commit the `VERSION` change, and tag again. Nothing was published, because the check runs first.
- **A build step failed after the check.** Fix the problem on `main`, then either delete and re-push the same tag (only if nothing was published yet) or release the next PATCH number.
- **A broken release went out.** Do not delete or replace it. Fix it and release the next PATCH number (`1.1.1`), with a "Fixed" note that says what was wrong.

## How users get the update

Nothing updates itself. Each release publishes three Docker tags:

| Tag | Moves when | For people who want |
|---|---|---|
| `1.1.0` | never | exactly this version, updating by hand |
| `1.1` | a PATCH release of 1.1 comes out (`1.1.1`, `1.1.2`...) | bug fixes automatically, features when they choose |
| `latest` | any release, including MAJOR ones | always the newest (reading the changelog first) |

To update, a user changes the tag in their compose file if needed, then runs `docker compose pull && docker compose up -d`. The database migrates by itself on start; they should back up `/config` first. See [INSTALL.md](./INSTALL.md#first-run-updating-backup-and-restore). People running the native binary download the new archive from the GitHub Release and replace the binary.

## Pre-releases

To let people try a version before it is final, release it with a suffix: `1.1.0-beta.1`, then `1.1.0-beta.2`, then `1.1.0-rc.1` ("release candidate"), and finally `1.1.0`. The steps are the same: `VERSION` holds `1.1.0-beta.1` and the tag is `v1.1.0-beta.1`.

A pre-release is handled differently in two ways:

- The GitHub Release is marked as a pre-release.
- The Docker image gets **only** its exact tag (`1.1.0-beta.1`). The `1.1` and `latest` tags are left alone, so nobody is moved onto a beta without asking for it.

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
- [ ] Release workflow finished: GitHub Release has all archives and `sha256sums.txt`
- [ ] Image `ghcr.io/rdborg/mediarium:X.Y.Z` pulls and `/api/version` reports `X.Y.Z`
- [ ] Stable release only: `X.Y` and `latest` point at the new image
```
