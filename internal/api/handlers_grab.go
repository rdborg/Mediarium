package api

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/ryanborg/mediarium/internal/indexers"
	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/metadata"
	"github.com/ryanborg/mediarium/internal/parser"
	"github.com/ryanborg/mediarium/internal/queue"
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

// handleGrab is where "grab" in the Phase 1 exit criteria
// ("search, grab, download, and organize a movie start to finish") starts:
// it enqueues the release and kicks off the download/import pipeline in
// the background, returning immediately so the UI can poll /api/queue.
func (s *Server) handleGrab(w http.ResponseWriter, r *http.Request) {
	movieID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid movie id")
		return
	}
	movie, err := s.MovieRepo.Get(movieID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "movie not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var req grabRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
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

// handleSearchGrab is the "grab" action straight from the unified search
// results (search has an inline grab action, no separate
// add-to-library step required first). It resolves the release to a TMDB
// movie automatically (creating the library entry if this is the first
// time it's been grabbed) and then runs the same pipeline as
// handleGrab.
func (s *Server) handleSearchGrab(w http.ResponseWriter, r *http.Request) {
	var req grabRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
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
		writeError(w, http.StatusPreconditionFailed, "TMDB API key not configured yet — set it in Settings")
		return
	}

	release := parser.Parse(req.ReleaseTitle)
	if release.Title == "" {
		writeError(w, http.StatusBadRequest, "could not extract a title from the release name")
		return
	}
	if release.Season > 0 {
		s.grabFromSearchTV(w, r, req, release)
		return
	}
	candidates, err := s.TMDB().SearchMovies(r.Context(), release.Title)
	if err != nil {
		writeError(w, http.StatusBadGateway, "look up release on TMDB: "+err.Error())
		return
	}
	match := bestTMDBMatch(candidates, release.Year)
	if match == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no TMDB match found for %q", release.Title))
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
	// One download per movie: a grab by hand is refused while another one
	// for the movie is still running (automation skips it on its own).
	if err := s.checkMovieNotDownloading(movie.ID); err != nil {
		return 0, err
	}
	return s.grabMovie(movie, releaseTitle, downloadURL, sizeBytes, protocol, false)
}

// grabMovie is grabRelease with auto set for automation's grabs, which are
// refused with errAlreadyGrabbed while another download for the movie is
// still running (a person grabbing by hand is always obeyed).
func (s *Server) grabMovie(movie library.Movie, releaseTitle, downloadURL string, sizeBytes int64, protocol indexers.Protocol, auto bool) (int64, error) {
	if protocol == indexers.ProtocolTorrent && !s.torrentsEnabled() {
		return 0, errTorrentsDisabled
	}
	s.grabMu.Lock()
	if auto {
		if err := s.claimMovie(movie.ID); err != nil {
			s.grabMu.Unlock()
			return 0, err
		}
	}
	queueID, err := s.QueueRepo.Enqueue(queue.Item{
		MovieID: movie.ID, ReleaseTitle: releaseTitle, NZBURL: downloadURL, SizeBytes: sizeBytes, Protocol: queue.Protocol(protocol),
	})
	s.grabMu.Unlock()
	if err != nil {
		return 0, err
	}
	grabbedMessage := "Grabbed \"" + releaseTitle + "\" for " + movie.Title
	_ = s.QueueRepo.LogActivity(movie.ID, "grabbed", grabbedMessage)
	s.notifyEvent("grabbed", movie.Title, grabbedMessage)

	s.background(func() {
		if err := s.runPipeline(queueID, movie.ID, movie.Title, movie.Year, movie.TMDBID, releaseTitle, downloadURL, protocol); err != nil {
			log.Printf("api: pipeline for queue item %d failed: %v", queueID, err)
		}
	})
	return queueID, nil
}

// bestTMDBMatch picks the first search result matching year (when the
// release name had one), falling back to the plain first result — good
// enough for Phase 1's automatic search-to-grab resolution; a manual
// re-match UI is a reasonable Phase 2/3 add if this heuristic guesses
// wrong often enough to matter.
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
		PosterPath: match.PosterPath, Monitored: true, ReleaseDate: match.ReleaseDate, AddedBy: userID,
	})
}
