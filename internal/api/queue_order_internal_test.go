package api

import (
	"fmt"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

func TestQueueOrder(t *testing.T) {
	item := func(id int64, st queue.Status, added, completed string) queue.Item {
		return queue.Item{ID: id, Status: st, AddedAt: added, CompletedAt: completed}
	}
	ids := func(items []queue.Item) string {
		out := ""
		for _, it := range items {
			out += fmt.Sprintf("%d ", it.ID)
		}
		return out
	}

	tests := []struct {
		name  string
		items []queue.Item
		want  string
	}{
		{
			"in progress first, then failed, then the rest",
			[]queue.Item{
				item(1, queue.StatusCompleted, "2026-01-01T00:00:00.000Z", "2026-01-01T09:00:00Z"),
				item(2, queue.StatusStopped, "2026-01-01T00:00:00.000Z", "2026-01-01T08:00:00Z"),
				item(3, queue.StatusFailed, "2026-01-01T00:00:00.000Z", "2026-01-01T07:00:00Z"),
				item(4, queue.StatusPaused, "2026-01-01T00:00:00.000Z", ""),
				item(5, queue.StatusQueued, "2026-01-01T00:00:00.000Z", ""),
				item(6, queue.StatusImporting, "2026-01-01T00:00:00.000Z", ""),
				item(7, queue.StatusConflict, "2026-01-01T00:00:00.000Z", ""),
				item(8, queue.StatusDownloading, "2026-01-01T00:00:00.000Z", ""),
			},
			"6 8 5 4 3 7 2 1 ",
		},
		{
			"in progress: the one that started first is on top",
			[]queue.Item{
				item(10, queue.StatusDownloading, "2026-01-01T10:05:00.000Z", ""),
				item(11, queue.StatusDownloading, "2026-01-01T10:01:00.000Z", ""),
				item(12, queue.StatusImporting, "2026-01-01T10:03:00.000Z", ""),
			},
			"11 12 10 ",
		},
		{
			"paused items follow the active ones whatever their start time",
			[]queue.Item{
				item(20, queue.StatusPaused, "2026-01-01T09:00:00.000Z", ""),
				item(21, queue.StatusDownloading, "2026-01-01T11:00:00.000Z", ""),
				item(22, queue.StatusQueued, "2026-01-01T10:00:00.000Z", ""),
			},
			"21 22 20 ",
		},
		{
			"failed: the newest failure first",
			[]queue.Item{
				item(30, queue.StatusFailed, "2026-01-01T01:00:00.000Z", "2026-01-01T05:00:00.123456789Z"),
				item(31, queue.StatusFailed, "2026-01-01T02:00:00.000Z", "2026-01-01T05:00:00Z"),
				item(32, queue.StatusFailed, "2026-01-01T03:00:00.000Z", "2026-01-01T09:00:00Z"),
			},
			// 30 failed a fraction of a second after 31, though "Z" sorts after "."
			"32 30 31 ",
		},
		{
			"the same start time keeps a fixed place by id",
			[]queue.Item{
				item(41, queue.StatusDownloading, "2026-01-01T10:00:00.000Z", ""),
				item(40, queue.StatusDownloading, "2026-01-01T10:00:00.000Z", ""),
			},
			"40 41 ",
		},
		{
			"waiting in line: a person's first, then in the order they were added, whatever their time",
			[]queue.Item{
				{ID: 50, Status: queue.StatusQueued, AddedAt: "2026-01-01T10:00:00.000Z", Priority: queue.PriorityAutomatic, LineSeq: 5},
				{ID: 51, Status: queue.StatusQueued, AddedAt: "2026-01-01T10:01:00.000Z", Priority: queue.PriorityManual, LineSeq: 7},
				{ID: 52, Status: queue.StatusQueued, AddedAt: "2026-01-01T09:00:00.000Z", Priority: queue.PriorityAutomatic, LineSeq: 6},
				{ID: 53, Status: queue.StatusQueued, AddedAt: "2026-01-01T08:00:00.000Z", Priority: queue.PriorityAutomatic, LineSeq: 1}, // resumed: front of its group
				{ID: 54, Status: queue.StatusDownloading, AddedAt: "2026-01-01T11:00:00.000Z"},
				{ID: 55, Status: queue.StatusFailed, AddedAt: "2026-01-01T07:00:00.000Z", CompletedAt: "2026-01-01T07:30:00Z"},
			},
			"54 51 53 50 52 55 ",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := append([]queue.Item(nil), tc.items...)
			sortQueueItems(got)
			if ids(got) != tc.want {
				t.Fatalf("got %s, want %s", ids(got), tc.want)
			}
			// The same list in another order gives the same result, and
			// sorting twice changes nothing: rows do not jump about.
			reversed := make([]queue.Item, len(tc.items))
			for i, it := range tc.items {
				reversed[len(tc.items)-1-i] = it
			}
			sortQueueItems(reversed)
			if ids(reversed) != tc.want {
				t.Fatalf("a different starting order gave %s", ids(reversed))
			}
		})
	}
}

func TestProgressDoesNotChangeTheOrder(t *testing.T) {
	a := queue.Item{ID: 1, Status: queue.StatusDownloading, AddedAt: "2026-01-01T10:00:00.000Z", ProgressPct: 5}
	b := queue.Item{ID: 2, Status: queue.StatusDownloading, AddedAt: "2026-01-01T10:01:00.000Z", ProgressPct: 95}
	items := []queue.Item{b, a}
	sortQueueItems(items)
	first := items[0].ID
	a.ProgressPct, b.ProgressPct = 99, 10
	items = []queue.Item{a, b}
	sortQueueItems(items)
	if items[0].ID != first || first != 1 {
		t.Fatalf("progress must not reorder rows, got %d first", items[0].ID)
	}
}

func TestRestoredMovieStatus(t *testing.T) {
	tests := []struct {
		name string
		m    library.Movie
		want library.Status
	}{
		{"downloaded stays downloaded", library.Movie{Status: library.StatusDownloaded}, library.StatusDownloaded},
		{"stuck while upgrading, the file is there", library.Movie{Status: library.StatusDownloading, FilePath: "/proc/self/exe"}, library.StatusDownloaded},
		{"stuck, the recorded file is gone", library.Movie{Status: library.StatusDownloading, FilePath: "/no/such/file.mkv"}, library.StatusMissing},
		{"first download", library.Movie{Status: library.StatusDownloading}, library.StatusMissing},
	}
	for _, tc := range tests {
		if got := restoredMovieStatus(tc.m); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestCoversEpisode(t *testing.T) {
	ep := library.Episode{Season: 2, Episode: 3}
	tests := []struct {
		season   int
		episodes []int
		want     bool
	}{
		{2, nil, true}, // a season pack
		{2, []int{3}, true},
		{2, []int{1, 2, 3}, true},
		{2, []int{4}, false},
		{1, nil, false},
	}
	for _, tc := range tests {
		if got := coversEpisode(tc.season, tc.episodes, ep); got != tc.want {
			t.Errorf("season %d episodes %v: got %v, want %v", tc.season, tc.episodes, got, tc.want)
		}
	}
}
