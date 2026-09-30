-- Releases that failed because the release itself was bad (missing
-- articles, failed repair, no video inside...). Automation never grabs a
-- blocklisted release again; a user can still pick one manually.
CREATE TABLE blocklist (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    release_title TEXT NOT NULL,
    title_key     TEXT NOT NULL UNIQUE,      -- lower-cased, trimmed release title
    protocol      TEXT NOT NULL DEFAULT 'usenet',
    reason        TEXT NOT NULL DEFAULT '',
    movie_id      INTEGER REFERENCES movies(id) ON DELETE CASCADE,
    series_id     INTEGER REFERENCES series(id) ON DELETE CASCADE,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
