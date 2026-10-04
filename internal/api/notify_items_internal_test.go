package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/store"
)

func TestNotifyItemBuilders(t *testing.T) {
	m := movieItem(library.Movie{TMDBID: 597, Title: "Titanic", Year: 1997, PosterPath: "/abc.jpg"})
	if m.Title != "Titanic" || m.LinkPath != "/title/597" || m.PosterURL != "https://image.tmdb.org/t/p/w342/abc.jpg" {
		t.Errorf("movie item %+v", m)
	}
	if movieItem(library.Movie{}).PosterURL != "" {
		t.Error("a movie without a poster must not get a poster address")
	}

	sr := library.Series{ID: 4, Title: "Severance", Year: 2022}
	tests := []struct {
		name     string
		season   int
		episodes []int
		media    string
		episode  string
	}{
		{"whole show", 0, nil, "show", ""},
		{"season pack", 2, nil, "show", "Season 2"},
		{"one episode", 2, []int{3}, "episode", "S02E03"},
		{"a run of episodes", 1, []int{4, 5, 6}, "episode", "S01E04-E06"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			it := seriesItem(sr, tc.season, tc.episodes)
			if it.Media != tc.media || it.Episode != tc.episode || it.LinkPath != "/series/4" {
				t.Errorf("got %+v", it)
			}
		})
	}
	if it := seriesItemFor(sr, 0, []library.Episode{{Season: 3, Episode: 1}}); it.Episode != "S03E01" {
		t.Errorf("one imported episode: %+v", it)
	}
	if it := seriesItemFor(sr, 0, []library.Episode{{Season: 3, Episode: 1}, {Season: 3, Episode: 2}}); it.Episode != "Season 3" {
		t.Errorf("a pack: %+v", it)
	}
}

// An import reaches a subscribed webhook as one composed message: a clear
// subject, the details, and a link only once an address is set.
func TestNotifyItemReachesTargetsComposed(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan map[string]any, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		got <- m
	}))
	t.Cleanup(srv.Close)

	s := &Server{Settings: settings.New(db, nil), NotifyRepo: notify.NewRepo(db, box)}
	if _, err := s.NotifyRepo.Create(notify.Target{Name: "hook", Type: notify.TargetWebhook, Events: []string{notify.EventImported}, Config: map[string]string{"url": srv.URL}}); err != nil {
		t.Fatal(err)
	}
	next := func() map[string]any {
		t.Helper()
		select {
		case m := <-got:
			return m
		case <-time.After(3 * time.Second):
			t.Fatal("no message arrived")
			return nil
		}
	}
	item := notify.Item{Media: "movie", Title: "Titanic", Year: 1997, Quality: "Bluray-1080p", Path: "/data/Movies/Titanic (1997)/Titanic (1997).mkv", LinkPath: "/title/597"}

	s.notifyItem("imported", item)
	m := next()
	if m["title"] != "Titanic (1997) is ready to watch" || m["event"] != "imported" {
		t.Fatalf("payload %v", m)
	}
	if !strings.Contains(m["message"].(string), "Quality: Bluray-1080p") || m["link"] != nil {
		t.Fatalf("payload %v", m)
	}

	if err := s.Settings.Set(settings.KeyPublicURL, "https://media.example.com", false); err != nil {
		t.Fatal(err)
	}
	s.notifyItem("imported", item)
	if m = next(); m["link"] != "https://media.example.com/title/597" {
		t.Fatalf("payload %v", m)
	}

	// A kind nobody listens for sends nothing.
	s.notifyItem("failed", item)
	select {
	case m := <-got:
		t.Fatalf("an unsubscribed event was sent: %v", m)
	case <-time.After(300 * time.Millisecond):
	}
}
