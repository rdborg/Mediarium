package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/store"
	"github.com/rdborg/mediarium/internal/vpn"
)

func newBareServer(t *testing.T) *Server {
	t.Helper()
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
	t.Cleanup(s.VPNManager.Close)
	return s
}

// saveQuietVPN stores a valid connection whose server never answers and marks
// it switched on, as it would be after a person pressed Activate.
func saveQuietVPN(t *testing.T, s *Server, label string) int64 {
	t.Helper()
	mine, err := vpn.GenerateKeyPair()
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	theirs, err := vpn.GenerateKeyPair()
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	stored, err := s.VPNRepo.Create(label, "custom", vpn.Config{
		PrivateKey:     mine.PrivateKey,
		PeerPublicKey:  theirs.PublicKey,
		Endpoint:       "127.0.0.1:1",
		AllowedIPs:     []string{"0.0.0.0/0"},
		LocalAddresses: []string{"10.77.0.2/32"},
	})
	if err != nil {
		t.Fatalf("create vpn config: %v", err)
	}
	if err := s.VPNRepo.SetActive(stored.ID); err != nil {
		t.Fatalf("set active: %v", err)
	}
	return stored.ID
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func vpnStatusNow(t *testing.T, s *Server) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleVPNStatus(rec, httptest.NewRequest(http.MethodGet, "/api/vpn/status", nil))
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode status %q: %v", rec.Body.String(), err)
	}
	return out
}

// After a restart the connection that was switched on is brought back by
// itself, and until its server has answered the status never says connected.
func TestRestartBringsTheActiveVPNBack(t *testing.T) {
	s := newBareServer(t)
	saveQuietVPN(t, s, "Home VPN")

	// The state right after a restart, before anything is done.
	if s.VPNManager.Wanted() {
		t.Fatal("a fresh manager should have nothing selected")
	}

	s.restoreVPN()

	if !s.VPNManager.Wanted() {
		t.Fatal("the active connection was not selected again after the restart")
	}
	waitFor(t, "the tunnel to come up", func() bool { return s.VPNManager.Tunnel() != nil })

	got := vpnStatusNow(t, s)
	if got["connected"] != false {
		t.Fatalf("no server answered, but status says connected: %v", got)
	}
	if got["label"] != "Home VPN" || got["state"] != "connecting" {
		t.Fatalf("status = %v, want Home VPN connecting", got)
	}
}

// A connection that is switched on but not up must keep torrents from going
// out directly, even with the kill switch off.
func TestTorrentsWaitForASwitchedOnVPN(t *testing.T) {
	s := newBareServer(t)
	if err := s.Settings.Set(settings.KeyVPNRequireForTorrents, "0", false); err != nil {
		t.Fatalf("set: %v", err)
	}

	s.VPNManager.Broken("Home VPN", "Couldn't find the VPN server. Check its address and your internet connection.")
	tunnel, err := s.torrentTunnel()
	if err == nil || tunnel != nil {
		t.Fatalf("torrentTunnel = %v, %v; want a refusal while the VPN is switched on but down", tunnel, err)
	}
	for _, want := range []string{"Home VPN", "Disconnect", "Couldn't find the VPN server"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}

	// Switched off again: no VPN selected and the kill switch is off, so direct.
	s.VPNManager.Deactivate()
	if tunnel, err := s.torrentTunnel(); err != nil || tunnel != nil {
		t.Fatalf("after disconnecting: %v, %v; want a direct connection", tunnel, err)
	}
}

func TestDownloadsStatusBlockedWhileVPNIsDown(t *testing.T) {
	s := newBareServer(t)
	s.VPNManager.Broken("Home VPN", "Nope.")
	rec := httptest.NewRecorder()
	s.handleDownloadsStatus(rec, httptest.NewRequest(http.MethodGet, "/api/downloads/status", nil))
	var out struct {
		Torrent struct {
			Ready        bool   `json:"ready"`
			BlockedByVPN bool   `json:"blockedByVpn"`
			VPNState     string `json:"vpnState"`
			State        string `json:"state"`
		} `json:"torrent"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.Torrent.BlockedByVPN || out.Torrent.Ready || out.Torrent.State != "blocked" || out.Torrent.VPNState != "down" {
		t.Fatalf("torrent status = %+v, want blocked by a down VPN", out.Torrent)
	}
}

// Removing the connection that is switched on also switches it off.
func TestRemovingTheActiveVPNDisconnectsIt(t *testing.T) {
	s := newBareServer(t)
	id := saveQuietVPN(t, s, "Home VPN")
	s.restoreVPN()
	waitFor(t, "the tunnel to come up", func() bool { return s.VPNManager.Tunnel() != nil })

	req := httptest.NewRequest(http.MethodDelete, "/api/vpn/configs/"+strconv.FormatInt(id, 10), nil)
	req.SetPathValue("id", strconv.FormatInt(id, 10))
	rec := httptest.NewRecorder()
	s.handleDeleteVPNConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d: %s", rec.Code, rec.Body.String())
	}
	if s.VPNManager.Tunnel() != nil || s.VPNManager.Wanted() {
		t.Fatal("the tunnel kept running after its connection was removed")
	}
}

func TestSavedVPNThatCantBeReadShowsAsDown(t *testing.T) {
	s := newBareServer(t)
	saveQuietVPN(t, s, "Home VPN")
	// A different key can't decrypt what was saved.
	other, err := crypto.LoadOrCreateKey(filepath.Join(t.TempDir(), "other.key"))
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	s.VPNRepo = vpn.NewRepo(s.db, other)

	s.restoreVPN()
	got := vpnStatusNow(t, s)
	if got["connected"] != false || got["state"] != "down" || got["reason"] == "" {
		t.Fatalf("status = %v, want down with a reason", got)
	}
	if _, err := s.torrentTunnel(); err == nil {
		t.Fatal("torrents must wait while the saved VPN can't be read")
	}
}
