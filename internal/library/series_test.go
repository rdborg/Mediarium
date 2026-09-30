package library_test

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/store"
)

func newRepo(t *testing.T) *library.Repo {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return library.NewRepo(db)
}

func TestAddSeriesAndEpisodes(t *testing.T) {
	repo := newRepo(t)
	series, err := repo.AddSeries(
		library.Series{TMDBID: 1399, Title: "Fixture Show", Year: 2011, Monitored: true},
		[]library.Episode{
			{Season: 1, Episode: 1, Title: "Pilot", AirDate: "2011-04-17"},
			{Season: 1, Episode: 2, Title: "Second", AirDate: "2011-04-24"},
			{Season: 2, Episode: 1, Title: "Return", AirDate: "2012-04-01"},
		},
	)
	if err != nil {
		t.Fatalf("add series: %v", err)
	}
	if series.EpisodeCount != 3 || series.DownloadedCount != 0 {
		t.Fatalf("expected 3 episodes / 0 downloaded, got %d / %d", series.EpisodeCount, series.DownloadedCount)
	}

	eps, err := repo.ListEpisodes(series.ID)
	if err != nil {
		t.Fatalf("list episodes: %v", err)
	}
	if len(eps) != 3 || eps[0].Season != 1 || eps[0].Episode != 1 || eps[2].Season != 2 {
		t.Fatalf("expected episodes ordered by season then episode, got %+v", eps)
	}
	for _, e := range eps {
		if e.Status != library.StatusMissing || !e.Monitored {
			t.Fatalf("expected new episodes missing+monitored, got %+v", e)
		}
	}

	found, ok, err := repo.GetSeriesByTMDBID(1399)
	if err != nil || !ok || found.ID != series.ID {
		t.Fatalf("GetSeriesByTMDBID: found=%+v ok=%v err=%v", found, ok, err)
	}
	if _, ok, err := repo.GetSeriesByTMDBID(9999); err != nil || ok {
		t.Fatalf("expected no match for an unknown tmdb id, got ok=%v err=%v", ok, err)
	}
}

func TestDuplicateSeriesRejected(t *testing.T) {
	repo := newRepo(t)
	if _, err := repo.AddSeries(library.Series{TMDBID: 1, Title: "A"}, nil); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if _, err := repo.AddSeries(library.Series{TMDBID: 1, Title: "A again"}, nil); err == nil {
		t.Fatal("expected a duplicate tmdb_id to be rejected")
	}
	list, _ := repo.ListSeries()
	if len(list) != 1 {
		t.Fatalf("expected the failed insert to leave exactly 1 series, got %d", len(list))
	}
}

// UpsertEpisodes must add new episodes and refresh metadata for existing
// ones without clobbering download state — otherwise a routine metadata
// refresh would silently mark downloaded episodes as missing again.
func TestUpsertEpisodesPreservesDownloadState(t *testing.T) {
	repo := newRepo(t)
	series, err := repo.AddSeries(library.Series{TMDBID: 5, Title: "S", Monitored: true},
		[]library.Episode{{Season: 1, Episode: 1, Title: "Old Title", AirDate: ""}})
	if err != nil {
		t.Fatalf("add series: %v", err)
	}
	ep, err := repo.GetEpisode(series.ID, 1, 1)
	if err != nil {
		t.Fatalf("get episode: %v", err)
	}
	if err := repo.SetEpisodeStatus(ep.ID, library.StatusDownloaded, "WEBDL-1080p", "/tv/S/S01E01.mkv"); err != nil {
		t.Fatalf("set status: %v", err)
	}

	err = repo.UpsertEpisodes(series.ID, []library.Episode{
		{Season: 1, Episode: 1, Title: "New Title", AirDate: "2020-01-01"},
		{Season: 1, Episode: 2, Title: "Brand New", AirDate: "2020-01-08"},
	}, true)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, _ := repo.GetEpisode(series.ID, 1, 1)
	if got.Title != "New Title" || got.AirDate != "2020-01-01" {
		t.Fatalf("expected refreshed metadata, got %+v", got)
	}
	if got.Status != library.StatusDownloaded || got.Quality != "WEBDL-1080p" || got.FilePath != "/tv/S/S01E01.mkv" {
		t.Fatalf("upsert clobbered download state: %+v", got)
	}
	eps, _ := repo.ListEpisodes(series.ID)
	if len(eps) != 2 {
		t.Fatalf("expected the new episode to be inserted (2 total), got %d", len(eps))
	}

	refreshed, _ := repo.GetSeries(series.ID)
	if refreshed.DownloadedCount != 1 || refreshed.EpisodeCount != 2 {
		t.Fatalf("expected 1/2 downloaded, got %d/%d", refreshed.DownloadedCount, refreshed.EpisodeCount)
	}
}

func TestDeleteSeriesCascadesEpisodes(t *testing.T) {
	repo := newRepo(t)
	series, err := repo.AddSeries(library.Series{TMDBID: 7, Title: "Gone"},
		[]library.Episode{{Season: 1, Episode: 1}, {Season: 1, Episode: 2}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := repo.DeleteSeries(series.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	eps, err := repo.ListEpisodes(series.ID)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(eps) != 0 {
		t.Fatalf("expected episodes to cascade-delete with their series, got %d", len(eps))
	}
}
