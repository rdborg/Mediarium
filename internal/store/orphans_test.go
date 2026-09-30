package store_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/queue"
)

// Removing a movie, a show or an artist leaves no row that only that title
// could show: its detailed events go with it, while the lines of the Activity
// feed stay (detached) as history.
func TestRemovingATitleLeavesNoOrphanedRows(t *testing.T) {
	db := openTemp(t)
	movies := library.NewRepo(db)
	artists := music.NewRepo(db)
	q := queue.NewRepo(db)

	movie, err := movies.Add(library.Movie{TMDBID: 1, Title: "Alien", Year: 1979, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	series, err := movies.AddSeries(library.Series{TMDBID: 2, Title: "Dark", Monitored: true}, []library.Episode{{Season: 1, Episode: 1}})
	if err != nil {
		t.Fatal(err)
	}
	artist, albums, err := artists.AddArtist(music.Artist{MBID: "a-1", Name: "Portishead", Monitored: true}, []music.Album{{MBID: "rg-1", Title: "Dummy", Type: "album"}})
	if err != nil {
		t.Fatal(err)
	}

	log := func(e queue.ItemEvent) {
		t.Helper()
		if err := q.LogItemEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	log(queue.ItemEvent{MovieID: movie.ID, Kind: "searched", Message: "Searched", ItemOnly: true})
	log(queue.ItemEvent{MovieID: movie.ID, Kind: "grabbed", Message: "Grabbed Alien"})
	log(queue.ItemEvent{SeriesID: series.ID, Kind: "searched", Message: "Searched", ItemOnly: true})
	log(queue.ItemEvent{SeriesID: series.ID, Kind: "grabbed", Message: "Grabbed Dark"})
	log(queue.ItemEvent{AlbumID: albums[0].ID, Kind: "searched", Message: "Searched", ItemOnly: true})
	log(queue.ItemEvent{AlbumID: albums[0].ID, Kind: "grabbed", Message: "Grabbed Dummy"})

	if err := movies.Delete(movie.ID); err != nil {
		t.Fatal(err)
	}
	if err := movies.DeleteSeries(series.ID); err != nil {
		t.Fatal(err)
	}
	if err := artists.DeleteArtist(artist.ID); err != nil {
		t.Fatal(err)
	}

	if got := count(t, db, `SELECT COUNT(*) FROM activity WHERE item_only = 1`); got != 0 {
		t.Errorf("%d item-only events are left with no title to show them", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM activity WHERE item_only = 0`); got != 3 {
		t.Errorf("the feed lines should stay as history, got %d of 3", got)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM activity WHERE movie_id IS NOT NULL OR series_id IS NOT NULL OR album_id IS NOT NULL`); got != 0 {
		t.Errorf("%d events still point at a removed title", got)
	}
	assertSound(t, db)

	// Removing a title that is not there is not an error for movies and shows,
	// and is for artists (there is nothing to show).
	if err := movies.Delete(9999); err != nil {
		t.Errorf("deleting a missing movie: %v", err)
	}
	if err := artists.DeleteArtist(9999); err == nil {
		t.Error("deleting a missing artist should say so")
	}
}
