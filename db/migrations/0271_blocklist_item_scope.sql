-- A blocklist entry can now be for one title only. A release that is the wrong
-- movie (same name, other year) should not be blocked for the right one.
-- scope 'all' is how every entry worked until now and stays the default;
-- scope 'item' means only the title named by movie_id, series_id, album_id or
-- book_id is kept away from the release. For an item entry title_key holds the
-- release key plus the title, so one release can be blocked for several titles.
ALTER TABLE blocklist ADD COLUMN scope    TEXT NOT NULL DEFAULT 'all';
ALTER TABLE blocklist ADD COLUMN album_id INTEGER REFERENCES albums(id) ON DELETE CASCADE;
ALTER TABLE blocklist ADD COLUMN book_id  INTEGER REFERENCES books(id) ON DELETE CASCADE;
