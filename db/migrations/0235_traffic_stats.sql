-- How much each indexer's grabs weighed, and how much each Usenet server
-- delivered, per day (UTC), for the Statistics page (#167). Rows are kept for
-- 90 days like the other indexer counters.
ALTER TABLE indexer_stats ADD COLUMN grab_bytes INTEGER NOT NULL DEFAULT 0;

CREATE TABLE usenet_server_stats (
    server_id INTEGER NOT NULL REFERENCES download_clients(id) ON DELETE CASCADE,
    day       TEXT    NOT NULL,
    bytes     INTEGER NOT NULL DEFAULT 0,
    articles  INTEGER NOT NULL DEFAULT 0,
    missing   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (server_id, day)
);
