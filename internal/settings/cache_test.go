package settings_test

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/store"
)

func openStore(t *testing.T) (*settings.Store, func(q string, args ...any)) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	return settings.New(db, box), func(q string, args ...any) {
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
}

func get(t *testing.T, s *settings.Store, key string) string {
	t.Helper()
	v, err := s.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// A value that was read stays read: the next Get does not go to the database.
// Every way of writing (Set, Apply) shows up at once, and an unset key that
// was read as "" shows what is saved afterwards.
func TestReadsAreKeptUntilTheNextWrite(t *testing.T) {
	s, exec := openStore(t)
	if got := get(t, s, "ui.test"); got != "" {
		t.Fatalf("unset key = %q", got)
	}
	// Changed behind the store's back: the kept answer is still given. This
	// shows the read is not repeated; nothing in the program writes this way.
	exec(`INSERT INTO settings (key, value, encrypted) VALUES ('ui.test', 'sneaky', 0)`)
	if got := get(t, s, "ui.test"); got != "" {
		t.Fatalf("a repeated read went to the database again: %q", got)
	}
	if err := s.Set("ui.test", "one", false); err != nil {
		t.Fatal(err)
	}
	if got := get(t, s, "ui.test"); got != "one" {
		t.Fatalf("after Set = %q", got)
	}
	var b settings.Batch
	_ = b.Set("ui.test", "two", false)
	_ = b.Set("ui.other", "x", false)
	if err := s.Apply(&b); err != nil {
		t.Fatal(err)
	}
	if got := get(t, s, "ui.test"); got != "two" {
		t.Fatalf("after Apply = %q", got)
	}
	if got := get(t, s, "ui.other"); got != "x" {
		t.Fatalf("after Apply = %q", got)
	}
}

// Secrets are never kept in memory by the store: they are read and decrypted
// each time, and a change shows at once.
func TestEncryptedValuesAreNotKept(t *testing.T) {
	s, exec := openStore(t)
	if err := s.Set("secret.key", "hunter2", true); err != nil {
		t.Fatal(err)
	}
	if got := get(t, s, "secret.key"); got != "hunter2" {
		t.Fatalf("secret = %q", got)
	}
	exec(`DELETE FROM settings WHERE key = 'secret.key'`)
	if got := get(t, s, "secret.key"); got != "" {
		t.Fatalf("a removed secret was still answered: %q", got)
	}
}

// Readers and writers at the same time never leave an old value behind.
func TestConcurrentReadsAndWritesEndOnTheLastWrite(t *testing.T) {
	s, _ := openStore(t)
	var wg sync.WaitGroup
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_, _ = s.Get("ui.race")
			}
		}()
	}
	for i := 0; i < 50; i++ {
		if err := s.Set("ui.race", "v", false); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Set("ui.race", "last", false); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if got := get(t, s, "ui.race"); got != "last" {
		t.Fatalf("ended on %q, want the last write", got)
	}
}
