package migrate

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Medusa is read with GET /api/v2/series (paged), the key in the x-api-key
// header. A show has its ids, title, start year and, in config, its folder
// (location) and the paused flag.

type medusaSeries struct {
	ID struct {
		TVDB flexInt `json:"tvdb"`
		TMDB flexInt `json:"tmdb"`
		Slug string  `json:"slug"`
	} `json:"id"`
	Externals struct {
		TMDB flexInt `json:"tmdb"`
	} `json:"externals"`
	Title   string `json:"title"`
	Indexer string `json:"indexer"`
	Year    struct {
		Start flexInt `json:"start"`
	} `json:"year"`
	Config struct {
		Location string `json:"location"`
		Paused   bool   `json:"paused"`
	} `json:"config"`
}

// medusaPageSize is a variable so a test can page through a short list.
var medusaPageSize = 500

const medusaMaxPages = 40

func fetchMedusa(ctx context.Context, hc *http.Client, c Conn) ([]legacyShow, error) {
	var (
		shows []legacyShow
		seen  = map[string]bool{}
	)
	for page := 1; page <= medusaMaxPages; page++ {
		var list []medusaSeries
		q := url.Values{"page": {fmt.Sprint(page)}, "limit": {fmt.Sprint(medusaPageSize)}}
		if err := read(ctx, hc, c, call{path: "/api/v2/series", query: q, keyHeader: "x-api-key"}, &list); err != nil {
			return shows, err
		}
		added := 0
		for _, m := range list {
			key := m.ID.Slug
			if key == "" {
				key = fmt.Sprintf("%s:%d:%s", m.Indexer, m.ID.TVDB, m.Title)
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			added++
			shows = append(shows, m.show())
		}
		// A server that ignores paging repeats the list: stop when a page
		// brings nothing new.
		if len(list) < medusaPageSize || added == 0 {
			break
		}
	}
	return shows, nil
}

func (m medusaSeries) show() legacyShow {
	s := legacyShow{Title: strings.TrimSpace(m.Title), Year: int(m.Year.Start), Location: strings.TrimSpace(m.Config.Location), Paused: m.Config.Paused}
	switch strings.ToLower(m.Indexer) {
	case "tmdb":
		s.TMDBID = int(m.ID.TMDB)
	case "tvdb", "":
		s.TVDBID = int(m.ID.TVDB)
	}
	if s.TMDBID == 0 {
		s.TMDBID = int(m.Externals.TMDB)
	}
	return s
}
