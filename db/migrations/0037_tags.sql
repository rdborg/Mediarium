-- Tags on movies and shows ("Kids", "4K"): for filtering the library and for
-- the collections on media servers. Kept with the title, gone with it.
CREATE TABLE movie_tags (
    movie_id INTEGER NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    tag      TEXT    NOT NULL COLLATE NOCASE,
    PRIMARY KEY (movie_id, tag)
);
CREATE TABLE series_tags (
    series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    tag       TEXT    NOT NULL COLLATE NOCASE,
    PRIMARY KEY (series_id, tag)
);
CREATE INDEX movie_tags_tag ON movie_tags (tag);
CREATE INDEX series_tags_tag ON series_tags (tag);
