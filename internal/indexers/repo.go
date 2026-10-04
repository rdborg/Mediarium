package indexers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/crypto"
)

// Repo persists configured indexer instances, decrypting/encrypting API
// keys and secret settings at the boundary (credentials
// encrypted at rest).
type Repo struct {
	db        *sql.DB
	box       *crypto.Box
	cardigann *CardigannManager
}

func NewRepo(db *sql.DB, box *crypto.Box) *Repo {
	return &Repo{db: db, box: box}
}

// SetCardigann attaches the engine that runs definition-based indexers to
// every instance the repo returns.
func (r *Repo) SetCardigann(m *CardigannManager) { r.cardigann = m }

// ErrNotFound is returned for an unknown indexer id.
var ErrNotFound = errors.New("indexer not found")

// Secrets lists which of a definition-based indexer's settings are secret
// (stored encrypted, never sent back to the UI).
type Secrets func(name string) bool

func defaultKind(inst Instance) Kind {
	if inst.Kind != "" {
		return inst.Kind
	}
	if inst.Protocol == ProtocolTorrent {
		return KindTorznab
	}
	return KindNewznab
}

// splitSettings separates secret and plain settings.
func splitSettings(settings map[string]string, secret Secrets) (plain, secrets map[string]string) {
	plain, secrets = map[string]string{}, map[string]string{}
	for k, v := range settings {
		if secret != nil && secret(k) {
			if v != "" {
				secrets[k] = v
			}
		} else {
			plain[k] = v
		}
	}
	return plain, secrets
}

func (r *Repo) encodeSettings(settings map[string]string, secret Secrets) (string, any, error) {
	if settings == nil {
		return "", nil, nil
	}
	plain, secrets := splitSettings(settings, secret)
	pj, err := json.Marshal(plain)
	if err != nil {
		return "", nil, fmt.Errorf("encode indexer settings: %w", err)
	}
	var enc any
	if len(secrets) > 0 {
		sj, err := json.Marshal(secrets)
		if err != nil {
			return "", nil, fmt.Errorf("encode indexer secrets: %w", err)
		}
		e, err := r.box.Encrypt(string(sj))
		if err != nil {
			return "", nil, fmt.Errorf("encrypt indexer secrets: %w", err)
		}
		enc = e
	}
	return string(pj), enc, nil
}

// Create stores a new indexer instance and returns it with its assigned ID.
// secret decides which settings are encrypted (nil: none are).
func (r *Repo) Create(inst Instance, secret Secrets) (Instance, error) {
	if inst.Protocol == "" {
		inst.Protocol = ProtocolUsenet
	}
	inst.Kind = defaultKind(inst)
	var encAPIKey any
	if inst.APIKey != "" {
		enc, err := r.box.Encrypt(inst.APIKey)
		if err != nil {
			return Instance{}, fmt.Errorf("encrypt indexer api key: %w", err)
		}
		encAPIKey = enc
	}
	settingsJSON, encSecrets, err := r.encodeSettings(inst.Settings, secret)
	if err != nil {
		return Instance{}, err
	}
	res, err := r.db.Exec(
		`INSERT INTO indexers (name, definition_id, base_url, api_key_encrypted, protocol, enabled, kind, settings_json, secrets_encrypted) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inst.Name, inst.DefinitionID, inst.BaseURL, encAPIKey, string(inst.Protocol), inst.Enabled, string(inst.Kind), settingsJSON, encSecrets,
	)
	if err != nil {
		return Instance{}, fmt.Errorf("insert indexer: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Instance{}, fmt.Errorf("get inserted indexer id: %w", err)
	}
	inst.ID = id
	if inst.IsCardigann() {
		inst.Cardigann = r.cardigann
	}
	return inst, nil
}

// Update rewrites an instance's name, base URL, API key, settings, enabled
// flag and protocol (an empty Kind is derived from the protocol).
func (r *Repo) Update(inst Instance, secret Secrets) error {
	if inst.Protocol == "" {
		inst.Protocol = ProtocolUsenet
	}
	var encAPIKey any
	if inst.APIKey != "" {
		enc, err := r.box.Encrypt(inst.APIKey)
		if err != nil {
			return fmt.Errorf("encrypt indexer api key: %w", err)
		}
		encAPIKey = enc
	}
	settingsJSON, encSecrets, err := r.encodeSettings(inst.Settings, secret)
	if err != nil {
		return err
	}
	res, err := r.db.Exec(
		`UPDATE indexers SET name = ?, base_url = ?, api_key_encrypted = ?, settings_json = ?, secrets_encrypted = ?, enabled = ?, protocol = ?, kind = ? WHERE id = ?`,
		inst.Name, inst.BaseURL, encAPIKey, settingsJSON, encSecrets, inst.Enabled, string(inst.Protocol), string(defaultKind(inst)), inst.ID,
	)
	if err != nil {
		return fmt.Errorf("update indexer %d: %w", inst.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

const indexerColumns = `id, name, definition_id, base_url, api_key_encrypted, protocol, enabled, kind, settings_json, secrets_encrypted, last_test_error, last_test_at, priority`

// List returns every configured indexer instance, secrets decrypted.
func (r *Repo) List() ([]Instance, error) {
	rows, err := r.db.Query(`SELECT ` + indexerColumns + ` FROM indexers ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list indexers: %w", err)
	}
	defer rows.Close()

	var out []Instance
	for rows.Next() {
		inst, err := r.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}

// Get returns one indexer instance, secrets decrypted.
func (r *Repo) Get(id int64) (Instance, error) {
	row := r.db.QueryRow(`SELECT `+indexerColumns+` FROM indexers WHERE id = ?`, id)
	inst, err := r.scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Instance{}, ErrNotFound
	}
	return inst, err
}

type scanner interface{ Scan(dest ...any) error }

func (r *Repo) scan(sc scanner) (Instance, error) {
	var (
		inst                  Instance
		encAPIKey, encSecrets sql.NullString
		protocol, kind        string
		settingsJSON, testAt  string
	)
	if err := sc.Scan(&inst.ID, &inst.Name, &inst.DefinitionID, &inst.BaseURL, &encAPIKey, &protocol, &inst.Enabled,
		&kind, &settingsJSON, &encSecrets, &inst.LastTestError, &testAt, &inst.Priority); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Instance{}, err
		}
		return Instance{}, fmt.Errorf("scan indexer: %w", err)
	}
	inst.Protocol = Protocol(protocol)
	inst.Kind = Kind(kind)
	inst.Kind = defaultKind(inst)
	if testAt != "" {
		inst.LastTestAt, _ = time.Parse(time.RFC3339, testAt)
	}
	if encAPIKey.Valid && encAPIKey.String != "" {
		plain, err := r.box.Decrypt(encAPIKey.String)
		if err != nil {
			return Instance{}, fmt.Errorf("decrypt api key for indexer %d: %w", inst.ID, err)
		}
		inst.APIKey = plain
	}
	if inst.IsCardigann() {
		inst.Settings = map[string]string{}
		if strings.TrimSpace(settingsJSON) != "" {
			if err := json.Unmarshal([]byte(settingsJSON), &inst.Settings); err != nil {
				return Instance{}, fmt.Errorf("read settings for indexer %d: %w", inst.ID, err)
			}
		}
		if encSecrets.Valid && encSecrets.String != "" {
			plain, err := r.box.Decrypt(encSecrets.String)
			if err != nil {
				return Instance{}, fmt.Errorf("decrypt settings for indexer %d: %w", inst.ID, err)
			}
			var secrets map[string]string
			if err := json.Unmarshal([]byte(plain), &secrets); err != nil {
				return Instance{}, fmt.Errorf("read secret settings for indexer %d: %w", inst.ID, err)
			}
			for k, v := range secrets {
				inst.Settings[k] = v
			}
		}
		inst.Cardigann = r.cardigann
	}
	return inst, nil
}

// SetPriority sets how much an indexer is preferred: 1 preferred, 2 normal,
// 3 last resort.
func (r *Repo) SetPriority(id int64, priority int) error {
	if priority < PriorityPreferred || priority > PriorityLast {
		return fmt.Errorf("priority %d is not 1, 2 or 3", priority)
	}
	res, err := r.db.Exec(`UPDATE indexers SET priority = ? WHERE id = ?`, priority, id)
	if err != nil {
		return fmt.Errorf("set indexer priority: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Delete removes an indexer instance.
func (r *Repo) Delete(id int64) error {
	_, err := r.db.Exec(`DELETE FROM indexers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete indexer %d: %w", id, err)
	}
	if r.cardigann != nil {
		r.cardigann.Forget(id)
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

// SetTestResult records the outcome of a Test ("" = passed).
func (r *Repo) SetTestResult(id int64, errMsg string, at time.Time) error {
	_, err := r.db.Exec(`UPDATE indexers SET last_test_error = ?, last_test_at = ? WHERE id = ?`,
		errMsg, at.UTC().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("record indexer %d test result: %w", id, err)
	}
	return nil
}
