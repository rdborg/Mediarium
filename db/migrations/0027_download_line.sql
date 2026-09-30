-- The download line.
--
-- Downloads now wait in a line and start a few at a time (settings key
-- downloads.concurrent, one by default). An item with status 'queued' is
-- waiting in line; the dispatcher starts the next one whenever a place is free.
--
--   priority   1 for something a person asked for, 0 for what the automatic
--              searches added. A person's downloads go first.
--   line_seq   the place in the line within a priority. Lower goes first. A
--              new item takes the next number; a resumed one takes a number
--              below every other waiting item.
ALTER TABLE download_queue ADD COLUMN priority INTEGER NOT NULL DEFAULT 0;
ALTER TABLE download_queue ADD COLUMN line_seq INTEGER NOT NULL DEFAULT 0;
UPDATE download_queue SET line_seq = id;
