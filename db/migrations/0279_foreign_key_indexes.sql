-- Removing a movie, show, album or book (and an album's tracks) makes SQLite
-- look for the rows that point at it, to delete or clear them. Without an index
-- on the pointing column that is a read of the whole table every time, which
-- shows on a long blocklist or a big playlist list. These columns get one.
CREATE INDEX IF NOT EXISTS idx_blocklist_movie ON blocklist (movie_id) WHERE movie_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_blocklist_series ON blocklist (series_id) WHERE series_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_blocklist_album ON blocklist (album_id) WHERE album_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_blocklist_book ON blocklist (book_id) WHERE book_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_music_playlist_tracks_track ON music_playlist_tracks (track_id);
CREATE INDEX IF NOT EXISTS idx_music_listens_pending_track ON music_listens_pending (track_id);
CREATE INDEX IF NOT EXISTS idx_book_progress_book ON book_progress (book_id);
CREATE INDEX IF NOT EXISTS idx_kobo_state_book ON kobo_state (book_id);
CREATE INDEX IF NOT EXISTS idx_download_queue_indexer ON download_queue (indexer_id) WHERE indexer_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_download_queue_client ON download_queue (download_client_id) WHERE download_client_id IS NOT NULL;
