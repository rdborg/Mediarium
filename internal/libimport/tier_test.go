package libimport

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/quality"
)

func TestTierFromNames(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		folders []string
		want    quality.Tier
	}{
		{"the file name says it", "Show.S01E01.1080p.WEB-DL.x264-GRP", nil, quality.TierWebDL1080p},
		{"Sonarr style name", "Show - S01E01 - Pilot WEBDL-1080p", nil, quality.TierWebDL1080p},
		{"Sonarr style bluray", "Show - S01E01 - Pilot Bluray-720p", []string{"Season 1"}, quality.TierBluray720p},
		{"the file name wins over the folders", "Show.S01E01.720p.HDTV", []string{"Season 1", "Show [2160p BluRay]"}, quality.TierHDTV720p},
		{"nothing anywhere", "Show - S01E01 - Pilot", []string{"Season 1", "Show (2008)"}, quality.TierUnknown},
		{"the season folder says it", "Show - S01E01 - Pilot", []string{"Season 1 1080p BluRay"}, quality.TierBluray1080p},
		{"the show folder says it", "S01E01", []string{"Season 01", "Show (2008) [1080p WEB-DL]"}, quality.TierWebDL1080p},
		{"the nearest folder is read first", "S01E01", []string{"Season 01 720p", "Show 2160p"}, quality.TierWebDL720p},
		{"resolution from the file, source from the folder", "Show.S01E01.1080p", []string{"Show.BluRay"}, quality.TierBluray1080p},
		{"source from the file, resolution from the folder", "Show.S01E01.BluRay", []string{"Season 01", "Show 1080p"}, quality.TierBluray1080p},
		{"a cinema recording stays one", "Show.S01E01.HDCAM", []string{"Show 1080p"}, quality.TierPreRelease},
		{"an old DVD rip", "Show.S01E01.DVDRip.XviD", nil, quality.TierDVD},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tierFromNames(tc.file, tc.folders); got != tc.want {
				t.Fatalf("tier = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTierFromPath(t *testing.T) {
	tests := []struct {
		path string
		want quality.Tier
	}{
		{"/data/tv/Show (2008)/Season 01/Show - S01E01 - Pilot WEBDL-1080p.mkv", quality.TierWebDL1080p},
		{"/data/tv/Show (2008)/Season 01/Show.S01E01.720p.BluRay.x264-GRP.mkv", quality.TierBluray720p},
		{"/data/tv/Show [1080p BluRay]/Season 01/Show - S01E01.mkv", quality.TierBluray1080p},
		{"/data/tv/Show (2008)/Season 01/Show - S01E01.mkv", quality.TierUnknown},
		{"Show.S01E01.1080p.HDTV.mkv", quality.TierHDTV1080p},
	}
	for _, tc := range tests {
		t.Run(filepath.Base(tc.path), func(t *testing.T) {
			if got := TierFromPath(tc.path); got != tc.want {
				t.Fatalf("tier = %q, want %q", got, tc.want)
			}
		})
	}
}
