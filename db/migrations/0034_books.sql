-- Ebooks and audiobooks. One row per book (an Open Library "work"); the
-- same book can be wanted as an ebook, an audiobook or both, and each format
-- has its own state, quality (file format) and place on disk.
CREATE TABLE books (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    ol_key            TEXT NOT NULL UNIQUE,            -- Open Library work key, "OL45883W"
    title             TEXT NOT NULL,
    author            TEXT NOT NULL DEFAULT '',
    author_key        TEXT NOT NULL DEFAULT '',        -- Open Library author key, "OL34184A"
    year              INTEGER NOT NULL DEFAULT 0,      -- first published
    cover_id          INTEGER NOT NULL DEFAULT 0,      -- Open Library cover id; 0 = none
    description       TEXT NOT NULL DEFAULT '',
    want_ebook        INTEGER NOT NULL DEFAULT 0,
    want_audiobook    INTEGER NOT NULL DEFAULT 0,
    ebook_status      TEXT NOT NULL DEFAULT 'missing', -- missing|downloading|downloaded
    ebook_format      TEXT NOT NULL DEFAULT '',        -- epub, azw3, mobi, pdf
    ebook_path        TEXT NOT NULL DEFAULT '',
    audiobook_status  TEXT NOT NULL DEFAULT 'missing',
    audiobook_format  TEXT NOT NULL DEFAULT '',        -- m4b, mp3, ...
    audiobook_path    TEXT NOT NULL DEFAULT '',        -- the book's folder
    added_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    added_by          INTEGER
);
CREATE INDEX idx_books_author ON books(author);

-- A download for a book says which format it is for.
ALTER TABLE download_queue ADD COLUMN book_id INTEGER REFERENCES books(id) ON DELETE CASCADE;
ALTER TABLE download_queue ADD COLUMN book_format TEXT NOT NULL DEFAULT '';
