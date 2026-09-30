package api_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/music"
)

func TestCalendarListsMonitoredAlbums(t *testing.T) {
	e := newMusicEnv(t)
	day := func(offset int) string { return time.Now().UTC().AddDate(0, 0, offset).Format("2006-01-02") }

	artist, albums, err := e.server.MusicRepo.AddArtist(music.Artist{MBID: "mb-cal", Name: "Fixture Band", Monitored: true, MonitorNew: true}, []music.Album{
		{MBID: "g1", Title: "Out Last Week", ReleaseDate: day(-7), Monitored: true, Status: music.StatusDownloaded},
		{MBID: "g2", Title: "Coming Soon", ReleaseDate: day(20), Monitored: true, Type: music.TypeEP},
		{MBID: "g3", Title: "Long Ago", ReleaseDate: day(-400), Monitored: true},
		{MBID: "g4", Title: "Far Away", ReleaseDate: day(400), Monitored: true},
		{MBID: "g5", Title: "Not Monitored", ReleaseDate: day(5), Monitored: false},
		{MBID: "g6", Title: "Only A Year", ReleaseDate: "2031", Monitored: true},
		{MBID: "g7", Title: "No Date", Monitored: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = e.server.MusicRepo.AddArtist(music.Artist{MBID: "mb-off", Name: "Unfollowed Act", Monitored: false}, []music.Album{
		{MBID: "h1", Title: "Still Monitored", ReleaseDate: day(3), Monitored: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.server.MovieRepo.Add(library.Movie{TMDBID: 1, Title: "Movie A", Year: 2030, Monitored: true, ReleaseDate: day(3)}); err != nil {
		t.Fatal(err)
	}

	entries := getJSON[[]map[string]any](t, e.client, e.base+"/api/calendar")
	var kinds, dates []string
	var album map[string]any
	for _, en := range entries {
		kinds = append(kinds, en["kind"].(string))
		dates = append(dates, en["releaseDate"].(string))
		if en["kind"] == "album" && en["subtitle"] == "EP" {
			album = en
		}
	}
	// The album's own flag decides, not whether its artist is followed.
	if len(entries) != 4 || kinds[0] != "album" || kinds[1] != "movie" || kinds[2] != "album" || kinds[3] != "album" {
		t.Fatalf("want the album out last week, the movie, the unfollowed artist's album, the EP - in date order: kinds %v dates %v", kinds, dates)
	}
	if entries[2]["title"] != "Unfollowed Act — Still Monitored" {
		t.Fatalf("third entry: %v", entries[2])
	}
	if album == nil || album["title"] != "Fixture Band — Coming Soon" || album["albumId"] != float64(albums[1].ID) ||
		album["artistId"] != float64(artist.ID) || album["id"] != album["albumId"] || album["releaseDate"] != day(20) || album["status"] != "missing" {
		t.Fatalf("album entry: %v", album)
	}
	if first := entries[0]; first["status"] != "downloaded" || first["subtitle"] != "Album" {
		t.Fatalf("first entry: %v", first)
	}

	// The dashboard's coming-up list keeps to movies and episodes.
	dash := getJSON[map[string]any](t, e.client, e.base+"/api/dashboard")
	for _, u := range dash["upcoming"].([]any) {
		if u.(map[string]any)["kind"] == "album" {
			t.Fatalf("albums must not appear in the dashboard's upcoming list: %v", u)
		}
	}

	// With the module off the calendar is the same as before.
	postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/modules", map[string]any{"music": false}, http.StatusOK)
	for _, en := range getJSON[[]map[string]any](t, e.client, e.base+"/api/calendar") {
		if en["kind"] == "album" {
			t.Fatalf("no albums while music is off: %v", en)
		}
	}
}
