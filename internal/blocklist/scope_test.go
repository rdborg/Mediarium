package blocklist_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/store"
)

const wrongYear = "Fixture.Movie.1999.1080p.BluRay.x264-GRP"

func TestBlockedHonoursTheScope(t *testing.T) {
	repo, movies := newRepos(t)
	one, _ := movies.Add(library.Movie{TMDBID: 1, Title: "One", Monitored: true})
	two, _ := movies.Add(library.Movie{TMDBID: 2, Title: "Two", Monitored: true})
	show, err := movies.AddSeries(library.Series{TMDBID: 7, Title: "Show", Monitored: true}, nil)
	if err != nil {
		t.Fatal(err)
	}

	must := func(e blocklist.Entry) {
		t.Helper()
		if err := repo.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	must(blocklist.Entry{ReleaseTitle: "Only.For.One", MovieID: one.ID, ItemOnly: true})
	must(blocklist.Entry{ReleaseTitle: "For.Everyone", MovieID: one.ID})
	must(blocklist.Entry{ReleaseTitle: "Only.For.Show.S01E01", SeriesID: show.ID, ItemOnly: true})
	must(blocklist.Entry{ReleaseTitle: "Tied.To.Nothing", ItemOnly: true}) // no title to limit it to: blocked everywhere

	keys, err := repo.Keys()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		release string
		scope   blocklist.Scope
		want    bool
	}{
		{"scoped entry blocks its own movie", "Only.For.One", blocklist.Scope{MovieID: one.ID}, true},
		{"scoped entry ignores the case of the title", "only.for.one ", blocklist.Scope{MovieID: one.ID}, true},
		{"scoped entry does not block another movie", "Only.For.One", blocklist.Scope{MovieID: two.ID}, false},
		{"scoped entry does not block a search for no title", "Only.For.One", blocklist.Scope{}, false},
		{"scoped entry does not block a series with the same number", "Only.For.One", blocklist.Scope{SeriesID: one.ID}, false},
		{"global entry blocks the movie it came from", "For.Everyone", blocklist.Scope{MovieID: one.ID}, true},
		{"global entry blocks another movie", "For.Everyone", blocklist.Scope{MovieID: two.ID}, true},
		{"global entry blocks a search for no title", "For.Everyone", blocklist.Scope{}, true},
		{"series entry blocks the series", "Only.For.Show.S01E01", blocklist.Scope{SeriesID: show.ID}, true},
		{"series entry does not block a movie", "Only.For.Show.S01E01", blocklist.Scope{MovieID: one.ID}, false},
		{"an entry with no title is global", "Tied.To.Nothing", blocklist.Scope{MovieID: two.ID}, true},
		{"unknown release", "Nothing", blocklist.Scope{MovieID: one.ID}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := blocklist.Blocked(keys, tt.release, tt.scope); got != tt.want {
				t.Fatalf("Blocked(%q, %+v) = %v, want %v", tt.release, tt.scope, got, tt.want)
			}
		})
	}
}

func TestSameReleaseCanBeBlockedForTwoTitles(t *testing.T) {
	repo, movies := newRepos(t)
	one, _ := movies.Add(library.Movie{TMDBID: 1, Title: "One", Monitored: true})
	two, _ := movies.Add(library.Movie{TMDBID: 2, Title: "Two", Monitored: true})
	three, _ := movies.Add(library.Movie{TMDBID: 3, Title: "Three", Monitored: true})
	for _, id := range []int64{one.ID, two.ID} {
		if err := repo.Add(blocklist.Entry{ReleaseTitle: wrongYear, MovieID: id, ItemOnly: true}); err != nil {
			t.Fatal(err)
		}
	}
	// Adding it again refreshes the entry instead of piling up.
	if err := repo.Add(blocklist.Entry{ReleaseTitle: wrongYear, MovieID: one.ID, ItemOnly: true, Reason: "again"}); err != nil {
		t.Fatal(err)
	}
	all, _ := repo.List()
	if len(all) != 2 {
		t.Fatalf("want one entry per title, got %d", len(all))
	}
	keys, _ := repo.Keys()
	if !blocklist.Blocked(keys, wrongYear, blocklist.Scope{MovieID: one.ID}) || !blocklist.Blocked(keys, wrongYear, blocklist.Scope{MovieID: two.ID}) {
		t.Fatal("both titles keep the release away")
	}
	if blocklist.Blocked(keys, wrongYear, blocklist.Scope{MovieID: three.ID}) {
		t.Fatal("a third title can still take it")
	}
	for _, e := range all {
		if !e.ItemOnly || e.MovieID == 0 {
			t.Fatalf("entry %+v should be for one movie", e)
		}
	}
}

func TestItemOnlyNarrowsTheGlobalEntryOfTheSameTitle(t *testing.T) {
	repo, movies := newRepos(t)
	one, _ := movies.Add(library.Movie{TMDBID: 1, Title: "One", Monitored: true})
	two, _ := movies.Add(library.Movie{TMDBID: 2, Title: "Two", Monitored: true})
	// A global entry another movie made for the same release stays global.
	_ = repo.Add(blocklist.Entry{ReleaseTitle: "Release.A", MovieID: two.ID})
	// This movie made its own global entry (a failed download): it is narrowed.
	_ = repo.Add(blocklist.Entry{ReleaseTitle: "Release.B", MovieID: one.ID})
	_ = repo.Add(blocklist.Entry{ReleaseTitle: "Release.B", MovieID: one.ID, ItemOnly: true})
	_ = repo.Add(blocklist.Entry{ReleaseTitle: "Release.A", MovieID: one.ID, ItemOnly: true})

	keys, _ := repo.Keys()
	if blocklist.Blocked(keys, "Release.B", blocklist.Scope{MovieID: two.ID}) {
		t.Fatal("Release.B is only blocked for movie one now")
	}
	if !blocklist.Blocked(keys, "Release.B", blocklist.Scope{MovieID: one.ID}) {
		t.Fatal("Release.B is still blocked for movie one")
	}
	if !blocklist.Blocked(keys, "Release.A", blocklist.Scope{MovieID: two.ID}) {
		t.Fatal("the global entry another movie made must stay global")
	}
}

// An entry for everywhere that is tied to no title (a person blocked a
// download that was for no title) is a block on purpose: blocking the same
// release for one title later leaves it in place.
func TestItemOnlyKeepsAGlobalEntryTiedToNoTitle(t *testing.T) {
	repo, movies := newRepos(t)
	one, _ := movies.Add(library.Movie{TMDBID: 1, Title: "One", Monitored: true})
	two, _ := movies.Add(library.Movie{TMDBID: 2, Title: "Two", Monitored: true})
	if err := repo.Add(blocklist.Entry{ReleaseTitle: "Release.C"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Add(blocklist.Entry{ReleaseTitle: "Release.C", MovieID: one.ID, ItemOnly: true}); err != nil {
		t.Fatal(err)
	}
	keys, _ := repo.Keys()
	for _, sc := range []blocklist.Scope{{MovieID: one.ID}, {MovieID: two.ID}, {}} {
		if !blocklist.Blocked(keys, "Release.C", sc) {
			t.Errorf("Release.C is still blocked everywhere, but not for %+v", sc)
		}
	}
	if list, _ := repo.List(); len(list) != 2 {
		t.Fatalf("want both entries, got %+v", list)
	}
}

func TestSetScopeSwitchesBothWays(t *testing.T) {
	repo, movies := newRepos(t)
	one, _ := movies.Add(library.Movie{TMDBID: 1, Title: "One", Monitored: true})
	two, _ := movies.Add(library.Movie{TMDBID: 2, Title: "Two", Monitored: true})
	_ = repo.Add(blocklist.Entry{ReleaseTitle: wrongYear, MovieID: one.ID})
	_ = repo.Add(blocklist.Entry{ReleaseTitle: "No.Title"})
	list, _ := repo.List()
	var id, noTitle int64
	for _, e := range list {
		if e.ReleaseTitle == wrongYear {
			id = e.ID
		} else {
			noTitle = e.ID
		}
	}

	if err := repo.SetScope(id, true); err != nil {
		t.Fatal(err)
	}
	keys, _ := repo.Keys()
	if blocklist.Blocked(keys, wrongYear, blocklist.Scope{MovieID: two.ID}) || !blocklist.Blocked(keys, wrongYear, blocklist.Scope{MovieID: one.ID}) {
		t.Fatal("after switching to the title only, movie two is free and movie one is blocked")
	}
	if err := repo.SetScope(id, true); err != nil {
		t.Fatalf("switching to the scope it already has is fine: %v", err)
	}
	if err := repo.SetScope(id, false); err != nil {
		t.Fatal(err)
	}
	keys, _ = repo.Keys()
	if !blocklist.Blocked(keys, wrongYear, blocklist.Scope{MovieID: two.ID}) {
		t.Fatal("switched back to everywhere, movie two is blocked again")
	}
	if err := repo.SetScope(noTitle, true); !errors.Is(err, blocklist.ErrNoTitle) {
		t.Fatalf("an entry with no title cannot be limited to one: %v", err)
	}
	if err := repo.SetScope(9999, true); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a missing entry is ErrNoRows, got %v", err)
	}
}

// Entries made before the scope existed carry no scope but may have a movie or
// series id: they keep blocking everywhere.
func TestExistingEntriesStayGlobal(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	movies := library.NewRepo(db)
	one, _ := movies.Add(library.Movie{TMDBID: 1, Title: "One", Monitored: true})
	// The way a row looked before the migration: no scope given.
	if _, err := db.Exec(`INSERT INTO blocklist (release_title, title_key, protocol, reason, movie_id) VALUES ('Old.Release', 'old.release', 'usenet', 'bad', ?)`, one.ID); err != nil {
		t.Fatal(err)
	}
	repo := blocklist.NewRepo(db)
	list, err := repo.List()
	if err != nil || len(list) != 1 || list[0].ItemOnly {
		t.Fatalf("an old entry is for everywhere: %+v, %v", list, err)
	}
	keys, _ := repo.Keys()
	for _, sc := range []blocklist.Scope{{}, {MovieID: one.ID}, {MovieID: one.ID + 1}, {SeriesID: 3}} {
		if !blocklist.Blocked(keys, "Old.Release", sc) {
			t.Fatalf("an old entry must block for %+v", sc)
		}
	}
	if why, ok, _ := repo.Reason("Old.Release"); !ok || why != "bad" {
		t.Fatalf("Reason = %q, %v", why, ok)
	}
}

func TestItemEntryGoesWithItsTitle(t *testing.T) {
	repo, movies := newRepos(t)
	one, _ := movies.Add(library.Movie{TMDBID: 1, Title: "One", Monitored: true})
	_ = repo.Add(blocklist.Entry{ReleaseTitle: wrongYear, MovieID: one.ID, ItemOnly: true})
	if err := movies.Delete(one.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := repo.List(); len(list) != 0 {
		t.Fatalf("the entry should go when its movie is deleted: %+v", list)
	}
}
