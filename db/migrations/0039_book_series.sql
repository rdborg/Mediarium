-- Book series: the series a library follows (the missing books in them are
-- added, and new ones as they appear), and each book's place in its series
-- and release date.
CREATE TABLE book_series (
    source         TEXT    NOT NULL,  -- "openlibrary" or "hardcover"
    series_key     TEXT    NOT NULL,  -- Open Library series key "OL326110L", or Hardcover's series id
    name           TEXT    NOT NULL,
    want_ebook     INTEGER NOT NULL DEFAULT 1,
    want_audiobook INTEGER NOT NULL DEFAULT 0,
    followed_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (source, series_key)
);

ALTER TABLE books ADD COLUMN series_name     TEXT NOT NULL DEFAULT '';
ALTER TABLE books ADD COLUMN series_position TEXT NOT NULL DEFAULT '';
-- "2026-11-04" when known. A book with a date still ahead isn't searched for
-- until then, and shows on the calendar.
ALTER TABLE books ADD COLUMN release_date    TEXT NOT NULL DEFAULT '';
