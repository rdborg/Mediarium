package mediaservers

import (
	"context"
	"log/slog"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ServerStore is what the Refresher needs from the Repo.
type ServerStore interface {
	List() ([]Server, error)
	RecordCheck(id int64, serverID string, checkErr error) error
}

// Refresher asks media servers to rescan the folders new files were
// imported into. Imports are collected for a short while first, so a season
// pack (or several downloads finishing together) causes one scan per folder
// rather than one per file. It runs in the background and never reports
// back to the import: a media server that is down only shows up in the log
// and on the dashboard.
type Refresher struct {
	store  ServerStore
	client *Client

	// Delay is how long to wait for more imports after the last one; MaxWait
	// caps the total wait from the first one.
	Delay   time.Duration
	MaxWait time.Duration
	// OnDone, when set, runs after each round of refreshes (the API uses it
	// to forget cached "Watch in" lookups, since a new title may now be
	// there).
	OnDone func()

	mu      sync.Mutex
	pending map[MediaKind]map[string]bool
	first   time.Time
	timer   *time.Timer
	running sync.WaitGroup
}

// NewRefresher returns a refresher with a 15-second quiet period.
func NewRefresher(store ServerStore, client *Client) *Refresher {
	return &Refresher{store: store, client: client, Delay: 15 * time.Second, MaxWait: 2 * time.Minute}
}

// Imported records that files were imported; the folders holding them are
// scanned once things have been quiet for Delay.
func (r *Refresher) Imported(kind MediaKind, files ...string) {
	if len(files) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending == nil {
		r.pending = map[MediaKind]map[string]bool{}
	}
	if r.pending[kind] == nil {
		r.pending[kind] = map[string]bool{}
	}
	for _, f := range files {
		if f != "" {
			r.pending[kind][filepath.Dir(f)] = true
		}
	}
	now := time.Now()
	if r.timer == nil {
		r.first = now
		r.running.Add(1)
		r.timer = time.AfterFunc(r.Delay, r.fire)
		return
	}
	// Push the scan back, unless that would wait too long in total. A timer
	// that already fired is left alone: its run has not collected the
	// folders yet (it needs r.mu), so it picks these up too.
	if now.Sub(r.first)+r.Delay <= r.MaxWait && r.timer.Stop() {
		r.timer.Reset(r.Delay)
	}
}

// Flush runs any waiting refresh now and waits for it to finish.
func (r *Refresher) Flush() {
	r.mu.Lock()
	if r.timer != nil && r.timer.Stop() {
		r.mu.Unlock()
		r.fire()
	} else {
		r.mu.Unlock()
	}
	r.running.Wait()
}

func (r *Refresher) take() map[MediaKind][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[MediaKind][]string{}
	for kind, set := range r.pending {
		for f := range set {
			out[kind] = append(out[kind], f)
		}
		sort.Strings(out[kind])
	}
	r.pending, r.timer = nil, nil
	return out
}

func (r *Refresher) fire() {
	defer r.running.Done()
	work := r.take()
	if len(work) == 0 {
		return
	}
	servers, err := r.store.List()
	if err != nil {
		slog.Error("media servers: load servers for library refresh", "err", err)
		return
	}
	for _, s := range servers {
		if !s.Enabled || !s.RefreshAfterImport {
			continue
		}
		r.refreshServer(s, work)
	}
	if r.OnDone != nil {
		r.OnDone()
	}
}

func (r *Refresher) refreshServer(s Server, work map[MediaKind][]string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var firstErr error
	for _, kind := range []MediaKind{MediaMovie, MediaTV} {
		folders := work[kind]
		if len(folders) == 0 {
			continue
		}
		done, err := r.client.RefreshFolders(ctx, s, kind, folders)
		if len(done) > 0 {
			slog.Info("media server library refresh started", "server", s.Name, "type", string(s.Kind), "media", string(kind), "scans", done)
		}
		if err != nil {
			slog.Warn("media server library refresh failed", "server", s.Name, "type", string(s.Kind), "media", string(kind), "err", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if err := r.store.RecordCheck(s.ID, "", firstErr); err != nil {
		slog.Error("media servers: record refresh result", "server", s.Name, "err", err)
	}
}
