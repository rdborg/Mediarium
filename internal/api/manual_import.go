package api

import (
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/libimport"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// Manual import: a video file that Mediarium could not match (a failed
// download's leftovers, a file you copied somewhere yourself) can be pointed
// at a movie or an episode in the library. It is then named and placed in the
// library like any download. The folder to look in is the downloads folder
// unless the person picks another one.

const maxManualFiles = 500

// maxManualWalk is how many files and folders one look may go through, so a
// folder with a whole drive under it can't keep the page waiting.
const maxManualWalk = 100000

// manualMinSize is the smallest file worth listing: smaller ones are samples
// and extras.
const manualMinSize = 20 << 20

type manualFile struct {
	Path       string `json:"path"` // relative to the chosen folder
	Size       int64  `json:"size"`
	Modified   string `json:"modified"`
	Title      string `json:"title,omitempty"` // what the file name suggests
	Year       int    `json:"year,omitempty"`
	Season     int    `json:"season,omitempty"`
	Episode    int    `json:"episode,omitempty"`
	Quality    string `json:"quality,omitempty"`
	Incomplete bool   `json:"incomplete,omitempty"` // it is inside an incomplete downloads folder
}

type manualListPayload struct {
	Folder           string       `json:"folder"`
	DownloadsFolder  string       `json:"downloadsFolder"`
	Files            []manualFile `json:"files"`
	More             bool         `json:"more,omitempty"`             // there were more than were listed
	HiddenIncomplete int          `json:"hiddenIncomplete,omitempty"` // files left out because they are in an incomplete folder
}

// handleManualImportList lists the video files in a folder (the downloads
// folder unless ?folder= names another), biggest first, with what each file's
// name suggests. Files in an incomplete downloads folder are left out unless
// ?incomplete=1 asks for them.
func (s *Server) handleManualImportList(w http.ResponseWriter, r *http.Request) {
	root, err := s.manualFolder(r.URL.Query().Get("folder"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	withIncomplete := r.URL.Query().Get("incomplete") == "1"
	out := manualListPayload{Folder: root, DownloadsFolder: s.downloadsRoot(), Files: []manualFile{}}
	seen := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if r.Context().Err() != nil {
			return r.Context().Err()
		}
		if err != nil {
			return nil
		}
		if seen++; seen > maxManualWalk {
			out.More = true
			return filepath.SkipAll
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || isPartialName(name) || !organizer.IsVideoFile(path) || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = strings.TrimPrefix(filepath.ToSlash(rel), "./")
		if isSampleFile(rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() < manualMinSize { // samples and extras are not worth listing
			return nil
		}
		incomplete := inIncompleteFolder(rel)
		if incomplete && !withIncomplete {
			out.HiddenIncomplete++
			return nil
		}
		guess := parser.Parse(guessName(path))
		f := manualFile{Path: rel, Size: info.Size(), Modified: info.ModTime().UTC().Format(time.RFC3339), Title: guess.Title, Year: guess.Year, Season: guess.Season, Incomplete: incomplete}
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
	Folder     string `json:"folder,omitempty"` // the folder the path is in; empty means the downloads folder
	Path       string `json:"path"`             // relative to that folder
	Mode       string `json:"mode,omitempty"`   // "link" (the default), "copy" or "move"
	Incomplete bool   `json:"incomplete,omitempty"`
	MovieID    int64  `json:"movieId,omitempty"`
	SeriesID   int64  `json:"seriesId,omitempty"`
	Season     int    `json:"season,omitempty"`
	Episode    int    `json:"episode,omitempty"`
}

// handleManualImport brings one file into the library as the movie or episode
// chosen: linked or copied, and for a move the original is removed afterwards.
func (s *Server) handleManualImport(w http.ResponseWriter, r *http.Request) {
	var req manualImportRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	mode, ok := parseManualMode(req.Mode)
	if !ok {
		writeError(w, http.StatusBadRequest, "Choose how the file should come in: hardlink, copy or move.")
		return
	}
	src, err := s.manualSource(req.Folder, req.Path, req.Incomplete)
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
		dest, err := s.buildDestPath(s.movieRootFor(m), m.Title, m.Year, m.TMDBID, guessName(src), src)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		dest = s.keepMovieFolder(m, s.movieRootFor(m), dest)
		if err := s.checkLibraryRoot(dest); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		res, err := manualPlace(src, dest, mode)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Couldn't move the file into your library: %s", err.Error()))
			return
		}
		if res.Skipped {
			writeError(w, http.StatusConflict, fmt.Sprintf("A file is already at %s. Remove it first if this one should take its place.", dest))
			return
		}
		if err := s.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, string(tier), res.DestPath); err == nil {
			s.afterMovieImported(m.ID, guessName(src))
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// The file chosen takes the place of the one the movie had under
		// another name (an older quality), as an automatic upgrade does.
		s.removeReplacedFile(s.fileRoot(s.movieRootFor(m), m.FilePath), m.FilePath, res.DestPath, m.ID, 0, movieLabel(m))
		_ = s.QueueRepo.LogActivity(m.ID, "imported", fmt.Sprintf("%s imported by hand to %s", movieLabel(m), res.DestPath))
		kept := manualFinish(src, res.DestPath, mode)
		s.movieImported(res.DestPath)
		writeJSON(w, http.StatusOK, map[string]any{"path": res.DestPath, "sourceKept": kept})
	case req.SeriesID > 0 && req.Season >= 0 && req.Episode > 0:
		sr, err := s.MovieRepo.GetSeries(req.SeriesID)
		if err != nil {
			writeError(w, http.StatusNotFound, "That show isn't in your library.")
			return
		}
		ep, err := s.MovieRepo.GetEpisode(sr.ID, req.Season, req.Episode)
		if err != nil {
			// The person's own numbering for the show: the season and episode
			// the file's name carries may be what releases call an episode the
			// library lists under another number.
			if ls, le, ok := s.customFor(sr).ToLibrary(req.Season, req.Episode); ok {
				if mapped, merr := s.MovieRepo.GetEpisode(sr.ID, ls, le); merr == nil {
					ep, err = mapped, nil
				}
			}
		}
		if err != nil {
			// An anime's files are often numbered by TheTVDB's seasons, which
			// the movie database does not have. The number counted from the
			// first episode that the file's name carries says which one it is.
			found := false
			if sr.SeriesType == library.SeriesAnime {
				ep, found = s.episodeByAbsolute(sr.ID, filepath.Base(src))
				if !found {
					ep, found = s.episodeByTVDBSeason(sr, req.Season, req.Episode)
				}
			}
			if !found {
				if sr.SeriesType == library.SeriesAnime {
					writeError(w, http.StatusNotFound, fmt.Sprintf("%s has no season %d episode %d. Its episodes are numbered the way the movie database counts them, so pick the season and episode shown on the show's page.", sr.Title, req.Season, req.Episode))
					return
				}
				writeError(w, http.StatusNotFound, fmt.Sprintf("%s has no season %d episode %d.", sr.Title, req.Season, req.Episode))
				return
			}
		}
		dest, err := s.buildTVDestPath(sr, ep, release, guessName(src), src)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := s.checkLibraryRoot(dest); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		res, err := manualPlace(src, dest, mode)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Couldn't move the file into your library: %s", err.Error()))
			return
		}
		if res.Skipped {
			writeError(w, http.StatusConflict, fmt.Sprintf("A file is already at %s. Remove it first if this one should take its place.", dest))
			return
		}
		if err := s.MovieRepo.SetEpisodeStatus(ep.ID, library.StatusDownloaded, string(tier), res.DestPath); err == nil {
			s.afterEpisodeImported(ep.ID, guessName(src))
			s.forgetHeldEpisode(ep.SeriesID, ep.Season, ep.Episode)
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// The same for an episode; a file another episode still uses stays.
		s.removeReplacedFile(s.fileRoot(s.seriesRootFor(sr), ep.FilePath), ep.FilePath, res.DestPath, 0, sr.ID, fmt.Sprintf("%s S%02dE%02d", sr.Title, ep.Season, ep.Episode))
		_ = s.QueueRepo.LogSeriesActivity(sr.ID, "imported", fmt.Sprintf("%s S%02dE%02d imported by hand to %s", sr.Title, ep.Season, ep.Episode, res.DestPath))
		kept := manualFinish(src, res.DestPath, mode)
		s.episodesImported(res.DestPath)
		writeJSON(w, http.StatusOK, map[string]any{"path": res.DestPath, "sourceKept": kept})
	default:
		writeError(w, http.StatusBadRequest, "Choose a movie, or a show with its season and episode.")
	}
}

// episodeByTVDBSeason is the episode of a show that a season and episode
// counted the way TheTVDB (and so Sonarr) counts stands for, by the same rules
// an import uses. Only the one episode, never a guess.
func (s *Server) episodeByTVDBSeason(sr library.Series, season, episode int) (library.Episode, bool) {
	episodes, err := s.MovieRepo.ListEpisodes(sr.ID)
	if err != nil {
		return library.Episode{}, false
	}
	place := library.ImportPlacer(episodes, true, s.importMaps(sr), nil)
	to, numbers, ok := place(library.ImportFile{Season: season, Episodes: []int{episode}})
	if !ok || len(numbers) != 1 {
		return library.Episode{}, false
	}
	ep, err := s.MovieRepo.GetEpisode(sr.ID, to, numbers[0])
	return ep, err == nil
}

// episodeByAbsolute is the episode of a show that a file name's number,
// counted from the first episode, stands for. Only a name with exactly one
// such number counts, and only when the show has that many episodes.
func (s *Server) episodeByAbsolute(seriesID int64, name string) (library.Episode, bool) {
	numbers := libimport.AbsoluteNumbers(name)
	if len(numbers) != 1 {
		return library.Episode{}, false
	}
	episodes, err := s.MovieRepo.ListEpisodes(seriesID)
	if err != nil {
		return library.Episode{}, false
	}
	order := library.AbsoluteOrder(episodes)
	if n := numbers[0]; n >= 1 && n <= len(order) {
		return order[n-1], true
	}
	return library.Episode{}, false
}
