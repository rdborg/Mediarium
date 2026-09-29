package api

import (
	"testing"

	"github.com/ryanborg/mediarium/internal/indexers"
	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/quality"
)

// This file lives in package api (not api_test) specifically to exercise
// pickBestResult/matchesMovie, which are unexported — the automation
// loop's actual decision logic, as opposed to the HTTP-level behavior
// covered by pipeline_integration_test.go.

func TestPickBestResultPrefersHigherQualityAndAcceptsProfile(t *testing.T) {
	profile := quality.Presets()[quality.Preset1080p]
	results := []indexers.Result{
		{Title: "Movie.2024.720p.WEBRip.x264-GROUP", DownloadURL: "a"},
		{Title: "Movie.2024.1080p.BluRay.x264-GROUP", DownloadURL: "b"},
		{Title: "Movie.2024.2160p.BluRay.REMUX-GROUP", DownloadURL: "c"}, // outside the 1080p preset's allow-list
		{Title: "Movie.2024.CAM-GROUP", DownloadURL: "d"},                // unrecognized, rejected
	}

	best := pickBestResult(results, profile, 2024)
	if best == nil {
		t.Fatal("expected a best result")
	}
	if best.DownloadURL != "b" {
		t.Fatalf("expected the 1080p BluRay result to win, got %+v", best)
	}
}

func TestPickBestResultFiltersByYear(t *testing.T) {
	profile := quality.Presets()[quality.Preset1080p]
	results := []indexers.Result{
		{Title: "Movie.2019.1080p.BluRay.x264-GROUP", DownloadURL: "wrong-year"},
		{Title: "Movie.2024.1080p.WEB-DL.x264-GROUP", DownloadURL: "right-year"},
	}
	best := pickBestResult(results, profile, 2024)
	if best == nil || best.DownloadURL != "right-year" {
		t.Fatalf("expected the matching-year result to win, got %+v", best)
	}
}

func TestPickBestResultNoneAccepted(t *testing.T) {
	profile := quality.Presets()[quality.Preset1080p]
	results := []indexers.Result{
		{Title: "Movie.2024.CAM-GROUP", DownloadURL: "a"},
	}
	if best := pickBestResult(results, profile, 0); best != nil {
		t.Fatalf("expected nil, got %+v", best)
	}
}

func TestMatchesMovie(t *testing.T) {
	movie := library.Movie{Title: "The Matrix", Year: 1999}

	cases := []struct {
		release string
		want    bool
	}{
		{"The.Matrix.1999.1080p.BluRay.x264-GROUP", true},
		{"The.Matrix.Reloaded.2003.1080p.BluRay.x264-GROUP", false}, // different title
		{"The.Matrix.2021.1080p.WEB-DL.x264-GROUP", false},          // wrong year (Resurrections)
		{"the-matrix-1999-remastered", true},                        // punctuation-insensitive
	}
	for _, tc := range cases {
		t.Run(tc.release, func(t *testing.T) {
			if got := matchesMovie(tc.release, movie); got != tc.want {
				t.Errorf("matchesMovie(%q) = %v, want %v", tc.release, got, tc.want)
			}
		})
	}
}
