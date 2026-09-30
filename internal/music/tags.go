package music

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/dhowden/tag"
)

// FileTagReader reads the tags embedded in audio files (ID3, Vorbis
// comments, MP4 atoms) with github.com/dhowden/tag. It only ever reads:
// nothing in a user's files is written or rewritten.
type FileTagReader struct{}

// ReadTags implements TagReader. A file without tags is not an error
// (ok is false); a damaged one is reported as one, and the caller falls
// back to the file name.
func (FileTagReader) ReadTags(path string) (info FileInfo, ok bool, err error) {
	// A tag reader parses whatever bytes it is given; never let a malformed
	// file bring the import down.
	defer func() {
		if r := recover(); r != nil {
			info, ok, err = FileInfo{}, false, fmt.Errorf("read tags of %s: unreadable tag data", path)
		}
	}()
	f, err := os.Open(path)
	if err != nil {
		return FileInfo{}, false, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if errors.Is(err, tag.ErrNoTagsFound) {
		return FileInfo{}, false, nil
	}
	if err != nil {
		return FileInfo{}, false, fmt.Errorf("read tags of %s: %w", path, err)
	}
	track, _ := m.Track()
	disc, _ := m.Disc()
	info = FileInfo{
		Path: path, Disc: disc, Track: track,
		Title:       strings.TrimSpace(m.Title()),
		Artist:      strings.TrimSpace(m.Artist()),
		AlbumArtist: strings.TrimSpace(m.AlbumArtist()),
		Album:       strings.TrimSpace(m.Album()),
		Year:        m.Year(),
		HasPicture:  m.Picture() != nil,
		FileType:    string(m.FileType()),
	}
	return info, true, nil
}

// variousArtists names the album artist compilations carry.
func variousArtists(name string) bool {
	switch NormalizeName(name) {
	case "variousartists", "various", "va", "compilation":
		return true
	}
	return false
}

// TagConsensus is what most of an album's files say about it. Empty or
// zero fields are ones the files do not agree on (or do not have).
type TagConsensus struct {
	Artist string // the album artist, else the track artist; never "Various Artists"
	Album  string
	Year   int
}

// ConsensusOf reads the album, artist and year most of the files agree on:
// a value counts only when more than half of the files that have one say
// it. The album artist tag wins over the track artist (a compilation's
// tracks each have their own artist).
func ConsensusOf(infos []FileInfo) TagConsensus {
	pick := func(values []string) string {
		count := map[string]int{}
		spelling := map[string]string{}
		n := 0
		for _, v := range values {
			if strings.TrimSpace(v) == "" {
				continue
			}
			n++
			k := NormalizeName(v)
			count[k]++
			if _, ok := spelling[k]; !ok {
				spelling[k] = strings.TrimSpace(v)
			}
		}
		for k, c := range count {
			if c*2 > n {
				return spelling[k]
			}
		}
		return ""
	}
	albumArtists := make([]string, 0, len(infos))
	artists := make([]string, 0, len(infos))
	albums := make([]string, 0, len(infos))
	years := make([]string, 0, len(infos))
	for _, in := range infos {
		albumArtists = append(albumArtists, in.AlbumArtist)
		artists = append(artists, in.Artist)
		albums = append(albums, in.Album)
		if in.Year > 0 {
			years = append(years, fmt.Sprint(in.Year))
		}
	}
	c := TagConsensus{Album: pick(albums)}
	if a := pick(albumArtists); a != "" && !variousArtists(a) {
		c.Artist = a
	} else if a == "" {
		if t := pick(artists); !variousArtists(t) {
			c.Artist = t
		}
	}
	fmt.Sscanf(pick(years), "%d", &c.Year)
	return c
}
