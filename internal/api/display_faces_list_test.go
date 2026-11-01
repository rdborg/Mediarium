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
	if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999, Monitored: true, PosterPath: "/en.jpg"}); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 603, "title": "Matrix", "poster_path": "/it.jpg", "name": "Matrix"})
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
	if again.Load() != 0 {
		t.Fatalf("the list asked TMDB %d times after the face was stored", again.Load())
	}
}
