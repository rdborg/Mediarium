package api

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/store"
)

func TestWaitingPositions(t *testing.T) {
	items := []queue.Item{
		{ID: 1, Status: queue.StatusDownloading},
		{ID: 2, Status: queue.StatusQueued, Priority: queue.PriorityAutomatic, LineSeq: 2},
		{ID: 3, Status: queue.StatusQueued, Priority: queue.PriorityManual, LineSeq: 3},
		{ID: 4, Status: queue.StatusPaused},
		{ID: 5, Status: queue.StatusQueued, Priority: queue.PriorityAutomatic, LineSeq: 1},
	}
	got := waitingPositions(items)
	want := map[int64]int{3: 1, 5: 2, 2: 3}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for id, pos := range want {
		if got[id] != pos {
			t.Fatalf("download %d is number %d in line, want %d (all: %v)", id, got[id], pos, got)
		}
	}
}

func TestDownloadsAtOnceStaysBetweenOneAndFive(t *testing.T) {
	tests := []struct {
		saved string
		want  int
	}{
		{"", 1}, {"1", 1}, {"3", 3}, {"5", 5}, {"6", 5}, {"99", 5}, {"0", 1}, {"-2", 1}, {"two", 1}, {" 2 ", 2},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("saved %q", tc.saved), func(t *testing.T) {
			db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			s := &Server{Settings: settings.New(db, nil)}
			if tc.saved != "" {
				if err := s.Settings.Set(settings.KeyDownloadsConcurrent, tc.saved, false); err != nil {
					t.Fatal(err)
				}
			}
			if got := s.downloadsAtOnce(); got != tc.want {
				t.Fatalf("saved %q gives %d, want %d", tc.saved, got, tc.want)
			}
		})
	}
}

func TestGrabKindDecidesPriorityAndDuplicates(t *testing.T) {
	tests := []struct {
		name     string
		kind     grabKind
		priority queue.Priority
		refuses  bool
	}{
		{"a release a person picked", grabPicked, queue.PriorityManual, false},
		{"a search a person asked for", grabSearchNow, queue.PriorityManual, true},
		{"the scheduled searches", grabAutomatic, queue.PriorityAutomatic, true},
		{"a search by hand", searchKind(true), queue.PriorityManual, true},
		{"a search by the schedule", searchKind(false), queue.PriorityAutomatic, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.kind.priority(); got != tc.priority {
				t.Errorf("priority %d, want %d", got, tc.priority)
			}
			if got := tc.kind.refusesDuplicates(); got != tc.refuses {
				t.Errorf("refusesDuplicates %v, want %v", got, tc.refuses)
			}
		})
	}
}
