-- The "Other" media type (#200): things that are not broadcast media (a
-- lecture series, a game's soundtrack, a home video collection). An item is
-- made by name, never looked up on TMDB or TVDB, and keeps its files in a
-- folder of its own under the Other library folder.
CREATE TABLE other_items (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'missing', -- missing|downloading|downloaded
    path        TEXT NOT NULL DEFAULT '',        -- the item's folder
    file_count  INTEGER NOT NULL DEFAULT 0,
    size_bytes  INTEGER NOT NULL DEFAULT 0,
    added_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    added_by    INTEGER,
    origin_kind TEXT NOT NULL DEFAULT '',
    origin_name TEXT NOT NULL DEFAULT ''
);

-- A download for an Other item says which one.
ALTER TABLE download_queue ADD COLUMN other_id INTEGER REFERENCES other_items(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_download_queue_other ON download_queue (other_id) WHERE other_id IS NOT NULL;
