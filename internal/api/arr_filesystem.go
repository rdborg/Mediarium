package api

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/organizer"
)

// The folder browser the Radarr and Sonarr apps open when they pick a path
// (nzb360 asks on its home screen). It lists the same places the app's own
// file picker does, through the same checks: only the library folders and the
// downloads folder, links that leave them left out.

// arrFSRoots are the folders the apps may look into: the library folders
// (movies, shows, music, ebooks, audiobooks) and the downloads folder.
func (s *Server) arrFSRoots() []string {
	roots := s.browseRoots()
	for _, r := range []string{s.musicRoot(), s.ebooksRoot(), s.audiobooksRoot(), s.otherRoot()} {
		if r != "" {
			roots = append(roots, r)
		}
	}
	return roots
}

// arrFSEntry is one folder or file in a listing.
type arrFSEntry struct {
	Type         string `json:"type"`
	Size         int64  `json:"size"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	LastModified string `json:"lastModified"`
}

// arrFSListing is what GET /filesystem answers.
type arrFSListing struct {
	Parent      string       `json:"parent"`
	Directories []arrFSEntry `json:"directories"`
	Files       []arrFSEntry `json:"files"`
}

func arrFSTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

// arrFilesystem answers GET /filesystem. An empty path lists the library
// and downloads folders; any other path has to be inside them, else the
// answer is an empty list.
func (s *Server) arrFilesystem(w http.ResponseWriter, r *http.Request) {
	roots := s.arrFSRoots()
	q := r.URL.Query()
	includeFiles := strings.EqualFold(q.Get("includeFiles"), "true")
	out := arrFSListing{Directories: []arrFSEntry{}, Files: []arrFSEntry{}}
	want := q.Get("path")
	if want == "" {
		seen := map[string]bool{}
		for _, root := range roots {
			clean, ok := pathInside([]string{root}, root)
			if !ok || seen[clean] {
				continue
			}
			seen[clean] = true
			e := arrFSEntry{Type: "folder", Name: filepath.Base(clean), Path: clean}
			if info, err := os.Stat(clean); err == nil {
				e.LastModified = arrFSTime(info.ModTime())
			}
			out.Directories = append(out.Directories, e)
		}
		sort.SliceStable(out.Directories, func(i, j int) bool { return out.Directories[i].Path < out.Directories[j].Path })
		writeJSON(w, http.StatusOK, out)
		return
	}
	clean, ok := pathInside(roots, want)
	if !ok {
		writeJSON(w, http.StatusOK, out)
		return
	}
	entries, err := os.ReadDir(clean)
	if err != nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	if !isRoot(roots, clean) && rootContaining(roots, filepath.Dir(clean)) != "" {
		out.Parent = filepath.Dir(clean)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") {
			continue
		}
		full := filepath.Join(clean, name)
		if _, ok := pathInside(roots, full); !ok {
			continue // a link that leaves the folders
		}
		info, err := os.Stat(full)
		if err != nil {
			continue
		}
		switch {
		case info.IsDir():
			out.Directories = append(out.Directories, arrFSEntry{Type: "folder", Name: name, Path: full, LastModified: arrFSTime(info.ModTime())})
		case includeFiles && info.Mode().IsRegular():
			out.Files = append(out.Files, arrFSEntry{Type: "file", Name: name, Path: full, Size: info.Size(), LastModified: arrFSTime(info.ModTime())})
		}
	}
	less := func(l []arrFSEntry) func(i, j int) bool {
		return func(i, j int) bool { return strings.ToLower(l[i].Name) < strings.ToLower(l[j].Name) }
	}
	sort.SliceStable(out.Directories, less(out.Directories))
	sort.SliceStable(out.Files, less(out.Files))
	writeJSON(w, http.StatusOK, out)
}

// arrFilesystemType answers GET /filesystem/type: whether the path is a
// folder or a file. Anything outside the allowed folders is "unknown".
func (s *Server) arrFilesystemType(w http.ResponseWriter, r *http.Request) {
	kind := "unknown"
	if clean, ok := pathInside(s.arrFSRoots(), r.URL.Query().Get("path")); ok {
		if info, err := os.Stat(clean); err == nil {
			if info.IsDir() {
				kind = "folder"
			} else {
				kind = "file"
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"type": kind})
}

// arrFilesystemMediaFiles answers GET /filesystem/mediafiles: the video files
// directly in a folder.
func (s *Server) arrFilesystemMediaFiles(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	roots := s.arrFSRoots()
	if clean, ok := pathInside(roots, r.URL.Query().Get("path")); ok {
		if entries, err := os.ReadDir(clean); err == nil {
			for _, e := range entries {
				full := filepath.Join(clean, e.Name())
				if _, ok := pathInside(roots, full); !ok {
					continue
				}
				info, err := os.Stat(full)
				if err != nil || !info.Mode().IsRegular() || !organizer.IsVideoFile(e.Name()) {
					continue
				}
				out = append(out, map[string]any{"path": full, "relativePath": e.Name(), "name": e.Name(), "size": info.Size(), "dateModified": arrFSTime(info.ModTime())})
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}
