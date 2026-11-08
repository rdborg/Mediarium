package musicbrainz

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Searching by album or song (#143), for the music search: an album found by
// its name, or the albums a song is on (a soundtrack found by a song from it).

// FoundAlbum is a release group a search found, with its main artist. Song is
// set when it was found through one of its songs.
type FoundAlbum struct {
	ReleaseGroup
	ArtistID   string
	ArtistName string
	Song       string
	Score      int
}

func mainArtist(credits []struct {
	Name   string `json:"name"`
	Artist struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
}) (id, name string) {
	if len(credits) == 0 {
		return "", ""
	}
	name = credits[0].Artist.Name
	if name == "" {
		name = credits[0].Name
	}
	return credits[0].Artist.ID, name
}

// SearchAlbums finds albums, EPs and singles by title, best match first.
func (c *Client) SearchAlbums(ctx context.Context, title string, limit int) ([]FoundAlbum, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return []FoundAlbum{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	q := url.Values{"query": {"releasegroup:(" + luceneEscape(title) + ")"}, "limit": {strconv.Itoa(limit)}}
	var out struct {
		Groups []TaggedReleaseGroup `json:"release-groups"`
	}
	if err := c.get(ctx, "/ws/2/release-group", q, &out); err != nil {
		return nil, fmt.Errorf("search albums %q: %w", title, err)
	}
	found := make([]FoundAlbum, 0, len(out.Groups))
	for _, g := range out.Groups {
		id, name := mainArtist(g.ArtistCredit)
		found = append(found, FoundAlbum{ReleaseGroup: g.ReleaseGroup, ArtistID: id, ArtistName: name, Score: g.Score})
	}
	return found, nil
}

// SearchSongs finds songs by title and returns the albums they are on, each
// once, best match first.
func (c *Client) SearchSongs(ctx context.Context, title string, limit int) ([]FoundAlbum, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return []FoundAlbum{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	q := url.Values{"query": {"recording:(" + luceneEscape(title) + ")"}, "limit": {strconv.Itoa(limit)}}
	var out struct {
		Recordings []struct {
			Title        string `json:"title"`
			Score        int    `json:"score"`
			ArtistCredit []struct {
				Name   string `json:"name"`
				Artist struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"artist"`
			} `json:"artist-credit"`
			Releases []struct {
				Status       string       `json:"status"`
				Date         string       `json:"date"`
				ReleaseGroup ReleaseGroup `json:"release-group"`
			} `json:"releases"`
		} `json:"recordings"`
	}
	if err := c.get(ctx, "/ws/2/recording", q, &out); err != nil {
		return nil, fmt.Errorf("search songs %q: %w", title, err)
	}
	seen := map[string]bool{}
	found := []FoundAlbum{}
	for _, rec := range out.Recordings {
		id, name := mainArtist(rec.ArtistCredit)
		for _, rel := range rec.Releases {
			g := rel.ReleaseGroup
			if g.ID == "" || seen[g.ID] || (rel.Status != "" && rel.Status != "Official") {
				continue
			}
			seen[g.ID] = true
			if g.FirstReleaseDate == "" {
				g.FirstReleaseDate = rel.Date
			}
			found = append(found, FoundAlbum{ReleaseGroup: g, ArtistID: id, ArtistName: name, Song: rec.Title, Score: rec.Score})
		}
	}
	return found, nil
}
