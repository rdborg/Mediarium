-- Channel archiver (V4 D21): the channels and playlists that are followed, and
-- the videos seen in them. A video id is stored once it has been seen, so it is
-- never listed as new again.
CREATE TABLE archiver_sources (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    url          TEXT    NOT NULL UNIQUE,
    name         TEXT    NOT NULL DEFAULT '',   -- the folder name; filled in from the first listing
    format       TEXT    NOT NULL,              -- video, mp3 or m4a
    backlog      INTEGER NOT NULL DEFAULT 0,    -- 1: also download the videos that were already there
    added_at     TEXT    NOT NULL,
    last_checked TEXT    NOT NULL DEFAULT '',   -- RFC 3339, empty until the first check
    status       TEXT    NOT NULL DEFAULT '',   -- '', ok or error
    message      TEXT    NOT NULL DEFAULT ''    -- plain words about the last check
);

CREATE TABLE archiver_videos (
    source_id INTEGER NOT NULL REFERENCES archiver_sources(id) ON DELETE CASCADE,
    video_id  TEXT    NOT NULL,
    title     TEXT    NOT NULL DEFAULT '',
    url       TEXT    NOT NULL DEFAULT '',      -- the video's own address, kept for websites other than YouTube
    state    TEXT    NOT NULL,                 -- queued, done, failed or skipped (older than the follow)
    attempts  INTEGER NOT NULL DEFAULT 0,
    error     TEXT    NOT NULL DEFAULT '',      -- the last lines yt-dlp said, shortened
    path      TEXT    NOT NULL DEFAULT '',      -- where the file went
    seen_at   TEXT    NOT NULL,
    rank      INTEGER NOT NULL DEFAULT 0,       -- place in the listing, 0 = newest
    done_at   TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (source_id, video_id)
);
CREATE INDEX archiver_videos_state ON archiver_videos (state, seen_at);
