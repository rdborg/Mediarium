package musicbrainz

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Artist is a MusicBrainz artist.
type Artist struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SortName       string `json:"sort-name"`
	Disambiguation string `json:"disambiguation"`
	Type           string `json:"type"`    // Person, Group, ...
	Country        string `json:"country"` // ISO code, may be empty
	Score          int    `json:"score"`   // search relevance 0-100 (search results only)
}

// ReleaseGroup is an album, EP or single as a work, across all its
// editions (the "releases").
type ReleaseGroup struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	PrimaryType      string   `json:"primary-type"`    // Album, EP, Single, Broadcast, Other
	SecondaryTypes   []string `json:"secondary-types"` // Compilation, Live, Remix, Soundtrack, ...
	FirstReleaseDate string   `json:"first-release-date"`
	Disambiguation   string   `json:"disambiguation"`
}

// Year is the year of the group's first release, 0 if unknown.
func (g ReleaseGroup) Year() int { return yearOf(g.FirstReleaseDate) }

// Release is one edition of a release group (a country's CD, a vinyl, a
// digital release), with its media when they were asked for.
type Release struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Status  string   `json:"status"` // Official, Promotion, Bootleg, Pseudo-Release
	Country string   `json:"country"`
	Date    string   `json:"date"` // "YYYY", "YYYY-MM" or "YYYY-MM-DD"
	Media   []Medium `json:"media"`
}

// Medium is one disc (or side, or file set) of a release.
type Medium struct {
	Position   int     `json:"position"`
	Format     string  `json:"format"` // CD, Digital Media, 12" Vinyl, ...
	Title      string  `json:"title"`
	TrackCount int     `json:"track-count"`
	Tracks     []Track `json:"tracks"`
}

// Track is one track on a medium.
type Track struct {
	ID        string    `json:"id"`
	Number    string    `json:"number"`   // as printed: "1", "A1"
	Position  int       `json:"position"` // 1-based on its medium
	Title     string    `json:"title"`
	Length    int       `json:"length"` // milliseconds, 0 if unknown
	Recording Recording `json:"recording"`
}

// Recording is the recorded performance a track is an instance of.
type Recording struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Length int    `json:"length"`
}

// TrackRef is one track of a release, flattened: which disc, its position on
// that disc, its title and length.
type TrackRef struct {
	Disc     int
	Position int
	Title    string
	LengthMs int
}

// Tracklist flattens a release's media into one list, disc by disc.
func (r Release) Tracklist() []TrackRef {
	var out []TrackRef
	for i, m := range r.Media {
		disc := m.Position
		if disc <= 0 {
			disc = i + 1
		}
		for j, t := range m.Tracks {
			pos := t.Position
			if pos <= 0 {
				pos = j + 1
			}
			title := t.Title
			if title == "" {
				title = t.Recording.Title
			}
			length := t.Length
			if length == 0 {
				length = t.Recording.Length
			}
			out = append(out, TrackRef{Disc: disc, Position: pos, Title: title, LengthMs: length})
		}
	}
	return out
}

// Release-group types an artist's discography can be filtered on.
const (
	TypeAlbum  = "album"
	TypeEP     = "ep"
	TypeSingle = "single"
)

// SearchArtists finds artists by name, best match first.
func (c *Client) SearchArtists(ctx context.Context, name string, limit int) ([]Artist, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return []Artist{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	var out struct {
		Artists []Artist `json:"artists"`
	}
	q := url.Values{"query": {luceneEscape(name)}, "limit": {strconv.Itoa(limit)}}
	if err := c.get(ctx, "/ws/2/artist", q, &out); err != nil {
		return nil, fmt.Errorf("search artists %q: %w", name, err)
	}
	if out.Artists == nil {
		out.Artists = []Artist{}
	}
	return out.Artists, nil
}

// GetArtist looks up one artist by MusicBrainz id.
func (c *Client) GetArtist(ctx context.Context, mbid string) (Artist, error) {
	var a Artist
	if err := c.get(ctx, "/ws/2/artist/"+url.PathEscape(mbid), nil, &a); err != nil {
		return Artist{}, fmt.Errorf("get artist %s: %w", mbid, err)
	}
	return a, nil
}

// ArtistReleaseGroups lists an artist's release groups of the given types
// (TypeAlbum, TypeEP, TypeSingle; none means all three), oldest first. Groups
// with a secondary type (compilations, live albums, remixes, soundtracks,
// DJ mixes...) are left out unless includeSecondary is set: a discography
// is its studio releases.
func (c *Client) ArtistReleaseGroups(ctx context.Context, mbid string, types []string, includeSecondary bool) ([]ReleaseGroup, error) {
	if len(types) == 0 {
		types = []string{TypeAlbum, TypeEP, TypeSingle}
	}
	const page = 100
	var all []ReleaseGroup
	for offset := 0; ; offset += page {
		var out struct {
			Count  int            `json:"release-group-count"`
			Groups []ReleaseGroup `json:"release-groups"`
		}
		q := url.Values{
			"artist": {mbid}, "type": {strings.Join(types, "|")},
			"limit": {strconv.Itoa(page)}, "offset": {strconv.Itoa(offset)},
		}
		if err := c.get(ctx, "/ws/2/release-group", q, &out); err != nil {
			return nil, fmt.Errorf("list release groups of artist %s: %w", mbid, err)
		}
		all = append(all, out.Groups...)
		if len(out.Groups) < page || offset+page >= out.Count || offset >= 1000 {
			break
		}
	}
	out := make([]ReleaseGroup, 0, len(all))
	for _, g := range all {
		if !includeSecondary && len(g.SecondaryTypes) > 0 {
			continue
		}
		out = append(out, g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := dateKey(out[i].FirstReleaseDate), dateKey(out[j].FirstReleaseDate)
		if di != dj {
			return di < dj
		}
		return out[i].Title < out[j].Title
	})
	return out, nil
}

// ReleaseGroupReleases lists every release (edition) of a release group,
// with their media formats and track counts.
func (c *Client) ReleaseGroupReleases(ctx context.Context, releaseGroupID string) ([]Release, error) {
	var out struct {
		Releases []Release `json:"releases"`
	}
	q := url.Values{"release-group": {releaseGroupID}, "inc": {"media"}, "limit": {"100"}}
	if err := c.get(ctx, "/ws/2/release", q, &out); err != nil {
		return nil, fmt.Errorf("list releases of release group %s: %w", releaseGroupID, err)
	}
	return out.Releases, nil
}

// GetRelease looks up one release with its tracklist.
func (c *Client) GetRelease(ctx context.Context, releaseID string) (Release, error) {
	var r Release
	q := url.Values{"inc": {"recordings+media"}}
	if err := c.get(ctx, "/ws/2/release/"+url.PathEscape(releaseID), q, &r); err != nil {
		return Release{}, fmt.Errorf("get release %s: %w", releaseID, err)
	}
	return r, nil
}

// CanonicalTracklist picks the canonical release of a release group (see
// CanonicalRelease) and returns it with its tracklist. Two requests.
func (c *Client) CanonicalTracklist(ctx context.Context, releaseGroupID string) (Release, error) {
	releases, err := c.ReleaseGroupReleases(ctx, releaseGroupID)
	if err != nil {
		return Release{}, err
	}
	pick, ok := CanonicalRelease(releases)
	if !ok {
		return Release{}, fmt.Errorf("release group %s has no releases: %w", releaseGroupID, ErrNotFound)
	}
	return c.GetRelease(ctx, pick.ID)
}

// CanonicalRelease picks the edition that stands for a release group: among
// the official releases (all of them when none is marked official), the
// earliest; releases from the same date are ordered by how common their
// country is among the candidates (the edition most of the world got), then
// by id so the choice is stable. ok is false for an empty list.
func CanonicalRelease(releases []Release) (Release, bool) {
	if len(releases) == 0 {
		return Release{}, false
	}
	var candidates []Release
	for _, r := range releases {
		if strings.EqualFold(r.Status, "official") {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		candidates = append(candidates, releases...)
	}
	countries := map[string]int{}
	for _, r := range candidates {
		if r.Country != "" {
			countries[r.Country]++
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if da, db := dateKey(a.Date), dateKey(b.Date); da != db {
			return da < db
		}
		if ca, cb := countries[a.Country], countries[b.Country]; ca != cb {
			return ca > cb
		}
		return a.ID < b.ID
	})
	return candidates[0], true
}

// CoverArtURL is the Cover Art Archive address of a release group's front
// cover, 500 pixels wide. The archive redirects to the image; a group
// without art answers 404.
func CoverArtURL(releaseGroupID string) string {
	if releaseGroupID == "" {
		return ""
	}
	return CoverArtBaseURL + "/release-group/" + url.PathEscape(releaseGroupID) + "/front-500"
}

// dateKey makes partial dates sortable, a precise date before a bare year
// or month of the same period ("2001-05-02" < "2001-05" < "2001"), unknown
// dates last.
func dateKey(d string) string {
	d = strings.TrimSpace(d)
	if d == "" {
		return "9999-99-99"
	}
	switch len(d) {
	case 4:
		return d + "-99-99"
	case 7:
		return d + "-99"
	}
	return d
}

func yearOf(d string) int {
	if len(d) < 4 {
		return 0
	}
	y := 0
	for i := 0; i < 4; i++ {
		if d[i] < '0' || d[i] > '9' {
			return 0 // strconv would also take a sign ("-123")
		}
		y = y*10 + int(d[i]-'0')
	}
	return y
}

// luceneEscape escapes the characters the MusicBrainz search syntax
// (Lucene) treats as operators, so a name such as "AC/DC" or "!!!" is
// searched as written.
func luceneEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`+-&|!(){}[]^"~*?:\/`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
