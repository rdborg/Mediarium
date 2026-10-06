package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

func TestMatchesShow(t *testing.T) {
	daily := library.Series{Title: "The Daily Show", Year: 1996}
	tests := []struct {
		release string
		series  library.Series
		want    bool
	}{
		{"The.Daily.Show.2024.03.15.Guest.1080p.WEB-GRP", daily, true}, // the air date's year is not the show's
		{"The.Daily.Show.S29E10.1080p.WEB-GRP", daily, true},
		{"The.Office.US.S01E01.720p-GRP", library.Series{Title: "The Office US", Year: 2005}, true},
		{"Doctor.Who.2005.S01E01.720p-GRP", library.Series{Title: "Doctor Who", Year: 1963}, false},
		{"Doctor.Who.2005.S01E01.720p-GRP", library.Series{Title: "Doctor Who", Year: 2005}, true},
		{"Other.Show.2024.03.15.1080p-GRP", daily, false},
	}
	for _, tt := range tests {
		if got := matchesShow(tt.release, tt.series); got != tt.want {
			t.Errorf("matchesShow(%q, %q %d) = %v, want %v", tt.release, tt.series.Title, tt.series.Year, got, tt.want)
		}
	}
}
