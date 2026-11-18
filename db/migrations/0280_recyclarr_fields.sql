-- What Recyclarr (and the apps built for Radarr and Sonarr) send that
-- Mediarium would otherwise drop, kept so the next read gives back what was
-- written and a sync with Recyclarr reports "nothing changed".
--
-- Custom formats: the "include when renaming" switch, and the same name for a
-- Radarr format and a Sonarr format. The TRaSH Guides use many names in both
-- (HDR, x265 (HD), BR-DISK, ...) with different conditions, and Recyclarr
-- syncs a Radarr and a Sonarr instance one after the other, so a name is now
-- unique per app. The table is rebuilt for it; ids stay as they are.
CREATE TABLE custom_formats_new (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL COLLATE NOCASE,
    app             TEXT NOT NULL DEFAULT 'radarr',  -- whose numbering the conditions use: radarr or sonarr
    trash_id        TEXT NOT NULL DEFAULT '',        -- the TRaSH Guides id, for formats imported from there
    specs           TEXT NOT NULL,                   -- the conditions, JSON
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    trash_score     INTEGER,                         -- see 0216
    include_in_name INTEGER NOT NULL DEFAULT 0,      -- "include custom format when renaming", only kept
    UNIQUE (name, app)
);
INSERT INTO custom_formats_new (id, name, app, trash_id, specs, created_at, trash_score)
    SELECT id, name, app, trash_id, specs, created_at, trash_score FROM custom_formats;
DROP TABLE custom_formats;
ALTER TABLE custom_formats_new RENAME TO custom_formats;

-- Quality profiles: the quality list, cutoff and the few profile fields of
-- Radarr's and Sonarr's own shape, as last written through their API (JSON,
-- empty when the profile was made in Mediarium). It is only used while it
-- still agrees with the profile.
ALTER TABLE quality_profiles ADD COLUMN arr_extra TEXT NOT NULL DEFAULT '';
