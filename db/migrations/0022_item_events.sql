-- Per-item activity log: the activity feed also records events for a show
-- (series_id), each event has a level, and item_only marks the detailed
-- events (searches, download and post-processing steps, retries) that are
-- shown on the movie's or show's own page but not in the global feed.
ALTER TABLE activity ADD COLUMN series_id INTEGER REFERENCES series(id) ON DELETE SET NULL;
ALTER TABLE activity ADD COLUMN level     TEXT NOT NULL DEFAULT 'info';
ALTER TABLE activity ADD COLUMN item_only INTEGER NOT NULL DEFAULT 0;

UPDATE activity SET level = 'error' WHERE event_type = 'failed';
UPDATE activity SET level = 'warn'  WHERE event_type IN ('blocklisted', 'conflict');

CREATE INDEX idx_activity_movie_id  ON activity(movie_id, id);
CREATE INDEX idx_activity_series_id ON activity(series_id, id);
