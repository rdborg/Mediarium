-- What a basic account may do (JSON; empty means everything a basic account
-- could always do), and requests: titles a basic account asked for when it
-- may not add them itself, waiting for an administrator.
ALTER TABLE users ADD COLUMN permissions TEXT NOT NULL DEFAULT '';

CREATE TABLE requests (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    kind          TEXT    NOT NULL,             -- "movie", "tv", "music" or "book"
    title         TEXT    NOT NULL,
    year          INTEGER NOT NULL DEFAULT 0,
    poster        TEXT    NOT NULL DEFAULT '',  -- a picture address for the list
    payload       TEXT    NOT NULL,             -- the add request as it was sent (JSON)
    requested_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    status        TEXT    NOT NULL DEFAULT 'pending', -- pending, approved, declined
    note          TEXT    NOT NULL DEFAULT '',  -- why it was declined, or what went wrong
    created_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    decided_at    TEXT    NOT NULL DEFAULT '',
    decided_by    INTEGER REFERENCES users(id) ON DELETE SET NULL
);
CREATE INDEX requests_status ON requests (status, created_at);
