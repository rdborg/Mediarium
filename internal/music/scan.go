package music

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ScannedAlbum is one album folder found in an existing music folder.
type ScannedAlbum struct {
	Folder string   // the folder's name as found
	Path   string   // full path
	Title  string   // album name read from the folder name
	Year   int      // year read from the folder name, 0 if none
	Files  []string // audio files inside (also in CD1/CD2 sub-folders), sorted
}

// ScannedArtist is one artist folder with its album folders.
type ScannedArtist struct {
	Name   string // the folder's name
	Path   string
	Albums []ScannedAlbum
	// Loose are audio files directly in the artist folder (not in an album
	// folder): they cannot be told apart, so they are reported, not imported.
	Loose []string
}

// ScanLibrary reads an existing music folder laid out the usual way,
// root/Artist/Album/tracks, without changing anything. Hidden folders are
// skipped, and album folders without audio files are left out.
func ScanLibrary(root string) ([]ScannedArtist, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read music folder %s: %w", root, err)
	}
	var out []ScannedArtist
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		artist := ScannedArtist{Name: e.Name(), Path: filepath.Join(root, e.Name())}
		sub, err := os.ReadDir(artist.Path)
		if err != nil {
			return nil, fmt.Errorf("read artist folder %s: %w", artist.Path, err)
		}
		for _, a := range sub {
			p := filepath.Join(artist.Path, a.Name())
			if !a.IsDir() {
				if IsAudioFile(a.Name()) {
					artist.Loose = append(artist.Loose, p)
				}
				continue
			}
			if strings.HasPrefix(a.Name(), ".") {
				continue
			}
			files, err := FindAudioFiles(p)
			if err != nil {
				return nil, err
			}
			if len(files) == 0 {
				continue
			}
			title, year := ParseAlbumFolder(a.Name(), artist.Name)
			artist.Albums = append(artist.Albums, ScannedAlbum{Folder: a.Name(), Path: p, Title: title, Year: year, Files: files})
		}
		if len(artist.Albums) > 0 || len(artist.Loose) > 0 {
			out = append(out, artist)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// reYearFirst matches a folder name that starts with its year, bracketed
// ("(1997) OK Computer") or followed by a separator ("1997 - OK Computer").
var reYearFirst = regexp.MustCompile(`^(?:[(\[]((?:19|20)\d{2})[)\]]\s*(?:[-–.]\s*)?|((?:19|20)\d{2})\s*[-–.]\s*)(.+)$`)

// ParseAlbumFolder reads an album folder name: "Album (2020)",
// "Album [2020] [FLAC]", "2020 - Album", "(2020) Album" or
// "Artist - Album (2020)" (the artist part is dropped when it is the
// artist's own name).
func ParseAlbumFolder(name, artist string) (title string, year int) {
	name = strings.TrimSpace(name)
	if before, after, ok := strings.Cut(name, " - "); ok && SameArtist(before, artist) {
		name = strings.TrimSpace(after)
	}
	if m := reYearFirst.FindStringSubmatch(name); m != nil {
		if rest := cutAlbum(m[3]); rest != "" {
			year, _ = strconv.Atoi(m[1] + m[2]) // one of the two is empty
			return rest, year
		}
	}
	title = cutAlbum(name)
	if title == "" {
		title = name
	}
	return title, yearFrom(name, title)
}
