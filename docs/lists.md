# Import lists

An import list is a list somewhere else (on Trakt, TMDB, IMDb, MDBList, Simkl or Letterboxd, or an RSS feed) that Mediarium reads on a schedule. Whatever is new on it is added to your library, with the quality profile, folder, monitoring and tags you chose for that list. It is the same idea as Lists in Radarr and Sonarr.

You find it under **Settings > Info, lists and subtitles > Import lists**. It is for administrators. Nothing is added until you add a list yourself.

## The rules every list follows

- **Nothing is ever removed.** If a title leaves the list, it stays in your library.
- **A title is added once.** If you remove it from your library later, the list does not put it back. **Start this list over** (under the list) makes the list forget what it has seen, so it treats everything on it as new again.
- **Your Not interested list is respected.** A title you marked as not interested (Discover) is skipped. If you take it off that list, the next read adds it.
- **Titles already in your library are skipped**, in any quality.
- **A few at a time.** **Add at most this many at a time** (5 to 100, 10 to start) limits how many titles one read adds, so a list of 250 movies is not searched for all at once. The rest wait for the next read, and the list's result says how many.
- **Searching for what it adds.** With **Start searching for each one as it is added** on, the titles a read added are searched for right after, the same way the scheduled searches do it: no more than 10 automatic downloads an hour, the delay profile and the size limits apply, a movie that is not out yet is left for later, and an indexer that is paused is not asked. Whatever is not found right away is picked up by the next scheduled search.
- **Movies and shows.** Choose **Movies and shows**, **Movies only** or **Shows only** for each list. Letterboxd only has movies.
- **Only the ones you want.** **Rated at least** (TMDB's rating, 0 to 10) and **From this year on** hold back the rest. They use TMDB's rating and year whichever service the list came from, so the same number means the same thing everywhere. A title nobody has rated yet does not pass a minimum rating.
- **Leave what is on the list now alone.** Tick this when you add a list and only titles added to it afterwards are brought into your library.

Each list has an **On / Off** switch, **Run now**, and says when it was last read and what it did: how many titles it found, how many it added and why the others were skipped. How often it is read is up to you, from every hour to every week (every day to start). Mediarium checks which lists are due every ten minutes. **Check the list** in the form reads the list and shows how many movies and shows are on it and a few names, without adding anything.

Mediarium is polite to the services. It asks as itself (`Mediarium/<version>`), reads each list with as few requests as it can, spaces out its lookups, reads one list at a time, and when a service says to slow down it stops and tries again at the next read.

## Where the lists come from

### Trakt

Needs the Trakt client ID from the **Trakt** card at the bottom of this page (the official build has one built in). Only public things can be read: Mediarium does not sign in to Trakt, so a private list or a private profile gives "That Trakt list or profile is private".

- **Trakt list.** Paste the address of a list, like `https://trakt.tv/users/name/lists/list-name` or `https://trakt.tv/lists/2142754`.
- **Trakt watchlist.** Paste someone's watchlist address or just their Trakt user name. Their profile has to be public.
- **Trending, popular or anticipated.** Trakt's own charts, for movies, shows or both. **How many titles to look at** is up to 100.

### TMDB

Uses the TMDB key you already have.

- **TMDB list.** Paste the address of a public list on themoviedb.org (`https://www.themoviedb.org/list/8504994`) or just its number. Lists can hold movies and shows.
- **Trending, popular, top rated or upcoming.** TMDB's charts. Popular and top rated can be narrowed to a **genre** (when you add movies only or shows only) and an **original language**, for example top rated Korean shows. Trending and upcoming cannot be narrowed.

### IMDb (a CSV file)

IMDb does not let programs download its lists: its export address answers a program with a check meant for people, and Mediarium does not try to get past that. So you export the list yourself:

1. Open the list (or your watchlist) on imdb.com in your browser and choose **Export**.
2. In Mediarium, add an IMDb list and **Upload the file**.

The titles are read from the file (movies, TV series and mini series; episodes are left out) and matched by their IMDb number. The file stays with the list, so the list keeps working, but it only knows what was in that file. To bring in newer titles, edit the list and upload a newer export; titles already handled are not added twice.

You can also give an address that serves the CSV (a file on your own server, say). The address of an IMDb list page (`https://www.imdb.com/list/ls055592025/`) is accepted too, but IMDb will most likely refuse it, and the list then says so.

### MDBList

Needs your own MDBList API key (you make one at mdblist.com/preferences, free). Paste a list address like `https://mdblist.com/lists/name/list-name`. The key is kept encrypted, is only sent to MDBList and is never shown again. MDBList's own daily limit for your key applies; a list is read in one or a few requests.

### Simkl (your own lists)

A Simkl list belongs to an account, so this is the one source that needs you to sign in. It takes two steps, both in the **Simkl** card at the bottom of this page:

1. **A Client ID.** The official build has one built in. Otherwise make a free one: sign in at simkl.com, open **Settings**, then **Developer**, press **New app** (the button may also read *New Application*), name it Mediarium, enter `urn:ietf:wg:oauth:2.0:oob` as the **Redirect URI** (exactly that, nothing else) and save. Copy the **Client ID** and paste it in the card. Mediarium signs in with a code, so it only needs the Client ID; if Simkl also shows a Secret, you do not need to enter it anywhere. The badge on the card says *Client ID saved*; that is only the first step, the account is connected separately below it. A personal Client ID also avoids the limit of a shared one. The **Test** button next to the field asks Simkl for a sign-in code the same way **Connect** does (the newer way first, the older PIN way for older apps) and answers *Simkl accepted this Client ID.* when it works. It does not start a sign-in, so there is nothing to finish. When Simkl turns it down, its own words are shown and a warning line (with only the first four characters of the Client ID) goes to the log.
2. **Connect your account.** Press **Connect my Simkl account**. Mediarium shows a short code in big letters, with a **Copy** button and an **Open Simkl and enter this code** button. Open Simkl with that button (or go to the address it opens yourself), enter the code and approve. The card shows how long the code lasts and says *Waiting for you to approve it on Simkl* until you do; then it says *Connected as* and your Simkl name. Mediarium never sees your Simkl password and needs no public address. Apps made on Simkl today use its OAuth 2.0 sign-in with a device code; older Simkl apps use the earlier PIN code at `simkl.com/pin`. Mediarium tries the first and falls back to the second by itself, so you do nothing different. It keeps the access token it gets back encrypted (and the refresh token, when Simkl gives one, which it uses to renew the sign-in when it runs out) and never shows them again. **Test connection** asks Simkl whose account the saved sign-in belongs to and shows the name. **Disconnect** forgets it. If the code runs out, you decline on Simkl or Simkl refuses something, the card says so and **Start again** gives you a new code.

Then add a **Simkl list** and choose which of your lists to read: **Plan to watch**, **Watching**, **Completed**, **On hold** or **Dropped**. Movies, shows and anime on that list are read together; an anime film counts as a movie and any other anime as a show. Titles are found by their TMDB number, or by their IMDb or TheTVDB number when Simkl has no TMDB one, and by name and year as a last resort. Like every other list, it adds new titles only and never removes anything, and it uses the monitoring, folder, tags and limits you set on the list.

If the saved sign-in can no longer be read (the key file `secret.key` does not belong to this database), the card says so and Health lists it. Connect again and the lists carry on. If Simkl stops accepting the sign-in, the list says so in its result and the same applies.

### Letterboxd (a CSV file)

Letterboxd does not let programs read its pages (it shows them a check meant for people), so, as with IMDb, you export the file yourself: your watchlist from **Settings > Import & Export** on letterboxd.com, or a single list exported as CSV, and upload it. Letterboxd files do not carry TMDB numbers, so Mediarium finds each movie by its name and year. A title is only taken when the name matches and the year is within one year; a few titles may not be found, and the list's result says how many. Movies only.

### RSS feeds

Any RSS or Atom feed: an indexer's own RSS feed, a tracker's, or a site that posts new releases or titles. Paste the feed's address. If it needs your key, keep the key in the address; the address is kept on your server and only sent to that site.

- **Which title an entry is.** An entry's IMDb, TMDB or TheTVDB id says it exactly, when the feed has one (Newznab and Torznab feeds usually do, and a link to the title's IMDb or TMDB page counts too). Otherwise the entry's name is read like a release name: `Dune.Part.Two.2024.2160p.WEB-DL-GRP` is Dune: Part Two from 2024, and a name with a season or episode (`Fargo.S05E03`) is a show. The many releases of one title count once.
- **Also download wanted releases from this feed.** Off at first. When it's on, every entry with a download link (an NZB, a torrent file or a magnet link) is checked the way a new release from an indexer is: it is downloaded when it is for a monitored title in your library that is missing, or when it is a better version than the file the title has, under that title's quality profile. The limit of 10 automatic downloads an hour, the blocklist, the size limits and the delay profile all apply. Up to 200 entries are checked each time the feed is read. This works for titles the feed just added and for titles that were already in your library, so a feed of new releases can bring you upgrades (#160).

### Plex watchlists

The Plex watchlists accounts have connected (**Settings > Account & Access > Linked services > Plex watchlist**) show in the same view, each with **Check now**. They work as before: every 20 minutes, new titles are added, or asked for when the account has to ask. They are managed by the account that connected them.

## When a list fails

The result under the list says why, in words. The usual ones:

- *Add your TMDB API key first*: every list needs it to find and add titles.
- *That Trakt list or profile is private*: make it public on Trakt.
- *MDBList doesn't accept that API key*: copy the key again.
- *Connect your Simkl account first*, or *Simkl no longer accepts the saved sign-in*: connect the account again in the Simkl card.
- *Simkl answered with status 400*: Simkl's own words follow in brackets. Check that you pasted the Client ID of your Simkl app (not the secret) and that the app's Redirect URI is exactly `urn:ietf:wg:oauth:2.0:oob`. *This client_id is an OAuth 2.0 app: use POST /oauth2/device instead of /oauth/pin* came from older versions of Mediarium that only knew the PIN sign-in; update, and press **Connect** again.
- *That site only lets people in with a browser*: the address you gave is guarded. Export the file in your browser and upload it.
- *TMDB has no public list with that number*: check the number. Lists made on the TMDB website recently may not be readable by programs.

The list is tried again at its next read.

## For developers

Everything below is for administrators.

- `GET /api/import-lists` returns `{"lists": [...], "sources": [...], "plexWatchlists": [...]}`. A list never returns its file or its key: `hasCsv` and `hasKey` say they are set.
- `POST /api/import-lists` and `PUT /api/import-lists/{id}` take `name`, `source` (`trakt-list`, `trakt-watchlist`, `trakt-chart`, `tmdb-list`, `tmdb-chart`, `imdb`, `mdblist`, `simkl`, `letterboxd`), `media` (`movie`, `tv`, `both`), `config` (`url`, `chart`, `count`, `genre`, `language`, `csv`, `status` for Simkl: `plantowatch`, `watching`, `completed`, `hold` or `dropped`), `key` (MDBList; empty keeps the saved one), `enabled`, `intervalHours` (1 to 168), `profileId` (0 = default), `movieRoot`, `tvRoot`, `monitored`, `searchNow`, `tags`, `minRating`, `minYear`, `maxPerRun` (1 to 100) and `startFresh`. A new list is read at once.
- `PUT /api/import-lists/{id}/enabled`, `DELETE /api/import-lists/{id}`, `POST /api/import-lists/{id}/run` (answers `202`; the list shows `running` while it is read), `POST /api/import-lists/{id}/forget`, `POST /api/import-lists/preview` (the same body as create, plus an optional `id`) and `POST /api/import-lists/plex/{userId}/run`.
- The Simkl account (administrators): `GET /api/settings/simkl` returns `{"connected", "unreadable", "hasClientId"}` and never the token. `POST /api/settings/simkl/pin` starts the sign-in and returns the `code`, the address to enter it at (`url`, and `fullUrl` with the code filled in when Simkl gives one), the `flow` (`device` or `pin`), `expiresIn` and `interval` in seconds. `GET /api/settings/simkl/pin/{code}` answers `{"done": false, "interval"}` while the code waits (`interval` is how many seconds to wait before asking again; it grows when Simkl asks to slow down), `{"done": true, "name", "status": {...}}` once it was entered and `410` when it expired, was declined on Simkl or was never handed out. `POST /api/settings/simkl/test` returns `{"ok", "message"}` with the Simkl account's name. `DELETE /api/settings/simkl` disconnects. The Client ID is the `simklClientId` setting.
- Tables: `import_lists` and `import_list_items` (what each list has handled: added, in-library, existing, excluded, filtered, no-match or failed).
