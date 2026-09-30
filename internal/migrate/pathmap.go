package migrate

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// PathMapping translates a folder as Radarr/Sonarr see it (From, inside
// their container) to the same folder as Mediarium sees it (To, inside its
// own container). /data/media/movies -> /movies turns
// /data/media/movies/Heat (1995) into /movies/Heat (1995).
type PathMapping struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// RootFolder is one of Radarr's or Sonarr's root folders and where it lands
// in Mediarium.
type RootFolder struct {
	Path         string `json:"path"`         // as Radarr/Sonarr see it
	MappedTo     string `json:"mappedTo"`     // as Mediarium sees it, after the path map
	Exists       bool   `json:"exists"`       // MappedTo is a folder Mediarium can read
	Suggested    bool   `json:"suggested"`    // the mapping was suggested, not sent in the request
	Titles       int    `json:"titles"`       // titles in this root folder
	FoldersFound int    `json:"foldersFound"` // of those, how many title folders exist at the translated path
}

// arrPath normalises a path reported by Radarr/Sonarr: forward slashes, no
// trailing slash. (They may run on Windows and report C:\Movies\...)
func arrPath(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, `\`, "/"))
	if p == "" {
		return ""
	}
	p = path.Clean(p)
	return p
}

func under(p, root string) (string, bool) {
	switch {
	case root == "":
		return "", false
	case p == root:
		return "", true
	case root == "/" && strings.HasPrefix(p, "/"):
		return strings.TrimPrefix(p, "/"), true
	case strings.HasPrefix(p, root+"/"):
		return p[len(root)+1:], true
	}
	return "", false
}

// translate applies the longest matching mapping. Without one, the path is
// used as it is (both apps may mount the folder at the same place).
func translate(p string, maps []PathMapping) (string, bool) {
	p = arrPath(p)
	best, bestLen, rest := -1, -1, ""
	for i, m := range maps {
		from := arrPath(m.From)
		if r, ok := under(p, from); ok && len(from) > bestLen {
			best, bestLen, rest = i, len(from), r
		}
	}
	if best < 0 {
		return filepath.FromSlash(p), false
	}
	to := strings.TrimSpace(maps[best].To)
	if rest == "" {
		return filepath.Clean(to), true
	}
	return filepath.Join(to, filepath.FromSlash(rest)), true
}

func isDir(p string) bool {
	if p == "" {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// cleanMappings drops empty rows from a requested path map.
func cleanMappings(in []PathMapping) []PathMapping {
	var out []PathMapping
	for _, m := range in {
		if strings.TrimSpace(m.From) == "" || strings.TrimSpace(m.To) == "" {
			continue
		}
		out = append(out, PathMapping{From: arrPath(m.From), To: filepath.Clean(strings.TrimSpace(m.To))})
	}
	return out
}

// rootsOf lists the root folders to report: the app's own list plus the
// parent folder of any title living outside all of them, each with the
// titles inside it.
func rootsOf(listed []arrRootFolder, titles []string) map[string][]string {
	roots := map[string][]string{}
	for _, r := range listed {
		if p := arrPath(r.Path); p != "" {
			roots[p] = nil
		}
	}
	for _, t := range titles {
		t = arrPath(t)
		if t == "" {
			continue
		}
		best := ""
		for r := range roots {
			if _, ok := under(t, r); ok && len(r) > len(best) {
				best = r
			}
		}
		if best == "" {
			best = path.Dir(t)
			if _, ok := roots[best]; !ok {
				roots[best] = nil
			}
		}
		roots[best] = append(roots[best], t)
	}
	return roots
}

// suggestSample caps how many title folders are checked per root.
const suggestSample = 50

// suggestMappings proposes a mapping for every root folder the requested
// map doesn't cover and that doesn't exist as-is in Mediarium, when
// Mediarium's library folder (libRoot) is plainly the same place: either
// its last path segment matches the root's ("/data/media/movies" and
// "/movies"), or it holds the same title folders.
func suggestMappings(roots map[string][]string, requested []PathMapping, libRoot string) []PathMapping {
	if strings.TrimSpace(libRoot) == "" {
		return nil
	}
	libRoot = filepath.Clean(libRoot)
	var out []PathMapping
	keys := make([]string, 0, len(roots))
	for r := range roots {
		keys = append(keys, r)
	}
	sort.Strings(keys)
	for _, root := range keys {
		if _, mapped := translate(root, requested); mapped {
			continue
		}
		if isDir(filepath.FromSlash(root)) {
			continue // Mediarium sees it at the same path
		}
		if strings.EqualFold(path.Base(root), filepath.Base(libRoot)) {
			out = append(out, PathMapping{From: root, To: libRoot})
			continue
		}
		titles := roots[root]
		if len(titles) > suggestSample {
			titles = titles[:suggestSample]
		}
		found := 0
		for _, t := range titles {
			if isDir(filepath.Join(libRoot, path.Base(t))) {
				found++
			}
		}
		if found > 0 && found*2 >= len(titles) {
			out = append(out, PathMapping{From: root, To: libRoot})
		}
	}
	return out
}

// describeRoots reports each root folder with where it lands.
func describeRoots(roots map[string][]string, effective []PathMapping, suggested []PathMapping) []RootFolder {
	isSuggested := map[string]bool{}
	for _, m := range suggested {
		isSuggested[m.From] = true
	}
	out := make([]RootFolder, 0, len(roots))
	for root, titles := range roots {
		mappedTo, _ := translate(root, effective)
		rf := RootFolder{Path: root, MappedTo: mappedTo, Exists: isDir(mappedTo), Suggested: isSuggested[root], Titles: len(titles)}
		for _, t := range titles {
			if p, _ := translate(t, effective); isDir(p) {
				rf.FoldersFound++
			}
		}
		out = append(out, rf)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// insideFolder reports whether p is libRoot or inside it.
func insideFolder(p, libRoot string) bool {
	if libRoot == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(libRoot), filepath.Clean(p))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
