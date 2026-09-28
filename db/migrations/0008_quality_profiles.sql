-- Editable quality profiles (Radarr/Sonarr "quality profile" parity). The
-- three built-in presets are seeded by the app on first start, not here, so
-- their tier lists stay defined in one place (internal/quality).
CREATE TABLE quality_profiles (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL UNIQUE,
    allowed_tiers   TEXT NOT NULL,          -- comma-separated tier names
    cutoff          TEXT NOT NULL,
    upgrade_allowed INTEGER NOT NULL DEFAULT 1
);

-- NULL = use the default profile (setting library.default_profile_id).
ALTER TABLE movies ADD COLUMN profile_id INTEGER REFERENCES quality_profiles(id) ON DELETE SET NULL;
ALTER TABLE series ADD COLUMN profile_id INTEGER REFERENCES quality_profiles(id) ON DELETE SET NULL;
