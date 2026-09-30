-- The problem log (Settings > System > Logs and errors).
--
-- One row per problem worth a person's attention: a download that failed, a
-- Usenet provider that refused the login, a full disk. The same problem
-- repeating within a few minutes is folded into one row (count goes up and
-- last_at moves on) instead of adding rows.
--
--   code       a stable name such as usenet.too_many_connections
--   subject    what it is about when one code can hit several things (a server,
--              a search source, a download); repeats fold together per subject
--   message    a short plain sentence
--   detail     technical detail, with passwords, keys and tokens already taken out
--   title      the movie, show or album it concerns, when known
--   link       the page in the app for that title, when known
--   is_read    0 until an administrator marks it as read
--
-- Rows older than 30 days are removed by the daily clean-up, and the table is
-- kept to 5000 rows.
CREATE TABLE problems (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    first_at    TEXT    NOT NULL,
    last_at     TEXT    NOT NULL,
    level       TEXT    NOT NULL,
    area        TEXT    NOT NULL,
    code        TEXT    NOT NULL,
    subject     TEXT    NOT NULL DEFAULT '',
    message     TEXT    NOT NULL,
    detail      TEXT    NOT NULL DEFAULT '',
    title       TEXT    NOT NULL DEFAULT '',
    link        TEXT    NOT NULL DEFAULT '',
    download_id INTEGER NOT NULL DEFAULT 0,
    count       INTEGER NOT NULL DEFAULT 1,
    is_read     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_problems_last_at ON problems (last_at);
CREATE INDEX idx_problems_fold ON problems (code, subject, last_at);
