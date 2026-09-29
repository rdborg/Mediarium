package queue_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/queue"
	"github.com/ryanborg/mediarium/internal/store"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestItemEvents(t *testing.T) {
	db := openDB(t)
	repo := queue.NewRepo(db)
	lib := library.NewRepo(db)
	m, err := lib.Add(library.Movie{TMDBID: 1, Title: "Film", Year: 2026})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := lib.Add(library.Movie{TMDBID: 2, Title: "Other", Year: 2026})
	sr, err := lib.AddSeries(library.Series{TMDBID: 3, Title: "Show"}, []library.Episode{{Season: 1, Episode: 1}})
	if err != nil {
		t.Fatal(err)
	}

	log := func(e queue.ItemEvent) {
		t.Helper()
		if err := repo.LogItemEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	// Global feed events for the movie (with their level by type), item-only
	// detail, and a show's events.
	if err := repo.LogActivity(m.ID, "grabbed", "Grabbed Film.2026.1080p"); err != nil {
		t.Fatal(err)
	}
	if err := repo.LogActivity(m.ID, "failed", "Film: unpack: corrupt"); err != nil {
		t.Fatal(err)
	}
	log(queue.ItemEvent{MovieID: m.ID, Kind: "searched", Level: queue.LevelWarn, Message: "Searched: 3 releases, none acceptable: 3 CAM/TeleSync", ItemOnly: true})
	log(queue.ItemEvent{MovieID: other.ID, Kind: "searched", Message: "Searched: no releases found", ItemOnly: true})
	if err := repo.LogSeriesActivity(sr.ID, "grabbed", "Grabbed Show.S01E01"); err != nil {
		t.Fatal(err)
	}

	events, err := repo.MovieEvents(m.ID, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("movie events: %+v", events)
	}
	want := []struct {
		kind  string
		level queue.Level
	}{{"searched", queue.LevelWarn}, {"failed", queue.LevelError}, {"grabbed", queue.LevelInfo}}
	for i, w := range want {
		if events[i].Kind != w.kind || events[i].Level != w.level || events[i].At == "" {
			t.Errorf("event %d = %+v, want %s/%s (newest first)", i, events[i], w.kind, w.level)
		}
	}
	if s, _ := repo.SeriesEvents(sr.ID, 200); len(s) != 1 || s[0].Kind != "grabbed" {
		t.Fatalf("series events: %+v", s)
	}

	// The same search outcome again is not repeated.
	log(queue.ItemEvent{MovieID: m.ID, Kind: "searched", Level: queue.LevelWarn, Message: "Searched: 3 releases, none acceptable: 3 CAM/TeleSync", ItemOnly: true})
	if events, _ = repo.MovieEvents(m.ID, 200); len(events) != 3 || events[0].Kind != "searched" {
		t.Fatalf("a repeated search should not add an event: %+v", events)
	}

	// The global feed leaves item-only events out.
	feed, err := repo.ListActivity(100)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range feed {
		if e.EventType == "searched" {
			t.Fatalf("an item-only event reached the global feed: %+v", e)
		}
	}
	if len(feed) != 3 {
		t.Fatalf("global feed: %+v", feed)
	}

	// Item-only events are capped per item; the limit caps the answer.
	for i := 0; i < 320; i++ {
		log(queue.ItemEvent{MovieID: other.ID, Kind: "searched", Message: fmt.Sprintf("Searched: %d releases", i), ItemOnly: true})
	}
	all, _ := repo.MovieEvents(other.ID, 1000)
	if len(all) != 300 || all[0].Message != "Searched: 319 releases" {
		t.Fatalf("want the newest 300 item-only events, got %d (first %+v)", len(all), all[0])
	}
	if some, _ := repo.MovieEvents(other.ID, 200); len(some) != 200 {
		t.Fatalf("limit: got %d", len(some))
	}
}

// Download steps are recorded for the queue item's movie or show.
func TestStatusEvents(t *testing.T) {
	db := openDB(t)
	repo := queue.NewRepo(db)
	lib := library.NewRepo(db)
	m, _ := lib.Add(library.Movie{TMDBID: 1, Title: "Film", Year: 2026})
	id, err := repo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "Film.2026.1080p.WEB-DL-GRP"})
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range []queue.Status{queue.StatusDownloading, queue.StatusImporting, queue.StatusCompleted} {
		if err := repo.SetStatus(id, st, ""); err != nil {
			t.Fatal(err)
		}
	}
	events, _ := repo.MovieEvents(m.ID, 200)
	if len(events) != 2 || events[0].Message != "Download finished, post-processing: Film.2026.1080p.WEB-DL-GRP" ||
		events[1].Message != "Download started: Film.2026.1080p.WEB-DL-GRP" {
		t.Fatalf("download steps: %+v", events)
	}
}
