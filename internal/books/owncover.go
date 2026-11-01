package books

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// A person can give a book a cover of their own: when Open Library has none,
// or only for another edition. The picture is checked, cut down to a sensible
// size, written as a JPEG and kept in the database (see the 0190 migration).
// A cover picked from another edition of the book only remembers that
// edition's Open Library cover id.

// MaxOwnCoverBytes is the largest picture a person may give, as a file or
// behind a link.
const MaxOwnCoverBytes = 8 << 20

// A chosen cover is kept at most this big. Bigger than any shelf or the
// player needs, small enough that the database doesn't grow by megabytes.
const (
	ownCoverW = 800
	ownCoverH = 1200
)

var (
	// ErrCoverTooBig is returned for a picture over MaxOwnCoverBytes.
	ErrCoverTooBig = fmt.Errorf("the picture is larger than %d MB", MaxOwnCoverBytes>>20)
	// ErrCoverFormat is returned for something that is not a JPEG, PNG or WebP
	// picture.
	ErrCoverFormat = errors.New("the picture is not a JPEG, PNG or WebP")
	// ErrCoverUnreadable is returned for a picture that can't be decoded.
	ErrCoverUnreadable = errors.New("the picture can't be read")
)

// PrepareOwnCover checks a picture a person gave and returns it as the JPEG
// that is kept. The bytes decide what it is, not a file name or a header: only
// a JPEG, PNG or WebP that really decodes is accepted.
func PrepareOwnCover(data []byte) ([]byte, error) {
	if len(data) > MaxOwnCoverBytes {
		return nil, ErrCoverTooBig
	}
	switch http.DetectContentType(data) {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return nil, ErrCoverFormat
	}
	out, err := ResizeCover(data, ownCoverW, ownCoverH)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCoverUnreadable, err)
	}
	return out, nil
}

// OLCoverID is the Open Library cover the book shows when it has no picture of
// its own: the one picked from another edition, else the automatic one.
func (b Book) OLCoverID() int {
	if b.PickedCoverID > 0 {
		return b.PickedCoverID
	}
	return b.CoverID
}

// OwnCoverURL is the address of the book's own cover picture on this server;
// the saved time in it makes a browser fetch a new picture after a change.
func OwnCoverURL(bookID, saved int64) string {
	return fmt.Sprintf("/api/books/%d/own-cover?v=%d", bookID, saved)
}

// CoverPath is the address of the book's cover on this server: its own
// picture when a person picked one, else the Open Library cover, else the
// cached Hardcover cover of a book filed under "hc-<id>" ("" when there is
// none). size is "S", "M" or "L" and only matters for an Open Library cover.
func (b Book) CoverPath(size string) string {
	if b.OwnCover > 0 {
		return OwnCoverURL(b.ID, b.OwnCover)
	}
	if id := b.OLCoverID(); id > 0 {
		return LocalCoverURL(id, size)
	}
	if id, ok := ParseHardcoverKey(b.OLKey); ok {
		return HardcoverCoverURL(id)
	}
	return ""
}

// SetOwnCover keeps jpeg as the book's own cover. It replaces any cover
// picked from another edition.
func (r *Repo) SetOwnCover(id int64, jpeg []byte, now time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("save cover: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE books SET own_cover = ?, picked_cover_id = 0 WHERE id = ?`, now.UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("save cover: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.Exec(`INSERT INTO book_own_covers (book_id, image) VALUES (?, ?)
		ON CONFLICT(book_id) DO UPDATE SET image = excluded.image`, id, jpeg); err != nil {
		return fmt.Errorf("save cover: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("save cover: %w", err)
	}
	return nil
}

// OwnCoverImage returns the picture of a book's own cover, or sql.ErrNoRows
// when it has none.
func (r *Repo) OwnCoverImage(id int64) ([]byte, error) {
	var img []byte
	if err := r.db.QueryRow(`SELECT image FROM book_own_covers WHERE book_id = ?`, id).Scan(&img); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("read cover: %w", err)
	}
	return img, nil
}

// PickCover makes the book show Open Library cover coverID, one of its other
// editions' covers. It replaces a picture of its own.
func (r *Repo) PickCover(id int64, coverID int) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("pick cover: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE books SET picked_cover_id = ?, own_cover = 0 WHERE id = ?`, coverID, id)
	if err != nil {
		return fmt.Errorf("pick cover: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.Exec(`DELETE FROM book_own_covers WHERE book_id = ?`, id); err != nil {
		return fmt.Errorf("pick cover: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("pick cover: %w", err)
	}
	return nil
}

// ClearOwnCover takes away whatever cover a person chose, so the book shows its
// automatic one again.
func (r *Repo) ClearOwnCover(id int64) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("remove cover: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE books SET own_cover = 0, picked_cover_id = 0 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("remove cover: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.Exec(`DELETE FROM book_own_covers WHERE book_id = ?`, id); err != nil {
		return fmt.Errorf("remove cover: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("remove cover: %w", err)
	}
	return nil
}
