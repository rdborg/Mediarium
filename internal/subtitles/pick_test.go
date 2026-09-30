package subtitles_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rdborg/mediarium/internal/subtitles"
)

func TestPickPrefersTheSameRelease(t *testing.T) {
	results := []subtitles.Result{
		{FileID: 1, Release: "Movie.2001.720p.WEB-DL.x264-OTHER", DownloadsAll: 90000, Rating: 9},
		{FileID: 2, Release: "Movie.2001.1080p.BluRay.x264-GRP", DownloadsAll: 200, Rating: 5},
		{FileID: 3, Release: "Movie 2001 1080p BluRay", DownloadsAll: 5000, Rating: 8},
	}
	best := subtitles.Pick(results, "Movie.2001.1080p.BluRay.x264-GRP.mkv")
	if best == nil || best.FileID != 2 {
		t.Fatalf("expected the subtitle timed for the same release group, got %+v", best)
	}

	// No release info in the file name: fall back to popularity and rating.
	best = subtitles.Pick(results, "movie.mkv")
	if best == nil || best.FileID != 1 {
		t.Fatalf("expected the most popular, best-rated subtitle, got %+v", best)
	}

	if subtitles.Pick(nil, "x.mkv") != nil {
		t.Fatal("no results should give no pick")
	}
}

func TestFindSendsTMDBAndEpisodeParams(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = map[string]string{}
		for k := range r.URL.Query() {
			got[k] = r.URL.Query().Get(k)
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer srv.Close()

	c := subtitles.NewWithBaseURL("k", srv.URL)
	if _, err := c.Find(context.Background(), subtitles.Query{ParentTMDBID: 1399, Season: 2, Episode: 5, Type: "episode", Language: "pt-BR"}); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"parent_tmdb_id": "1399", "season_number": "2", "episode_number": "5", "type": "episode", "languages": "pt-BR"}
	if len(got) != len(want) {
		t.Fatalf("params = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("param %s = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
}

func TestQuotaIsATypedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotAcceptable)
	}))
	defer srv.Close()
	if _, err := subtitles.NewWithBaseURL("k", srv.URL).RequestDownload(context.Background(), 1); err != subtitles.ErrQuota {
		t.Fatalf("expected ErrQuota, got %v", err)
	}
}
