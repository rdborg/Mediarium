-- Usenet servers (the news-server accounts the built-in downloader connects
-- to) get a priority: 0 is the primary, higher numbers are backup servers
-- tried only for articles the higher-priority ones lack.
ALTER TABLE download_clients ADD COLUMN priority INTEGER NOT NULL DEFAULT 0;
