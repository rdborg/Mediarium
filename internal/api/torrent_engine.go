package api

import (
	"context"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/anacrolix/torrent"

	"github.com/ryanborg/mediarium/internal/torrentclient"
)

// seedCheckInterval is how often a seeding torrent is checked against its
// seeding goal.
const seedCheckInterval = 30 * time.Second

// torrentRegistry runs the torrent engines and the torrents in them. Every
// torrent shares one engine per listen port (and one per VPN tunnel), so the
// app listens on a single port however many torrents are active; an engine
// is started with its first torrent and closed after its last. It also
// remembers which torrents have reached their seeding goal. The zero value
// is ready to use.
type torrentRegistry struct {
	mu           sync.Mutex
	engines      map[engineKey]*torrentEngine
	jobs         map[int64]*torrentJob // by queue item
	goalMet      map[int64]bool        // queue items whose seeding goal was reached
	lastIncoming time.Time             // latest incoming peer connection of any engine, kept after it closes
}

type engineKey struct {
	port   int
	tunnel torrentclient.TunnelDialer
}

type torrentEngine struct {
	key  engineKey
	tc   *torrentclient.Client
	jobs int
}

// torrentJob is one queue item's torrent, from adding it until it stops
// (download failed, seeding goal reached, or torrents switched off).
type torrentJob struct {
	queueID int64
	dir     string
	cancel  context.CancelFunc
	engine  *torrentEngine
	t       *torrent.Torrent
	stopped bool
}

// start registers a torrent download for queueID saved in dir, on the
// engine for port and tunnel (started if needed; dataRoot is its default
// folder). cancel aborts the download and seeding. If port is taken by
// another program, the engine falls back to a port the system picks, and
// says so in the log.
func (r *torrentRegistry) start(queueID int64, dir, dataRoot string, port int, tunnel torrentclient.TunnelDialer, cancel context.CancelFunc) (*torrentJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.engines == nil {
		r.engines = map[engineKey]*torrentEngine{}
		r.jobs = map[int64]*torrentJob{}
		r.goalMet = map[int64]bool{}
	}
	key := engineKey{port: port, tunnel: tunnel}
	e := r.engines[key]
	if e == nil {
		tc, err := torrentclient.New(torrentclient.Config{DataDir: dataRoot, ListenPort: port, Tunnel: tunnel})
		if err != nil && port != 0 && tunnel == nil {
			log.Printf("torrent: listen_port=%d unavailable, using a port the system picks: %v", port, err)
			tc, err = torrentclient.New(torrentclient.Config{DataDir: dataRoot, Tunnel: tunnel})
		}
		if err != nil {
			return nil, err
		}
		e = &torrentEngine{key: key, tc: tc}
		r.engines[key] = e
	}
	e.jobs++
	job := &torrentJob{queueID: queueID, dir: filepath.Clean(dir), cancel: cancel, engine: e}
	if old := r.jobs[queueID]; old != nil {
		r.stopLocked(old)
	}
	r.jobs[queueID] = job
	delete(r.goalMet, queueID)
	return job, nil
}

// attach records the torrent a job is running. It reports false (and
// removes t) when the job was stopped in the meantime.
func (r *torrentRegistry) attach(job *torrentJob, t *torrent.Torrent) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if job.stopped {
		if t != nil {
			job.engine.tc.Remove(t)
		}
		return false
	}
	job.t = t
	return true
}

// stop ends a job: aborts its download or seeding, removes its torrent from
// the engine (the files stay on disk) and closes the engine if it was the
// last one. Safe to call more than once.
func (r *torrentRegistry) stop(job *torrentJob) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked(job)
}

func (r *torrentRegistry) stopLocked(job *torrentJob) {
	if job.stopped {
		return
	}
	job.stopped = true
	job.cancel()
	if job.t != nil {
		job.engine.tc.Remove(job.t)
	}
	if r.jobs[job.queueID] == job {
		delete(r.jobs, job.queueID)
	}
	e := job.engine
	e.jobs--
	if e.jobs == 0 {
		if at := e.tc.LastIncoming(); at.After(r.lastIncoming) {
			r.lastIncoming = at
		}
		e.tc.Close()
		if r.engines[e.key] == e {
			delete(r.engines, e.key)
		}
	}
}

// stopAll stops every torrent (downloads in flight fail, seeding stops) and
// returns how many there were.
func (r *torrentRegistry) stopAll() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(r.jobs)
	for _, job := range r.jobs {
		r.stopLocked(job)
	}
	return n
}

// stopQueue stops queueID's torrent, if one is running, and reports
// whether there was one.
func (r *torrentRegistry) stopQueue(queueID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	job := r.jobs[queueID]
	if job == nil {
		return false
	}
	r.stopLocked(job)
	return true
}

// markGoalMet records that queueID's torrent reached its seeding goal.
func (r *torrentRegistry) markGoalMet(queueID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.goalMet == nil {
		r.goalMet = map[int64]bool{}
	}
	r.goalMet[queueID] = true
}

// seedingDone reports whether queueID's torrent reached its seeding goal.
func (r *torrentRegistry) seedingDone(queueID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.goalMet[queueID]
}

// activeDir reports whether a running torrent (downloading or seeding) is
// saved in dir.
func (r *torrentRegistry) activeDir(dir string) bool {
	dir = filepath.Clean(dir)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, job := range r.jobs {
		if job.dir == dir {
			return true
		}
	}
	return false
}

// active reports whether queueID has a running torrent.
func (r *torrentRegistry) active(queueID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.jobs[queueID] != nil
}

// torrentEngineStatus is what the Downloaders page shows about incoming
// connections.
type torrentEngineStatus struct {
	Listening    bool      // an engine is listening on a host port now
	Port         int       // the port it listens on (0 when none)
	Torrents     int       // torrents running
	LastIncoming time.Time // latest incoming peer connection (zero if none seen since start)
}

func (r *torrentRegistry) status() torrentEngineStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := torrentEngineStatus{Torrents: len(r.jobs), LastIncoming: r.lastIncoming}
	for _, e := range r.engines {
		if p := e.tc.ListenPort(); p != 0 {
			st.Listening, st.Port = true, p
		}
		if at := e.tc.LastIncoming(); at.After(st.LastIncoming) {
			st.LastIncoming = at
		}
	}
	return st
}
