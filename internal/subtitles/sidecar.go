package subtitles

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rdborg/mediarium/internal/parser"
)

// SidecarExts are the subtitle file types picked up from finished downloads.
// A VobSub subtitle is a .idx/.sub pair; both halves travel together.
var SidecarExts = map[string]bool{".srt": true, ".ass": true, ".ssa": true, ".sub": true, ".idx": true, ".sup": true}

// Sidecar is a subtitle that came with a release: one subtitle track, which is
// one file, or two for a VobSub .idx/.sub pair.
type Sidecar struct {
	Lang     string // canonical code, e.g. "en"
	Forced   bool
	SDH      bool
	Season   int   // 0 unless the file's name or folder names an episode
	Episodes []int // the episodes it is for
	Files    []string
	Size     int64 // total bytes, used to prefer the fuller of two subtitles for the same slot
}

// Label is how the subtitle is described to the person: "en", "en forced".
func (sc Sidecar) Label() string {
	l := sc.Lang
	if sc.Forced {
		l += " forced"
	}
	if sc.SDH {
		l += " sdh"
	}
	return l
}

// suffix is what goes between the video's name and the extension:
// ".en", ".en.forced", ".pt-BR.sdh".
func (sc Sidecar) suffix() string {
	s := "." + sc.Lang
	if sc.Forced {
		s += ".forced"
	}
	if sc.SDH {
		s += ".sdh"
	}
	return s
}

// MatchesEpisodes reports whether the subtitle belongs to the given episodes
// of a season. A subtitle that names no episode matches only when the download
// holds a single video (single), since then it can only be that video's.
func (sc Sidecar) MatchesEpisodes(season int, episodes []int, single bool) bool {
	if sc.Season == 0 {
		return single
	}
	if sc.Season != season {
		return false
	}
	for _, e := range sc.Episodes {
		for _, want := range episodes {
			if e == want {
				return true
			}
		}
	}
	return false
}

// isSamplePath mirrors how the importer skips sample clips.
func isSamplePath(rel string) bool {
	parts := strings.Split(strings.ToLower(filepath.ToSlash(rel)), "/")
	for _, p := range parts[:len(parts)-1] {
		if p == "sample" || p == "samples" {
			return true
		}
	}
	return strings.Contains(parts[len(parts)-1], "sample")
}

// episodeOf reads the season and episodes a subtitle is for from its own name,
// then from the folders above it (Subs/Show.S01E02.1080p/2_English.srt).
func episodeOf(rel string) (int, []int) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		name := parts[i]
		if i == len(parts)-1 {
			name = strings.TrimSuffix(name, filepath.Ext(name))
		}
		if r := parser.Parse(name); r.Season > 0 && len(r.Episodes) > 0 {
			return r.Season, r.Episodes
		}
	}
	return 0, nil
}

// FindSidecars lists the subtitles in a finished download folder whose
// language can be told, skipping sample clips. It only reads.
func FindSidecars(root string) ([]Sidecar, error) {
	groups := map[string]*Sidecar{}
	var order []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil // an unreadable corner must not stop the import
		}
		// Real files only: a link named like a subtitle would copy whatever it
		// points at into the library.
		if d.IsDir() || !d.Type().IsRegular() || !SidecarExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || isSamplePath(rel) {
			return nil
		}
		det, ok := DetectLanguage(rel)
		if !ok {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() == 0 {
			return nil
		}
		key := strings.ToLower(strings.TrimSuffix(path, filepath.Ext(path)))
		sc, exists := groups[key]
		if !exists {
			season, eps := episodeOf(rel)
			sc = &Sidecar{Lang: det.Lang, Forced: det.Forced, SDH: det.SDH, Season: season, Episodes: eps}
			groups[key] = sc
			order = append(order, key)
		}
		sc.Files = append(sc.Files, path)
		sc.Size += info.Size()
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s for subtitles: %w", root, err)
	}
	out := make([]Sidecar, 0, len(order))
	for _, k := range order {
		out = append(out, *groups[k])
	}
	return out, nil
}

// ImportSidecars copies subtitles next to an imported video as
// "<video name>.<lang>[.forced][.sdh].<ext>", the naming Plex, Jellyfin and
// Kodi read. It never overwrites a file that is already there (so of two
// subtitles for the same slot the fuller one, tried first, wins) and never
// touches the source files. It returns the subtitles it placed; an error means
// some file could not be copied, and the rest were still tried.
func ImportSidecars(videoPath string, scs []Sidecar) ([]Sidecar, error) {
	base := strings.TrimSuffix(videoPath, filepath.Ext(videoPath))
	ordered := append([]Sidecar(nil), scs...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Size > ordered[j].Size })

	var imported []Sidecar
	var errs []error
	for _, sc := range ordered {
		placed, err := placeSidecar(base, sc)
		if err != nil {
			errs = append(errs, err)
		}
		if placed {
			imported = append(imported, sc)
		}
	}
	return imported, errors.Join(errs...)
}

func placeSidecar(base string, sc Sidecar) (bool, error) {
	dests := make([]string, len(sc.Files))
	for i, src := range sc.Files {
		dests[i] = base + sc.suffix() + strings.ToLower(filepath.Ext(src))
		if _, err := os.Lstat(dests[i]); err == nil {
			return false, nil // the slot is taken: leave it alone
		}
	}
	var created []string
	for i, src := range sc.Files {
		if err := copyNew(src, dests[i]); err != nil {
			for _, c := range created { // only ever removes files created just now
				_ = os.Remove(c)
			}
			if errors.Is(err, fs.ErrExist) {
				return false, nil
			}
			return false, fmt.Errorf("copy subtitle %s: %w", filepath.Base(src), err)
		}
		created = append(created, dests[i])
	}
	return true, nil
}

// copyNew copies src to dst, failing if dst exists.
func copyNew(src, dst string) error {
	if fi, err := os.Lstat(src); err != nil {
		return err
	} else if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", filepath.Base(src))
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}
