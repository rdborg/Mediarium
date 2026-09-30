package libimport

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

type Kind string

const (
	KindMovie Kind = "movie"
	KindTV    Kind = "tv"
)

// File is one video file found on disk. Season and Episodes are only set
// for TV.
type File struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	Season    int    `json:"season,omitempty"`
	Episodes  []int  `json:"episodes,omitempty"`
	Quality   string `json:"quality"`
	// ModTime is when the file was last changed; an import uses it as the
	// date the title was added. Zero when unknown.
	ModTime time.Time `json:"-"`
}

// Group is everything on disk that belongs to one movie or one series.
type Group struct {
	Key   string `json:"key"`
	Kind  Kind   `json:"kind"`
	Title string `json:"title"`
	Year  int    `json:"year"`
	Files []File `json:"files"`
}

// Result is a finished scan. Skipped lists video files that were found but
// could not be attributed to anything, with a short reason each.
type Result struct {
	Groups  []Group
	Skipped []string
}

var (
	// Plex-style extras live in these folders or carry these suffixes; none
	// of them is the main feature.
	extraDirs   = map[string]bool{"extras": true, "featurettes": true, "trailers": true, "behind the scenes": true, "deleted scenes": true, "interviews": true, "scenes": true, "shorts": true, "other": true, "sample": true, "samples": true, "subs": true, "subtitles": true}
	extraSuffix = regexp.MustCompile(`(?i)-(trailer|featurette|behindthescenes|deleted|interview|scene|short|other)$`)
	nonAlnum    = regexp.MustCompile(`[^a-z0-9]+`)
	// "Season 1", "Season 01", "Specials", "S01".
	seasonFolder = regexp.MustCompile(`(?i)^(season[ ._-]*\d+|specials|s\d{1,2})$`)
)

type videoFile struct {
	path string
	rel  []string // path components relative to the scan root; the last is the file name
	size int64
	mod  time.Time
}

func walkVideos(root string) ([]videoFile, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("scan folder: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("scan folder: %s is not a directory", root)
	}
	var out []videoFile
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err // nothing at all could be read
			}
			return nil // an unreadable subfolder shouldn't abort the whole scan
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") || extraDirs[strings.ToLower(name)]) {
				return filepath.SkipDir
			}
			return nil
		}
		// Hidden files are not videos: macOS leaves "._Movie.mkv" stubs next
		// to the real file on drives it has written to.
		if strings.HasPrefix(name, ".") || !organizer.IsVideoFile(path) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		// Judged by the path below root: a folder called "sample" that the
		// library itself sits in must not hide every file in it.
		if organizer.IsSample(rel) {
			return nil
		}
		if extraSuffix.MatchString(strings.TrimSuffix(name, filepath.Ext(name))) {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, videoFile{path: path, rel: strings.Split(filepath.ToSlash(rel), "/"), size: fi.Size(), mod: fi.ModTime()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out, nil
}

func normalize(s string) string { return nonAlnum.ReplaceAllString(strings.ToLower(s), "") }

func fileBase(name string) string { return strings.TrimSuffix(name, filepath.Ext(name)) }

// fixFutureYear handles titles that end in a number that looks like a year
// ("Blade Runner 2049"): a year further out than next year is part of the
// title, not a release year.
func fixFutureYear(title string, year int) (string, int) {
	if year > time.Now().Year()+1 {
		return strings.TrimSpace(fmt.Sprintf("%s %d", title, year)), 0
	}
	return title, year
}

func groupKey(title string, year int) string { return fmt.Sprintf("%s|%d", normalize(title), year) }

// ScanMovies finds every movie file under root. The title comes from the
// file name, falling back to (or being completed by) the containing folder:
// "Inception (2010)/movie.mkv" is as common as a flat folder of release names.
func ScanMovies(root string) (Result, error) {
	files, err := walkVideos(root)
	if err != nil {
		return Result{}, err
	}
	var res Result
	groups := map[string]*Group{}
	var order []string
	for _, f := range files {
		title, year, tier := movieIdentity(f.rel)
		if title == "" {
			res.Skipped = append(res.Skipped, f.path+" - couldn't work out a title")
			continue
		}
		key := groupKey(title, year)
		g, ok := groups[key]
		if !ok {
			g = &Group{Key: key, Kind: KindMovie, Title: title, Year: year}
			groups[key] = g
			order = append(order, key)
		}
		g.Files = append(g.Files, File{Path: f.path, SizeBytes: f.size, Quality: string(tier), ModTime: f.mod})
	}
	for _, k := range order {
		res.Groups = append(res.Groups, *groups[k])
	}
	return res, nil
}

func movieIdentity(rel []string) (string, int, quality.Tier) {
	fr := parser.Parse(fileBase(rel[len(rel)-1]))
	title, year, tier := fr.Title, fr.Year, quality.Classify(fr)
	if len(rel) >= 2 {
		dr := parser.Parse(rel[len(rel)-2])
		if (year == 0 && dr.Year != 0 && dr.Title != "") || (title == "" && dr.Title != "") {
			title, year = dr.Title, dr.Year
		}
		if tier == quality.TierUnknown {
			tier = quality.Classify(dr)
		}
	}
	title, year = fixFutureYear(title, year)
	return title, year, tier
}

// ScanTV finds every episode file under root. Files whose season/episode
// can't be read from their name are reported as skipped rather than guessed
// at.
func ScanTV(root string) (Result, error) {
	files, err := walkVideos(root)
	if err != nil {
		return Result{}, err
	}
	var res Result
	groups := map[string]*Group{}
	var order []string
	seen := map[string]int{} // group key + season/episode -> index into that group's Files, to dedupe
	for _, f := range files {
		title, year, season, episodes, tier, ok := tvIdentity(f.rel)
		if !ok {
			res.Skipped = append(res.Skipped, f.path+" - couldn't read a season/episode number from the name")
			continue
		}
		key := groupKey(title, year)
		g, exists := groups[key]
		if !exists {
			g = &Group{Key: key, Kind: KindTV, Title: title, Year: year}
			groups[key] = g
			order = append(order, key)
		}
		file := File{Path: f.path, SizeBytes: f.size, Season: season, Episodes: episodes, Quality: string(tier), ModTime: f.mod}
		dupKey := fmt.Sprintf("%s#%d#%d", key, season, episodes[0])
		if idx, dup := seen[dupKey]; dup {
			// Two files claim the same episode (say a 720p and a 1080p
			// copy): keep the bigger one and report the other.
			if g.Files[idx].SizeBytes >= file.SizeBytes {
				res.Skipped = append(res.Skipped, f.path+" - duplicate copy of the same episode")
				continue
			}
			res.Skipped = append(res.Skipped, g.Files[idx].Path+" - duplicate copy of the same episode")
			g.Files[idx] = file
			continue
		}
		seen[dupKey] = len(g.Files)
		g.Files = append(g.Files, file)
	}
	for _, k := range order {
		res.Groups = append(res.Groups, *groups[k])
	}
	return res, nil
}

func tvIdentity(rel []string) (title string, year, season int, episodes []int, tier quality.Tier, ok bool) {
	fr := parser.Parse(fileBase(rel[len(rel)-1]))
	// Season 0 is the specials; only a missing episode number means "not an episode".
	if len(fr.Episodes) == 0 {
		return "", 0, 0, nil, "", false
	}
	title, year = fr.Title, fr.Year
	tier = tierFromNames(fileBase(rel[len(rel)-1]), nearestFolders(rel))
	// The folder directly under the scan root is the series folder, unless
	// it is itself a "Season 01" folder (the scan was pointed at one show),
	// in which case only the file name carries the show's name.
	if len(rel) >= 2 {
		top := parser.Parse(rel[0])
		if !seasonFolder.MatchString(rel[0]) && top.Title != "" {
			title, year = top.Title, top.Year
		}
	}
	if title == "" {
		return "", 0, 0, nil, "", false
	}
	title, year = fixFutureYear(title, year)
	return title, year, fr.Season, fr.Episodes, tier, true
}
