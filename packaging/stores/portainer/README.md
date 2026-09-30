# Portainer app template

**Status: ready once the repository and image are public. Nothing to submit: Portainer templates are added by URL.**

Portainer has no central store to submit to. Anyone can add a template file by URL, and it then shows under **Templates → Application**.

## Files here

| File | What it is |
|---|---|
| `templates.json` | A Portainer v3 template file with one entry: a type 3 (Compose stack) template for Mediarium, with a form for the data folder, config folder, PUID, PGID, timezone, web port and the three library folders. |
| `docker-compose.yml` | The stack it deploys (fetched by Portainer from this repository: `packaging/stores/portainer/docker-compose.yml`). |

## One-line instruction for users

> In Portainer, go to **Settings → App Templates**, set the URL to
> `https://raw.githubusercontent.com/rdborg/Mediarium/main/packaging/stores/portainer/templates.json`,
> save, then open **Templates → Application → Mediarium**.

Note that this **replaces** Portainer's own template list with this file (only Mediarium is shown). To keep Portainer's list, use a **custom template** instead: **Templates → Custom → Add Custom Template**, choose **Repository**, URL `https://github.com/rdborg/Mediarium`, compose path `packaging/stores/portainer/docker-compose.yml`, and fill in the variables when deploying. Or simply paste the normal compose file under **Stacks → Add stack**.

## Requirements

- [ ] The Mediarium repository is public (Portainer downloads `templates.json`, the stack file and the logo from it).
- [ ] The image `ghcr.io/rdborg/mediarium:latest` is published and public.
- [ ] Test once: add the URL in a Portainer 2.x instance, deploy the template, open port 8264.

## After that

- Add the one-line instruction to `docs/INSTALL.md` (Using a Docker app) and mark Portainer as available in `docs/PLATFORMS.md`.
- Optional: ask community template lists (for example the widely used "selfhosted templates" collections on GitHub) to include the entry, by pull request to those repos.

## What only the maintainer can do

- Make the repository and image public, and test in Portainer.
