package queue_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/queue"
)

// The sizes in an NZB are the encoded article sizes, so a transfer ends a
// little short of 100%. Once the transfer is over (the files are being
// processed) and when the download completes, it reads 100%.
func TestProgressReadsFullOnceTheTransferIsOver(t *testing.T) {
	tests := []struct {
		status queue.Status
		want   float64
	}{
		{queue.StatusDownloading, 96.7},
		{queue.StatusPaused, 96.7},
		{queue.StatusFailed, 96.7},
		{queue.StatusStopped, 96.7},
		{queue.StatusImporting, 100},
		{queue.StatusCompleted, 100},
	}
	for _, tc := range tests {
		t.Run(string(tc.status), func(t *testing.T) {
			repo := queue.NewRepo(openDB(t))
			id, err := repo.Enqueue(queue.Item{ReleaseTitle: "Release"})
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.SetProgress(id, 96.7); err != nil {
				t.Fatal(err)
			}
			if err := repo.SetStatus(id, tc.status, ""); err != nil {
				t.Fatal(err)
			}
			got, err := repo.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if got.ProgressPct != tc.want {
				t.Fatalf("progress = %v, want %v", got.ProgressPct, tc.want)
			}
		})
	}
}
