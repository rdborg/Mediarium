package queue_test

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/queue"
)

// lineHarness runs a Dispatcher over a real database with a fake pipeline: a
// started download sits there until the test finishes it, so the test decides
// exactly when a place is freed.
type lineHarness struct {
	t      *testing.T
	repo   *queue.Repo
	d      *queue.Dispatcher
	limit  atomic.Int64
	manual atomic.Bool

	mu        sync.Mutex
	started   []int64
	dones     map[int64]func()
	refuse    map[int64]bool // Start fails for these
	refused   []int64
	running   int
	maxAtOnce int
}

func newLineHarness(t *testing.T, limit int) *lineHarness {
	t.Helper()
	h := &lineHarness{t: t, repo: queue.NewRepo(openDB(t)), dones: map[int64]func(){}, refuse: map[int64]bool{}}
	h.limit.Store(int64(limit))
	h.d = queue.NewDispatcher(h.repo, queue.DispatchConfig{
		Limit:      func() int { return int(h.limit.Load()) },
		ManualOnly: h.manual.Load,
		Start: func(it queue.Item, done func()) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.refuse[it.ID] {
				return errors.New("cannot start")
			}
			h.started = append(h.started, it.ID)
			h.dones[it.ID] = done
			h.running++
			h.maxAtOnce = max(h.maxAtOnce, h.running)
			return nil
		},
		OnStartError: func(it queue.Item, err error) {
			h.mu.Lock()
			h.refused = append(h.refused, it.ID)
			h.mu.Unlock()
			_ = h.repo.SetStatus(it.ID, queue.StatusFailed, err.Error())
		},
	})
	return h
}

func (h *lineHarness) add(p queue.Priority) int64 {
	h.t.Helper()
	id, err := h.repo.Enqueue(queue.Item{ReleaseTitle: fmt.Sprintf("Release %d", time.Now().UnixNano()), Priority: p})
	if err != nil {
		h.t.Fatal(err)
	}
	return id
}

// finish ends a running download the way its pipeline would, with the status
// it would leave, and frees its place.
func (h *lineHarness) finish(id int64, status queue.Status) {
	h.t.Helper()
	h.mu.Lock()
	done := h.dones[id]
	delete(h.dones, id)
	h.running--
	h.mu.Unlock()
	if done == nil {
		h.t.Fatalf("download %d is not running", id)
	}
	if err := h.repo.SetStatus(id, status, ""); err != nil {
		h.t.Fatal(err)
	}
	done()
}

func (h *lineHarness) startedIDs() []int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]int64(nil), h.started...)
}

func (h *lineHarness) runningIDs() []int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []int64
	for _, id := range h.started {
		if h.dones[id] != nil {
			out = append(out, id)
		}
	}
	return out
}

// drain finishes downloads in the order they started until none are left, and
// returns the order they started in.
func (h *lineHarness) drain() []int64 {
	h.t.Helper()
	for {
		running := h.runningIDs()
		if len(running) == 0 {
			return h.startedIDs()
		}
		h.finish(running[0], queue.StatusCompleted)
	}
}

func TestLineStartsInOrder(t *testing.T) {
	const (
		A = queue.PriorityAutomatic
		M = queue.PriorityManual
	)
	tests := []struct {
		name  string
		limit int
		add   []queue.Priority
		want  []int // positions in the add list, in the order they should start
	}{
		{"first come first served", 1, []queue.Priority{A, A, A, A}, []int{0, 1, 2, 3}},
		{"a person's download goes before automatic ones waiting", 1, []queue.Priority{A, A, A, M}, []int{0, 3, 1, 2}},
		{"the first one is already running, so nothing jumps it", 1, []queue.Priority{A, M}, []int{0, 1}},
		{"manual downloads keep their own order", 1, []queue.Priority{A, M, M, A, M}, []int{0, 1, 2, 4, 3}},
		{"limit three starts three at once, in order", 3, []queue.Priority{A, A, A, A, A}, []int{0, 1, 2, 3, 4}},
		{"limit three with a person's download waiting", 3, []queue.Priority{A, A, A, A, M, A}, []int{0, 1, 2, 4, 3, 5}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newLineHarness(t, tc.limit)
			ids := make([]int64, len(tc.add))
			for i, p := range tc.add {
				ids[i] = h.add(p)
				h.d.Kick() // each one is added and the line looked at, like a real grab
			}
			var want []int64
			for _, i := range tc.want {
				want = append(want, ids[i])
			}
			if got := h.drain(); !reflect.DeepEqual(got, want) {
				t.Fatalf("started in order %v, want %v", got, want)
			}
		})
	}
}

func TestLineNeverRunsMoreThanTheLimit(t *testing.T) {
	for _, limit := range []int{1, 3} {
		t.Run(fmt.Sprintf("limit %d", limit), func(t *testing.T) {
			h := newLineHarness(t, limit)
			for range 7 {
				h.add(queue.PriorityAutomatic)
			}
			h.d.Kick()
			if got := len(h.runningIDs()); got != limit {
				t.Fatalf("%d running after the first look, want %d", got, limit)
			}
			if h.d.Running() != limit {
				t.Fatalf("dispatcher counts %d running, want %d", h.d.Running(), limit)
			}
			// Kicking again changes nothing while the places are taken.
			h.d.Kick()
			h.d.Kick()
			if got := len(h.runningIDs()); got != limit {
				t.Fatalf("%d running after more looks, want %d", got, limit)
			}
			if got := len(h.drain()); got != 7 {
				t.Fatalf("%d downloads started, want 7", got)
			}
			if h.maxAtOnce > limit {
				t.Fatalf("%d ran at the same time, limit is %d", h.maxAtOnce, limit)
			}
			if n, _ := h.repo.CountWaiting(); n != 0 {
				t.Fatalf("%d still waiting after everything ran", n)
			}
		})
	}
}

func TestLineMovesOnWhenADownloadEndsAnyWay(t *testing.T) {
	tests := []struct {
		name   string
		status queue.Status
	}{
		{"finished", queue.StatusCompleted},
		{"failed", queue.StatusFailed},
		{"paused by a person", queue.StatusPaused},
		{"stopped by a person", queue.StatusStopped},
		{"waiting on a decision about an existing file", queue.StatusConflict},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newLineHarness(t, 1)
			first, second, third := h.add(queue.PriorityAutomatic), h.add(queue.PriorityAutomatic), h.add(queue.PriorityAutomatic)
			h.d.Kick()
			if !reflect.DeepEqual(h.startedIDs(), []int64{first}) {
				t.Fatalf("only the first should have started, got %v", h.startedIDs())
			}
			h.finish(first, tc.status)
			if !reflect.DeepEqual(h.startedIDs(), []int64{first, second}) {
				t.Fatalf("after the first %s the second should start, got %v", tc.name, h.startedIDs())
			}
			if got, _ := h.repo.Get(third); got.Status != queue.StatusQueued {
				t.Fatalf("the third is still waiting, got %s", got.Status)
			}
			// A failed download is left as it is: it is not tried again.
			if got, _ := h.repo.Get(first); got.Status != tc.status {
				t.Fatalf("the first should stay %s, got %s", tc.status, got.Status)
			}
		})
	}
}

func TestLineSkipsWhatCannotStart(t *testing.T) {
	h := newLineHarness(t, 1)
	bad, good := h.add(queue.PriorityAutomatic), h.add(queue.PriorityAutomatic)
	h.refuse[bad] = true
	h.d.Kick()
	if !reflect.DeepEqual(h.startedIDs(), []int64{good}) {
		t.Fatalf("the one that cannot start should be skipped, started %v", h.startedIDs())
	}
	if !reflect.DeepEqual(h.refused, []int64{bad}) {
		t.Fatalf("refused = %v, want [%d]", h.refused, bad)
	}
	if got, _ := h.repo.Get(bad); got.Status != queue.StatusFailed {
		t.Fatalf("the refused download should be failed, got %s", got.Status)
	}
	if h.d.Running() != 1 {
		t.Fatalf("a refused download must not hold a place, %d running", h.d.Running())
	}
}

func TestLinePausedAndStoppedWaitersAreSkipped(t *testing.T) {
	h := newLineHarness(t, 1)
	first, paused, stopped, last := h.add(queue.PriorityAutomatic), h.add(queue.PriorityAutomatic), h.add(queue.PriorityAutomatic), h.add(queue.PriorityAutomatic)
	h.d.Kick()
	// A person pauses and stops two of the ones waiting.
	if err := h.repo.Pause(paused, false); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.SetStatus(stopped, queue.StatusStopped, ""); err != nil {
		t.Fatal(err)
	}
	h.finish(first, queue.StatusCompleted)
	if !reflect.DeepEqual(h.startedIDs(), []int64{first, last}) {
		t.Fatalf("paused and stopped downloads must be skipped, started %v", h.startedIDs())
	}
}

func TestLineLimitChangesWhileRunning(t *testing.T) {
	h := newLineHarness(t, 3)
	for range 6 {
		h.add(queue.PriorityAutomatic)
	}
	h.d.Kick()
	running := h.runningIDs()
	if len(running) != 3 {
		t.Fatalf("%d running, want 3", len(running))
	}

	// Lower the limit to one: the three running ones carry on, nothing new
	// starts until fewer than one are left.
	h.limit.Store(1)
	h.d.Kick()
	if got := len(h.runningIDs()); got != 3 {
		t.Fatalf("lowering the limit must not stop running downloads, %d running", got)
	}
	h.finish(running[0], queue.StatusCompleted)
	h.finish(running[1], queue.StatusCompleted)
	if got := len(h.startedIDs()); got != 3 {
		t.Fatalf("nothing new should start while one is still running, %d started", got)
	}
	h.finish(running[2], queue.StatusCompleted)
	if got := len(h.runningIDs()); got != 1 {
		t.Fatalf("one should start once the running ones are done, %d running", got)
	}

	// Raise it again: the line fills up to the new limit at once.
	h.limit.Store(4)
	h.d.Kick()
	if got := len(h.runningIDs()); got != 3 {
		t.Fatalf("raising the limit should fill the free places (only 3 left to run), %d running", got)
	}
}

func TestLineAfterARestart(t *testing.T) {
	db := openDB(t)
	repo := queue.NewRepo(db)
	add := func(status queue.Status) int64 {
		id, err := repo.Enqueue(queue.Item{ReleaseTitle: "r-" + string(status)})
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
	downloading, importing := add(queue.StatusDownloading), add(queue.StatusImporting)
	waiting1, waiting2, waiting3 := add(queue.StatusQueued), add(queue.StatusQueued), add(queue.StatusQueued)
	paused := add(queue.StatusPaused)

	// The app starts again: what was running is paused, what was waiting still waits.
	n, err := repo.RecoverInterrupted()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("RecoverInterrupted paused %d, want 2", n)
	}
	for _, id := range []int64{downloading, importing} {
		got, _ := repo.Get(id)
		if got.Status != queue.StatusPaused || !got.Interrupted {
			t.Fatalf("a download that was running should come back paused and interrupted: %+v", got)
		}
	}
	for _, id := range []int64{waiting1, waiting2, waiting3} {
		if got, _ := repo.Get(id); got.Status != queue.StatusQueued {
			t.Fatalf("a download that was waiting must still be waiting, got %s", got.Status)
		}
	}

	var started []int64
	var mu sync.Mutex
	d := queue.NewDispatcher(repo, queue.DispatchConfig{
		Limit: func() int { return 2 },
		Start: func(it queue.Item, done func()) error {
			mu.Lock()
			started = append(started, it.ID)
			mu.Unlock()
			return nil
		},
	})
	d.Kick()
	if !reflect.DeepEqual(started, []int64{waiting1, waiting2}) {
		t.Fatalf("after a restart the waiting ones carry on in order up to the limit, got %v", started)
	}
	for _, id := range []int64{downloading, importing, paused} {
		if got, _ := repo.Get(id); got.Status != queue.StatusPaused {
			t.Fatalf("paused downloads stay paused until a person resumes them, got %s", got.Status)
		}
	}

	// Safe mode holds what was waiting as well.
	if n, err := repo.PauseWaiting(); err != nil || n != 1 {
		t.Fatalf("PauseWaiting = %d, %v; want 1 (the third one is the only one still waiting)", n, err)
	}
}

func TestLineResumeGoesToTheFrontOfItsPriority(t *testing.T) {
	h := newLineHarness(t, 1)
	running := h.add(queue.PriorityAutomatic)
	h.d.Kick()
	a1, a2 := h.add(queue.PriorityAutomatic), h.add(queue.PriorityAutomatic)
	m1 := h.add(queue.PriorityManual)
	old := h.add(queue.PriorityAutomatic)
	if err := h.repo.Pause(old, false); err != nil {
		t.Fatal(err)
	}
	if ok, err := h.repo.Requeue(old); err != nil || !ok {
		t.Fatalf("Requeue = %v, %v", ok, err)
	}
	if ok, _ := h.repo.Requeue(a1); ok {
		t.Fatal("only a paused download can be put back in line")
	}
	// The resumed automatic one goes ahead of the other automatic ones, but not
	// ahead of what a person asked for.
	want := []int64{running, m1, old, a1, a2}
	if got := h.drain(); !reflect.DeepEqual(got, want) {
		t.Fatalf("started in order %v, want %v", got, want)
	}
}

func TestLineResumeAllKeepsTheirOrder(t *testing.T) {
	h := newLineHarness(t, 1)
	ids := []int64{h.add(queue.PriorityAutomatic), h.add(queue.PriorityAutomatic), h.add(queue.PriorityAutomatic)}
	for _, id := range ids {
		if err := h.repo.Pause(id, false); err != nil {
			t.Fatal(err)
		}
	}
	// Resume all: newest first, each to the front, while nothing may start.
	release := h.d.Hold()
	for i := len(ids) - 1; i >= 0; i-- {
		if ok, err := h.repo.Requeue(ids[i]); err != nil || !ok {
			t.Fatalf("Requeue: %v %v", ok, err)
		}
		h.d.Kick()
	}
	if len(h.startedIDs()) != 0 {
		t.Fatalf("nothing may start while the line is held, started %v", h.startedIDs())
	}
	release()
	if got := h.drain(); !reflect.DeepEqual(got, ids) {
		t.Fatalf("resumed downloads should start oldest first, got %v want %v", got, ids)
	}
}

func TestLineSafeModeOnlyStartsWhatAPersonAskedFor(t *testing.T) {
	h := newLineHarness(t, 3)
	h.manual.Store(true)
	auto := h.add(queue.PriorityAutomatic)
	manual := h.add(queue.PriorityManual)
	h.d.Kick()
	if got := h.startedIDs(); !reflect.DeepEqual(got, []int64{manual}) {
		t.Fatalf("in safe mode only the manual one starts, got %v", got)
	}
	if got, _ := h.repo.Get(auto); got.Status != queue.StatusQueued {
		t.Fatalf("the automatic one keeps waiting, got %s", got.Status)
	}
	h.manual.Store(false)
	h.d.Kick()
	if len(h.startedIDs()) != 2 {
		t.Fatalf("with safe mode off the automatic one starts too, started %v", h.startedIDs())
	}
}

// A torrent that is only seeding after it was imported has ended its
// pipeline: the seeding carries on by itself and holds no place.
func TestLineSeedingTorrentDoesNotHoldAPlace(t *testing.T) {
	repo := queue.NewRepo(openDB(t))
	first, _ := repo.Enqueue(queue.Item{ReleaseTitle: "Seeded", Protocol: queue.ProtocolTorrent})
	second, _ := repo.Enqueue(queue.Item{ReleaseTitle: "Next"})

	stopSeeding := make(chan struct{})
	var seeding sync.WaitGroup
	var mu sync.Mutex
	var started []int64
	var d *queue.Dispatcher
	d = queue.NewDispatcher(repo, queue.DispatchConfig{
		Limit: func() int { return 1 },
		Start: func(it queue.Item, done func()) error {
			mu.Lock()
			started = append(started, it.ID)
			mu.Unlock()
			if it.ID == first {
				// download and import take a moment, then the pipeline ends
				// and a goroutine keeps seeding.
				seeding.Add(1)
				go func() {
					defer seeding.Done()
					<-stopSeeding
				}()
				go func() {
					_ = repo.SetStatus(it.ID, queue.StatusCompleted, "")
					done()
				}()
			}
			return nil
		},
	})
	d.Kick()
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(started)
		mu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the next download never started while the first was only seeding")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !reflect.DeepEqual(started, []int64{first, second}) {
		t.Fatalf("started %v", started)
	}
	close(stopSeeding)
	seeding.Wait()
}

func TestLineHoldStopsNewDownloadsUntilReleased(t *testing.T) {
	h := newLineHarness(t, 2)
	h.add(queue.PriorityAutomatic)
	release := h.d.Hold()
	h.d.Kick()
	if h.d.FreePlace() {
		t.Fatal("a held line has no free place")
	}
	if len(h.startedIDs()) != 0 {
		t.Fatalf("started %v while held", h.startedIDs())
	}
	release()
	release() // releasing twice is harmless
	if len(h.startedIDs()) != 1 {
		t.Fatalf("releasing the hold starts what was waiting, started %v", h.startedIDs())
	}
}

// Many things can look at the line at the same moment (a grab, a finished
// download, the scheduled check). It must still never run more than the limit
// and start every download exactly once.
func TestLineConcurrentKicksRespectTheLimit(t *testing.T) {
	const total, limit = 60, 3
	repo := queue.NewRepo(openDB(t))
	var running, maxRunning, finished atomic.Int64
	seen := sync.Map{}
	var wg sync.WaitGroup
	d := queue.NewDispatcher(repo, queue.DispatchConfig{
		Limit: func() int { return limit },
		Start: func(it queue.Item, done func()) error {
			if _, dup := seen.LoadOrStore(it.ID, true); dup {
				t.Errorf("download %d started twice", it.ID)
			}
			n := running.Add(1)
			for {
				m := maxRunning.Load()
				if n <= m || maxRunning.CompareAndSwap(m, n) {
					break
				}
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(time.Millisecond)
				running.Add(-1)
				_ = repo.SetStatus(it.ID, queue.StatusCompleted, "")
				finished.Add(1)
				done()
			}()
			return nil
		},
	})
	var kickers sync.WaitGroup
	for range 6 {
		kickers.Add(1)
		go func() {
			defer kickers.Done()
			for range total / 6 {
				if _, err := repo.Enqueue(queue.Item{ReleaseTitle: "r"}); err != nil {
					t.Error(err)
				}
				d.Kick()
			}
		}()
	}
	kickers.Wait()
	deadline := time.Now().Add(20 * time.Second)
	for finished.Load() < total {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d downloads finished", finished.Load(), total)
		}
		d.Kick()
		time.Sleep(2 * time.Millisecond)
	}
	wg.Wait()
	if maxRunning.Load() > limit {
		t.Fatalf("%d ran at the same time, limit is %d", maxRunning.Load(), limit)
	}
}

func TestPositionsAndCounts(t *testing.T) {
	repo := queue.NewRepo(openDB(t))
	a, _ := repo.Enqueue(queue.Item{ReleaseTitle: "a"})
	m, _ := repo.Enqueue(queue.Item{ReleaseTitle: "m", Priority: queue.PriorityManual})
	if n, _ := repo.CountWaiting(); n != 2 {
		t.Fatalf("CountWaiting = %d, want 2", n)
	}
	if n, _ := repo.CountAutomaticOpen(); n != 1 {
		t.Fatalf("CountAutomaticOpen = %d, want 1 (the manual one is not counted)", n)
	}
	if n, _ := repo.CountAutomaticAddedSince(time.Now().Add(-time.Hour)); n != 1 {
		t.Fatalf("CountAutomaticAddedSince = %d, want 1", n)
	}
	next, ok, err := repo.NextWaiting(false)
	if err != nil || !ok || next.ID != m {
		t.Fatalf("NextWaiting = %+v %v %v, want the manual one (%d)", next, ok, err, m)
	}
	next, ok, _ = repo.NextWaiting(true)
	if !ok || next.ID != m {
		t.Fatalf("NextWaiting(manual only) = %+v, want %d", next, m)
	}
	if err := repo.Pause(m, false); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := repo.NextWaiting(true); ok {
		t.Fatal("with the manual one paused nothing manual is waiting")
	}
	if next, _, _ = repo.NextWaiting(false); next.ID != a {
		t.Fatalf("NextWaiting = %d, want %d", next.ID, a)
	}
	// A claim only works once.
	if ok, _ := repo.Claim(a); !ok {
		t.Fatal("the first claim should work")
	}
	if ok, _ := repo.Claim(a); ok {
		t.Fatal("a second claim of the same download must not work")
	}
}
