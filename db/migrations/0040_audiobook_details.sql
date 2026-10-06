-- Audiobook details from Audnexus (Audible's catalogue): who reads it and how
-- long it runs. Looked up once per book; audio_checked_at says when.
ALTER TABLE books ADD COLUMN asin             TEXT    NOT NULL DEFAULT '';
ALTER TABLE books ADD COLUMN narrators        TEXT    NOT NULL DEFAULT ''; -- "Ray Porter, Jane Doe"
ALTER TABLE books ADD COLUMN runtime_min      INTEGER NOT NULL DEFAULT 0;
ALTER TABLE books ADD COLUMN audio_checked_at TEXT    NOT NULL DEFAULT '';
