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
	name := album
	if year > 0 {
		name = fmt.Sprintf("%s (%d)", album, year)
	}
	return nonEmpty(clean.apply(name), "Unknown Album")
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

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
