package indexers

import (
	"database/sql"
	"fmt"

	"github.com/ryanborg/mediarium/internal/crypto"
)

// Repo persists configured indexer instances, decrypting/encrypting API
// keys at the boundary (PRD.md §11 — credentials encrypted at rest).
type Repo struct {
	db  *sql.DB
	box *crypto.Box
}

func NewRepo(db *sql.DB, box *crypto.Box) *Repo {
	return &Repo{db: db, box: box}
}

// Create stores a new indexer instance and returns it with its assigned ID.
func (r *Repo) Create(inst Instance) (Instance, error) {
	if inst.Protocol == "" {
		inst.Protocol = ProtocolUsenet
	}
	var encAPIKey any
	if inst.APIKey != "" {
		enc, err := r.box.Encrypt(inst.APIKey)
		if err != nil {
			return Instance{}, fmt.Errorf("encrypt indexer api key: %w", err)
		}
		encAPIKey = enc
	}
	res, err := r.db.Exec(
		`INSERT INTO indexers (name, definition_id, base_url, api_key_encrypted, protocol, enabled) VALUES (?, ?, ?, ?, ?, ?)`,
		inst.Name, inst.DefinitionID, inst.BaseURL, encAPIKey, string(inst.Protocol), inst.Enabled,
	)
	if err != nil {
		return Instance{}, fmt.Errorf("insert indexer: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Instance{}, fmt.Errorf("get inserted indexer id: %w", err)
	}
	inst.ID = id
	return inst, nil
}

// List returns every configured indexer instance, API keys decrypted.
func (r *Repo) List() ([]Instance, error) {
	rows, err := r.db.Query(`SELECT id, name, definition_id, base_url, api_key_encrypted, protocol, enabled FROM indexers ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list indexers: %w", err)
	}
	defer rows.Close()

	var out []Instance
	for rows.Next() {
		var (
			inst      Instance
			encAPIKey sql.NullString
			protocol  string
		)
		if err := rows.Scan(&inst.ID, &inst.Name, &inst.DefinitionID, &inst.BaseURL, &encAPIKey, &protocol, &inst.Enabled); err != nil {
			return nil, fmt.Errorf("scan indexer: %w", err)
		}
		inst.Protocol = Protocol(protocol)
		if encAPIKey.Valid && encAPIKey.String != "" {
			plain, err := r.box.Decrypt(encAPIKey.String)
			if err != nil {
				return nil, fmt.Errorf("decrypt api key for indexer %d: %w", inst.ID, err)
			}
			inst.APIKey = plain
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}

// Delete removes an indexer instance.
func (r *Repo) Delete(id int64) error {
	_, err := r.db.Exec(`DELETE FROM indexers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete indexer %d: %w", id, err)
	}
	return nil
}

// SetEnabled toggles an indexer instance on/off without editing its config.
func (r *Repo) SetEnabled(id int64, enabled bool) error {
	_, err := r.db.Exec(`UPDATE indexers SET enabled = ? WHERE id = ?`, enabled, id)
	if err != nil {
		return fmt.Errorf("set indexer %d enabled=%v: %w", id, enabled, err)
	}
	return nil
}
