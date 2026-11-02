package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/queue"
)

type grabRequest struct {
	ReleaseTitle string `json:"releaseTitle"`
	DownloadURL  string `json:"downloadUrl"`
	SizeBytes    int64  `json:"sizeBytes"`
	Protocol     string `json:"protocol,omitempty"` // "usenet" (default) or "torrent"
	// ReplaceQueueID names the download that is in the way: it is cancelled
	// and this release grabbed in its place (see replaceBlocking).
	ReplaceQueueID int64 `json:"replaceQueueId,omitempty"`
	// Replace says the person picked this release by hand and agreed to it
	// taking the place of the file the title (or each episode it covers) has:
	// the import then swaps the new file in whatever "Better versions" and "If
	// a file already exists" say, and the old file goes to the recycle bin.
	// Left out, the grab behaves like an automatic one.
	Replace bool `json:"replace,omitempty"`
}

func (r grabRequest) protocol() indexers.Protocol {
	if indexers.Protocol(r.Protocol) == indexers.ProtocolTorrent {
		return indexers.ProtocolTorrent
	}
	return indexers.ProtocolUsenet
}

// handleGrab queues a release for a movie and starts the download and import
// in the background. It returns straight away, so the page can follow the
// progress through /api/queue.
func (s *Server) handleGrab(w http.ResponseWriter, r *http.Request) {
	movieID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid movie ID.")
		return
	}
	movie, err := s.MovieRepo.Get(movieID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That movie isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var req grabRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if req.ReleaseTitle == "" || req.DownloadURL == "" {
		writeError(w, http.StatusBadRequest, "releaseTitle and downloadUrl are required")
		return
	}
	if !s.checkGrabURL(w, r, &req.DownloadURL) {
		return
	}

	if req.ReplaceQueueID > 0 {
		replaceMu.Lock()
		defer replaceMu.Unlock()
		if err := s.replaceBlocking(req.ReplaceQueueID, func() error { return s.checkMovieNotDownloading(movie.ID) }); err != nil {
			writeGrabError(w, err, http.StatusInternalServerError)
			return
		}
	}
	queueID, err := s.grabMovieOpts(movie, req.ReleaseTitle, req.DownloadURL, req.SizeBytes, req.protocol(), grabPicked, grabOpts{byHand: req.Replace})
	if err != nil {
		writeGrabError(w, err, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]int64{"queueId": queueID})
}

// handleSearchGrab downloads a release straight from the search results, with
// no need to add the title to the library first. It resolves the release to a TMDB
// movie automatically (creating the library entry if this is the first
// time it's been grabbed) and then runs the same pipeline as
// handleGrab.
func (s *Server) handleSearchGrab(w http.ResponseWriter, r *http.Request) {
	var req grabRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if req.ReleaseTitle == "" || req.DownloadURL == "" {
		writeError(w, http.StatusBadRequest, "releaseTitle and downloadUrl are required")
		return
	}
	if !s.checkGrabURL(w, r, &req.DownloadURL) {
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "Add your TMDB API key first (Settings > Info, lists and subtitles > Movie and show info).")
		return
	}

	release := parser.Parse(req.ReleaseTitle)
	if release.Title == "" {
		writeError(w, http.StatusBadRequest, "Couldn't work out a title from that release name.")
		return
	}
	if release.Season > 0 || release.Special {
		if !s.requireModule(w, moduleTV) {
			return
		}
		s.grabFromSearchTV(w, r, req, release)
		return
	}
	if !s.requireModule(w, moduleMovies) {
		return
	}
	candidates, err := s.TMDB().SearchMovies(r.Context(), release.Title)
	if err != nil {
		writeUpstreamError(w, "look up that release", err)
		return
	}
	match := bestTMDBMatch(candidates, release.Title, release.Year)
	if match == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Couldn't find a match for %q in the movie database.", release.Title))
		return
	}

	if _, inLibrary, err := s.MovieRepo.GetByTMDBID(match.TMDBID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if !inLibrary && !s.mayAddNew(w, r, auth.PermMovies) {
		return
	}
	userID, _ := requester(r)
	movie, err := s.findOrAddMovie(*match, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	queueID, err := s.grabMovieOpts(movie, req.ReleaseTitle, req.DownloadURL, req.SizeBytes, req.protocol(), grabPicked, grabOpts{byHand: req.Replace})
	if err != nil {
		writeGrabError(w, err, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]int64{"movieId": movie.ID, "queueId": queueID})
}

// grabRelease enqueues a release for movie and launches the download/
// organize pipeline in the background. Shared by handleGrab,
// handleSearchGrab, and internal/api/automation.go's hunt/RSS loops so
// there's exactly one place that decides what "grabbing something" means.
func (s *Server) grabRelease(movie library.Movie, releaseTitle, downloadURL string, sizeBytes int64, protocol indexers.Protocol) (int64, error) {
	return s.grabMovie(movie, releaseTitle, downloadURL, sizeBytes, protocol, grabPicked)
}

// grabMovie is grabRelease with a kind of grab. Everything except a release a
// person picked is refused with errAlreadyGrabbed while another download for
// the movie is still running (a person grabbing by hand is always obeyed).
// The release goes into the download line; it starts when a place is free.
func (s *Server) grabMovie(movie library.Movie, releaseTitle, downloadURL string, sizeBytes int64, protocol indexers.Protocol, kind grabKind) (int64, error) {
	return s.grabMovieReplacing(movie, releaseTitle, downloadURL, sizeBytes, protocol, kind, "")
}

// grabMovieReplacing is grabMovie for a release that is meant to take the
// place of the movie's file at replaces ("" for an ordinary grab): the queue
// item remembers that path, and the import swaps the new file in and removes
// the old one only once the new one is in the library.
func (s *Server) grabMovieReplacing(movie library.Movie, releaseTitle, downloadURL string, sizeBytes int64, protocol indexers.Protocol, kind grabKind, replaces string) (int64, error) {
	return s.grabMovieOpts(movie, releaseTitle, downloadURL, sizeBytes, protocol, kind, grabOpts{replaces: replaces})
}

// grabOpts are the extras of a grab that is meant to replace a file.
type grabOpts struct {
	// replaces is the path of the movie's file a smaller version takes the
	// place of ("" for an ordinary grab).
	replaces string
	// byHand says a person picked the release and agreed to it replacing the
	// file the title has (see grabRequest.Replace).
	byHand bool
}

// grabMovieOpts is grabMovieReplacing with the extras in one place.
func (s *Server) grabMovieOpts(movie library.Movie, releaseTitle, downloadURL string, sizeBytes int64, protocol indexers.Protocol, kind grabKind, o grabOpts) (int64, error) {
	if protocol == indexers.ProtocolTorrent && !s.torrentReleasesOn() {
		return 0, errTorrentsDisabled
	}
	if dest, err := s.buildDestPath(s.movieRootFor(movie), movie.Title, movie.Year, movie.TMDBID, releaseTitle, "check.mkv"); err == nil {
		if err := destWritable(filepath.Dir(dest)); err != nil {
			return 0, err
		}
	}
	s.grabMu.Lock()
	if !kind.refusesDuplicates() {
		// One download per movie: a grab by hand is refused while another one
		// for the movie is still running. Checked with the lock held, so two
		// clicks at once cannot both get through.
		if err := s.checkMovieNotDownloading(movie.ID); err != nil {
			s.grabMu.Unlock()
			return 0, err
		}
	}
	if kind.refusesDuplicates() {
		if err := s.grabRefused(kind, releaseTitle, downloadURL); err != nil {
			s.grabMu.Unlock()
			return 0, err
		}
		if err := s.claimMovie(movie.ID); err != nil {
			s.grabMu.Unlock()
			return 0, err
		}
		// The movie was missing when this run looked, but the run may have been
		// searching for a while: if it has been downloaded (or a download
		// started) since, there is nothing left to fill in.
		if movie.Status == library.StatusMissing {
			if now, err := s.MovieRepo.Get(movie.ID); err == nil && now.Status != library.StatusMissing {
				s.grabMu.Unlock()
				return 0, errAlreadyGrabbed
			}
		}
	}
	queueID, err := s.QueueRepo.Enqueue(queue.Item{
		MovieID: movie.ID, ReleaseTitle: releaseTitle, NZBURL: downloadURL, SizeBytes: sizeBytes, Protocol: queue.Protocol(protocol), Priority: kind.priority(), ReplacesFile: o.replaces, ReplaceByHand: o.byHand && kind == grabPicked,
	})
	if err == nil {
		// From here the movie is taken, as an episode or an album is, even while
		// its download waits for a free place.
		_ = s.MovieRepo.SetStatus(movie.ID, library.StatusDownloading, "", "")
		s.forgetHeldMovie(movie.ID) // nothing left to wait for
	}
	s.grabMu.Unlock()
	if err != nil {
		return 0, err
	}
	grabbedMessage := s.grabbedMessage(releaseTitle, movie.Title)
	_ = s.QueueRepo.LogActivity(movie.ID, "grabbed", grabbedMessage)
	grabbedItem := s.localizedMovieItem(movie)
	grabbedItem.Quality, grabbedItem.SizeBytes, grabbedItem.Release = string(quality.Classify(parser.Parse(releaseTitle))), sizeBytes, releaseTitle
	s.movieReleaseDetails(&grabbedItem, movie, releaseTitle, downloadURL)
	s.notifyItem("grabbed", grabbedItem)

	s.dispatch.Kick()
	return queueID, nil
}

// bestTMDBMatch picks the search result that has the release's title (its
// English or its original one), preferring the one from the release's year.
// When none is named like the release it returns nil rather than guess: a
// different film would be added to the library and the file imported under
// the wrong name.
func bestTMDBMatch(candidates []metadata.Movie, title string, year int) *metadata.Movie {
	var first *metadata.Movie
	for i := range candidates {
		if !sameTitle(title, candidates[i].Title) && !sameTitle(title, candidates[i].OriginalTitle) {
			continue
		}
		if year != 0 && candidates[i].Year() == year {
			return &candidates[i]
		}
		if first == nil {
			first = &candidates[i]
		}
	}
	return first
}

// findOrAddMovie returns the library movie for match, adding it (as added by
// userID) when it is not there yet. Its genres are left to the background
// genre job: a search result only carries genre ids.
func (s *Server) findOrAddMovie(match metadata.Movie, userID int64) (library.Movie, error) {
	existing, err := s.MovieRepo.List()
	if err != nil {
		return library.Movie{}, err
	}
	for _, m := range existing {
		if m.TMDBID == match.TMDBID {
			return m, nil
		}
	}
	return s.MovieRepo.Add(library.Movie{
		TMDBID: match.TMDBID, Title: match.Title, Year: match.Year(), Overview: match.Overview,
		PosterPath: match.PosterPath, Monitored: true, ReleaseDate: match.ReleaseDate, NoUpgrade: true, AddedBy: userID,
		RootPath: s.newMovieFolder(),
	})
}
