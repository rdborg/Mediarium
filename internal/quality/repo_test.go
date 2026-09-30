package quality_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/store"
)

func newRepo(t *testing.T) *quality.Repo {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return quality.NewRepo(db)
}

func TestValidate(t *testing.T) {
	ok := quality.Profile{Name: "Mine", Allowed: []quality.Tier{quality.TierBluray1080p, quality.TierWebDL720p}, Cutoff: quality.TierBluray1080p}
	cases := []struct {
		name    string
		mutate  func(p *quality.Profile)
		wantErr bool
	}{
		{"valid", func(p *quality.Profile) {}, false},
		{"empty name", func(p *quality.Profile) { p.Name = "  " }, true},
		{"name too long", func(p *quality.Profile) { p.Name = string(make([]byte, 61)) }, true},
		{"no tiers", func(p *quality.Profile) { p.Allowed = nil }, true},
		{"made-up tier", func(p *quality.Profile) { p.Allowed = append(p.Allowed, "Bluray-8K") }, true},
		{"cutoff not allowed", func(p *quality.Profile) { p.Cutoff = quality.TierRemux2160p }, true},
		{"unknown tier is a valid choice", func(p *quality.Profile) {
			p.Allowed = append(p.Allowed, quality.TierUnknown)
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := ok
			p.Allowed = append([]quality.Tier(nil), ok.Allowed...)
			tc.mutate(&p)
			_, err := quality.Validate(p)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil && !errors.Is(err, quality.ErrInvalid) {
				t.Fatalf("validation errors must wrap ErrInvalid, got %v", err)
			}
		})
	}
}

func TestValidateOrdersAndDedupesTiers(t *testing.T) {
	got, err := quality.Validate(quality.Profile{
		Name:    "x",
		Allowed: []quality.Tier{quality.TierBluray1080p, quality.TierWebDL720p, quality.TierBluray1080p},
		Cutoff:  quality.TierBluray1080p,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Allowed) != 2 || got.Allowed[0] != quality.TierWebDL720p || got.Allowed[1] != quality.TierBluray1080p {
		t.Fatalf("allowed = %v, want worst-to-best without duplicates", got.Allowed)
	}
}

func TestRepoCRUDAndSeeding(t *testing.T) {
	repo := newRepo(t)

	seeded, err := repo.SeedPresets(0)
	if err != nil || len(seeded) != len(quality.Presets()) {
		t.Fatalf("seed: %v, %d profiles", err, len(seeded))
	}
	again, _ := repo.SeedPresets(0)
	if len(again) != len(seeded) {
		t.Fatalf("seeding twice must not duplicate: %d -> %d", len(seeded), len(again))
	}
	for i, want := range []string{"Cinema recordings", "720p", "1080p", "4K & over", "Any"} {
		if seeded[i].Name != want || seeded[i].UpgradeAllowed {
			t.Fatalf("seeded[%d] = %+v, want %q with upgrades off", i, seeded[i], want)
		}
	}

	created, err := repo.Create(quality.Profile{Name: "Remux only", Allowed: []quality.Tier{quality.TierRemux2160p}, Cutoff: quality.TierRemux2160p})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := repo.Create(quality.Profile{Name: "Remux only", Allowed: []quality.Tier{quality.TierRemux2160p}, Cutoff: quality.TierRemux2160p}); !errors.Is(err, quality.ErrInvalid) {
		t.Fatalf("duplicate name should be invalid, got %v", err)
	}

	created.Name = "Remux 4K"
	created.UpgradeAllowed = true
	updated, err := repo.Update(created)
	if err != nil || updated.Name != "Remux 4K" {
		t.Fatalf("update: %v %+v", err, updated)
	}
	got, err := repo.Get(created.ID)
	if err != nil || got.Name != "Remux 4K" || !got.UpgradeAllowed || got.Cutoff != quality.TierRemux2160p {
		t.Fatalf("get after update: %v %+v", err, got)
	}

	// The default profile can't be deleted; an unreferenced one can.
	if err := repo.Delete(created.ID, created.ID); !errors.Is(err, quality.ErrInUse) {
		t.Fatalf("deleting the default should be ErrInUse, got %v", err)
	}
	if err := repo.Delete(created.ID, seeded[0].ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}
