package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/quality"
)

// "Replace with a smaller version": a title was put on a lower quality
// profile (4K to 1080p, say) and its big file should give way to a smaller
// release that the profile accepts. Nothing is deleted first. The release is
// searched for and grabbed like any other; its queue item remembers the file
// it is meant to replace, and the import (see runPipeline) removes that file
// only after the new one is in the library. If nothing is found, or the
// download or import fails, the old file stays.

// smallerEnough says a release of size bytes is at least a tenth smaller than
// the file of current bytes. A release that is only a little smaller is not
// worth a download.
func smallerEnough(size, current int64) bool {
	return size > 0 && current > 0 && size*10 < current*9
}

// pickSmallerResult returns the best release the profile accepts that is
// smaller than the current file and not of a higher quality than it: from a 4K
// file on a 1080p profile, the best 1080p release that is smaller. A file
// whose quality is not known is only compared by size. Returns nil when there
// is none.
func pickSmallerResult(results []indexers.Result, profile quality.Profile, currentTier quality.Tier, currentSize int64, wantYear int) *indexers.Result {
	var smaller []indexers.Result
	for _, res := range results {
		if !smallerEnough(res.SizeBytes, currentSize) {
			continue
		}
		if tierKnown(string(currentTier)) && profile.TierRank(quality.Classify(res.Parsed())) > profile.TierRank(currentTier) {
			continue
		}
		smaller = append(smaller, res)
	}
	return pickBestFor(smaller, profile, wantYear)
}

// smallerVersionPolicy is the import policy for a download that replaces the
// file at replaces. It only applies while the movie still has that file
// (current): a title whose file changed in the meantime is imported the
// ordinary way, which never swaps in a lower quality. When the new file gets
// the old file's name it overwrites it in place (the import stages the new
// file beside the old one and renames it over it, so the old one is never gone
// before the new one is whole); under another name the old file is removed
// afterwards. The last result says the old file is to be removed.
func smallerVersionPolicy(replaces, current, dest string, policy organizer.ConflictPolicy, isBetter func() bool) (organizer.ConflictPolicy, func() bool, bool) {
	if replaces == "" || current == "" || filepath.Clean(replaces) != filepath.Clean(current) {
		return policy, isBetter, false
	}
	if filepath.Clean(dest) == filepath.Clean(current) {
		return organizer.ConflictOverwrite, nil, true
	}
	return policy, isBetter, true
}

// shrinkProblem is why a movie cannot be replaced with a smaller version, "" if it can.
// It also returns the file and its size.
func shrinkProblem(m library.Movie) (path string, size int64, problem string) {
	switch {
	case m.Status == library.StatusDownloading:
		return "", 0, "This movie is already downloading."
	case m.Status != library.StatusDownloaded || m.FilePath == "":
		return "", 0, "This movie has no file to replace."
	}
	info, err := os.Stat(m.FilePath)
	if err != nil || !info.Mode().IsRegular() {
		return "", 0, "Mediarium can't find this movie's file. Use Rescan files first."
	}
	return m.FilePath, info.Size(), ""
}

// replaceWithSmaller searches for a smaller release of the movie's file and
// grabs the best one. It reports whether a download was started, and the
// sentence to tell the person.
func (s *Server) replaceWithSmaller(ctx context.Context, m library.Movie, instances []indexers.Instance, profiles profileSet, blocked map[string]bool) (bool, string, error) {
	path, size, problem := shrinkProblem(m)
	if problem != "" {
		return false, problem, errShrinkRefused
	}
	profile := s.profileForMovie(profiles, m)
	sources := s.sourcesFor(m.SourcePref)
	m = s.movieWithAltTitles(ctx, m)
	outcomes := s.searchMovieTitles(ctx, instances, m, nil)
	found := s.limitMovieResults(ctx, m, profile, false, filterSources(matchingResults(dropBlocked(s.autoCandidates(ctx, indexers.MergeResults(outcomes)), blocked, blocklist.Scope{MovieID: m.ID}), m), sources))
	best := pickSmallerResult(found, profile, quality.Tier(m.Quality), size, m.Year)
	if best == nil {
		return false, "Nothing smaller that the quality profile accepts was found. The file you have stays.", nil
	}
	if _, err := s.grabMovieReplacing(m, best.Title, best.DownloadURL, best.SizeBytes, best.Protocol, grabPicked, path); err != nil {
		return false, "", err
	}
	return true, "Started downloading a smaller version. Your current file stays until the new one is in your library.", nil
}

// errShrinkRefused marks a movie that cannot be replaced (see shrinkProblem).
var errShrinkRefused = errors.New("cannot replace this movie's file")

// handleMovieReplaceSmaller starts the replacement for one movie.
func (s *Server) handleMovieReplaceSmaller(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid movie ID.")
		return
	}
	m, err := s.MovieRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That movie isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, _, problem := shrinkProblem(m); problem != "" {
		writeError(w, http.StatusConflict, problem)
		return
	}
	instances, ok := s.hasIndexers(w)
	if !ok {
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	started, message, err := s.replaceWithSmaller(r.Context(), m, instances, profiles, s.searchSkipKeys())
	if err != nil {
		if errors.Is(err, errShrinkRefused) {
			writeError(w, http.StatusConflict, message)
			return
		}
		writeGrabError(w, err, http.StatusInternalServerError)
		return
	}
	grabbed := 0
	if started {
		grabbed = 1
	}
	writeJSON(w, http.StatusOK, searchNowResponse{Grabbed: grabbed, Message: message})
}

// handleBulkReplaceSmaller starts the replacement for the movies of a
// selection. Like the bulk search it answers at once and works through the
// titles one after the other in the background, at most maxBulkSearch of them.
// Shows are left out: only movies are replaced.
func (s *Server) handleBulkReplaceSmaller(w http.ResponseWriter, r *http.Request) {
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
	for _, ref := range refs {
		if ref.Kind != library.KindMovie {
			res.Skipped++
			continue
		}
		m, err := s.MovieRepo.Get(ref.ID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			res.Failed = append(res.Failed, bulkFailure{Kind: ref.Kind, ID: ref.ID, Reason: reasonNotFound})
		case err != nil:
			res.Failed = append(res.Failed, bulkFailure{Kind: ref.Kind, ID: ref.ID, Reason: plainerror.Message(err)})
		default:
			if _, _, problem := shrinkProblem(m); problem != "" {
				res.Skipped++
			} else {
				movies = append(movies, m)
			}
		}
	}
	if len(movies) > maxBulkSearch {
		res.LeftOut = len(movies) - maxBulkSearch
		movies = movies[:maxBulkSearch]
	}
	res.Searching = len(movies)
	res.Message = bulkReplaceMessage(res)
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
		s.runBulkReplaceSmaller(movies)
	})
	writeJSON(w, http.StatusAccepted, res)
}

// runBulkReplaceSmaller works through the movies and notes the outcome in Activity.
func (s *Server) runBulkReplaceSmaller(movies []library.Movie) {
	ctx, cancel := context.WithTimeout(context.Background(), bulkSearchTimeout)
	defer cancel()
	instances, err := s.searchableIndexers()
	if err != nil || len(instances) == 0 {
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		log.Printf("replace with smaller: load quality profiles: %v", err)
		return
	}
	blocked := s.searchSkipKeys()
	started := 0
	for _, m := range movies {
		if ctx.Err() != nil {
			break
		}
		// The movie may have changed since the request: look again.
		if now, err := s.MovieRepo.Get(m.ID); err == nil {
			m = now
		}
		ok, _, err := s.replaceWithSmaller(ctx, m, instances, profiles, blocked)
		if err != nil && !errors.Is(err, errShrinkRefused) {
			log.Printf("replace with smaller: %q: %v", m.Title, err)
		}
		if ok {
			started++
		}
	}
	msg := fmt.Sprintf("Looked for a smaller version of %s: nothing suitable was found. The files you have stay.", plural(len(movies), "movie"))
	if started > 0 {
		msg = fmt.Sprintf("Looked for a smaller version of %s and started %s. Each file stays until its new one is in your library.", plural(len(movies), "movie"), plural(started, "download"))
	}
	_ = s.QueueRepo.LogActivity(0, "searched", msg)
}

func bulkReplaceMessage(r bulkSearchResult) string {
	if r.Searching == 0 {
		return "Nothing to replace. Only movies that have a file can be replaced with a smaller version."
	}
	msg := fmt.Sprintf("Looking for a smaller version of %s now. Anything found shows up in Activity, and each file stays until its new one is in your library.", plural(r.Searching, "movie"))
	if r.LeftOut > 0 {
		msg += fmt.Sprintf(" That is the most it does at once. The other %d are left out: run it again for them.", r.LeftOut)
	}
	if r.Skipped > 0 {
		msg += fmt.Sprintf(" %d could not be replaced (shows, or movies without a file).", r.Skipped)
	}
	return msg
}
