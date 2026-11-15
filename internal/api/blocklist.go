package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
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

// dropBlocked removes blocklisted releases from search results, and those
// held back for now when blocked comes from searchSkipKeys. sc is the title the
// search is for: a release blocklisted for that title alone is dropped too. A
// search for no one title (sc is zero) drops only what is blocked everywhere.
func dropBlocked(results []indexers.Result, blocked map[string]bool, sc blocklist.Scope) []indexers.Result {
	if len(blocked) == 0 {
		return results
	}
	out := make([]indexers.Result, 0, len(results))
	for _, r := range results {
		if !searchSkips(r, blocked, sc) {
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
	reason := blocklistReason(cause)
	if err := s.Blocklist.Add(blocklist.Entry{
		ReleaseTitle: releaseTitle, Protocol: string(protocol), Reason: reason, MovieID: movieID, SeriesID: seriesID,
	}); err != nil {
		log.Printf("api: %v", err)
		return false
	}
	_ = s.QueueRepo.LogItemActivity(movieID, seriesID, "blocklisted", fmt.Sprintf("Blocklisted \"%s\": %s", releaseTitle, reason))
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
	if !s.automationEnabled() || !s.moviesEnabled() {
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
	if !s.huntMovie(context.Background(), m, instances, profiles, s.searchSkipKeys(), false) {
		s.retryEvent(movieID, 0, noOtherRelease)
	}
}

func (s *Server) retrySeries(seriesID int64) {
	if !s.automationEnabled() || !s.tvEnabled() {
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
	if s.autoGrabTV(s.targetedTVSearcher(context.Background(), instances, maxTVSearchesPerHunt, false), "retry", profiles, tvScope{seriesID: seriesID}) == 0 {
		s.retryEvent(0, seriesID, noOtherRelease)
	}
}

type blocklistPayload struct {
	ID           int64  `json:"id"`
	ReleaseTitle string `json:"releaseTitle"`
	Protocol     string `json:"protocol"`
	Reason       string `json:"reason"`
	CreatedAt    string `json:"createdAt,omitempty"`
	// Scope is "all" (the release is blocked for every title) or "item" (for
	// the one title named by the fields below only).
	Scope string `json:"scope"`
	// ItemKind (movie, series, album or book), ItemID and ItemTitle name the
	// title the entry was made for, when there is one. For an entry for
	// everywhere it is where the release failed.
	ItemKind  string `json:"itemKind,omitempty"`
	ItemID    int64  `json:"itemId,omitempty"`
	ItemTitle string `json:"itemTitle,omitempty"`
}

// parseBlocklistScope reads the scope a request names: "item" is for one title,
// "all" or nothing is everywhere.
func parseBlocklistScope(scope string) (itemOnly, ok bool) {
	switch scope {
	case "", blocklist.ScopeAll:
		return false, true
	case blocklist.ScopeItem:
		return true, true
	}
	return false, false
}

// onlyForThisTitle is the note added to an Activity line for a release that is
// blocklisted for one title only.
func onlyForThisTitle(itemOnly bool) string {
	if itemOnly {
		return " (only for this title)"
	}
	return ""
}

// blocklistTitle names the title an entry is tied to.
func (s *Server) blocklistTitle(e blocklist.Entry) (kind string, id int64, title string) {
	switch {
	case e.MovieID != 0:
		if m, err := s.MovieRepo.Get(e.MovieID); err == nil {
			return "movie", e.MovieID, m.Title
		}
	case e.SeriesID != 0:
		if sr, err := s.MovieRepo.GetSeries(e.SeriesID); err == nil {
			return "series", e.SeriesID, sr.Title
		}
	case e.AlbumID != 0:
		if al, err := s.MusicRepo.GetAlbum(e.AlbumID); err == nil {
			return "album", e.AlbumID, al.Title
		}
	case e.BookID != 0:
		if b, err := s.BookRepo.Get(e.BookID); err == nil {
			return "book", e.BookID, b.Title
		}
	}
	return "", 0, ""
}

// handleListBlocklist lists the blocklist, newest first. With no query it is
// every entry, as a plain list. With limit (and offset) it is one page, and the
// answer is {items, total, limit, offset}; the limit is at most 500.
func (s *Server) handleListBlocklist(w http.ResponseWriter, r *http.Request) {
	var (
		entries       []blocklist.Entry
		total         int
		err           error
		limit, offset int
	)
	paged := r.URL.Query().Get("limit") != ""
	if paged {
		limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
		limit = min(max(limit, 1), 500)
		offset = max(offset, 0)
		entries, total, err = s.Blocklist.Page(limit, offset)
	} else {
		entries, err = s.Blocklist.List()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []blocklistPayload{}
	for _, e := range entries {
		p := blocklistPayload{ID: e.ID, ReleaseTitle: e.ReleaseTitle, Protocol: e.Protocol, Reason: e.Reason, Scope: blocklist.ScopeAll}
		if e.ItemOnly {
			p.Scope = blocklist.ScopeItem
		}
		p.ItemKind, p.ItemID, p.ItemTitle = s.blocklistTitle(e)
		if !e.CreatedAt.IsZero() {
			p.CreatedAt = e.CreatedAt.Format(time.RFC3339)
		}
		out = append(out, p)
	}
	if paged {
		writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": total, "limit": limit, "offset": offset})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRemoveBlocklistEntry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid blocklist ID.")
		return
	}
	if err := s.Blocklist.Remove(id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That blocklist entry no longer exists.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

// handleSetBlocklistScope switches an entry between "all" (every title) and
// "item" (only the title it was made for).
func (s *Server) handleSetBlocklistScope(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid blocklist ID.")
		return
	}
	var req struct {
		Scope string `json:"scope"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Scope == "" {
		writeError(w, http.StatusBadRequest, "Send the scope as \"item\" or \"all\".")
		return
	}
	itemOnly, ok := parseBlocklistScope(req.Scope)
	if !ok {
		writeError(w, http.StatusBadRequest, "Send the scope as \"item\" or \"all\".")
		return
	}
	switch err := s.Blocklist.SetScope(id, itemOnly); {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "That blocklist entry no longer exists.")
	case errors.Is(err, blocklist.ErrNoTitle):
		writeError(w, http.StatusConflict, "This release isn't tied to a title, so it can only be blocked everywhere.")
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, http.StatusOK, nil)
	}
}

func (s *Server) handleClearBlocklist(w http.ResponseWriter, r *http.Request) {
	if err := s.Blocklist.Clear(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}
