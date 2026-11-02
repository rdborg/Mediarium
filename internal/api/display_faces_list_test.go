package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// TestLibraryListDoesNotCallTMDB is the promise that a library list never
// asks TMDB while it answers, however many titles it holds. The translated
// title is shown only after the background job has stored it.
func TestLibraryListDoesNotCallTMDB(t *testing.T) {
	server, base, admin := loginNewServer(t)
	putJSONStatus(t, admin, base+"/api/settings", map[string]any{"infoLanguage": "it-IT"}, http.StatusOK)

	const n = 300
	for i := 1; i <= n; i++ {
		if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 1000 + i, Title: "Movie", Year: 2000, Monitored: true, PosterPath: "/en.jpg"}); err != nil {
			t.Fatalf("add movie %d: %v", i, err)
		}
	}

	var calls atomic.Int32
	tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "TMDB should not be asked while listing", http.StatusInternalServerError)
	}))
	t.Cleanup(tmdb.Close)
	server.TestSetTMDBBaseURL("k", tmdb.URL)

	list := getJSON[[]map[string]any](t, admin, base+"/api/movies?brief=1")
	if len(list) != n {
		t.Fatalf("listed %d movies, want %d", len(list), n)
	}
	if list[0]["title"] != "Movie" || list[0]["posterUrl"] != "https://image.tmdb.org/t/p/w500/en.jpg" {
		t.Fatalf("a title with no stored translation should stay in English, got %v %v", list[0]["title"], list[0]["posterUrl"])
	}
	if calls.Load() != 0 {
		t.Fatalf("listing %d titles called TMDB %d times", n, calls.Load())
	}
}

// TestStoredDisplayFaceIsShownWithoutAskingAgain checks that once the
// background job has saved a translation, the list uses it and does not ask
// TMDB a second time.
func TestStoredDisplayFaceIsShownWithoutAskingAgain(t *testing.T) {
	server, base, admin := loginNewServer(t)
	putJSONStatus(t, admin, base+"/api/settings", map[string]any{"infoLanguage": "it-IT"}, http.StatusOK)
	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999, Overview: "A plot.", Monitored: true, PosterPath: "/en.jpg"})
	if err != nil {
		t.Fatal(err)
	}
	series, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 1416, Title: "Grey's Anatomy", Year: 2005, Overview: "Follows the doctors.", Monitored: true, PosterPath: "/en.jpg"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 603, "title": "Matrix", "poster_path": "/it.jpg", "name": "Grey's Anatomy",
			"overview": "La vita che salvi potrebbe essere la tua.",
		})
	}))
	t.Cleanup(first.Close)
	server.TestSetTMDBBaseURL("k", first.URL)
	server.TestBackfillDisplayFaces(t.Context())
	if calls.Load() == 0 {
		t.Fatal("the background job did not ask TMDB")
	}

	var again atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		again.Add(1)
		http.Error(w, "listed titles must be read from the database", http.StatusInternalServerError)
	}))
	t.Cleanup(second.Close)
	server.TestSetTMDBBaseURL("k", second.URL)

	list := getJSON[[]map[string]any](t, admin, base+"/api/movies?brief=1")
	if len(list) != 1 || list[0]["title"] != "Matrix" || list[0]["posterUrl"] != "https://image.tmdb.org/t/p/w500/it.jpg" {
		t.Fatalf("stored face = %v %v", list[0]["title"], list[0]["posterUrl"])
	}
	moviePage := getJSON[map[string]any](t, admin, base+"/api/movies/"+jsonID(movie.ID))
	if moviePage["overview"] != "La vita che salvi potrebbe essere la tua." {
		t.Fatalf("stored plot = %v", moviePage["overview"])
	}
	showPage := getJSON[map[string]any](t, admin, base+"/api/series/"+jsonID(series.ID))
	if showPage["overview"] != "La vita che salvi potrebbe essere la tua." || showPage["title"] != "Grey's Anatomy" {
		t.Fatalf("stored show = %v %v", showPage["title"], showPage["overview"])
	}
	if again.Load() != 0 {
		t.Fatalf("the list asked TMDB %d times after the face was stored", again.Load())
	}

	// A face saved before plots were kept is asked once more, then the page
	// reads the plot from the database.
	if err := server.TestExec(`UPDATE display_faces SET overview = NULL WHERE kind = 'tv' AND tmdb_id = ?`, series.TMDBID); err != nil {
		t.Fatal(err)
	}
	var third atomic.Int32
	refill := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		third.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1416, "name": "Grey's Anatomy", "poster_path": "/it.jpg", "overview": "Trama italiana."})
	}))
	t.Cleanup(refill.Close)
	server.TestSetTMDBBaseURL("k", refill.URL)
	server.TestBackfillDisplayFaces(t.Context())
	if third.Load() == 0 {
		t.Fatal("a face without a plot was not asked again")
	}
	showPage = getJSON[map[string]any](t, admin, base+"/api/series/"+jsonID(series.ID))
	if showPage["overview"] != "Trama italiana." {
		t.Fatalf("refilled plot = %v", showPage["overview"])
	}
}
