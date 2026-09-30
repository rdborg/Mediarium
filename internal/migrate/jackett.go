package migrate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/rdborg/mediarium/internal/indexers"
)

// Jackett is read with GET /api/v2.0/indexers?configured=true, the API key
// in the apikey query parameter (that is how Jackett accepts it). Every
// configured indexer becomes a Torznab indexer pointing at Jackett's own
// feed for it, which is how Jackett is meant to be used; when Mediarium has
// a site definition with the same id, the site can be added directly
// instead (JackettConn.Direct lists those ids).

type jackettCap struct {
	ID   flexInt `json:"ID"`
	Name string  `json:"Name"`
}

type jackettIndexer struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Type        string       `json:"type"` // public, private, semi-private
	Configured  *bool        `json:"configured"`
	SiteLink    string       `json:"site_link"`
	Caps        []jackettCap `json:"caps"`
	LastError   string       `json:"last_error"`
	Description string       `json:"description"`
}

type jackettData struct {
	Version  string
	Base     string // Jackett address as entered, no trailing slash
	Key      string // its API key, used as the Torznab key of every indexer
	Indexers []jackettIndexer
}

func fetchJackett(ctx context.Context, hc *http.Client, c JackettConn) (jackettData, error) {
	var d jackettData
	base, err := c.baseURL()
	if err != nil {
		return d, err
	}
	d.Base = base.String()
	d.Key = strings.TrimSpace(c.APIKey)
	err = read(ctx, hc, c.Conn, call{path: "/api/v2.0/indexers", query: url.Values{"configured": {"true"}}, keyQuery: "apikey"}, &d.Indexers)
	if err != nil {
		if strings.Contains(err.Error(), "expected data") {
			return d, fmt.Errorf("%w; if Jackett has an admin password, it may not give the indexer list to the API key alone", err)
		}
		return d, err
	}
	return d, nil
}

// torznabURL is Jackett's feed for one indexer, without the trailing /api
// Mediarium adds itself.
func (d jackettData) torznabURL(id string) string {
	return d.Base + "/api/v2.0/indexers/" + url.PathEscape(id) + "/results/torznab"
}

func (x jackettIndexer) categoryIDs() []int {
	var out []int
	for _, c := range x.Caps {
		if c.ID > 0 {
			out = append(out, int(c.ID))
		}
	}
	return out
}

func (im *Importer) planJackett(ctx context.Context, p *plan, ip *IndexersPreview, d *jackettData, direct []string, seen seenIndexers) {
	want := map[string]bool{}
	for _, id := range direct {
		if id = strings.ToLower(strings.TrimSpace(id)); id != "" {
			want[id] = true
		}
	}
	for _, x := range d.Indexers {
		if x.Configured != nil && !*x.Configured {
			continue
		}
		ipl := im.planJackettIndexer(ctx, d, x, want[strings.ToLower(x.ID)], seen)
		ip.Summary.count(ipl.item.Action)
		ip.Items = append(ip.Items, ipl.item)
		p.indexers = append(p.indexers, ipl)
	}
}

// siteFor finds Mediarium's supported site definition with the id of a
// Jackett indexer; ok is false when there is none (or the site list is not
// available).
func (im *Importer) siteFor(ctx context.Context, id string) (indexers.DefinitionSummary, bool) {
	if im.deps.Definition == nil || id == "" {
		return indexers.DefinitionSummary{}, false
	}
	sum, err := im.deps.Definition(ctx, id)
	if errors.Is(err, indexers.ErrDefinitionNotFound) && strings.ToLower(id) != id {
		sum, err = im.deps.Definition(ctx, strings.ToLower(id))
	}
	if err != nil || !sum.Supported || len(sum.Links) == 0 {
		return indexers.DefinitionSummary{}, false
	}
	return sum, true
}

func (im *Importer) planJackettIndexer(ctx context.Context, d *jackettData, x jackettIndexer, wantDirect bool, seen seenIndexers) indexerPlan {
	name := strings.TrimSpace(x.Name)
	if name == "" {
		name = x.ID
	}
	item := IndexerItem{
		Name: name, Implementation: "Torznab", Protocol: string(indexers.ProtocolTorrent), Enabled: true,
		Categories: x.categoryIDs(), SourceID: x.ID,
	}
	ipl := indexerPlan{}
	sum, canDirect := im.siteFor(ctx, x.ID)
	item.CanAddDirectly = canDirect

	if canDirect && wantDirect {
		item.Direct, item.Implementation, item.DefinitionID, item.BaseURL = true, "Cardigann", sum.ID, sum.Links[0]
		private := !strings.EqualFold(x.Type, "public")
		switch {
		case seen.def[strings.ToLower(sum.ID)]:
			item.Action, item.Reason = ActionExists, "already added (same site)"
		default:
			seen.def[strings.ToLower(sum.ID)] = true
			item.Action = ActionAdd
			item.Reason = "added as the site itself, not through Jackett"
			if private {
				ipl.masked = true
				item.Reason += "; it is a private tracker, so it is added switched off: enter your login under Settings > Indexers & Search"
			}
			ipl.inst = indexers.Instance{
				Name: name, Kind: indexers.KindCardigann, DefinitionID: sum.ID, BaseURL: sum.Links[0],
				Protocol: indexers.ProtocolTorrent, Enabled: !private, Settings: map[string]string{},
			}
			ipl.secret = sum.IsSecret
		}
		ipl.item = item
		return ipl
	}

	addr := d.torznabURL(x.ID)
	item.BaseURL = addr
	switch {
	case seen.url[normURL(addr)]:
		item.Action, item.Reason = ActionExists, "already added (same address)"
	default:
		seen.url[normURL(addr)] = true
		item.Action = ActionAdd
		item.Reason = "added through Jackett's Torznab feed, with Jackett's API key"
		if canDirect {
			item.Reason += "; Mediarium's site list also has " + sum.Name + ", which can be added as the site itself instead (list its id \"" + x.ID + "\" in jackett.direct)"
		} else if wantDirect {
			item.Reason += "; Mediarium's site list has no " + x.ID + ", so it stays on Jackett"
		}
		if x.LastError != "" {
			item.Reason += "; Jackett reports an error for it: " + x.LastError
		}
		ipl.inst = indexers.Instance{Name: name, Kind: indexers.KindTorznab, BaseURL: addr, APIKey: d.Key, Protocol: indexers.ProtocolTorrent, Enabled: true}
	}
	ipl.item = item
	return ipl
}
