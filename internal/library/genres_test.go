package library_test

import (
	"reflect"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

func TestGenresAndAddedByRoundTrip(t *testing.T) {
	repo := newRepo(t)

	cases := []struct {
		name    string
		genres  []string
		addedBy int64
	}{
		{"unknown genres, unknown adder", nil, 0},
		{"no genres on TMDB", []string{}, 3},
		{"two genres", []string{"Drama", "Crime"}, 7},
	}
	for i, tc := range cases {
		m, err := repo.Add(library.Movie{TMDBID: 100 + i, Title: tc.name, Genres: tc.genres, AddedBy: tc.addedBy})
		if err != nil {
			t.Fatalf("%s: add movie: %v", tc.name, err)
		}
		got, err := repo.Get(m.ID)
		if err != nil {
			t.Fatalf("%s: get movie: %v", tc.name, err)
		}
		if !reflect.DeepEqual(got.Genres, tc.genres) || got.AddedBy != tc.addedBy {
			t.Fatalf("%s: movie round trip: genres %#v addedBy %d", tc.name, got.Genres, got.AddedBy)
		}

		sr, err := repo.AddSeries(library.Series{TMDBID: 200 + i, Title: tc.name, Genres: tc.genres, AddedBy: tc.addedBy}, nil)
		if err != nil {
			t.Fatalf("%s: add series: %v", tc.name, err)
		}
		if !reflect.DeepEqual(sr.Genres, tc.genres) || sr.AddedBy != tc.addedBy {
			t.Fatalf("%s: series round trip: genres %#v addedBy %d", tc.name, sr.Genres, sr.AddedBy)
		}
	}

	// Only the titles added without genres are waiting for them, movies first.
	gaps, err := repo.MissingGenres(10)
	if err != nil {
		t.Fatalf("missing genres: %v", err)
	}
	if len(gaps) != 2 || gaps[0].Kind != "movie" || gaps[0].TMDBID != 100 || gaps[1].Kind != "series" || gaps[1].TMDBID != 200 {
		t.Fatalf("unexpected gaps: %+v", gaps)
	}
	if err := repo.SetMovieGenres(gaps[0].ID, []string{"Comedy"}); err != nil {
		t.Fatalf("set movie genres: %v", err)
	}
	if err := repo.SetSeriesGenres(gaps[1].ID, nil); err != nil {
		t.Fatalf("set series genres: %v", err)
	}
	if gaps, _ := repo.MissingGenres(10); len(gaps) != 0 {
		t.Fatalf("expected no gaps left, got %+v", gaps)
	}
	m, _ := repo.Get(gaps[0].ID)
	if !reflect.DeepEqual(m.Genres, []string{"Comedy"}) {
		t.Fatalf("stored genres not returned: %#v", m.Genres)
	}
}
