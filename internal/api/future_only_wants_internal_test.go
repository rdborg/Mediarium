package api

import (
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
)

func dayOffset(n int) string { return time.Now().UTC().AddDate(0, 0, n).Format("2006-01-02") }

// futureOnlyShow adds a show with two old seasons (six episodes aired long
// ago), a season airing now (one aired, one coming) and sets it to follow only
// the episodes that have not aired yet.
func futureOnlyShow(t *testing.T, s *Server, packs bool) library.Series {
	t.Helper()
	var eps []library.Episode
	for season := 1; season <= 2; season++ {
		for n := 1; n <= 3; n++ {
			eps = append(eps, library.Episode{Season: season, Episode: n, AirDate: dayOffset(-900 + season*100 + n), Status: library.StatusMissing})
		}
	}
	eps = append(eps,
		library.Episode{Season: 3, Episode: 1, AirDate: dayOffset(1), Status: library.StatusMissing},
		library.Episode{Season: 3, Episode: 2, AirDate: dayOffset(8), Status: library.StatusMissing},
	)
	sr, err := s.MovieRepo.AddSeries(library.Series{TMDBID: 4001, Title: "Secrets Uncovered", Year: 2018, Monitored: true}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.MovieRepo.ApplyMonitorMode(sr.ID, library.MonitorFuture, dayOffset(0), false); err != nil {
		t.Fatal(err)
	}
	if packs {
		if err := s.MovieRepo.SetSeriesSeasonPack(sr.ID, library.SeasonPackOn); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.MovieRepo.GetSeries(sr.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func wantedEpisodes(wants []tvWant) (n int, wholeSeasons int, packs int) {
	for _, w := range wants {
		n += len(w.episodes)
		if w.wholeSeason {
			wholeSeasons++
		}
		if w.pack != nil {
			packs++
		}
	}
	return
}

// A show set to follow only new episodes asks for nothing from the seasons that
// have aired, however the search is started: the hunt, a search of the whole
// show by hand, the bulk search. Only a season or an episode asked for by name
// looks at the old ones.
func TestFutureOnlyShowIsNotSearchedFromSeasonOne(t *testing.T) {
	s := newAuditServer(t)
	futureOnlyShow(t, s, true)
	profiles, err := s.loadProfiles()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		scope tvScope
		eps   int
	}{
		{"the scheduled hunt, with packs", tvScope{packs: true}, 0},
		{"the RSS sync", tvScope{}, 0},
		{"Find missing episodes on the whole show", searchNowScope(0, seriesSearchNowRequest{}), 0},
		{"the bulk search", tvScope{force: true, asMarked: true}, 0},
		{"Find missing on season 1, asked for by name", searchNowScope(0, seriesSearchNowRequest{Season: intPtr(1)}), 3},
		{"one old episode, asked for by name", searchNowScope(0, seriesSearchNowRequest{Season: intPtr(2), Episode: 2}), 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wants, _, err := s.tvWants(profiles, c.scope)
			if err != nil {
				t.Fatal(err)
			}
			n, whole, packs := wantedEpisodes(wants)
			if n != c.eps || (c.eps == 0 && whole != 0) || packs != 0 {
				t.Fatalf("wants %d episodes, %d whole seasons, %d packs; want %d episodes and no pack", n, whole, packs, c.eps)
			}
		})
	}
}

func intPtr(n int) *int { return &n }

// A season that has ended is looked for as a pack only when every episode in it
// that is not in yet is monitored: a pack would bring in the others too.
func TestSeasonPackNeedsEveryMissingEpisodeMonitored(t *testing.T) {
	cases := []struct {
		name        string
		unmonitored []int // episodes of season 1 that are not monitored
		wantPack    bool
	}{
		{"all monitored", nil, true},
		{"one old episode is not", []int{1}, false},
		{"all but the last are not (a show added mid-season to follow new ones)", []int{1, 2}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newAuditServer(t)
			var eps []library.Episode
			for n := 1; n <= 3; n++ {
				eps = append(eps, library.Episode{Season: 1, Episode: n, AirDate: dayOffset(-60 + n), Status: library.StatusMissing})
			}
			sr, err := s.MovieRepo.AddSeries(library.Series{TMDBID: 4002, Title: "Packed Show", Year: 2024, Monitored: true}, eps)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.MovieRepo.SetSeriesSeasonPack(sr.ID, library.SeasonPackOn); err != nil {
				t.Fatal(err)
			}
			list, _ := s.MovieRepo.ListEpisodes(sr.ID)
			for _, n := range c.unmonitored {
				if err := s.MovieRepo.SetEpisodeMonitored(list[n-1].ID, false); err != nil {
					t.Fatal(err)
				}
			}
			profiles, err := s.loadProfiles()
			if err != nil {
				t.Fatal(err)
			}
			wants, _, err := s.tvWants(profiles, tvScope{packs: true})
			if err != nil {
				t.Fatal(err)
			}
			_, _, packs := wantedEpisodes(wants)
			if (packs > 0) != c.wantPack {
				t.Fatalf("pack planned = %v, want %v", packs > 0, c.wantPack)
			}
		})
	}
}

// The search for the complete series skips a pack that would bring in
// episodes that are not monitored, whether it is a season or the whole series.
func TestCompleteSeriesPackMustHoldOnlyWantedEpisodes(t *testing.T) {
	eps := []library.Episode{
		{Season: 1, Episode: 1, Monitored: false, Status: library.StatusMissing},
		{Season: 2, Episode: 1, Monitored: true, Status: library.StatusMissing},
		{Season: 3, Episode: 1, Monitored: true, Status: library.StatusMissing},
		{Season: 3, Episode: 2, Monitored: false, Status: library.StatusDownloaded},
	}
	unwanted := unwantedBySeason(eps)
	if unwanted[1] != 1 || unwanted[2] != 0 || unwanted[3] != 0 {
		t.Fatalf("unwanted = %v", unwanted)
	}
	cases := []struct {
		name     string
		from, to int
		want     bool
	}{
		{"season 2 alone", 2, 2, true},
		{"seasons 2 to 3", 2, 3, true},
		{"seasons 1 to 3 holds the unmonitored season 1", 1, 3, false},
		{"complete series holds season 1", 1, 0, false},
		{"complete from season 2", 2, 0, true},
		{"seasons 1 to 1 is only the unmonitored one", 1, 1, false},
	}
	for _, c := range cases {
		if got := packFree(c.from, c.to, unwanted); got != c.want {
			t.Errorf("%s: packFree = %v, want %v", c.name, got, c.want)
		}
	}
	// The pick leaves a pack alone when it brings in an unmonitored season.
	free := func(from, to int) bool { return packFree(from, to, unwanted) }
	if pickCompletePack(packResults("Show.S02-S03.1080p.BluRay.x264-GRP"), library.Series{Title: "Show"}, []int{2, 3}, 9, hd1080Profile(), nil, free) == nil {
		t.Fatal("a pack of seasons 2 to 3, which holds only wanted ones, was refused")
	}
	if got := pickCompletePack(packResults("Show.Complete.Series.1080p.BluRay.x264-GRP"), library.Series{Title: "Show"}, []int{2, 3}, 9, hd1080Profile(), nil, free); got != nil {
		t.Fatalf("took %q, which brings in the unmonitored season 1", got.Title)
	}
}

// A pack posted before the season's first episode aired is the season of an
// older show of the same name.
func TestPacksAreNotTakenFromBeforeTheShowAired(t *testing.T) {
	old := indexers.Result{Title: "Scrubs.S01.1080p.WEB-DL.x264-GRP", PublishDate: time.Now().AddDate(-5, 0, 0)}
	fresh := indexers.Result{Title: "Scrubs.S01.1080p.WEB-DL.x264-GRP2", PublishDate: time.Now().AddDate(0, 0, -1)}
	undated := indexers.Result{Title: "Scrubs.S01.1080p.WEB-DL.x264-GRP3"}
	got := dropPostedBeforeAiring([]indexers.Result{old, fresh, undated}, dayOffset(-30))
	if len(got) != 2 || got[0].Title != fresh.Title || got[1].Title != undated.Title {
		t.Fatalf("got %+v", got)
	}
	if got := firstAirDate([]library.Episode{{AirDate: "2026-03-02"}, {}, {AirDate: "2026-01-05"}}); got != "2026-01-05" {
		t.Fatalf("first air date = %q", got)
	}
}
