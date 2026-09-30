package trakt_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rdborg/mediarium/internal/trakt"
)

func TestParseListURL(t *testing.T) {
	cases := []struct {
		url      string
		wantUser string
		wantList string
		wantErr  bool
	}{
		{"https://trakt.tv/users/garycrawfordgeorge/lists/imdb-top-250", "garycrawfordgeorge", "imdb-top-250", false},
		{"trakt.tv/users/garycrawfordgeorge/lists/imdb-top-250", "garycrawfordgeorge", "imdb-top-250", false},
		{"https://trakt.tv/users/someone/lists/best-movies/", "someone", "best-movies", false},
		{"https://trakt.tv/users/someone/lists/12345?sort=rank", "someone", "12345", false},
		{"https://trakt.tv/movies/tron-legacy-2010", "", "", true},
		{"not a url at all", "", "", true},
		// The names are put into a request made with Mediarium's own Trakt key.
		{"https://trakt.tv/users/../lists/x", "", "", true},
		{"https://trakt.tv/users/me/lists/..", "", "", true},
		{"https://trakt.tv/users/me%2f..%2fusers/lists/x", "", "", true},
		{"https://trakt.tv/users/me/lists/a%20b", "", "", true},
		{"https://trakt.tv/users/some_user-1/lists/My_List-2", "some_user-1", "My_List-2", false},
	}
	for _, tc := range cases {
		user, list, err := trakt.ParseListURL(tc.url)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseListURL(%q): expected error, got user=%q list=%q", tc.url, user, list)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseListURL(%q): unexpected error: %v", tc.url, err)
			continue
		}
		if user != tc.wantUser || list != tc.wantList {
			t.Errorf("ParseListURL(%q) = (%q, %q), want (%q, %q)", tc.url, user, list, tc.wantUser, tc.wantList)
		}
	}
}

func TestListMoviesRequiresClientID(t *testing.T) {
	c := trakt.New("")
	if _, err := c.ListMovies(context.Background(), "someone", "a-list"); err != trakt.ErrNoClientID {
		t.Fatalf("expected ErrNoClientID, got %v", err)
	}
}

func TestListMoviesParsesFixtureResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/users/someone/lists/a-list/items/movie", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("trakt-api-key"); got != "fixture-client-id" {
			t.Errorf("expected trakt-api-key header %q, got %q", "fixture-client-id", got)
		}
		if got := r.Header.Get("trakt-api-version"); got != "2" {
			t.Errorf("expected trakt-api-version header \"2\", got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		// Real Trakt list-items response shape (movie.ids.tmdb is what this
		// app actually reads) plus one item with no tmdb match, which must
		// be skipped rather than producing a zero-value TMDBID entry.
		fmt.Fprint(w, `[
			{
				"rank": 1,
				"id": 100,
				"listed_at": "2014-09-01T09:10:11.000Z",
				"type": "movie",
				"movie": {
					"title": "TRON: Legacy",
					"year": 2010,
					"ids": {"trakt": 1, "slug": "tron-legacy-2010", "imdb": "tt1104001", "tmdb": 20526}
				}
			},
			{
				"rank": 2,
				"id": 101,
				"listed_at": "2014-09-01T09:11:11.000Z",
				"type": "movie",
				"movie": {
					"title": "Some Obscure Film With No TMDB Match",
					"year": 1999,
					"ids": {"trakt": 2, "slug": "obscure-1999", "imdb": null, "tmdb": null}
				}
			}
		]`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := trakt.NewWithBaseURL("fixture-client-id", srv.URL)
	items, err := c.ListMovies(context.Background(), "someone", "a-list")
	if err != nil {
		t.Fatalf("ListMovies: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item (the one with a tmdb id), got %d: %+v", len(items), items)
	}
	if items[0].TMDBID != 20526 || items[0].Title != "TRON: Legacy" || items[0].Year != 2010 {
		t.Fatalf("unexpected item: %+v", items[0])
	}
}

func TestListMoviesUnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := trakt.NewWithBaseURL("fixture-client-id", srv.URL)
	if _, err := c.ListMovies(context.Background(), "someone", "missing-list"); err == nil {
		t.Fatal("expected an error for a 404 response, got nil")
	}
}

// Sanity check that the fixture JSON in the test above actually matches
// what encoding/json expects for a null "imdb"/"tmdb" (Go zero-values a
// null int field to 0) — asserted separately so a change to the decode
// shape fails with a clear message pointing here.
func TestNullTMDBIDDecodesToZero(t *testing.T) {
	var items []struct {
		Movie struct {
			IDs struct {
				TMDB int `json:"tmdb"`
			} `json:"ids"`
		} `json:"movie"`
	}
	raw := `[{"movie": {"ids": {"tmdb": null}}}]`
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if items[0].Movie.IDs.TMDB != 0 {
		t.Fatalf("expected null tmdb id to decode to 0, got %d", items[0].Movie.IDs.TMDB)
	}
}
