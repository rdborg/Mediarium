package api

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
)

// Resume interrupted downloads after a restart. When Mediarium stops or
// restarts while a download is running, the download comes back paused (see
// recoverAtStartup). With "Resume interrupted downloads after a restart" on
// (the default) the ones that were downloading carry on by themselves: Usenet
// from the articles it already has, a built-in torrent from its files (the
// hash check picks up what is there). They are started one at a time, a few
// seconds apart, and only when there is a free place in the line.

const (
	// resumeFirstDelay lets the VPN and the media-server check come up first.
	resumeFirstDelay = 15 * time.Second
	// resumeStagger is the pause between two resumed downloads.
	resumeStagger = 5 * time.Second
	// resumePoll is how often a held-back download looks again (a free place,
	// the VPN, someone watching).
	resumePoll = 5 * time.Second
	// resumeVPNWait is how long a torrent waits for its VPN before it is left paused.
	resumeVPNWait = 10 * time.Minute
	// resumeSlotWait is how long a download waits for a free place before it is left paused.
	resumeSlotWait = 6 * time.Hour
)

// resumeInterruptedOn is the setting. It is on unless it was switched off.
func (s *Server) resumeInterruptedOn() bool {
	v, _ := s.Settings.Get(settings.KeyResumeInterrupted)
	return v != "0"
}

// resumeDeps is what the resume run needs from the app around it, so a test
// can fake all of it.
type resumeDeps struct {
	Enabled func() bool // the setting
	Safe    func() bool // safe mode: nothing starts by itself
	Get     func(id int64) (queue.Item, error)
	Builtin func(item queue.Item) bool    // runs in Mediarium itself (not an external client or a debrid service)
	Watch   func() (watching, known bool) // someone is watching, and whether that is known yet
	Hold    func(ids []int64)             // hand the downloads to the "while someone is watching" list
	VPN     func(item queue.Item) error   // nil when the download may start now
	Free    func() bool                   // there is a free place in the line
	Resume  func(item queue.Item) error   // put the download back in line
	Event   func(item queue.Item, msg string)
	Sleep   func(ctx context.Context, d time.Duration) bool // false when ctx ended
	Now     func() time.Time
}

// resumeResult counts what a run did.
type resumeResult struct {
	Resumed, Held, Left int
}

// runResumeInterrupted works through items, oldest first, one at a time.
func runResumeInterrupted(ctx context.Context, d resumeDeps, items []queue.Item) resumeResult {
	var out resumeResult
	if len(items) == 0 || !d.Enabled() || d.Safe() {
		out.Left = len(items)
		return out
	}
	if !d.Sleep(ctx, resumeFirstDelay) {
		out.Left = len(items)
		return out
	}
	// Wait until it is known whether someone is watching (at most a minute).
	for i := 0; i < 12; i++ {
		if _, known := d.Watch(); known {
			break
		}
		if !d.Sleep(ctx, resumePoll) {
			out.Left = len(items)
			return out
		}
	}
	started := false
	for idx, it := range items {
		if ctx.Err() != nil {
			out.Left += len(items) - idx
			return out
		}
		// The person may have paused, stopped or removed it since the start.
		cur, err := d.Get(it.ID)
		if err != nil || cur.Status != queue.StatusPaused || !cur.Interrupted || !d.Builtin(cur) {
			out.Left++
			continue
		}
		if !d.Enabled() || d.Safe() { // switched off while the run was going
			out.Left += len(items) - idx
			return out
		}
		if w, _ := d.Watch(); w {
			// Someone is watching: the rest waits with the downloads that
			// were paused for that, and comes back when nobody is.
			rest := make([]int64, 0, len(items)-idx)
			for _, r := range items[idx:] {
				rest = append(rest, r.ID)
			}
			d.Hold(rest)
			out.Held += len(rest)
			return out
		}
		if cur.Protocol == queue.ProtocolTorrent {
			if !resumeWaitFor(ctx, d, resumeVPNWait, func() bool { return d.VPN(cur) == nil }) {
				out.Left++
				continue
			}
		}
		if !resumeWaitFor(ctx, d, resumeSlotWait, d.Free) {
			out.Left += len(items) - idx
			return out
		}
		if started && !d.Sleep(ctx, resumeStagger) {
			out.Left += len(items) - idx
			return out
		}
		if err := d.Resume(cur); err != nil {
			slog.Warn("queue: could not resume an interrupted download", "queueId", cur.ID, "err", err)
			out.Left++
			continue
		}
		started = true
		out.Resumed++
		d.Event(cur, "Resumed after a restart")
	}
	return out
}

// resumeWaitFor polls ok every resumePoll for up to limit; false when it never held
// or ctx ended.
func resumeWaitFor(ctx context.Context, d resumeDeps, limit time.Duration, ok func() bool) bool {
	deadline := d.Now().Add(limit)
	for {
		if ok() {
			return true
		}
		if !d.Now().Before(deadline) || !d.Sleep(ctx, resumePoll) {
			return false
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// resumeDepsFor connects runResumeInterrupted to the running app.
func (s *Server) resumeDepsFor() resumeDeps {
	return resumeDeps{
		Enabled: s.resumeInterruptedOn,
		Safe:    func() bool { return s.cfg.PauseAutomation },
		Get:     s.QueueRepo.Get,
		Builtin: func(it queue.Item) bool {
			switch it.Protocol {
			case queue.ProtocolTorrent:
				return s.torrentReleasesOn() && s.itemTorrentRoute(it) == indexers.RouteBuiltin
			case queue.ProtocolUsenet:
				return true
			}
			return false
		},
		Watch: func() (bool, bool) {
			if !s.streamPauseOn() {
				return false, true
			}
			st := &s.streamPause
			st.mu.Lock()
			defer st.mu.Unlock()
			return st.watching > 0, !st.checked.IsZero()
		},
		Hold: func(ids []int64) {
			s.holdForStreaming(ids)
		},
		VPN: func(queue.Item) error {
			_, err := s.torrentTunnel()
			return err
		},
		Free: func() bool { return s.dispatch.Running() < s.dispatch.Limit() },
		Resume: func(it queue.Item) error {
			queueControlMu.Lock()
			defer queueControlMu.Unlock()
			return s.resumeItem(s.freshItem(it))
		},
		Event: func(it queue.Item, msg string) { s.queueEvent(it.ID, "resumed", queue.LevelInfo, msg) },
		Sleep: sleepCtx,
		Now:   time.Now,
	}
}

// startResumeInterrupted carries on with the downloads a restart interrupted,
// in the background, until it is done or Mediarium stops.
func (s *Server) startResumeInterrupted(items []queue.Item) {
	if len(items) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.resumeStop = cancel
	go func() {
		res := runResumeInterrupted(ctx, s.resumeDepsFor(), items)
		if ctx.Err() != nil {
			return
		}
		slog.Info("queue: resumed interrupted downloads", "resumed", res.Resumed, "heldForStream", res.Held, "leftPaused", res.Left)
		if res.Resumed > 0 {
			_ = s.QueueRepo.LogActivity(0, "resumed", fmt.Sprintf("Mediarium was restarted, so %d of the downloads that were running carried on by themselves.", res.Resumed))
		}
		// The ones that could not carry on stay paused: say so, as the
		// start-up check does when the feature is switched off.
		skip := map[int64]bool{}
		if res.Held > 0 {
			for _, id := range s.pausedForStreaming() {
				skip[id] = true
			}
		}
		if n := stillInterrupted(s.QueueRepo.Get, items, skip); n > 0 {
			_ = s.QueueRepo.LogActivity(0, "paused", fmt.Sprintf("Mediarium was restarted, so %d of the downloads that were running could not carry on by themselves and are paused. Press Resume in Activity to carry on.", n))
		}
	}()
}

// stillInterrupted counts the items that are still paused because of the
// restart and that nobody has dealt with: not resumed, not paused or removed by
// a person since, and not in skip (held back while someone watches, which has
// its own way back).
func stillInterrupted(get func(id int64) (queue.Item, error), items []queue.Item, skip map[int64]bool) int {
	n := 0
	for _, it := range items {
		if skip[it.ID] {
			continue
		}
		cur, err := get(it.ID)
		if err == nil && cur.Status == queue.StatusPaused && cur.Interrupted {
			n++
		}
	}
	return n
}
