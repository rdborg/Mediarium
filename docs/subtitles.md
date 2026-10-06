# Subtitles

Mediarium can save subtitles next to your movies and episodes. This page covers where they come from, the daily limits and how to control it.

## The switch

Turn subtitles on under **Settings > Info, lists and subtitles > Subtitles**, with **Subtitles for my movies and shows**. With it off, Mediarium doesn't fetch subtitles and hides the parts of the app that deal with them: the Wanted page has no Subtitles tab, and title pages have no subtitle tools. Your account, key and language choices are kept for when you turn it on again.

Subtitles that come inside a download are always imported and always listed in a title's files.

For scripts: the switch is the setting `subtitles.enabled` (`subtitlesEnabled` in `GET`/`PUT /api/settings`). While it is off, `/api/subtitles/*`, `/api/movies/{id}/subtitles*` and `/api/episodes/{id}/subtitles*` answer `409` with "Subtitles are switched off".

## Where subtitles come from

1. **The release itself.** When a movie or episode is imported, Mediarium copies the subtitle files from the finished download (`.srt`, `.ass`, `.ssa`, `.sub` with `.idx`, and `.sup`) next to the video, named the way Plex, Jellyfin and Kodi expect: `Movie (2020).en.srt`, `Movie (2020).fr.srt`. This is free and uses none of your daily allowance.
2. **OpenSubtitles.** For languages the release didn't include, Mediarium downloads the subtitle that fits your file best (release group, source, codec).

## Subtitles that come with a release

- The language is read from the file name or its folder: `English.srt`, `2_English.srt`, `Subs/French/track.srt`, `movie.en.srt`, `movie.pt-BR.srt`, `Deutsch.srt`. ISO codes (`en`, `eng`, `fre`, `fra`) and language names (`English`, `Português`) both work. A language in the middle of a release name (`Movie.2020.FRENCH.1080p`) is ignored, because it usually describes the audio.
- Every language Mediarium recognises is imported, wanted or not. Only the languages you picked count as done, so a French file doesn't stop an English subtitle from being offered.
- A `forced` file (foreign-language lines only) is kept as `Movie.en.forced.srt` and doesn't count as a full subtitle. A `sdh` (hearing-impaired) file is kept as `Movie.en.sdh.srt` and does count.
- In a season pack, a subtitle goes to the episode named in its file name or folder (`S01E02`). One that names no episode is skipped rather than guessed. In a single-episode or movie download, the subtitles belong to that video.
- Sample clips are ignored. Existing files are never overwritten, and nothing in the download folder is changed or deleted.
- Activity records it, for example: `Movie imported with 2 subtitle file(s): en, fr`.

## Getting subtitles from OpenSubtitles

Nothing spends your small daily allowance unless you ask, or tick the automatic option.

- The dashboard shows "N downloaded titles have no subtitles yet" with a **Get subtitles** button that opens the Subtitles tab of the Wanted page.
- There you can fetch subtitles for the titles you select, or for everything missing. One request works for about a minute, stops at the daily limit and tells you how many are still waiting, so you can run it again later.
- A manual request also retries titles OpenSubtitles had nothing for lately. The automatic sweep waits three days before asking again.

To let Mediarium do it for you, tick **Download after each import, and look again every few hours for anything missing** under **Languages** on the Subtitles page, then press **Save languages**. It fetches after each import and sweeps every six hours, always within the daily limit. **Search for missing subtitles now** starts a sweep at once (it needs an OpenSubtitles key).

## Subtitles made for your exact file

When Mediarium searches, it also sends a fingerprint of the video file (OpenSubtitles' file hash, read from the first and last 64 KB). A subtitle someone timed against that exact file is marked **made for this file** in the search list and is always picked first, because it is in sync.

Often there is no such subtitle yet, and Mediarium picks the one whose release name fits best. With automatic downloading on, the box **For a month after, swap a subtitle for one made for that exact video file if one turns up** (on by default) has it look again every three days for a month. When one appears it replaces the earlier pick and the swap is written to Activity. It only touches subtitles it picked itself: one you chose by hand, moved in time or edited, or one next to a video that was replaced, is left alone. A run swaps at most five and keeps a few of the day's downloads free for new imports.

## Daily limits and the free account

OpenSubtitles allows about **5** downloads per 24 hours per internet address without an account and about **20** with a free OpenSubtitles.com account (VIP accounts get more). Requests are also paced to under 5 per second.

Mediarium counts what it has used and remembers the figures OpenSubtitles reports with each download (how many are left and when the limit resets), even after a restart. `GET /api/subtitles/quota` returns them, with how many titles and subtitle files are still wanted. When more is wanted than the day allows, it says so, for example: "42 subtitles wanted, 3 downloads left today. At 5 a day that takes 9 days. A free OpenSubtitles account raises it to about 20 a day."

To add your account, enter your OpenSubtitles.com username and password under **Your OpenSubtitles.com account (optional)** on the Subtitles page and press **Save account**. **Test login** checks it. The password is stored encrypted.

## Shared keys

Official builds include an OpenSubtitles key (and a Trakt key) that everyone shares. When one reaches its limit, the dashboard says so and suggests your own free key. Enter yours in the OpenSubtitles card on the Subtitles page (Trakt's is on **Movie info and lists**). **Switch back to the shared key** on the same card undoes it. `GET /api/usage` shows the requests made and limits reached per service in the last 24 hours.

## Fixing a subtitle that is out of time

On a movie's or show's page, open **Files**. Each `.srt` or `.vtt` subtitle has a **Timing** button:

- **Move by ... seconds**, then **Earlier** or **Later**, shifts every line by that much (tenths of a second work, for example 1.5).
- **Line up with** another subtitle of the same title that is in time (for example the English one that came with the release): Mediarium compares when the lines start, finds the best offset, and also fixes a subtitle made for another frame rate (25 against 23.976 frames a second), which drifts further out as the film goes on. If the two don't fit together well (less than 40% of the lines match, for example subtitles for another cut of the film), nothing is changed and it says so.
- **Put the original back** undoes all of it. The first time a subtitle is changed, the original is kept next to it as `<name>.srt.bak`.

Only the times change; the text, styling and numbering stay as they are. Accounts need the **Subtitles** permission. Scripts: `POST /api/subtitles/timing` with `kind` (`movie` or `series`), `id`, `file` (relative to the title's folder) and one of `shiftMs`, `reference` or `undo: true`.

## Marking a title "no subtitles wanted"

For a title that doesn't need subtitles, such as a film you watch in the original language, select it on the Subtitles tab (Upcoming > Wanted > Subtitles) and press **Not needed** (administrators only). It then stays out of the list, the dashboard offer, the automatic sweep and **Get all**. To undo it, tick **Show titles marked as not needed**, select the title and press **Put back on the list**. Over the API: `POST /api/subtitles/dismiss` and `DELETE /api/subtitles/dismiss` with `{"items":[{"kind":"movie","id":1}]}` (`kind` is `movie` or `episode`).
