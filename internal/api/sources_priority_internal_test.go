package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
)

func TestPreferredIndexerWinsATie(t *testing.T) {
	u := indexers.ProtocolUsenet
	tor := indexers.ProtocolTorrent
	cases := []struct {
		name            string
		best, candidate indexers.Result
		want            bool
	}{
		{"a preferred indexer beats a normal one", indexers.Result{Protocol: u, Priority: 2}, indexers.Result{Protocol: u, Priority: 1}, true},
		{"a last-resort indexer never replaces a normal one", indexers.Result{Protocol: tor, Priority: 2}, indexers.Result{Protocol: u, Priority: 3}, false},
		{"priority goes before Usenet over torrents", indexers.Result{Protocol: u, Priority: 2}, indexers.Result{Protocol: tor, Priority: 1}, true},
		{"same priority: Usenet wins over a torrent", indexers.Result{Protocol: tor, Priority: 2}, indexers.Result{Protocol: u, Priority: 2}, true},
		{"not set counts as normal", indexers.Result{Protocol: tor}, indexers.Result{Protocol: u, Priority: 2}, true},
	}
	for _, tc := range cases {
		if got := preferUsenet(&tc.best, &tc.candidate); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
