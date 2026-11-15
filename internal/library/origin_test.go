package library_test

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/origin"
	"github.com/rdborg/mediarium/internal/store"
)

// A title keeps how it came into the library; one added without an origin
// (everything from before it was kept) has none.
func TestAddKeepsOrigin(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	repo := library.NewRepo(db)

	cases := []struct {
		name string
		in   origin.Origin
		want origin.Origin
	}{
		{"a list", origin.Origin{Kind: origin.List, Name: "Trending"}, origin.Origin{Kind: origin.List, Name: "Trending"}},
		{"by hand", origin.Origin{Kind: origin.Manual}, origin.Origin{Kind: origin.Manual}},
		{"nothing", origin.Origin{}, origin.Origin{}},
		{"an unknown kind", origin.Origin{Kind: "teleport", Name: "x"}, origin.Origin{}},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := repo.Add(library.Movie{TMDBID: 100 + i, Title: "Film " + c.name, Origin: c.in})
			if err != nil {
				t.Fatal(err)
			}
			got, err := repo.Get(m.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Origin != c.want {
				t.Fatalf("movie origin = %+v, want %+v", got.Origin, c.want)
			}
			s, err := repo.AddSeries(library.Series{TMDBID: 200 + i, Title: "Show " + c.name, Origin: c.in}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if s.Origin != c.want {
				t.Fatalf("series origin = %+v, want %+v", s.Origin, c.want)
			}
			again, err := repo.GetSeries(s.ID)
			if err != nil {
				t.Fatal(err)
			}
			if again.Origin != c.want {
				t.Fatalf("stored series origin = %+v, want %+v", again.Origin, c.want)
			}
		})
	}
}
