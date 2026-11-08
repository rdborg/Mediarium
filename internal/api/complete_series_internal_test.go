package api

import (
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/quality"
)

func hd1080Profile() quality.Profile {
	return quality.Profile{Name: "HD", Allowed: []quality.Tier{quality.TierWebDL1080p, quality.TierBluray1080p}, Cutoff: quality.TierBluray1080p}
}

func packResults(titles ...string) []indexers.Result {
	out := make([]indexers.Result, len(titles))
	for i, t := range titles {
		out[i] = indexers.Result{Title: t, SizeBytes: 30 << 30}
	}
	return out
}

func TestReleaseSeasonRange(t *testing.T) {
	cases := []struct {
		title      string
		season     int
		lo, hi     int
		wantSpread bool
	}{
		{"Show.S03.1080p.WEB-DL-GRP", 3, 3, 3, false},
		{"Show.S03E04.1080p.WEB-DL-GRP", 3, 3, 3, false},
		{"Show.S01-S09.1080p.WEB-DL-GRP", 1, 1, 9, true},
		{"Show.Seasons.2-4.1080p.WEB-DL-GRP", 2, 2, 4, true},
		{"Show.Complete.Series.1080p.WEB-DL-GRP", 1, 1, openEnded, true},
	}
	for _, c := range cases {
		lo, hi := releaseSeasonRange(c.title, c.season)
		if lo != c.lo || hi != c.hi || (hi > lo) != c.wantSpread {
			t.Errorf("releaseSeasonRange(%q, %d) = %d, %d", c.title, c.season, lo, hi)
		}
	}
}

func TestResolveTVTargetForWholeSeriesPacks(t *testing.T) {
	cases := []struct {
		title    string
		season   int
		episodes int
	}{
		{"Show.Complete.Series.1080p.WEB-DL-GRP", 1, 0},
		{"Show.S01-S05.1080p.WEB-DL-GRP", 1, 0},
		{"Show.S02E03.1080p.WEB-DL-GRP", 2, 1},
	}
	for _, c := range cases {
		season, episodes := resolveTVTarget(c.title, 0, 0)
		if season != c.season || len(episodes) != c.episodes {
			t.Errorf("resolveTVTarget(%q) = %d, %v", c.title, season, episodes)
		}
	}
}

func TestReleaseCoversEpisode(t *testing.T) {
	ep := func(season, episode int) library.Episode { return library.Episode{Season: season, Episode: episode} }
	cases := []struct {
		name  string
		title string
		ep    library.Episode
		want  bool
	}{
		{"pack of several seasons, inside", "Show.S01-S04.1080p-GRP", ep(3, 2), true},
		{"pack of several seasons, outside", "Show.S01-S04.1080p-GRP", ep(5, 1), false},
		{"complete series, any season", "Show.Complete.Series.1080p-GRP", ep(9, 22), true},
		{"complete series leaves specials alone", "Show.Complete.Series.1080p-GRP", ep(0, 1), false},
		{"one season", "Show.S02.1080p-GRP", ep(2, 5), true},
		{"one season, another season", "Show.S02.1080p-GRP", ep(3, 5), false},
		{"one episode", "Show.S02E05.1080p-GRP", ep(2, 6), false},
	}
	for _, c := range cases {
		season, episodes := resolveTVTarget(c.title, 0, 0)
		if got := releaseCoversEpisode(c.title, season, episodes, c.ep); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPackCovers(t *testing.T) {
	cases := []struct {
		name     string
		from, to int
		seasons  []int
		want     bool
	}{
		{"all of them", 1, 9, []int{2, 3, 9}, true},
		{"complete, no numbers", 1, 0, []int{1, 8}, true},
		{"stops short", 1, 5, []int{2, 6}, false},
		{"starts late", 3, 9, []int{2, 4}, false},
		{"nothing wanted", 1, 9, nil, false},
	}
	for _, c := range cases {
		if got := packCovers(c.from, c.to, c.seasons); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPickCompletePack(t *testing.T) {
	series := library.Series{Title: "The Office US", Year: 2005}
	all := []int{1, 2, 3, 4, 5, 6, 7, 8, 9}
	cases := []struct {
		name    string
		results []indexers.Result
		seasons []int
		profile quality.Profile
		want    string
	}{
		{"a complete series", packResults("The.Office.US.Complete.Series.1080p.WEB-DL.x264-GRP"), all, hd1080Profile(), "The.Office.US.Complete.Series.1080p.WEB-DL.x264-GRP"},
		{"a range of seasons", packResults("The.Office.US.S01-S09.1080p.BluRay.x264-GRP"), all, hd1080Profile(), "The.Office.US.S01-S09.1080p.BluRay.x264-GRP"},
		{"seasons written out", packResults("The Office US Seasons 1-9 1080p BluRay x264-GRP"), all, hd1080Profile(), "The Office US Seasons 1-9 1080p BluRay x264-GRP"},
		{"a range that stops short", packResults("The.Office.US.S01-S07.1080p.BluRay.x264-GRP"), all, hd1080Profile(), ""},
		{"a range that covers only the seasons wanted", packResults("The.Office.US.S01-S07.1080p.BluRay.x264-GRP"), []int{2, 5}, hd1080Profile(), "The.Office.US.S01-S07.1080p.BluRay.x264-GRP"},
		{"a single season is not a series", packResults("The.Office.US.S03.1080p.BluRay.x264-GRP"), all, hd1080Profile(), ""},
		{"another show", packResults("The.Office.UK.Complete.Series.1080p.BluRay.x264-GRP"), all, hd1080Profile(), ""},
		{"quality outside the profile", packResults("The.Office.US.Complete.Series.720p.HDTV.x264-GRP"), all, hd1080Profile(), ""},
		{"the better quality wins", packResults("The.Office.US.Complete.Series.1080p.WEB-DL.x264-GRP", "The.Office.US.Complete.Series.1080p.BluRay.x264-GRP"), all, hd1080Profile(), "The.Office.US.Complete.Series.1080p.BluRay.x264-GRP"},
		{"nothing at all", nil, all, hd1080Profile(), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pickCompletePack(c.results, series, c.seasons, 9, c.profile, nil)
			switch {
			case got == nil && c.want != "":
				t.Fatalf("picked nothing, want %q", c.want)
			case got != nil && got.Title != c.want:
				t.Fatalf("picked %q, want %q", got.Title, c.want)
			}
		})
	}
}

// A pack of nine seasons is judged per season: the profile's largest size is for
// one season, not for the whole series.
func TestPickCompletePackJudgesSizePerSeason(t *testing.T) {
	series := library.Series{Title: "The Office US", Year: 2005}
	p := hd1080Profile()
	p.MaxSizeGB = 10
	big := []indexers.Result{{Title: "The.Office.US.Complete.Series.1080p.BluRay.x264-GRP", SizeBytes: 80 << 30}}
	huge := []indexers.Result{{Title: "The.Office.US.Complete.Series.1080p.BluRay.x264-GRP", SizeBytes: 200 << 30}}
	seasons := []int{1, 2, 3, 4, 5, 6, 7, 8, 9}
	if pickCompletePack(big, series, seasons, 9, p, nil) == nil {
		t.Error("80 GB for nine seasons is under 10 GB a season, it should be taken")
	}
	if pickCompletePack(huge, series, seasons, 9, p, nil) != nil {
		t.Error("200 GB for nine seasons is over 10 GB a season, it should be left")
	}
	if pickCompletePack(big, series, seasons, 9, p, func(indexers.Result, int) bool { return false }) != nil {
		t.Error("the size table said no")
	}
}

func TestPickCompletePackNeverTakesPacksWhenTheProfileForbidsThem(t *testing.T) {
	p := hd1080Profile()
	p.NoSeasonPacks = true
	got := pickCompletePack(packResults("Show.Complete.Series.1080p.BluRay.x264-GRP"), library.Series{Title: "Show"}, []int{1, 2}, 2, p, nil)
	if got != nil {
		t.Fatalf("picked %q", got.Title)
	}
}

func TestCompleteNeeds(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	ep := func(season, episode int, status library.Status, monitored bool, air, q string) library.Episode {
		return library.Episode{Season: season, Episode: episode, Status: status, Monitored: monitored, AirDate: air, Quality: q}
	}
	web := string(quality.TierWebDL1080p)
	eps := []library.Episode{
		ep(0, 1, library.StatusMissing, true, "2010-01-01", ""), // a special: left alone
		ep(1, 1, library.StatusMissing, true, "2010-01-01", ""), // missing
		ep(1, 2, library.StatusDownloaded, true, "2010-01-08", web),
		ep(2, 1, library.StatusDownloaded, true, "2011-01-01", web), // complete
		ep(3, 1, library.StatusMissing, false, "2012-01-01", ""),    // not monitored
		ep(4, 1, library.StatusMissing, true, "2013-01-01", ""),     // missing, but a download is on its way
		ep(4, 2, library.StatusDownloading, true, "2013-01-08", ""),
		ep(5, 1, library.StatusMissing, true, "2099-01-01", ""), // not aired yet
		ep(6, 1, library.StatusMissing, true, "2015-01-01", ""),
	}
	needs, busy := completeNeeds(hd1080Profile(), eps, now)
	if len(needs) != 2 || needs[0].season != 1 || needs[1].season != 6 {
		t.Fatalf("needs = %+v", needs)
	}
	if busy != 1 {
		t.Fatalf("busy = %d, want 1", busy)
	}
	if needs[0].floor == 0 {
		t.Fatalf("season 1 has a file already, so a pack below it should be refused: %+v", needs[0])
	}
	if needs[1].floor != 0 {
		t.Fatalf("season 6 has no file: %+v", needs[1])
	}
}

func TestSeasonsText(t *testing.T) {
	cases := map[string][]int{
		"no seasons":            nil,
		"season 4":              {4},
		"seasons 1 and 2":       {1, 2},
		"seasons 1, 2 and 5":    {1, 2, 5},
		"seasons 3, 4, 6 and 9": {3, 4, 6, 9},
	}
	for want, in := range cases {
		if got := seasonsText(in); got != want {
			t.Errorf("seasonsText(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestWholeSeriesQueries(t *testing.T) {
	got := wholeSeriesQueries("Law & Order", 4)
	want := []string{"Law Order complete series", "Law Order S01-S04", "Law Order seasons 1-4"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("query %d = %q, want %q", i, got[i], want[i])
		}
	}
	if one := wholeSeriesQueries("Show", 1); len(one) != 1 {
		t.Errorf("a show of one season is asked for once: %v", one)
	}
}
