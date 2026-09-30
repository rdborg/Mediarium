package api

import (
	"net/http/httptest"
	"testing"
)

func TestWhichRequestsGetTheBusyDeadline(t *testing.T) {
	tests := []struct {
		method, path string
		want         bool
	}{
		{"GET", "/api/auth/me", true},
		{"GET", "/api/queue", true},
		{"GET", "/api/dashboard", true},
		{"GET", "/api/movies", true},
		{"GET", "/api/movies/5/events", true},
		{"GET", "/api/settings", true},
		{"POST", "/api/queue/3/retry", false},
		{"PUT", "/api/settings", false},
		{"GET", "/api/search", false},
		{"GET", "/api/movies/5/search", false},
		{"GET", "/api/series/5/search", false},
		{"GET", "/api/movies/5/similar", false},
		{"GET", "/api/movies/5/subtitles", false},
		{"GET", "/api/movies/5/files", false},
		{"GET", "/api/movies/5/disk-usage", false},
		{"GET", "/api/series/5/disk-usage", false},
		{"GET", "/api/music/artists/5/disk-usage", false},
		{"GET", "/api/settings/filesystem-check", false},
		{"GET", "/api/settings/folder-check", false},
		{"GET", "/api/files/stream", false},
		{"GET", "/api/system/backup", false},
		{"GET", "/api/discover/trending", false},
		{"GET", "/api/tmdb/movies/603", false},
		{"GET", "/api/music/covers/release-group/x", false},
		{"GET", "/api/version", false},
	}
	for _, tc := range tests {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		if got := guarded(r); got != tc.want {
			t.Errorf("%s %s: guarded = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}
