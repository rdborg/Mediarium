-- Authors you follow: their new books are added to the library by
-- themselves, wanted in the formats chosen here.
CREATE TABLE book_authors (
    author_key     TEXT PRIMARY KEY,  -- Open Library author key, "OL26320A"
    name           TEXT    NOT NULL,
    want_ebook     INTEGER NOT NULL DEFAULT 1,
    want_audiobook INTEGER NOT NULL DEFAULT 0,
    followed_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
