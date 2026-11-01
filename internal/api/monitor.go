package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/monitor"
	"github.com/rdborg/mediarium/internal/settings"
)

const (
	defaultMonitorMinutes = 30
	minMonitorMinutes     = 5
)

// monitorMinutes reads monitor.interval_minutes: 30 when unset, 0 for off,
// otherwise at least 5 (a check logs into every provider, so hammering them
// helps nobody).
func (s *Server) monitorMinutes() int {
	v, _ := s.Settings.Get(settings.KeyMonitorIntervalMinutes)
	if v == "" {
		return defaultMonitorMinutes
	}
	n, err := strconv.Atoi(v)
	switch {
	case err != nil || n < 0:
		return defaultMonitorMinutes
	case n == 0:
		return 0
	case n < minMonitorMinutes:
		return minMonitorMinutes
	}
	return n
}

func (s *Server) monitorInterval() time.Duration {
	return time.Duration(s.monitorMinutes()) * time.Minute
}

// newMonitor wires the connectivity monitor to the real Usenet servers and
// indexers. Usenet servers get the same login as the Test button in Settings;
// indexers get the lighter check in probeIndexer.
func (s *Server) newMonitor() *monitor.Monitor {
	return &monitor.Monitor{
		Checks:   s.monitorChecks,
		Interval: s.monitorInterval,
		Notify: func(ev monitor.Event) {
			noteMonitorProblem(ev)
			s.notifyEvent("health", ev.Title, ev.Message)
		},
	}
}

// monitorChecks lists one check per enabled Usenet server and per enabled
// indexer (torrent indexers only while torrents are on, as with searching).
func (s *Server) monitorChecks(ctx context.Context) ([]monitor.Check, error) {
	var checks []monitor.Check

	servers, err := s.ClientRepo.List() // enabled servers only
	if err != nil {
		return nil, err
	}
	for _, sc := range servers {
		cfg := sc.Config
		checks = append(checks, monitor.Check{
			Kind: monitor.KindUsenet, ID: sc.ID, Name: sc.Name,
			Run: func(ctx context.Context) error {
				// Mediarium's own downloads hold the connections: a check on
				// top would be refused for being one too many, and that is not
				// a server that stopped working.
				working, busy := download.LoginUse(cfg)
				if working {
					return nil
				}
				if busy {
					return monitor.ErrSkip
				}
				return runProbe(ctx, probeTimeout, func() connTestResult {
					return testNNTP(cfg.Host, cfg.Port, cfg.UseSSL, cfg.Username, cfg.Password)
				})
			},
		})
	}

	instances, err := s.IndexerRepo.List()
	if err != nil {
		return nil, err
	}
	torrentsOn := s.torrentReleasesOn()
	for _, inst := range instances {
		if !inst.Enabled || inst.SecretError != "" || (inst.Protocol == indexers.ProtocolTorrent && !torrentsOn) {
			continue
		}
		checks = append(checks, monitor.Check{
			Kind: monitor.KindIndexer, ID: inst.ID, Name: inst.Name,
			Run: func(ctx context.Context) error { return s.probeIndexer(ctx, inst) },
		})
	}
	return checks, nil
}

// forceProbeKey marks a context whose checks ask the indexers whatever the
// schedule says: the "Check now" button.
type forceProbeKey struct{}

// probeIndexer is the monitor's check of one indexer. It must not use up an
// indexer's requests (free ones allow only a few a day), so it asks as little
// as it can: not at all when a real search or feed read worked since the last
// check, not more than once every six hours, and with the capabilities question
// instead of a search. The Test button in Settings still does a real search.
func (s *Server) probeIndexer(ctx context.Context, inst indexers.Instance) error {
	tr := inst.Tracker
	if ctx.Value(forceProbeKey{}) == nil {
		switch tr.ProbeAdvice(inst.ID, inst.RequestsPerDay) {
		case indexers.ProbeSkipWorks:
			return nil
		case indexers.ProbeSkipWait:
			return monitor.ErrSkip
		}
	}
	tr.NoteProbe(inst.ID)
	if inst.IsCardigann() {
		// a site that a definition drives has no capabilities call to ask
		return runProbe(ctx, s.indexerTestBudget(inst)+5*time.Second, func() connTestResult { return s.testIndexerInstance(ctx, inst) })
	}
	err := runProbe(ctx, probeTimeout, func() connTestResult {
		cctx, cancel := context.WithTimeout(ctx, max(connTestTimeout, indexers.FeedWait(inst.BaseURL)))
		defer cancel()
		if err := indexers.NewNewznabClient(inst.Name, inst.BaseURL, inst.APIKey).Caps(cctx); err != nil {
			return connTestResult{Message: err.Error()}
		}
		return connTestResult{OK: true}
	})
	if err != nil && tr.LearnLimit(inst.ID, err.Error()) {
		return monitor.ErrSkip // it works, its requests for today are used up
	}
	return err
}

// probeTimeout bounds a single monitor check, whatever the probe itself does.
var probeTimeout = 30 * time.Second

// runProbe runs a connection test and turns a failed result into an error. The
// NNTP probe has no context of its own, so it runs in a goroutine the monitor
// can stop waiting for.
// A check that is allowed longer than the usual limit is one that waits for
// FlareSolverr, and says so when it runs out.
func runProbe(ctx context.Context, limit time.Duration, probe func() connTestResult) error {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	done := make(chan connTestResult, 1)
	go func() { done <- probe() }()
	select {
	case res := <-done:
		if res.OK {
			return nil
		}
		return errors.New(res.Message)
	case <-ctx.Done():
		if limit > probeTimeout {
			return fmt.Errorf("The check waited %d seconds and didn't finish. If this site needs FlareSolverr, check that it is running and has internet access, then try again.", int(limit/time.Second))
		}
		return errors.New("the check timed out")
	}
}

// StartMonitor starts the background connectivity monitor and returns the
// function that stops it. Called once from cmd/app/main.go.
func (s *Server) StartMonitor() (stop func()) {
	if s.cfg.PauseAutomation {
		return func() {} // safe mode: no background checks
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Monitor.Run(ctx)
	}()
	return func() {
		cancel()
		<-done
	}
}

// handleMonitorStatus reports the last result for every enabled Usenet server
// and indexer.
func (s *Server) handleMonitorStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Monitor.Statuses())
}

// handleMonitorRun checks everything right now (even when the schedule is off)
// and returns the fresh results.
func (s *Server) handleMonitorRun(w http.ResponseWriter, r *http.Request) {
	s.Monitor.RunOnce(context.WithValue(r.Context(), forceProbeKey{}, true))
	writeJSON(w, http.StatusOK, s.Monitor.Statuses())
}
