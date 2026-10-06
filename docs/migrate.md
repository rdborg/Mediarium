# Move from Radarr, Sonarr, Prowlarr, SABnzbd and other apps

Already running Radarr, Sonarr, Prowlarr, SABnzbd or NZBGet (on a Synology NAS or anywhere else)? Mediarium can copy your setup over so you don't have to type it all again. It moves no files. Your movies and shows stay where they are. The page is **Settings > System > Move from other apps**, and only administrators can open it. You can also bring over Jackett, NZBHydra2, Overseerr/Jellyseerr, Ombi, Bazarr, Medusa and SickChill (see the tables below).

It's **read-only on the other side**. Every request Mediarium sends to the other apps is a plain `GET`, except for NZBGet, which only speaks JSON-RPC over `POST`. There Mediarium calls just the read-only methods `version` and `config`. Nothing in those apps is changed, so you can try Mediarium next to them and switch over when you're ready.

## Contents

1. [What is brought over](#what-is-brought-over)
2. [Before you start](#before-you-start)
3. [Step 1: Find the addresses and API keys](#step-1-find-the-addresses-and-api-keys)
4. [Step 2: Preview](#step-2-preview)
5. [Step 3: Check the folder mapping](#step-3-check-the-folder-mapping)
6. [Step 4: Import](#step-4-import)
7. [Running both side by side](#running-both-side-by-side)
8. [Stopping the old apps](#stopping-the-old-apps)
9. [Troubleshooting](#troubleshooting)
10. [API](#api)

## What is brought over

| From | What | How |
|---|---|---|
| Radarr | Every movie with a TMDB id, its **monitored** flag and its quality profile | The movie is added to your library. If Radarr has a file for it, Mediarium finds that file in the movie's own folder and registers it as downloaded **where it is** (no copy, no move, no rename). Quality is read from the file name, or taken from Radarr when the name says nothing. |
| Sonarr | Every show, its **monitored** flag, which seasons are monitored, its quality profile and its series type (standard, anime or daily) | The show is added with its full episode list from TMDB. Shows are found by TMDB id (Sonarr 4 reports it) or looked up on TMDB by their TVDB id. Episode files in the show's folder are registered as downloaded where they are. |
| Radarr and Sonarr | Quality profiles | Each profile is matched to the closest built-in profile by resolution: **720p**, **1080p**, **4K & over**, or **Any** (for profiles that take SD, or several resolutions). You can pick another profile per Radarr/Sonarr profile. |
| Prowlarr | Newznab and Torznab indexers | Added with their address and API key (stored encrypted). Each one is tested first, and one that fails the test is added **switched off** so you can look at it. |
| Prowlarr | Sites from the community definitions (Cardigann) | Added from Mediarium's own site list (it comes from the same place as Prowlarr's), with the settings that fit. Sites Mediarium has no definition for, and Prowlarr's own built-in indexers, are listed as "add by hand". |
| SABnzbd | Usenet servers | Added with host, port, SSL, username, password (stored encrypted), connections and **priority** (0 = main server, higher numbers = backups), and switched off if they're off in SABnzbd. |
| Jackett | Every configured indexer | Added as a **Torznab** indexer pointing at Jackett's own feed for it (`<jackett address>/api/v2.0/indexers/<id>/results/torznab`), with Jackett's API key (stored encrypted). Each one is tested and added switched off if the test fails. Jackett keeps running for these indexers. When Mediarium's site list has a site with the same id, the preview marks it as able to be **added directly** instead (through the API only: list its id under `jackett.direct`). That removes the need for Jackett for that site, but a private tracker's login isn't copied, so it's added switched off until you enter it. |
| Overseerr / Jellyseerr | Requests that are not downloaded yet | Requests that are **pending, approved or being processed** become monitored titles: a movie by its TMDB id, a show by its TMDB id with the requested seasons monitored (past and future episodes; seasons nobody asked for stay unmonitored). Who asked is written to the title's activity ("Requested by Ann in Overseerr"). Requests that are already available there are skipped ("already downloaded there": the files are found with Import existing), and so are declined, failed and partly available ones. Several requests for the same title are one entry. |
| Ombi | Requests that are not downloaded yet | The same as Overseerr: movies by TMDB id, shows by TVDB id (looked up on TMDB). Pending and approved requests are added. Available, denied and partly available ones are skipped. The requester is written to the title's activity. A title Overseerr and Ombi both list is added once. |
| Medusa, SickChill | Every show, its **paused** flag (paused shows are added unmonitored) and its folder | Like Sonarr: shows are found by TVDB id (mapped to TMDB; Medusa shows indexed by TMDB use that id), added with their full episode list from TMDB, and the episode files in the show's folder are registered as downloaded where they are, through the same folder mapping as Sonarr. They don't tell how many files a show has, so a show whose folder isn't found is **skipped** (not added as missing) to avoid downloading it again. Their quality settings aren't matched to profiles, so shows get your default profile. |
| Bazarr | Subtitle languages | Only when you tick the **Subtitle languages from Bazarr** card: Mediarium's subtitle languages (Settings → Info, lists and subtitles → Subtitles) become the languages enabled in Bazarr. The languages you have now keep their place (the first one is the default for a manual search), and the others follow in Bazarr's order. Bazarr's language codes are mapped to Mediarium's (`pb` → `pt-BR`, `zt` → `zh-TW`), and a language without a usable code is listed and skipped. The preview always shows the change, plus Bazarr's **subtitle providers** and which of them have no Mediarium equivalent. Mediarium gets subtitles from OpenSubtitles only, so no other provider is carried over. Language profiles, forced/HI options, scores and provider accounts aren't imported. |
| NZBHydra2 | Its search endpoint | NZBHydra2 shows all of its indexers as **one** Newznab endpoint, so it's added as one Newznab indexer (`<address>/api`) with NZBHydra2's API key (stored encrypted), tested first. If it has torrent indexers too, tick **It also has torrent indexers (add its torrent feed too)** (`nzbhydra.torznab` in the API) to add its Torznab endpoint (`<address>/torznab/api`) as a second indexer. NZBHydra2's own list of indexers isn't copied (its API only gives it to a logged-in admin). Mediarium searches them through NZBHydra2, so keep NZBHydra2 running. |
| NZBGet | Usenet servers | Same as SABnzbd: host, port, encryption (SSL), username, password (stored encrypted), connections, **priority** (NZBGet's *Level*: 0 = main server, higher = backups), switched off when *Active* is `no`. A server with the same host and username as one already in Mediarium (or one imported from SABnzbd in the same run) isn't added twice. |

**Not brought over:** download history, the blocklist, custom formats and release profiles, tags, import lists, notifications, Prowlarr's app links, SABnzbd's categories and scripts, episode-by-episode monitoring (seasons are kept), and specials (season 0: Mediarium doesn't track them).

**No search is started** for anything imported. Monitored titles with no file yet are picked up by Mediarium's regular automatic search, like any other wanted title (see [Running both side by side](#running-both-side-by-side) to avoid downloading things twice), or you can search them yourself.

**Running it again is safe.** An import never adds something twice: movies and shows are matched by TMDB id, indexers by address (or site), and Usenet servers by host and username. Running it again only adds what's new, and links files that weren't found the first time (after you fix the folder mapping, for example).

## Before you start

- Mediarium is installed and you've finished the first-run wizard, with your **movies** and **TV** folders set (Settings → Library → Folders and file names). On a Synology, follow the [Synology guide](./synology.md) first.
- A TMDB API key is set (Settings → Info, lists and subtitles → Movie info and lists), unless your copy of Mediarium came with one.
- The movies and TV folders Radarr and Sonarr use are **mounted into Mediarium's container** too (see [Step 3](#step-3-check-the-folder-mapping)).
- Mediarium can reach the other apps over the network. When they all run on the same NAS, use the NAS's address (for example `http://192.168.1.20:7878`), not `localhost`. Inside a container, `localhost` is the container itself.

## Step 1: Find the addresses and API keys

You need each app's address and its **API key**, for the apps you want to import from. An app you fill in needs a full `http://` or `https://` address and its key (NZBGet needs a username instead), and the folder boxes in step 3 must be full paths. Mistakes are pointed out under the box before anything is checked.

| App | Usual address | Where the API key is |
|---|---|---|
| Radarr | `http://<nas-ip>:7878` | **Settings → General → Security → API Key** |
| Sonarr | `http://<nas-ip>:8989` | **Settings → General → Security → API Key** |
| Prowlarr | `http://<nas-ip>:9696` | **Settings → General → Security → API Key** |
| SABnzbd | `http://<nas-ip>:8080` | **Config** (the gear icon) → **General → Security → API Key**. Use the **API Key**, not the NZB Key. |
| Jackett | `http://<nas-ip>:9117` | The **API Key** at the top right of Jackett's dashboard. Mediarium reads the list of your configured indexers with it. If you set an admin password in Jackett and the list is refused, remove the password while you import, or add the indexers by hand. |
| Overseerr / Jellyseerr | `http://<nas-ip>:5055` | **Settings → General → API Key**. Both use the same API. |
| Ombi | `http://<nas-ip>:3579` | **Settings → Configuration → Ombi Configuration → API Key** (a key of the administrator account). |
| Medusa | `http://<nas-ip>:8081` | **Config → General → Interface → API Key** (Medusa's API v2). |
| SickChill | `http://<nas-ip>:8081` | **Config → General → Interface → API Key** (generate one if it's empty). |
| Bazarr | `http://<nas-ip>:6767` | **Settings → General → Security → API Key**. |
| NZBHydra2 | `http://<nas-ip>:5076` | **Config → Main → Security → API key** (and tick "Enable API key" if it's off). Enter the address without `/api`. |
| NZBGet | `http://<nas-ip>:6789` | No API key. NZBGet uses a **username and password** (**Settings → Security → ControlUsername / ControlPassword**; the defaults are `nzbget` and `tegbzn6789`). |

If you gave an app a **URL base** (for example `/radarr`), include it: `http://192.168.1.20:7878/radarr`. The quickest way to get the right address is to open the app in your browser and copy what the address bar shows, up to the port (and URL base).

## Step 2: Preview

Under **1. Connect your current apps**, fill in the address and key of each app you use (leave the others empty) and press **Check and preview**. Mediarium reads the apps and shows, for each one:

- whether it could connect, and the app's version;
- how many items are **to add**, **already here** and **skipped**;
- every movie, show, indexer, server and request with what would happen to it and why (press **Show all** or **Show the list** on its card).

A preview changes nothing, in Mediarium or in the other apps. Common reasons you may see:

| Reason | What it means |
|---|---|
| already in your library | Mediarium has it already, so it's left as it is (a file that wasn't linked yet still gets linked). |
| folder not found at `/movies/Heat (1995)` | The movie's folder isn't where the folder mapping says it is. The title is **skipped**, not added as missing, so Mediarium doesn't download a copy of something you have. Fix the mapping ([Step 3](#step-3-check-the-folder-mapping)) and preview again. |
| no TMDB id | Radarr has no TMDB id for it, or TMDB has no show with Sonarr's TVDB id. Add it by hand from Discover. |
| Radarr has no file for it yet | Added as wanted, with its monitored flag. |
| the folder is outside Mediarium's movies folder | The files are registered where they are and play fine. New downloads for it go to Mediarium's movies folder. |
| Prowlarr hid its API key / SABnzbd did not reveal the password | Newer versions hide secrets from their API. The indexer or server is added **switched off**. Open it in Settings (**Indexers**, or **Downloading → Usenet and torrents** for Usenet servers), paste the key or password, and switch it on. |
| unsupported indexer type, add it by hand | Prowlarr runs this indexer with its own built-in code, or Mediarium's site list has no definition for it. Look for it under Settings → Indexers & Search. |

## Step 3: Check the folder mapping

Radarr and Sonarr report where each title lives **as their own container sees it**, which may not be where Mediarium's container sees the same folder. After the preview, **2. Check your folders** lists the **root folders** of Radarr, Sonarr, Medusa and SickChill, the same folder in Mediarium, and how many title folders were found there. Change the folder in the middle column if it's wrong, then press **Check again with these folders**.

A mapping says "what Radarr calls *from*, Mediarium calls *to*". Only the start of the path is swapped: with `/movies` → `/data/media/movies`, Radarr's `/movies/Heat (1995)` becomes `/data/media/movies/Heat (1995)`.

**Example: a Synology with a `data` shared folder.**

```
/volume1/data
├── usenet/            SABnzbd's downloads
└── media/
    ├── movies/        your movies
    └── tv/            your shows
```

- If Radarr, Sonarr **and** Mediarium all map `/volume1/data` as `/data` (the layout the [Synology guide](./synology.md) uses), they see the same paths (`/data/media/movies/...`) and **no mapping is needed**.
- If Radarr maps `/volume1/data/media/movies` as `/movies` and Sonarr maps `/volume1/data/media/tv` as `/tv`, while Mediarium maps `/volume1/data` as `/data`, you need:

  | From (Radarr/Sonarr) | To (Mediarium) |
  |---|---|
  | `/movies` | `/data/media/movies` |
  | `/tv` | `/data/media/tv` |

**Suggestions.** For a root folder that doesn't exist in Mediarium as it is, Mediarium suggests a mapping to its own movies (or TV) folder when the last folder names match (`/movies` and `/data/media/movies` both end in `movies`), or when Mediarium's folder holds the same title folders. A suggestion is used unless you enter your own mapping for that root folder, and the preview shows which ones are suggested. Check them: every root folder should show a green badge such as "12 of 12 title folders". A red badge means the folder wasn't found or holds no title folder.

## Step 4: Import

Under **3. What will be imported**, each app has a card with a tick box. Untick the parts you don't want (a Bazarr card starts unticked). Radarr and Sonarr profiles are listed under **Quality profiles**. Pick the Mediarium profile each one should become, or **Default profile**. Then, under **4. Import**, press **Import now**. It runs in the background and shows its progress, and you can leave the page. At the end you get counts for each part (added, already here, skipped, failed) and the notes and errors, if any.

## Running both side by side

You can keep the old apps running while you get used to Mediarium.

- **Avoid downloading things twice.** Radarr/Sonarr and Mediarium would both search for monitored titles that have no file. Until you switch, either turn off **Automatic searching and downloading** in Mediarium (Settings → Library → Quality → Automation; the Import step has the same switch, **Pause automatic searching while I test**), or unmonitor those titles in one of the two apps.
- **Files downloaded by Radarr or Sonarr after the import** aren't known to Mediarium yet. Run the import again (it only adds what's new), or use **Import existing** on the Library page ([import-library.md](./import-library.md)).
- Mediarium downloads with its own built-in Usenet and torrent downloaders into its own downloads folder, not through SABnzbd. Both use connections from your Usenet provider's limit, so if downloads fail with "too many connections", lower the connections in one of them.

## Stopping the old apps

When Mediarium does everything you need, run the import once more to pick up anything new, then stop Radarr, Sonarr, Prowlarr and SABnzbd. On a Synology: **Container Manager → Container**, select them, **Action → Stop** (or stop their project). Keep them stopped, not deleted, for a few weeks in case you want to look something up. See [If you already run other apps](./synology.md#if-you-already-run-other-apps). None of this touches your movies and TV folders.

## Troubleshooting

- **"could not reach 192.168.1.20:7878"**: the address or port is wrong, the app isn't running, or a firewall is blocking it. Open the same address in your browser from another computer on the network.
- **"the API key was not accepted"**: copy the key again from the place in [Step 1](#step-1-find-the-addresses-and-api-keys). For SABnzbd, use the API Key, not the NZB Key.
- **"this address is Sonarr, not Radarr"**: the addresses are swapped.
- **"answered 404"**: check the URL base (the part after the port, if you set one in the app).
- **Many titles "folder not found"**: the folder mapping is wrong, or the folder isn't mounted into Mediarium's container. The root folder list in the preview shows where each root folder ends up.
- **Episodes left out** ("aren't listed on TMDB for this show"): TMDB and TheTVDB sometimes number seasons differently (common with anime and long-running shows). Those files stay on disk, and Mediarium doesn't track them.

## API

All three endpoints are for administrators. See [reference/api.md](./reference/api.md) for the full route list.

- `POST /api/migrate/preview` with `{radarr?, sonarr?, prowlarr?, sabnzbd?, nzbget?, jackett?, nzbhydra?, overseerr?, ombi?, bazarr?, medusa?, sickchill?, pathMap?, profileMapping?}` (each app `{url, apiKey}`, except `nzbget`: `{url, username, password}`, `jackett`: `{url, apiKey, direct?: ["<indexer id>", ...]}` and `nzbhydra`: `{url, apiKey, torznab?: true}`; `pathMap` `[{from, to}]`, `profileMapping` `{"<Radarr/Sonarr profile name>": <Mediarium profile id>}`) answers what an import would do, per app, changing nothing. API keys and passwords are never sent back.
- `POST /api/migrate/run` with the same body plus `include` (`{movies, series, indexers, usenetServers, qualityProfiles, requests, subtitleLanguages}`, each `true` unless set to `false`, except `subtitleLanguages`, which is `false` unless set to `true`) starts the import in the background (202), or answers 409 while one is running.
- `GET /api/migrate/status` follows it: `{running, step, done, total, startedAt, finishedAt, results, items, errors, notes}`.
- The preview has one key per app that was sent: `radarr`, `sonarr`, `medusa` and `sickchill` are `{ok, error?, version?, summary, rootFolders, profiles, items}` (Medusa and SickChill have no profiles); `prowlarr`, `jackett` and `nzbhydra` are `{ok, error?, version?, summary, items}` with indexers; `sabnzbd` and `nzbget` are `{ok, error?, version?, summary, items, completeDir?, categories?}` with Usenet servers; `overseerr` and `ombi` are `{ok, error?, version?, summary, items}` with requests (`title, year, mediaType, tmdbId, tvdbId, status, requestedBy, seasons, action, reason`); `bazarr` is `{ok, error?, version?, included, summary, languages, current, new, providers, providersError?}`. Every item has an `action` (`add`, `exists` or `skip`) and a `reason`.
- The status `results` has `movies`, `series` (Sonarr, Medusa and SickChill), `indexers` (Prowlarr, Jackett, NZBHydra2), `usenetServers` (SABnzbd, NZBGet), `requests` and `subtitleLanguages`, each `{added, existing, skipped, failed, filesLinked, disabled}`. The `step` goes through `reading`, `movies`, `series`, `indexers`, `usenetServers`, `requests`, `subtitleLanguages` and `done`, and each item's `kind` is `movie`, `series`, `indexer`, `usenetServer`, `request` or `subtitleLanguages`.
