-- Type-ahead search over the whole library. One full-text index holds the
-- name of every movie, show, book, artist and album, so a few typed letters
-- find titles by the start of any word, with accents ignored, without
-- reading the library tables.
--   title   the main name (artist name for an artist)
--   alt     other names it is also found by: the other and translated titles of
--           a movie or show, the author of a book, an artist's sort name, the
--           artist of an album
-- The row id is the table id times 8 plus the kind (1 movie, 2 show, 3 book,
-- 4 artist, 5 album), so a row is found, replaced and removed by its id and
-- the code reading hits needs no extra columns. Triggers keep it current; the
-- app also fills it again at start when it is empty.
CREATE VIRTUAL TABLE IF NOT EXISTS library_search USING fts5(
    title, alt,
    tokenize = 'unicode61 remove_diacritics 2',
    prefix = '2 3 4'
);

-- The other names of a movie or show: the ones kept for searching indexers
-- (title_names, a JSON list) and the translated ones (display_faces).
CREATE VIEW IF NOT EXISTS library_search_alt_movies AS
SELECT m.id AS id,
       trim(
         coalesce((SELECT group_concat(j.value, ' ')
                     FROM title_names tn, json_each(CASE WHEN json_valid(tn.names) THEN tn.names ELSE '[]' END) j
                    WHERE tn.kind = 'movie' AND tn.tmdb_id = m.tmdb_id), '')
         || ' ' ||
         coalesce((SELECT group_concat(df.title, ' ')
                     FROM display_faces df
                    WHERE df.kind = 'movie' AND df.tmdb_id = m.tmdb_id), '')
       ) AS alt
  FROM movies m;

CREATE VIEW IF NOT EXISTS library_search_alt_series AS
SELECT m.id AS id,
       trim(
         coalesce((SELECT group_concat(j.value, ' ')
                     FROM title_names tn, json_each(CASE WHEN json_valid(tn.names) THEN tn.names ELSE '[]' END) j
                    WHERE tn.kind = 'tv' AND tn.tmdb_id = m.tmdb_id), '')
         || ' ' ||
         coalesce((SELECT group_concat(df.title, ' ')
                     FROM display_faces df
                    WHERE df.kind = 'tv' AND df.tmdb_id = m.tmdb_id), '')
       ) AS alt
  FROM series m;

-- Movies, shows, books, artists and albums.
CREATE TRIGGER IF NOT EXISTS library_search_movies_insert AFTER INSERT ON movies WHEN NEW.copy_of IS NULL
BEGIN
    INSERT INTO library_search(rowid, title, alt)
    SELECT NEW.id * 8 + 1, NEW.title, coalesce((SELECT alt FROM library_search_alt_movies WHERE id = NEW.id), '');
END;
CREATE TRIGGER IF NOT EXISTS library_search_movies_update AFTER UPDATE OF title, tmdb_id ON movies WHEN NEW.copy_of IS NULL
BEGIN
    DELETE FROM library_search WHERE rowid = OLD.id * 8 + 1;
    INSERT INTO library_search(rowid, title, alt)
    SELECT NEW.id * 8 + 1, NEW.title, coalesce((SELECT alt FROM library_search_alt_movies WHERE id = NEW.id), '');
END;
CREATE TRIGGER IF NOT EXISTS library_search_movies_delete AFTER DELETE ON movies
BEGIN
    DELETE FROM library_search WHERE rowid = OLD.id * 8 + 1;
END;

CREATE TRIGGER IF NOT EXISTS library_search_series_insert AFTER INSERT ON series
BEGIN
    INSERT INTO library_search(rowid, title, alt)
    SELECT NEW.id * 8 + 2, NEW.title, coalesce((SELECT alt FROM library_search_alt_series WHERE id = NEW.id), '');
END;
CREATE TRIGGER IF NOT EXISTS library_search_series_update AFTER UPDATE OF title, tmdb_id ON series
BEGIN
    DELETE FROM library_search WHERE rowid = OLD.id * 8 + 2;
    INSERT INTO library_search(rowid, title, alt)
    SELECT NEW.id * 8 + 2, NEW.title, coalesce((SELECT alt FROM library_search_alt_series WHERE id = NEW.id), '');
END;
CREATE TRIGGER IF NOT EXISTS library_search_series_delete AFTER DELETE ON series
BEGIN
    DELETE FROM library_search WHERE rowid = OLD.id * 8 + 2;
END;

CREATE TRIGGER IF NOT EXISTS library_search_books_insert AFTER INSERT ON books
BEGIN
    INSERT INTO library_search(rowid, title, alt)
    SELECT NEW.id * 8 + 3, NEW.title, coalesce(NEW.author, '');
END;
CREATE TRIGGER IF NOT EXISTS library_search_books_update AFTER UPDATE OF title, author ON books
BEGIN
    DELETE FROM library_search WHERE rowid = OLD.id * 8 + 3;
    INSERT INTO library_search(rowid, title, alt)
    SELECT NEW.id * 8 + 3, NEW.title, coalesce(NEW.author, '');
END;
CREATE TRIGGER IF NOT EXISTS library_search_books_delete AFTER DELETE ON books
BEGIN
    DELETE FROM library_search WHERE rowid = OLD.id * 8 + 3;
END;

CREATE TRIGGER IF NOT EXISTS library_search_artists_insert AFTER INSERT ON artists
BEGIN
    INSERT INTO library_search(rowid, title, alt)
    SELECT NEW.id * 8 + 4, NEW.name, trim(coalesce(NEW.sort_name, ''));
END;
CREATE TRIGGER IF NOT EXISTS library_search_artists_update AFTER UPDATE OF name, sort_name ON artists
BEGIN
    DELETE FROM library_search WHERE rowid = OLD.id * 8 + 4;
    INSERT INTO library_search(rowid, title, alt)
    SELECT NEW.id * 8 + 4, NEW.name, trim(coalesce(NEW.sort_name, ''));
END;
CREATE TRIGGER IF NOT EXISTS library_search_artists_delete AFTER DELETE ON artists
BEGIN
    DELETE FROM library_search WHERE rowid = OLD.id * 8 + 4;
END;

CREATE TRIGGER IF NOT EXISTS library_search_albums_insert AFTER INSERT ON albums
BEGIN
    INSERT INTO library_search(rowid, title, alt)
    SELECT NEW.id * 8 + 5, NEW.title, coalesce((SELECT name FROM artists WHERE id = NEW.artist_id), '');
END;
CREATE TRIGGER IF NOT EXISTS library_search_albums_update AFTER UPDATE OF title, artist_id ON albums
BEGIN
    DELETE FROM library_search WHERE rowid = OLD.id * 8 + 5;
    INSERT INTO library_search(rowid, title, alt)
    SELECT NEW.id * 8 + 5, NEW.title, coalesce((SELECT name FROM artists WHERE id = NEW.artist_id), '');
END;
CREATE TRIGGER IF NOT EXISTS library_search_albums_delete AFTER DELETE ON albums
BEGIN
    DELETE FROM library_search WHERE rowid = OLD.id * 8 + 5;
END;

-- The other names of a movie or show arrive later than the title itself.
CREATE TRIGGER IF NOT EXISTS library_search_title_names_insert_movie AFTER INSERT ON title_names WHEN NEW.kind = 'movie'
BEGIN
    DELETE FROM library_search WHERE rowid IN (SELECT id * 8 + 1 FROM movies WHERE tmdb_id = NEW.tmdb_id AND copy_of IS NULL);
    INSERT INTO library_search(rowid, title, alt)
    SELECT b.id * 8 + 1, b.title, coalesce(v.alt, '')
      FROM movies b LEFT JOIN library_search_alt_movies v ON v.id = b.id
     WHERE b.tmdb_id = NEW.tmdb_id AND b.copy_of IS NULL;
END;
CREATE TRIGGER IF NOT EXISTS library_search_title_names_insert_tv AFTER INSERT ON title_names WHEN NEW.kind = 'tv'
BEGIN
    DELETE FROM library_search WHERE rowid IN (SELECT id * 8 + 2 FROM series WHERE tmdb_id = NEW.tmdb_id);
    INSERT INTO library_search(rowid, title, alt)
    SELECT b.id * 8 + 2, b.title, coalesce(v.alt, '')
      FROM series b LEFT JOIN library_search_alt_series v ON v.id = b.id
     WHERE b.tmdb_id = NEW.tmdb_id;
END;
CREATE TRIGGER IF NOT EXISTS library_search_title_names_update_movie AFTER UPDATE ON title_names WHEN NEW.kind = 'movie'
BEGIN
    DELETE FROM library_search WHERE rowid IN (SELECT id * 8 + 1 FROM movies WHERE tmdb_id = NEW.tmdb_id AND copy_of IS NULL);
    INSERT INTO library_search(rowid, title, alt)
    SELECT b.id * 8 + 1, b.title, coalesce(v.alt, '')
      FROM movies b LEFT JOIN library_search_alt_movies v ON v.id = b.id
     WHERE b.tmdb_id = NEW.tmdb_id AND b.copy_of IS NULL;
END;
CREATE TRIGGER IF NOT EXISTS library_search_title_names_update_tv AFTER UPDATE ON title_names WHEN NEW.kind = 'tv'
BEGIN
    DELETE FROM library_search WHERE rowid IN (SELECT id * 8 + 2 FROM series WHERE tmdb_id = NEW.tmdb_id);
    INSERT INTO library_search(rowid, title, alt)
    SELECT b.id * 8 + 2, b.title, coalesce(v.alt, '')
      FROM series b LEFT JOIN library_search_alt_series v ON v.id = b.id
     WHERE b.tmdb_id = NEW.tmdb_id;
END;

CREATE TRIGGER IF NOT EXISTS library_search_display_faces_insert_movie AFTER INSERT ON display_faces WHEN NEW.kind = 'movie'
BEGIN
    DELETE FROM library_search WHERE rowid IN (SELECT id * 8 + 1 FROM movies WHERE tmdb_id = NEW.tmdb_id AND copy_of IS NULL);
    INSERT INTO library_search(rowid, title, alt)
    SELECT b.id * 8 + 1, b.title, coalesce(v.alt, '')
      FROM movies b LEFT JOIN library_search_alt_movies v ON v.id = b.id
     WHERE b.tmdb_id = NEW.tmdb_id AND b.copy_of IS NULL;
END;
CREATE TRIGGER IF NOT EXISTS library_search_display_faces_insert_tv AFTER INSERT ON display_faces WHEN NEW.kind = 'tv'
BEGIN
    DELETE FROM library_search WHERE rowid IN (SELECT id * 8 + 2 FROM series WHERE tmdb_id = NEW.tmdb_id);
    INSERT INTO library_search(rowid, title, alt)
    SELECT b.id * 8 + 2, b.title, coalesce(v.alt, '')
      FROM series b LEFT JOIN library_search_alt_series v ON v.id = b.id
     WHERE b.tmdb_id = NEW.tmdb_id;
END;
CREATE TRIGGER IF NOT EXISTS library_search_display_faces_update_movie AFTER UPDATE ON display_faces WHEN NEW.kind = 'movie'
BEGIN
    DELETE FROM library_search WHERE rowid IN (SELECT id * 8 + 1 FROM movies WHERE tmdb_id = NEW.tmdb_id AND copy_of IS NULL);
    INSERT INTO library_search(rowid, title, alt)
    SELECT b.id * 8 + 1, b.title, coalesce(v.alt, '')
      FROM movies b LEFT JOIN library_search_alt_movies v ON v.id = b.id
     WHERE b.tmdb_id = NEW.tmdb_id AND b.copy_of IS NULL;
END;
CREATE TRIGGER IF NOT EXISTS library_search_display_faces_update_tv AFTER UPDATE ON display_faces WHEN NEW.kind = 'tv'
BEGIN
    DELETE FROM library_search WHERE rowid IN (SELECT id * 8 + 2 FROM series WHERE tmdb_id = NEW.tmdb_id);
    INSERT INTO library_search(rowid, title, alt)
    SELECT b.id * 8 + 2, b.title, coalesce(v.alt, '')
      FROM series b LEFT JOIN library_search_alt_series v ON v.id = b.id
     WHERE b.tmdb_id = NEW.tmdb_id;
END;

-- Fill the index with what is already in the library.
INSERT INTO library_search(rowid, title, alt)
SELECT m.id * 8 + 1, m.title, coalesce(v.alt, '') FROM movies m LEFT JOIN library_search_alt_movies v ON v.id = m.id WHERE m.copy_of IS NULL;
INSERT INTO library_search(rowid, title, alt)
SELECT s.id * 8 + 2, s.title, coalesce(v.alt, '') FROM series s LEFT JOIN library_search_alt_series v ON v.id = s.id;
INSERT INTO library_search(rowid, title, alt)
SELECT id * 8 + 3, title, coalesce(author, '') FROM books;
INSERT INTO library_search(rowid, title, alt)
SELECT id * 8 + 4, name, trim(coalesce(sort_name, '')) FROM artists;
INSERT INTO library_search(rowid, title, alt)
SELECT a.id * 8 + 5, a.title, coalesce(r.name, '') FROM albums a LEFT JOIN artists r ON r.id = a.artist_id;
