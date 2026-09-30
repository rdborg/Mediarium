package queue_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

func seedItem(t *testing.T, repo *queue.Repo, movieID int64, status queue.Status) int64 {
	t.Helper()
	id, err := repo.Enqueue(queue.Item{MovieID: movieID, ReleaseTitle: "Release." + string(status)})
	if err != nil {
		t.Fatal(err)
	}
	if status != queue.StatusQueued {
		if err := repo.SetStatus(id, status, ""); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func TestPauseKeepsTheItemAndItsProgress(t *testing.T) {
	db := openDB(t)
	repo := queue.NewRepo(db)
	lib := library.NewRepo(db)
	m, _ := lib.Add(library.Movie{TMDBID: 1, Title: "Film"})

	id := seedItem(t, repo, m.ID, queue.StatusDownloading)
	if err := repo.SetProgress(id, 42); err != nil {
		t.Fatal(err)
	}
	if err := repo.Pause(id, false); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != queue.StatusPaused || got.Interrupted || got.ProgressPct != 42 || got.CompletedAt != "" {
		t.Fatalf("a paused item keeps its progress and has not finished: %+v", got)
	}
	// Pausing something that is not there says so.
	if err := repo.Pause(9999, false); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("pausing a missing item should say it is not there, got %v", err)
	}
	// A paused download still holds its movie, so nothing else is grabbed for it.
	if busy, _ := repo.HasActiveForMovie(m.ID); !busy {
		t.Fatal("a paused download must still hold its movie")
	}
	// It does not use up a download slot.
	if n, _ := repo.CountRunning(); n != 0 {
		t.Fatalf("a paused download is not running, CountRunning = %d", n)
	}
}

func TestRecoverInterruptedTable(t *testing.T) {
	tests := []struct {
		from            queue.Status
		wantStatus      queue.Status
		wantInterrupted bool
	}{
		{queue.StatusQueued, queue.StatusQueued, false}, // still waiting in line: the line carries on by itself
		{queue.StatusDownloading, queue.StatusPaused, true},
		{queue.StatusImporting, queue.StatusPaused, true},
		{queue.StatusPaused, queue.StatusPaused, false}, // paused by a person stays as it was
		{queue.StatusFailed, queue.StatusFailed, false},
		{queue.StatusCompleted, queue.StatusCompleted, false},
		{queue.StatusStopped, queue.StatusStopped, false},
		{queue.StatusConflict, queue.StatusConflict, false},
	}
	db := openDB(t)
	repo := queue.NewRepo(db)
	lib := library.NewRepo(db)
	ids := make([]int64, len(tests))
	for i, tc := range tests {
		m, _ := lib.Add(library.Movie{TMDBID: 100 + i, Title: "Film"})
		ids[i] = seedItem(t, repo, m.ID, tc.from)
		if tc.from == queue.StatusPaused {
			_ = repo.Pause(ids[i], false)
		}
	}
	n, err := repo.RecoverInterrupted()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected the 2 that were running to be paused, got %d", n)
	}
	for i, tc := range tests {
		got, _ := repo.Get(ids[i])
		if got.Status != tc.wantStatus || got.Interrupted != tc.wantInterrupted {
			t.Errorf("%s: got %s interrupted=%v, want %s interrupted=%v", tc.from, got.Status, got.Interrupted, tc.wantStatus, tc.wantInterrupted)
		}
	}
	// Doing it again changes nothing: a restart never resumes anything.
	if n, _ := repo.RecoverInterrupted(); n != 0 {
		t.Fatalf("second recovery paused %d more", n)
	}
}

func TestResumingClearsTheInterruptedFlag(t *testing.T) {
	db := openDB(t)
	repo := queue.NewRepo(db)
	id := seedItem(t, repo, 0, queue.StatusDownloading)
	_, _ = repo.RecoverInterrupted()
	if it, _ := repo.Get(id); !it.Interrupted {
		t.Fatal("setup: expected an interrupted item")
	}
	if err := repo.SetStatus(id, queue.StatusQueued, ""); err != nil {
		t.Fatal(err)
	}
	if it, _ := repo.Get(id); it.Interrupted || it.Status != queue.StatusQueued {
		t.Fatalf("a resumed item is no longer interrupted: %+v", it)
	}
}

func TestStoppedItemsAreFinishedButPausedOnesAreNot(t *testing.T) {
	db := openDB(t)
	repo := queue.NewRepo(db)
	stopped := seedItem(t, repo, 0, queue.StatusStopped)
	paused := seedItem(t, repo, 0, queue.StatusDownloading)
	_ = repo.Pause(paused, false)
	failed := seedItem(t, repo, 0, queue.StatusFailed)

	finished := map[int64]string{}
	list, _ := repo.List()
	for _, it := range list {
		finished[it.ID] = it.CompletedAt
	}
	if finished[stopped] == "" {
		t.Fatal("a stopped item is finished, so it has a finish time")
	}
	if finished[paused] != "" {
		t.Fatal("a paused item is not finished")
	}
	// Old finished items are pruned, but never one that is paused.
	n, err := repo.PruneFinished(time.Now().Add(time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("expected the stopped and failed items to be pruned, got %d, %v", n, err)
	}
	if _, err := repo.Get(paused); err != nil {
		t.Fatalf("a paused item must survive pruning: %v", err)
	}
	_ = failed

	// Clear finished takes stopped ones too, and leaves paused ones alone.
	again := seedItem(t, repo, 0, queue.StatusStopped)
	if n, _ := repo.ClearFinished(); n != 1 {
		t.Fatalf("expected 1 finished item cleared, got %d", n)
	}
	if _, err := repo.Get(again); err == nil {
		t.Fatal("the stopped item should be gone")
	}
	if _, err := repo.Get(paused); err != nil {
		t.Fatalf("a paused item must survive Clear finished: %v", err)
	}
}
