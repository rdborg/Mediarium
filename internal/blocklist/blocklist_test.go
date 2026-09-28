package blocklist_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ryanborg/mediarium/internal/blocklist"
	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/store"
)

func newRepos(t *testing.T) (*blocklist.Repo, *library.Repo) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return blocklist.NewRepo(db), library.NewRepo(db)
}

func TestAddIsCaseInsensitiveAndIdempotent(t *testing.T) {
	repo, _ := newRepos(t)
	if err := repo.Add(blocklist.Entry{ReleaseTitle: "Some.Movie.2001.1080p-GRP", Reason: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Add(blocklist.Entry{ReleaseTitle: "  some.movie.2001.1080p-grp ", Reason: "second"}); err != nil {
		t.Fatal(err)
	}
	list, err := repo.List()
	if err != nil || len(list) != 1 || list[0].Reason != "second" {
		t.Fatalf("expected one entry with the refreshed reason, got %v %+v", err, list)
	}
	keys, _ := repo.Keys()
	if !keys[blocklist.Key("SOME.MOVIE.2001.1080P-GRP")] {
		t.Fatalf("lookup should ignore case, keys = %v", keys)
	}
}

func TestRemoveAndClear(t *testing.T) {
	repo, _ := newRepos(t)
	_ = repo.Add(blocklist.Entry{ReleaseTitle: "a"})
	_ = repo.Add(blocklist.Entry{ReleaseTitle: "b"})
	list, _ := repo.List()
	if err := repo.Remove(list[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Remove(list[0].ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("removing twice should report not found, got %v", err)
	}
	if err := repo.Clear(); err != nil {
		t.Fatal(err)
	}
	if list, _ := repo.List(); len(list) != 0 {
		t.Fatalf("clear left %+v", list)
	}
}

func TestCountRecentIsPerTargetAndWindowed(t *testing.T) {
	repo, movies := newRepos(t)
	m1, _ := movies.Add(library.Movie{TMDBID: 1, Title: "One", Monitored: true})
	m2, _ := movies.Add(library.Movie{TMDBID: 2, Title: "Two", Monitored: true})
	_ = repo.Add(blocklist.Entry{ReleaseTitle: "a", MovieID: m1.ID})
	_ = repo.Add(blocklist.Entry{ReleaseTitle: "b", MovieID: m1.ID})
	_ = repo.Add(blocklist.Entry{ReleaseTitle: "c", MovieID: m2.ID})

	if n, _ := repo.CountRecent(m1.ID, 0, time.Now().Add(-time.Hour)); n != 2 {
		t.Fatalf("movie 1 count = %d, want 2", n)
	}
	if n, _ := repo.CountRecent(m2.ID, 0, time.Now().Add(-time.Hour)); n != 1 {
		t.Fatalf("movie 2 count = %d, want 1", n)
	}
	if n, _ := repo.CountRecent(m1.ID, 0, time.Now().Add(time.Hour)); n != 0 {
		t.Fatalf("entries older than the window must not count, got %d", n)
	}
	if n, _ := repo.CountRecent(0, 0, time.Now().Add(-time.Hour)); n != 0 {
		t.Fatalf("no target means no count, got %d", n)
	}
}
