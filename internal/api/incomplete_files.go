package api

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/mediainfo"
)

// Files already in the library that look incomplete: a video whose saved
// length (read from its header by the file details job) is under half of what
// the movie or episode should be. These are the fakes and cut-off files that
// were imported before the length check existed, or while it was off. The
// Library can filter on them, the movie and episode pages say so, and an
// administrator can delete them and search again.
//
// A file that is an archive rather than a video (a release that was never
// unpacked, saved under a video's name) can't be played at all, so it is in
// the same list, the same filter and the same "delete and search again".

// shortFileNote says how short a file is, or that it isn't a video at all.
type shortFileNote struct {
	Minutes  int    `json:"minutes"`            // the file's length, at least 1; 0 for an archive
	Expected int    `json:"expected,omitempty"` // the length it should have; left out when TMDB doesn't say
	Archive  string `json:"archive,omitempty"`  // "rar", "zip" or "7z" when the file is an archive, not a video
}

// incompleteEntry is one library file that looks cut off.
type incompleteEntry struct {
	library.FileLength
	note shortFileNote
}

// incompleteSet is every such file, found through the saved file details.
type incompleteSet struct {
	at       time.Time
	gen      int64
	byPath   map[string]incompleteEntry
	movies   map[int64]incompleteEntry   // by movie id
	episodes map[int64]incompleteEntry   // by episode id
	series   map[int64][]incompleteEntry // the entries of each show's episodes
}

// shortCandidateSeconds is the longest a file is looked at for when its title's
// runtime isn't known: the movie limit of mediainfo.TooShort.
const shortCandidateSeconds = 10 * 60

func entryNote(f library.FileLength) shortFileNote {
	if f.Archive != "" {
		return shortFileNote{Archive: f.Archive}
	}
	return shortFileNote{Minutes: lengthMinutes(f.Seconds), Expected: int(math.Round(f.Expected))}
}

// reason is what the blocklist keeps for the release the file came from.
func (e incompleteEntry) reason() string {
	if e.Archive != "" {
		return notVideoReason
	}
	return incompleteReason
}

// why ends the activity line about the deleted file.
func (e incompleteEntry) why() string {
	if e.Archive != "" {
		return fmt.Sprintf("because it is a %s archive, not a video", strings.ToUpper(e.Archive))
	}
	return "because it looks incomplete"
}

// incompleteNow returns the files that look incomplete, kept for a few seconds
// and renewed when a file's details or a runtime were saved since.
func (s *Server) incompleteNow() *incompleteSet {
	st := &s.mediaInfo
	cur := st.cache.incomplete()
	if cur != nil && cur.gen == st.gen.Load() {
		if time.Since(cur.at) < mediaSummaryTTL {
			return cur
		}
		if time.Since(cur.at) < mediaSummaryMaxStale { // renewed in the background, as mediaSummaryNow does
			st.cache.renew(&st.cache.renewingIncomplete, func() { s.buildIncomplete() })
			return cur
		}
	}
	return s.buildIncomplete()
}

// buildIncomplete reads the list afresh (unless another caller has just done
// so) and keeps it.
func (s *Server) buildIncomplete() *incompleteSet {
	st := &s.mediaInfo
	st.mu.Lock()
	defer st.mu.Unlock()
	gen := st.gen.Load()
	if cur := st.cache.incomplete(); cur != nil && cur.gen == gen && time.Since(cur.at) < mediaSummaryTTL {
		return cur
	}
	out := &incompleteSet{at: time.Now(), gen: gen, byPath: map[string]incompleteEntry{},
		movies: map[int64]incompleteEntry{}, episodes: map[int64]incompleteEntry{}, series: map[int64][]incompleteEntry{}}
	files, err := s.MovieRepo.ShortFileCandidates(shortCandidateSeconds)
	if err != nil {
		slog.Warn("incomplete files: couldn't list the short files", "err", err)
	}
	for _, f := range files {
		kind := mediainfo.KindMovie
		if f.Episode {
			kind = mediainfo.KindEpisode
			if f.Season == 0 {
				f.Expected = 0 // specials are often short: only the lowest limit counts
			}
		}
		if !mediainfo.TooShort(f.Seconds, f.Expected, kind) {
			continue
		}
		out.add(f)
	}
	archives, err := s.MovieRepo.ArchiveFiles(mediainfo.ArchiveContainers())
	if err != nil {
		slog.Warn("incomplete files: couldn't list the archives", "err", err)
	}
	for _, f := range archives {
		out.add(f)
	}
	st.cache.setIncomplete(out)
	return out
}

func (set *incompleteSet) add(f library.FileLength) {
	if _, dup := set.byPath[f.Path]; dup {
		return
	}
	e := incompleteEntry{FileLength: f, note: entryNote(f)}
	set.byPath[f.Path] = e
	if f.Episode {
		set.episodes[f.ID] = e
		set.series[f.SeriesID] = append(set.series[f.SeriesID], e)
	} else {
		set.movies[f.ID] = e
	}
}

// shortFileOf is the note for the file at path, nil when it is fine or not known.
func (s *Server) shortFileOf(path string) *shortFileNote {
	if path == "" {
		return nil
	}
	if e, ok := s.incompleteNow().byPath[path]; ok {
		n := e.note
		return &n
	}
	return nil
}

// redoIncompleteRequest names what to delete and search again. A show stands
// for all of its files that look incomplete.
type redoIncompleteRequest struct {
	MovieIDs   []int64 `json:"movieIds"`
	SeriesIDs  []int64 `json:"seriesIds"`
	EpisodeIDs []int64 `json:"episodeIds"`
}

type redoIncompleteAnswer struct {
	Files     int `json:"files"`     // files deleted
	Titles    int `json:"titles"`    // movies and episodes that are wanted again
	Searching int `json:"searching"` // titles a search was started for
	Skipped   int `json:"skipped"`   // asked for, but no longer incomplete (or gone)
}

// maxRedoSearches is how many searches one request starts; the automatic
// search does the rest.
const maxRedoSearches = 25

// handleRedoIncomplete deletes the library files that look incomplete among
// the given movies, shows and episodes, blocklists the release each one came
// from when it is known, and searches for another. Every file is checked again
// here: only one that still looks incomplete, inside a library folder, is touched.
func (s *Server) handleRedoIncomplete(w http.ResponseWriter, r *http.Request) {
	var req redoIncompleteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid request.")
		return
	}
	if len(req.MovieIDs)+len(req.SeriesIDs)+len(req.EpisodeIDs) == 0 {
		writeError(w, http.StatusBadRequest, "Choose at least one movie, show or episode.")
		return
	}
	set := s.incompleteNow()
	var ans redoIncompleteAnswer
	var searches []func()

	for _, id := range dedupe(req.MovieIDs) {
		e, ok := set.movies[id]
		m, err := s.MovieRepo.Get(id)
		if !ok || err != nil || m.Status != library.StatusDownloaded || m.FilePath != e.Path || !s.inLibrary(m.FilePath) {
			ans.Skipped++
			continue
		}
		s.blocklistIncomplete(m.ReleaseTitle, e.reason(), id, 0)
		n, trashed, err := s.discardFileWithSidecars(s.fileRoot(s.movieRootFor(m), m.FilePath), m.FilePath, movieLabel(m))
		if err != nil {
			writeError(w, http.StatusConflict, fmt.Sprintf("Couldn't delete the file of %s: %s", movieLabel(m), err.Error()))
			return
		}
		if err := s.MovieRepo.ClearMovieFile(id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = s.QueueRepo.LogActivity(id, "removed", fmt.Sprintf("%s: its file %s was deleted%s %s. The movie is wanted again.", movieLabel(m), filepath.Base(m.FilePath), binNote(trashed), e.why()))
		ans.Files += n
		ans.Titles++
		searches = append(searches, func() { s.searchMovieInBackground(id) })
	}

	// The episodes asked for, and every incomplete episode of the shows asked for.
	wantEps := map[int64][]int64{} // show -> episode ids
	for _, id := range dedupe(req.EpisodeIDs) {
		if e, ok := set.episodes[id]; ok {
			wantEps[e.SeriesID] = append(wantEps[e.SeriesID], id)
		} else {
			ans.Skipped++
		}
	}
	for _, sid := range dedupe(req.SeriesIDs) {
		for _, e := range set.series[sid] {
			if !slices.Contains(wantEps[sid], e.ID) {
				wantEps[sid] = append(wantEps[sid], e.ID)
			}
		}
	}
	singles := len(req.SeriesIDs) == 0 // only single episodes asked for: search just those
	for sid, ids := range wantEps {
		series, err := s.MovieRepo.GetSeries(sid)
		if err != nil {
			ans.Skipped += len(ids)
			continue
		}
		var eps []library.Episode
		for _, id := range ids {
			ep, err := s.MovieRepo.GetEpisodeByID(id)
			if err != nil || ep.Status != library.StatusDownloaded || ep.FilePath != set.episodes[id].Path || !s.inLibrary(ep.FilePath) {
				ans.Skipped++
				continue
			}
			eps = append(eps, ep)
		}
		if len(eps) == 0 {
			continue
		}
		for _, ep := range eps {
			s.blocklistIncomplete(ep.ReleaseTitle, set.episodes[ep.ID].reason(), 0, sid)
		}
		done, err := s.deleteEpisodeFiles(series, eps)
		ans.Files += done.Files
		ans.Titles += done.Episodes
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if singles {
			for _, ep := range eps {
				sc := tvScope{seriesID: sid, season: ep.Season, episode: ep.Episode, specials: ep.Season == 0, force: true}
				searches = append(searches, func() { s.searchEpisodeAgain(sc) })
			}
		} else {
			searches = append(searches, func() { s.searchSeriesInBackground(sid) })
		}
	}

	if len(searches) > maxRedoSearches {
		searches = searches[:maxRedoSearches]
	}
	ans.Searching = len(searches)
	if len(searches) > 0 {
		s.background(func() {
			for _, search := range searches {
				search()
			}
		})
	}
	s.mediaInfo.gen.Add(1) // the lists look again
	writeJSON(w, http.StatusOK, ans)
}

// searchEpisodeAgain runs the automatic search for one episode, as the
// "Search" button of the episode does.
func (s *Server) searchEpisodeAgain(sc tvScope) {
	instances, err := s.searchableIndexers()
	if err != nil || len(instances) == 0 {
		return
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		return
	}
	s.autoGrabTV(s.targetedTVSearcher(context.Background(), instances, maxTVSearchesPerHunt, true), "search-now", profiles, sc)
}

// blocklistIncomplete blocklists the release a file came from, when the
// history knows it, so the same one isn't grabbed again.
func (s *Server) blocklistIncomplete(release, reason string, movieID, seriesID int64) {
	if release == "" {
		return
	}
	if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: release, Reason: reason, MovieID: movieID, SeriesID: seriesID}); err != nil {
		slog.Warn("incomplete files: couldn't blocklist a release", "err", err)
		return
	}
	_ = s.QueueRepo.LogItemActivity(movieID, seriesID, "blocklisted", fmt.Sprintf("Blocklisted \"%s\": %s", release, reason))
}

// inLibrary says whether path lies inside a movie or TV library folder.
func (s *Server) inLibrary(path string) bool {
	return path != "" && rootContaining(s.videoRoots(), path) != ""
}

func dedupe(ids []int64) []int64 {
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}
