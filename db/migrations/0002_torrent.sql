-- Phase 2: torrent support alongside the existing Usenet/NNTP path.
-- Indexers can now be usenet or torrent; the
-- download_queue tracks which protocol handled a given grab.

ALTER TABLE indexers ADD COLUMN protocol TEXT NOT NULL DEFAULT 'usenet';
ALTER TABLE download_queue ADD COLUMN protocol TEXT NOT NULL DEFAULT 'usenet';
