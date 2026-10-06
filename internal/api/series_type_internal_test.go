package api

import (
	"reflect"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

func showEpisodes() []library.Episode {
	var eps []library.Episode
	for e := 1; e <= 12; e++ { // season 1: absolute 1-12
		eps = append(eps, library.Episode{Season: 1, Episode: e})
	}
	for e := 1; e <= 13; e++ { // season 2: absolute 13-25
		eps = append(eps, library.Episode{Season: 2, Episode: e, AirDate: "2024-04-" + twoDigits(e)})
	}
	eps = append(eps, library.Episode{Season: 0, Episode: 1}) // a special: never counted
	return eps
}

func twoDigits(n int) string { return string(rune('0'+n/10)) + string(rune('0'+n%10)) }

func TestMapRelease(t *testing.T) {
	eps := showEpisodes()
	cases := []struct {
		name, release, typ string
		season             int
		episodes           []int
	}{
		{"absolute in season 1", "[SubsPlease] Show - 05 (1080p)", library.SeriesAnime, 1, []int{5}},
		{"absolute in season 2", "[SubsPlease] Show - 14 (1080p)", library.SeriesAnime, 2, []int{2}},
		{"a batch over the season end", "[Judas] Show - 11-14 (Batch)", library.SeriesAnime, 1, []int{11, 12}},
		{"beyond the list", "[SubsPlease] Show - 99 (1080p)", library.SeriesAnime, 0, nil},
		{"season marker wins", "Show.S02E03.1080p", library.SeriesAnime, 2, []int{3}},
		{"daily by date", "Show.2024.04.07.1080p.WEB", library.SeriesDaily, 2, []int{7}},
		{"a standard show is left alone", "[SubsPlease] Show - 05 (1080p)", library.SeriesStandard, 0, nil},
	}
	for _, tc := range cases {
		got := mapRelease(parser.Parse(tc.release), tc.typ, eps)
		if got.Season != tc.season || !reflect.DeepEqual(got.Episodes, tc.episodes) {
			t.Errorf("%s: season %d episodes %v", tc.name, got.Season, got.Episodes)
		}
	}
}

func TestGuessSeriesType(t *testing.T) {
	anim := []metadata.Genre{{ID: 16, Name: "Animation"}}
	cases := []struct {
		show metadata.Show
		want string
	}{
		{metadata.Show{Genres: anim, OriginCountry: []string{"JP"}}, library.SeriesAnime},
		{metadata.Show{Genres: anim, OriginCountry: []string{"US"}}, library.SeriesStandard}, // a western cartoon
		{metadata.Show{Type: "Talk Show"}, library.SeriesDaily},
		{metadata.Show{Type: "News"}, library.SeriesDaily},
		{metadata.Show{Type: "Scripted"}, library.SeriesStandard},
	}
	for _, tc := range cases {
		if got := guessSeriesType(tc.show); got != tc.want {
			t.Errorf("%+v: %s, want %s", tc.show, got, tc.want)
		}
	}
}

func TestPickAnimeRelease(t *testing.T) {
	series := library.Series{Title: "Show", SeriesType: library.SeriesAnime}
	eps := showEpisodes()
	mapper := func(r parser.Release) parser.Release { return mapRelease(r, series.SeriesType, eps) }
	results := []indexers.Result{
		{Title: "[SubsPlease] Show - 13 (1080p) [AAAA1111].mkv", DownloadURL: "wrong-episode", SizeBytes: 1 << 30},
		{Title: "[SubsPlease] Show - 14 (1080p) [BBBB2222].mkv", DownloadURL: "right", SizeBytes: 1 << 30},
		{Title: "[SubsPlease] Other Show - 14 (1080p) [CCCC3333].mkv", DownloadURL: "other-show", SizeBytes: 1 << 30},
	}
	profile := quality.Profile{Name: "Any", Allowed: []quality.Tier{quality.TierWebDL1080p, quality.TierUnknown, quality.TierHDTV1080p, quality.TierBluray1080p}, Cutoff: quality.TierWebDL1080p}
	got := pickTVResult(results, series, 2, 2, profile, nil, mapper)
	if got == nil || got.DownloadURL != "right" {
		t.Fatalf("picked %+v", got)
	}
	if got := pickTVResult(results, series, 2, 2, profile, nil); got != nil {
		t.Fatalf("without the mapping an absolute release can't be placed: %+v", got)
	}
}
