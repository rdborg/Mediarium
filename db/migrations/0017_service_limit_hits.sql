-- Times a shared third-party service (Trakt, OpenSubtitles, TMDB) refused us
-- because a usage limit was reached. Kept so the dashboard can tell the person
-- to switch to their own key, even after a restart.
CREATE TABLE service_limit_hits (
    service TEXT NOT NULL,                  -- 'trakt', 'opensubtitles' or 'tmdb'
    at      TEXT NOT NULL
);
CREATE INDEX idx_service_limit_hits_at ON service_limit_hits (service, at);
