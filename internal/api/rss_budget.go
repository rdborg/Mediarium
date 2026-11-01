package api

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
)

// RSS and the daily request limit. An indexer with a daily limit (set, or
// learned from its answers) is read by RSS sync only as often as a third of
// that limit allows, and never with the part kept for searches (see
// indexers.RSSAdvice). Indexers without a limit are read on every run.

// rssCallsPerRun is how many requests one RSS run makes to an indexer: one
// feed for movies and one for TV, when both are switched on, each read over up
// to the pages an automatic search reads.
func (s *Server) rssCallsPerRun() int {
	n := 0
	if s.moviesEnabled() {
		n++
	}
	if s.tvEnabled() {
		n++
	}
	return max(n, 1) * indexers.AutoSearchPages
}

// rssInstances keeps the indexers this RSS run may read, and notes the run
// against the ones with a daily limit.
func (s *Server) rssInstances(instances []indexers.Instance) []indexers.Instance {
	calls := s.rssCallsPerRun()
	out := make([]indexers.Instance, 0, len(instances))
	for _, inst := range instances {
		plan := inst.Tracker.RSSAdvice(inst.ID, inst.RequestsPerDay, calls)
		if !plan.Allowed {
			slog.Debug("rss-sync: indexer skipped to keep its daily limit for searches", "indexer", inst.Name, "budget", plan.Budget, "usedToday", plan.Used)
			continue
		}
		if plan.Limited {
			inst.Tracker.NoteRSS(inst.ID, calls)
		}
		out = append(out, inst)
	}
	return out
}

// rssNote says in plain words how often RSS reads an indexer with a daily
// limit, when that is less often than the general setting. It returns the
// effective gap in minutes (0 when the indexer is read on every run) and the
// sentence.
func (s *Server) rssNote(inst indexers.Instance) (int, string) {
	plan := inst.Tracker.RSSAdvice(inst.ID, inst.RequestsPerDay, s.rssCallsPerRun())
	if !plan.Limited {
		return 0, ""
	}
	if plan.Every == 0 {
		return 0, "Its daily limit is small, so new releases are only found by the searches."
	}
	every := max(plan.Every, s.releaseCheckInterval())
	if every <= s.releaseCheckInterval() {
		return 0, ""
	}
	mins := int(every / time.Minute)
	if every < 90*time.Minute {
		return mins, fmt.Sprintf("New releases are checked about every %d minutes, to keep most of its daily limit for searches.", mins)
	}
	return mins, fmt.Sprintf("New releases are checked about every %d hours, to keep most of its daily limit for searches.", int((every+30*time.Minute)/time.Hour))
}
