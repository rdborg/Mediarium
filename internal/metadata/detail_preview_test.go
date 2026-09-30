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

func TestMovieDirectors(t *testing.T) {
	tests := []struct {
		name string
		crew string
		want string
	}{
		{"one director", `[{"name":"A","job":"Director"},{"name":"B","job":"Writer"}]`, "A"},
		{"listed twice", `[{"name":"A","job":"Director"},{"name":"A","job":"Director"}]`, "A"},
		{"only the director job", `[{"name":"A","job":"Director of Photography"},{"name":"B","job":"Assistant Director"}]`, ""},
		{"at most three", `[{"name":"A","job":"Director"},{"name":"B","job":"Director"},{"name":"C","job":"Director"},{"name":"D","job":"Director"}]`, "A,B,C"},
		{"a blank name", `[{"name":"","job":"Director"},{"name":"B","job":"Director"}]`, "B"},
		{"no crew", `[]`, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"id":1,"title":"X","credits":{"cast":[],"crew":` + tc.crew + `}}`))
			}))
			defer srv.Close()
			d, err := metadata.NewWithBaseURL("k", srv.URL).GetMovieDetail(context.Background(), 1)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(d.Directors(), ","); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestShowCreatorsAndSeasons(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":1,"name":"S","created_by":[{"name":"One"},{"name":""},{"name":"Two"}],
			"external_ids":{"imdb_id":"tt1"},
			"seasons":[{"season_number":1,"name":"Season 1","episode_count":8,"air_date":"2020-01-01"}]}`))
	}))
	defer srv.Close()
	d, err := metadata.NewWithBaseURL("k", srv.URL).GetShowFull(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(d.Creators(), ","); got != "One,Two" {
		t.Errorf("creators %q", got)
	}
	if d.ExternalIDs.IMDBID != "tt1" {
		t.Errorf("imdb id %q", d.ExternalIDs.IMDBID)
	}
	if len(d.Seasons) != 1 || d.Seasons[0].Name != "Season 1" || d.Seasons[0].EpisodeCount != 8 || d.Seasons[0].AirDate != "2020-01-01" {
		t.Errorf("seasons %+v", d.Seasons)
	}
}

// The detail of a title is kept for a while, and a failed answer is not.
func TestDetailIsCachedOnlyOnSuccess(t *testing.T) {
	var hits atomic.Int64
	fail := atomic.Bool{}
	fail.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/tv/") {
			_, _ = w.Write([]byte(`{"id":2,"name":"Show"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":1,"title":"Movie"}`))
	}))
	defer srv.Close()
	c := metadata.NewWithBaseURL("k", srv.URL)
	ctx := context.Background()

	if _, err := c.GetMovieDetail(ctx, 1); err == nil {
		t.Fatal("expected the failure")
	}
	fail.Store(false)
	for i := 0; i < 3; i++ {
		if d, err := c.GetMovieDetail(ctx, 1); err != nil || d.Title != "Movie" {
			t.Fatalf("movie: %v %+v", err, d)
		}
		if d, err := c.GetShowFull(ctx, 2); err != nil || d.Name != "Show" {
			t.Fatalf("show: %v %+v", err, d)
		}
	}
	if n := hits.Load(); n != 3 { // the failure, then one movie and one show
		t.Fatalf("%d requests, want 3", n)
	}
}
