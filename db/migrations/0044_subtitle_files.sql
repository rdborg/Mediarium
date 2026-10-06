-- The subtitles Mediarium downloaded, so a better one can replace a guessed
-- one later (one made for that exact video file). A subtitle someone picked
-- by hand is never replaced.
CREATE TABLE subtitle_files (
    kind          TEXT    NOT NULL,             -- "movie" or "episode"
    media_id      INTEGER NOT NULL,
    language      TEXT    NOT NULL,
    file_id       INTEGER NOT NULL,             -- OpenSubtitles file id
    hash_match    INTEGER NOT NULL DEFAULT 0,   -- 1 when made for this exact video file
    chosen        INTEGER NOT NULL DEFAULT 0,   -- 1 when picked by hand
    video_hash    TEXT    NOT NULL DEFAULT '',  -- the video's OpenSubtitles hash at the time
    sub_path      TEXT    NOT NULL,
    sub_size      INTEGER NOT NULL DEFAULT 0,   -- to notice a subtitle changed since
    sub_mtime     INTEGER NOT NULL DEFAULT 0,
    downloaded_at TEXT    NOT NULL,
    checked_at    TEXT    NOT NULL DEFAULT '',  -- last looked for a better one
    PRIMARY KEY (kind, media_id, language)
);
