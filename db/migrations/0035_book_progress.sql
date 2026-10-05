-- Where each person is in a book: the reading position in an ebook (an EPUB
-- location, or a page) and the listening position in an audiobook (track and
-- seconds), so a book opens where it was left on any device.
CREATE TABLE book_progress (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    book_id    INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    format     TEXT    NOT NULL,             -- ebook or audiobook
    position   TEXT    NOT NULL DEFAULT '',  -- ebook: an EPUB CFI; audiobook: "track:seconds"
    percent    REAL    NOT NULL DEFAULT 0,   -- 0 to 100
    finished   INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (user_id, book_id, format)
);
CREATE INDEX book_progress_recent ON book_progress (user_id, updated_at);
