package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// The review lists what came out long ago and is still missing, oldest first,
// with where it came from; a kept title leaves it.
func TestWantedReview(t *testing.T) {
	s := newBareServer(t)
	add := func(tmdb int, title, released string) library.Movie {
		t.Helper()
		m, err := s.MovieRepo.Add(library.Movie{TMDBID: tmdb, Title: title, Year: 2000, ReleaseDate: released, Monitored: true})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	add(1, "Sams im Glück", "2012-03-01")
	older := add(2, "Older Film", "2005-06-01")
	add(3, "New Film", "2099-01-01")
	got := add(4, "Got It", "2001-01-01")
	if err := s.MovieRepo.SetStatus(got.ID, library.StatusDownloaded, "Bluray-1080p", "/movies/x.mkv"); err != nil {
		t.Fatal(err)
	}
	sr, err := s.MovieRepo.AddSeries(library.Series{TMDBID: 9, Title: "Old Show", Year: 2010, Monitored: true}, []library.Episode{
		{Season: 1, Episode: 1, AirDate: "2010-01-01", Monitored: true},
		{Season: 1, Episode: 2, AirDate: "2010-01-08", Monitored: true},
		{Season: 0, Episode: 1, AirDate: "2009-01-01", Monitored: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO import_lists (name, source) VALUES ('German classics', 'trakt')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO import_list_items (list_id, ref, kind, tmdb_id, outcome) VALUES (1, 'tmdb:movie:2', 'movie', 2, 'added')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO requests (kind, title, payload, status) VALUES ('movie', 'Sams im Glück', '{"tmdbId":1}', 'approved')`); err != nil {
		t.Fatal(err)
	}

	list := func() []reviewItem {
		t.Helper()
		w := httptest.NewRecorder()
		s.handleWantedReview(w, httptest.NewRequest(http.MethodGet, "/api/wanted/review?days=365", nil))
		var out []reviewItem
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		return out
	}
	items := list()
	if len(items) != 3 || items[0].Title != "Older Film" || items[1].Title != "Old Show" || items[2].Title != "Sams im Glück" {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Origin != (reviewOrigin{Kind: "list", Name: "German classics"}) || items[2].Origin.Kind != "request" {
		t.Errorf("origins = %+v / %+v", items[0].Origin, items[2].Origin)
	}
	if items[1].Missing != 2 || items[1].OutSince != "2010-01-01" || items[1].ID != sr.ID {
		t.Errorf("show = %+v (specials don't count)", items[1])
	}

	w := httptest.NewRecorder()
	s.handleWantedKeep(w, httptest.NewRequest(http.MethodPost, "/api/wanted/review/keep", strings.NewReader(`{"items":[{"kind":"movie","id":`+jsonInt(older.ID)+`}]}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("keep: %d %s", w.Code, w.Body.String())
	}
	if items := list(); len(items) != 2 || items[0].Title != "Old Show" {
		t.Errorf("after keeping: %+v", items)
	}
	w = httptest.NewRecorder()
	s.handleWantedReview(w, httptest.NewRequest(http.MethodGet, "/api/wanted/review?days=5", nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("days=5: %d", w.Code)
	}
}

func jsonInt(n int64) string { b, _ := json.Marshal(n); return string(b) }
