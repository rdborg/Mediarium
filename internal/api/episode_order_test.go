package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/settings"
)

// officeS04TMDB and officeS04TVDB are season 4 of The Office as the movie
// database (14 episodes, doubles counted once) and TheTVDB (19) list it.
var officeS04TMDB = [][2]string{
	{"Fun Run", "2007-09-27"}, {"Dunder Mifflin Infinity", "2007-10-04"}, {"Launch Party", "2007-10-11"}, {"Money", "2007-10-18"},
	{"Local Ad", "2007-10-25"}, {"Branch Wars", "2007-11-01"}, {"Survivor Man", "2007-11-08"}, {"The Deposition", "2007-11-15"},
	{"Dinner Party", "2008-04-10"}, {"Chair Model", "2008-04-17"}, {"Night Out", "2008-04-24"}, {"Did I Stutter?", "2008-05-01"},
	{"Job Fair", "2008-05-08"}, {"Goodbye, Toby", "2008-05-15"},
}

var officeS04TVDB = [][2]string{
	{"Fun Run (1)", "2007-09-27"}, {"Fun Run (2)", "2007-09-27"}, {"Dunder Mifflin Infinity (1)", "2007-10-04"}, {"Dunder Mifflin Infinity (2)", "2007-10-04"},
	{"Launch Party (1)", "2007-10-11"}, {"Launch Party (2)", "2007-10-11"}, {"Money (1)", "2007-10-18"}, {"Money (2)", "2007-10-18"},
	{"Local Ad", "2007-10-25"}, {"Branch Wars", "2007-11-01"}, {"Survivor Man", "2007-11-08"}, {"The Deposition", "2007-11-15"},
	{"Dinner Party", "2008-04-10"}, {"Chair Model", "2008-04-17"}, {"Night Out", "2008-04-24"}, {"Did I Stutter?", "2008-05-01"},
	{"Job Fair", "2008-05-08"}, {"Goodbye, Toby (1)", "2008-05-15"}, {"Goodbye, Toby (2)", "2008-05-15"},
}

func officeEpisodes(season int, list [][2]string) []library.Episode {
	out := make([]library.Episode, len(list))
	for i, e := range list {
		out[i] = library.Episode{Season: season, Episode: i + 1, Title: e[0], AirDate: e[1]}
	}
	return out
}

// fakeTVDBOffice answers the login for the key "good" and lists The Office
// (show 73244) with TheTVDB's season 4 and a special of its own; any other
// show is unknown.
func fakeTVDBOffice(t *testing.T) *httptest.Server {
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
		if r.PathValue("id") != "73244" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		eps := []map[string]any{{"seasonNumber": 0, "number": 1, "name": "A TheTVDB special", "aired": "2007-01-01"}}
		for i, e := range officeS04TVDB {
			eps = append(eps, map[string]any{"seasonNumber": 4, "number": i + 1, "name": e[0], "aired": e[1]})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"episodes": eps}, "links": map[string]any{"next": nil}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestTVDBOrderedEpisodes(t *testing.T) {
	s := newBareServer(t)
	s.tvdbTestBase = fakeTVDBOffice(t).URL
	ctx := context.Background()
	sr := library.Series{Title: "The Office"}
	tmdb := append([]library.Episode{{Season: 0, Episode: 1, Title: "Webisode"}}, officeEpisodes(4, officeS04TMDB)...)

	if _, err := s.tvdbOrderedEpisodes(ctx, sr, 73244, tmdb); err == nil {
		t.Fatal("without a saved key the order can't be read")
	}
	_ = s.Settings.Set(settings.KeyTVDBAPIKey, "good", true)
	// Filling in shows is off, and the order doesn't need it.
	got, err := s.tvdbOrderedEpisodes(ctx, sr, 73244, tmdb)
	if err != nil {
		t.Fatal(err)
	}
	seasons := map[int]int{}
	for _, e := range got {
		seasons[e.Season]++
		if e.Season == 0 && e.Title != "Webisode" {
			t.Errorf("a special must stay TMDB's: %+v", e)
		}
	}
	if seasons[0] != 1 || seasons[4] != 19 {
		t.Errorf("episodes per season %v, want 1 special and 19 in season 4", seasons)
	}
	// The show's own TheTVDB id is the fallback.
	if _, err := s.tvdbOrderedEpisodes(ctx, library.Series{Title: "The Office", TVDBID: 73244}, 0, tmdb); err != nil {
		t.Errorf("the saved id should do: %v", err)
	}
	if _, err := s.tvdbOrderedEpisodes(ctx, sr, 0, tmdb); err == nil {
		t.Error("a show with no TheTVDB id can't be read")
	}
	if _, err := s.tvdbOrderedEpisodes(ctx, sr, 999, tmdb); err == nil {
		t.Error("a show TheTVDB doesn't know can't be read")
	}
	_ = s.Settings.Set(settings.KeyTVDBAPIKey, "bad", true)
	if _, err := s.tvdbOrderedEpisodes(ctx, sr, 73244, tmdb); err == nil {
		t.Error("a refused key can't read the order")
	}
}

// syncEpisodes in TheTVDB's order keeps every file on its episode, never
// deletes an episode with a file, and does it again without changing anything.
func TestSyncEpisodesInTheTVDBOrder(t *testing.T) {
	s := newBareServer(t)
	sr, err := s.MovieRepo.AddSeries(library.Series{TMDBID: 2316, Title: "The Office", Monitored: true}, officeEpisodes(4, officeS04TMDB))
	if err != nil {
		t.Fatal(err)
	}
	old, _ := s.MovieRepo.ListEpisodes(sr.ID)
	for _, e := range old {
		if e.Title == "Fun Run" || e.Title == "Dunder Mifflin Infinity" || e.Title == "Goodbye, Toby" {
			_ = s.MovieRepo.SetEpisodeStatus(e.ID, library.StatusDownloaded, "HDTV", "/tv/office/"+strconv.Itoa(e.Episode)+".mkv")
		}
	}
	target := officeEpisodes(4, officeS04TVDB)
	for run := 0; run < 2; run++ {
		list, blocked, err := s.syncEpisodes(sr, target, true, true)
		if err != nil || len(blocked) != 0 || len(list) != 19 {
			t.Fatalf("run %d: %d episodes, blocked %v, err %v", run, len(list), blocked, err)
		}
		got, _ := s.MovieRepo.ListEpisodes(sr.ID)
		if len(got) != 19 {
			t.Fatalf("run %d: %d stored, want 19", run, len(got))
		}
		files := map[int]string{}
		for _, e := range got {
			if e.FilePath != "" {
				files[e.Episode] = e.FilePath
			}
		}
		if len(files) != 3 || files[1] != "/tv/office/1.mkv" || files[3] != "/tv/office/2.mkv" || files[18] != "/tv/office/14.mkv" {
			t.Errorf("run %d: files %v", run, files)
		}
	}
}

func TestSetEpisodeOrderRefusals(t *testing.T) {
	s := newBareServer(t)
	s.tvdbTestBase = fakeTVDBOffice(t).URL
	std, _ := s.MovieRepo.AddSeries(library.Series{TMDBID: 1, Title: "Show", Monitored: true}, nil)
	ani, _ := s.MovieRepo.AddSeries(library.Series{TMDBID: 2, Title: "Anime", Monitored: true, SeriesType: library.SeriesAnime}, nil)
	put := func(id, body string) (int, string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPut, "/api/series/"+id+"/episode-order", strings.NewReader(body))
		r.SetPathValue("id", id)
		s.handleSetEpisodeOrder(w, r)
		return w.Code, w.Body.String()
	}
	idOf := func(sr library.Series) string { return strconv.FormatInt(sr.ID, 10) }
	cases := []struct {
		name, id, body string
		code           int
	}{
		{"a bad id", "x", `{"order":"tvdb"}`, http.StatusBadRequest},
		{"an unknown order", idOf(std), `{"order":"imdb"}`, http.StatusBadRequest},
		{"a show that isn't there", "9999", `{"order":"tvdb"}`, http.StatusNotFound},
		{"an anime show", idOf(ani), `{"order":"tvdb"}`, http.StatusBadRequest},
		{"no TheTVDB key", idOf(std), `{"order":"tvdb"}`, http.StatusBadRequest},
		{"the order it already has", idOf(std), `{"order":"tmdb"}`, http.StatusOK},
	}
	for _, c := range cases {
		if code, body := put(c.id, c.body); code != c.code {
			t.Errorf("%s: status %d (%s), want %d", c.name, code, body, c.code)
		}
	}
	if got, _ := s.MovieRepo.GetSeries(std.ID); got.EpisodeOrder != library.EpisodeOrderTMDB {
		t.Errorf("a refused change must leave the order alone, got %q", got.EpisodeOrder)
	}
}

// A show's page says whether a TheTVDB key is saved, so the episode order
// picker works for managers who can't read the TheTVDB settings.
func TestSeriesDetailSaysWhetherATheTVDBKeyIsSaved(t *testing.T) {
	s := newBareServer(t)
	sr, _ := s.MovieRepo.AddSeries(library.Series{TMDBID: 1, Title: "Show", Monitored: true}, nil)
	get := func() bool {
		t.Helper()
		id := strconv.FormatInt(sr.ID, 10)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/series/"+id, nil)
		r.SetPathValue("id", id)
		s.handleGetSeries(w, r)
		var out struct {
			Key *bool `json:"thetvdbKey"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Key == nil {
			t.Fatalf("no thetvdbKey in %s (%v)", w.Body.String(), err)
		}
		return *out.Key
	}
	tests := []struct {
		name, key string
		want      bool
	}{
		{"no key", "", false},
		{"a key", "good", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_ = s.Settings.Set(settings.KeyTVDBAPIKey, tc.key, true)
			if got := get(); got != tc.want {
				t.Fatalf("thetvdbKey = %v, want %v", got, tc.want)
			}
		})
	}
}
