package mediaservers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rdborg/mediarium/internal/crypto"
)

// Repo stores media servers. The token is kept encrypted with the app's
// secret key, like every other credential.
type Repo struct {
	db  *sql.DB
	box *crypto.Box
}

func NewRepo(db *sql.DB, box *crypto.Box) *Repo {
	return &Repo{db: db, box: box}
}

const serverColumns = `id, name, kind, base_url, public_url, token_encrypted, enabled, refresh_after_import, path_map, server_id, last_error, last_checked_at`

func (r *Repo) encrypt(token string) (string, error) {
	if token == "" {
		return "", nil
	}
	enc, err := r.box.Encrypt(token)
	if err != nil {
		return "", fmt.Errorf("encrypt media server token: %w", err)
	}
	return enc, nil
}

func encodePathMap(m []PathMapping) (string, error) {
	if m == nil {
		m = []PathMapping{}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("encode path map: %w", err)
	}
	return string(b), nil
}

// Create saves a new server and returns it with its id.
func (r *Repo) Create(s Server) (Server, error) {
	tok, err := r.encrypt(s.Token)
	if err != nil {
		return Server{}, err
	}
	pm, err := encodePathMap(s.PathMap)
	if err != nil {
		return Server{}, err
	}
	res, err := r.db.Exec(
		`INSERT INTO media_servers (name, kind, base_url, public_url, token_encrypted, enabled, refresh_after_import, path_map, server_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.Name, string(s.Kind), s.BaseURL, s.PublicURL, tok, s.Enabled, s.RefreshAfterImport, pm, s.ServerID,
	)
	if err != nil {
		return Server{}, fmt.Errorf("insert media server: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Server{}, fmt.Errorf("get inserted media server id: %w", err)
	}
	return r.Get(id)
}

// Update replaces a server's settings. The caller fills Token (an empty one
// is stored as empty); the last test result and server id are kept unless
// the address, kind or token changed, in which case they are cleared. The
// server id itself only changes through RecordCheck.
func (r *Repo) Update(s Server) (Server, error) {
	old, err := r.Get(s.ID)
	if err != nil {
		return Server{}, err
	}
	tok, err := r.encrypt(s.Token)
	if err != nil {
		return Server{}, err
	}
	pm, err := encodePathMap(s.PathMap)
	if err != nil {
		return Server{}, err
	}
	serverID, lastErr, checked := old.ServerID, old.LastError, nullTime(old.LastCheckedAt)
	if old.Kind != s.Kind || old.BaseURL != s.BaseURL || old.Token != s.Token {
		serverID, lastErr, checked = "", "", sql.NullString{}
	}
	_, err = r.db.Exec(
		`UPDATE media_servers SET name = ?, kind = ?, base_url = ?, public_url = ?, token_encrypted = ?, enabled = ?,
		 refresh_after_import = ?, path_map = ?, server_id = ?, last_error = ?, last_checked_at = ? WHERE id = ?`,
		s.Name, string(s.Kind), s.BaseURL, s.PublicURL, tok, s.Enabled, s.RefreshAfterImport, pm, serverID, lastErr, checked, s.ID,
	)
	if err != nil {
		return Server{}, fmt.Errorf("update media server %d: %w", s.ID, err)
	}
	return r.Get(s.ID)
}

// RecordCheck stores the outcome of a test or refresh: a nil err clears the
// last error. A non-empty serverID (learned from a good test) is saved too.
func (r *Repo) RecordCheck(id int64, serverID string, checkErr error) error {
	msg := ""
	if checkErr != nil {
		msg = checkErr.Error()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var err error
	if serverID != "" {
		_, err = r.db.Exec(`UPDATE media_servers SET last_error = ?, last_checked_at = ?, server_id = ? WHERE id = ?`, msg, now, serverID, id)
	} else {
		_, err = r.db.Exec(`UPDATE media_servers SET last_error = ?, last_checked_at = ? WHERE id = ?`, msg, now, id)
	}
	if err != nil {
		return fmt.Errorf("record media server %d check: %w", id, err)
	}
	return nil
}

// Get returns one server, or an error wrapping sql.ErrNoRows.
func (r *Repo) Get(id int64) (Server, error) {
	row := r.db.QueryRow(`SELECT `+serverColumns+` FROM media_servers WHERE id = ?`, id)
	s, err := r.scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Server{}, fmt.Errorf("media server %d: %w", id, sql.ErrNoRows)
	}
	return s, err
}

// List returns every server in the order they were added.
func (r *Repo) List() ([]Server, error) {
	rows, err := r.db.Query(`SELECT ` + serverColumns + ` FROM media_servers ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list media servers: %w", err)
	}
	defer rows.Close()
	out := []Server{}
	for rows.Next() {
		s, err := r.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list media servers: %w", err)
	}
	return out, nil
}

// Delete removes a server; deleting one that doesn't exist is sql.ErrNoRows.
func (r *Repo) Delete(id int64) error {
	res, err := r.db.Exec(`DELETE FROM media_servers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete media server %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("media server %d: %w", id, sql.ErrNoRows)
	}
	return nil
}

type scanner interface{ Scan(dest ...any) error }

func (r *Repo) scan(sc scanner) (Server, error) {
	var (
		s                Server
		kind, tok, pm    string
		checked          sql.NullString
		enabled, refresh bool
	)
	if err := sc.Scan(&s.ID, &s.Name, &kind, &s.BaseURL, &s.PublicURL, &tok, &enabled, &refresh, &pm, &s.ServerID, &s.LastError, &checked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Server{}, err
		}
		return Server{}, fmt.Errorf("scan media server: %w", err)
	}
	s.Kind, s.Enabled, s.RefreshAfterImport = Kind(kind), enabled, refresh
	if tok != "" {
		plain, err := r.box.Decrypt(tok)
		if err != nil {
			return Server{}, fmt.Errorf("decrypt token for media server %d: %w", s.ID, err)
		}
		s.Token = plain
	}
	if pm != "" {
		if err := json.Unmarshal([]byte(pm), &s.PathMap); err != nil {
			return Server{}, fmt.Errorf("decode path map for media server %d: %w", s.ID, err)
		}
	}
	if s.PathMap == nil {
		s.PathMap = []PathMapping{}
	}
	if checked.Valid {
		if t, err := time.Parse(time.RFC3339Nano, checked.String); err == nil {
			s.LastCheckedAt = t
		}
	}
	return s, nil
}

func nullTime(t time.Time) sql.NullString {
	if t.IsZero() {
		return sql.NullString{}
	}
	return sql.NullString{String: t.UTC().Format(time.RFC3339Nano), Valid: true}
}
