package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/subtitles"
)

type subtitleResultPayload struct {
	FileID   int     `json:"fileId"`
	Language string  `json:"language"`
	Release  string  `json:"release"`
	Rating   float64 `json:"rating"`
	// Score is how well the subtitle fits the video file's release (higher
	// is better); results are returned best-first.
	Score float64 `json:"score"`
	// HashMatch: made for this exact video file, so it is in sync.
	HashMatch bool `json:"hashMatch,omitempty"`
}

// errSubtitleItemNotFound means a movie or episode id matches nothing.
var errSubtitleItemNotFound = errors.New("not found")

// loadSubtitleItem loads the movie or episode with the given id.
func (s *Server) loadSubtitleItem(kind string, id int64) (subtitleItem, error) {
	switch kind {
	case "movie":
		m, err := s.MovieRepo.Get(id)
		if errors.Is(err, sql.ErrNoRows) {
			return subtitleItem{}, fmt.Errorf("movie %d: %w", id, errSubtitleItemNotFound)
		}
		if err != nil {
			return subtitleItem{}, err
		}
		return movieSubtitleItem(m), nil
	case "episode":
		ep, err := s.MovieRepo.GetEpisodeByID(id)
		if errors.Is(err, sql.ErrNoRows) {
			return subtitleItem{}, fmt.Errorf("episode %d: %w", id, errSubtitleItemNotFound)
		}
		if err != nil {
			return subtitleItem{}, err
		}
		sr, err := s.MovieRepo.GetSeries(ep.SeriesID)
		if err != nil {
			return subtitleItem{}, err
		}
		return episodeSubtitleItem(sr, ep), nil
	}
	return subtitleItem{}, fmt.Errorf("unknown kind %q", kind)
}

// subtitleItemFor loads the movie or episode named by the path's {id}. It
// writes the error response itself and returns ok=false on failure,
// including with a 409 while subtitles are switched off.
func (s *Server) subtitleItemFor(w http.ResponseWriter, r *http.Request, kind string) (subtitleItem, bool) {
	if s.refuseIfSubtitlesOff(w) {
		return subtitleItem{}, false
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid "+kind+" ID.")
		return subtitleItem{}, false
	}
	it, err := s.loadSubtitleItem(kind, id)
	switch {
	case errors.Is(err, errSubtitleItemNotFound):
		writeError(w, http.StatusNotFound, kind+" not found")
		return subtitleItem{}, false
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
		return subtitleItem{}, false
	}
	return it, true
}

func (s *Server) handleSearchMovieSubtitles(w http.ResponseWriter, r *http.Request) {
	s.searchSubtitles(w, r, "movie")
}

func (s *Server) handleSearchEpisodeSubtitles(w http.ResponseWriter, r *http.Request) {
	s.searchSubtitles(w, r, "episode")
}

// searchSubtitles lists subtitle candidates for one movie or episode in a
// language, best fit for the video file first (subtitles module).
func (s *Server) searchSubtitles(w http.ResponseWriter, r *http.Request, kind string) {
	it, ok := s.subtitleItemFor(w, r, kind)
	if !ok {
		return
	}
	if !s.Subtitles().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "Add your OpenSubtitles API key first (Settings > Info, lists and subtitles > Subtitles).")
		return
	}
	language := r.URL.Query().Get("lang")
	if language == "" {
		language = s.subtitleLanguages()[0]
	}
	q := it.query(language)
	q.MovieHash = videoHash(it.filePath)
	results, err := s.Subtitles().Find(r.Context(), q)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Couldn't search for subtitles: "+err.Error())
		return
	}
	videoName := ""
	if it.filePath != "" {
		videoName = pathBase(it.filePath)
	}
	out := make([]subtitleResultPayload, len(results))
	for i, res := range results {
		out[i] = subtitleResultPayload{
			FileID: res.FileID, Language: res.Language, Release: res.Release, Rating: res.Rating,
			Score: subtitles.Score(res, videoName), HashMatch: res.HashMatch,
		}
	}
	sortSubtitleResults(out)
	writeJSON(w, http.StatusOK, out)
}

type downloadSubtitleRequest struct {
	FileID   int    `json:"fileId"`
	Language string `json:"language"`
}

func (s *Server) handleDownloadMovieSubtitle(w http.ResponseWriter, r *http.Request) {
	s.downloadSubtitle(w, r, "movie")
}

func (s *Server) handleDownloadEpisodeSubtitle(w http.ResponseWriter, r *http.Request) {
	s.downloadSubtitle(w, r, "episode")
}

// downloadSubtitle saves a chosen subtitle next to the video file as
// "<base>.<lang>.srt".
func (s *Server) downloadSubtitle(w http.ResponseWriter, r *http.Request, kind string) {
	it, ok := s.subtitleItemFor(w, r, kind)
	if !ok {
		return
	}
	if it.filePath == "" {
		writeError(w, http.StatusPreconditionFailed, "There's no downloaded file to add a subtitle to yet.")
		return
	}
	if _, err := os.Stat(it.filePath); err != nil {
		writeError(w, http.StatusPreconditionFailed, "The video file is missing. It may have been moved or deleted.")
		return
	}
	var req downloadSubtitleRequest
	if err := decodeJSON(r, &req); err != nil || req.FileID <= 0 {
		writeError(w, http.StatusBadRequest, "Pick a subtitle from the list to download.")
		return
	}
	req.Language = strings.TrimSpace(req.Language)
	if req.Language == "" {
		req.Language = s.subtitleLanguages()[0]
	}
	// The language ends up in the subtitle's file name, so only a plain code
	// like en or pt-BR is accepted.
	if !languageCode.MatchString(req.Language) {
		writeError(w, http.StatusBadRequest, "That isn't a language code Mediarium can use. Pick one from the list.")
		return
	}
	path, err := s.writeSubtitle(r.Context(), it, req.Language, subtitlePick{fileID: req.FileID, chosen: true, videoHash: videoHash(it.filePath)})
	switch {
	case errors.Is(err, subtitles.ErrQuota):
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusBadGateway, "Couldn't download the subtitle: "+err.Error())
		return
	}
	// The file name only: any account may download a subtitle, and the folder
	// on the server is not for them to see.
	writeJSON(w, http.StatusOK, map[string]string{"path": pathBase(path)})
}

type subtitleStatusPayload struct {
	Languages []string `json:"languages"` // configured
	Present   []string `json:"present"`   // configured languages already on disk
}

func (s *Server) handleMovieSubtitleStatus(w http.ResponseWriter, r *http.Request) {
	s.subtitleStatus(w, r, "movie")
}

func (s *Server) handleEpisodeSubtitleStatus(w http.ResponseWriter, r *http.Request) {
	s.subtitleStatus(w, r, "episode")
}

func (s *Server) subtitleStatus(w http.ResponseWriter, r *http.Request, kind string) {
	it, ok := s.subtitleItemFor(w, r, kind)
	if !ok {
		return
	}
	have := languagesOnDisk(it.filePath)
	out := subtitleStatusPayload{Languages: s.subtitleLanguages(), Present: []string{}}
	for _, l := range out.Languages {
		if subtitles.LanguageSatisfied(have, l) {
			out.Present = append(out.Present, l)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type subtitleWantedPayload struct {
	Kind      string   `json:"kind"`
	ID        int64    `json:"id"`
	MovieTMDB int      `json:"tmdbId,omitempty"`
	SeriesID  int64    `json:"seriesId,omitempty"`
	Title     string   `json:"title"`
	Subtitle  string   `json:"subtitle,omitempty"`
	Missing   []string `json:"missing"`
	// Dismissed is set (only with ?includeDismissed=1) on titles marked "no
	// subtitles wanted".
	Dismissed bool `json:"dismissed,omitempty"`
}

// handleSubtitlesWanted lists downloaded items that lack a subtitle in one
// of the configured languages, leaving out titles marked "no subtitles wanted"
// unless ?includeDismissed=1 is given (they are then flagged dismissed). It
// answers 409 while subtitles are switched off.
func (s *Server) handleSubtitlesWanted(w http.ResponseWriter, r *http.Request) {
	if s.refuseIfSubtitlesOff(w) {
		return
	}
	items, err := s.downloadedSubtitleItems()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	includeDismissed := r.URL.Query().Get("includeDismissed") == "1"
	dismissed := s.dismissedSubtitles()
	out := []subtitleWantedPayload{}
	for _, it := range items {
		isDismissed := dismissed[it.ref()]
		if isDismissed && !includeDismissed {
			continue
		}
		if _, err := os.Stat(it.filePath); err != nil {
			continue
		}
		missing := s.missingSubtitleLanguages(it)
		if len(missing) == 0 {
			continue
		}
		p := subtitleWantedPayload{Kind: it.kind, ID: it.id, Title: it.title, Subtitle: it.subtitle, Missing: missing, MovieTMDB: it.tmdbID, Dismissed: isDismissed}
		if it.kind == "episode" {
			if ep, err := s.MovieRepo.GetEpisodeByID(it.id); err == nil {
				p.SeriesID = ep.SeriesID
			}
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSubtitleSweep looks for missing subtitles now, without waiting out the
// pause after a recent failure. It answers 409 while subtitles are switched off.
func (s *Server) handleSubtitleSweep(w http.ResponseWriter, r *http.Request) {
	if s.refuseIfSubtitlesOff(w) {
		return
	}
	if !s.Subtitles().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "Add your OpenSubtitles API key first (Settings > Info, lists and subtitles > Subtitles).")
		return
	}
	n, err := s.subtitleSweep(r.Context(), true, maxSubtitleDownloadsRun)
	msg := "Downloaded " + plural(n, "subtitle") + "."
	if errors.Is(err, subtitles.ErrQuota) {
		msg += " You've reached today's OpenSubtitles download limit, so the rest will be tried tomorrow."
	} else if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"downloaded": n, "message": msg})
}
