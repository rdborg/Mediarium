package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/queue"
)

func TestPackSubtitle(t *testing.T) {
	cases := []struct {
		release string
		season  int
		want    string
	}{
		{"Show.S01.1080p.WEB-DL.x264-GRP", 1, "Season 1"},
		{"Show.S01-S09.1080p.WEB-DL.x264-GRP", 1, "Seasons 1-9"},
		{"Show.Complete.Series.1080p.BluRay.x264-GRP", 1, "Complete series"},
		{"Show.S03.Complete.1080p.WEB-DL-GRP", 3, "Season 3"},
	}
	for _, c := range cases {
		if got := packSubtitle(queue.Item{ReleaseTitle: c.release, Season: c.season}); got != c.want {
			t.Errorf("%q season %d = %q, want %q", c.release, c.season, got, c.want)
		}
	}
}
