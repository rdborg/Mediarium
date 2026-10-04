-- "Not interested": movies and shows that Discover no longer shows.
CREATE TABLE exclusions (
    kind       TEXT NOT NULL,          -- "movie" or "tv"
    tmdb_id    INTEGER NOT NULL,
    title      TEXT NOT NULL,
    year       INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (kind, tmdb_id)
);
