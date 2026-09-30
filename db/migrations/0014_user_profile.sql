-- Profile details shown in the UI (name in the sidebar, email for notices).
ALTER TABLE users ADD COLUMN first_name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN last_name  TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN email      TEXT NOT NULL DEFAULT '';
