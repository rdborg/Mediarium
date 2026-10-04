# The Library page

The Library shows what's in your library: a tab each for **Movies**, **TV** and **Music** (only the kinds that are switched on, see [modules.md](./modules.md)). Every title is a poster or a row, with a status, and the things you do most are one click away: search for a release now, monitor or stop monitoring, open the page, and (for administrators) remove it. Above the list you can search by name, filter by genre, decade and status, and sort. The list is sorted by **Recently added**, newest first, until you pick another sort.

![The Library page on the Movies tab: posters with a Downloaded badge, the Import existing and Add new buttons, and the Select button](images/library.png)

*The Library, Movies tab.*

To do the same thing to many titles at once, use **Select**.

## Finding your way

- **Status words.** Each title says where it is: **Downloaded**, **Downloading**, **Pending** (waiting its turn in the download line), **Waiting for a release** (monitored, and Mediarium checks regularly for one), **Partial** (some episodes are downloaded), **Not monitored** (in your library, but nothing is downloaded on its own) and **Failed**. The chips under the toolbar filter by these words and show how many titles each holds.
- **Clear filters.** When a search, chip, genre or year is narrowing the list, a **Clear filters** button appears beside the chips, and in the "Nothing matches these filters" message when nothing is left. It puts them all back.
- **Sort direction.** The button with the up-and-down arrows next to **Sort** reverses the order (oldest first, Z to A). The sort and its direction are remembered in this browser.
- **Press `/`** anywhere (except while you type in a box) to jump to the search box at the top, and **Esc** to leave it.

## Selecting many titles

**Select** is for administrators. Members see the Library without it.

![The Library in select mode with five movies ticked and the bar showing 5 selected, Monitor, Stop monitoring, Better versions, Quality profile, Download from, Search now and Remove from library](images/library-bulk.png)

*Five movies selected. The bar stays in view while you scroll.*

1. Press **Select** in the toolbar. A tick circle appears on every poster and a tick box on every row of the list view.
2. Tick titles by clicking them. Hold **Shift** and click to tick everything between the last one you ticked and this one. In the list view the box in the header ticks everything in the view.
3. The bar at the top of the page stays in view while you scroll. It says how many are selected ("12 selected") and has:
   - **Select all N in this view** and **Select none**. "This view" is what's showing after your search, genre, year and status filters. The Library isn't split into pages, so that's all of them at once. A line beside the buttons says what that reaches: "Everything in your library is in this view (64 movies)." or, with a filter on, "Filters are on: this view has 7 of the 300 shows in your library."
   - The actions below.
   - **Done**, which leaves select mode and clears the ticks.

Actions only reach titles you can see. If you tick ten titles and then type in the search box, the bar counts, and the actions change, only the ticked titles that still match. Clear the search and the others are still ticked.

On a phone the bar takes one row and the actions fold away behind an **Actions** button.

## What you can do to the selection

| Action | What it does |
|---|---|
| **Monitor** / **Stop monitoring** | Turns automatic searching on or off. Stopping also removes downloads that were only waiting for these titles. |
| **Better versions** | **Look for better versions** lets Mediarium swap a download for a better one, if the title's quality profile allows upgrades. **Leave what I have alone** stops that for these titles, whatever their profile says, and removes waiting downloads that would have replaced a file. |
| **Quality profile** | Gives them a profile, or the default. |
| **Download from** | Usenet only, torrents only, both, or the choice in Settings. |
| **Search now** | Looks for releases of the selected titles that are **monitored and missing something**. A confirmation says how many will be searched, for example "42 of the 64 selected are monitored and missing something". At most **25 titles** are searched per press, and the rest are left to the automatic search. The search runs in the background, one title after another, and the result is written to Activity ("Searched for 25 titles and started 3 downloads."). Only one such search runs at a time. Movies that aren't out yet are skipped. |
| **Remove from library** | Takes them out of the library. See below. |

Every action says how it went. When all went well you get a short message such as **Done: 12 updated.** If some couldn't be done, a note stays on the page with the reason for each ("Alpha: It isn't in your library any more.") until you dismiss it, and the message says how many worked: "Done: 10 updated. 2 titles could not be changed."

### Removing many titles

Removing shows a question with the names of the titles and the number. Downloads running for them are cancelled and their working files in the downloads folder are deleted, whatever you choose.

**Files in your library are only deleted if you tick "Also delete the files of all N from the disk".** That goes for one title or many. When you remove a single title, the box shows how many files and how much space they take in your library (for example "3 files, 11.0 GB"). They go to the recycle bin first (**Activity > Recycle bin**) and are deleted for good after 7 days, so a slip can be undone with **Put back** (see [Removing a movie or show](downloads.md#the-recycle-bin)). When you tick it, a red line repeats how many titles are affected. A title whose files lie outside your library folder is never touched. It's left in place, and the note says why. The others carry on.

Every removal writes a line to **Activity** that says what happened to the files, for one title and for many alike: "Inception removed from library. Its files were deleted (4 files, 1 folder)." or "Inception removed from library. Its files were kept (1 file)." A title with no file on disk says "It had no files on disk."

## More than one library folder

**Settings > Library > Folders and file names** has **More movie folders** and **More TV folders**: extra folders besides the main ones, one per line (another disk, a "Kids" folder, a 4K folder). Each must already exist and, with Docker, be mapped into the container. Up to 10 of each.

- When there are extra folders, the add dialog asks **Keep it in**: the main folder (the default) or one of the extra ones. New downloads for that title go there.
- A movie whose files are already in an extra folder (for example after an import) keeps getting its new files there.
- Everything that protects your files works the same in every folder: removing a title, the recycle bin (each folder has its own, shown in Activity > Recycle bin), renaming, and importing by hand never touch anything outside these folders.
- Stored as `library.movies_extra_paths` and `library.tv_extra_paths`; scripts use `moviesExtraPaths` and `tvExtraPaths` (lists) in `PUT /api/settings`, and `rootPath` when adding a movie or show.

## Renaming existing files

Files keep the name they had when they arrived. After you change the naming preset (Settings > Library > Folders and file names), **Rename existing files** on the same page lists every movie and episode whose name would change, as "old name → new name". Untick any you want to leave alone and press **Rename**. Subtitles, `.nfo` and artwork named after a video move with it, empty folders left behind are removed, and your media server is told. A file is never moved outside the library folder or onto another file; those are listed as not renamed. Scripts: `GET /api/rename?kind=movie|tv` (the preview) and `POST /api/rename` with `{"items": [{"kind": "movie", "id": 12}]}`.

## Statistics

**See statistics** under the dashboard's cards opens the Statistics page: how many movies, shows, episodes and albums you have and how many are downloaded, how much space the library takes, the number of titles at each quality (with their size), and how many downloads finished or failed each month, with how much came from Usenet. The months go back as far as the history is kept (90 days unless changed). Scripts: `GET /api/stats/library`.

## Music

The Music tab has the same **Select** mode for artists, with **Follow**, **Stop following**, **Quality profile** (the music profiles) and **Remove from library**. Removing asks the same way and keeps the album folders unless you tick the box.

## For scripts

All of these are for administrators. A title is `{"kind": "movie" | "tv", "id": n}`. Each request changes all its titles in one database transaction, so a failure part way changes nothing. A title that's not in the library (any more) is skipped and reported, and the rest are changed. Up to 5000 titles per request.

| Request | Body | Notes |
|---|---|---|
| `PUT /api/library/bulk/monitored` | `{items, monitored}` | |
| `PUT /api/library/bulk/no-upgrade` | `{items, noUpgrade}` | `true` leaves the titles alone; `false` looks for better versions again |
| `PUT /api/library/bulk/profile` | `{items, profileId}` | `0` is the default profile; an unknown profile is refused (`400`) and nothing changes |
| `PUT /api/library/bulk/sources` | `{items, sources}` | `""` (default), `usenet`, `torrent` or `both` |
| `POST /api/library/bulk/search-now` | `{items}` | answers `202` at once; `{searching, leftOut, limit, skipped, failed, message}` |
| `POST /api/library/bulk/remove` | `{items, deleteFiles}` | `deleteFiles` is `false` when left out |
| `PUT /api/music/bulk/follow` | `{ids, monitored}` | artists |
| `PUT /api/music/bulk/profile` | `{ids, profileId}` | artists; `0` is the default music profile |
| `POST /api/music/bulk/remove` | `{ids, deleteFiles}` | artists |

Every answer except search-now is `{updated, failed: [{kind, id, title?, reason}], message}`.

## Big lists

`GET /api/movies` and `GET /api/series` are the two biggest answers, and the Library page asks for them every few seconds. They're sent compressed (gzip) when the client accepts it, and carry an `ETag`. A client that sends it back in `If-None-Match` gets `304 Not Modified` with no body while nothing has changed. These two answers are marked `Cache-Control: private, no-cache` (the browser may keep a copy but has to ask first). Every other answer in the API is `no-store`.

The interactive release lists (`GET /api/movies/{id}/search`, `GET /api/series/{id}/search` and `POST /api/music/albums/{id}/search`) answer with a plain list. With `?detail=1` they answer `{results, sources, unanswered: [{name, message}]}`, which also says how many indexers were asked and which of them didn't answer.
