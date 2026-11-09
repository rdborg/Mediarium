-- Adding just one song (#143): Mediarium works in albums, so the album is
-- added and downloaded as usual, but only the chosen song counts as wanted.
-- only_song is the song's title as picked in the search; once the album's
-- tracklist is known (at the first import) it is turned into the per-track
-- monitored flag, and the other tracks are not imported. Every existing
-- album and track stays fully wanted (monitored = 1, only_song = '').
ALTER TABLE albums ADD COLUMN only_song TEXT NOT NULL DEFAULT '';
ALTER TABLE tracks ADD COLUMN monitored INTEGER NOT NULL DEFAULT 1;
