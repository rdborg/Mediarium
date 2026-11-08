// Package requests keeps the titles basic accounts asked for when they may
// not add titles themselves. An administrator approves (the title is then
// added as the person asked) or declines them.
package requests

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Statuses.
const (
	Pending  = "pending"
	Approved = "approved"
	Declined = "declined"
)

// What a request asks for. Requests made before removal requests existed are
// all adds.
const (
	ActionAdd    = "add"
	ActionRemove = "remove"
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
	// FourK marks a request for the 4K second copy: the title as usual if it
	// isn't in the library yet, and a copy of it in the 4K quality.
	FourK bool `json:"fourK,omitempty"`
	// AvailableAt is when the title was seen in the library after the request
	// was approved ("" until then, "skipped" for requests older than the check).
	AvailableAt string `json:"availableAt,omitempty"`
	// Action is "add" (the usual request) or "remove": a request to take a
	// title out of the library. For a removal Payload holds the library id and
	// the reason the person gave.
	Action string `json:"action"`
	// Reason is why the person wants a title removed (removal requests only).
	Reason string `json:"reason,omitempty"`
}

// Repo stores requests.
type Repo struct{ db *sql.DB }

// NewRepo returns a Repo over db.
func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

const columns = `r.id, r.kind, r.title, r.year, r.poster, r.payload, COALESCE(r.requested_by, 0),
	COALESCE(NULLIF(TRIM(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), ''), u.username, ''),
	r.status, r.note, r.created_at, r.decided_at, r.four_k, r.available_at, r.action`

func scan(sc interface{ Scan(...any) error }) (Request, error) {
	var r Request
	var created string
	var fourK int
	err := sc.Scan(&r.ID, &r.Kind, &r.Title, &r.Year, &r.Poster, &r.Payload, &r.RequestedBy, &r.Requester, &r.Status, &r.Note, &created, &r.DecidedAt, &fourK, &r.AvailableAt, &r.Action)
	if err != nil {
		return r, err
	}
	if r.Action == ActionRemove {
		var p struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal([]byte(r.Payload), &p)
		r.Reason = p.Reason
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339, created)
	r.FourK = fourK != 0
	return r, nil
}

// Create stores a new pending request.
func (rp *Repo) Create(r Request) (Request, error) {
	var by any
	if r.RequestedBy > 0 {
		by = r.RequestedBy
	}
	fourK := 0
	if r.FourK {
		fourK = 1
	}
	action := r.Action
	if action != ActionRemove {
		action = ActionAdd
	}
	res, err := rp.db.Exec(`INSERT INTO requests (kind, title, year, poster, payload, requested_by, four_k, action) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Kind, r.Title, r.Year, r.Poster, r.Payload, by, fourK, action)
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

// CountSince counts the requests an account made since a moment (RFC 3339,
// UTC) that still count against its quota: waiting and approved ones, not
// declined ones, and not requests to remove a title. oldest is when the oldest of them was made ("" when none).
func (rp *Repo) CountSince(by int64, since string) (n int, oldest string, err error) {
	var first sql.NullString
	if err := rp.db.QueryRow(`SELECT COUNT(*), MIN(created_at) FROM requests WHERE requested_by = ? AND action = 'add' AND status <> 'declined' AND created_at >= ?`, by, since).Scan(&n, &first); err != nil {
		return 0, "", fmt.Errorf("count requests: %w", err)
	}
	return n, first.String, nil
}

// Unannounced returns approved requests whose title hasn't been seen in the
// library yet.
func (rp *Repo) Unannounced() ([]Request, error) {
	rows, err := rp.db.Query(`SELECT ` + columns + ` FROM requests r LEFT JOIN users u ON u.id = r.requested_by WHERE r.status = 'approved' AND r.action = 'add' AND r.available_at = '' ORDER BY r.id`)
	if err != nil {
		return nil, fmt.Errorf("list requests: %w", err)
	}
	defer rows.Close()
	var out []Request
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan request: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetAvailable records when a request's title turned up in the library.
func (rp *Repo) SetAvailable(id int64, stamp string) error {
	if _, err := rp.db.Exec(`UPDATE requests SET available_at = ? WHERE id = ?`, stamp, id); err != nil {
		return fmt.Errorf("mark request available: %w", err)
	}
	return nil
}
