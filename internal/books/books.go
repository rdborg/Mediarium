// Package books keeps the ebooks and audiobooks library: one row per book,
// wanted as an ebook, an audiobook or both, each format with its own state
// and file. Book details come from Open Library (see openlibrary.go).
package books

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Format is one of the two ways a book can be had.
type Format string

const (
	Ebook     Format = "ebook"
	Audiobook Format = "audiobook"
)

// Valid reports whether f is a known format.
func (f Format) Valid() bool { return f == Ebook || f == Audiobook }

// Status of one format of a book.
const (
	StatusMissing     = "missing"
	StatusDownloading = "downloading"
	StatusDownloaded  = "downloaded"
)

// Book is one book in the library.
type Book struct {
	ID          int64
	OLKey       string // Open Library work key, "OL45883W"
	Title       string
	Author      string
	AuthorKey   string
	Year        int
	CoverID     int
	Description string
	AddedAt     string
	AddedBy     int64

	WantEbook     bool
	EbookStatus   string
	EbookFormat   string // epub, azw3, mobi or pdf
	EbookPath     string
	WantAudiobook bool
	AudioStatus   string
	AudioFormat   string // m4b, mp3, ...
	AudioPath     string // the book's folder

	SeriesName     string // "Harry Potter"; "" when not known
	SeriesPosition string // "2"; "" when not known
	ReleaseDate    string // "2026-11-04" when known; "" = out already or unknown

	ASIN           string // Audible id of the audiobook, when found
	Narrators      string // "Ray Porter, Jane Doe"
	RuntimeMin     int    // the audiobook's length in minutes
	AudioCheckedAt string // when the audiobook details were looked up ("" = not yet)
}

// Released reports whether the book is out on day now: no release date is
// known, or the date has come.
func (b Book) Released(now time.Time) bool {
	return b.ReleaseDate == "" || b.ReleaseDate <= now.Format("2006-01-02")
}

// Wants reports whether the book is wanted in format f.
func (b Book) Wants(f Format) bool {
	if f == Ebook {
		return b.WantEbook
	}
	return b.WantAudiobook
}

// Status is the state of format f.
func (b Book) Status(f Format) string {
	if f == Ebook {
		return b.EbookStatus
	}
	return b.AudioStatus
}

// Path is where format f is on disk ("" = not there).
func (b Book) Path(f Format) string {
	if f == Ebook {
		return b.EbookPath
	}
	return b.AudioPath
}

// Name is "Title (Year)", or the title alone when the year is unknown.
func (b Book) Name() string {
	if b.Year > 0 {
		return fmt.Sprintf("%s (%d)", b.Title, b.Year)
	}
	return b.Title
}

// ErrExists is returned when a book is already in the library.
var ErrExists = errors.New("that book is already in your library")

// Repo stores books.
type Repo struct{ db *sql.DB }

// NewRepo returns a Repo over db.
func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

const columns = `id, ol_key, title, author, author_key, year, cover_id, description, added_at, COALESCE(added_by, 0),
	want_ebook, ebook_status, ebook_format, ebook_path, want_audiobook, audiobook_status, audiobook_format, audiobook_path,
	series_name, series_position, release_date, asin, narrators, runtime_min, audio_checked_at`

func scan(sc interface{ Scan(...any) error }) (Book, error) {
	var b Book
	err := sc.Scan(&b.ID, &b.OLKey, &b.Title, &b.Author, &b.AuthorKey, &b.Year, &b.CoverID, &b.Description, &b.AddedAt, &b.AddedBy,
		&b.WantEbook, &b.EbookStatus, &b.EbookFormat, &b.EbookPath, &b.WantAudiobook, &b.AudioStatus, &b.AudioFormat, &b.AudioPath,
		&b.SeriesName, &b.SeriesPosition, &b.ReleaseDate, &b.ASIN, &b.Narrators, &b.RuntimeMin, &b.AudioCheckedAt)
	return b, err
}

// Add stores a new book. A book already in the library (same work key)
// answers ErrExists.
func (r *Repo) Add(b Book) (Book, error) {
	var nullBy any
	if b.AddedBy > 0 {
		nullBy = b.AddedBy
	}
	res, err := r.db.Exec(`INSERT INTO books (ol_key, title, author, author_key, year, cover_id, description, want_ebook, want_audiobook, added_by,
		series_name, series_position, release_date)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.OLKey, b.Title, b.Author, b.AuthorKey, b.Year, b.CoverID, b.Description, b.WantEbook, b.WantAudiobook, nullBy,
		b.SeriesName, b.SeriesPosition, b.ReleaseDate)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Book{}, ErrExists
		}
		return Book{}, fmt.Errorf("add book: %w", err)
	}
	id, _ := res.LastInsertId()
	return r.Get(id)
}

// Get returns one book.
func (r *Repo) Get(id int64) (Book, error) {
	b, err := scan(r.db.QueryRow(`SELECT `+columns+` FROM books WHERE id = ?`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Book{}, err
		}
		return Book{}, fmt.Errorf("get book: %w", err)
	}
	return b, nil
}

// GetByKey returns the book with an Open Library work key.
func (r *Repo) GetByKey(olKey string) (Book, bool, error) {
	b, err := scan(r.db.QueryRow(`SELECT `+columns+` FROM books WHERE ol_key = ?`, olKey))
	if errors.Is(err, sql.ErrNoRows) {
		return Book{}, false, nil
	}
	if err != nil {
		return Book{}, false, fmt.Errorf("get book: %w", err)
	}
	return b, true, nil
}

// List returns every book, newest first.
func (r *Repo) List() ([]Book, error) {
	rows, err := r.db.Query(`SELECT ` + columns + ` FROM books ORDER BY added_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list books: %w", err)
	}
	defer rows.Close()
	out := []Book{}
	for rows.Next() {
		b, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan book: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SetWant chooses whether format f is wanted.
func (r *Repo) SetWant(id int64, f Format, want bool) error {
	col := "want_ebook"
	if f == Audiobook {
		col = "want_audiobook"
	}
	if _, err := r.db.Exec(`UPDATE books SET `+col+` = ? WHERE id = ?`, want, id); err != nil {
		return fmt.Errorf("set wanted format: %w", err)
	}
	return nil
}

// SetState records the state of format f: its status, file format and path.
// An empty fileFormat or path keeps the one stored.
func (r *Repo) SetState(id int64, f Format, status, fileFormat, path string) error {
	prefix := "ebook"
	if f == Audiobook {
		prefix = "audiobook"
	}
	_, err := r.db.Exec(`UPDATE books SET `+prefix+`_status = ?,
		`+prefix+`_format = CASE WHEN ? <> '' THEN ? ELSE `+prefix+`_format END,
		`+prefix+`_path = CASE WHEN ? <> '' THEN ? ELSE `+prefix+`_path END
		WHERE id = ?`, status, fileFormat, fileFormat, path, path, id)
	if err != nil {
		return fmt.Errorf("set book state: %w", err)
	}
	return nil
}

// ClearFile forgets the file of format f (after it was removed).
func (r *Repo) ClearFile(id int64, f Format) error {
	prefix := "ebook"
	if f == Audiobook {
		prefix = "audiobook"
	}
	_, err := r.db.Exec(`UPDATE books SET `+prefix+`_status = 'missing', `+prefix+`_format = '', `+prefix+`_path = '' WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("clear book file: %w", err)
	}
	return nil
}

// Delete removes a book and its queue entries.
func (r *Repo) Delete(id int64) error {
	res, err := r.db.Exec(`DELETE FROM books WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete book: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Wanted lists the books still missing in a format they are wanted in.
func (r *Repo) Wanted() ([]Book, error) {
	all, err := r.List()
	if err != nil {
		return nil, err
	}
	var out []Book
	for _, b := range all {
		if (b.WantEbook && b.EbookStatus == StatusMissing) || (b.WantAudiobook && b.AudioStatus == StatusMissing) {
			out = append(out, b)
		}
	}
	return out, nil
}
