-- A blocklist entry made for the upload that failed is saved under the id its
-- indexer gave it (the guid). The same upload can turn up later without a
-- guid, so the entry also keeps the indexer and size (or the info hash of a
-- magnet link) of that upload, and a release without a guid is matched by them.
-- Entries made before this have none, and block as they did.
ALTER TABLE blocklist ADD COLUMN fallback_id TEXT NOT NULL DEFAULT '';
