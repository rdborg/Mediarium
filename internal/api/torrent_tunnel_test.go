package api

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/store"
)

// With no VPN connected, the kill switch decides: off means a direct
// connection, on means the download is refused. (A connected VPN is always
// used; that branch needs a real WireGuard tunnel and is not covered here.)
func TestTorrentTunnelWithoutVPN(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{ConfigDir: dir, DownloadsDir: dir, DownloadsIncomplete: dir, DownloadsComplete: dir, MoviesDir: dir}
	db, err := store.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	s, err := New(db, cfg, box, "", "test")
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	tests := []struct {
		name       string
		killSwitch string
		wantErr    bool
	}{
		{"kill switch off goes direct", "0", false},
		{"kill switch on refuses", "1", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.Settings.Set(settings.KeyVPNRequireForTorrents, tc.killSwitch, false); err != nil {
				t.Fatalf("set: %v", err)
			}
			tunnel, err := s.torrentTunnel()
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, want error: %v", err, tc.wantErr)
			}
			if tunnel != nil {
				t.Fatalf("expected no tunnel, got %v", tunnel)
			}
		})
	}
}
