package api

import (
	"database/sql"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/cleanup"
)

// diskUsagePayload says how much a title has on disk in the library: what
// "Also delete everything on disk" would delete. It counts the files in the
// library only, not leftovers in the downloads folder.
type diskUsagePayload struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

// add counts one file.
func (u *diskUsagePayload) add(info fs.FileInfo) {
	u.Files++
	u.Bytes += info.Size()
}

// addFolder adds every file under folder (symbolic links are not followed).
func (u *diskUsagePayload) addFolder(folder string) {
	_ = filepath.WalkDir(folder, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			u.add(info)
		}
		return nil
	})
}

// addWithSidecars adds a video file and the files beside it that are named
// after it (subtitles, .nfo, artwork), which is what removing it deletes.
func (u *diskUsagePayload) addWithSidecars(file string, seen map[string]bool) {
	dir, name := filepath.Split(file)
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	entries, err := os.ReadDir(filepath.Clean(dir))
	if err != nil {
		return
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !(n == name || strings.HasPrefix(n, stem+".")) {
			continue
		}
		p := filepath.Join(filepath.Clean(dir), n)
		if seen[p] {
			continue
		}
		seen[p] = true
		if info, err := e.Info(); err == nil {
			u.add(info)
		}
	}
}

func (s *Server) handleMovieDiskUsage(w http.ResponseWriter, r *http.Request) {
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
	var u diskUsagePayload
	if m.FilePath != "" {
		root, folder := s.moviesRoot(), filepath.Dir(m.FilePath)
		if s.isTitleFolder(root, folder, []string{m.FilePath}, func(name string) bool { return matchesMovie(name, m) }) {
			u.addFolder(folder)
		} else {
			u.addWithSidecars(m.FilePath, map[string]bool{})
		}
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleSeriesDiskUsage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid show ID.")
		return
	}
	series, err := s.MovieRepo.GetSeries(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That show isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	eps, err := s.MovieRepo.ListEpisodes(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var files []string
	for _, ep := range eps {
		if ep.FilePath != "" {
			files = append(files, ep.FilePath)
		}
	}
	var u diskUsagePayload
	if len(files) > 0 {
		root := s.tvRoot()
		folder := commonDir(files)
		if seasonFolderName.MatchString(filepath.Base(folder)) {
			folder = filepath.Dir(folder)
		}
		if s.isTitleFolder(root, folder, files, func(name string) bool { return matchesShow(name, series) }) {
			u.addFolder(folder)
		} else {
			seen := map[string]bool{}
			for _, f := range files {
				u.addWithSidecars(f, seen)
			}
		}
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleArtistDiskUsage(w http.ResponseWriter, r *http.Request) {
	id, ok := musicID(w, r, "artist")
	if !ok {
		return
	}
	if _, err := s.MusicRepo.GetArtist(id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That artist isn't in your library.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	albums, err := s.MusicRepo.ListAlbums(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var u diskUsagePayload
	root := s.musicRoot()
	for _, a := range albums {
		// Only folders inside the music folder are ever deleted, so only those count.
		if a.Path != "" {
			if _, err := cleanup.InsideRoot(root, a.Path); err == nil {
				u.addFolder(a.Path)
			}
		}
	}
	writeJSON(w, http.StatusOK, u)
}
