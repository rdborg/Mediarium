-- Fast library import: titles are registered at once with what the scan
-- already knows, and a background worker fills in the rest.
--
-- no_upgrade      the title is left alone by the search for better versions
--                 (set for titles that came in through an import).
-- details_state   '' = nothing to do, 'pending' = details are still being
--                 fetched, 'problem' = the last attempt failed (retried later).
-- details_note    a plain-language reason for a problem, shown on the card.
ALTER TABLE movies ADD COLUMN no_upgrade    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE movies ADD COLUMN details_state TEXT    NOT NULL DEFAULT '';
ALTER TABLE movies ADD COLUMN details_note  TEXT    NOT NULL DEFAULT '';

ALTER TABLE series ADD COLUMN no_upgrade    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE series ADD COLUMN details_state TEXT    NOT NULL DEFAULT '';
ALTER TABLE series ADD COLUMN details_note  TEXT    NOT NULL DEFAULT '';

-- One import run (one confirmed review). The banner and the report read these.
CREATE TABLE import_batches (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    kind        TEXT    NOT NULL,                -- movie|tv
    root        TEXT    NOT NULL DEFAULT '',     -- the folder that was scanned
    created_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    finished_at TEXT,                            -- set once no title is waiting for details
    dismissed   INTEGER NOT NULL DEFAULT 0,      -- the finished banner was closed
    no_upgrade  INTEGER NOT NULL DEFAULT 1,
    monitor_missing INTEGER NOT NULL DEFAULT 1
);

-- One title in an import, and the work still to do for it.
CREATE TABLE import_items (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    batch_id    INTEGER NOT NULL REFERENCES import_batches(id) ON DELETE CASCADE,
    kind        TEXT    NOT NULL,                -- movie|series
    item_id     INTEGER NOT NULL DEFAULT 0,      -- movies.id or series.id; 0 when nothing was registered
    tmdb_id     INTEGER NOT NULL,
    title       TEXT    NOT NULL,
    outcome     TEXT    NOT NULL DEFAULT 'added', -- added|already|failed
    state       TEXT    NOT NULL DEFAULT 'pending', -- pending|done|problem
    note        TEXT    NOT NULL DEFAULT '',
    attempts    INTEGER NOT NULL DEFAULT 0,
    next_try_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    files       TEXT    NOT NULL DEFAULT '[]',   -- shows: the episode files found on disk (JSON)
    imported    INTEGER NOT NULL DEFAULT 0,      -- shows: episodes marked downloaded
    skipped     INTEGER NOT NULL DEFAULT 0       -- shows: episodes that already were
);
CREATE INDEX idx_import_items_batch ON import_items(batch_id);
CREATE INDEX idx_import_items_state ON import_items(state, next_try_at);
