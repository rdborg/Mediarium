package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/indexers"
)

// What the indexer list, Health and the Statistics page say about indexers
// that keep failing (the back-off in internal/indexers/backoff.go) and how
// each one has been doing.

// addPauseInfo fills in the pause fields of an indexer's payload while its
// searches are being skipped.
func addPauseInfo(p *indexerPayload, inst indexers.Instance) {
	h, ok := inst.Tracker.Health(inst.ID)
	if !ok {
		return
	}
	p.Failures = h.Failures
	if h.Paused(time.Now()) {
		p.PausedUntil = h.PausedUntil.UTC().Format(time.RFC3339)
		p.PausedNote = indexers.PausedMessage(inst.Name, h, nil)
		p.PausedWhy = h.Reason
	}
}

// addQuotaInfo fills in where the indexer stands against its daily limit.
func addQuotaInfo(p *indexerPayload, inst indexers.Instance) {
	q := inst.Tracker.Quota(inst.ID, inst.RequestsPerDay)
	p.DailyLimit, p.LimitLearned, p.RequestsToday = q.Limit, q.Learned, q.Used
	p.LimitNote = q.Message(nil)
}

// handleResumeIndexer is "Try again now": the indexer is asked by the next
// search, and its failures are forgotten.
func (s *Server) handleResumeIndexer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid indexer ID.")
		return
	}
	if _, err := s.IndexerRepo.Get(id); errors.Is(err, indexers.ErrNotFound) {
		writeError(w, http.StatusNotFound, "That indexer no longer exists.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.IndexerRepo.Tracker().Resume(id)
	writeJSON(w, http.StatusOK, nil)
}

// pausedIndexerProblems adds a Health line for every enabled indexer that is
// being skipped.
func (s *Server) pausedIndexerProblems(instances []indexers.Instance, addHealth func(id, level, title, detail, action, link string)) {
	now := time.Now()
	for _, inst := range instances {
		if !inst.Enabled {
			continue
		}
		h, ok := inst.Tracker.Health(inst.ID)
		if !ok || !h.Paused(now) {
			continue
		}
		addHealth(fmt.Sprintf("indexer-paused-%d", inst.ID), "warn", fmt.Sprintf("%s is paused", inst.Name),
			fmt.Sprintf("%s Searches skip it until then. Press Try again now once it is fixed.", indexers.PausedMessage(inst.Name, h, nil)),
			"Open indexers", "/settings/indexers")
	}
}

// indexerStatPayload is one indexer's line on the Statistics page.
type indexerStatPayload struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Protocol    string  `json:"protocol"`
	Enabled     bool    `json:"enabled"`
	Queries     int     `json:"queries"`
	Failures    int     `json:"failures"`
	Results     int     `json:"results"`
	Grabs       int     `json:"grabs"`
	GrabBytes   int64   `json:"grabBytes"`   // the size of the releases grabbed
	SuccessRate float64 `json:"successRate"` // 0 to 1, -1 when it was not searched
	PausedUntil string  `json:"pausedUntil,omitempty"`
}

type indexerStatsPayload struct {
	Days     int                  `json:"days"`
	Indexers []indexerStatPayload `json:"indexers"`
	// Servers is what each Usenet server delivered to the built-in
	// downloader over the same days (#167).
	Servers []serverTrafficPayload `json:"servers"`
}

// serverTrafficPayload is one Usenet server's line on the Statistics page.
type serverTrafficPayload struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Enabled  bool   `json:"enabled"`
	Bytes    int64  `json:"bytes"`
	Articles int64  `json:"articles"`
	Missing  int64  `json:"missing"` // articles the server didn't have
}

// statsDays is the period the Statistics page adds up.
const indexerStatsDays = 30

// handleIndexerStats gives each indexer's searches, failures, releases found
// and grabs over the last 30 days.
func (s *Server) handleIndexerStats(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	tr := s.IndexerRepo.Tracker()
	stats, err := tr.Stats(now.AddDate(0, 0, -(indexerStatsDays - 1)))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	list, err := s.IndexerRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byID := map[int64]indexers.Instance{}
	for _, inst := range list {
		byID[inst.ID] = inst
	}
	out := indexerStatsPayload{Days: indexerStatsDays, Indexers: []indexerStatPayload{}}
	for _, st := range stats {
		inst := byID[st.IndexerID]
		p := indexerStatPayload{
			ID: st.IndexerID, Name: st.Name, Protocol: string(inst.Protocol), Enabled: inst.Enabled,
			Queries: st.Queries, Failures: st.Failures, Results: st.Results, Grabs: st.Grabs, GrabBytes: st.GrabBytes, SuccessRate: st.SuccessRate(),
		}
		if until, paused := tr.PausedUntil(st.IndexerID); paused {
			p.PausedUntil = until.UTC().Format(time.RFC3339)
		}
		out.Indexers = append(out.Indexers, p)
	}
	// The busiest first.
	sort.SliceStable(out.Indexers, func(i, j int) bool { return out.Indexers[i].Queries > out.Indexers[j].Queries })
	out.Servers = []serverTrafficPayload{}
	_ = s.ClientRepo.SaveTraffic(now) // what was counted in the last minute, too
	if servers, err := s.ClientRepo.TrafficSince(now.AddDate(0, 0, -(indexerStatsDays - 1))); err == nil {
		for _, t := range servers {
			out.Servers = append(out.Servers, serverTrafficPayload{ID: t.ServerID, Name: t.Name, Host: t.Host, Enabled: t.Enabled, Bytes: t.Bytes, Articles: t.Articles, Missing: t.Missing})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// usenetMonthsPayload is what each Usenet server delivered per calendar month.
type usenetMonthsPayload struct {
	Months []usenetMonthPayload `json:"months"`
}

type usenetMonthPayload struct {
	Month   string                 `json:"month"` // "2026-11"
	Servers []serverTrafficPayload `json:"servers"`
}

// maxUsenetMonths is the longest history the month view gives.
const maxUsenetMonths = 24

// handleUsenetMonths gives each Usenet server's traffic month by month, newest
// month first, for the last `months` months (default 12, at most 24), so a
// yearly plan can be judged over the year (#167).
func (s *Server) handleUsenetMonths(w http.ResponseWriter, r *http.Request) {
	n := 12
	if v, err := strconv.Atoi(r.URL.Query().Get("months")); err == nil && v >= 1 {
		n = min(v, maxUsenetMonths)
	}
	now := time.Now().UTC()
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -(n - 1), 0)
	_ = s.ClientRepo.SaveTraffic(now)
	servers, err := s.ClientRepo.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byID := map[int64]serverTrafficPayload{}
	for _, sc := range servers {
		byID[sc.ID] = serverTrafficPayload{ID: sc.ID, Name: sc.Name, Host: sc.Config.Host, Enabled: sc.Enabled}
	}
	rows, err := s.ClientRepo.TrafficByMonth(first)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Every month in the range is listed, also the ones with nothing.
	out := usenetMonthsPayload{Months: []usenetMonthPayload{}}
	idx := map[string]int{}
	for m := now.AddDate(0, 0, 1-now.Day()); !m.Before(first); m = m.AddDate(0, -1, 0) {
		key := m.Format("2006-01")
		idx[key] = len(out.Months)
		out.Months = append(out.Months, usenetMonthPayload{Month: key, Servers: []serverTrafficPayload{}})
	}
	for _, row := range rows {
		i, ok := idx[row.Month]
		p, known := byID[row.ServerID]
		if !ok || !known {
			continue
		}
		p.Bytes, p.Articles, p.Missing = row.Bytes, row.Articles, row.Missing
		out.Months[i].Servers = append(out.Months[i].Servers, p)
	}
	writeJSON(w, http.StatusOK, out)
}

// serverTrafficJob saves what each Usenet server delivered, and once a day
// forgets days older than the indexer counters keep.
func (s *Server) serverTrafficJob(context.Context) {
	now := time.Now()
	if err := s.ClientRepo.SaveTraffic(now); err != nil {
		slog.Warn("statistics: save usenet server traffic", "err", err)
	}
	if day := now.UTC().Format("2006-01-02"); day != s.trafficPrunedOn { // the scheduler runs one job at a time
		s.trafficPrunedOn = day
		s.ClientRepo.PruneTraffic(now, download.TrafficKeepDays)
	}
}
