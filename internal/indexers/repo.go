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
	tracker   *Tracker
}

func NewRepo(db *sql.DB, box *crypto.Box) *Repo {
	return &Repo{db: db, box: box, tracker: NewTracker(db, nil)}
}

// Tracker is where the repo's indexers keep their back-off and search
// counters.
func (r *Repo) Tracker() *Tracker { return r.tracker }

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
		`INSERT INTO indexers (name, definition_id, base_url, api_key_encrypted, protocol, enabled, kind, settings_json, secrets_encrypted, requests_per_day, priority_rank, min_seeders, extra_categories, torrent_route) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inst.Name, inst.DefinitionID, inst.BaseURL, encAPIKey, string(inst.Protocol), inst.Enabled, string(inst.Kind), settingsJSON, encSecrets, max(0, inst.RequestsPerDay), ClampPriority(inst.Priority), ClampMinSeeders(inst.MinSeeders), inst.ExtraCategories.Encode(), CleanTorrentRoute(inst.TorrentRoute),
	)
	if err != nil {
		return Instance{}, fmt.Errorf("insert indexer: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Instance{}, fmt.Errorf("get inserted indexer id: %w", err)
	}
	inst.ID = id
	inst.Priority = ClampPriority(inst.Priority)
	inst.MinSeeders = ClampMinSeeders(inst.MinSeeders)
	inst.TorrentRoute = CleanTorrentRoute(inst.TorrentRoute)
	inst.Tracker = r.tracker
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
		`UPDATE indexers SET name = ?, base_url = ?, api_key_encrypted = ?, settings_json = ?, secrets_encrypted = ?, enabled = ?, protocol = ?, kind = ?, requests_per_day = ?, min_seeders = ?, extra_categories = ?, torrent_route = ? WHERE id = ?`,
		inst.Name, inst.BaseURL, encAPIKey, settingsJSON, encSecrets, inst.Enabled, string(inst.Protocol), string(defaultKind(inst)), max(0, inst.RequestsPerDay), ClampMinSeeders(inst.MinSeeders), inst.ExtraCategories.Encode(), CleanTorrentRoute(inst.TorrentRoute), inst.ID,
	)
	if err != nil {
		return fmt.Errorf("update indexer %d: %w", inst.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	// Editing an indexer is an attempt to fix it: it gets asked again at once.
	r.tracker.Resume(inst.ID)
	return nil
}

const indexerColumns = `id, name, definition_id, base_url, api_key_encrypted, protocol, enabled, kind, settings_json, secrets_encrypted, last_test_error, last_test_at, priority_rank, requests_per_day, min_seeders, extra_categories, torrent_route`

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
		extraJSON             string
	)
	if err := sc.Scan(&inst.ID, &inst.Name, &inst.DefinitionID, &inst.BaseURL, &encAPIKey, &protocol, &inst.Enabled,
		&kind, &settingsJSON, &encSecrets, &inst.LastTestError, &testAt, &inst.Priority, &inst.RequestsPerDay, &inst.MinSeeders, &extraJSON, &inst.TorrentRoute); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Instance{}, err
		}
		return Instance{}, fmt.Errorf("scan indexer: %w", err)
	}
	inst.Tracker = r.tracker
	inst.ExtraCategories = DecodeExtraCategories(extraJSON)
	inst.TorrentRoute = CleanTorrentRoute(inst.TorrentRoute)
	inst.Protocol = Protocol(protocol)
	inst.Kind = Kind(kind)
	inst.Kind = defaultKind(inst)
	if testAt != "" {
		inst.LastTestAt, _ = time.Parse(time.RFC3339, testAt)
	}
	if encAPIKey.Valid && encAPIKey.String != "" {
		plain, err := r.box.Decrypt(encAPIKey.String)
		if err != nil {
			// One unreadable key must not take the other indexers down with
			// it: this one is marked and left out of searches.
			inst.markUnreadable()
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
			var secrets map[string]string
			if err != nil {
				inst.markUnreadable()
			} else if err := json.Unmarshal([]byte(plain), &secrets); err != nil {
				inst.markUnreadable()
			}
			for k, v := range secrets {
				inst.Settings[k] = v
			}
		}
		inst.Cardigann = r.cardigann
	}
	return inst, nil
}

// SetPriority sets how much an indexer is preferred: 1 to 50, lower first.
func (r *Repo) SetPriority(id int64, priority int) error {
	if priority < PriorityMin || priority > PriorityMax {
		return fmt.Errorf("priority %d is not between %d and %d", priority, PriorityMin, PriorityMax)
	}
	res, err := r.db.Exec(`UPDATE indexers SET priority_rank = ? WHERE id = ?`, priority, id)
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
	r.tracker.Delete(id)
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
