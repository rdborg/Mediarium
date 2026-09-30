package download

import (
	"database/sql"
	"fmt"

	"github.com/rdborg/mediarium/internal/crypto"
)

// StoredClient is a configured Usenet server (a provider account the
// built-in downloader connects to) as persisted in the download_clients
// table, with its own DB id. The table keeps its original name; in the app
// and API these are "Usenet servers".
type StoredClient struct {
	ID       int64
	Name     string
	Config   ClientConfig
	Priority int  // 0 = primary; higher numbers are backup servers
	Enabled  bool // disabled servers are kept but never used
}

// Repo persists Usenet server configs, encrypting the password at rest.
type Repo struct {
	db  *sql.DB
	box *crypto.Box
}

func NewRepo(db *sql.DB, box *crypto.Box) *Repo {
	return &Repo{db: db, box: box}
}

func (r *Repo) encryptPassword(password string) (any, error) {
	if password == "" {
		return nil, nil
	}
	enc, err := r.box.Encrypt(password)
	if err != nil {
		return nil, fmt.Errorf("encrypt usenet server password: %w", err)
	}
	return enc, nil
}

// Create stores a new server (enabled).
func (r *Repo) Create(sc StoredClient) (StoredClient, error) {
	enc, err := r.encryptPassword(sc.Config.Password)
	if err != nil {
		return StoredClient{}, err
	}
	res, err := r.db.Exec(
		`INSERT INTO download_clients (name, host, port, use_ssl, username, password_encrypted, connections, priority, enabled) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		sc.Name, sc.Config.Host, sc.Config.Port, sc.Config.UseSSL, sc.Config.Username, enc, sc.Config.Connections, sc.Priority,
	)
	if err != nil {
		return StoredClient{}, fmt.Errorf("insert usenet server: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return StoredClient{}, fmt.Errorf("get inserted usenet server id: %w", err)
	}
	sc.ID, sc.Enabled = id, true
	return sc, nil
}

// Update rewrites a server. An empty password keeps the stored one, so the
// edit form never has to show or resend it.
func (r *Repo) Update(sc StoredClient) error {
	var (
		res sql.Result
		err error
	)
	if sc.Config.Password == "" {
		res, err = r.db.Exec(
			`UPDATE download_clients SET name = ?, host = ?, port = ?, use_ssl = ?, username = ?, connections = ?, priority = ?, enabled = ? WHERE id = ?`,
			sc.Name, sc.Config.Host, sc.Config.Port, sc.Config.UseSSL, sc.Config.Username, sc.Config.Connections, sc.Priority, sc.Enabled, sc.ID,
		)
	} else {
		var enc any
		if enc, err = r.encryptPassword(sc.Config.Password); err != nil {
			return err
		}
		res, err = r.db.Exec(
			`UPDATE download_clients SET name = ?, host = ?, port = ?, use_ssl = ?, username = ?, password_encrypted = ?, connections = ?, priority = ?, enabled = ? WHERE id = ?`,
			sc.Name, sc.Config.Host, sc.Config.Port, sc.Config.UseSSL, sc.Config.Username, enc, sc.Config.Connections, sc.Priority, sc.Enabled, sc.ID,
		)
	}
	if err != nil {
		return fmt.Errorf("update usenet server %d: %w", sc.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

const selectServers = `SELECT id, name, host, port, use_ssl, username, password_encrypted, connections, priority, enabled FROM download_clients`

func (r *Repo) query(where string) ([]StoredClient, error) {
	rows, err := r.db.Query(selectServers + where + ` ORDER BY priority, id`)
	if err != nil {
		return nil, fmt.Errorf("list usenet servers: %w", err)
	}
	defer rows.Close()

	var out []StoredClient
	for rows.Next() {
		var (
			sc          StoredClient
			username    sql.NullString
			encPassword sql.NullString
		)
		if err := rows.Scan(&sc.ID, &sc.Name, &sc.Config.Host, &sc.Config.Port, &sc.Config.UseSSL, &username, &encPassword, &sc.Config.Connections, &sc.Priority, &sc.Enabled); err != nil {
			return nil, fmt.Errorf("scan usenet server: %w", err)
		}
		sc.Config.Username = username.String
		if encPassword.Valid && encPassword.String != "" {
			plain, err := r.box.Decrypt(encPassword.String)
			if err != nil {
				return nil, fmt.Errorf("decrypt password for usenet server %d: %w", sc.ID, err)
			}
			sc.Config.Password = plain
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// List returns the enabled servers in priority order — what a download uses.
func (r *Repo) List() ([]StoredClient, error) { return r.query(` WHERE enabled = 1`) }

// ListAll returns every server, disabled ones included, for the settings UI.
func (r *Repo) ListAll() ([]StoredClient, error) { return r.query("") }

// Get returns one server by id.
func (r *Repo) Get(id int64) (StoredClient, error) {
	all, err := r.ListAll()
	if err != nil {
		return StoredClient{}, err
	}
	for _, sc := range all {
		if sc.ID == id {
			return sc, nil
		}
	}
	return StoredClient{}, sql.ErrNoRows
}

func (r *Repo) Delete(id int64) error {
	res, err := r.db.Exec(`DELETE FROM download_clients WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete usenet server %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
