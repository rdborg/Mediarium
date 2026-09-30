package vpn_test

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/store"
	"github.com/rdborg/mediarium/internal/vpn"
)

func newTestRepo(t *testing.T) *vpn.Repo {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	return vpn.NewRepo(db, box)
}

func TestRepoCreateListActivate(t *testing.T) {
	repo := newTestRepo(t)

	cfgA, err := repo.Create("Provider A", "custom", vpn.Config{
		PrivateKey: "priv-a-secret", PeerPublicKey: "pub-a", Endpoint: "vpn-a.example:51820",
		AllowedIPs: []string{"0.0.0.0/0"}, LocalAddresses: []string{"10.2.0.2/32"},
	})
	if err != nil {
		t.Fatalf("create config A: %v", err)
	}
	cfgB, err := repo.Create("Provider B", "mullvad", vpn.Config{
		PrivateKey: "priv-b-secret", PeerPublicKey: "pub-b", Endpoint: "vpn-b.example:51820",
		AllowedIPs: []string{"0.0.0.0/0"}, LocalAddresses: []string{"10.2.0.3/32"},
	})
	if err != nil {
		t.Fatalf("create config B: %v", err)
	}

	list, err := repo.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(list))
	}
	for _, c := range list {
		if c.Config.PrivateKey == "" {
			t.Fatalf("expected decrypted private key round-trip, got empty for %s", c.Label)
		}
	}

	if _, ok, err := repo.Active(); err != nil || ok {
		t.Fatalf("expected no active config initially, got ok=%v err=%v", ok, err)
	}

	if err := repo.SetActive(cfgA.ID); err != nil {
		t.Fatalf("set active A: %v", err)
	}
	active, ok, err := repo.Active()
	if err != nil || !ok {
		t.Fatalf("expected an active config, got ok=%v err=%v", ok, err)
	}
	if active.ID != cfgA.ID || active.Config.PrivateKey != "priv-a-secret" {
		t.Fatalf("expected config A active with decrypted key, got %+v", active)
	}

	// Switching active configs should deactivate the previous one.
	if err := repo.SetActive(cfgB.ID); err != nil {
		t.Fatalf("set active B: %v", err)
	}
	active, ok, err = repo.Active()
	if err != nil || !ok || active.ID != cfgB.ID {
		t.Fatalf("expected config B active, got %+v ok=%v err=%v", active, ok, err)
	}

	if err := repo.Deactivate(); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if _, ok, err := repo.Active(); err != nil || ok {
		t.Fatalf("expected no active config after deactivate, got ok=%v err=%v", ok, err)
	}

	if err := repo.Delete(cfgA.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, _ = repo.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 config after delete, got %d", len(list))
	}
}
