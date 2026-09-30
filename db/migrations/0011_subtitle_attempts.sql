-- Remembers when we last looked for a subtitle in a language for an item, so
-- the periodic sweep does not re-ask OpenSubtitles (and burn its daily
-- download quota) for something it just failed to find.
CREATE TABLE subtitle_attempts (
    kind         TEXT NOT NULL,             -- 'movie' or 'episode'
    media_id     INTEGER NOT NULL,
    language     TEXT NOT NULL,
    attempted_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (kind, media_id, language)
);
