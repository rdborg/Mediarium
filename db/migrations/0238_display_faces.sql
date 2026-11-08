-- The title and poster shown for a movie or show already in the library, in
-- one info language. The English title stays on the movie or series row
-- (indexer searches use it). Lists read this table and never ask TMDB while
-- they answer; a background job fills the rows that are missing.
--   kind      'movie' or 'tv'
--   language  a TMDB tag such as 'it-IT'
--   title     the translated title, or the English one when TMDB has none
--   poster    the poster path TMDB returned, or the English one when it has none
CREATE TABLE IF NOT EXISTS display_faces (
    kind     TEXT    NOT NULL,
    tmdb_id  INTEGER NOT NULL,
    language TEXT    NOT NULL,
    title    TEXT    NOT NULL DEFAULT '',
    poster   TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (kind, tmdb_id, language)
);
