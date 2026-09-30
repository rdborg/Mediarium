package migrate

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/url"
	"strings"

	"github.com/rdborg/mediarium/internal/indexers"
)

// NZBHydra2 presents everything it searches as ONE Newznab endpoint
// (<url>/api) and, for its torrent indexers, one Torznab endpoint
// (<url>/torznab/api). The importer confirms the API key with the endpoint's
// t=caps answer and adds the endpoints as indexers. Hydra's own indexer
// list is not read: it is behind its admin login, which the API key does not
// open, so the preview shows the aggregate entry only.

type newznabCaps struct {
	XMLName xml.Name `xml:"caps"`
	Server  struct {
		Title   string `xml:"title,attr"`
		Version string `xml:"version,attr"`
	} `xml:"server"`
	Categories struct {
		Category []struct {
			ID     int `xml:"id,attr"`
			Subcat []struct {
				ID int `xml:"id,attr"`
			} `xml:"subcat"`
		} `xml:"category"`
	} `xml:"categories"`
}

func (c newznabCaps) categoryIDs() []int {
	var out []int
	for _, cat := range c.Categories.Category {
		out = append(out, cat.ID)
		for _, s := range cat.Subcat {
			out = append(out, s.ID)
		}
	}
	return out
}

type hydraData struct {
	Base    string // NZBHydra2 address without a trailing /api
	Key     string
	Torznab bool // the owner asked for the Torznab endpoint too
	Newznab newznabCaps
}

// hydraBase normalises the address: an entered ".../api" or ".../torznab/api"
// is cut back to Hydra's own address.
func hydraBase(c Conn) (string, error) {
	u, err := c.baseURL()
	if err != nil {
		return "", err
	}
	p := strings.TrimSuffix(u.Path, "/api")
	p = strings.TrimSuffix(p, "/torznab")
	u.Path = p
	return u.String(), nil
}

func fetchNZBHydra(ctx context.Context, hc *http.Client, c HydraConn) (hydraData, error) {
	d := hydraData{Torznab: c.Torznab, Key: strings.TrimSpace(c.APIKey)}
	base, err := hydraBase(c.Conn)
	if err != nil {
		return d, err
	}
	d.Base = base
	err = read(ctx, hc, Conn{URL: base, APIKey: c.APIKey}, call{path: "/api", query: url.Values{"t": {"caps"}}, keyQuery: "apikey", xml: true}, &d.Newznab)
	return d, err
}

func (im *Importer) planNZBHydra(p *plan, ip *IndexersPreview, d *hydraData, seen seenIndexers) {
	type endpoint struct {
		name, addr string
		kind       indexers.Kind
		protocol   indexers.Protocol
		impl       string
	}
	eps := []endpoint{
		{"NZBHydra2", d.Base, indexers.KindNewznab, indexers.ProtocolUsenet, "Newznab"},
		{"NZBHydra2 (torrents)", d.Base + "/torznab", indexers.KindTorznab, indexers.ProtocolTorrent, "Torznab"},
	}
	for _, e := range eps {
		item := IndexerItem{
			Name: e.name, Implementation: e.impl, Protocol: string(e.protocol), BaseURL: e.addr, Enabled: true,
			Categories: d.Newznab.categoryIDs(),
		}
		ipl := indexerPlan{}
		switch {
		case e.kind == indexers.KindTorznab && !d.Torznab:
			item.Action, item.Reason = ActionSkip, "NZBHydra2 has a separate Torznab endpoint for its torrent indexers; set nzbhydra.torznab to true if it has any"
		case seen.url[normURL(e.addr)]:
			item.Action, item.Reason = ActionExists, "already added (same address)"
		default:
			seen.url[normURL(e.addr)] = true
			item.Action = ActionAdd
			item.Reason = "one endpoint for every indexer NZBHydra2 searches, with NZBHydra2's API key"
			if e.kind == indexers.KindTorznab {
				item.Reason = "NZBHydra2's torrent indexers, as one Torznab endpoint with NZBHydra2's API key"
			}
			ipl.inst = indexers.Instance{Name: e.name, Kind: e.kind, BaseURL: e.addr, APIKey: d.Key, Protocol: e.protocol, Enabled: true}
		}
		ipl.item = item
		ip.Summary.count(item.Action)
		ip.Items = append(ip.Items, ipl.item)
		p.indexers = append(p.indexers, ipl)
	}
}
