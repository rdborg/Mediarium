package queue_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/queue"
)

// A Start that panics must not keep its place: the item is reported as not
// started and the next one in the line gets the place.
func TestAStartThatPanicsDoesNotHoldItsPlace(t *testing.T) {
	repo := queue.NewRepo(openDB(t))
	first, err := repo.Enqueue(queue.Item{ReleaseTitle: "First"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Enqueue(queue.Item{ReleaseTitle: "Second"})
	if err != nil {
		t.Fatal(err)
	}

	var (
		started []int64
		refused []int64
	)
	d := queue.NewDispatcher(repo, queue.DispatchConfig{
		Limit: func() int { return 1 },
		Start: func(it queue.Item, done func()) error {
			if it.ID == first {
				panic("start blew up")
			}
			started = append(started, it.ID)
			return nil
		},
		OnStartError: func(it queue.Item, err error) {
			refused = append(refused, it.ID)
			_ = repo.SetStatus(it.ID, queue.StatusFailed, err.Error())
		},
	})

	d.Kick() // must not panic

	if len(refused) != 1 || refused[0] != first {
		t.Fatalf("refused = %v, want just item %d", refused, first)
	}
	if len(started) != 1 || started[0] != second {
		t.Fatalf("started = %v, want the next item %d to get the place", started, second)
	}
	if d.Running() != 1 || !d.Holding(second) || d.Holding(first) {
		t.Fatalf("running = %d, holding first = %v, holding second = %v", d.Running(), d.Holding(first), d.Holding(second))
	}
}
