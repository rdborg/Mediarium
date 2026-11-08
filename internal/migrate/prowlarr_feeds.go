package migrate

import (
	"context"
	"net/http"
	"strings"

	"github.com/rdborg/mediarium/internal/indexers"
)

// ProwlarrFeed is one of a running Prowlarr's indexers, as the feed Prowlarr
// serves for it (V4 D16, #134): Mediarium searches through Prowlarr, so every
// indexer Prowlarr has works, C#-only ones included.
type ProwlarrFeed struct {
	ID         int
	Name       string
	Protocol   indexers.Protocol
	Enabled    bool
	Priority   int
	MinSeeders int
	FeedURL    string // Prowlarr's address for this indexer's feed, without /api
	APIKey     string // Prowlarr's own key, which its feeds take
}

// ProwlarrFeeds reads a running Prowlarr's indexers. hc may be nil for the
// usual client (local addresses allowed, a timeout).
func ProwlarrFeeds(ctx context.Context, hc *http.Client, c Conn) ([]ProwlarrFeed, error) {
	if hc == nil {
		hc = newHTTPClient()
	}
	d, err := fetchProwlarr(ctx, hc, c)
	if err != nil {
		return nil, err
	}
	out := make([]ProwlarrFeed, 0, len(d.Indexers))
	for _, x := range d.Indexers {
		if x.ID <= 0 {
			continue
		}
		p := indexers.ProtocolUsenet
		if strings.EqualFold(x.Protocol, string(indexers.ProtocolTorrent)) {
			p = indexers.ProtocolTorrent
		}
		out = append(out, ProwlarrFeed{
			ID: x.ID, Name: strings.TrimSpace(x.Name), Protocol: p, Enabled: x.Enable, Priority: x.Priority,
			MinSeeders: x.minSeeders(), FeedURL: d.feedURL(x.ID), APIKey: d.Key,
		})
	}
	return out, nil
}
