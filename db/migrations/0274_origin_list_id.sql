-- The import list behind a title or a download, by its id (#190). Until now a
-- list was known only by its name, so renaming it reset the downloads its
-- history counts, and two lists with the same name shared one count. Empty
-- (NULL) for anything not from a list.
ALTER TABLE movies ADD COLUMN origin_list_id INTEGER;
ALTER TABLE series ADD COLUMN origin_list_id INTEGER;
ALTER TABLE download_queue ADD COLUMN origin_list_id INTEGER;

-- What came from a list before this is matched by name, where exactly one
-- list has that name. A name two lists share, or a list removed since, stays
-- unmatched: guessing would credit one list with another's work.
UPDATE movies SET origin_list_id = (SELECT id FROM import_lists WHERE name = movies.origin_name)
    WHERE origin_kind = 'list' AND (SELECT COUNT(*) FROM import_lists WHERE name = movies.origin_name) = 1;
UPDATE series SET origin_list_id = (SELECT id FROM import_lists WHERE name = series.origin_name)
    WHERE origin_kind = 'list' AND (SELECT COUNT(*) FROM import_lists WHERE name = series.origin_name) = 1;
UPDATE download_queue SET origin_list_id = (SELECT id FROM import_lists WHERE name = download_queue.origin_name)
    WHERE origin_kind = 'list' AND (SELECT COUNT(*) FROM import_lists WHERE name = download_queue.origin_name) = 1;

CREATE INDEX IF NOT EXISTS idx_download_queue_origin_list ON download_queue (origin_list_id) WHERE origin_list_id IS NOT NULL;
