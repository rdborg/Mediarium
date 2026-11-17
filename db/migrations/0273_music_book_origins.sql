-- Where an artist, an album or a book came from (#191), as movies and shows
-- already keep it: the person who added it, a request, a library folder scan,
-- an app using the API, or a followed artist, author or series. Rows from
-- before this was kept have an empty kind, and the app shows nothing for them.
--
-- An artist's and a book's person is the added_by they already have. An album
-- can be added later than its artist, by someone else, so it gets its own.
ALTER TABLE artists ADD COLUMN origin_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE artists ADD COLUMN origin_name TEXT NOT NULL DEFAULT '';
ALTER TABLE albums ADD COLUMN origin_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE albums ADD COLUMN origin_name TEXT NOT NULL DEFAULT '';
ALTER TABLE albums ADD COLUMN added_by INTEGER NOT NULL DEFAULT 0;
ALTER TABLE books ADD COLUMN origin_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE books ADD COLUMN origin_name TEXT NOT NULL DEFAULT '';
