package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/thetvdb"
)

func tmdbEp(season, ep int, aired string) library.Episode {
	return library.Episode{Season: season, Episode: ep, AirDate: aired}
}

func tvdbEp(season, ep int, aired string) thetvdb.Episode {
	return thetvdb.Episode{Season: season, Number: ep, Aired: aired, Name: fmt.Sprintf("S%dE%d", season, ep)}
}

func TestFillFromTVDB(t *testing.T) {
	cases := []struct {
		name  string
		tmdb  []library.Episode
		tvdb  []thetvdb.Episode
		added int
	}{
		{
			"a nearly empty TMDB page gets TheTVDB's seasons (Formula 1)",
			[]library.Episode{tmdbEp(1, 1, "2024-03-02")},
			[]thetvdb.Episode{tvdbEp(2023, 1, "2023-03-05"), tvdbEp(2023, 2, "2023-03-19"), tvdbEp(2024, 1, "2024-03-02")},
			3,
		},
		{
			"episodes missing in a season that agrees are added",
			[]library.Episode{tmdbEp(1, 1, "2020-01-01"), tmdbEp(1, 2, "2020-01-08"), tmdbEp(2, 1, "2021-01-01"), tmdbEp(2, 2, "2021-01-08")},
			[]thetvdb.Episode{tvdbEp(1, 1, "2020-01-01"), tvdbEp(1, 2, "2020-01-09"), tvdbEp(1, 3, "2020-01-15"), tvdbEp(2, 1, "2021-01-01"), tvdbEp(2, 2, "2021-01-08"), tvdbEp(3, 1, "2022-01-01")},
			2, // S01E03 and the whole of season 3
		},
		{
			"a show the two count differently is left alone",
			[]library.Episode{tmdbEp(1, 1, "2019-01-01"), tmdbEp(1, 2, "2019-01-08"), tmdbEp(1, 3, "2019-01-15"), tmdbEp(1, 4, "2019-01-22")},
			[]thetvdb.Episode{tvdbEp(1, 1, "2018-05-01"), tvdbEp(1, 2, "2018-05-08"), tvdbEp(2019, 1, "2019-01-01"), tvdbEp(2019, 2, "2019-01-08")},
			0,
		},
		{
			"specials are never filled",
			[]library.Episode{tmdbEp(1, 1, "2020-01-01")},
			[]thetvdb.Episode{tvdbEp(0, 1, "2019-12-01"), tvdbEp(1, 1, "2020-01-01")},
			0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, added := fillFromTVDB(c.tmdb, c.tvdb)
			if added != c.added || len(out) != len(c.tmdb)+c.added {
				t.Errorf("added %d (list %d), want %d", added, len(out), c.added)
			}
			for i, e := range c.tmdb {
				if out[i] != e {
					t.Errorf("TMDB's episode %d changed: %+v", i, out[i])
				}
			}
		})
	}
}

// fakeTVDB answers a login with the key "good" and lists two pages of episodes.
func fakeTVDB(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["apikey"] != "good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"token": "T"}})
	})
	mux.HandleFunc("GET /series/{id}/episodes/default", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer T" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		page := r.URL.Query().Get("page")
		var next any
		eps := []map[string]any{{"seasonNumber": 2024, "number": 1, "name": "Bahrain", "aired": "2024-03-02"}}
		if page == "0" {
			next = "page=1"
			eps = []map[string]any{{"seasonNumber": 2023, "number": 1, "name": "Bahrain", "aired": "2023-03-05"}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"episodes": eps}, "links": map[string]any{"next": next}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestTVDBFillNeedsTheSwitchAndAKey(t *testing.T) {
	s := newBareServer(t)
	s.tvdbTestBase = fakeTVDB(t).URL
	sr := library.Series{Title: "Formula 1"}
	tmdb := []library.Episode{tmdbEp(1, 1, "2024-03-02")}
	if got := s.withTVDBEpisodes(context.Background(), sr, 387219, tmdb); len(got) != 1 {
		t.Fatalf("with filling off, TMDB's list must stay as it is: %v", got)
	}
	_ = s.Settings.Set(settings.KeyTVDBAPIKey, "good", true)
	_ = s.Settings.Set(settings.KeyTVDBFill, "1", false)
	got := s.withTVDBEpisodes(context.Background(), sr, 387219, tmdb)
	if len(got) != 3 {
		t.Fatalf("both pages of TheTVDB's seasons should be added: %v", got)
	}
	sr.SeriesType = library.SeriesAnime
	if got := s.withTVDBEpisodes(context.Background(), sr, 387219, tmdb); len(got) != 1 {
		t.Errorf("anime must never be filled: %v", got)
	}
	_ = s.Settings.Set(settings.KeyTVDBAPIKey, "bad", true)
	if got := s.withTVDBEpisodes(context.Background(), library.Series{Title: "Formula 1"}, 387219, tmdb); len(got) != 1 {
		t.Errorf("a refused key must leave TMDB's list alone: %v", got)
	}
}

func TestPutTVDBChecksTheKey(t *testing.T) {
	s := newBareServer(t)
	s.tvdbTestBase = fakeTVDB(t).URL
	put := func(body string) (int, string) {
		w := httptest.NewRecorder()
		s.handlePutTVDB(w, httptest.NewRequest(http.MethodPut, "/api/settings/thetvdb", strings.NewReader(body)))
		return w.Code, w.Body.String()
	}
	if code, _ := put(`{"fill": true}`); code != http.StatusBadRequest {
		t.Errorf("filling without a key: status %d", code)
	}
	if code, _ := put(`{"key": "bad"}`); code != http.StatusBadRequest {
		t.Errorf("a refused key: status %d", code)
	}
	if code, body := put(`{"key": "good", "fill": true}`); code != http.StatusOK || !strings.Contains(body, `"set":true`) || !strings.Contains(body, `"fill":true`) {
		t.Errorf("a good key: %d %s", code, body)
	}
	if code, body := put(`{"remove": true}`); code != http.StatusOK || !strings.Contains(body, `"set":false`) || !strings.Contains(body, `"fill":false`) {
		t.Errorf("removing: %d %s", code, body)
	}
}
