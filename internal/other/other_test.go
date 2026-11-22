package other

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/store"
)

func newRepo(t *testing.T) *Repo {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewRepo(db)
}

func TestCleanName(t *testing.T) {
	tests := []struct {
		in, want string
		bad      bool
	}{
		{"  Lecture   Series ", "Lecture Series", false},
		{"", "", true},
		{"   ", "", true},
		{strings.Repeat("a", MaxNameLen), strings.Repeat("a", MaxNameLen), false},
		{strings.Repeat("a", MaxNameLen+1), "", true},
		{strings.Repeat("é", MaxNameLen), strings.Repeat("é", MaxNameLen), false},
	}
	for _, tt := range tests {
		got, err := CleanName(tt.in)
		if (err != nil) != tt.bad || got != tt.want {
			t.Errorf("CleanName(%q) = %q, %v", tt.in, got, err)
		}
	}
}

func TestRepoLifecycle(t *testing.T) {
	r := newRepo(t)
	a, err := r.Add("  Home Videos 1998 ", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Home Videos 1998" || a.Status != StatusMissing {
		t.Fatalf("unexpected item %+v", a)
	}
	if _, err := r.Add("home videos 1998", 0, "", ""); !errors.Is(err, ErrExists) {
		t.Errorf("a duplicate name should be refused, got %v", err)
	}
	b, err := r.Add("Another", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Rename(b.ID, "HOME VIDEOS 1998"); !errors.Is(err, ErrExists) {
		t.Errorf("renaming onto another item's name should be refused, got %v", err)
	}
	if err := r.Rename(b.ID, "Another"); err != nil {
		t.Errorf("renaming to its own name should work: %v", err)
	}
	if err := r.SetState(a.ID, StatusDownloaded, "/other/Home Videos 1998", 3, 300); err != nil {
		t.Fatal(err)
	}
	if err := r.SetState(a.ID, StatusDownloading, "", 0, 0); err != nil {
		t.Fatal(err)
	}
	got, err := r.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDownloading || got.Path != "/other/Home Videos 1998" || got.FileCount != 3 || got.SizeBytes != 300 {
		t.Errorf("a new download must keep the files already there: %+v", got)
	}
	list, err := r.List()
	if err != nil || len(list) != 2 || list[0].Name != "Another" {
		t.Errorf("list = %+v, %v", list, err)
	}
	if err := r.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(a.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("a deleted item should be gone, got %v", err)
	}
	if err := r.Delete(a.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("deleting twice should say no rows, got %v", err)
	}
}
