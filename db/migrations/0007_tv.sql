-- TV support (PRD.md §7 Phase 2/3): series + per-episode tracking, same
-- status lifecycle as movies (missing -> downloading -> downloaded).
-- Metadata comes from TMDB (PRD §4.4 — "TMDB (movies/TV)"), so series are
-- keyed by tmdb_id, not tvdb_id.

CREATE TABLE series (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    tmdb_id        INTEGER NOT NULL UNIQUE,
    title          TEXT NOT NULL,
    year           INTEGER,
    overview       TEXT,
    poster_path    TEXT,
    first_air_date TEXT,
    monitored      INTEGER NOT NULL DEFAULT 1,
    added_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE episodes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id  INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    season     INTEGER NOT NULL,
    episode    INTEGER NOT NULL,
    title      TEXT,
    overview   TEXT,
    air_date   TEXT, -- "YYYY-MM-DD" from TMDB, empty if unannounced
    status     TEXT NOT NULL DEFAULT 'missing', -- missing|downloading|downloaded
    quality    TEXT,
    file_path  TEXT,
    monitored  INTEGER NOT NULL DEFAULT 1,
    UNIQUE (series_id, season, episode)
);

-- A queue item is now either a movie grab (movie_id set) or a TV grab
-- (series_id + season set; episode NULL means a whole-season pack).
ALTER TABLE download_queue ADD COLUMN series_id INTEGER REFERENCES series(id) ON DELETE SET NULL;
ALTER TABLE download_queue ADD COLUMN season INTEGER;
ALTER TABLE download_queue ADD COLUMN episode INTEGER;
