-- Music module: artists -> albums -> tracks, keyed by MusicBrainz ids, with
-- audio quality profiles of their own (music tiers are not video tiers).
-- The built-in profiles are seeded by the app from internal/music, so their
-- tier lists are defined in one place.

CREATE TABLE music_profiles (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL UNIQUE,
    allowed_tiers   TEXT NOT NULL,          -- comma-separated tier names
    cutoff          TEXT NOT NULL,
    upgrade_allowed INTEGER NOT NULL DEFAULT 1,
    fallback        TEXT NOT NULL DEFAULT '[]' -- JSON array of profile ids, tried in order
);

CREATE TABLE artists (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    mbid           TEXT NOT NULL UNIQUE,     -- MusicBrainz artist id
    name           TEXT NOT NULL,
    sort_name      TEXT NOT NULL DEFAULT '',
    disambiguation TEXT NOT NULL DEFAULT '',
    monitored      INTEGER NOT NULL DEFAULT 1,
    monitor_new    INTEGER NOT NULL DEFAULT 1, -- albums found later start monitored
    profile_id     INTEGER REFERENCES music_profiles(id) ON DELETE SET NULL, -- NULL = the default profile
    added_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    added_by       INTEGER
);

CREATE TABLE albums (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    artist_id    INTEGER NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    mbid         TEXT NOT NULL,                  -- MusicBrainz release-group id
    release_mbid TEXT NOT NULL DEFAULT '',       -- the release whose tracklist is used, once known
    title        TEXT NOT NULL,
    type         TEXT NOT NULL DEFAULT 'album',  -- album|ep|single
    release_date TEXT NOT NULL DEFAULT '',       -- "YYYY", "YYYY-MM" or "YYYY-MM-DD"; empty if unknown
    monitored    INTEGER NOT NULL DEFAULT 1,
    status       TEXT NOT NULL DEFAULT 'missing', -- missing|downloading|downloaded
    quality      TEXT NOT NULL DEFAULT '',
    path         TEXT NOT NULL DEFAULT '',       -- the album's folder once imported
    UNIQUE (artist_id, mbid)
);
CREATE INDEX idx_albums_artist ON albums(artist_id);
CREATE INDEX idx_albums_status ON albums(status);

CREATE TABLE tracks (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    album_id  INTEGER NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    disc      INTEGER NOT NULL DEFAULT 1,
    position  INTEGER NOT NULL,
    title     TEXT NOT NULL,
    length_ms INTEGER NOT NULL DEFAULT 0,
    file_path TEXT NOT NULL DEFAULT '',
    UNIQUE (album_id, disc, position)
);

-- A queue item may now be an album grab, and an item event may belong to an
-- album (its own activity log).
ALTER TABLE download_queue ADD COLUMN album_id INTEGER REFERENCES albums(id) ON DELETE SET NULL;
ALTER TABLE activity ADD COLUMN album_id INTEGER REFERENCES albums(id) ON DELETE SET NULL;
CREATE INDEX idx_activity_album_id ON activity(album_id, id);
