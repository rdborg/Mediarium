package api

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/rdborg/mediarium/internal/debrid"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/settings"
)

// Every mix of "what is set up" and "what the indexer asks for" for a torrent
// (#166, jasonmackie76): a download client and a debrid service are set up
// together, and an indexer's choice is followed whenever that place is ready.
func TestTorrentRouteMatrix(t *testing.T) {
	type setup struct{ debrid, client, builtin bool }
	routes := []string{"", indexers.RouteClient, indexers.RouteDebrid, indexers.RouteBuiltin}

	// The place a route names is used while it is set up; otherwise (and for
	// no choice) the usual order: client, debrid, built-in.
	want := func(su setup, route string) string {
		ready := map[string]bool{indexers.RouteClient: su.client, indexers.RouteDebrid: su.debrid, indexers.RouteBuiltin: su.builtin}
		if route != "" && ready[route] {
			return route
		}
		switch {
		case su.client:
			return indexers.RouteClient
		case su.debrid:
			return indexers.RouteDebrid
		}
		return indexers.RouteBuiltin
	}

	// A few cases written out by hand, so the rule above can't drift unnoticed.
	fixed := map[string]string{
		"debrid+client+builtin|":        indexers.RouteClient,
		"debrid+client+builtin|client":  indexers.RouteClient,
		"debrid+client+builtin|debrid":  indexers.RouteDebrid,
		"debrid+client+builtin|builtin": indexers.RouteBuiltin,
		"debrid+client|debrid":          indexers.RouteDebrid,
		"debrid+client|client":          indexers.RouteClient,
		"debrid|client":                 indexers.RouteDebrid,
		"client|debrid":                 indexers.RouteClient,
		"|builtin":                      indexers.RouteBuiltin,
	}

	s := newBareServer(t)
	ids := map[string]sql.NullInt64{}
	for _, r := range routes {
		inst, err := s.IndexerRepo.Create(indexers.Instance{Name: "idx-" + r, BaseURL: "https://idx-" + r + ".test", Protocol: indexers.ProtocolTorrent, Enabled: true, TorrentRoute: r}, nil)
		if err != nil {
			t.Fatal(err)
		}
		ids[r] = sql.NullInt64{Int64: inst.ID, Valid: true}
	}
	put := func(k, v string) {
		t.Helper()
		if err := s.Settings.Set(k, v, false); err != nil {
			t.Fatal(err)
		}
	}

	for mask := 0; mask < 8; mask++ {
		su := setup{debrid: mask&1 != 0, client: mask&2 != 0, builtin: mask&4 != 0}
		put(settings.KeyDownloadClientKind, "")
		put(settings.KeyDownloadClientURL, "")
		put(settings.KeyDebridService, "")
		put(settings.KeyDebridAPIKey, "")
		put(settings.KeyTorrentEnabled, "0")
		name := ""
		add := func(part string) {
			if name != "" {
				name += "+"
			}
			name += part
		}
		if su.debrid {
			put(settings.KeyDebridService, debrid.RealDebrid)
			put(settings.KeyDebridAPIKey, "k")
			add("debrid")
		}
		if su.client {
			put(settings.KeyDownloadClientKind, "qbittorrent")
			put(settings.KeyDownloadClientURL, "http://127.0.0.1:1")
			add("client")
		}
		if su.builtin {
			put(settings.KeyTorrentEnabled, "1")
			add("builtin")
		}
		for _, r := range routes {
			w := want(su, r)
			if f, ok := fixed[name+"|"+r]; ok && f != w {
				t.Fatalf("the rule disagrees with the written-out case %s|%s: %q vs %q", name, r, w, f)
			}
			t.Run(fmt.Sprintf("%s|%s", name, r), func(t *testing.T) {
				if got := s.torrentRouteFor(ids[r]); got != w {
					t.Errorf("got %q, want %q", got, w)
				}
			})
		}
		// A torrent with no known indexer takes the usual order.
		if got, w := s.torrentRouteFor(sql.NullInt64{}), want(su, ""); got != w {
			t.Errorf("%s, no indexer: got %q, want %q", name, got, w)
		}
	}
}
