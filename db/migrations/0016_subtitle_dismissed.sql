-- Titles the person chose not to get subtitles for ("no subtitles wanted").
-- They are left out of the Wanted list, the health offer and every automatic
-- or manual bulk fetch.
CREATE TABLE subtitle_dismissed (
    kind         TEXT NOT NULL,             -- 'movie' or 'episode'
    media_id     INTEGER NOT NULL,
    dismissed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (kind, media_id)
);

-- One row per subtitle download OpenSubtitles counted against the daily limit,
-- so Mediarium can estimate how many are left when OpenSubtitles has not said.
CREATE TABLE subtitle_downloads (
    downloaded_at TEXT NOT NULL
);
CREATE INDEX idx_subtitle_downloads_at ON subtitle_downloads (downloaded_at);

-- The last quota OpenSubtitles reported in a download response (a single row),
-- kept so the remaining count survives a restart.
CREATE TABLE subtitle_quota (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    remaining   INTEGER NOT NULL,
    requests    INTEGER,                    -- downloads used in the window, when reported
    reset_at    TEXT,                       -- when the window resets, when reported
    observed_at TEXT NOT NULL
);
