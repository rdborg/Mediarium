-- The id the indexer gave a release (its guid), kept on the download so that
-- when the download fails only that upload goes on the blocklist and not
-- another one with the same name and size. Downloads added before this have
-- none and are blocklisted by name, size or info hash as before.
ALTER TABLE download_queue ADD COLUMN release_guid TEXT NOT NULL DEFAULT '';
