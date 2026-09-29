# Activity per title, one download at a time, retries

## A title's own activity log

Every movie and show keeps its own log of what happened to it, newest first. It answers "why has this not downloaded yet?" without reading server logs.

- `GET /api/movies/{id}/events` and `GET /api/series/{id}/events` (every account can read them) answer a list, newest first, of at most 200 events: `[{"at": "2026-09-29T08:30:00.000Z", "kind": "searched", "message": "...", "level": "info"}]`. `level` is `info`, `warn` or `error`. A show's log covers all its episodes.

What is recorded (`kind`):

| kind | when | example |
|---|---|---|
| `searched` | each automatic search (the scheduled search, **Search now**, the search after adding a title, the retry after a bad release) | `Searched: 12 releases, none acceptable: 8 CAM/TeleSync, 4 wrong year` (warn), or `Searched: 5 releases, 2 acceptable; picked "..." from MyIndexer with the "1080p" profile` |
| `grabbed` | a release was queued, by hand or automatically; a grab that only a fallback profile accepted says which profile it used | `Grabbed "..." for ...` |
| `download` | the download started, and finished (post-processing starts) | `Download started: <release>` |
| `postprocess` | PAR2 verify or repair, unpacking, and their failures | `Repairing 3 missing article(s) with PAR2: <release>`, `Unpacking failed (...): <release>` (error) |
| `imported`, `failed`, `conflict` | the end of a download, with the plain reason when it failed | `The Film: unpack: ...` (error) |
| `blocklisted` | a release was blocklisted, automatically (its own fault: corrupt, incomplete) or by you | |
| `retry` | after a blocklisted release, why no other release was taken right away | `No other acceptable release; will try again at the next scheduled search` (warn) |
| `retried` | **Retry** on a failed download, and whether it reused the files already downloaded | `Retrying from the files already downloaded: <release>` |
| `removed` | a download was removed from the download list | |

The reasons a search lists are, per release: `blocklisted`, a downloader the title does not use (`from torrent sites (this title uses Usenet only)`), `wrong year`, `for another show`, `not this episode`, `not a season pack`, a quality no profile in the title's chain accepts (its tier, such as `CAM/TeleSync`, or `unknown quality`), `excluded by the profile's release terms`, and for upgrade searches `not an upgrade over <tier>`. Indexers that did not answer are named too.

A search with exactly the same outcome as a recent one (the scheduled search every half hour finding the same releases) moves that event's time forward instead of adding another. The detailed events (`searched`, `download`, `postprocess`, `retry`, `retried`, `removed`) are shown only on the title's own log; the global activity feed (`GET /api/activity`) keeps showing grabs, imports, failures and blocklisting. Each title keeps its latest 300 detailed events.

## One download per title

A movie, or an episode, never has two downloads running at once:

- Automatic searches skip a movie or episode that is already queued, downloading or being post-processed.
- A grab by hand (from a title's release list, the search page, or **Retry**) is refused with `409` and the message `Already downloading: <release>. Cancel it first to pick another.`
- A season pack covers every episode of its season: while a pack is downloading, a single episode of that season cannot be grabbed, and neither can another pack of the same season.

## Retry without downloading again

**Retry** on a failed Usenet download whose files were all downloaded and are still in its working folder (the failure came later: PAR2 repair, unpacking or the import) does not download them again. It checks them with PAR2 again and repeats post-processing (repair, unpack, import). Only when the files are gone, or fail the PAR2 check, is the release downloaded again. Torrents are always downloaded again (their data is shared with seeding).

## When a release turns out to be bad

When an automatic download fails because of the release itself (corrupt or incomplete: PAR2 cannot repair it, the archive cannot be unpacked, no video file in it), the release is blocklisted and Mediarium immediately searches once more for another acceptable release. If there is none, the title's log says `No other acceptable release; will try again at the next scheduled search`, and the regular search (every 30 minutes) keeps looking. If more than a few releases for the same title failed within a day, the immediate retry is skipped (and the log says so) until the next scheduled search.
