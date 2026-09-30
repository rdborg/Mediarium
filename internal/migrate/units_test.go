package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/quality"
)

func TestTranslate(t *testing.T) {
	maps := []PathMapping{
		{From: "/data/media", To: filepath.FromSlash("/volume1/other")},
		{From: "/data/media/movies", To: filepath.FromSlash("/movies")},
		{From: "/tv/", To: filepath.FromSlash("/shows")},
	}
	tests := []struct {
		in         string
		want       string
		wantMapped bool
	}{
		{"/data/media/movies/Heat (1995)", "/movies/Heat (1995)", true}, // longest prefix wins
		{"/data/media/movies", "/movies", true},
		{"/data/media/music/x", "/volume1/other/music/x", true},
		{"/data/media/moviesextra/x", "/volume1/other/moviesextra/x", true}, // whole segments only
		{"/tv/Show/", "/shows/Show", true},
		{"/tvshows/Show", "/tvshows/Show", false},
		{`D:\Media\Movies\Heat (1995)`, "D:/Media/Movies/Heat (1995)", false},
		{"/elsewhere/Heat", "/elsewhere/Heat", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, mapped := translate(tt.in, cleanMappings(maps))
			if got != filepath.FromSlash(tt.want) || mapped != tt.wantMapped {
				t.Fatalf("translate(%q) = %q, %v; want %q, %v", tt.in, got, mapped, filepath.FromSlash(tt.want), tt.wantMapped)
			}
		})
	}
}

func TestSuggestMappings(t *testing.T) {
	tmp := t.TempDir()
	movies := filepath.Join(tmp, "movies")
	library := filepath.Join(tmp, "library")
	for _, d := range []string{movies, filepath.Join(library, "Heat (1995)"), filepath.Join(library, "Alien (1979)")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name      string
		roots     map[string][]string
		requested []PathMapping
		libRoot   string
		want      []PathMapping
	}{
		{
			name:    "same last folder name",
			roots:   map[string][]string{"/data/media/movies": nil},
			libRoot: movies,
			want:    []PathMapping{{From: "/data/media/movies", To: movies}},
		},
		{
			name:    "same title folders",
			roots:   map[string][]string{"/volume1/films": {"/volume1/films/Heat (1995)", "/volume1/films/Alien (1979)", "/volume1/films/Nope (2022)"}},
			libRoot: library,
			want:    []PathMapping{{From: "/volume1/films", To: library}},
		},
		{
			name:    "too few title folders in common",
			roots:   map[string][]string{"/volume1/films": {"/volume1/films/Heat (1995)", "/volume1/films/A", "/volume1/films/B", "/volume1/films/C"}},
			libRoot: library,
		},
		{
			name:      "already mapped by the request",
			roots:     map[string][]string{"/data/media/movies": nil},
			requested: []PathMapping{{From: "/data/media", To: "/x"}},
			libRoot:   movies,
		},
		{
			name:    "the folder exists as it is",
			roots:   map[string][]string{filepath.ToSlash(movies): nil},
			libRoot: movies,
		},
		{
			name:  "no library folder set",
			roots: map[string][]string{"/data/media/movies": nil},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := suggestMappings(tt.roots, tt.requested, tt.libRoot)
			if len(got) != len(tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %+v, want %+v", got, tt.want)
				}
			}
		})
	}
}

func TestRootsOf(t *testing.T) {
	roots := rootsOf([]arrRootFolder{{Path: "/movies/"}}, []string{"/movies/A (2001)", "/movies4k/B (2002)", "/movies4k/C (2003)"})
	if len(roots) != 2 || len(roots["/movies"]) != 1 || len(roots["/movies4k"]) != 2 {
		t.Fatalf("roots: %+v", roots)
	}
}

func q(id int, name string, res int) *arrQuality {
	return &arrQuality{ID: id, Name: name, Resolution: res}
}

func TestClosestPreset(t *testing.T) {
	tests := []struct {
		name string
		p    arrProfile
		want string
	}{
		{"cutoff 1080p", arrProfile{Name: "HD-1080p", Cutoff: 7, Items: []arrProfileItem{{Quality: q(7, "Bluray-1080p", 1080), Allowed: true}, {Quality: q(4, "HDTV-720p", 720), Allowed: false}}}, quality.Preset1080p},
		{"group cutoff 2160p", arrProfile{Name: "UHD", Cutoff: 1003, Items: []arrProfileItem{{ID: 1003, Name: "WEB 2160p", Allowed: true, Items: []arrProfileItem{{Quality: q(18, "WEBDL-2160p", 2160), Allowed: true}}}}}, quality.Preset4K},
		{"720p and 1080p, cutoff 720p", arrProfile{Name: "HD - 720p/1080p", Cutoff: 4, Items: []arrProfileItem{{Quality: q(4, "HDTV-720p", 720), Allowed: true}, {Quality: q(9, "HDTV-1080p", 1080), Allowed: true}}}, quality.Preset720p},
		{"SD and HD", arrProfile{Name: "Mixed", Cutoff: 7, Items: []arrProfileItem{{Quality: q(1, "SDTV", 480), Allowed: true}, {Quality: q(7, "Bluray-1080p", 1080), Allowed: true}}}, quality.PresetAny},
		{"SD only", arrProfile{Name: "SD", Cutoff: 2, Items: []arrProfileItem{{Quality: q(2, "DVD", 480), Allowed: true}}}, quality.PresetAny},
		{"named any", arrProfile{Name: "Any", Cutoff: 7, Items: []arrProfileItem{{Quality: q(7, "Bluray-1080p", 1080), Allowed: true}}}, quality.PresetAny},
		{"cutoff unknown, best allowed used", arrProfile{Name: "Custom", Cutoff: 999, Items: []arrProfileItem{{Quality: q(4, "HDTV-720p", 720), Allowed: true}}}, quality.Preset720p},
		{"nothing known", arrProfile{Name: "Empty", Cutoff: 0}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, why := closestPreset(tt.p); got != tt.want {
				t.Fatalf("got %q (%s), want %q", got, why, tt.want)
			}
		})
	}
}

func TestMapProfilesOverridesAndDeletedPresets(t *testing.T) {
	stored := []quality.Profile{{ID: 10, Name: "1080p"}, {ID: 11, Name: "Mine"}}
	arr := []arrProfile{
		{ID: 1, Name: "Ultra-HD", Cutoff: 19, Items: []arrProfileItem{{Quality: q(19, "Bluray-2160p", 2160), Allowed: true}}},
		{ID: 2, Name: "HD-1080p", Cutoff: 7, Items: []arrProfileItem{{Quality: q(7, "Bluray-1080p", 1080), Allowed: true}}},
		{ID: 3, Name: "Custom", Cutoff: 7, Items: []arrProfileItem{{Quality: q(7, "Bluray-1080p", 1080), Allowed: true}}},
		{ID: 4, Name: "Bad pick", Cutoff: 7, Items: []arrProfileItem{{Quality: q(7, "Bluray-1080p", 1080), Allowed: true}}},
	}
	choice, list := mapProfiles("radarr", arr, stored, "1080p", map[string]int64{"custom": 11, "Bad pick": 99}, map[int]int{2: 5})
	if len(list) != 4 {
		t.Fatalf("list: %+v", list)
	}
	tests := []struct {
		arrID  int
		wantID int64
	}{
		{1, 0},  // "4K & over" was deleted: default profile
		{2, 10}, // closest resolution
		{3, 11}, // chosen by the user
		{4, 0},  // chosen profile doesn't exist: default
	}
	for _, tt := range tests {
		if got := choice.forArr(tt.arrID); got.ProfileID != tt.wantID || got.How == "" {
			t.Fatalf("arr profile %d: got %+v, want profile %d", tt.arrID, got, tt.wantID)
		}
	}
	if choice.forArr(2).Titles != 5 || choice.forArr(1).ProfileName != "Default (1080p)" {
		t.Fatalf("details: %+v / %+v", choice.forArr(2), choice.forArr(1))
	}
}

func TestNewznabBase(t *testing.T) {
	tests := []struct {
		base, apiPath, want string
		ok                  bool
	}{
		{"https://api.nzbgeek.info", "/api", "https://api.nzbgeek.info", true},
		{"https://api.nzbgeek.info/", "", "https://api.nzbgeek.info", true},
		{"https://indexer.example/api", "", "https://indexer.example", true},
		{"https://indexer.example/api", "/api", "https://indexer.example/api", true}, // Prowlarr calls .../api/api

		{"http://jackett:9117/api/v2.0/indexers/all/results/torznab", "/api", "http://jackett:9117/api/v2.0/indexers/all/results/torznab", true},
		{"https://nzb.example", "/nzb/api", "https://nzb.example/nzb", true},
		{"https://odd.example", "/newznab/rss", "https://odd.example/newznab/rss", false},
	}
	for _, tt := range tests {
		got, ok := newznabBase(tt.base, tt.apiPath)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("newznabBase(%q, %q) = %q, %v; want %q, %v", tt.base, tt.apiPath, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSiteSetting(t *testing.T) {
	sel := indexers.SettingSummary{Name: "sort", Type: "select", Options: []indexers.SettingOption{{Value: "time"}, {Value: "seeders"}}}
	chk := indexers.SettingSummary{Name: "freeleech", Type: "checkbox"}
	txt := indexers.SettingSummary{Name: "username", Type: "text"}
	tests := []struct {
		name   string
		s      indexers.SettingSummary
		raw    string
		want   string
		wantOK bool
	}{
		{"select by position", sel, "1", "seeders", true},
		{"select by value", sel, "time", "time", true},
		{"select out of range", sel, "7", "", false},
		{"select empty", sel, "", "", true},
		{"checkbox true", chk, "true", "true", true},
		{"checkbox false", chk, "false", "false", true},
		{"checkbox junk", chk, "maybe", "", false},
		{"text", txt, " ryan ", "ryan", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := siteSetting(tt.s, tt.raw)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("got %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestTierFromArr(t *testing.T) {
	tests := map[string]quality.Tier{
		"Bluray-1080p": quality.TierBluray1080p,
		"WEBRip-2160p": quality.TierWebDL2160p,
		"Remux-1080p":  quality.TierRemux1080p,
		"DVD":          quality.TierDVD,
		"TELESYNC":     quality.TierPreRelease,
		"Raw-HD":       quality.TierUnknown,
		"":             quality.TierUnknown,
	}
	for in, want := range tests {
		if got := tierFromArr(in); got != want {
			t.Fatalf("tierFromArr(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMasked(t *testing.T) {
	tests := map[string]bool{"********": true, "**********": true, " *** ": true, "": false, "pa**word": false, "secret": false}
	for in, want := range tests {
		if got := masked(in); got != want {
			t.Fatalf("masked(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestFlexInt(t *testing.T) {
	tests := []struct {
		in   string
		want int
		err  bool
	}{
		{`563`, 563, false}, {`"563"`, 563, false}, {`""`, 0, false}, {`true`, 1, false}, {`false`, 0, false}, {`null`, 0, false}, {`"abc"`, 0, true},
	}
	for _, tt := range tests {
		var f flexInt
		err := json.Unmarshal([]byte(tt.in), &f)
		if (err != nil) != tt.err || (!tt.err && int(f) != tt.want) {
			t.Fatalf("flexInt(%s) = %d, %v; want %d, err %v", tt.in, f, err, tt.want, tt.err)
		}
	}
}

func TestInsideFolder(t *testing.T) {
	root := filepath.FromSlash("/movies")
	tests := map[string]bool{"/movies/Heat (1995)": true, "/movies": true, "/movies4k/Heat": false, "/other": false}
	for p, want := range tests {
		if got := insideFolder(filepath.FromSlash(p), root); got != want {
			t.Fatalf("insideFolder(%q) = %v, want %v", p, got, want)
		}
	}
	if insideFolder("/movies/x", "") {
		t.Fatal("no library folder: nothing is inside it")
	}
}
