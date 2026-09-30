package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

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

	queueID, err := s.grabRelease(movie, req.ReleaseTitle, req.DownloadURL, req.SizeBytes, req.protocol())
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
		writeError(w, http.StatusPreconditionFailed, "Add your TMDB API key first (Settings > Info, lists and subtitles > Movie info and lists).")
		return
	}

	release := parser.Parse(req.ReleaseTitle)
	if release.Title == "" {
		writeError(w, http.StatusBadRequest, "Couldn't work out a title from that release name.")
		return
	}
	if release.Season > 0 {
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
	match := bestTMDBMatch(candidates, release.Year)
	if match == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Couldn't find a match for %q in the movie database.", release.Title))
		return
	}

	userID, _ := requester(r)
	movie, err := s.findOrAddMovie(*match, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	queueID, err := s.grabRelease(movie, req.ReleaseTitle, req.DownloadURL, req.SizeBytes, req.protocol())
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
	if protocol == indexers.ProtocolTorrent && !s.torrentsEnabled() {
		return 0, errTorrentsDisabled
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
		if s.releaseBlocklisted(releaseTitle) {
			s.grabMu.Unlock()
			return 0, errBlocklisted
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
		MovieID: movie.ID, ReleaseTitle: releaseTitle, NZBURL: downloadURL, SizeBytes: sizeBytes, Protocol: queue.Protocol(protocol), Priority: kind.priority(),
	})
	if err == nil {
		// From here the movie is taken, as an episode or an album is, even while
		// its download waits for a free place.
		_ = s.MovieRepo.SetStatus(movie.ID, library.StatusDownloading, "", "")
	}
	s.grabMu.Unlock()
	if err != nil {
		return 0, err
	}
	grabbedMessage := s.grabbedMessage(releaseTitle, movie.Title)
	_ = s.QueueRepo.LogActivity(movie.ID, "grabbed", grabbedMessage)
	grabbedItem := movieItem(movie)
	grabbedItem.Quality, grabbedItem.SizeBytes, grabbedItem.Release = string(quality.Classify(parser.Parse(releaseTitle))), sizeBytes, releaseTitle
	s.notifyItem("grabbed", grabbedItem)

	s.dispatch.Kick()
	return queueID, nil
}

// bestTMDBMatch picks the first search result matching year (when the
// release name had one), falling back to the plain first result — good
// enough for matching a search result to a title automatically. A screen for
// re-matching by hand could be added if this guesses wrong too often.
func bestTMDBMatch(candidates []metadata.Movie, year int) *metadata.Movie {
	if len(candidates) == 0 {
		return nil
	}
	if year != 0 {
		for i := range candidates {
			if candidates[i].Year() == year {
				return &candidates[i]
			}
		}
	}
	return &candidates[0]
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
	})
}
