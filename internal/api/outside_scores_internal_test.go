package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/settings"
)

// A title page asks OMDb once and keeps the answer; a Discover card shows what
// is kept; nothing is asked without a key.
func TestOutsideScores(t *testing.T) {
	var asked atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if r.URL.Query().Get("apikey") != "goodkey" {
			_, _ = w.Write([]byte(`{"Response":"False","Error":"Invalid API key!"}`))
			return
		}
		if r.URL.Query().Get("i") == "tt0999999" {
			_, _ = w.Write([]byte(`{"Response":"False","Error":"Incorrect IMDb ID."}`))
			return
		}
		_, _ = w.Write([]byte(`{"Response":"True","imdbRating":"8.7","imdbVotes":"2,100,000","Metascore":"73","Ratings":[{"Source":"Rotten Tomatoes","Value":"83%"}]}`))
	}))
	defer fake.Close()
	s := newBareServer(t)
	s.scores().testBase = fake.URL + "/"

	get := func(path string) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		parts := strings.Split(strings.Split(path, "?")[0], "/")
		req.SetPathValue("kind", parts[3])
		req.SetPathValue("tmdbId", parts[4])
		s.handleTitleScores(w, req)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	put := func(body string) int {
		w := httptest.NewRecorder()
		s.handlePutOMDb(w, httptest.NewRequest(http.MethodPut, "/api/settings/omdb", strings.NewReader(body)))
		return w.Code
	}

	if out := get("/api/scores/movie/603?imdb=tt0133093"); out["set"] != false || out["scores"] != nil {
		t.Fatalf("without a key: %v", out)
	}
	if put(`{"key":"wrongkey"}`) != http.StatusBadRequest {
		t.Error("a refused key was saved")
	}
	if put(`{"key":"goodkey"}`) != http.StatusOK {
		t.Fatal("the key wasn't saved")
	}
	if v, _ := s.Settings.Get(settings.KeyOMDbAPIKey); v != "goodkey" {
		t.Fatalf("saved key = %q", v)
	}
	before := asked.Load()
	out := get("/api/scores/movie/603?imdb=tt0133093")
	sc, _ := out["scores"].(map[string]any)
	if sc["imdb"] != 8.7 || sc["rottenTomatoes"] != 83.0 || sc["metacritic"] != 73.0 || sc["imdbVotes"] != 2100000.0 {
		t.Fatalf("scores = %v", out)
	}
	get("/api/scores/movie/603?imdb=tt0133093")
	if n := asked.Load() - before; n != 1 {
		t.Errorf("OMDb was asked %d times for one title", n)
	}
	// Not known: remembered too.
	get("/api/scores/movie/604?imdb=tt0999999")
	get("/api/scores/movie/604?imdb=tt0999999")
	if n := asked.Load() - before; n != 2 {
		t.Errorf("an unknown title was asked again (%d)", n)
	}
	// A card gets what is kept.
	if v := s.withScores(discoverPayload{TMDBID: 603, MediaType: "movie"}); v.Scores == nil || v.Scores.RottenTomatoes != 83 {
		t.Errorf("card scores = %+v", v.Scores)
	}
	// Kept across a restart.
	s.scoreState = nil
	s.scoresOnce = sync.Once{}
	s.scores().testBase = fake.URL + "/"
	if v := s.keptScoresFor("movie", 603); v == nil || v.IMDb != 8.7 {
		t.Errorf("after a restart: %+v", v)
	}

	// The day's allowance: background lookups stop at their share.
	st := s.scores()
	now := time.Now()
	n := 0
	for st.takeLookup(true, now) {
		n++
	}
	if n != scoreWarmPerDay {
		t.Errorf("background lookups allowed = %d, want %d", n, scoreWarmPerDay)
	}
	if !st.takeLookup(false, now) {
		t.Error("a title page was refused while the day has lookups left")
	}

	if put(`{"remove":true}`) != http.StatusOK || s.omdbClient() != nil {
		t.Error("the key wasn't removed")
	}
}
