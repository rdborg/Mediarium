package music

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// audioExtensions are the audio files an album is made of.
var audioExtensions = map[string]bool{
	".flac": true, ".mp3": true, ".m4a": true, ".aac": true, ".ogg": true, ".opus": true, ".alac": true,
}

// IsAudioFile reports whether path has an audio file extension.
func IsAudioFile(path string) bool { return audioExtensions[strings.ToLower(filepath.Ext(path))] }

// artworkNames are the cover images kept with an album: media servers and
// players look for these names.
var artworkNames = map[string]bool{
	"cover.jpg": true, "cover.jpeg": true, "cover.png": true,
	"folder.jpg": true, "folder.jpeg": true, "folder.png": true,
}

// IsArtwork reports whether path is an album cover image kept on import.
func IsArtwork(path string) bool { return artworkNames[strings.ToLower(filepath.Base(path))] }

// FindAudioFiles returns every audio file under dir, sorted by path (so CD1
// comes before CD2 and track 01 before 02). Hidden folders and macOS
// resource forks are skipped.
func FindAudioFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != dir && (strings.HasPrefix(name, ".") || name == "__MACOSX") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, "._") || !IsAudioFile(name) {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s for audio files: %w", dir, err)
	}
	sort.Strings(out)
	return out, nil
}

// FindArtwork returns the cover images (cover.jpg, folder.jpg...) under dir,
// at most one per lowercased name, shallowest first.
func FindArtwork(dir string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && IsArtwork(path) {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s for artwork: %w", dir, err)
	}
	sort.Slice(found, func(i, j int) bool {
		di, dj := strings.Count(found[i], string(os.PathSeparator)), strings.Count(found[j], string(os.PathSeparator))
		if di != dj {
			return di < dj
		}
		return found[i] < found[j]
	})
	seen := map[string]bool{}
	var out []string
	for _, f := range found {
		key := strings.ToLower(filepath.Base(f))
		if !seen[key] {
			seen[key] = true
			out = append(out, f)
		}
	}
	return out, nil
}

// FileInfo is what is known about one audio file: where it sits on the
// album (disc, track), its title and, when its tags were read, who made it
// and which album it is on. Zero values mean unknown.
type FileInfo struct {
	Path        string
	Disc        int
	Track       int
	Title       string
	Artist      string // the track artist tag
	AlbumArtist string
	Album       string
	Year        int
	HasPicture  bool   // a cover picture is embedded
	FileType    string // FLAC, MP3, M4A, ALAC, OGG, ... as the tag reader names it
}

// TagReader reads the tags embedded in an audio file. ok is false when the
// file has none. FileTagReader is the one used in the app; when a reader is
// nil, or a file has no tags, files are identified by their folder and file
// names alone (see IdentifyFile).
type TagReader interface {
	ReadTags(path string) (info FileInfo, ok bool, err error)
}

var (
	reDiscTrack  = regexp.MustCompile(`^(\d)[-.](\d{1,3})(?:\s*[-._)\]]\s*|\s+)(.*)$`)
	reTrack      = regexp.MustCompile(`^(\d{1,3})(?:\s*[-._)\]]\s*|\s+)(.*)$`)
	reArtistPart = regexp.MustCompile(`^.+?\s-\s(\d{1,3})\s*[-._]\s*(.*)$`)
	reDiscFolder = regexp.MustCompile(`(?i)^(?:cd|disc|disk|dvd)[\s._-]*(\d{1,2})\b`)
)

// IdentifyFile works out a file's disc, track and title: from its name
// ("07 - Title.flac", "2-07 Title.flac", "Artist - 07 - Title.mp3",
// "107-artist-title.mp3") and its folder ("CD2/07 - Title.flac"); then from
// its tags when a TagReader is given, whose values win where they are set.
func IdentifyFile(path string, tags TagReader) FileInfo {
	info := infoFromName(path)
	if tags != nil {
		if t, ok, err := tags.ReadTags(path); err == nil && ok {
			if t.Disc > 0 {
				info.Disc = t.Disc
			}
			if t.Track > 0 {
				info.Track = t.Track
			}
			if strings.TrimSpace(t.Title) != "" {
				info.Title = t.Title
			}
			info.Artist, info.AlbumArtist, info.Album = t.Artist, t.AlbumArtist, t.Album
			info.Year, info.HasPicture, info.FileType = t.Year, t.HasPicture, t.FileType
		}
	}
	return info
}

func infoFromName(path string) FileInfo {
	info := FileInfo{Path: path}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.TrimSpace(base)
	if m := reDiscFolder.FindStringSubmatch(filepath.Base(filepath.Dir(path))); m != nil {
		info.Disc, _ = strconv.Atoi(m[1])
	}
	switch {
	case reDiscTrack.MatchString(base):
		m := reDiscTrack.FindStringSubmatch(base)
		info.Disc, _ = strconv.Atoi(m[1])
		info.Track, _ = strconv.Atoi(m[2])
		info.Title = m[3]
	case reTrack.MatchString(base):
		m := reTrack.FindStringSubmatch(base)
		n, _ := strconv.Atoi(m[1])
		if len(m[1]) == 3 && n >= 100 {
			// "107-artist-title": disc 1, track 07 (scene multi-disc naming).
			info.Disc, info.Track = n/100, n%100
		} else {
			info.Track = n
		}
		info.Title = m[2]
	case reArtistPart.MatchString(base):
		m := reArtistPart.FindStringSubmatch(base)
		info.Track, _ = strconv.Atoi(m[1])
		info.Title = m[2]
	default:
		info.Title = base
	}
	info.Title = strings.TrimSpace(strings.NewReplacer("_", " ").Replace(info.Title))
	return info
}

// TrackSlot is one track of the album's tracklist to fill.
type TrackSlot struct {
	Disc     int
	Position int
	Title    string
}

// Match pairs a file with a track slot (both by index).
type Match struct {
	File int
	Slot int
}

// MatchTracks pairs downloaded files with the album's tracklist, each slot
// and each file used at most once:
//
//  1. by disc and track number when the file's title agrees (or it has
//     none); on a one-disc album a missing disc number is disc 1, and on a
//     several-disc album a file without a disc number may count through the
//     discs ("13" is the first track of disc 2 after a 12-track disc 1);
//  2. then by title, for files whose numbers did not fit;
//  3. then by number alone, for what is left.
//
// It returns the matches and the files that fit no track.
func MatchTracks(files []FileInfo, slots []TrackSlot) (matches []Match, unmatched []int) {
	multiDisc := false
	for _, s := range slots {
		if s.Disc > 1 {
			multiDisc = true
		}
	}
	slotOf := map[[2]int]int{}
	for i, s := range slots {
		slotOf[[2]int{s.Disc, s.Position}] = i
	}
	numbered := func(f FileInfo) (int, bool) {
		if f.Track <= 0 {
			return 0, false
		}
		disc := f.Disc
		if disc == 0 && !multiDisc {
			disc = 1
		}
		if disc > 0 {
			i, ok := slotOf[[2]int{disc, f.Track}]
			return i, ok
		}
		if f.Track <= len(slots) { // counted through the discs
			return f.Track - 1, true
		}
		return 0, false
	}

	fileDone := make([]bool, len(files))
	slotDone := make([]bool, len(slots))
	take := func(fi, si int) {
		fileDone[fi], slotDone[si] = true, true
		matches = append(matches, Match{File: fi, Slot: si})
	}

	for fi, f := range files {
		if si, ok := numbered(f); ok && !slotDone[si] && (f.Title == "" || titlesAgree(f.Title, slots[si].Title)) {
			take(fi, si)
		}
	}
	for fi, f := range files {
		if fileDone[fi] || f.Title == "" {
			continue
		}
		cand := -1
		for si, s := range slots {
			if !slotDone[si] && titlesAgree(f.Title, s.Title) {
				if cand >= 0 {
					cand = -2 // more than one fits: leave it
					break
				}
				cand = si
			}
		}
		if cand >= 0 {
			take(fi, cand)
		}
	}
	for fi, f := range files {
		if fileDone[fi] {
			continue
		}
		if si, ok := numbered(f); ok && !slotDone[si] {
			take(fi, si)
		}
	}
	for fi := range files {
		if !fileDone[fi] {
			unmatched = append(unmatched, fi)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Slot < matches[j].Slot })
	return matches, unmatched
}

// titlesAgree compares a file's title with a track title: equal once
// normalized, or one containing the other (a scene file name carries the
// artist too: "radiohead-airbag").
func titlesAgree(fileTitle, trackTitle string) bool {
	a, b := NormalizeName(fileTitle), NormalizeName(trackTitle)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if len(b) >= 4 && strings.Contains(a, b) {
		return true
	}
	return len(a) >= 4 && strings.Contains(b, a)
}
