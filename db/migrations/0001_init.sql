-- Initial schema: one SQLite file covering config, library,
-- indexer state, download queue, and credentials for Phase 1 (movies, Usenet).

CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    is_admin      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at TEXT NOT NULL
);

CREATE TABLE api_keys (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    key_hash   TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    revoked_at TEXT
);

-- Generic app settings (library paths, naming preset/tokens, TMDB key, etc.).
-- `encrypted` marks values stored via internal/crypto (AES-GCM) rather than plaintext.
CREATE TABLE settings (
    key       TEXT PRIMARY KEY,
    value     TEXT NOT NULL,
    encrypted INTEGER NOT NULL DEFAULT 0
);

-- One row per configured indexer instance (a Cardigann definition + the
-- user's own base URL/API key for it).
CREATE TABLE indexers (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    name                 TEXT NOT NULL,
    definition_id        TEXT NOT NULL,
    base_url             TEXT NOT NULL,
    api_key_encrypted    TEXT,
    enabled              INTEGER NOT NULL DEFAULT 1,
    created_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- Download clients. Phase 1 is NNTP-only; `type` leaves room for a torrent
-- row in Phase 2 without a schema rework.
CREATE TABLE download_clients (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    type                TEXT NOT NULL DEFAULT 'nntp',
    name                TEXT NOT NULL,
    host                TEXT NOT NULL,
    port                INTEGER NOT NULL,
    use_ssl             INTEGER NOT NULL DEFAULT 1,
    username            TEXT,
    password_encrypted  TEXT,
    connections         INTEGER NOT NULL DEFAULT 4,
    enabled             INTEGER NOT NULL DEFAULT 1,
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- Library. Phase 1 = movies only; `media_type` is here so TV (Phase 2) is an
-- additive row shape, not a new table.
CREATE TABLE movies (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    media_type    TEXT NOT NULL DEFAULT 'movie',
    tmdb_id       INTEGER NOT NULL UNIQUE,
    title         TEXT NOT NULL,
    year          INTEGER,
    overview      TEXT,
    poster_path   TEXT,
    status        TEXT NOT NULL DEFAULT 'missing', -- missing|downloading|downloaded
    quality       TEXT,
    file_path     TEXT,
    monitored     INTEGER NOT NULL DEFAULT 1,
    added_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- One row per grabbed release moving through download -> import.
CREATE TABLE download_queue (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id            INTEGER REFERENCES movies(id) ON DELETE CASCADE,
    indexer_id          INTEGER REFERENCES indexers(id) ON DELETE SET NULL,
    download_client_id  INTEGER REFERENCES download_clients(id) ON DELETE SET NULL,
    release_title       TEXT NOT NULL,
    nzb_url             TEXT,
    size_bytes          INTEGER,
    status              TEXT NOT NULL DEFAULT 'queued', -- queued|downloading|importing|completed|failed
    progress_pct        REAL NOT NULL DEFAULT 0,
    error               TEXT,
    added_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    completed_at        TEXT
);

-- Activity feed (grabbed / downloaded / imported / failed events).
CREATE TABLE activity (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id   INTEGER REFERENCES movies(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL,
    message    TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_download_queue_status ON download_queue(status);
CREATE INDEX idx_movies_status ON movies(status);
CREATE INDEX idx_activity_created_at ON activity(created_at);
