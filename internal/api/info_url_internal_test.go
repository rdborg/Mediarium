package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
)

// The release list gets the page of a release on its indexer, never a link that
// carries a key, and never the download link.
func TestSearchPayloadInfoURL(t *testing.T) {
	s := cacheServer(t, newFakeTorBoxCache(t, hashA), "torbox", nil)
	s.offered = offeredURLs{}
	tests := []struct {
		name string
		res  indexers.Result
		want string
	}{
		{"torrent page", indexers.Result{Title: "A.2024.1080p-GRP", DownloadURL: "https://t.example/dl/1.torrent?passkey=SECRET", InfoURL: "https://t.example/details/1", Protocol: indexers.ProtocolTorrent}, "https://t.example/details/1"},
		{"usenet page", indexers.Result{Title: "B.2024.1080p-GRP", DownloadURL: "https://n.example/getnzb/1.nzb&r=SECRET", InfoURL: "https://n.example/details/1#comments", Protocol: indexers.ProtocolUsenet}, "https://n.example/details/1"},
		{"page with a key", indexers.Result{Title: "C.2024.1080p-GRP", DownloadURL: "https://n.example/x.nzb", InfoURL: "https://n.example/api?t=details&id=1&apikey=SECRET"}, ""},
		{"no page", indexers.Result{Title: "D.2024.1080p-GRP", DownloadURL: "https://n.example/x.nzb"}, ""},
		{"soulseek has none", indexers.Result{Title: "E", DownloadURL: "slsk://user/file", Protocol: indexers.ProtocolSoulseek}, ""},
		{"the download link", indexers.Result{Title: "F.2024.1080p-GRP", DownloadURL: "https://t.example/t/9", InfoURL: "https://t.example/t/9"}, ""},
		{"a magnet", indexers.Result{Title: "G.2024.1080p-GRP", DownloadURL: "magnet:?xt=urn:btih:abc", InfoURL: "magnet:?xt=urn:btih:abc"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := s.toSearchResultPayload(tc.res)
			if p.InfoURL != tc.want {
				t.Errorf("infoUrl = %q, want %q", p.InfoURL, tc.want)
			}
			body, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), "SECRET") {
				t.Errorf("the payload leaks a key: %s", body)
			}
			if (tc.want != "") != strings.Contains(string(body), `"infoUrl"`) {
				t.Errorf("infoUrl in the JSON should be left out when empty: %s", body)
			}
		})
	}
}
