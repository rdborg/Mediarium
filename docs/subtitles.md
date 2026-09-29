# Subtitles

How Mediarium gets subtitles for your movies and episodes, what limits apply, and how to stay in control of it.

## Where subtitles come from

1. **The release itself.** Many downloads already contain subtitle files. When a movie or episode is imported, Mediarium copies the subtitle files it finds in the finished download (`.srt`, `.ass`, `.ssa`, `.sub` with `.idx`, and `.sup`) next to the video, named the way Plex, Jellyfin and Kodi expect: `Movie (2020).en.srt`, `Movie (2020).fr.srt`. This costs nothing and uses no download allowance.
2. **OpenSubtitles.** For languages the release did not include, Mediarium can download a subtitle from OpenSubtitles.com, picking the one that best fits your file (release group, source, codec).

## Subtitles that come with a release

- The language is read from the file name or the folder it sits in: `English.srt`, `2_English.srt`, `Subs/French/track.srt`, `movie.en.srt`, `movie.pt-BR.srt`, `Deutsch.srt`. ISO codes (`en`, `eng`, `fre`, `fra`) and language names (`English`, `Português`) both work. A language mentioned only in the middle of a release name (`Movie.2020.FRENCH.1080p`) is ignored, because it usually describes the audio.
- Every language Mediarium can recognise is imported, whether or not it is on your wanted list. Only the languages you chose count as "done" (so a French file does not stop an English subtitle being offered).
- A `forced` file (foreign-language lines only) is kept as `Movie.en.forced.srt` but does not count as a full subtitle. A `sdh` or hearing-impaired file is kept as `Movie.en.sdh.srt` and does count.
- In a season pack, a subtitle is given to the episode named in its file name or folder (`S01E02`). A subtitle that names no episode in a pack is skipped rather than guessed. For a single-episode or movie download, the subtitles belong to that one video.
- Sample clips are ignored. Existing files are never overwritten, and nothing in the download folder is changed or deleted.
- The activity log records it, for example: `Movie imported with 2 subtitle file(s): en, fr`.

## Offered, not fetched

By default Mediarium does **not** download from OpenSubtitles on its own, so nothing spends your small daily allowance without you asking. Instead:

- The dashboard shows "N downloaded titles have no subtitles yet" with a **Get subtitles** button that opens the Subtitles tab of the Wanted page.
- On that page you can fetch subtitles for selected titles, or for everything missing. One request works for about a minute, stops cleanly when the daily limit is reached and tells you how many are still waiting, so you can run it again later.
- A manual request also retries titles OpenSubtitles had nothing for recently (the automatic sweep waits three days before asking again).

To have Mediarium fetch them for you, turn on automatic download in **Settings, Subtitles**. It then fetches after each import and runs a sweep every six hours, always within the daily limit.

## Daily limits and the free account

OpenSubtitles limits downloads per 24 hours: about **5** per internet address without a personal account, about **20** with a free OpenSubtitles.com account (VIP accounts get more). Requests are also paced to stay under 5 per second.

Mediarium tracks how many it has used, and remembers the figures OpenSubtitles reports with each download (how many remain and when the limit resets), even after a restart. `GET /api/subtitles/quota` returns them, together with how many titles and subtitle files are still wanted. When more is wanted than the day allows, it says so in plain words, for example: "You have 42 subtitles to get but 3 downloads left today. At 5 a day that takes 9 days. A free OpenSubtitles account raises it to about 20 a day."

To add your account, open **Settings, Subtitles** and enter your OpenSubtitles.com username and password.

## Shared keys

Official builds include an OpenSubtitles key (and a Trakt key) shared by everyone using Mediarium. If one of them reaches its limit, the dashboard says so and suggests using your own free key, which you can enter under Settings and clear again later to go back to the shared one. `GET /api/usage` shows, per service, the requests made and limits reached in the last 24 hours.

## Marking a title "no subtitles wanted"

If you do not want subtitles for a title (for example a film you watch in the original language), mark it in the Subtitles tab. It is then left out of the Wanted list, the dashboard offer, the automatic sweep and "get all". Choose to show dismissed titles to undo it. Over the API: `POST /api/subtitles/dismiss` and `DELETE /api/subtitles/dismiss` with `{"items":[{"kind":"movie","id":1}]}` (`kind` is `movie` or `episode`).
