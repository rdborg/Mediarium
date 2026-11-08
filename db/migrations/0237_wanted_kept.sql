-- Titles a person looked at in "Wanted for a long time" and chose to keep
-- (#168): they stay out of that review until kept_until.
CREATE TABLE wanted_kept (
    kind       TEXT    NOT NULL,  -- movie or tv
    title_id   INTEGER NOT NULL,  -- movies.id or series.id
    kept_until TEXT    NOT NULL,
    PRIMARY KEY (kind, title_id)
);
