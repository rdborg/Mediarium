package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/plainerror"
)

// Bulk changes to the library: the Library page's select mode sends one
// request for the whole selection instead of one per title. Every one of them
// is for administrators, takes {"items": [{"kind": "movie" | "tv", "id": n}]}
// and answers with what was done and, for anything that was not, why.

const (
	// maxBulkItems is the most titles one bulk request may name.
	maxBulkItems = 5000

	// maxBulkSearch is how many titles one "search now" for a selection
	// looks up. Every title asks each indexer, so a bigger batch would
	// hammer them (and could start dozens of downloads at once).
	maxBulkSearch = 25

	// bulkSearchTimeout ends a batch search that has run far too long.
	bulkSearchTimeout = 45 * time.Minute
)

// bulkState remembers that a batch search is running, so two of them do not
// hit the indexers at the same time.
type bulkState struct {
	searching atomic.Bool
}

// removeProblem is a reason a title could not be removed, with the status the
// single-title endpoints answer with.
type removeProblem struct {
	status int
	msg    string
}

func (e *removeProblem) Error() string { return e.msg }

// writeRemoveError answers a failed removal the way the delete endpoints do.
func writeRemoveError(w http.ResponseWriter, err error) {
	var problem *removeProblem
	if errors.As(err, &problem) {
		writeError(w, problem.status, problem.msg)
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

type bulkItem struct {
	Kind string `json:"kind"`
	ID   int64  `json:"id"`
}

type bulkFailure struct {
	Kind   string `json:"kind"`
	ID     int64  `json:"id"`
	Title  string `json:"title,omitempty"`
	Reason string `json:"reason"`
}

type bulkResult struct {
	Updated int           `json:"updated"`
	Failed  []bulkFailure `json:"failed"`
	Message string        `json:"message"`
}

const reasonNotFound = "It isn't in your library any more."

// parseBulkItems checks the titles a request names and drops repeats.
func parseBulkItems(items []bulkItem) ([]library.Ref, error) {
	if len(items) == 0 {
		return nil, errors.New("Pick at least one title first.")
	}
	if len(items) > maxBulkItems {
		return nil, fmt.Errorf("That is too many titles at once. Change up to %d at a time.", maxBulkItems)
	}
	seen := map[library.Ref]bool{}
	out := make([]library.Ref, 0, len(items))
	for _, it := range items {
		if (it.Kind != library.KindMovie && it.Kind != library.KindShow) || it.ID <= 0 {
			return nil, errors.New("One of the chosen titles isn't valid. Reload the page and try again.")
		}
		ref := library.Ref{Kind: it.Kind, ID: it.ID}
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out, nil
}

// bulkMessage says in plain words how a bulk change went. noun is "title" or
// "artist".
func bulkMessage(updated int, failed []bulkFailure, noun string) string {
	switch {
	case len(failed) == 0:
		return fmt.Sprintf("Done: %d updated.", updated)
	case updated == 0:
		return fmt.Sprintf("Nothing was changed. %s could not be changed.", plural(len(failed), noun))
	default:
		return fmt.Sprintf("Done: %d updated. %s could not be changed.", updated, plural(len(failed), noun))
	}
}

type bulkMonitoredRequest struct {
	Items     []bulkItem `json:"items"`
	Monitored *bool      `json:"monitored"`
}

// handleBulkMonitored turns monitoring on or off for the chosen titles.
func (s *Server) handleBulkMonitored(w http.ResponseWriter, r *http.Request) {
	var req bulkMonitoredRequest
	if err := decodeJSON(r, &req); err != nil || req.Monitored == nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	s.bulkSet(w, req.Items, library.BulkMonitored, *req.Monitored)
}

type bulkNoUpgradeRequest struct {
	Items     []bulkItem `json:"items"`
	NoUpgrade *bool      `json:"noUpgrade"`
}

// handleBulkNoUpgrade switches the search for better versions off (noUpgrade
// true) or back on for the chosen titles.
func (s *Server) handleBulkNoUpgrade(w http.ResponseWriter, r *http.Request) {
	var req bulkNoUpgradeRequest
	if err := decodeJSON(r, &req); err != nil || req.NoUpgrade == nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	s.bulkSet(w, req.Items, library.BulkNoUpgrade, *req.NoUpgrade)
}

type bulkProfileRequest struct {
	Items     []bulkItem `json:"items"`
	ProfileID *int64     `json:"profileId"` // 0 = use the default profile
}

// handleBulkProfile gives the chosen titles a quality profile.
func (s *Server) handleBulkProfile(w http.ResponseWriter, r *http.Request) {
	var req bulkProfileRequest
	if err := decodeJSON(r, &req); err != nil || req.ProfileID == nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if err := s.checkProfileChoice(*req.ProfileID); err != nil {
		writeProfileError(w, err)
		return
	}
	s.bulkSet(w, req.Items, library.BulkProfile, *req.ProfileID)
}

type bulkSourcesRequest struct {
	Items   []bulkItem `json:"items"`
	Sources *string    `json:"sources"` // "" = follow the default
}

// handleBulkSources chooses where the chosen titles are downloaded from.
func (s *Server) handleBulkSources(w http.ResponseWriter, r *http.Request) {
	var req bulkSourcesRequest
	if err := decodeJSON(r, &req); err != nil || req.Sources == nil || !validSourcePref(*req.Sources) {
		writeError(w, http.StatusBadRequest, `Choose where to download from: "usenet", "torrent" or "both". Leave it blank to use the default.`)
		return
	}
	s.bulkSet(w, req.Items, library.BulkSourcePref, *req.Sources)
}

// bulkSet applies one change to the chosen titles in a single transaction.
func (s *Server) bulkSet(w http.ResponseWriter, items []bulkItem, field library.BulkField, value any) {
	refs, err := parseBulkItems(items)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	missing, err := s.MovieRepo.BulkSet(field, value, refs)
	if err != nil {
		log.Printf("bulk change: %v", err)
		writeError(w, http.StatusInternalServerError, "Nothing was changed because the database could not save it. Try again in a moment.")
		return
	}
	s.afterBulkChange(field, value, refs, missing)
	res := bulkResult{Updated: len(refs) - len(missing), Failed: []bulkFailure{}}
	for _, m := range missing {
		res.Failed = append(res.Failed, bulkFailure{Kind: m.Kind, ID: m.ID, Reason: reasonNotFound})
	}
	res.Message = bulkMessage(res.Updated, res.Failed, "title")
	writeJSON(w, http.StatusOK, res)
}

// afterBulkChange does for every changed title what the single-title
// endpoints do: a title that stops being monitored, or is set to leave what
// it has alone, loses the downloads that are only waiting.
func (s *Server) afterBulkChange(field library.BulkField, value any, refs, missing []library.Ref) {
	on, _ := value.(bool)
	var reason string
	var onlyUpgrades bool
	switch {
	case field == library.BulkMonitored && !on:
		reason = stoppedMonitoringNote
	case field == library.BulkNoUpgrade && on:
		reason, onlyUpgrades = betterVersionsOffNote, true
	default:
		return
	}
	gone := map[library.Ref]bool{}
	for _, m := range missing {
		gone[m] = true
	}
	for _, ref := range refs {
		if gone[ref] {
			continue
		}
		if ref.Kind == library.KindMovie {
			s.clearWaitingDownloads(ref.ID, 0, onlyUpgrades, reason)
		} else {
			s.clearWaitingDownloads(0, ref.ID, onlyUpgrades, reason)
		}
	}
}

type bulkRemoveRequest struct {
	Items []bulkItem `json:"items"`
	// DeleteFiles also deletes the titles' files from the library folder.
	// Left out, it is false: nothing on disk is touched.
	DeleteFiles bool `json:"deleteFiles"`
}

// handleBulkRemove takes the chosen titles out of the library, one after the
// other. Their downloads are cancelled either way; files in the library
// folder are deleted only when the request says deleteFiles: true. A title
// that cannot be removed (its files lie outside the library folder, say) is
// reported and the rest carry on.
func (s *Server) handleBulkRemove(w http.ResponseWriter, r *http.Request) {
	var req bulkRemoveRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	refs, err := parseBulkItems(req.Items)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res := bulkResult{Failed: []bulkFailure{}}
	for _, ref := range refs {
		var title string
		var err error
		if ref.Kind == library.KindMovie {
			title, err = s.removeMovie(ref.ID, req.DeleteFiles)
		} else {
			title, err = s.removeSeries(ref.ID, req.DeleteFiles)
		}
		if err == nil {
			res.Updated++
			continue
		}
		f := bulkFailure{Kind: ref.Kind, ID: ref.ID, Title: title, Reason: plainerror.Message(err)}
		var problem *removeProblem
		if errors.As(err, &problem) && problem.status == http.StatusNotFound {
			f.Reason = reasonNotFound
		}
		res.Failed = append(res.Failed, f)
	}
	res.Message = bulkRemoveMessage(res.Updated, res.Failed, req.DeleteFiles, "title")
	writeJSON(w, http.StatusOK, res)
}

func bulkRemoveMessage(removed int, failed []bulkFailure, filesToo bool, noun string) string {
	what := "removed"
	if filesToo {
		what = "removed, with their files"
	}
	switch {
	case len(failed) == 0:
		return fmt.Sprintf("Done: %d %s.", removed, what)
	case removed == 0:
		return fmt.Sprintf("Nothing was removed. %s could not be removed.", plural(len(failed), noun))
	default:
		return fmt.Sprintf("Done: %d %s. %s could not be removed.", removed, what, plural(len(failed), noun))
	}
}

type bulkSearchRequest struct {
	Items []bulkItem `json:"items"`
}

type bulkSearchResult struct {
	// Searching is how many titles the search started for.
	Searching int `json:"searching"`
	// LeftOut is how many wanted titles were over the limit of Limit and
	// were not searched this time.
	LeftOut int `json:"leftOut"`
	Limit   int `json:"limit"`
	// Skipped is how many chosen titles did not need a search: not
	// monitored, nothing missing, or not released yet.
	Skipped int           `json:"skipped"`
	Failed  []bulkFailure `json:"failed"`
	Message string        `json:"message"`
}

// handleBulkSearchNow looks for releases of the chosen titles that are
// monitored and still missing something. It answers at once and does the
// searching in the background, one title after the other, so a long
// selection never keeps the page waiting. At most maxBulkSearch titles are
// searched per request.
func (s *Server) handleBulkSearchNow(w http.ResponseWriter, r *http.Request) {
	var req bulkSearchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	refs, err := parseBulkItems(req.Items)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.bulk.searching.Load() {
		writeError(w, http.StatusConflict, "A search for a group of titles is still running. Wait for it to finish, then try again.")
		return
	}
	if _, ok := s.hasIndexers(w); !ok {
		return
	}

	res := bulkSearchResult{Limit: maxBulkSearch, Failed: []bulkFailure{}}
	var movies []library.Movie
	var shows []library.Series
	for _, ref := range refs {
		if ref.Kind == library.KindMovie {
			m, err := s.MovieRepo.Get(ref.ID)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				res.Failed = append(res.Failed, bulkFailure{Kind: ref.Kind, ID: ref.ID, Reason: reasonNotFound})
			case err != nil:
				res.Failed = append(res.Failed, bulkFailure{Kind: ref.Kind, ID: ref.ID, Reason: plainerror.Message(err)})
			case !m.Monitored || m.Status != library.StatusMissing || unreleased(m.ReleaseDate):
				res.Skipped++
			default:
				movies = append(movies, m)
			}
			continue
		}
		sr, err := s.MovieRepo.GetSeries(ref.ID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			res.Failed = append(res.Failed, bulkFailure{Kind: ref.Kind, ID: ref.ID, Reason: reasonNotFound})
		case err != nil:
			res.Failed = append(res.Failed, bulkFailure{Kind: ref.Kind, ID: ref.ID, Reason: plainerror.Message(err)})
		case !sr.Monitored || sr.DownloadedCount >= sr.EpisodeCount:
			res.Skipped++
		default:
			shows = append(shows, sr)
		}
	}

	// Movies first, then shows, up to the limit.
	wanted := len(movies) + len(shows)
	if wanted > maxBulkSearch {
		res.LeftOut = wanted - maxBulkSearch
		if len(movies) > maxBulkSearch {
			movies, shows = movies[:maxBulkSearch], nil
		} else {
			shows = shows[:maxBulkSearch-len(movies)]
		}
	}
	res.Searching = len(movies) + len(shows)
	res.Message = bulkSearchMessage(res)
	if res.Searching == 0 {
		writeJSON(w, http.StatusOK, res)
		return
	}
	if !s.bulk.searching.CompareAndSwap(false, true) {
		writeError(w, http.StatusConflict, "A search for a group of titles is still running. Wait for it to finish, then try again.")
		return
	}
	s.background(func() {
		defer s.bulk.searching.Store(false)
		s.runBulkSearch(movies, shows)
	})
	writeJSON(w, http.StatusAccepted, res)
}

// runBulkSearch searches for each title in turn and notes the outcome in
// Activity when it is done.
func (s *Server) runBulkSearch(movies []library.Movie, shows []library.Series) {
	ctx, cancel := context.WithTimeout(context.Background(), bulkSearchTimeout)
	defer cancel()
	instances, err := s.searchableIndexers()
	if err != nil || len(instances) == 0 {
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		log.Printf("bulk search: load quality profiles: %v", err)
		return
	}
	blocked := s.blockedKeys()
	started := 0
	for _, m := range movies {
		if ctx.Err() != nil {
			break
		}
		if s.huntMovie(ctx, m, instances, profiles, blocked, true) {
			started++
		}
	}
	for _, sr := range shows {
		if ctx.Err() != nil {
			break
		}
		started += s.autoGrabTV(s.targetedTVSearcher(ctx, instances, maxTVSearchesPerHunt), "bulk-search", profiles,
			tvScope{seriesID: sr.ID, force: true})
	}
	total := len(movies) + len(shows)
	msg := fmt.Sprintf("Searched for %s: nothing suitable was found.", plural(total, "title"))
	if started > 0 {
		msg = fmt.Sprintf("Searched for %s and started %s.", plural(total, "title"), plural(started, "download"))
	}
	_ = s.QueueRepo.LogActivity(0, "searched", msg)
}

func bulkSearchMessage(r bulkSearchResult) string {
	if r.Searching == 0 {
		if len(r.Failed) > 0 {
			return fmt.Sprintf("Nothing was searched. %s could not be found in your library.", plural(len(r.Failed), "title"))
		}
		return "Nothing to search for. Only monitored titles that are missing something, and already out, are searched."
	}
	msg := fmt.Sprintf("Searching for %s now. Anything found shows up in Activity.", plural(r.Searching, "title"))
	if r.LeftOut > 0 {
		msg += fmt.Sprintf(" That is the most it searches at once. The other %d are left to the automatic search.", r.LeftOut)
	}
	if r.Skipped > 0 {
		msg += fmt.Sprintf(" %d did not need a search.", r.Skipped)
	}
	return msg
}
