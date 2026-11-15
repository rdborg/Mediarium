-- Where a title or a download came from (#190, #191): the person who added it,
-- the import list, or the scheduled job. Rows from before this was kept have
-- an empty kind, and the app shows nothing for them.
--
-- A title's person is the added_by it already has; a download keeps the
-- person's id and name itself, because the account may be removed later.
ALTER TABLE movies ADD COLUMN origin_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE movies ADD COLUMN origin_name TEXT NOT NULL DEFAULT '';
ALTER TABLE series ADD COLUMN origin_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE series ADD COLUMN origin_name TEXT NOT NULL DEFAULT '';
ALTER TABLE download_queue ADD COLUMN origin_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE download_queue ADD COLUMN origin_name TEXT NOT NULL DEFAULT '';
ALTER TABLE download_queue ADD COLUMN origin_user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE download_queue ADD COLUMN origin_user TEXT NOT NULL DEFAULT '';

-- What each import list added, one row per title and kept when the list is
-- told to start over, so a list's history shows how useful it has been.
-- Lists that already ran are filled in from what they remember.
CREATE TABLE IF NOT EXISTS import_list_log (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    list_id INTEGER NOT NULL REFERENCES import_lists(id) ON DELETE CASCADE,
    at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    kind    TEXT    NOT NULL DEFAULT '',
    tmdb_id INTEGER NOT NULL DEFAULT 0,
    title   TEXT    NOT NULL DEFAULT '',
    year    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_import_list_log_list ON import_list_log (list_id, at);

INSERT INTO import_list_log (list_id, at, kind, tmdb_id, title, year)
    SELECT list_id, seen_at, kind, tmdb_id, title, year FROM import_list_items WHERE outcome = 'added';
