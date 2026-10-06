package queue

import (
	"fmt"
	"log/slog"
	"sync"
)

// Dispatcher runs the download line: it starts the next waiting item whenever
// fewer than Limit are running, and again each time one ends. A download keeps
// its place for its whole pipeline (download, repair, unpack, import), so a
// limit of one really means one at a time.
//
// It knows nothing about how a download is run. Start begins one and returns
// straight away; the function it is handed (done) must be called exactly once
// when that download has completely ended, whatever the outcome.
type Dispatcher struct {
	repo *Repo
	cfg  DispatchConfig

	mu     sync.Mutex
	active map[int64]struct{} // started and not yet ended: these hold a place
	holds  int                // while above zero nothing new starts (see Hold)
}

// DispatchConfig is what a Dispatcher needs from the app around it.
type DispatchConfig struct {
	// Limit is how many downloads may run at once. It is read on every
	// decision, so a change applies at once: running downloads finish, and
	// nothing new starts until fewer than the new limit are running.
	Limit func() int
	// ManualOnly, when it returns true, holds back everything the automatic
	// searches added (safe mode). Downloads a person asked for still start.
	ManualOnly func() bool
	// Start begins the pipeline of item. An error means it could not begin.
	Start func(item Item, done func()) error
	// OnStartError is told about an item Start refused; the item is not
	// waiting any more, so it should be put right (marked failed).
	OnStartError func(item Item, err error)
	// Hold, when it returns true, starts nothing new for now (the disk is
	// nearly full, say). Running downloads carry on, and the line is looked
	// at again on the next Kick.
	Hold func() bool
}

// NewDispatcher returns a dispatcher over repo. Nothing runs until Kick.
func NewDispatcher(repo *Repo, cfg DispatchConfig) *Dispatcher {
	if cfg.Limit == nil {
		cfg.Limit = func() int { return 1 }
	}
	return &Dispatcher{repo: repo, cfg: cfg, active: map[int64]struct{}{}}
}

// Limit is the number of downloads allowed to run at once: at least one.
func (d *Dispatcher) Limit() int {
	if n := d.cfg.Limit(); n > 0 {
		return n
	}
	return 1
}

// Running is how many downloads hold a place right now.
func (d *Dispatcher) Running() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.active)
}

// Holding reports whether id is one of the downloads holding a place.
func (d *Dispatcher) Holding(id int64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.active[id]
	return ok
}

// Hold stops new downloads from starting until the returned function is
// called, which also starts whatever can now start. Pausing everything uses
// it so the line does not move on while the running downloads wind down.
func (d *Dispatcher) Hold() (release func()) {
	d.mu.Lock()
	d.holds++
	d.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			d.mu.Lock()
			d.holds--
			d.mu.Unlock()
			d.Kick()
		})
	}
}

// Kick starts waiting downloads while there are free places. Call it whenever
// something may have changed: a download was added, resumed or ended, or the
// limit was raised. It never blocks on a download.
func (d *Dispatcher) Kick() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.holds > 0 {
		return
	}
	if d.cfg.Hold != nil && len(d.active) < d.Limit() && d.cfg.Hold() {
		return
	}
	skipped := map[int64]bool{}
	for len(d.active) < d.Limit() {
		item, ok, err := d.repo.NextWaiting(d.manualOnly())
		if err != nil {
			slog.Warn("queue: look for the next download", "err", err)
			return
		}
		if !ok || skipped[item.ID] {
			return
		}
		claimed, err := d.repo.Claim(item.ID)
		if err != nil {
			slog.Warn("queue: start a download", "queueId", item.ID, "err", err)
			skipped[item.ID] = true
			continue
		}
		if !claimed {
			continue // paused, stopped or removed a moment ago: look again
		}
		id := item.ID
		d.active[id] = struct{}{}
		var once sync.Once
		done := func() {
			once.Do(func() {
				d.mu.Lock()
				delete(d.active, id)
				d.mu.Unlock()
				d.Kick()
			})
		}
		if err := d.start(item, done); err != nil {
			delete(d.active, id)
			d.failed(item, err)
		}
	}
}

// start calls Start and turns a panic in it into an error, so a place is not
// held for ever by a download that never began.
func (d *Dispatcher) start(item Item, done func()) (err error) {
	defer func() {
		if p := recover(); p != nil {
			slog.Error("queue: starting a download crashed", "queueId", item.ID, "panic", fmt.Sprint(p))
			err = fmt.Errorf("starting the download crashed: %v", p)
		}
	}()
	return d.cfg.Start(item, done)
}

func (d *Dispatcher) manualOnly() bool {
	return d.cfg.ManualOnly != nil && d.cfg.ManualOnly()
}

func (d *Dispatcher) failed(item Item, err error) {
	if d.cfg.OnStartError != nil {
		d.cfg.OnStartError(item, err)
		return
	}
	if e := d.repo.SetStatus(item.ID, StatusFailed, err.Error()); e != nil {
		slog.Warn("queue: mark a download that could not start", "queueId", item.ID, "err", e)
	}
}

// FreePlace reports whether a download added now would start straight away.
func (d *Dispatcher) FreePlace() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	// Outside the download hours (or with the disk too full) nothing starts.
	return d.holds == 0 && len(d.active) < d.Limit() && (d.cfg.Hold == nil || !d.cfg.Hold())
}
