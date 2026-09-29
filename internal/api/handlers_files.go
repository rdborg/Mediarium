package api

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/mediafiles"
)

// libraryRoots are the folders a title's files may be read from.
func (s *Server) libraryRoots() []string {
	return []string{s.moviesRoot(), s.tvRoot()}
}

// fileEntry is one file in a title's folder.
type fileEntry struct {
	Path      string `json:"path"` // relative to the title's folder, "/"-separated
	Size      int64  `json:"size"`
	Modified  string `json:"modified"` // RFC 3339, UTC
	Kind      string `json:"kind"`     // video | subtitle | image | nfo | other
	Main      bool   `json:"main,omitempty"`
	EpisodeID int64  `json:"episodeId,omitempty"`
}

// filesPayload lists a title's folder. Folder is the folder's path relative
// to its library folder (never the full path on the server); it is empty
// when nothing is on disk yet.
type filesPayload struct {
	Folder string      `json:"folder"`
	Files  []fileEntry `json:"files"`
}

// movieFolder finds a library movie's folder on disk.
func (s *Server) movieFolder(m library.Movie) (mediafiles.Folder, error) {
	return mediafiles.ForFile(m.FilePath, s.libraryRoots(), false)
}

// seriesFolder finds a show's folder from its episodes' files (the show
// folder above the season folders). If episodes disagree, the folder most of
// them are in wins. It also returns the episodes, for matching files.
func (s *Server) seriesFolder(seriesID int64) (mediafiles.Folder, []library.Episode, error) {
	episodes, err := s.MovieRepo.ListEpisodes(seriesID)
	if err != nil {
		return mediafiles.Folder{}, nil, fmt.Errorf("list episodes: %w", err)
	}
	counts := map[string]int{}
	folders := map[string]mediafiles.Folder{}
	var lastErr error = fs.ErrNotExist
	for _, ep := range episodes {
		if ep.FilePath == "" {
			continue
		}
		f, err := mediafiles.ForFile(ep.FilePath, s.libraryRoots(), true)
		if err != nil {
			if !errors.Is(lastErr, mediafiles.ErrOutside) {
				lastErr = err
			}
			continue
		}
		counts[f.Dir]++
		folders[f.Dir] = f
	}
	best := ""
	for dir, n := range counts {
		if best == "" || n > counts[best] || (n == counts[best] && dir < best) {
			best = dir
		}
	}
	if best == "" {
		return mediafiles.Folder{}, episodes, lastErr
	}
	return folders[best], episodes, nil
}

// writeFolderError answers a folder lookup failure: nothing on disk is an
// empty listing, a folder outside the library is refused.
func writeFolderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		writeJSON(w, http.StatusOK, filesPayload{Files: []fileEntry{}})
	case errors.Is(err, mediafiles.ErrOutside):
		writeError(w, http.StatusForbidden, "this title's files are not inside the library folders")
	default:
		writeFileReadError(w, err)
	}
}

// writeFileReadError answers an unexpected disk error without the server
// paths it may contain (members must not see them); the log has the detail.
func writeFileReadError(w http.ResponseWriter, err error) {
	slog.Warn("read title files", "err", err)
	writeError(w, http.StatusInternalServerError, "could not read the title's files on disk")
}

// listFolder lists a folder's files, marking the ones the library tracks:
// main maps a relative path to the episode id it holds (0 for a movie file).
func listFolder(w http.ResponseWriter, folder mediafiles.Folder, main map[string]int64) {
	files, err := folder.List()
	if err != nil {
		writeFolderError(w, err)
		return
	}
	out := filesPayload{Folder: folder.Name, Files: make([]fileEntry, len(files))}
	for i, f := range files {
		epID, isMain := main[f.Path]
		out.Files[i] = fileEntry{
			Path: f.Path, Size: f.Size, Modified: f.Modified.Format(time.RFC3339), Kind: string(f.Kind),
			Main: isMain, EpisodeID: epID,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleMovieFiles lists the files in a movie's folder on disk.
func (s *Server) handleMovieFiles(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid movie id")
		return
	}
	m, err := s.MovieRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "movie not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	folder, err := s.movieFolder(m)
	if err != nil {
		writeFolderError(w, err)
		return
	}
	main := map[string]int64{}
	if rel, ok := folder.Rel(m.FilePath); ok {
		main[rel] = 0
	}
	listFolder(w, folder, main)
}

// handleSeriesFiles lists the files in a show's folder on disk, season
// folders included; episode files carry their episode id.
func (s *Server) handleSeriesFiles(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid series id")
		return
	}
	if _, err := s.MovieRepo.GetSeries(id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "series not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	folder, episodes, err := s.seriesFolder(id)
	if err != nil {
		writeFolderError(w, err)
		return
	}
	main := map[string]int64{}
	for _, ep := range episodes {
		if rel, ok := folder.Rel(ep.FilePath); ok {
			main[rel] = ep.ID
		}
	}
	listFolder(w, folder, main)
}

// handleStreamFile plays a video, or shows a picture or text file, from a title's folder in the browser:
// ?movie={id}&path=<relative path> or ?series={id}&path=<relative path>.
// The file is sent as it is (no transcoding) with range support, so a
// <video> element can seek; whether it plays depends on the browser
// supporting the container and codecs. Pictures (jpg, png, webp, gif) keep
// their image type; text files (nfo, srt, ass, ssa, vtt, txt, up to 2 MB) are
// always sent as plain text. Anything else, html and svg included, is refused.
func (s *Server) handleStreamFile(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	movieID, seriesID := q.Get("movie"), q.Get("series")
	if (movieID == "") == (seriesID == "") {
		writeError(w, http.StatusBadRequest, "give either movie or series")
		return
	}
	rel := q.Get("path")
	if rel == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	var folder mediafiles.Folder
	if movieID != "" {
		id, err := strconv.ParseInt(movieID, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid movie id")
			return
		}
		m, err := s.MovieRepo.Get(id)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "movie not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if folder, err = s.movieFolder(m); err != nil {
			writeStreamFolderError(w, err)
			return
		}
	} else {
		id, err := strconv.ParseInt(seriesID, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid series id")
			return
		}
		if _, err := s.MovieRepo.GetSeries(id); errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "series not found")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if folder, _, err = s.seriesFolder(id); err != nil {
			writeStreamFolderError(w, err)
			return
		}
	}

	contentType, isText := mediafiles.PreviewContentType(rel)
	if contentType == "" {
		writeError(w, http.StatusUnsupportedMediaType, "only videos, pictures (jpg, png, webp, gif) and text files (nfo, srt, ass, ssa, vtt, txt) can be opened here")
		return
	}
	file, info, err := folder.Open(rel)
	switch {
	case errors.Is(err, mediafiles.ErrInvalidPath):
		writeError(w, http.StatusBadRequest, "invalid file path")
		return
	case errors.Is(err, fs.ErrNotExist):
		writeError(w, http.StatusNotFound, "file not found")
		return
	case err != nil:
		writeFileReadError(w, err)
		return
	}
	defer file.Close()
	if isText && info.Size() > mediafiles.MaxTextPreview {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("this text file is too large to preview (over %d MB)", mediafiles.MaxTextPreview>>20))
		return
	}

	name := path.Base(rel)
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
	http.ServeContent(w, r, name, info.ModTime(), file)
}

func writeStreamFolderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		writeError(w, http.StatusNotFound, "no files on disk for this title")
	case errors.Is(err, mediafiles.ErrOutside):
		writeError(w, http.StatusForbidden, "this title's files are not inside the library folders")
	default:
		writeFileReadError(w, err)
	}
}
