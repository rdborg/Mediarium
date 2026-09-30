-- Safe defaults for an import: the titles are added without monitoring, and
-- the person can start it afterwards with one click on the report.
--
-- import_batches.monitor   the titles were added monitored (watched for new
--                          episodes and better versions). Batches made before
--                          this existed count as monitored, which is what they did.
-- import_items.created     the import added this title itself. 0 when it was in
--                          the library already, so "start monitoring" leaves it be.
ALTER TABLE import_batches ADD COLUMN monitor INTEGER NOT NULL DEFAULT 1;
ALTER TABLE import_items   ADD COLUMN created INTEGER NOT NULL DEFAULT 1;
