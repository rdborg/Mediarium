package api

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/ryanborg/mediarium/internal/blocklist"
	"github.com/ryanborg/mediarium/internal/download"
	"github.com/ryanborg/mediarium/internal/indexers"
	"github.com/ryanborg/mediarium/internal/library"
)

const (
	// maxAutoRetries bounds automatic "try the next-best release" attempts
	// per movie/series per day, so an indexer full of duds can't keep the
	// downloader busy forever.
	maxAutoRetries = 5
	retryWindow    = 24 * time.Hour
)

// badReleaseError marks a pipeline failure that is the release's fault (no
// video inside, failed repair, unusable NZB) as opposed to a problem with
// our setup (no download client, VPN down, disk full), which must never
// blocklist anything.
type badReleaseError struct{ err error }

func (e *badReleaseError) Error() string { return e.err.Error() }
func (e *badReleaseError) Unwrap() error { return e.err }

func badRelease(err error) error { return &badReleaseError{err: err} }

func isBadRelease(err error) bool {
	var b *badReleaseError
	var d *download.ReleaseError
	return errors.As(err, &b) || errors.As(err, &d)
}

// dropBlocked removes blocklisted releases from search results.
func dropBlocked(results []indexers.Result, blocked map[string]bool) []indexers.Result {
	if len(blocked) == 0 {
		return results
	}
	out := make([]indexers.Result, 0, len(results))
	for _, r := range results {
		if !blocked[blocklist.Key(r.Title)] {
			out = append(out, r)
		}
	}
	return out
}

// blockedKeys returns the blocklist as a lookup set; on a database error it
// logs and returns an empty set (a hiccup shouldn't stop automation).
func (s *Server) blockedKeys() map[string]bool {
	keys, err := s.Blocklist.Keys()
	if err != nil {
		log.Printf("api: load blocklist: %v", err)
		return map[string]bool{}
	}
	return keys
}

// blocklistBadRelease blocklists a release that failed through its own
// fault and reports whether it was recorded. A pipeline calls it before it
// hands the title back to "missing", so no automation run can see the title
// as wanted while the bad release is not blocklisted yet (and grab it
// again), and then calls retryAfterBadRelease.
func (s *Server) blocklistBadRelease(releaseTitle string, protocol indexers.Protocol, movieID, seriesID int64, cause error) bool {
	if err := s.Blocklist.Add(blocklist.Entry{
		ReleaseTitle: releaseTitle, Protocol: string(protocol), Reason: cause.Error(), MovieID: movieID, SeriesID: seriesID,
	}); err != nil {
		log.Printf("api: %v", err)
		return false
	}
	_ = s.QueueRepo.LogItemActivity(movieID, seriesID, "blocklisted", "Blocklisted \""+releaseTitle+"\": "+cause.Error())
	return true
}

// retryAfterBadRelease, when automation is on, tries the next-best release
// for the same movie or series in the background. Call it only after the
// failed item has been put back to "missing".
func (s *Server) retryAfterBadRelease(movieID, seriesID int64) {
	s.background(func() {
		if movieID != 0 {
			s.retryMovie(movieID)
		} else if seriesID != 0 {
			s.retrySeries(seriesID)
		}
	})
}

func (s *Server) retryBudgetLeft(movieID, seriesID int64) bool {
	n, err := s.Blocklist.CountRecent(movieID, seriesID, time.Now().Add(-retryWindow))
	if err != nil {
		log.Printf("api: %v", err)
		return false
	}
	if n > maxAutoRetries {
		log.Printf("api: giving up on automatic retries (movie %d / series %d): %d releases blocklisted in the last day", movieID, seriesID, n)
		s.retryEvent(movieID, seriesID, retryPausedMessage(n))
		return false
	}
	return true
}

func (s *Server) retryMovie(movieID int64) {
	if !s.automationEnabled() {
		return
	}
	m, err := s.MovieRepo.Get(movieID)
	if err != nil || !m.Monitored || m.Status != library.StatusMissing || !s.retryBudgetLeft(movieID, 0) {
		return
	}
	instances, err := s.searchableIndexers()
	if err != nil || len(instances) == 0 {
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		return
	}
	if !s.huntMovie(context.Background(), m, instances, profiles, s.blockedKeys(), false) {
		s.retryEvent(movieID, 0, noOtherRelease)
	}
}

func (s *Server) retrySeries(seriesID int64) {
	if !s.automationEnabled() {
		return
	}
	sr, err := s.MovieRepo.GetSeries(seriesID)
	if err != nil || !sr.Monitored || !s.retryBudgetLeft(0, seriesID) {
		return
	}
	instances, err := s.searchableIndexers()
	if err != nil || len(instances) == 0 {
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		return
	}
	if s.autoGrabTV(s.targetedTVSearcher(context.Background(), instances, maxTVSearchesPerHunt), "retry", profiles, tvScope{seriesID: seriesID}) == 0 {
		s.retryEvent(0, seriesID, noOtherRelease)
	}
}

type blocklistPayload struct {
	ID           int64  `json:"id"`
	ReleaseTitle string `json:"releaseTitle"`
	Protocol     string `json:"protocol"`
	Reason       string `json:"reason"`
	CreatedAt    string `json:"createdAt,omitempty"`
}

func (s *Server) handleListBlocklist(w http.ResponseWriter, r *http.Request) {
	entries, err := s.Blocklist.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []blocklistPayload{}
	for _, e := range entries {
		p := blocklistPayload{ID: e.ID, ReleaseTitle: e.ReleaseTitle, Protocol: e.Protocol, Reason: e.Reason}
		if !e.CreatedAt.IsZero() {
			p.CreatedAt = e.CreatedAt.Format(time.RFC3339)
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRemoveBlocklistEntry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid blocklist id")
		return
	}
	if err := s.Blocklist.Remove(id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "blocklist entry not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

func (s *Server) handleClearBlocklist(w http.ResponseWriter, r *http.Request) {
	if err := s.Blocklist.Clear(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}
