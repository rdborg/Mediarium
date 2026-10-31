package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// Prowlarr's Torznab download endpoint answers with a redirect to a magnet
// link. net/http can't follow that as a request, so torrentSource has to take
// the redirect target as the magnet instead of failing on the scheme.
func TestTorrentSourceFollowsMagnetRedirects(t *testing.T) {
	const magnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Release"
	torrent := []byte("d4:infod6:lengthi1e4:name3:abcee")

	mux := http.NewServeMux()
	mux.HandleFunc("/magnet", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, magnet, http.StatusMovedPermanently)
	})
	mux.HandleFunc("/via", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/magnet", http.StatusFound)
	})
	mux.HandleFunc("/release.torrent", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_, _ = w.Write(torrent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tests := []struct {
		name       string
		path       string
		wantMagnet string
		wantFile   bool
	}{
		{"a redirect to a magnet", "/magnet", magnet, false},
		{"an ordinary redirect on the way to a magnet", "/via", magnet, false},
		{"a normal torrent download", "/release.torrent", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			destDir := t.TempDir()
			gotMagnet, gotFile, err := (&Server{}).torrentSource(context.Background(), srv.URL+tc.path, destDir)
			if err != nil {
				t.Fatalf("torrentSource: %v", err)
			}
			if gotMagnet != tc.wantMagnet {
				t.Errorf("magnet = %q, want %q", gotMagnet, tc.wantMagnet)
			}
			if tc.wantFile {
				if gotFile == "" {
					t.Fatal("no .torrent file was saved")
				}
				data, err := os.ReadFile(gotFile)
				if err != nil {
					t.Fatalf("read the saved torrent: %v", err)
				}
				if !bytes.Equal(data, torrent) {
					t.Errorf("saved torrent = %q, want %q", data, torrent)
				}
				return
			}
			if gotFile != "" {
				t.Errorf("torrent file = %q, want none when the link ends at a magnet", gotFile)
			}
		})
	}
}
