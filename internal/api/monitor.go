package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ryanborg/mediarium/internal/indexers"
	"github.com/ryanborg/mediarium/internal/monitor"
	"github.com/ryanborg/mediarium/internal/settings"
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
// indexers, using the same probes as the Test buttons in Settings.
func (s *Server) newMonitor() *monitor.Monitor {
	return &monitor.Monitor{
		Checks:   s.monitorChecks,
		Interval: s.monitorInterval,
		Notify: func(ev monitor.Event) {
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
				return runProbe(ctx, func() connTestResult {
					return testNNTP(cfg.Host, cfg.Port, cfg.UseSSL, cfg.Username, cfg.Password)
				})
			},
		})
	}

	instances, err := s.IndexerRepo.List()
	if err != nil {
		return nil, err
	}
	torrentsOn := s.torrentsEnabled()
	for _, inst := range instances {
		if !inst.Enabled || (inst.Protocol == indexers.ProtocolTorrent && !torrentsOn) {
			continue
		}
		checks = append(checks, monitor.Check{
			Kind: monitor.KindIndexer, ID: inst.ID, Name: inst.Name,
			Run: func(ctx context.Context) error {
				return runProbe(ctx, func() connTestResult {
					return s.testIndexerInstance(ctx, inst)
				})
			},
		})
	}
	return checks, nil
}

// probeTimeout bounds a single monitor check, whatever the probe itself does.
const probeTimeout = 30 * time.Second

// runProbe runs a connection test and turns a failed result into an error. The
// NNTP probe has no context of its own, so it runs in a goroutine the monitor
// can stop waiting for.
func runProbe(ctx context.Context, probe func() connTestResult) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
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
		return errors.New("the check timed out")
	}
}

// StartMonitor starts the background connectivity monitor and returns the
// function that stops it. Called once from cmd/app/main.go.
func (s *Server) StartMonitor() (stop func()) {
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
	s.Monitor.RunOnce(r.Context())
	writeJSON(w, http.StatusOK, s.Monitor.Statuses())
}
