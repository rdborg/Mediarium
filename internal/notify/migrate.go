package notify

import (
	"encoding/json"
	"fmt"
)

// MigrateSecrets re-saves the targets that still keep a secret value in plain
// text, so that it ends up encrypted. It matters after a field becomes a
// secret (Discord and webhook addresses and ntfy topics contain the whole
// credential) and for targets saved before the generic config existed, whose
// address sits in a plain column. It returns how many targets it changed and
// is safe to run at every start: with nothing left in plain text it does
// nothing.
func (r *Repo) MigrateSecrets() (int, error) {
	rows, err := r.db.Query(`SELECT id, type, COALESCE(url, ''), config_json FROM notification_targets`)
	if err != nil {
		return 0, fmt.Errorf("look for plain-text notification secrets: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var (
			id            int64
			typ, url, cfg string
		)
		if err := rows.Scan(&id, &typ, &url, &cfg); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan notification target: %w", err)
		}
		if url != "" || r.plainConfigHasSecret(typ, cfg) {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	changed := 0
	for _, id := range ids {
		t, err := r.Get(id)
		if err != nil {
			return changed, fmt.Errorf("load notification target %d: %w", id, err)
		}
		if _, err := r.Update(t); err != nil {
			return changed, fmt.Errorf("encrypt the secrets of notification target %d: %w", id, err)
		}
		changed++
	}
	return changed, nil
}

// plainConfigHasSecret reports whether the plain-text config JSON of a target
// of this type holds a value for a field that is a secret.
func (r *Repo) plainConfigHasSecret(typ, cfgJSON string) bool {
	if cfgJSON == "" {
		return false
	}
	info, ok := TypeByName(typ)
	if !ok {
		return false
	}
	var m map[string]string
	if json.Unmarshal([]byte(cfgJSON), &m) != nil {
		return false
	}
	for _, f := range info.Fields {
		if f.Secret() && m[f.Name] != "" {
			return true
		}
	}
	return false
}
