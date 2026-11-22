-- Releases a person chose to keep although their file was far shorter than the
-- movie or episode (a dub that runs 12 minutes where the original runs 25).
-- The length check lets these through from then on and the release is no
-- longer blocklisted for it.
CREATE TABLE IF NOT EXISTS kept_short_releases (
    title_key  TEXT PRIMARY KEY, -- lower-cased, trimmed release title
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
