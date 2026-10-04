package api

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/settings"
)

// More than one library folder: besides the main movies and TV folders,
// Settings can list extra ones (another disk, a "Kids" folder). Each movie or
// show may be kept in one of them; new files go there. Every safety check
// that keeps file operations inside "the library" accepts any of these
// folders, and the folder a file is actually in is used for it.

// extraPaths reads one of the "more folders" settings: one path per line,
// clean, absolute, without duplicates.
func (s *Server) extraPaths(key string) []string {
	v, _ := s.Settings.Get(key)
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(v, "\n") {
		p := strings.TrimSpace(line)
		if p == "" || !filepath.IsAbs(p) {
			continue
		}
		p = filepath.Clean(p)
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// movieRoots is the main movies folder followed by the extra ones.
func (s *Server) movieRoots() []string {
	return withMain(s.moviesRoot(), s.extraPaths(settings.KeyMoviesExtraPaths))
}

// tvRoots is the main TV folder followed by the extra ones.
func (s *Server) tvRoots() []string {
	return withMain(s.tvRoot(), s.extraPaths(settings.KeyTVExtraPaths))
}

func withMain(main string, extra []string) []string {
	out := []string{}
	if main != "" {
		out = append(out, filepath.Clean(main))
	}
	for _, e := range extra {
		if e != filepath.Clean(main) {
			out = append(out, e)
		}
	}
	return out
}

// rootContaining returns the folder of roots that path lies in, or "" when
// it is in none of them. The deepest match wins, so a folder inside another
// is told apart. This only picks a folder; the file operations still check,
// symbolic links included, that the path is inside it.
func rootContaining(roots []string, path string) string {
	best := ""
	clean := filepath.Clean(path)
	for _, r := range roots {
		rel, err := filepath.Rel(r, clean)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if len(r) > len(best) {
			best = r
		}
	}
	return best
}

// isRoot reports whether root is one of roots.
func isRoot(roots []string, root string) bool {
	for _, r := range roots {
		if r == filepath.Clean(root) {
			return true
		}
	}
	return false
}

// movieFileRoot is the library folder a movie's file is in, for checks and
// removals: the folder that holds it, or the main folder when none does (so
// the usual "outside the library" refusal applies).
func (s *Server) movieFileRoot(path string) string {
	if r := rootContaining(s.movieRoots(), path); r != "" {
		return r
	}
	return s.moviesRoot()
}

// tvFileRoot is movieFileRoot for TV.
func (s *Server) tvFileRoot(path string) string {
	if r := rootContaining(s.tvRoots(), path); r != "" {
		return r
	}
	return s.tvRoot()
}

// movieHome is where a movie's new files go: its chosen folder while that is
// still one of the library folders, otherwise the main one.
func (s *Server) movieHome(m library.Movie) string {
	if m.RootPath != "" && isRoot(s.movieRoots(), m.RootPath) {
		return filepath.Clean(m.RootPath)
	}
	// A movie imported from an extra folder stays in that folder.
	if m.FilePath != "" {
		if r := rootContaining(s.movieRoots(), m.FilePath); r != "" {
			return r
		}
	}
	return s.moviesRoot()
}

// seriesHome is movieHome for a show.
func (s *Server) seriesHome(sr library.Series) string {
	if sr.RootPath != "" && isRoot(s.tvRoots(), sr.RootPath) {
		return filepath.Clean(sr.RootPath)
	}
	return s.tvRoot()
}

// rootKey names a library folder in the recycle bin's addresses: "movies"
// for the main one, "movies-2" for the first extra one, and so on.
func rootKey(kind string, i int) string {
	if i == 0 {
		return kind
	}
	return kind + "-" + strconv.Itoa(i+1)
}

// firstOf is the first path, or "".
func firstOf(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}
