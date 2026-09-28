-- Which downloaders an item may use: '' = follow the default in Settings,
-- otherwise 'usenet', 'torrent' or 'both'.
ALTER TABLE movies ADD COLUMN source_pref TEXT NOT NULL DEFAULT '';
ALTER TABLE series ADD COLUMN source_pref TEXT NOT NULL DEFAULT '';
