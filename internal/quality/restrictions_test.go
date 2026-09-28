package quality_test

import (
	"testing"

	"github.com/ryanborg/mediarium/internal/quality"
)

func TestTitleAllowed(t *testing.T) {
	p := quality.Profile{Name: "Strict", MustNotContain: []string{"HDR", "hardcoded"}, MustContain: []string{"x265", "HEVC"}}
	cases := []struct {
		title string
		want  bool
	}{
		{"Movie.2001.1080p.BluRay.x265-GRP", true},
		{"Movie.2001.1080p.BluRay.HEVC-GRP", true},
		{"Movie 2001 1080p BluRay x265 HDR", false}, // excluded term
		{"Movie.2001.1080p.BluRay.x264-GRP", false}, // none of the required terms
		{"Movie.2001.1080p.x265.HardCoded-GRP", false},
	}
	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			ok, why := p.TitleAllowed(tc.title)
			if ok != tc.want {
				t.Fatalf("TitleAllowed = %v (%s), want %v", ok, why, tc.want)
			}
			if !ok && why == "" {
				t.Fatal("a rejection must explain itself")
			}
		})
	}
	if ok, _ := (quality.Profile{}).TitleAllowed("anything at all"); !ok {
		t.Fatal("a profile without restrictions must allow everything")
	}
}

func TestTermsMatchAcrossSeparators(t *testing.T) {
	p := quality.Profile{MustContain: []string{"Directors Cut"}}
	if ok, _ := p.TitleAllowed("Movie.2001.Directors.Cut.1080p-GRP"); !ok {
		t.Fatal("dots in the title should match a spaced term")
	}
	p = quality.Profile{MustNotContain: []string{"WEB-DL"}}
	if ok, _ := p.TitleAllowed("Movie.2001.1080p.WEB.DL.x264-GRP"); ok {
		t.Fatal("dashes and dots should be interchangeable when matching")
	}
}

func TestScoreSumsMatchingTerms(t *testing.T) {
	p := quality.Profile{Preferred: []quality.Preferred{{Term: "x265", Score: 50}, {Term: "HDR", Score: 20}, {Term: "CAM", Score: -1000}}}
	if got := p.Score("Movie.2001.2160p.x265.HDR-GRP"); got != 70 {
		t.Fatalf("score = %d, want 70", got)
	}
	if got := p.Score("Movie.2001.CAM.x264"); got != -1000 {
		t.Fatalf("score = %d, want -1000", got)
	}
	if got := p.Score("Movie.2001.1080p.x264"); got != 0 {
		t.Fatalf("score = %d, want 0", got)
	}
}

func TestRestrictionsRoundTripAndValidate(t *testing.T) {
	repo := newRepo(t)
	created, err := repo.Create(quality.Profile{
		Name: "Picky", Allowed: []quality.Tier{quality.TierBluray1080p}, Cutoff: quality.TierBluray1080p,
		MustContain:    []string{" x265 ", "x265", ""},
		MustNotContain: []string{"HDR"},
		Preferred:      []quality.Preferred{{Term: "REPACK", Score: 25}, {Term: "repack", Score: 5}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.MustContain) != 1 || got.MustContain[0] != "x265" || len(got.MustNotContain) != 1 {
		t.Fatalf("terms should be trimmed and de-duplicated, got %+v / %+v", got.MustContain, got.MustNotContain)
	}
	if len(got.Preferred) != 1 || got.Preferred[0].Term != "REPACK" || got.Preferred[0].Score != 25 {
		t.Fatalf("preferred round trip: %+v", got.Preferred)
	}

	bad := []quality.Profile{
		{Name: "a", Allowed: []quality.Tier{quality.TierBluray1080p}, Cutoff: quality.TierBluray1080p, MustContain: []string{"a|b"}},
		{Name: "b", Allowed: []quality.Tier{quality.TierBluray1080p}, Cutoff: quality.TierBluray1080p, Preferred: []quality.Preferred{{Term: "x", Score: 999999}}},
	}
	for _, p := range bad {
		if _, err := repo.Create(p); err == nil {
			t.Fatalf("expected %+v to be rejected", p)
		}
	}
}
