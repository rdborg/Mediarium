# Downloads

How Mediarium's built-in downloaders use your network and disk: the torrent port, seeding, what happens to downloaded files after they are imported, the clean-up, and what removing a title deletes.

## Torrent port (58264)

All torrents run in one built-in engine that listens on **port 58264, TCP and UDP**. The port is in the range that is never assigned to any service (49152 to 65535), so it is unlikely to clash with anything else, and it echoes the web interface's 8264.

- **It is optional.** Torrents download without it: Mediarium connects out to peers either way. Publishing the port lets other peers connect **to you** as well, which finds more peers (faster downloads, fewer stalled torrents) and lets you seed back properly.
- **Docker:** the compose files in this repository publish it for you:
  ```yaml
  ports:
    - "8264:8264"
    - "58264:58264/tcp"
    - "58264:58264/udp"
  ```
  With `docker run`, add `-p 58264:58264/tcp -p 58264:58264/udp`. Keep the same number on both sides of the colon.
- **Router:** for peers on the internet to reach you, forward port 58264 (TCP and UDP) on your router to the machine running Mediarium, as you would for any torrent client.
- **Changing it:** Settings > Downloads & VPN > Listen port (stored as `torrent.listen_port`). If you change it, publish the new number instead (for example `51413:51413/tcp` and `/udp`). An empty value or `0` means the default, 58264; installs that had saved `0` (which used to mean "any port") now use 58264. A port you had saved yourself is kept.
- **If the port is taken** by another program on the same machine, the engine falls back to a port the system picks and says so in the log; Settings > Downloads & VPN then shows the port actually in use.
- **Is it working?** Settings > Downloads & VPN shows whether an incoming connection has been seen since Mediarium started (`GET /api/downloads/status` answers `torrent.listenPort`, `torrent.listening`, `torrent.activePort`, `torrent.activeTorrents`, `torrent.incomingSeen` and `torrent.lastIncomingAt`). The engine only listens while a torrent is downloading or seeding, and a peer only connects when it wants a torrent you have, so "not seen yet" right after starting is normal; after a while of seeding it usually means the port is not reachable (not published, not forwarded, or blocked by a firewall).
- **With the VPN kill switch on** (Settings > VPN, "require VPN for torrents"), torrent traffic only goes out through the tunnel and nothing listens on the host at all, so no incoming connection is ever seen. That is expected.

## Seeding

After a torrent finishes downloading it is imported straight away, and keeps seeding in the background until its **seeding goal** is met: the seed ratio limit (`torrent.seed_ratio_limit`, uploaded divided by size, for example 2.0) or the seed time limit (`torrent.seed_time_limit_h`, hours), whichever comes first. With both at 0 a torrent seeds until Mediarium stops or torrents are switched off. Seeding does not survive a restart. When the goal is met the torrent stops and its data is deleted from the downloads folder (see below).

## What happens to downloaded files

Every download works in its own folder, `incomplete/queue-N` inside the downloads folder. Once its files are in the library (hardlinked, or copied when a hardlink is not possible) that folder is no longer needed:

- **Usenet:** the folder is deleted straight after the import, including an import you finish later from a parked conflict ("Ask me": both **Replace** and **Keep the existing file** remove the download's folder).
- **Torrents:** the data is kept while the torrent seeds, and deleted once its seeding goal is met, provided the download has been imported. A hardlinked file in the library is a second name for the same data, so deleting the download's copy never touches the library file.
- **Failed downloads** keep their folder for a day, so you can retry them or look inside; after that the clean-up removes it.

## Clean up (System > Clean up)

The clean-up looks only inside the working folder (`<downloads>/incomplete`). Nothing else in the downloads folder, and **nothing in the movie or TV library, is ever deleted by it**, even when a library folder sits inside the downloads folder. Symbolic links are removed as links and never followed, and paths that would lead outside (`../`, a linked folder) are refused.

It finds, with the space each one frees (a file that is also hardlinked into the library frees nothing and counts as 0):

| Reason | What it is |
|---|---|
| `imported-leftover` | the folder of a download that was imported and is not seeding any more (after a restart, for example, since seeding does not survive one) |
| `seeding-finished` | a torrent that has reached its seeding goal |
| `orphaned` | a folder or file no download owns any more, or a failed download's folder, unchanged for a day |
| `empty-folder` | a folder with no files in it |

Anything a download is still using (queued, downloading, importing, seeding, or waiting for a conflict decision) is never touched.

- **Automatic:** on by default (`cleanup.auto`), it runs once a day.
- **Clean up now:** runs it straight away.
- **API:** `GET /api/system/cleanup` answers `{reclaimableBytes, items: [{path, sizeBytes, reason}], lastRunAt, auto, historyRetentionDays}` (`path` is relative to the downloads folder, for example `incomplete/queue-12`). `POST /api/system/cleanup` runs it and answers `{removedBytes, removed: [...], prunedQueueItems, prunedActivity, lastRunAt, errors}`. Both are for administrators only.

### History and activity

Each clean-up also drops finished (completed or failed) downloads from the queue history and activity entries older than **90 days** (`cleanup.history_retention_days`; `0` keeps them forever). Downloads still running or waiting for a decision are never dropped. The settings are `cleanupAuto` and `historyRetentionDays` in `PUT /api/settings`.

## Removing a movie or show

Removing a title from the library leaves nothing behind in the downloads folder: its downloads are cancelled (a running Usenet download stops, a torrent stops downloading or seeding), their folders are deleted, failed ones' leftovers included, and they disappear from the queue. The activity list keeps a line saying so.

With **Also delete everything on disk** (`?deleteFiles=true`, ticked by default in the app) the title's files go too:

- **A movie in its own folder** (`Movie (Year)/`): the whole folder, with subtitles, `.nfo`, artwork and extras.
- **A show:** the show's folder with every season, subtitle, `.nfo` and artwork.
- **A file loose in the library folder, or in a folder shared with other titles:** only the video and the files named after it (`Movie (Year).en.srt`, `Movie (Year).nfo` and so on); other titles' files are left alone. A folder counts as shared when another title in the library has a file in it, or when it holds a video named after something else.

The library folder itself is never deleted. A file that is not inside the movie or TV folder, or is reached through a symbolic link leading out of it, is never deleted either: the removal is refused (409) and nothing is changed, so you can remove it by hand or remove the title without deleting files.
