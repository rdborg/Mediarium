-- Family accounts: an administrator can add "member" accounts that browse,
-- add titles and follow downloads but cannot see or change settings.
-- users.is_admin stays the source of truth for the role (1 = admin,
-- 0 = member).
--
-- Before roles existed every account could do everything, so every account
-- that already exists stays an administrator.
UPDATE users SET is_admin = 1;

-- When the account last signed in with its password (NULL = never).
ALTER TABLE users ADD COLUMN last_login_at TEXT;

-- Which account added a title. NULL when unknown: added before accounts were
-- tracked, found by a library import, or the account has since been deleted
-- (ids are never reused, so a stale id simply resolves to nobody).
ALTER TABLE movies ADD COLUMN added_by INTEGER;
ALTER TABLE series ADD COLUMN added_by INTEGER;

-- TMDB genre names as a JSON array, e.g. ["Drama","Crime"]. NULL means not
-- fetched yet; a background job fills those in a few at a time.
ALTER TABLE movies ADD COLUMN genres TEXT;
ALTER TABLE series ADD COLUMN genres TEXT;
