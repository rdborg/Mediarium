# Subtitles

Mediarium can save subtitles next to your movies and episodes. This page covers where they come from, the daily limits and how to control it.

## The switch

Turn subtitles on under **Settings > Info, lists and subtitles > Subtitles**, with **Subtitles for my movies and shows**. With it off, Mediarium doesn't fetch subtitles and hides the parts of the app that deal with them: the Wanted page has no Subtitles tab, and title pages have no subtitle tools. Your account, key and language choices are kept for when you turn it on again.

Subtitles that come inside a download are always imported and always listed in a title's files.

For scripts: the switch is the setting `subtitles.enabled` (`subtitlesEnabled` in `GET`/`PUT /api/settings`). While it is off, `/api/subtitles/*`, `/api/movies/{id}/subtitles*` and `/api/episodes/{id}/subtitles*` answer `409` with "Subtitles are switched off".

## Which languages

**Languages to keep** under **Settings > Info, lists and subtitles > Subtitles** offers English, Spanish, French, German, Italian, Portuguese (Portugal and Brazil), Dutch, Swedish, Danish, Finnish, Norwegian, Polish, Czech, Slovak, Slovenian, Hungarian, Romanian, Bulgarian, Croatian, Serbian, Bosnian, Ukrainian, Greek, Turkish, Russian, Arabic, Hebrew, Japanese, Korean, Chinese (Simplified and Traditional), Hindi, Indonesian, Thai, Vietnamese and Catalan. They use the language codes OpenSubtitles uses (`hr`, `sr`, `bs`, `sl` and so on). OpenSubtitles has a single Serbian: its Latin and Cyrillic subtitles are both `sr`, so Mediarium keeps whichever one it is given, and reads the file in the right alphabet. Scripts can send any code made of two or three letters (`pt-BR` and `zh-CN` style regions are fine), up to 50 languages in `subtitleLanguages`.

## Where subtitles come from

1. **The release itself.** When a movie or episode is imported, Mediarium copies the subtitle files from the finished download (`.srt`, `.ass`, `.ssa`, `.sub` with `.idx`, and `.sup`) next to the video, named the way Plex, Jellyfin and Kodi expect: `Movie (2020).en.srt`, `Movie (2020).fr.srt`. This is free and uses none of your daily allowance.
2. **Inside the video.** Many MKV and MP4 files carry their own subtitle tracks. Mediarium reads the file's track list (only the headers, never the video itself, so it's quick even on a network share) and a language that's already in there counts as done. See [Subtitles inside the video](#subtitles-inside-the-video).
3. **OpenSubtitles, SubDL and add-ons.** For languages still missing, Mediarium downloads the subtitle that fits your file best (release group, source, codec) from the sources you have switched on. See [Choosing your sources](#choosing-your-sources).

## Choosing your sources

The **Subtitle sources** card at the top of the Subtitles page lists the three kinds of source, each with its own switch:

| Source | What it is | Limits | Needs |
|---|---|---|---|
| **OpenSubtitles** | The biggest subtitle site | about 5 downloads a day, about 20 with a free account | a free API key (official builds include a shared one) |
| **SubDL** | A second site | about 2,000 searches and a few hundred downloads a day | a free API key |
| **Add-ons** | Sites Mediarium has no built-in support for, through [Stremio add-ons](#subtitle-add-ons) | each site's own | at least one add-on added |

Every source that is switched on is searched, and the best fit for your video file wins, whichever site it comes from. Each row also shows where that source stands: **On**, **Switched off**, **Needs a key** or **On, but the daily limit is reached today**. The key (and, for OpenSubtitles, your account) is entered inside the row; add-ons are added and removed in the **Subtitle add-ons** card below it, and the **Use add-ons** switch turns all of them on or off together.

A source that is switched off is never asked: not by the automatic download, not by a search you start by hand, not by **Get subtitles** or **Search for missing subtitles now**, and not by the monthly swap for an exact-file subtitle. What it would have used does not count towards any limit, and the dashboard notice only talks about the limits of the sources that are on. A source without a key can't be switched on until you save the key. A source you switched off stays off, also when you replace its key, until you switch it back on.

Some ways to use it:

- **Only SubDL.** Save a SubDL key and switch OpenSubtitles off. (If your build has no OpenSubtitles key included and you never added one, it is off already.) Everything is fetched from SubDL, and the OpenSubtitles daily limit plays no part. Subtitles made for your exact file can't be found this way, since only OpenSubtitles can match the file, so the monthly swap does nothing.
- **Only OpenSubtitles.** Leave SubDL without a key, or switch it off. This is how it worked before there were switches.
- **Only add-ons.** Add your add-on, then switch OpenSubtitles and SubDL off. Handy for a language one site covers better, such as subs.ro for Romanian.
- **Several together.** Leave them all on. When one runs out of downloads for the day, automatic downloading carries on with the others.

If you switch every source off, the page says **No subtitle source is switched on**, the dashboard shows the same, and the subtitle search and download buttons stay greyed out with that reason until you switch one on.

For scripts: the switches are `openSubtitlesEnabled`, `subdlEnabled` and `subtitleAddonsEnabled` in `GET`/`PUT /api/settings` (stored as `subtitles.opensubtitles_enabled`, `subtitles.subdl_enabled` and `subtitles.addons_enabled`; on unless set to `0`). `GET /api/settings` also lists `subtitleSources`, one entry per source with `id`, `enabled`, `ready` and `state` (`on`, `off`, `needs_key` or `limit`). Switching a source on that has no key answers `400`. `GET /api/subtitles/quota` carries `hasSource` (at least one source is on) and `hasKey` (OpenSubtitles is on, so its limit applies).

## Normal, SDH and forced subtitles

A subtitle comes in three kinds:

- **Normal** is the plain dialogue.
- **SDH** (subtitles for the deaf and hard of hearing) is the dialogue plus descriptions of sounds, like "[door slams]". Some sites call it "hearing impaired".
- **Forced** holds only the lines spoken in another language than the film's, such as a few words of a foreign tongue. Players show it even when you have subtitles off. OpenSubtitles calls it "foreign parts only".

Pick what you want for each language under **Settings > Info, lists and subtitles > Subtitles**, in **Which kind of subtitle** (it lists the languages you picked above it):

| Choice | What Mediarium looks for |
|---|---|
| **Normal or SDH** (the default) | one full subtitle, normal or SDH, whichever fits your video best |
| **Normal only** | one full subtitle that is not SDH |
| **SDH only** | one full subtitle for the hard of hearing |
| **Forced only** | only a forced track, no full subtitle |
| **... plus a forced track** | the full subtitle you chose, and a forced track next to it |

The default is what Mediarium always did, with two improvements: a forced track is never taken for the full subtitle any more, even when its release name fits your file best, and a subtitle that its source marks as SDH is saved with `.sdh` in its name (it used to be saved as a plain `.en.srt`). Subtitles saved before keep their names.

**File names.** A subtitle is saved next to the video as `Movie (2020).en.srt`, an SDH one as `Movie (2020).en.sdh.srt` and a forced one as `Movie (2020).en.forced.srt`. Plex reads `.forced` and `.sdh` (SDH needs Plex Media Server 1.20.3 or newer), Jellyfin reads `.forced` and `.sdh` in recent versions, and Emby reads both too (`.sdh` from Emby Server 4.8). An older server shows the file as a plain subtitle. A file that already has such a name next to a video is read the same way: `.forced` and `.foreign` mark a forced track, `.sdh`, `.hi` and `.cc` one for the hard of hearing, and a file with none of them is a normal subtitle.

**What counts as already there.** A forced file only fills a forced track, and never the full subtitle. **Normal only** is not met by an SDH file, **SDH only** is not met by a plain file, and **Normal or SDH** is met by either. Subtitle tracks inside the video count the same way, by the file's own flags (forced, hearing impaired) or by the track's name when it says *forced* or *SDH* (MP4 tracks carry no such flags, so they count as normal). The Wanted page lists a missing forced track as, for example, "English (forced)".

**How each source says what it is.** OpenSubtitles marks every subtitle as hearing impaired and/or foreign parts only, and Mediarium asks for them in the search when your choice needs it (it asks for nothing extra for the default). It also checks the answer, so the right kind is picked from every source. SubDL marks hearing impaired subtitles; a forced one is recognised by the word *forced* (or *foreign parts*) in its name or the uploader's comment. Subtitle add-ons carry no flags in the Stremio protocol, so only those words count: with **Forced only** or **SDH only** an add-on may find nothing. A subtitle marked both hearing impaired and foreign parts only is taken as SDH, since uploads are often ticked both by mistake. A name that starts with the word *forced* is taken as part of a title, and "not forced" or "non-SDH" count as neither.

**By hand.** In a movie's or episode's subtitle box the list marks each result **SDH** or **forced**, and what you download is saved with the matching name. A language that wants a forced track also gets a **Search English forced** button that lists only forced tracks.

**Swapping for a better one.** The month-long swap for a subtitle made for your exact file (see below) keeps the kind: a forced track is only swapped for a forced one. Under **Normal or SDH** either kind may replace the other, and the file it replaces is removed so you don't end up with both. A file with the other name that is already there is left alone.

Scripts: `GET`/`PUT /api/settings` carry `subtitleProfiles`, an object of language code to `{"full": "either" | "normal" | "sdh" | "none", "forced": true | false}` (`none` always wants the forced track; a language that is not listed has the default; `{}` puts every language back to it). It is stored as `subtitles.profiles`. `GET /api/movies/{id}/subtitles/status` also lists `slots` (`language`, `kind` `full` or `forced`, `present`, `embedded`). `GET /api/movies/{id}/subtitles?lang=en&variant=forced` lists only forced tracks; every result carries `variant` (`sdh`, `forced` or empty), and the download request takes the same `variant`. `/api/subtitles/wanted` names a missing forced track `en.forced`.

## Subtitles inside the video

Subtitle tracks inside an MKV (also WebM) or MP4 (also M4V and MOV) count as there, so nothing is downloaded for a language the video already has. A title's subtitle box shows them as **✓ in the video**.

- A track marked *forced* (only the foreign-language lines), or named "Forced", doesn't count as a full subtitle; it counts only for a [forced track](#normal-sdh-and-forced-subtitles). A track flagged as for the hard of hearing, or named "SDH", counts as an SDH subtitle.
- A track with no language set doesn't count for any language. An MKV track with no language element at all is English, as the Matroska rules say.
- Picture-based tracks (PGS from Blu-rays, VobSub from DVDs) count as well. If your player can't show them, switch this off.
- QuickTime "text" tracks in MOV files are left out: they're usually chapter names.

Switch it off with **Count subtitles already inside the video file** under **Settings > Info, lists and subtitles > Subtitles** (stored as `subtitles.count_embedded`; `subtitleCountEmbedded` in `PUT /api/settings`). `GET /api/movies/{id}/subtitles/status` lists the languages found inside the video in `embedded`.

## SubDL

[SubDL](https://subdl.com) is a second place to find subtitles. It's optional and needs a free API key: create an account at subdl.com, copy the key from your account panel and paste it into the **SubDL** card under **Settings > Info, lists and subtitles > Subtitles** (stored encrypted as `subtitles.subdl_api_key`; `subdlApiKey` in `PUT /api/settings`; test it with `POST /api/settings/test-service` and `{"service": "subdl"}`).

While SubDL is switched on (it is as soon as you save a key), every search asks it along with the other sources that are on, and the best fit for your file wins, whichever site it's from. SubDL has no file hash, so "made for this exact file" only ever comes from OpenSubtitles; a SubDL subtitle can later be swapped for one of those if [upgrades](#subtitles-made-for-your-exact-file) are on. When the OpenSubtitles daily limit is used up, automatic downloads carry on with SubDL and the add-ons. A short "too many requests" from OpenSubtitles is not the daily limit: Mediarium waits the few seconds it asks for and tries again. You can also use SubDL without OpenSubtitles: switch OpenSubtitles off, or just leave it without a key.

SubDL allows about 2,000 searches a day on a free key and a few hundred downloads a day from one address. Its downloads are zip files: Mediarium takes the SRT out (for an episode, the file named for that episode) and saves only that. When you choose a subtitle by hand, the list says which site each one is from. Scripts: results from SubDL carry `"source": "subdl"` and a `link`; download one with `{"source": "subdl", "link": "...", "language": "en"}`.

**How a title is looked up.** OpenSubtitles and SubDL are first asked with the TMDB id, which names the title exactly. Neither site has every title under that id, so when it finds nothing, Mediarium asks again with the IMDb id and then with the title and year, and uses the first answer that has subtitles. Up to three pages of results are read. If a site can't be reached or turns the request down while the others have nothing, the search shows that error instead of "No subtitles found", so you can tell a failed search from an empty one. A failed search is also not remembered: the next one asks again.

A lookup that comes back with nothing usable, for any reason, is followed by the next one: SubDL answering with other languages only, say, does not stop the title lookup. The language is judged in any spelling a site uses ("EN", "en", "english"), and a result is never left out because its release name differs from your file: the list is ranked by fit instead, and a subtitle for the hard of hearing stays in the list, just below an equally good plain one. SubDL gets one lookup per call (the id or the title, never both together). If every lookup finds nothing, SubDL is asked once more with as few filters as possible, and Mediarium checks the language itself.

**When a search finds nothing.** Under "No subtitles found" there is a collapsed **Details** line. It lists, for each site and each lookup, what was asked and what came back, for example "SubDL: by TMDB id 661: 0 results, the site answered normally" or "SubDL: by title "The Sixth Sense (1999)": 2 found, all dropped: wrong language (found: FR, DE)". It never shows a key. If you report a missing subtitle, paste these lines. The same lines go to the log. Scripts: `GET /api/movies/{id}/subtitles` and the episode version answer `{"results": [...], "diagnostics": [{"source", "lookup", "value", "bare", "count", "found", "note", "detail"}]}` (they used to answer the list alone).

**The list.** Each result shows the site it came from; for an add-on that is the add-on's own name. Rating is left empty when the source gives nothing to base it on: add-ons and SubDL carry no rating. Fit says how well the subtitle's release name matches your video (release group, source, resolution, codec); higher is better. Your library file is named by Mediarium, so its name usually says nothing about the release. Fit therefore also compares with the name of the release the file was downloaded as, and with the quality stored for the file (such as Bluray-1080p, which counts for source and resolution), and takes the best of those. A file that was imported from an existing library has no release name, so there Fit only has the stored quality to go on, and stays empty when that matches nothing.

## Subtitle add-ons

For a site Mediarium has no built-in support for, such as [subs.ro](https://subs.ro) or a Hebrew or Arabic subtitle site, you can add an **add-on**. An add-on is a small web service that follows the Stremio add-on protocol: Mediarium asks it for the subtitles of one title and it answers with links. A lot of sites have one, so a single feature covers them all.

To add one, open **Settings > Info, lists and subtitles > Subtitles**, find the **Subtitle add-ons** card, paste the add-on's address and press **Add add-on**. The address ends in `/manifest.json`; an address that starts with `stremio://` works too. Mediarium reads the manifest first and only accepts add-ons that offer subtitles. **Test** on a row checks that the add-on still answers, and **Remove** takes it out. You can add up to eight.

For subs.ro, create a free account on subs.ro, get your API key from subs.ro/api, install its Stremio add-on from the add-on's page with that key, and paste the address it gives you (it contains the key).

Add-ons are searched together with the other sources that are switched on, in automatic downloads and in the list you pick from by hand, where each result says which site it's from. They are asked with the title's IMDb id (read from TMDB), plus the file name and size when there is a file. Anything in a language you didn't pick is left out. Downloads can be SRT, WebVTT (converted to SRT) or a zip (for an episode, the file named for that episode). Subtitles saved in an older Windows code page, which is common for Romanian, Polish, Greek and Hebrew, are converted to UTF-8 so players show the letters correctly. The same goes for Chinese (GBK, or Big5 for Traditional), Japanese (Shift-JIS), Korean (EUC-KR) and Thai. A language Mediarium knows no older code page for is saved as it came, and a UTF-8 file with a single damaged byte is kept as UTF-8.

The address can hold your own key for the site, so the list is stored encrypted (`subtitles.addons`) and only the host is shown. Download links in search results are sealed with the server's key, so only links an add-on really offered can be fetched. For scripts: an administrator can list them with `GET /api/subtitles/addons` and add one with `POST /api/subtitles/addons` with `{"url": "..."}`, `DELETE /api/subtitles/addons/{id}` and `POST /api/subtitles/addons/{id}/test`. Results carry `"source": "addon"` and a `link` to pass back as it is.

## Hebrew subtitles

Hebrew has two big subtitle sites, Wizdom and Ktuvit, and OpenSubtitles and SubDL don't have everything they do. Here is how to get at both.

**Wizdom (built in).** [Wizdom](https://wizdom.xyz) has a public API that needs no key and no account, so Mediarium can ask it directly. It's off until you switch it on: open **Settings > Info, lists and subtitles > Subtitles**, find the **Wizdom (Hebrew)** card and turn on **Look for Hebrew subtitles on Wizdom**. **Test** checks that the site answers. You also need Hebrew among your subtitle languages.

- Wizdom is only asked when the language being searched is Hebrew. For every other language it's left out.
- It's searched together with your other sources, in automatic downloads and in the list you pick from by hand, where the result says it's from Wizdom. The title is looked up by its IMDb id (read from TMDB), then the best fit for your file is picked by release name, as with the other sources. Wizdom has no file hash, so "made for this exact file" only comes from OpenSubtitles.
- Its downloads are zip files. Mediarium takes the SRT out (for an episode, the file named for that episode). Many Hebrew files are saved in the old Windows Hebrew code page (windows-1255); those are converted to UTF-8 so players show the letters, and a file that's already UTF-8 is left alone. The text itself, including the marks that make it read right to left, is not changed.
- You can use Wizdom without an OpenSubtitles or SubDL key.
- Mediarium sends one search per title and one request per download, and says who it is in the User-Agent (`Mediarium`). Wizdom publishes no terms of use and no rate limit that I could find, so please don't run huge sweeps against it. If the site is down, a search just comes back empty or with an error for that source, and the other sources carry on.

**Ktuvit (through an add-on).** [Ktuvit](https://www.ktuvit.me) isn't built in. It has no public API: its search is a web page, the download needs a signed-in account, and the sign-in wants a hashed form of your password rather than the password itself. A built-in version would mean scraping its pages with your login, which breaks whenever the site changes, and its site says nothing about automated use. Use an add-on instead (see [Subtitle add-ons](#subtitle-add-ons)):

1. Open the add-on directory at [stremio-addons.net](https://stremio-addons.net) and search for "Hebrew". Community add-ons that serve Hebrew subtitles include *Hebrew Subs Multi* (it searches Wizdom, OpenSubtitles, SubDL and a few other sites, and Ktuvit as an option) and *ktuvit-stremio* (Ktuvit only). They're run by other people and come and go, so check that one is still being updated.
2. On the add-on's page, copy its address (the install link, or the one that ends in `/manifest.json`; `stremio://` addresses work too). Some of them ask you to enter your own Ktuvit sign-in on a setup page first, and then give you an address that has it built in.
3. In **Settings > Info, lists and subtitles > Subtitles**, paste it into the **Subtitle add-ons** card and press **Add add-on**.

Add-on subtitles are matched by IMDb id and converted from windows-1255 to UTF-8 the same way. If you use an add-on that already includes Wizdom, you can leave the **Wizdom** switch off.

For scripts: the switch is the setting `subtitles.wizdom` (`subtitleWizdom` in `GET`/`PUT /api/settings`), and `POST /api/settings/test-service` with `{"service": "wizdom"}` tests it. Results from Wizdom carry `"source": "wizdom"` and a `link` (the subtitle's number on Wizdom); download one with `{"source": "wizdom", "link": "67081", "language": "he"}`.

## Subtitles that come with a release

- The language is read from the file name or its folder: `English.srt`, `2_English.srt`, `Subs/French/track.srt`, `movie.en.srt`, `movie.pt-BR.srt`, `Deutsch.srt`. ISO codes (`en`, `eng`, `fre`, `fra`) and language names (`English`, `Português`) both work. A language in the middle of a release name (`Movie.2020.FRENCH.1080p`) is ignored, because it usually describes the audio.
- Every language Mediarium recognises is imported, wanted or not. Only the languages you picked count as done, so a French file doesn't stop an English subtitle from being offered.
- A `forced` file (foreign-language lines only) is kept as `Movie.en.forced.srt` and doesn't count as a full subtitle. A `sdh` (hearing-impaired) file is kept as `Movie.en.sdh.srt` and counts as a full subtitle unless you chose [**Normal only**](#normal-sdh-and-forced-subtitles) for that language.
- In a season pack, a subtitle goes to the episode named in its file name or folder (`S01E02`). One that names no episode is skipped rather than guessed. In a single-episode or movie download, the subtitles belong to that video.
- Sample clips are ignored. Existing files are never overwritten, and nothing in the download folder is changed or deleted.
- Activity records it, for example: `Movie imported with 2 subtitle file(s): en, fr`. A subtitle Mediarium downloads is logged with the service it came from, for example `Downloaded en subtitle for Heat from SubDL`, so you can see over time which service gives you the most.

## Getting subtitles from OpenSubtitles

Nothing spends your small daily allowance unless you ask, or tick the automatic option.

- The dashboard shows "N downloaded titles have no subtitles yet" with a **Get subtitles** button that opens the Subtitles tab of the Wanted page. Its second line only mentions the daily limit of OpenSubtitles while OpenSubtitles is switched on; with only SubDL or add-ons on, there is no count.
- There you can fetch subtitles for the titles you select, or for everything missing. One request works for about a minute, stops at the daily limit and tells you how many are still waiting, so you can run it again later.
- A manual request also retries titles OpenSubtitles had nothing for lately. The automatic sweep waits three days before asking again.

To let Mediarium do it for you, tick **Download after each import, and look again every few hours for anything missing** under **Languages** on the Subtitles page, then press **Save languages**. It fetches after each import and sweeps every six hours, always within the daily limit. **Search for missing subtitles now** starts a sweep at once (it needs at least one [source](#choosing-your-sources) switched on).

## Subtitles made for your exact file

When Mediarium searches, it also sends a fingerprint of the video file (OpenSubtitles' file hash, read from the first and last 64 KB). A subtitle someone timed against that exact file is marked **made for this file** in the search list and is always picked first, because it is in sync.

Often there is no such subtitle yet, and Mediarium picks the one whose release name fits best. With automatic downloading on, the box **For a month after, swap a subtitle for one made for that exact video file if one turns up** (on by default) has it look again every three days for a month. When one appears it replaces the earlier pick and the swap is written to Activity. It only touches subtitles it picked itself: one you chose by hand, moved in time or edited, or one next to a video that was replaced, is left alone. A run swaps at most five and keeps a few of the day's downloads free for new imports.

## Daily limits and the free account

OpenSubtitles allows about **5** downloads per 24 hours per internet address without an account and about **20** with a free OpenSubtitles.com account (VIP accounts get more). Requests are also paced to under 5 per second. These limits belong to OpenSubtitles, so they only count while it is [switched on](#choosing-your-sources).

Mediarium counts what it has used and remembers the figures OpenSubtitles reports with each download (how many are left and when the limit resets), even after a restart. `GET /api/subtitles/quota` returns them, with how many titles and subtitle files are still wanted. When more is wanted than the day allows, it says so, for example: "42 subtitles wanted, 3 downloads left today. At 5 a day that takes 9 days. A free OpenSubtitles account raises it to about 20 a day."

To add your account, enter your OpenSubtitles.com username and password under **Your OpenSubtitles.com account (optional)** on the Subtitles page and press **Save account**. **Test login** checks it. The password is stored encrypted.

## Shared keys

Official builds include an OpenSubtitles key (and a Trakt key) that everyone shares. When one reaches its limit, the dashboard says so and suggests your own free key. Enter yours in the OpenSubtitles card on the Subtitles page (Trakt's is on **Import lists**). **Switch back to the shared key** on the same card undoes it. `GET /api/usage` shows the requests made and limits reached per service in the last 24 hours.

## Subtitles in the player

The player on a title's **Files** card offers the subtitle files saved next to the video (named like `Film (2024).en.srt` or `Show - S01E01.fr.srt`, SRT or VTT) in its own subtitle menu (CC), the first one on. SRT files are converted to WebVTT in the browser, since browsers only read that. Subtitles inside the video file are shown by the browser too, when it can play that container. Other formats, such as `.ass`, aren't offered; the video's format still decides whether the browser can play it at all.

## Fixing a subtitle that is out of time

On a movie's or show's page, open **Files**. Each `.srt` or `.vtt` subtitle has a **Timing** button:

- **Move by ... seconds**, then **Earlier** or **Later**, shifts every line by that much (tenths of a second work, for example 1.5).
- **Line up with** another subtitle of the same title that is in time (for example the English one that came with the release): Mediarium compares when the lines start, finds the best offset, and also fixes a subtitle made for another frame rate (25 against 23.976 frames a second), which drifts further out as the film goes on. If the two don't fit together well (less than 40% of the lines match, for example subtitles for another cut of the film), nothing is changed and it says so.
- **Put the original back** undoes all of it. The first time a subtitle is changed, the original is kept next to it as `<name>.srt.bak`.

Only the times change; the text, styling and numbering stay as they are. Accounts need the **Subtitles** permission. Scripts: `POST /api/subtitles/timing` with `kind` (`movie` or `series`), `id`, `file` (relative to the title's folder) and one of `shiftMs`, `reference` or `undo: true`.

## Marking a title "no subtitles wanted"

For a title that doesn't need subtitles, such as a film you watch in the original language, select it on the Subtitles tab (Upcoming > Wanted > Subtitles) and press **Not needed** (administrators only). It then stays out of the list, the dashboard offer, the automatic sweep and **Get all**. To undo it, tick **Show titles marked as not needed**, select the title and press **Put back on the list**. Over the API: `POST /api/subtitles/dismiss` and `DELETE /api/subtitles/dismiss` with `{"items":[{"kind":"movie","id":1}]}` (`kind` is `movie` or `episode`).
