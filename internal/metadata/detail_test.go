package metadata_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rdborg/mediarium/internal/metadata"
)

func TestGenreNamesCachedAndTolerant(t *testing.T) {
	var hits atomic.Int32
	fail := atomic.Bool{}
	fail.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/genre/movie/list" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		hits.Add(1)
		if fail.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"genres":[{"id":28,"name":"Action"},{"id":878,"name":"Science Fiction"}]}`))
	}))
	defer srv.Close()

	c := metadata.NewWithBaseURL("k", srv.URL)
	ctx := context.Background()

	// A failing lookup yields no names, not an error, and is retried later.
	if got := c.GenreNames(ctx, "movie", []int{28}); len(got) != 0 {
		t.Fatalf("expected no names while TMDB is failing, got %v", got)
	}
	fail.Store(false)
	got := c.GenreNames(ctx, "movie", []int{878, 999, 28})
	if strings.Join(got, ",") != "Science Fiction,Action" {
		t.Fatalf("unexpected names %v", got)
	}
	before := hits.Load()
	c.GenreNames(ctx, "movie", []int{28})
	if hits.Load() != before {
		t.Fatal("genre table should be fetched once and then cached")
	}
	if got := c.GenreNames(ctx, "movie", nil); got == nil || len(got) != 0 {
		t.Fatalf("expected an empty non-nil slice, got %#v", got)
	}
}

func TestMovieDetailExtraction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/movie/603" || r.URL.Query().Get("append_to_response") != "videos,release_dates,credits" {
			t.Fatalf("unexpected request %s", r.URL.String())
		}
		w.Write([]byte(`{
			"id": 603, "title": "The Matrix", "release_date": "1999-03-30", "vote_average": 8.2654, "vote_count": 25000,
			"runtime": 136, "tagline": "Welcome to the Real World", "status": "Released",
			"original_language": "en", "homepage": "https://example.com", "imdb_id": "tt0133093",
			"genres": [{"id": 28, "name": "Action"}],
			"release_dates": {"results": [
				{"iso_3166_1": "GB", "release_dates": [{"certification": "15"}]},
				{"iso_3166_1": "US", "release_dates": [{"certification": ""}, {"certification": "R"}]}
			]},
			"videos": {"results": [
				{"name": "Featurette", "site": "YouTube", "key": "feat", "type": "Featurette", "official": true},
				{"name": "Vimeo trailer", "site": "Vimeo", "key": "vim", "type": "Trailer", "official": true},
				{"name": "Fan trailer", "site": "YouTube", "key": "fan", "type": "Trailer", "official": false},
				{"name": "Official Trailer", "site": "YouTube", "key": "off", "type": "Trailer", "official": true}
			]},
			"credits": {"cast": [
				{"name": "Third", "character": "C", "order": 2, "profile_path": "/c.jpg"},
				{"name": "First", "character": "A", "order": 0},
				{"name": "Second", "character": "B", "order": 1}
			]}
		}`))
	}))
	defer srv.Close()

	c := metadata.NewWithBaseURL("k", srv.URL)
	d, err := c.GetMovieDetail(context.Background(), 603)
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	if d.Certification() != "R" {
		t.Fatalf("expected US certification R, got %q", d.Certification())
	}
	if d.Runtime != 136 || d.IMDBID != "tt0133093" || d.Status != "Released" || d.OriginalLanguage != "en" {
		t.Fatalf("unexpected fields: %+v", d)
	}
	if metadata.RoundRating(d.VoteAverage) != 8.3 {
		t.Fatalf("rating rounding: %v", metadata.RoundRating(d.VoteAverage))
	}
	if g := c.MovieGenres(context.Background(), d.Movie); len(g) != 1 || g[0] != "Action" {
		t.Fatalf("genres: %v", g)
	}

	trailers := d.Trailers()
	if len(trailers) != 3 || trailers[0].Key != "off" || trailers[1].Key != "fan" || trailers[2].Key != "feat" {
		t.Fatalf("unexpected trailer order: %+v", trailers)
	}
	if trailers[0].URL != "https://www.youtube.com/watch?v=off" {
		t.Fatalf("unexpected trailer url %q", trailers[0].URL)
	}

	cast := d.Cast(2)
	if len(cast) != 2 || cast[0].Name != "First" || cast[1].Name != "Second" {
		t.Fatalf("unexpected cast: %+v", cast)
	}
}

func TestCertificationFallsBackToAnyCountry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id": 1, "title": "X", "release_dates": {"results": [
			{"iso_3166_1": "DE", "release_dates": [{"certification": ""}]},
			{"iso_3166_1": "FR", "release_dates": [{"certification": "12"}]}
		]}}`))
	}))
	defer srv.Close()
	d, err := metadata.NewWithBaseURL("k", srv.URL).GetMovieDetail(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.Certification() != "12" {
		t.Fatalf("got %q", d.Certification())
	}
}

func TestShowFullExtraction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tv/1396" || r.URL.Query().Get("append_to_response") != "videos,content_ratings,credits,external_ids" {
			t.Fatalf("unexpected request %s", r.URL.String())
		}
		w.Write([]byte(`{
			"id": 1396, "name": "Breaking Bad", "first_air_date": "2008-01-20", "vote_average": 8.9, "vote_count": 12000,
			"status": "Ended", "tagline": "Change the equation", "number_of_seasons": 5, "number_of_episodes": 62,
			"genres": [{"id": 18, "name": "Drama"}],
			"networks": [{"name": "AMC"}],
			"seasons": [{"season_number": 0, "episode_count": 3}, {"season_number": 1, "episode_count": 7}],
			"content_ratings": {"results": [{"iso_3166_1": "DE", "rating": "16"}, {"iso_3166_1": "US", "rating": "TV-MA"}]},
			"videos": {"results": [{"name": "Trailer", "site": "YouTube", "key": "abc", "type": "Trailer", "official": true}]},
			"credits": {"cast": [{"name": "Bryan Cranston", "character": "Walter White", "order": 0}]}
		}`))
	}))
	defer srv.Close()

	d, err := metadata.NewWithBaseURL("k", srv.URL).GetShowFull(context.Background(), 1396)
	if err != nil {
		t.Fatalf("get show: %v", err)
	}
	if d.ContentRating() != "TV-MA" || d.SeasonCount() != 5 || d.NumberOfEpisodes != 62 {
		t.Fatalf("unexpected: %q %d %d", d.ContentRating(), d.SeasonCount(), d.NumberOfEpisodes)
	}
	if n := d.NetworkNames(); len(n) != 1 || n[0] != "AMC" {
		t.Fatalf("networks: %v", n)
	}
	if len(d.Trailers()) != 1 || len(d.Cast(8)) != 1 || d.Status != "Ended" || d.Name != "Breaking Bad" {
		t.Fatalf("unexpected: %+v", d)
	}
}
