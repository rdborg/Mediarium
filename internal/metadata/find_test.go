package metadata_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rdborg/mediarium/internal/metadata"
)

func TestFindShowByTVDBID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("external_source") != "tvdb_id" {
			t.Errorf("external_source = %q, want tvdb_id", r.URL.Query().Get("external_source"))
		}
		switch r.URL.Path {
		case "/find/81189":
			json.NewEncoder(w).Encode(map[string]any{
				"movie_results": []any{},
				"tv_results":    []map[string]any{{"id": 1396, "name": "Breaking Bad", "first_air_date": "2008-01-20"}},
			})
		case "/find/1":
			json.NewEncoder(w).Encode(map[string]any{"movie_results": []any{}, "tv_results": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := metadata.NewWithBaseURL("k", srv.URL)

	tests := []struct {
		name    string
		tvdbID  int
		wantID  int
		wantOK  bool
		wantErr bool
	}{
		{name: "known show", tvdbID: 81189, wantID: 1396, wantOK: true},
		{name: "unknown to TMDB", tvdbID: 1},
		{name: "error status", tvdbID: 404, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			show, ok, err := c.FindShowByTVDBID(context.Background(), tt.tvdbID)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if ok != tt.wantOK || show.TMDBID != tt.wantID {
				t.Fatalf("got %+v ok=%v, want id %d ok=%v", show, ok, tt.wantID, tt.wantOK)
			}
		})
	}
}
