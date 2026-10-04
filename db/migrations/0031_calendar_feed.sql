-- One calendar feed link per account: a secret address that calendar apps
-- (Google, Apple, Outlook) can read without signing in. It only ever shows
-- the calendar, and the account can turn it off or make a new one.
CREATE TABLE calendar_feeds (
    user_id    INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    token      TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
