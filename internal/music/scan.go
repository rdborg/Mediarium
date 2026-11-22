package music

import (
	"fmt"
	"io/fs"
	"log/slog"
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

// isDir reports whether a directory entry is a folder, following a link to
// one (os.ReadDir says false for a symlinked folder).
func isDir(parent string, e fs.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&fs.ModeSymlink != 0 {
		info, err := os.Stat(filepath.Join(parent, e.Name()))
		return err == nil && info.IsDir()
	}
	return false
}

// ScanLibrary reads an existing music folder laid out the usual way,
// root/Artist/Album/tracks, without changing anything. Hidden folders are
// skipped, and album folders without audio files are left out. Linked folders
// are followed, and a folder that cannot be read is skipped (and logged)
// rather than stopping the whole scan.
func ScanLibrary(root string) ([]ScannedArtist, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read music folder %s: %w", root, err)
	}
	var out []ScannedArtist
	for _, e := range entries {
		if !isDir(root, e) || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		artist := ScannedArtist{Name: e.Name(), Path: filepath.Join(root, e.Name())}
		sub, err := os.ReadDir(artist.Path)
		if err != nil {
			slog.Warn("music scan: skipped a folder that can't be read", "folder", artist.Path, "err", err)
			continue
		}
		for _, a := range sub {
			p := filepath.Join(artist.Path, a.Name())
			if !isDir(artist.Path, a) {
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
				slog.Warn("music scan: skipped an album folder that can't be read", "folder", p, "err", err)
				continue
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
			return withoutTypeTag(rest), year
		}
	}
	title = cutAlbum(name)
	if title == "" {
		title = name
	}
	return withoutTypeTag(title), yearFrom(name, title)
}

// reTypeTag is the "[Single]" or "[EP]" that follows a title in a folder
// Mediarium named: it tells the release type, it is not part of the title.
var reTypeTag = regexp.MustCompile(`(?i)\s*\[(?:single|ep)\]\s*$`)

// withoutTypeTag drops a trailing "[Single]" or "[EP]" from a folder's title,
// unless that is all the title is.
func withoutTypeTag(title string) string {
	if t := strings.TrimSpace(reTypeTag.ReplaceAllString(title, "")); t != "" {
		return t
	}
	return title
}
