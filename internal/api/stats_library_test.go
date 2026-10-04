package api_test

import (
	"testing"
)

func TestLibraryStatsCountsWhatIsThere(t *testing.T) {
	server, base, client := loginNewServer(t)
	file := server.TestMoviesRoot() + "/Heat (1995)/Heat (1995).mkv"
	putFile(t, file, "12345")
	seedMovie(t, server.MovieRepo, 949, "Heat", 1995, file)
	seedMovie(t, server.MovieRepo, 950, "Ronin", 1998, "")
	got := getJSON[struct {
		Movies     int   `json:"movies"`
		MoviesHave int   `json:"moviesHave"`
		MovieBytes int64 `json:"movieBytes"`
		Quality    []struct {
			Label string `json:"label"`
			Count int    `json:"count"`
		} `json:"quality"`
	}](t, client, base+"/api/stats/library")
	if got.Movies != 2 || got.MoviesHave != 1 || got.MovieBytes != 5 || len(got.Quality) != 1 || got.Quality[0].Label != "WEBDL-1080p" {
		t.Fatalf("stats: %+v", got)
	}
}
