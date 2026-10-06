-- How a show's episodes are numbered in release names: "standard"
-- (S01E05), "anime" (absolute numbers, "Show - 105") or "daily" (air dates,
-- "Show 2024.03.15").
ALTER TABLE series ADD COLUMN series_type TEXT NOT NULL DEFAULT 'standard';
