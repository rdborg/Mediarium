package blocklist_test

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/store"
)

// FuzzAddAndLookup: a release title in any shape can be blocklisted and is
// then found again by its key, whatever bytes it holds.
func FuzzAddAndLookup(f *testing.F) {
	for _, s := range []string{"Some.Movie.2001.1080p-GRP", "  spaces  ", "", "\x00", "a\x00b", "\xff\xfe", "İstanbul", "Ⱥ", "'; DROP TABLE blocklist; --", "日本語", "%_"} {
		f.Add(s, "reason")
	}
	db, err := store.Open(filepath.Join(f.TempDir(), "app.db"))
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { db.Close() })
	repo := blocklist.NewRepo(db)
	f.Fuzz(func(t *testing.T, title, reason string) {
		if err := repo.Add(blocklist.Entry{ReleaseTitle: title, Protocol: "usenet", Reason: reason}); err != nil {
			t.Fatalf("Add(%q): %v", title, err)
		}
		keys, err := repo.Keys()
		if err != nil {
			t.Fatal(err)
		}
		if !keys[blocklist.Key(title)] {
			t.Fatalf("%q is not found by its key %q", title, blocklist.Key(title))
		}
		if k := blocklist.Key(title); blocklist.Key(k) != k {
			t.Fatalf("Key is not stable: %q -> %q -> %q", title, k, blocklist.Key(k))
		}
		if len(keys) > 2000 {
			_ = repo.Clear()
		}
	})
}
