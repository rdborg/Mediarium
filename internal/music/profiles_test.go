package music_test

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/music"
)

func TestValidateProfile(t *testing.T) {
	good := music.Profile{Name: " Mine ", Allowed: []music.Tier{music.TierFLAC24, music.TierFLAC, music.TierFLAC}, Cutoff: music.TierFLAC}
	cases := []struct {
		name    string
		mutate  func(p *music.Profile)
		wantErr string
	}{
		{"valid", func(p *music.Profile) {}, ""},
		{"empty name", func(p *music.Profile) { p.Name = "   " }, "name must be"},
		{"long name", func(p *music.Profile) { p.Name = strings.Repeat("x", 61) }, "name must be"},
		{"no tiers", func(p *music.Profile) { p.Allowed = nil }, "at least one"},
		{"unknown tier", func(p *music.Profile) { p.Allowed = append(p.Allowed, "MP3-999") }, "not a quality tier"},
		{"cutoff not allowed", func(p *music.Profile) { p.Cutoff = music.TierMP3320 }, "cutoff must be one of"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := good
			p.Allowed = append([]music.Tier(nil), good.Allowed...)
			tc.mutate(&p)
			got, err := music.ValidateProfile(p)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got.Name != "Mine" || len(got.Allowed) != 2 || got.Allowed[0] != music.TierFLAC || got.Allowed[1] != music.TierFLAC24 {
					t.Fatalf("tiers should be de-duplicated and ordered worst to best: %+v", got)
				}
				return
			}
			if !errors.Is(err, music.ErrProfileInvalid) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want an invalid-profile error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestProfileLifecycle(t *testing.T) {
	r := newRepo(t)
	if err := r.SeedPresets(); err != nil {
		t.Fatal(err)
	}
	seeded, _ := r.ListProfiles()
	lossy, lossless := seeded[0], seeded[1]

	created, err := r.CreateProfile(music.Profile{Name: "Anything", Allowed: []music.Tier{music.TierMP3192, music.TierFLAC}, Cutoff: music.TierFLAC,
		Fallback: []int64{lossy.ID, lossy.ID, 9999, 0}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(created.Fallback) != 1 || created.Fallback[0] != lossy.ID {
		t.Fatalf("fallback keeps only existing profiles, once: %v", created.Fallback)
	}
	if _, err := r.CreateProfile(music.Profile{Name: "Anything", Allowed: []music.Tier{music.TierFLAC}, Cutoff: music.TierFLAC}); !errors.Is(err, music.ErrProfileInvalid) {
		t.Fatalf("a duplicate name is a user error: %v", err)
	}

	created.Name, created.UpgradeAllowed = "Anything goes", true
	updated, err := r.UpdateProfile(created)
	if err != nil || updated.Name != "Anything goes" {
		t.Fatalf("update: %+v %v", updated, err)
	}
	if _, err := r.UpdateProfile(music.Profile{ID: 9999, Name: "x", Allowed: []music.Tier{music.TierFLAC}, Cutoff: music.TierFLAC}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown profile: %v", err)
	}
	if got, err := r.GetProfile(created.ID); err != nil || got.Name != "Anything goes" || !got.UpgradeAllowed {
		t.Fatalf("get: %+v %v", got, err)
	}

	// In use by an artist: cannot be deleted, and the count shows it.
	artist, _, err := r.AddArtist(music.Artist{MBID: "mb-1", Name: "A", Monitored: true, ProfileID: created.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if use, _ := r.ProfileUsage(); use[created.ID] != 1 {
		t.Fatalf("usage: %v", use)
	}
	if err := r.DeleteProfile(created.ID, lossless.ID); !errors.Is(err, music.ErrProfileInUse) {
		t.Fatalf("a profile in use cannot go: %v", err)
	}
	if err := r.DeleteProfile(lossless.ID, lossless.ID); !errors.Is(err, music.ErrProfileInUse) {
		t.Fatalf("the default cannot go: %v", err)
	}
	if err := r.SetArtistProfile(artist.ID, 0); err != nil {
		t.Fatal(err)
	}
	if err := r.SetArtistProfile(9999, 0); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown artist: %v", err)
	}

	// Deleting the lossy profile takes it out of the others' fallback chains.
	if err := r.DeleteProfile(created.ID, lossless.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := r.DeleteProfile(lossy.ID, lossless.ID); err != nil {
		t.Fatalf("delete lossy: %v", err)
	}
	left, _ := r.ListProfiles()
	if len(left) != 1 || len(left[0].Fallback) != 0 {
		t.Fatalf("the deleted profile must leave the fallback list: %+v", left)
	}
	if err := r.DeleteProfile(lossy.ID, lossless.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleting twice: %v", err)
	}
}
