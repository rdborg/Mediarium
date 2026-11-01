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
		{"a batch of the whole season is its pack", "[Judas] Show - 01-12 (Batch)", library.SeriesAnime, 1, nil},
		{"a batch of season 2 is its pack", "[Judas] Show - 13-25 (Batch)", library.SeriesAnime, 2, nil},
		{"part of a season stays episodes", "[Judas] Show - 01-06 (Batch)", library.SeriesAnime, 1, []int{1, 2, 3, 4, 5, 6}},
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

func TestMapReleaseTwoEpisodesOnOneDay(t *testing.T) {
	eps := []library.Episode{
		{Season: 2024, Episode: 40, AirDate: "2024-05-09", Title: "Emma Stone"},
		{Season: 2024, Episode: 42, AirDate: "2024-05-10", Title: "Tournament of Champions Final, Part 2"},
		{Season: 2024, Episode: 41, AirDate: "2024-05-10", Title: "Tournament of Champions Final, Part 1"},
		{Season: 2024, Episode: 43, AirDate: "2024-05-13", Title: "Tom Hanks; Billie Eilish"},
		{Season: 2024, Episode: 44, AirDate: "2024-05-13", Title: "Jon Stewart"},
	}
	cases := []struct {
		name, release string
		episodes      []int
	}{
		{"one episode that day", "Show.2024.05.09.1080p.WEB-GRP", []int{40}},
		{"by part number", "Show.2024.05.10.Part.2.1080p.WEB-GRP", []int{42}},
		{"by Pt", "Show.2024.05.10.Pt1.1080p.WEB-GRP", []int{41}},
		{"by guest", "Show.2024.05.13.Jon.Stewart.1080p.WEB-GRP", []int{44}},
		{"by one of two guests", "Show.2024.05.13.Billie.Eilish.720p.HDTV-GRP", []int{43}},
		{"nothing to tell them apart", "Show.2024.05.13.1080p.WEB-GRP", nil},
		{"words matching neither", "Show.2024.05.13.Someone.Else.1080p.WEB-GRP", nil},
		{"no episode that day", "Show.2024.05.14.1080p.WEB-GRP", nil},
	}
	for _, tc := range cases {
		got := mapRelease(parser.Parse(tc.release), library.SeriesDaily, eps)
		if !reflect.DeepEqual(got.Episodes, tc.episodes) {
			t.Errorf("%s: season %d episodes %v, want %v", tc.name, got.Season, got.Episodes, tc.episodes)
		}
	}
}

func TestMapSpecialByTitleOrDate(t *testing.T) {
	eps := []library.Episode{
		{Season: 1, Episode: 1, Title: "Rose", AirDate: "2005-03-26"},
		{Season: 0, Episode: 1, Title: "The Christmas Invasion", AirDate: "2005-12-25"},
		{Season: 0, Episode: 2, Title: "The Runaway Bride", AirDate: "2006-12-25"},
		{Season: 0, Episode: 3, Title: "Doctor Who Confidential", AirDate: "2007-01-01"}, // the show's words don't count, "confidential" does
		{Season: 0, Episode: 4, Title: "Pilot", AirDate: "2008-01-01"},                   // one short word: too easy to hit by chance
		{Season: 0, Episode: 5, Title: "Planet of the Dead", AirDate: "2009-04-11"},
		{Season: 0, Episode: 6, Title: "Dead Planet Special", AirDate: "2009-04-12"},
	}
	cases := []struct {
		name, release string
		special       bool
		episodes      []int
	}{
		{"by title", "Doctor.Who.2005.The.Christmas.Invasion.720p.HDTV.x264-GRP", true, []int{1}},
		{"by title, no year", "Doctor Who The Runaway Bride 1080p WEB", true, []int{2}},
		{"S00E marker wins", "Doctor.Who.2005.S00E05.720p.HDTV-GRP", true, []int{5}},
		{"title missing a word", "Doctor.Who.Christmas.720p", false, nil},
		{"the show's words aside", "Doctor.Who.Confidential.720p", true, []int{3}},
		{"one short word", "Doctor.Who.Pilot.720p", false, nil},
		{"two specials match the same", "Doctor.Who.Planet.of.the.Dead.720p", true, []int{5}},
		{"by air date", "Doctor.Who.2007.01.01.720p", true, []int{3}},
		{"a regular episode is left alone", "Doctor.Who.2005.S01E01.720p", false, []int{1}},
	}
	for _, tc := range cases {
		got := mapSpecial(parser.Parse(tc.release), "Doctor Who", eps)
		if got.Special != tc.special || !reflect.DeepEqual(got.Episodes, tc.episodes) {
			t.Errorf("%s: special=%v season=%d episodes=%v", tc.name, got.Special, got.Season, got.Episodes)
		}
	}
	// coversTarget only takes a real special for season 0.
	if !coversTarget(mapSpecial(parser.Parse("Doctor.Who.The.Christmas.Invasion.720p"), "Doctor Who", eps), 0, 1) {
		t.Error("the special by title should cover S00E01")
	}
	if coversTarget(parser.Parse("Doctor.Who.720p.HDTV"), 0, 1) {
		t.Error("a name with no marker isn't a special")
	}
	if !matchesShowFor("Doctor.Who.The.Christmas.Invasion.720p", library.Series{Title: "Doctor Who"}, 0) || matchesShowFor("Doctor.Who.The.Christmas.Invasion.720p", library.Series{Title: "Doctor Who"}, 1) {
		t.Error("the looser show match is for specials only")
	}
}

func TestSpecialFilesAreMatchedOnImport(t *testing.T) {
	s := &Server{}
	special := []library.Episode{{Season: 0, Episode: 1, Title: "The Christmas Invasion"}}
	tests := []struct {
		file     string
		targets  []library.Episode
		season   int
		episodes []int
	}{
		{"Show.S00E02.1080p.mkv", special, 0, []int{2}},                              // the name says which special
		{"Show.The.Christmas.Invasion.1080p.mkv", special, 0, []int{1}},              // no marker: the one special grabbed
		{"Show.S01E03.1080p.mkv", special, 1, []int{3}},                              // a regular episode stays one
		{"Show.Extras.1080p.mkv", []library.Episode{special[0], special[0]}, 0, nil}, // several targets, no marker: unknown
	}
	for _, tt := range tests {
		season, eps := s.tvFileEpisodes(tt.file, 0, tt.targets, nil)
		if season != tt.season || !reflect.DeepEqual(eps, tt.episodes) {
			t.Errorf("%s: %d %v", tt.file, season, eps)
		}
	}
}

func TestCopiesGoInTheCopiesFolder(t *testing.T) {
	if got := checkCopiesFolder("/media/4k", "/media/movies", "/media/tv"); got != nil {
		t.Errorf("a separate folder: %v", got)
	}
	for _, bad := range []string{"/media/movies", "/media/movies/4k", "/media", "relative/4k"} {
		if checkCopiesFolder(bad, "/media/movies", "/media/tv") == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
	if checkCopiesFolder("", "/media/movies") != nil {
		t.Error("empty turns copies off")
	}
}
