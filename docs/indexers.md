# Indexers

Indexers are the search engines Mediarium asks for releases. It supports three kinds, and they can be mixed freely: every search goes to all enabled indexers at once and the results come back as one list.

| Kind | What it is | You need |
|---|---|---|
| **Newznab** (Usenet) | The API almost every Usenet indexer offers. Results are NZB files, downloaded by the built-in Usenet client. | The indexer's address and your API key, plus a Usenet provider account (Settings > Downloads). |
| **Torznab** (torrents) | The same API for torrent sites, offered by some trackers and by Prowlarr or Jackett if you already run them. | The Torznab address and API key. |
| **Sites from the definition list** (torrents) | Torrent sites without an API, driven by a community-maintained definition that describes how to search the site and read its pages. This is the same engine idea Prowlarr and Jackett use ("Cardigann"), built into Mediarium, so no extra container is needed. | Nothing for public sites; your login or browser cookie for private ones. |

## Usenet or torrent?

- **Usenet**: you pay a Usenet provider for access and use an indexer to find NZBs. Downloads are a direct, encrypted connection to your provider; nobody else sees what you fetch.
- **Torrents**: free, but you download from (and upload to) other people. Everyone in a swarm can see your IP address. Mediarium's built-in VPN (Settings > VPN) can hide it. Torrents can be switched off entirely in Settings; torrent indexers are then skipped.

## Adding a Newznab or Torznab indexer

Settings > Indexers > Add: choose Usenet or torrent, paste the indexer's address and API key, test, save. Each saved indexer has **Test**, **Edit**, **Disable** and **Remove**. Edit changes the name, address and API key; leaving the API key blank keeps the saved one. A failed test says what went wrong in plain words (address not found, connection refused, no answer in time, certificate problem, wrong API key, IP address not allowed). The key is never sent back to the browser: the indexer list only says whether one is saved (`hasApiKey`).

## Adding a site from the definition list

Settings > Indexers > Add a site opens a searchable list of sites. Pick one, fill in what it asks for, test, save.

- **The list is not part of Mediarium.** Mediarium ships no list of sites. The first time you open the list, your server downloads the community definitions (one archive from GitHub, under 1 MB) and keeps a copy in `/config/indexer-definitions`. It refreshes that copy at most once a day when you open the list again, or when you press Refresh. You choose which sites, if any, to add.
- **Public sites** usually need nothing: pick one and save. Some let you choose sorting or filters.
- **Private sites** need your account. Depending on the site, the form asks for a username and password (Mediarium signs in for you and signs in again when the site logs it out), an API key or passkey, or the **cookie** from your browser (for sites whose login has a CAPTCHA). To copy a cookie: sign in to the site in your browser, open the developer tools (F12), reload a page of the site, open the request in the Network tab and copy the whole `Cookie` request header. Some sites also want your browser's User-Agent. When the site signs you out, copy a fresh cookie.
- Passwords, cookies, API keys and passkeys are stored encrypted and are never sent back to the browser. When you edit a site, leaving a secret field blank keeps the saved value.
- **Addresses**: sites often have several mirrors. The form lets you choose one of the addresses in the definition; the first is used by default.
- **Test** signs in (if needed), runs a search and reports how many releases came back. If a test fails, the dashboard shows it until a later test passes.
- Mediarium waits between requests to the same site (at least one second, longer if the definition asks), and keeps each site's sign-in between searches, so it does not hammer sites.

### What doesn't work (yet)

A few definitions use things the engine can't run; the list marks them as unsupported and says why. Today that is:

- logins that always show a CAPTCHA (use the cookie option if the site offers it);
- regular expressions with look-ahead or look-behind in the definition's extraction filters (in text clean-up replacements they are skipped, leaving the text unchanged);
- a small number of definitions with invalid selectors.

Sites change their pages. When a site changes before its definition is updated, searches from it fail or come back empty; refreshing the list picks up the fix once the community has made one.

## Sites behind a Cloudflare check

Many public sites sit behind Cloudflare, which sometimes shows a "checking your browser" page that only a real web browser can pass. Mediarium can't embed a browser. When a site shows that page you'll see:

> This site is behind a Cloudflare check. Set up FlareSolverr (see Settings > Indexers) to use it.

[FlareSolverr](https://github.com/FlareSolverr/FlareSolverr) is a small, separate, optional service that opens the page in a headless browser and hands back the cookies that let Mediarium in. Only sites that show the check use it; everything else talks to sites directly. Add it next to Mediarium in your compose file:

```yaml
services:
  flaresolverr:
    image: ghcr.io/flaresolverr/flaresolverr:latest
    container_name: flaresolverr
    restart: unless-stopped
    environment:
      - LOG_LEVEL=info
      - TZ=Etc/UTC
    # Only needed if Mediarium is not in the same compose file:
    # ports:
    #   - "8191:8191"
```

Then set its address in Settings > Indexers > FlareSolverr (stored as `flaresolverr.url`, `flareSolverrUrl` in `GET/PUT /api/settings`): `http://flaresolverr:8191` when both are in the same compose file, otherwise `http://<host>:8191`. There is no default; with no address set, Cloudflare-protected sites simply fail with the message above.

Cloudflare's clearance is tied to your IP address and the browser's User-Agent, so FlareSolverr should reach the internet the same way Mediarium does (same host, same VPN or none).

## Where the definitions come from, and their licence

The definitions are the community-maintained Cardigann YAML files from [github.com/Prowlarr/Indexers](https://github.com/Prowlarr/Indexers) (the current `definitions/v11` folder), many of them synced from [Jackett](https://github.com/Jackett/Jackett). At the time of writing the Prowlarr/Indexers repository publishes no licence file (GitHub reports none); Jackett is GPL-2.0. Mediarium, which is AGPL-3.0, does not include, modify or redistribute these files: your own server downloads them from GitHub when you open the site list, and reads them as data. The engine that runs them is Mediarium's own code. Credit for the definitions goes to the Prowlarr and Jackett contributors.

## Responsible use

Mediarium does not come with, recommend or endorse any site. You choose which indexers and sites to add, and you are responsible for using them, and what you download through them, in line with the law where you live and the rules of each site. See [LEGAL.md](./LEGAL.md).

## For developers

- Engine: `internal/indexers` (`cardigann*.go` for the engine, `definitions.go` for the download and cache, `cardigann_selector.go` for the CSS selector subset). Tests use local fixture sites only (`internal/indexers/testdata/cardigann`).
- To check the engine against a local copy of the real definitions: `MEDIARIUM_CARDIGANN_DEFS=/path/to/definitions/v11 go test ./internal/indexers -run Corpus -v`.
- API: `GET /api/indexer-definitions?refresh=1`, `POST /api/indexers` with `{definitionId, name, baseUrl?, settings}`, `PUT /api/indexers/{id}` with any of `{name, baseUrl, apiKey, protocol, enabled, settings}` (left out = unchanged; blank `apiKey` or secret setting = keep the saved one; `protocol` only for Newznab/Torznab), `POST /api/indexers/{id}/test`. See [reference/api.md](./reference/api.md).
- Result download links from definition-based sites look like `mediarium-indexer://<id>?link=...`; the grab pipeline resolves them through that indexer's session (login, details page, Cloudflare) when the download starts.

## FlareSolverr with the bundled compose file

The compose file in this repository already contains an optional `flaresolverr` service. Start it together with Mediarium with:

```bash
docker compose --profile cloudflare up -d
```

Then enter `http://flaresolverr:8191` under Settings > Indexers. The first request to a protected site can take up to a minute while FlareSolverr passes the check; later requests reuse its cookies and are fast.
