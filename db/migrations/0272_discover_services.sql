-- The streaming services a person picked on Discover ("Hide what I already
-- have on"), kept with the account instead of in one browser. NULL means the
-- person never saved a choice here, so the app may move the one an older
-- version kept in the browser; a comma separated list of service keys
-- (possibly empty) is a choice. discover_hide is the switch.
ALTER TABLE users ADD COLUMN discover_services TEXT;
ALTER TABLE users ADD COLUMN discover_hide INTEGER NOT NULL DEFAULT 0;
