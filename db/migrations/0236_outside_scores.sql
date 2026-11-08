-- Scores from OMDb (IMDb, Rotten Tomatoes, Metacritic), kept for a week so a
-- title page or a Discover card doesn't ask again (#169). found is 0 for a
-- title OMDb doesn't know, so it isn't asked about every time either.
CREATE TABLE outside_scores (
    imdb_id         TEXT    PRIMARY KEY,
    kind            TEXT    NOT NULL DEFAULT '',
    tmdb_id         INTEGER NOT NULL DEFAULT 0,
    imdb            REAL    NOT NULL DEFAULT 0,
    imdb_votes      INTEGER NOT NULL DEFAULT 0,
    rotten_tomatoes INTEGER NOT NULL DEFAULT 0,
    metacritic      INTEGER NOT NULL DEFAULT 0,
    found           INTEGER NOT NULL DEFAULT 1,
    fetched_at      TEXT    NOT NULL
);
CREATE INDEX outside_scores_tmdb ON outside_scores (kind, tmdb_id);
