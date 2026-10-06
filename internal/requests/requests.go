// Package requests keeps the titles basic accounts asked for when they may
// not add titles themselves. An administrator approves (the title is then
// added as the person asked) or declines them.
package requests

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Statuses.
const (
	Pending  = "pending"
	Approved = "approved"
	Declined = "declined"
)

// ErrNotFound is returned for a request that doesn't exist.
var ErrNotFound = errors.New("no such request")

// Request is one title someone asked for.
type Request struct {
	ID          int64     `json:"id"`
	Kind        string    `json:"kind"` // movie, tv, music or book
	Title       string    `json:"title"`
	Year        int       `json:"year,omitempty"`
	Poster      string    `json:"poster,omitempty"`
	Payload     string    `json:"-"` // the add request as sent (JSON)
	RequestedBy int64     `json:"requestedBy,omitempty"`
	Requester   string    `json:"requester,omitempty"` // their display name
	Status      string    `json:"status"`
	Note        string    `json:"note,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	DecidedAt   string    `json:"decidedAt,omitempty"`
}

// Repo stores requests.
type Repo struct{ db *sql.DB }

// NewRepo returns a Repo over db.
func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

const columns = `r.id, r.kind, r.title, r.year, r.poster, r.payload, COALESCE(r.requested_by, 0),
	COALESCE(NULLIF(TRIM(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), ''), u.username, ''),
	r.status, r.note, r.created_at, r.decided_at`

func scan(sc interface{ Scan(...any) error }) (Request, error) {
	var r Request
	var created string
	err := sc.Scan(&r.ID, &r.Kind, &r.Title, &r.Year, &r.Poster, &r.Payload, &r.RequestedBy, &r.Requester, &r.Status, &r.Note, &created, &r.DecidedAt)
	if err != nil {
		return r, err
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return r, nil
}

// Create stores a new pending request.
func (rp *Repo) Create(r Request) (Request, error) {
	var by any
	if r.RequestedBy > 0 {
		by = r.RequestedBy
	}
	res, err := rp.db.Exec(`INSERT INTO requests (kind, title, year, poster, payload, requested_by) VALUES (?, ?, ?, ?, ?, ?)`,
		r.Kind, r.Title, r.Year, r.Poster, r.Payload, by)
	if err != nil {
		return Request{}, fmt.Errorf("save request: %w", err)
	}
	id, _ := res.LastInsertId()
	return rp.Get(id)
}

// Get returns one request.
func (rp *Repo) Get(id int64) (Request, error) {
	r, err := scan(rp.db.QueryRow(`SELECT `+columns+` FROM requests r LEFT JOIN users u ON u.id = r.requested_by WHERE r.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	if err != nil {
		return Request{}, fmt.Errorf("read request: %w", err)
	}
	return r, nil
}

// List returns requests, newest first: every account's when by is 0,
// otherwise only by's.
func (rp *Repo) List(by int64) ([]Request, error) {
	q := `SELECT ` + columns + ` FROM requests r LEFT JOIN users u ON u.id = r.requested_by`
	var args []any
	if by > 0 {
		q += ` WHERE r.requested_by = ?`
		args = append(args, by)
	}
	q += ` ORDER BY CASE r.status WHEN 'pending' THEN 0 ELSE 1 END, r.created_at DESC, r.id DESC LIMIT 500`
	rows, err := rp.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list requests: %w", err)
	}
	defer rows.Close()
	out := []Request{}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan request: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Pending counts the requests waiting for an answer.
func (rp *Repo) Pending() (int, error) {
	var n int
	if err := rp.db.QueryRow(`SELECT COUNT(*) FROM requests WHERE status = 'pending'`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count requests: %w", err)
	}
	return n, nil
}

// PendingFor reports whether by already asked for this title and is waiting.
func (rp *Repo) PendingFor(by int64, kind, title string) (bool, error) {
	var n int
	err := rp.db.QueryRow(`SELECT COUNT(*) FROM requests WHERE requested_by = ? AND kind = ? AND title = ? COLLATE NOCASE AND status = 'pending'`, by, kind, strings.TrimSpace(title)).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check requests: %w", err)
	}
	return n > 0, nil
}

// Decide records an administrator's answer.
func (rp *Repo) Decide(id int64, status, note string, by int64) error {
	var decider any
	if by > 0 {
		decider = by
	}
	res, err := rp.db.Exec(`UPDATE requests SET status = ?, note = ?, decided_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), decided_by = ? WHERE id = ?`, status, note, decider, id)
	if err != nil {
		return fmt.Errorf("answer request: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a request.
func (rp *Repo) Delete(id int64) error {
	res, err := rp.db.Exec(`DELETE FROM requests WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("remove request: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
