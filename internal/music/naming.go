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

// TypeLabel is the word a single or an EP carries in its folder name
// ("Single", "EP"), and "" for an album or anything else.
func TypeLabel(albumType string) string {
	switch strings.ToLower(strings.TrimSpace(albumType)) {
	case "ep":
		return "EP"
	case "single":
		return "Single"
	}
	return ""
}

// FolderTitle is the title an album's folder is named after. A single or an EP
// gets its type after the title ("Title [Single]") so it is told from an album
// of the same name at a glance, and never clashes with one. An album keeps its
// title as it is, and so does a title that already ends in its type.
func FolderTitle(title, albumType string) string {
	label := TypeLabel(albumType)
	if label == "" {
		return title
	}
	t := strings.ToLower(strings.TrimSpace(title))
	l := strings.ToLower(label)
	for _, suffix := range []string{" [" + l + "]", " (" + l + ")", " - " + l} {
		if strings.HasSuffix(t, suffix) {
			return title
		}
	}
	return title + " [" + label + "]"
}

// UniqueAlbumPath is AlbumPath for a new import: a single or an EP is named
// "Title [Single] (2020)" or "Title [EP] (2020)", an album "Title (2020)". On
// top of that it keeps two albums out of one folder: two bands with the same
// name (they share an artist folder), or two releases of one name, type and
// year. Their files would land in one folder, the second import would find
// every file "already there" and fail. The one that comes second is told apart
// by the other band's disambiguation (or the start of its id), and the second
// of two same-named albums by the start of its id.
//
// An album that already has a folder (its Path) keeps the naming it was
// imported with: a single imported before the type was always added stays in
// its folder, and is only given the type when another release is in the way.
// Names already in use stay as they are: an album is only moved aside from a
// folder that is taken, never the one holding it. taken is every album of the
// library (the album itself is skipped).
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
	if typed := FolderTitle(albumTitle, album.Type); album.Path == "" {
		albumTitle = typed
	} else if typed != albumTitle && strings.EqualFold(filepath.Clean(album.Path), filepath.Clean(AlbumPath(root, artistName, typed, year, clean))) {
		// Imported with its type in the name: more files go to the same folder.
		albumTitle = typed
	}
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
	// Another release of this artist: its type (for a single or EP that has a
	// folder named without it), then its id.
	if typed := FolderTitle(albumTitle, album.Type); typed != albumTitle {
		if _, ok := holder(AlbumPath(root, artistName, typed, year, clean)); !ok {
			return AlbumPath(root, artistName, typed, year, clean)
		}
		albumTitle = typed
	}
	return AlbumPath(root, artistName, albumTitle+" ["+short(album.MBID)+"]", year, clean)
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
