package settings

import "fmt"

// Write is one setting to store.
type Write struct {
	Key     string
	Value   string
	Encrypt bool
}

// Batch collects settings to be saved together. Nothing is stored until
// Store.Apply: a save that is refused or fails part way leaves every setting as
// it was.
type Batch struct {
	writes []Write
}

// Set adds a setting to the batch. It never fails; the value is encrypted and
// stored by Apply.
func (b *Batch) Set(key, value string, encrypt bool) error {
	b.writes = append(b.writes, Write{Key: key, Value: value, Encrypt: encrypt})
	return nil
}

// Len is how many settings are waiting.
func (b *Batch) Len() int { return len(b.writes) }

// Apply stores a batch in one database transaction: every value is encrypted
// first, and either all of them are saved or none.
func (s *Store) Apply(b *Batch) error {
	if b == nil || len(b.writes) == 0 {
		return nil
	}
	stored := make([]string, len(b.writes))
	for i, w := range b.writes {
		stored[i] = w.Value
		if w.Encrypt {
			enc, err := s.box.Encrypt(w.Value)
			if err != nil {
				return fmt.Errorf("encrypt setting %s: %w", w.Key, err)
			}
			stored[i] = enc
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for i, w := range b.writes {
		if _, err := tx.Exec(
			`INSERT INTO settings (key, value, encrypted) VALUES (?, ?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value, encrypted = excluded.encrypted`,
			w.Key, stored[i], w.Encrypt,
		); err != nil {
			return fmt.Errorf("set setting %s: %w", w.Key, err)
		}
	}
	err = tx.Commit()
	s.forget(b.writes)
	if err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}

// forget drops what the read cache holds for the keys just written.
func (s *Store) forget(writes []Write) {
	s.mu.Lock()
	s.gen++
	for _, w := range writes {
		delete(s.cache, w.Key)
	}
	s.mu.Unlock()
}
