package api

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/cleanup"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// Manual import: a video file in the downloads folder that Mediarium could
// not match (a failed download's leftovers, a file you copied there
// yourself) can be pointed at a movie or an episode in the library. It is
// then named and moved into the library like any download.

const maxManualFiles = 500

type manualFile struct {
	Path     string `json:"path"` // relative to the downloads folder
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	Title    string `json:"title,omitempty"` // what the file name suggests
	Year     int    `json:"year,omitempty"`
	Season   int    `json:"season,omitempty"`
	Episode  int    `json:"episode,omitempty"`
	Quality  string `json:"quality,omitempty"`
}

type manualListPayload struct {
	Folder string       `json:"folder"`
	Files  []manualFile `json:"files"`
	More   bool         `json:"more,omitempty"` // there were more than were listed
}

// handleManualImportList lists the video files in the downloads folder,
// biggest first, with what each file's name suggests.
func (s *Server) handleManualImportList(w http.ResponseWriter, r *http.Request) {
	root := s.downloadsRoot()
	out := manualListPayload{Folder: root, Files: []manualFile{}}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || !organizer.IsVideoFile(path) || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() < 20<<20 { // samples and extras are not worth listing
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		rel = strings.TrimPrefix(rel, "./")
		guess := parser.Parse(guessName(path))
		f := manualFile{Path: rel, Size: info.Size(), Modified: info.ModTime().UTC().Format(time.RFC3339), Title: guess.Title, Year: guess.Year, Season: guess.Season}
		if len(guess.Episodes) > 0 {
			f.Episode = guess.Episodes[0]
		}
		if t := quality.Classify(guess); t != quality.TierUnknown {
			f.Quality = string(t)
		}
		out.Files = append(out.Files, f)
		return nil
	})
	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].Size > out.Files[j].Size })
	if len(out.Files) > maxManualFiles {
		out.Files, out.More = out.Files[:maxManualFiles], true
	}
	writeJSON(w, http.StatusOK, out)
}

// guessName is the best name to read a release from: the file's own name,
// or its folder's when the file is called something like "abc123.mkv".
func guessName(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if p := parser.Parse(base); p.Title != "" && (p.Year > 0 || p.Season > 0 || len(base) > 12) {
		return base
	}
	return filepath.Base(filepath.Dir(path))
}

type manualImportRequest struct {
	Path     string `json:"path"`
	MovieID  int64  `json:"movieId,omitempty"`
	SeriesID int64  `json:"seriesId,omitempty"`
	Season   int    `json:"season,omitempty"`
	Episode  int    `json:"episode,omitempty"`
}

// handleManualImport moves one file from the downloads folder into the
// library as the movie or episode chosen.
func (s *Server) handleManualImport(w http.ResponseWriter, r *http.Request) {
	var req manualImportRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	src, err := s.manualSource(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	release := parser.Parse(guessName(src))
	tier := quality.Classify(release)
	switch {
	case req.MovieID > 0:
		m, err := s.MovieRepo.Get(req.MovieID)
		if err != nil {
			writeError(w, http.StatusNotFound, "That movie isn't in your library.")
			return
		}
		dest, err := s.buildDestPath(s.movieHome(m), m.Title, m.Year, m.TMDBID, guessName(src), src)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		res, err := organizer.Import(src, dest, organizer.ConflictSkip, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Couldn't move the file into your library: "+err.Error())
			return
		}
		if res.Skipped {
			writeError(w, http.StatusConflict, fmt.Sprintf("A file is already at %s. Remove it first if this one should take its place.", dest))
			return
		}
		if err := s.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, string(tier), res.DestPath); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = s.QueueRepo.LogActivity(m.ID, "imported", fmt.Sprintf("%s imported by hand to %s", movieLabel(m), res.DestPath))
		s.movieImported(res.DestPath)
		writeJSON(w, http.StatusOK, map[string]any{"path": res.DestPath})
	case req.SeriesID > 0 && req.Season >= 0 && req.Episode > 0:
		sr, err := s.MovieRepo.GetSeries(req.SeriesID)
		if err != nil {
			writeError(w, http.StatusNotFound, "That show isn't in your library.")
			return
		}
		ep, err := s.MovieRepo.GetEpisode(sr.ID, req.Season, req.Episode)
		if err != nil {
			writeError(w, http.StatusNotFound, fmt.Sprintf("%s has no season %d episode %d.", sr.Title, req.Season, req.Episode))
			return
		}
		dest, err := s.buildTVDestPath(sr, ep, release, src)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		res, err := organizer.Import(src, dest, organizer.ConflictSkip, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Couldn't move the file into your library: "+err.Error())
			return
		}
		if res.Skipped {
			writeError(w, http.StatusConflict, fmt.Sprintf("A file is already at %s. Remove it first if this one should take its place.", dest))
			return
		}
		if err := s.MovieRepo.SetEpisodeStatus(ep.ID, library.StatusDownloaded, string(tier), res.DestPath); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = s.QueueRepo.LogSeriesActivity(sr.ID, "imported", fmt.Sprintf("%s S%02dE%02d imported by hand to %s", sr.Title, ep.Season, ep.Episode, res.DestPath))
		s.episodesImported(res.DestPath)
		writeJSON(w, http.StatusOK, map[string]any{"path": res.DestPath})
	default:
		writeError(w, http.StatusBadRequest, "Choose a movie, or a show with its season and episode.")
	}
}

// manualSource turns a path relative to the downloads folder into the file
// it names, refusing anything outside that folder or not a video file.
func (s *Server) manualSource(rel string) (string, error) {
	root := s.downloadsRoot()
	rel = strings.TrimSpace(rel)
	if rel == "" || filepath.IsAbs(rel) {
		return "", errors.New("Choose a file from the list.")
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	real, err := cleanup.InsideRoot(root, path)
	if err != nil {
		return "", errors.New("That file isn't in the downloads folder.")
	}
	info, err := os.Lstat(real)
	if err != nil || !info.Mode().IsRegular() || !organizer.IsVideoFile(real) {
		return "", errors.New("That isn't a video file in the downloads folder any more.")
	}
	return real, nil
}
