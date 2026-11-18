package music

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Naming: one layout that Plex, Jellyfin and Navidrome all read without
// help (each also reads the embedded tags, which this layout does not
// depend on):
//
//	<music>/<Artist>/<Album> (<Year>)/<track> - <Title>.<ext>        one disc
//	<music>/<Artist>/<Album> (<Year>)/<disc>-<track> - <Title>.<ext>  several discs
//
// Track numbers have two digits ("07"), the disc number none ("2-07").
// NamingPresets lists the media servers the layout is meant for, so a
// settings screen can name them.
var NamingPresets = []string{"plex", "jellyfin", "navidrome"}

// Sanitizer cleans one path component (a name) of characters the file
// system or a media server cannot take. internal/organizer.Sanitize with
// the configured mode fits; it is passed in so this package does not depend
// on the video organizer.
type Sanitizer func(string) string

func (s Sanitizer) apply(v string) string {
	if s == nil {
		return v
	}
	return s(v)
}

// ArtistFolder is the artist's folder name.
func ArtistFolder(artist string, clean Sanitizer) string {
	return nonEmpty(clean.apply(artist), "Unknown Artist")
}

// AlbumFolder is the album's folder name: "Album (Year)", or just "Album"
// when the year is not known.
func AlbumFolder(album string, year int, clean Sanitizer) string {
	if year <= 0 {
		return nonEmpty(clean.apply(album), "Unknown Album")
	}
	suffix := fmt.Sprintf(" (%d)", year)
	name := clean.apply(album + suffix)
	if strings.HasSuffix(name, suffix) {
		return name
	}
	// A name too long for a folder is cut from its end, which would take the
	// year with it, and two editions of a long-titled album would then share
	// one folder. The title is shortened instead, until the year fits.
	runes := []rune(clean.apply(album))
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		title := strings.TrimRight(string(runes), " .")
		if name = clean.apply(title + suffix); strings.HasSuffix(name, suffix) {
			return name
		}
	}
	return nonEmpty(clean.apply(strings.TrimSpace(suffix)), "Unknown Album")
}

// TrackFileName is a track's file name: "07 - Title.flac", or
// "2-07 - Title.flac" when the album has more than one disc. ext keeps its
// dot and is lowercased.
func TrackFileName(disc, position int, title string, multiDisc bool, ext string, clean Sanitizer) string {
	num := fmt.Sprintf("%02d", position)
	if multiDisc {
		num = fmt.Sprintf("%d-%02d", disc, position)
	}
	name := clean.apply(num + " - " + title)
	if strings.TrimSpace(title) == "" {
		name = num
	}
	return name + strings.ToLower(ext)
}

// AlbumPath is the album's folder under the music root.
func AlbumPath(root, artist, album string, year int, clean Sanitizer) string {
	return filepath.Join(root, ArtistFolder(artist, clean), AlbumFolder(album, year, clean))
}

// UniqueAlbumPath is AlbumPath, unless another album of the library already
// has that folder: two bands with the same name (they share an artist folder),
// or an album and a single of the same name and year. Their files would land
// in one folder, the second import would find every file "already there" and
// fail. The one that comes second is told apart by the other band's
// disambiguation (or the start of its id), and a single or EP by its type
// ("Title (2020) [Single]"). Names already in use stay as they are: an album is
// only moved aside from a folder that is taken, never the one holding it.
// taken is every album of the library (the album itself is skipped).
func UniqueAlbumPath(root string, artist Artist, album Album, taken []Album, clean Sanitizer) string {
	year := album.Year()
	holder := func(p string) (Album, bool) {
		for _, o := range taken {
			if o.ID != album.ID && o.Path != "" && strings.EqualFold(filepath.Clean(o.Path), filepath.Clean(p)) {
				return o, true
			}
		}
		return Album{}, false
	}
	artistName, albumTitle := artist.Name, album.Title
	other, ok := holder(AlbumPath(root, artistName, albumTitle, year, clean))
	if !ok {
		return AlbumPath(root, artistName, albumTitle, year, clean)
	}
	short := func(id string) string {
		id = strings.ReplaceAll(id, "-", "")
		return id[:min(len(id), 8)]
	}
	if other.ArtistID != artist.ID {
		// Another artist of the same name.
		if d := strings.TrimSpace(artist.Disambiguation); d != "" {
			artistName = artist.Name + " (" + d + ")"
		} else {
			artistName = artist.Name + " [" + short(artist.MBID) + "]"
		}
		if _, ok := holder(AlbumPath(root, artistName, albumTitle, year, clean)); !ok {
			return AlbumPath(root, artistName, albumTitle, year, clean)
		}
	}
	// Another album of this artist: its type, then its id.
	if kind := strings.ToLower(album.Type); kind == "ep" || kind == "single" {
		label := map[string]string{"ep": "EP", "single": "Single"}[kind]
		if _, ok := holder(AlbumPath(root, artistName, albumTitle+" ["+label+"]", year, clean)); !ok {
			return AlbumPath(root, artistName, albumTitle+" ["+label+"]", year, clean)
		}
	}
	return AlbumPath(root, artistName, albumTitle+" ["+short(album.MBID)+"]", year, clean)
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
