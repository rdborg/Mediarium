package api

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/rdborg/mediarium/internal/torrentopts"
)

func TestMapClientPath(t *testing.T) {
	cases := []struct {
		name, path, remote, local, want string
	}{
		{"no mapping", "/downloads/Movie.mkv", "", "", "/downloads/Movie.mkv"},
		{"inside", "/downloads/Show.S01/ep1.mkv", "/downloads", "/data/torrents", "/data/torrents/Show.S01/ep1.mkv"},
		{"trailing slash", "/downloads/Movie", "/downloads/", "/data/torrents/", "/data/torrents/Movie"},
		{"the folder itself", "/downloads", "/downloads", "/data/torrents", "/data/torrents"},
		{"only a prefix of a name", "/downloads2/Movie", "/downloads", "/data/torrents", "/downloads2/Movie"},
		{"outside", "/other/Movie", "/downloads", "/data/torrents", "/other/Movie"},
		{"root", "/Movie", "/", "/data", "/data/Movie"},
		{"windows client", `D:\Torrents\Movie\a.mkv`, `D:\Torrents`, "/data/torrents", "/data/torrents/Movie/a.mkv"},
		{"windows any case", `d:\torrents\Movie`, `D:\Torrents`, "/data/torrents", "/data/torrents/Movie"},
		{"spaces around the settings", "/downloads/Movie", " /downloads ", " /data/torrents ", "/data/torrents/Movie"},
		{"a path that climbs out is not mapped", "/downloads/../etc/passwd", "/downloads", "/data/torrents", "/etc/passwd"},
		{"linux is case sensitive", "/Downloads/Movie", "/downloads", "/data/torrents", "/Downloads/Movie"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapClientPath(tc.path, tc.remote, tc.local)
			want := filepath.FromSlash(tc.want)
			if got != want {
				t.Errorf("mapClientPath(%q, %q, %q) = %q, want %q", tc.path, tc.remote, tc.local, got, want)
			}
		})
	}
}

func TestClientCategoryPerKind(t *testing.T) {
	kinds := map[string]string{torrentopts.KindMovie: "films", torrentopts.KindTV: " shows ", torrentopts.KindMusic: "", torrentopts.KindBook: "reads/new"}
	cases := []struct {
		name, main, kind, want string
	}{
		{"movie has its own", "mediarium", torrentopts.KindMovie, "films"},
		{"tv is trimmed", "mediarium", torrentopts.KindTV, "shows"},
		{"music falls back to the main one", "mediarium", torrentopts.KindMusic, "mediarium"},
		{"books may use a subcategory", "mediarium", torrentopts.KindBook, "reads/new"},
		{"empty main with other kinds set leaves this kind without one", "", torrentopts.KindMusic, ""},
		{"empty main still lets a kind use its own", "", torrentopts.KindMovie, "films"},
		{"unknown kind falls back", "tor", "other", "tor"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := downloadClientConfig{Category: tc.main, KindCategories: kinds}
			if got := c.categoryFor(tc.kind); got != tc.want {
				t.Errorf("categoryFor(%q) = %q, want %q", tc.kind, got, tc.want)
			}
		})
	}
	// Nothing set at all: the default category, as before.
	if got := (downloadClientConfig{}).category(); got != defaultClientCategory {
		t.Errorf("category() with nothing set = %q", got)
	}
	// Only the main category cleared while kinds have their own: no default creeps back in.
	only := downloadClientConfig{KindCategories: map[string]string{torrentopts.KindMovie: "films"}}
	if got := only.allCategories(); !slices.Equal(got, []string{"films"}) {
		t.Errorf("allCategories() with an empty main = %v", got)
	}
	// Nothing set: one category, as before.
	if got := (downloadClientConfig{}).allCategories(); !slices.Equal(got, []string{"mediarium"}) {
		t.Errorf("allCategories() = %v", got)
	}
	// Torrents added earlier under the main category stay Mediarium's.
	got := downloadClientConfig{Category: "old", KindCategories: map[string]string{torrentopts.KindMovie: "films", torrentopts.KindTV: "old"}}.allCategories()
	if !slices.Equal(got, []string{"old", "films"}) {
		t.Errorf("allCategories() = %v", got)
	}
}
