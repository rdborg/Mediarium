package library_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

func TestTitleNamesComeWithMoviesAndShows(t *testing.T) {
	repo := newRepo(t)
	m, err := repo.Add(library.Movie{TMDBID: 120, Title: "The Lord of the Rings: The Fellowship of the Ring", Year: 2001, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	other, err := repo.Add(library.Movie{TMDBID: 121, Title: "The Two Towers", Year: 2002})
	if err != nil {
		t.Fatal(err)
	}
	show, err := repo.AddSeries(library.Series{TMDBID: 120, Title: "Fixture Show", Year: 2011}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok, err := repo.TitleNames("movie", 120); err != nil || ok {
		t.Fatalf("a title nobody asked about should not be found: ok=%v err=%v", ok, err)
	}

	names := []string{"Der Herr der Ringe: Die Gefaehrten", "Der Herr der Ringe"}
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	if err := repo.SaveTitleNames("movie", 120, names, "German", now); err != nil {
		t.Fatal(err)
	}
	got, ok, err := repo.TitleNames("movie", 120)
	if err != nil || !ok {
		t.Fatalf("saved names not found: ok=%v err=%v", ok, err)
	}
	if !reflect.DeepEqual(got.Names, names) || got.Languages != "German" || !got.CheckedAt.Equal(now) {
		t.Fatalf("read back %+v", got)
	}

	// The names of a movie reach the movie in every way it is loaded, and only that movie.
	byID, _ := repo.Get(m.ID)
	byTMDB, _, _ := repo.GetByTMDBID(120)
	list, _ := repo.ListWithCopies()
	if !reflect.DeepEqual(byID.AltTitles, names) || !reflect.DeepEqual(byTMDB.AltTitles, names) {
		t.Errorf("movie got %v / %v", byID.AltTitles, byTMDB.AltTitles)
	}
	for _, l := range list {
		switch l.ID {
		case m.ID:
			if !reflect.DeepEqual(l.AltTitles, names) {
				t.Errorf("listed movie got %v", l.AltTitles)
			}
		case other.ID:
			if len(l.AltTitles) != 0 {
				t.Errorf("another movie got %v", l.AltTitles)
			}
		}
	}
	// A movie and a show with the same TMDB id are different titles.
	if s, _ := repo.GetSeries(show.ID); len(s.AltTitles) != 0 {
		t.Errorf("a show took the movie's names: %v", s.AltTitles)
	}

	if err := repo.SaveTitleNames("tv", 120, []string{"Beispielserie"}, "German", now); err != nil {
		t.Fatal(err)
	}
	if s, _ := repo.GetSeries(show.ID); !reflect.DeepEqual(s.AltTitles, []string{"Beispielserie"}) {
		t.Errorf("show got %v", s.AltTitles)
	}
	shows, _ := repo.ListSeries()
	if len(shows) != 1 || !reflect.DeepEqual(shows[0].AltTitles, []string{"Beispielserie"}) {
		t.Errorf("listed show got %+v", shows)
	}

	// Looked at, nothing extra: found, with no names.
	if err := repo.SaveTitleNames("movie", 120, nil, "German", now); err != nil {
		t.Fatal(err)
	}
	if got, ok, _ := repo.TitleNames("movie", 120); !ok || len(got.Names) != 0 {
		t.Errorf("an empty list should be remembered as looked at: %+v ok=%v", got, ok)
	}

	if err := repo.ClearTitleNames(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := repo.TitleNames("tv", 120); ok {
		t.Error("names should be gone after clearing")
	}
}

// Names saved before umlaut spellings existed still get them when read, so a
// title already in the library is searched and matched as "Glueck" too.
func TestTitleNamesGetUmlautSpellingsWhenRead(t *testing.T) {
	repo := newRepo(t)
	m, err := repo.Add(library.Movie{TMDBID: 130, Title: "Sams in Luck", Year: 2012})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveTitleNames("movie", 130, []string{"Sams im Glück"}, "German", time.Now()); err != nil {
		t.Fatal(err)
	}
	want := []string{"Sams im Glück", "Sams im Glueck"}
	got, _ := repo.Get(m.ID)
	if !reflect.DeepEqual(got.AltTitles, want) {
		t.Errorf("movie names %v, want %v", got.AltTitles, want)
	}
	rec, _, _ := repo.TitleNames("movie", 130)
	if !reflect.DeepEqual(rec.Names, want) {
		t.Errorf("saved names %v, want %v", rec.Names, want)
	}
}
