-- Phase 3: unified calendar ("releases + episode airs").
-- Movies need their TMDB release date to appear on it.

ALTER TABLE movies ADD COLUMN release_date TEXT;
