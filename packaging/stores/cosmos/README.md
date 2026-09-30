# Cosmos Cloud: servapp kit (optional)

**Status: prepared, not submitted.**

Cosmos lists apps ("servapps") from the [azukaar/cosmos-servapps-official](https://github.com/azukaar/cosmos-servapps-official) market, added by pull request. Alternatively you can publish your own market from [azukaar/cosmos-marketplace-example](https://github.com/azukaar/cosmos-marketplace-example) (GitHub Pages) and users add its `servapps.json` URL in Cosmos's market settings.

## Files here

`servapps/Mediarium/`, laid out as in the official market:

| File | What it is |
|---|---|
| `description.json` | Name, short and long description, tags, repository, image page, `amd64`/`arm64`. |
| `cosmos-compose.json` | The app: an installer form asking for the one data folder, `/config` as a named volume, the data folder as `/data`, and a Cosmos route to port 8264. |
| `icon.png` | 256 x 256 icon. |
| `screenshots/1.png` ... `3.png` | 1280 x 720 (Dashboard, Discover, Library). |

## Requirements

- [ ] Image published and public; the market's CI checks the listed architectures against the image.
- [ ] Tested on a Cosmos server: install from a local copy, run the wizard, check the folders.
- [ ] Check against a current app in the market (for example Sonarr) that the `cosmos-compose.json` keys and the `{Context.data_path}` form value are still how the installer works; this kit follows the format as it was when prepared.
- [ ] The torrent port is not published by this file (Cosmos routes web traffic only). Torrents work without it; add a `ports` entry if you want incoming peers.

## Steps

1. Fork [azukaar/cosmos-servapps-official](https://github.com/azukaar/cosmos-servapps-official), copy `servapps/Mediarium/` into its `servapps/` folder.
2. Open a pull request "Add Mediarium"; the repo's validation workflow runs on it.
3. After merge, mark Cosmos as available in `docs/PLATFORMS.md`.

## What only the maintainer can do

- Test on Cosmos and open the pull request with your GitHub account.
