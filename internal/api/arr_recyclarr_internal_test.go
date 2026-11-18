package api

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/quality"
)

func qn(name string, allowed bool) arrItemIn {
	return arrItemIn{Quality: &arrQualityRef{Name: name}, Allowed: allowed}
}

func grp(id int, name string, allowed bool, kids ...arrItemIn) arrItemIn {
	return arrItemIn{ID: id, Name: name, Allowed: allowed, Items: kids}
}

func intp(n int) *int { return &n }

// The quality lists of the apps are worst first.
func TestArrDeriveQualities(t *testing.T) {
	cases := []struct {
		name        string
		app         arrApp
		items       []arrItemIn
		cutoff      *int
		wantOrder   []quality.QualityItem
		wantAllowed []quality.Tier
		wantCutoff  quality.Tier
	}{
		{
			name: "singles in the built-in order need no order of their own",
			app:  arrRadarr,
			items: []arrItemIn{
				qn("WEBDL-720p", false), qn("WEBDL-1080p", true), qn("Bluray-1080p", true),
			},
			wantOrder:   nil,
			wantAllowed: []quality.Tier{quality.TierWebDL1080p, quality.TierBluray1080p},
			wantCutoff:  quality.TierBluray1080p,
		},
		{
			name: "a group is a group, WEBRip counts as WEBDL, the cutoff is the group",
			app:  arrRadarr,
			items: []arrItemIn{
				qn("Bluray-720p", true),
				grp(1001, "WEB 1080p", true, qn("WEBDL-1080p", true), qn("WEBRip-1080p", true)),
				qn("Bluray-1080p", true),
			},
			cutoff:      intp(1001),
			wantOrder:   []quality.QualityItem{{Tiers: []quality.Tier{quality.TierBluray1080p}}, {Name: "WEB 1080p", Tiers: []quality.Tier{quality.TierWebDL1080p}}, {Tiers: []quality.Tier{quality.TierBluray720p}}},
			wantAllowed: []quality.Tier{quality.TierBluray720p, quality.TierWebDL1080p, quality.TierBluray1080p},
			wantCutoff:  quality.TierWebDL1080p,
		},
		{
			name: "the cutoff names a quality by its id",
			app:  arrSonarr,
			items: []arrItemIn{
				{Quality: &arrQualityRef{ID: 9, Name: "HDTV-1080p"}, Allowed: true},
				{Quality: &arrQualityRef{ID: 7, Name: "Bluray-1080p"}, Allowed: true},
			},
			cutoff:      intp(9),
			wantOrder:   nil,
			wantAllowed: []quality.Tier{quality.TierHDTV1080p, quality.TierBluray1080p},
			wantCutoff:  quality.TierHDTV1080p,
		},
		{
			name: "a group that is not allowed adds nothing, and its qualities can be allowed elsewhere",
			app:  arrRadarr,
			items: []arrItemIn{
				grp(1002, "WEB 720p", false, qn("WEBDL-720p", false), qn("WEBRip-720p", false)),
				qn("WEBDL-720p", true),
			},
			wantOrder:   nil,
			wantAllowed: []quality.Tier{quality.TierWebDL720p},
			wantCutoff:  quality.TierWebDL720p,
		},
		{
			name: "a cutoff that is not allowed falls back to the best allowed quality",
			app:  arrRadarr,
			items: []arrItemIn{
				qn("DVD", true), qn("Bluray-1080p", true), qn("HDTV-720p", false),
			},
			cutoff:      intp(4),
			wantOrder:   nil,
			wantAllowed: []quality.Tier{quality.TierDVD, quality.TierBluray1080p},
			wantCutoff:  quality.TierBluray1080p,
		},
		{
			name: "qualities with no counterpart are left out",
			app:  arrRadarr,
			items: []arrItemIn{
				qn("Raw-HD", true), qn("BR-DISK", true), qn("Quality From The Future", true), qn("Bluray-1080p", true),
			},
			wantOrder:   nil,
			wantAllowed: []quality.Tier{quality.TierBluray1080p},
			wantCutoff:  quality.TierBluray1080p,
		},
		{
			name:        "Sonarr has no cinema-recording quality",
			app:         arrSonarr,
			items:       []arrItemIn{qn("CAM", true), qn("SDTV", true)},
			wantAllowed: []quality.Tier{quality.TierSDTV},
			wantCutoff:  quality.TierSDTV,
		},
		{
			name: "two groups with one name get different names; a long name is cut",
			app:  arrRadarr,
			items: []arrItemIn{
				grp(1001, "Same", true, qn("DVD", true), qn("SDTV", true)),
				grp(1002, "same", true, qn("HDTV-720p", true), qn("WEBDL-720p", true)),
				grp(1003, strings.Repeat("L", 60), true, qn("Bluray-720p", true), qn("HDTV-1080p", true)),
			},
			wantOrder: []quality.QualityItem{
				{Name: strings.Repeat("L", 40), Tiers: []quality.Tier{quality.TierHDTV1080p, quality.TierBluray720p}},
				{Name: "same", Tiers: []quality.Tier{quality.TierWebDL720p, quality.TierHDTV720p}},
				{Name: "Same 2", Tiers: []quality.Tier{quality.TierDVD, quality.TierSDTV}},
			},
			wantAllowed: []quality.Tier{quality.TierSDTV, quality.TierDVD, quality.TierHDTV720p, quality.TierWebDL720p, quality.TierBluray720p, quality.TierHDTV1080p},
			wantCutoff:  quality.TierHDTV1080p,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := arrDeriveQualities(c.app, c.items, c.cutoff)
			if d.Cutoff != c.wantCutoff {
				t.Errorf("cutoff %q, want %q", d.Cutoff, c.wantCutoff)
			}
			// The order that comes out is a valid one for the profile store.
			p, err := quality.Validate(quality.Profile{Name: "t", Allowed: d.Allowed, Cutoff: d.Cutoff, QualityOrder: d.Order})
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if !reflect.DeepEqual(d.Order, c.wantOrder) && !reflect.DeepEqual(p.QualityOrder, c.wantOrder) {
				t.Errorf("order %+v, want %+v", p.QualityOrder, c.wantOrder)
			}
			if !reflect.DeepEqual(p.Allowed, c.wantAllowed) {
				t.Errorf("allowed %v, want %v", p.Allowed, c.wantAllowed)
			}
		})
	}
	if d := arrDeriveQualities(arrRadarr, []arrItemIn{qn("Raw-HD", true)}, nil); len(d.Allowed) != 0 {
		t.Errorf("only a quality with no counterpart gives nothing allowed: %+v", d)
	}
}

func TestArrCatalogIsConsistent(t *testing.T) {
	for _, app := range []arrApp{arrRadarr, arrSonarr} {
		ids, names := map[int]string{}, map[string]bool{}
		for _, c := range arrCatalog(app) {
			if other, dup := ids[c.Q.ID]; dup {
				t.Errorf("%s: id %d is both %s and %s", app, c.Q.ID, other, c.Q.Name)
			}
			ids[c.Q.ID] = c.Q.Name
			if low := strings.ToLower(c.Q.Name); names[low] {
				t.Errorf("%s: %s is listed twice", app, c.Q.Name)
			} else {
				names[low] = true
			}
			if got, ok := arrQualityByRef(app, c.Q.ID, c.Q.Name); !ok || got.Q.Name != c.Q.Name {
				t.Errorf("%s: %s cannot be found again", app, c.Q.Name)
			}
		}
		// Every name the TRaSH Guides use for qualities is known.
		for _, n := range []string{"Unknown", "SDTV", "DVD", "WEBDL-480p", "WEBRip-480p", "Bluray-480p", "Bluray-576p", "HDTV-720p", "WEBDL-720p", "WEBRip-720p", "Bluray-720p",
			"HDTV-1080p", "WEBDL-1080p", "WEBRip-1080p", "Bluray-1080p", "HDTV-2160p", "WEBDL-2160p", "WEBRip-2160p", "Bluray-2160p", "Raw-HD"} {
			if _, ok := arrQualityByRef(app, -1, n); !ok {
				t.Errorf("%s does not know %s", app, n)
			}
		}
	}
	for _, n := range []string{"Remux-1080p", "Remux-2160p", "BR-DISK", "DVD-R", "DVDSCR", "REGIONAL", "TELECINE", "TELESYNC", "CAM", "WORKPRINT"} {
		if _, ok := arrQualityByRef(arrRadarr, -1, n); !ok {
			t.Errorf("radarr does not know %s", n)
		}
	}
	for _, n := range []string{"Bluray-1080p Remux", "Bluray-2160p Remux"} {
		if _, ok := arrQualityByRef(arrSonarr, -1, n); !ok {
			t.Errorf("sonarr does not know %s", n)
		}
	}
}
