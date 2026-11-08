package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/queue"
)

func TestLocalFault(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"no permission", fmt.Errorf("couldn't move the file into your library: %w", fs.ErrPermission), true},
		{"disk full", fmt.Errorf("copy: %w", syscall.ENOSPC), true},
		{"share went away", errors.New("rename /movies/x.mkv: stale file handle"), true},
		{"share not connected", errors.New("open /data/movies: transport endpoint is not connected"), true},
		{"bad release", badRelease(errors.New("no video file in the download")), false},
		{"missing articles", errors.New("parts of it couldn't be found on your Usenet servers"), false},
		{"nothing", nil, false},
	}
	for _, tt := range tests {
		if got := localFault(tt.err); got != tt.want {
			t.Errorf("%s: localFault = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// A download that failed for a local reason is tried again by itself after a
// wait, three times in all, then left for a person. One a person dealt with
// first is left alone.
func TestDoctorRetriesALocalFailureThreeTimes(t *testing.T) {
	s := newAuditServer(t)
	old := doctorWaits
	doctorWaits = []time.Duration{0, 0, 0}
	t.Cleanup(func() { doctorWaits = old })

	enqueueFailed := func() int64 {
		t.Helper()
		id, err := s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: "Some.Movie.2024.1080p.WEB-DL-GRP", Protocol: "usenet"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.QueueRepo.SetStatus(id, queue.StatusFailed, "Couldn't move the file into your library."); err != nil {
			t.Fatal(err)
		}
		return id
	}
	var retried []int64
	s.doctorRetry = func(item queue.Item) (int64, error) {
		retried = append(retried, item.ID)
		_ = s.QueueRepo.Delete(item.ID)
		return enqueueFailed(), nil // the retry fails the same way
	}
	local := fmt.Errorf("couldn't move the file into your library: %w", fs.ErrPermission)

	id := enqueueFailed()
	for try := 1; try <= 3; try++ {
		s.doctorNoteFailure(id, local)
		s.downloadDoctorJob(context.Background())
		if len(retried) != try || retried[try-1] != id {
			t.Fatalf("try %d: retried %v, want %d last", try, retried, id)
		}
		items, _ := s.QueueRepo.List()
		id = items[0].ID
	}
	s.doctorNoteFailure(id, local) // the third retry failed too
	s.downloadDoctorJob(context.Background())
	if len(retried) != 3 {
		t.Fatalf("tried %d times, want 3", len(retried))
	}

	// A release fault is never retried here, and a download a person retried
	// or removed first is left alone.
	other := enqueueFailed()
	s.doctorNoteFailure(other, badRelease(errors.New("no video file in the download")))
	s.downloadDoctorJob(context.Background())
	if len(retried) != 3 {
		t.Fatal("a bad release was retried")
	}
	third, err := s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: "Another.Movie.2023.2160p-GRP", Protocol: "usenet"})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.QueueRepo.SetStatus(third, queue.StatusFailed, "x")
	s.doctorNoteFailure(third, local)
	_ = s.QueueRepo.Delete(third)
	s.downloadDoctorJob(context.Background())
	if len(retried) != 3 {
		t.Fatal("a removed download was retried")
	}
}

// A running download that hasn't moved for half an hour gets a note.
func TestStuckNote(t *testing.T) {
	s := newAuditServer(t)
	id, err := s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: "Some.Show.S01E01.1080p-GRP", Protocol: "torrent"})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.QueueRepo.SetStatus(id, queue.StatusDownloading, "")
	_ = s.QueueRepo.SetProgress(id, 42)
	start := time.Now()
	s.watchProgress(start)
	if note := s.stuckNote(id, start.Add(29*time.Minute)); note != "" {
		t.Errorf("noted too early: %q", note)
	}
	s.watchProgress(start.Add(20 * time.Minute)) // no change: still since start
	if note := s.stuckNote(id, start.Add(45*time.Minute)); !strings.Contains(note, "45 minutes") {
		t.Errorf("note = %q", note)
	}
	if note := s.stuckNote(id, start.Add(3*time.Hour)); !strings.Contains(note, "3 hours") {
		t.Errorf("note = %q", note)
	}
	_ = s.QueueRepo.SetProgress(id, 43)
	s.watchProgress(start.Add(50 * time.Minute))
	if note := s.stuckNote(id, start.Add(60*time.Minute)); note != "" {
		t.Errorf("moved again but still noted: %q", note)
	}
	_ = s.QueueRepo.SetStatus(id, queue.StatusPaused, "")
	s.watchProgress(start.Add(2 * time.Hour))
	if note := s.stuckNote(id, start.Add(5*time.Hour)); note != "" {
		t.Errorf("a paused download is noted: %q", note)
	}
}
