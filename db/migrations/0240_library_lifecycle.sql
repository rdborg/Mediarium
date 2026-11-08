-- Library lifecycle (V4 D13): titles the rules have put on the "Leaving soon"
-- shelf, and the titles someone chose to keep.
CREATE TABLE lifecycle_flags (
    kind       TEXT    NOT NULL,  -- movie or episode
    item_id    INTEGER NOT NULL,  -- movies.id, or the id of the file's first episode
    series_id  INTEGER NOT NULL DEFAULT 0,
    title      TEXT    NOT NULL,
    reason     TEXT    NOT NULL,
    flagged_at TEXT    NOT NULL,
    due_at     TEXT    NOT NULL,  -- when its files go, unless it is kept
    PRIMARY KEY (kind, item_id)
);

CREATE TABLE lifecycle_keeps (
    kind     TEXT    NOT NULL,  -- movie or series
    title_id INTEGER NOT NULL,  -- movies.id or series.id
    title    TEXT    NOT NULL,
    kept_at  TEXT    NOT NULL,
    PRIMARY KEY (kind, title_id)
);
