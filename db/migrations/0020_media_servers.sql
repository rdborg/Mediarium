-- Media servers (Plex, Jellyfin, Emby): Mediarium asks them to rescan the
-- right library folder after each import, and links title pages to the item
-- on the server ("Watch in Plex").

CREATE TABLE media_servers (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    name                 TEXT NOT NULL,
    kind                 TEXT NOT NULL,                -- plex | jellyfin | emby
    base_url             TEXT NOT NULL,                -- how Mediarium reaches the server
    public_url           TEXT NOT NULL DEFAULT '',     -- what people open in a browser; '' = base_url
    token_encrypted      TEXT NOT NULL DEFAULT '',     -- Plex token or Jellyfin/Emby API key, encrypted
    enabled              INTEGER NOT NULL DEFAULT 1,
    refresh_after_import INTEGER NOT NULL DEFAULT 1,
    path_map             TEXT NOT NULL DEFAULT '[]',   -- JSON [{"from":"/movies","to":"/data/movies"}]
    server_id            TEXT NOT NULL DEFAULT '',     -- Plex machineIdentifier / Jellyfin or Emby server Id, from the last good test
    last_error           TEXT NOT NULL DEFAULT '',     -- '' when the last test or refresh worked
    last_checked_at      TEXT,                         -- when the last test or refresh ran (NULL = never)
    created_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
