package api

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/ryanborg/mediarium/internal/indexers"
	"github.com/ryanborg/mediarium/internal/settings"
	"github.com/ryanborg/mediarium/internal/torrentclient"
)

// errTorrentsDisabled is what a torrent grab is refused with while the
// torrent switch (Settings > Downloads) is off. It is written for the person
// reading it, so it is passed to the UI as is.
var errTorrentsDisabled = errors.New("torrents are turned off - turn them on in Settings > Downloads to grab torrent releases")

// torrentsEnabled reports the torrent on/off switch. On unless explicitly
// turned off, so upgrading never changes anyone's behavior underneath them.
func (s *Server) torrentsEnabled() bool {
	v, _ := s.Settings.Get(settings.KeyTorrentEnabled)
	return v != "0"
}

// setTorrentsEnabled stores the switch and, when turning torrents off, shuts
// every running torrent client down (downloads in flight fail with
// errTorrentsDisabled; seeding stops).
func (s *Server) setTorrentsEnabled(enabled bool) error {
	value := "1"
	if !enabled {
		value = "0"
	}
	if err := s.Settings.Set(settings.KeyTorrentEnabled, value, false); err != nil {
		return err
	}
	if !enabled {
		s.torrents.stopAll()
	}
	return nil
}

// searchableIndexers is the indexer list searches and automation use: with
// torrents off, torrent indexers are skipped so they are never queried.
func (s *Server) searchableIndexers() ([]indexers.Instance, error) {
	all, err := s.IndexerRepo.List()
	if err != nil || s.torrentsEnabled() {
		return all, err
	}
	out := make([]indexers.Instance, 0, len(all))
	for _, inst := range all {
		if inst.Protocol != indexers.ProtocolTorrent {
			out = append(out, inst)
		}
	}
	return out, nil
}

// writeGrabError maps a grab failure onto a status: a refusal because torrents
// are off is a conflict with the current settings, anything else keeps
// fallback.
func writeGrabError(w http.ResponseWriter, err error, fallback int) {
	if errors.Is(err, errTorrentsDisabled) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeError(w, fallback, err.Error())
}

// torrentRegistry tracks the live per-grab torrent clients so the on/off
// switch can stop them. The zero value is ready to use.
type torrentRegistry struct {
	mu     sync.Mutex
	active map[*torrentclient.Client]*torrentEntry
}

type torrentEntry struct {
	cancel context.CancelFunc
	once   sync.Once
}

// add registers tc; cancel aborts the download that is using it.
func (r *torrentRegistry) add(tc *torrentclient.Client, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil {
		r.active = make(map[*torrentclient.Client]*torrentEntry)
	}
	r.active[tc] = &torrentEntry{cancel: cancel}
}

// closeClient unregisters and closes tc, at most once however many callers
// race to it (the download path, the seeding goroutine, the switch).
func (r *torrentRegistry) closeClient(tc *torrentclient.Client) {
	r.mu.Lock()
	e := r.active[tc]
	delete(r.active, tc)
	r.mu.Unlock()
	if e == nil {
		return
	}
	e.once.Do(func() {
		e.cancel()
		tc.Close()
	})
}

// stopAll cancels and closes every registered client, returning how many.
func (r *torrentRegistry) stopAll() int {
	r.mu.Lock()
	clients := make([]*torrentclient.Client, 0, len(r.active))
	for tc := range r.active {
		clients = append(clients, tc)
	}
	r.mu.Unlock()
	for _, tc := range clients {
		r.closeClient(tc)
	}
	return len(clients)
}

// count is how many torrent clients are live.
func (r *torrentRegistry) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.active)
}
