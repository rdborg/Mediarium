package books

import (
	"database/sql"
	"errors"
	"fmt"
)

// Progress is where one person is in one format of a book.
type Progress struct {
	BookID    int64   `json:"bookId"`
	Format    Format  `json:"format"`
	Position  string  `json:"position"` // ebook: an EPUB CFI; audiobook: "track:seconds"
	Percent   float64 `json:"percent"`
	Finished  bool    `json:"finished"`
	UpdatedAt string  `json:"updatedAt"`
}

// GetProgress returns a person's place in a book; ok is false when they never
// opened it.
func (r *Repo) GetProgress(userID, bookID int64, f Format) (Progress, bool, error) {
	p := Progress{BookID: bookID, Format: f}
	err := r.db.QueryRow(`SELECT position, percent, finished, updated_at FROM book_progress WHERE user_id = ? AND book_id = ? AND format = ?`,
		userID, bookID, string(f)).Scan(&p.Position, &p.Percent, &p.Finished, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, false, nil
	}
	if err != nil {
		return p, false, fmt.Errorf("get book progress: %w", err)
	}
	return p, true, nil
}

// SetProgress saves a person's place in a book.
func (r *Repo) SetProgress(userID int64, p Progress) error {
	if p.Percent < 0 {
		p.Percent = 0
	}
	if p.Percent > 100 {
		p.Percent = 100
	}
	_, err := r.db.Exec(`INSERT INTO book_progress (user_id, book_id, format, position, percent, finished, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		ON CONFLICT (user_id, book_id, format) DO UPDATE SET position = excluded.position, percent = excluded.percent,
			finished = excluded.finished, updated_at = excluded.updated_at`,
		userID, p.BookID, string(p.Format), p.Position, p.Percent, p.Finished)
	if err != nil {
		return fmt.Errorf("save book progress: %w", err)
	}
	return nil
}

// ListProgress returns a person's places in every book, the most recent first.
func (r *Repo) ListProgress(userID int64) ([]Progress, error) {
	rows, err := r.db.Query(`SELECT book_id, format, position, percent, finished, updated_at FROM book_progress WHERE user_id = ? ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list book progress: %w", err)
	}
	defer rows.Close()
	out := []Progress{}
	for rows.Next() {
		var p Progress
		var f string
		if err := rows.Scan(&p.BookID, &f, &p.Position, &p.Percent, &p.Finished, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan book progress: %w", err)
		}
		p.Format = Format(f)
		out = append(out, p)
	}
	return out, rows.Err()
}
