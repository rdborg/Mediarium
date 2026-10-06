-- What has been watched, read from Plex, Jellyfin and Emby when "Read what's
-- been watched" is on (off by default). Replaced whole at every sync.
CREATE TABLE watched (
    kind        TEXT    NOT NULL,           -- "movie" or "episode"
    title_id    INTEGER NOT NULL,           -- movies.id, or series.id for an episode
    season      INTEGER NOT NULL DEFAULT 0,
    episode     INTEGER NOT NULL DEFAULT 0,
    plays       INTEGER NOT NULL DEFAULT 0,
    last_played TEXT    NOT NULL DEFAULT '', -- RFC 3339; "" when the server doesn't say
    PRIMARY KEY (kind, title_id, season, episode)
);
