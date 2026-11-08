package requests_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/requests"
)

func TestRatingLevel(t *testing.T) {
	tests := []struct {
		cert  string
		level int
		known bool
	}{
		// United States, films and television.
		{"G", requests.LevelAll, true},
		{"TV-Y", requests.LevelAll, true},
		{"TV-G", requests.LevelAll, true},
		{"TV-Y7", requests.LevelKids, true},
		{"PG", requests.LevelKids, true},
		{"TV-PG", requests.LevelKids, true},
		{"PG-13", requests.LevelTeens, true},
		{"TV-14", requests.LevelTeens, true},
		{"R", requests.LevelMature, true},
		{"TV-MA", requests.LevelMature, true},
		{"NC-17", requests.LevelAdult, true},
		// United Kingdom.
		{"U", requests.LevelAll, true},
		{"12A", requests.LevelTeens, true},
		{"15", requests.LevelMature, true},
		{"18", requests.LevelAdult, true},
		{"R18", requests.LevelAdult, true},
		// Germany, France, Netherlands, Spain: plain ages and local words.
		{"0", requests.LevelAll, true},
		{"6", requests.LevelKids, true},
		{"12", requests.LevelTeens, true},
		{"16", requests.LevelMature, true},
		{"FSK 16", requests.LevelMature, true},
		{"16+", requests.LevelMature, true},
		{"AL", requests.LevelAll, true},
		{"TP", requests.LevelAll, true},
		// Spacing and case don't matter.
		{"  pg-13 ", requests.LevelTeens, true},
		{"tv-ma", requests.LevelMature, true},
		// No rating, or one the scale doesn't know.
		{"", 0, false},
		{"NR", 0, false},
		{"Not Rated", 0, false},
		{"Unrated", 0, false},
		{"Banana", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.cert, func(t *testing.T) {
			level, known := requests.RatingLevel(tt.cert)
			if level != tt.level || known != tt.known {
				t.Fatalf("RatingLevel(%q) = %d, %v; want %d, %v", tt.cert, level, known, tt.level, tt.known)
			}
		})
	}
}

func TestLimitLevel(t *testing.T) {
	tests := []struct {
		limit string
		level int
		ok    bool
	}{
		{"", 0, false},
		{"   ", 0, false},
		{"PG-13", requests.LevelTeens, true},
		{"TV-14", requests.LevelTeens, true},
		{"12", requests.LevelTeens, true},
		{"G", requests.LevelAll, true},
		{"R", requests.LevelMature, true},
		{"NC-17", requests.LevelAdult, true},
		{"nonsense", 0, false},
	}
	for _, tt := range tests {
		level, ok := requests.LimitLevel(tt.limit)
		if level != tt.level || ok != tt.ok {
			t.Errorf("LimitLevel(%q) = %d, %v; want %d, %v", tt.limit, level, ok, tt.level, tt.ok)
		}
	}
}

func TestAgeAllowed(t *testing.T) {
	tests := []struct {
		name  string
		limit string
		block bool
		cert  string
		want  bool
	}{
		{"no limit allows everything", "", false, "NC-17", true},
		{"no limit allows unrated even when the flag is set", "", true, "", true},
		{"same rating", "PG-13", false, "PG-13", true},
		{"movie and show ratings share a level", "PG-13", false, "TV-14", true},
		{"a plain age matches the scale", "12", false, "PG-13", true},
		{"lower is fine", "PG-13", false, "G", true},
		{"above the limit", "PG-13", false, "R", false},
		{"show above the limit", "PG-13", false, "TV-MA", false},
		{"adult above the limit", "R", false, "NC-17", false},
		{"kids limit blocks teen titles", "PG", false, "PG-13", false},
		{"kids limit allows a UK PG", "PG", false, "PG", true},
		{"German ages", "12", false, "16", false},
		{"unrated is allowed by default", "PG", false, "", true},
		{"not rated is allowed by default", "PG", false, "NR", true},
		{"unrated is refused when blocked", "PG", true, "", false},
		{"not rated is refused when blocked", "PG", true, "Not Rated", false},
		{"unknown wording counts as unrated", "PG", false, "Banana", true},
		{"unknown wording is refused when blocked", "PG", true, "Banana", false},
		{"rated titles are not affected by the unrated flag", "R", true, "PG", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := requests.AgeAllowed(tt.limit, tt.block, tt.cert); got != tt.want {
				t.Fatalf("AgeAllowed(%q, %v, %q) = %v, want %v", tt.limit, tt.block, tt.cert, got, tt.want)
			}
		})
	}
}
