-- Indexer priority: 1 = preferred, 2 = normal (the default), 3 = last resort.
-- Between two equally good releases the one from the preferred indexer wins.
ALTER TABLE indexers ADD COLUMN priority INTEGER NOT NULL DEFAULT 2;

-- The largest release a quality profile accepts, in GB. 0 = no limit.
ALTER TABLE quality_profiles ADD COLUMN max_size_gb REAL NOT NULL DEFAULT 0;
