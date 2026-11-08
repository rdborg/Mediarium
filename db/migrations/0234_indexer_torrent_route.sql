-- Where torrents from this indexer are downloaded: "" follows the usual order
-- (the download client, then a debrid service, then the built-in client);
-- "client", "debrid" or "builtin" sends them there when it is set up.
ALTER TABLE indexers ADD COLUMN torrent_route TEXT NOT NULL DEFAULT '';
