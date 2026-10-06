# Unraid Community Applications: submission kit

**Status: ready to submit once the requirements below are met. Not submitted.**

Community Applications (CA) lists apps from template repositories on GitHub. Submissions go through the CA portal at [ca.unraid.net/submit](https://ca.unraid.net/submit) (the old forum-post process has been replaced).

## Files here

| File | What it is |
|---|---|
| `ca_profile.xml` | Describes the template repository (required at the **root** of the repo, with a non-empty `<Profile>`). |
| `templates/mediarium.xml` | The Mediarium container template: image, ports 8264 and 58264 (TCP+UDP), `/config` and one `/data` share, `DOWNLOADS_DIR`/`MOVIES_DIR`/`TV_DIR` (plus optional, empty `MUSIC_DIR`/`EBOOKS_DIR`/`AUDIOBOOKS_DIR` in Advanced View), `PUID=99`, `PGID=100`, `TZ`. |

## Requirements

- [ ] The Mediarium GitHub repository is **public**.
- [ ] The image `ghcr.io/rdborg/mediarium` is **published** (tag `v2.1.0` pushed, release workflow green) and its GitHub package visibility is **Public** (GitHub → your profile → Packages → mediarium → Package settings → Change visibility). Check from a logged-out machine: `docker pull ghcr.io/rdborg/mediarium:latest`.
- [ ] The image is multi-arch; Unraid only needs `linux/amd64`.
- [ ] The icon URL in both files opens in a private browser window: `https://raw.githubusercontent.com/rdborg/Mediarium/main/branding/mediarium-icon-512.png`.
- [ ] A **template repository**: CA wants `ca_profile.xml` at the repository root, so use a small separate public repo rather than the Mediarium repo. The files here assume **`github.com/rdborg/unraid-templates`**; if you choose another name, change `<TemplateURL>` in `templates/mediarium.xml`.
- [ ] That repo has an OSI-approved `LICENSE` at its root (copy Mediarium's AGPL-3.0 `LICENSE`, or use MIT for the templates).

## Steps

1. **Create the template repo.** New public GitHub repo `rdborg/unraid-templates` containing:
   ```
   ca_profile.xml
   LICENSE
   README.md            (one line: "Unraid templates for Mediarium")
   templates/mediarium.xml
   ```
   Copy `ca_profile.xml` and `templates/mediarium.xml` from this folder.
2. **Test it on an Unraid server** before submitting. Copy `templates/mediarium.xml` to `/boot/config/plugins/dockerMan/templates-user/my-mediarium.xml` (the `flash` share → `config/plugins/dockerMan/templates-user/`), then Docker tab → **Add Container** → **Template** → `mediarium`. Check: it starts, the WebUI link works, the wizard shows `/data/downloads/mediarium`, `/data/media/movies` and `/data/media/tv` as writable, the icon shows.
3. **Optional, a support thread.** CA accepts the GitHub issues link as `<Support>`. Many users look for a thread in the [Docker Containers](https://forums.unraid.net/forum/47-docker-containers/) forum; if you post one ("[Support] Mediarium"), put its URL in `<Support>` and add `<Forum>` to `ca_profile.xml`.
4. **Submit.** Go to [ca.unraid.net/submit/new](https://ca.unraid.net/submit/new), sign in, enter `https://github.com/rdborg/unraid-templates`, run **Validate / Scan**, fix anything it reports (it checks the XML, `ca_profile.xml`, duplicates and shows a preview), then submit for review.
5. **After approval**, it appears in the Apps tab within a day or so. Update `docs/unraid.md` and `docs/PLATFORMS.md`: replace "coming soon" with "Apps tab → search Mediarium".

## Things to verify during submission

- `<Category>`: `Downloaders: MediaApp:Video` follows the style of other download managers' templates; the portal's validator is the authority.
- Whether Unraid adds `TZ` by itself. It is in the template as an advanced variable either way, which is harmless.

## What only the maintainer can do

- Make the repo and the GHCR package public, push the `v2.1.0` tag.
- Create the `unraid-templates` repo, sign in to the CA portal and submit.
- Optionally create the forum support thread.
