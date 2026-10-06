package parser

import (
	"reflect"
	"testing"
)

func TestAnimeAndDailyNames(t *testing.T) {
	cases := []struct {
		name     string
		title    string
		group    string
		season   int
		absolute []int
		airDate  string
	}{
		{"[SubsPlease] One Piece - 1085 (1080p) [ABCD1234].mkv", "One Piece", "SubsPlease", 0, []int{1085}, ""},
		{"[Erai-raws] Jujutsu Kaisen - 47 [1080p][Multiple Subtitle]", "Jujutsu Kaisen", "Erai-raws", 0, []int{47}, ""},
		{"[Judas] Frieren - 01-28 (Batch) [1080p]", "Frieren", "Judas", 0, seq(1, 28), ""},
		{"[Group] Show - 12v2 [720p]", "Show", "Group", 0, []int{12}, ""},
		{"One.Piece.E1085.1080p.WEB.H264-GRP", "One Piece", "GRP", 0, []int{1085}, ""},
		{"The.Daily.Show.2024.03.15.1080p.WEB.h264-GRP", "The Daily Show", "GRP", 0, nil, "2024-03-15"},
		{"Late Show 2024-11-02 720p HDTV x264", "Late Show", "", 0, nil, "2024-11-02"},
		// Normal names are left alone.
		{"Show.S01E05.1080p.WEB-GRP", "Show", "GRP", 1, nil, ""},
		{"Mission Impossible - Dead Reckoning 2023 1080p", "Mission Impossible - Dead Reckoning", "", 0, nil, ""},
	}
	for _, tc := range cases {
		r := Parse(tc.name)
		if r.Title != tc.title || r.Group != tc.group || r.Season != tc.season || !reflect.DeepEqual(r.Absolute, tc.absolute) || r.AirDate != tc.airDate {
			t.Errorf("%q:\n got title=%q group=%q season=%d abs=%v date=%q", tc.name, r.Title, r.Group, r.Season, r.Absolute, r.AirDate)
		}
	}
}

func seq(a, b int) []int {
	var out []int
	for i := a; i <= b; i++ {
		out = append(out, i)
	}
	return out
}
