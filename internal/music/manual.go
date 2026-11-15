package music

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ManualIDPrefix starts the id of an artist or album that was added by hand,
// with no match on MusicBrainz ("manual:3f9a1c0d27b4"). Such an entry is
// searched for, downloaded and imported like any other, but MusicBrainz is
// never asked about it, so a look-up can't change or replace what the person
// typed.
const ManualIDPrefix = "manual:"

// IsManualID reports whether an artist or album id belongs to an entry added by hand.
func IsManualID(id string) bool { return strings.HasPrefix(id, ManualIDPrefix) }

// Manual reports whether the artist was added by hand.
func (a Artist) Manual() bool { return IsManualID(a.MBID) }

// Manual reports whether the album was added by hand.
func (a Album) Manual() bool { return IsManualID(a.MBID) }

var manualExternalID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ErrBadExternalID is returned for an outside id with odd characters or that is too long.
var ErrBadExternalID = errors.New("an id can only have letters, digits, dots, dashes and underscores, up to 64 characters")

// NewManualID makes the id of an entry added by hand. An outside id the
// person typed (a Discogs or catalogue number, say) is used as it is, so
// adding the same one twice is caught; without one the id is random.
func NewManualID(externalID string) (string, error) {
	externalID = strings.TrimSpace(externalID)
	if externalID != "" {
		if !manualExternalID.MatchString(externalID) {
			return "", ErrBadExternalID
		}
		return ManualIDPrefix + externalID, nil
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return ManualIDPrefix + hex.EncodeToString(b[:]), nil
}

// TracksFromFiles makes a tracklist out of the audio files of an album that
// has none, because it was added by hand and MusicBrainz knows nothing about
// it. The numbers in the files' tags and names are kept when every file has
// its own; otherwise the files are numbered in name order, all on disc 1.
func TracksFromFiles(infos []FileInfo) []Track {
	if len(infos) == 0 {
		return nil
	}
	order := make([]int, len(infos))
	for i := range order {
		order[i] = i
	}
	disc := func(i int) int { return max(infos[i].Disc, 1) }
	sort.SliceStable(order, func(a, b int) bool {
		x, y := order[a], order[b]
		if disc(x) != disc(y) {
			return disc(x) < disc(y)
		}
		if infos[x].Track != infos[y].Track {
			return infos[x].Track < infos[y].Track
		}
		return strings.ToLower(infos[x].Path) < strings.ToLower(infos[y].Path)
	})
	numbered := true
	seen := map[[2]int]bool{}
	for _, i := range order {
		k := [2]int{disc(i), infos[i].Track}
		if infos[i].Track <= 0 || seen[k] {
			numbered = false
			break
		}
		seen[k] = true
	}
	if !numbered {
		sort.SliceStable(order, func(a, b int) bool {
			return strings.ToLower(infos[order[a]].Path) < strings.ToLower(infos[order[b]].Path)
		})
	}
	out := make([]Track, 0, len(order))
	for n, i := range order {
		title := strings.TrimSpace(infos[i].Title)
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(infos[i].Path), filepath.Ext(infos[i].Path))
		}
		t := Track{Disc: 1, Position: n + 1, Title: title, Monitored: true}
		if numbered {
			t.Disc, t.Position = disc(i), infos[i].Track
		}
		out = append(out, t)
	}
	return out
}

// FindArtistByName returns the artist in the library with this name (case,
// accents and punctuation don't count), so an album added by hand goes to the
// artist already there instead of a second one.
func (r *Repo) FindArtistByName(name string) (Artist, bool, error) {
	want := NormalizeName(name)
	if want == "" {
		return Artist{}, false, nil
	}
	list, err := r.ListArtists()
	if err != nil {
		return Artist{}, false, err
	}
	for _, a := range list {
		if NormalizeName(a.Name) == want {
			return a.Artist, true, nil
		}
	}
	return Artist{}, false, nil
}
