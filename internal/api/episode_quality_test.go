package api_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// Episodes that an earlier version imported as Unknown get a quality read from
// their file names, once.
func TestBackfillOfEpisodeQuality(t *testing.T) {
	server, _, _ := newHuntTestServer(t)
	show, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 9, Title: "Old Import", Year: 2001, Monitored: true}, []library.Episode{
		{Season: 1, Episode: 1}, {Season: 1, Episode: 2}, {Season: 1, Episode: 3}, {Season: 1, Episode: 4}, {Season: 1, Episode: 5}, {Season: 1, Episode: 6}, {Season: 1, Episode: 7},
	})
	if err != nil {
		t.Fatal(err)
	}
	eps, _ := server.MovieRepo.ListEpisodes(show.ID)
	set := func(i int, quality, path string) {
		t.Helper()
		if err := server.MovieRepo.SetEpisodeStatus(eps[i].ID, library.StatusDownloaded, quality, path); err != nil {
			t.Fatal(err)
		}
	}
	set(0, "Unknown", "/tv/Old Import/Season 01/Old Import - S01E01 - One WEBDL-1080p.mkv")
	set(1, "Unknown", "/tv/Old Import [720p HDTV]/Season 01/Old Import - S01E02 - Two.mkv")
	set(2, "Unknown", "/tv/Old Import/Season 01/Old Import - S01E03 - Three.mkv") // says nothing
	set(3, "Bluray-1080p", "/tv/Old Import/Season 01/Old Import - S01E04 - Four WEBDL-720p.mkv")
	// One file holding two episodes is read once and fills both.
	set(4, "Unknown", "/tv/Old Import/Season 01/Old.Import.S01E05E06.2160p.WEB-DL.mkv")
	set(5, "", "/tv/Old Import/Season 01/Old.Import.S01E05E06.2160p.WEB-DL.mkv")
	// An episode that is not on disk has no file to read.
	if eps[6].Quality != "" {
		t.Fatalf("a new episode already has a quality: %q", eps[6].Quality)
	}

	qualities := func() map[int]string {
		t.Helper()
		list, err := server.MovieRepo.ListEpisodes(show.ID)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int]string{}
		for _, e := range list {
			out[e.Episode] = e.Quality
		}
		return out
	}

	server.TestBackfillEpisodeQuality()
	want := map[int]string{1: "WEBDL-1080p", 2: "HDTV-720p", 3: "Unknown", 4: "Bluray-1080p", 5: "WEBDL-2160p", 6: "WEBDL-2160p", 7: ""}
	got := qualities()
	for ep, q := range want {
		if got[ep] != q {
			t.Errorf("episode %d: quality %q, want %q", ep, got[ep], q)
		}
	}

	// It runs once: an episode that becomes Unknown later is left as it is.
	if err := server.MovieRepo.SetEpisodeQualities(map[int64]string{}); err != nil {
		t.Fatal(err)
	}
	if err := server.MovieRepo.SetEpisodeStatus(eps[3].ID, library.StatusDownloaded, "Unknown", "/tv/Old Import/Season 01/Old Import - S01E04 - Four WEBDL-720p.mkv"); err != nil {
		t.Fatal(err)
	}
	server.TestBackfillEpisodeQuality()
	if got := qualities(); got[4] != "Unknown" {
		t.Errorf("the backfill ran again: %v", got)
	}
}
