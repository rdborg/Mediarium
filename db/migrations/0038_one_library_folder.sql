-- One library folder per kind of media: the "more movie / TV folders"
-- feature is gone (tags sort a library instead). Its settings are removed and
-- the per-title folder choice cleared; the column stays, unused.
UPDATE movies SET root_path = '';
UPDATE series SET root_path = '';
DELETE FROM settings WHERE key IN ('library.movies_extra_paths', 'library.tv_extra_paths');
