package library_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/store"
)

func movieEntry(tmdb int, title string, at time.Time) library.ImportEntry {
	return library.ImportEntry{Kind: "movie", TMDBID: tmdb, Title: title, Year: 2000, PosterPath: "/p.jpg", Quality: "Bluray-1080p", FilePath: "/movies/" + title + ".mkv", AddedAt: at}
}

func TestRegisterImportMovies(t *testing.T) {
	fileDate := time.Date(2018, 6, 1, 8, 30, 0, 0, time.UTC)
	tests := []struct {
		name        string
		seed        func(t *testing.T, r *library.Repo)
		noUpgrade   bool
		addedAt     time.Time
		wantOutcome string
		wantState   string
		wantStatus  library.Status
		wantNoUpg   bool
		wantAdded   time.Time // zero = about now
	}{
		{"a new movie is added with what the scan knows", nil, true, fileDate, library.ImportAdded, library.ImportPending, library.StatusDownloaded, true, fileDate},
		{"upgrades can stay on", nil, false, fileDate, library.ImportAdded, library.ImportPending, library.StatusDownloaded, false, fileDate},
		{"a missing file date falls back to now", nil, true, time.Time{}, library.ImportAdded, library.ImportPending, library.StatusDownloaded, true, time.Time{}},
		{"a date in the future falls back to now", nil, true, time.Now().Add(48 * time.Hour), library.ImportAdded, library.ImportPending, library.StatusDownloaded, true, time.Time{}},
		{
			"a movie that was only wanted is filled by the file", func(t *testing.T, r *library.Repo) {
				if _, err := r.Add(library.Movie{TMDBID: 100, Title: "Wanted", Year: 2000, Monitored: true}); err != nil {
					t.Fatal(err)
				}
			},
			true, fileDate, library.ImportAdded, library.ImportDone, library.StatusDownloaded, true, time.Time{},
		},
		{
			"a movie already downloaded is left as it is", func(t *testing.T, r *library.Repo) {
				m, err := r.Add(library.Movie{TMDBID: 100, Title: "Have", Year: 2000, Monitored: true})
				if err != nil {
					t.Fatal(err)
				}
				if err := r.SetStatus(m.ID, library.StatusDownloaded, "WEBDL-720p", "/elsewhere.mkv"); err != nil {
					t.Fatal(err)
				}
			},
			true, fileDate, library.ImportAlready, library.ImportDone, library.StatusDownloaded, false, time.Time{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepo(t)
			if tc.seed != nil {
				tc.seed(t, repo)
			}
			batchID, outcomes, err := repo.RegisterImport(library.ImportOptions{Kind: "movie", Root: "/movies", NoUpgrade: tc.noUpgrade}, []library.ImportEntry{movieEntry(100, "Title", tc.addedAt)})
			if err != nil {
				t.Fatal(err)
			}
			if len(outcomes) != 1 || outcomes[0].Outcome != tc.wantOutcome {
				t.Fatalf("outcome %+v, want %s", outcomes, tc.wantOutcome)
			}
			m, ok, err := repo.GetByTMDBID(100)
			if err != nil || !ok {
				t.Fatalf("movie not found: %v", err)
			}
			if m.Status != tc.wantStatus || m.NoUpgrade != tc.wantNoUpg {
				t.Fatalf("movie %+v, want status %s no-upgrade %v", m, tc.wantStatus, tc.wantNoUpg)
			}
			items, _ := repo.ImportBatchItems(batchID)
			if len(items) != 1 || items[0].State != tc.wantState {
				t.Fatalf("items %+v, want state %s", items, tc.wantState)
			}
			if tc.wantOutcome == library.ImportAdded && tc.wantState == library.ImportPending {
				if m.DetailsState != "pending" || m.FilePath != "/movies/Title.mkv" || m.Quality != "Bluray-1080p" || m.PosterPath != "/p.jpg" {
					t.Fatalf("a new movie should carry what the scan knows: %+v", m)
				}
			}
			recent, _ := repo.RecentlyAdded(5)
			if len(recent) != 1 {
				t.Fatalf("recent: %+v", recent)
			}
			added, err := time.Parse("2006-01-02T15:04:05.000Z", recent[0].AddedAt)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantOutcome == library.ImportAdded && tc.wantState == library.ImportPending {
				if tc.wantAdded.IsZero() {
					if time.Since(added) > time.Minute || added.After(time.Now().Add(time.Minute)) {
						t.Fatalf("added %v, want about now", added)
					}
				} else if !added.Equal(tc.wantAdded) {
					t.Fatalf("added %v, want %v", added, tc.wantAdded)
				}
			}
		})
	}
}

func TestImportBatchLifecycle(t *testing.T) {
	repo := newRepo(t)
	entries := []library.ImportEntry{movieEntry(1, "One", time.Time{}), movieEntry(2, "Two", time.Time{}), movieEntry(3, "Three", time.Time{})}
	batchID, _, err := repo.RegisterImport(library.ImportOptions{Kind: "movie", NoUpgrade: true}, entries)
	if err != nil {
		t.Fatal(err)
	}

	check := func(step string, running bool, pending, problems int) library.ImportBatch {
		t.Helper()
		b, err := repo.ImportBatchByID(batchID)
		if err != nil {
			t.Fatal(err)
		}
		if b.Running() != running || b.Pending != pending || b.Problems != problems || b.Added != 3 {
			t.Fatalf("%s: %+v, want running %v pending %d problems %d", step, b, running, pending, problems)
		}
		return b
	}
	check("after registering", true, 3, 0)
	if kinds, _ := repo.RunningImportKinds(); !kinds["movie"] || kinds["tv"] {
		t.Fatalf("running kinds %v", kinds)
	}

	due, err := repo.DueImportItems(time.Now(), 10)
	if err != nil || len(due) != 3 {
		t.Fatalf("due: %v %+v", err, due)
	}

	// One title is done, one fails and waits, one is not due yet.
	if err := repo.CompleteMovieImport(due[0], library.MovieDetails{Title: "One", Year: 2001, Overview: "About one", PosterPath: "/1.jpg", ReleaseDate: "2001-02-03", Genres: []string{"Drama"}}); err != nil {
		t.Fatal(err)
	}
	check("one done", true, 2, 0)
	m, _, _ := repo.GetByTMDBID(1)
	if m.DetailsState != "" || m.Overview != "About one" || m.Year != 2001 || m.ReleaseDate != "2001-02-03" || len(m.Genres) != 1 {
		t.Fatalf("details not stored: %+v", m)
	}
	if err := repo.SetImportItemState(due[1], library.ImportPending, "Couldn't reach the movie database.", 1, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	check("one waiting", true, 2, 0)
	if again, _ := repo.DueImportItems(time.Now(), 10); len(again) != 1 || again[0].TMDBID != 3 {
		t.Fatalf("only the untried title is due: %+v", again)
	}
	if later, _ := repo.DueImportItems(time.Now().Add(2*time.Hour), 10); len(later) != 2 {
		t.Fatalf("the waiting title is due again later: %+v", later)
	}

	// Giving up for now finishes the import but keeps the title for later.
	if err := repo.SetImportItemState(due[1], library.ImportProblem, "The movie database doesn't have this title.", 3, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetImportItemState(due[2], library.ImportProblem, "Couldn't reach the movie database.", 3, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	b := check("everything settled", false, 0, 2)
	if kinds, _ := repo.RunningImportKinds(); len(kinds) != 0 {
		t.Fatalf("nothing should be running: %v", kinds)
	}
	m, _, _ = repo.GetByTMDBID(2)
	if m.DetailsState != "problem" || m.DetailsNote == "" {
		t.Fatalf("the movie should show its problem: %+v", m)
	}
	if later, _ := repo.DueImportItems(time.Now().Add(2*time.Hour), 10); len(later) != 2 {
		t.Fatalf("problems are still tried again: %+v", later)
	}

	// A problem that is fixed later is counted as done again.
	later, _ := repo.DueImportItems(time.Now().Add(2*time.Hour), 10)
	if err := repo.CompleteMovieImport(later[0], library.MovieDetails{Title: "Two"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.ImportBatchByID(batchID); got.Problems != 1 {
		t.Fatalf("problems should drop to one: %+v", got)
	}

	// The banner keeps a finished import until it is dismissed.
	if active, _ := repo.ActiveImportBatches(); len(active) != 1 || active[0].ID != b.ID {
		t.Fatalf("active: %+v", active)
	}
	if err := repo.DismissImportBatch(batchID); err != nil {
		t.Fatal(err)
	}
	if active, _ := repo.ActiveImportBatches(); len(active) != 0 {
		t.Fatalf("a dismissed import is not active: %+v", active)
	}
}

func TestSeriesImport(t *testing.T) {
	episodes := []library.Episode{
		{Season: 1, Episode: 1, Title: "One", AirDate: "2011-01-01"},
		{Season: 1, Episode: 2, Title: "Two", AirDate: "2011-01-08"},
		{Season: 2, Episode: 1, Title: "Three", AirDate: "2012-01-01"},
		{Season: 2, Episode: 2, Title: "Four", AirDate: "2012-01-08"},
		{Season: 3, Episode: 1, Title: "Five", AirDate: "2999-01-01"}, // has not aired yet
	}
	files := []library.ImportFile{
		{Path: "/tv/s01e01.mkv", Quality: "HDTV-720p", Season: 1, Episodes: []int{1}},
		{Path: "/tv/s01e02-03.mkv", Quality: "HDTV-720p", Season: 1, Episodes: []int{2, 3}}, // episode 3 does not exist
		{Path: "/tv/s02e01.mkv", Quality: "HDTV-720p", Season: 2, Episodes: []int{1}},
	}
	tests := []struct {
		name          string
		monitor       bool
		monitorMiss   bool
		noUpgrade     bool
		wantSeriesMon bool
		wantMissing   bool // is the aired episode that is not on disk monitored?
		wantHaveMon   bool // are the episodes on disk monitored?
		wantFutureMon bool // is the episode that has not aired monitored?
	}{
		{"safe defaults: nothing is monitored", false, false, true, false, false, false, false},
		{"watching only: what is on disk and what is to come, not the gaps", true, false, false, true, false, true, true},
		{"missing episodes only: the gaps, and nothing else", false, true, true, true, true, false, true},
		{"everything on", true, true, false, true, true, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepo(t)
			batchID, outcomes, err := repo.RegisterImport(library.ImportOptions{Kind: "tv", Monitor: tc.monitor, NoUpgrade: tc.noUpgrade, MonitorMissing: tc.monitorMiss}, []library.ImportEntry{
				{Kind: "series", TMDBID: 7, Title: "Show", Year: 2011, Files: files, AddedAt: time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)},
			})
			if err != nil {
				t.Fatal(err)
			}
			series, ok, _ := repo.GetSeriesByTMDBID(7)
			if !ok || series.EpisodeCount != 0 || series.DetailsState != "pending" || series.Monitored != tc.wantSeriesMon || series.NoUpgrade != tc.noUpgrade {
				t.Fatalf("a new show has no episodes yet: %+v (found %v)", series, ok)
			}
			items, _ := repo.ImportBatchItems(batchID)
			if len(items) != 1 || len(items[0].Files) != 3 || outcomes[0].TitleID != series.ID {
				t.Fatalf("items %+v", items)
			}

			res, err := repo.CompleteSeriesImport(items[0], library.SeriesDetails{Title: "The Show", Year: 2011, Overview: "About", PosterPath: "/s.jpg", FirstAirDate: "2011-01-01"}, episodes)
			if err != nil {
				t.Fatal(err)
			}
			if res.Imported != 3 || res.Skipped != 0 || res.Unknown != 1 {
				t.Fatalf("result %+v, want 3 imported, 1 unknown", res)
			}
			series, _, _ = repo.GetSeriesByTMDBID(7)
			if series.Title != "The Show" || series.EpisodeCount != 5 || series.DownloadedCount != 3 || series.DetailsState != "" {
				t.Fatalf("show after details: %+v", series)
			}
			eps, _ := repo.ListEpisodes(series.ID)
			for _, ep := range eps {
				missing := (ep.Season == 2 && ep.Episode == 2) || ep.Season == 3
				want := tc.wantHaveMon
				if missing {
					want = tc.wantMissing
					if ep.Season == 3 {
						want = tc.wantFutureMon
					}
					if ep.Status != library.StatusMissing {
						t.Fatalf("S%02dE%02d is not on disk: %+v", ep.Season, ep.Episode, ep)
					}
				} else if ep.Status != library.StatusDownloaded || ep.FilePath == "" || ep.Quality != "HDTV-720p" {
					t.Fatalf("episode %+v should be downloaded in place", ep)
				}
				if ep.Monitored != want {
					t.Fatalf("S%02dE%02d monitored = %v, want %v", ep.Season, ep.Episode, ep.Monitored, want)
				}
			}
			b, _ := repo.ImportBatchByID(batchID)
			if b.Running() || b.Pending != 0 {
				t.Fatalf("the import should be finished: %+v", b)
			}
			done, _ := repo.ImportBatchItems(batchID)
			if done[0].Imported != 3 || done[0].Note == "" {
				t.Fatalf("the report should say what was left out: %+v", done[0])
			}

			// Importing the same files again changes nothing and counts them as already there.
			_, _, err = repo.RegisterImport(library.ImportOptions{Kind: "tv", Monitor: tc.monitor, NoUpgrade: tc.noUpgrade, MonitorMissing: tc.monitorMiss}, []library.ImportEntry{
				{Kind: "series", TMDBID: 7, Title: "Show", Files: files},
			})
			if err != nil {
				t.Fatal(err)
			}
			again, _ := repo.DueImportItems(time.Now(), 10)
			if len(again) != 1 {
				t.Fatalf("due: %+v", again)
			}
			res, err = repo.CompleteSeriesImport(again[0], library.SeriesDetails{Title: "The Show"}, episodes)
			if err != nil || res.Imported != 0 || res.Skipped != 3 {
				t.Fatalf("second import: %+v %v", res, err)
			}
		})
	}
}

func TestCompleteImportOfARemovedTitle(t *testing.T) {
	repo := newRepo(t)
	batchID, outcomes, err := repo.RegisterImport(library.ImportOptions{Kind: "movie"}, []library.ImportEntry{movieEntry(1, "Gone", time.Time{})})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(outcomes[0].TitleID); err != nil {
		t.Fatal(err)
	}
	due, _ := repo.DueImportItems(time.Now(), 5)
	if err := repo.CompleteMovieImport(due[0], library.MovieDetails{Title: "Gone"}); err != nil {
		t.Fatal(err)
	}
	items, _ := repo.ImportBatchItems(batchID)
	b, _ := repo.ImportBatchByID(batchID)
	if items[0].State != library.ImportDone || items[0].Note == "" || b.Running() {
		t.Fatalf("a removed title should not hold the import up: %+v %+v", items[0], b)
	}
}

func TestImportedForAddedDatesOnlyListsImports(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	repo := library.NewRepo(db)
	fromImport, _ := repo.Add(library.Movie{TMDBID: 1, Title: "A", Year: 2000, Monitored: true})
	fromDownload, _ := repo.Add(library.Movie{TMDBID: 2, Title: "B", Year: 2000, Monitored: true})
	for _, m := range []library.Movie{fromImport, fromDownload} {
		if err := repo.SetStatus(m.ID, library.StatusDownloaded, "WEBDL-1080p", "/movies/"+m.Title+".mkv"); err != nil {
			t.Fatal(err)
		}
	}
	// The queue package owns the activity log; write the rows it would write.
	for _, row := range []struct {
		id  int64
		msg string
	}{{fromImport.ID, "A: registered existing file /movies/A.mkv"}, {fromDownload.ID, "B imported to /movies/B.mkv"}} {
		if _, err := db.Exec(`INSERT INTO activity (movie_id, event_type, message) VALUES (?, 'imported', ?)`, row.id, row.msg); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.ImportedForAddedDates()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != "movie" || got[0].ID != fromImport.ID || got[0].Paths[0] != "/movies/A.mkv" {
		t.Fatalf("expected only the imported movie: %+v", got)
	}
}

// Movies an import adds are not monitored unless the person chose to watch
// them, and a movie that was already in the library keeps what it had.
func TestRegisterImportMovieMonitoring(t *testing.T) {
	tests := []struct {
		name      string
		monitor   bool
		seed      bool // a wanted movie is in the library already
		wantMon   bool
		wantNoUpg bool
	}{
		{"safe defaults", false, false, false, true},
		{"watching", true, false, true, false},
		{"a wanted movie keeps its monitoring", false, true, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepo(t)
			if tc.seed {
				if _, err := repo.Add(library.Movie{TMDBID: 100, Title: "Wanted", Year: 2000, Monitored: true}); err != nil {
					t.Fatal(err)
				}
			}
			batchID, _, err := repo.RegisterImport(library.ImportOptions{Kind: "movie", Monitor: tc.monitor, NoUpgrade: !tc.monitor}, []library.ImportEntry{movieEntry(100, "Title", time.Time{})})
			if err != nil {
				t.Fatal(err)
			}
			m, _, _ := repo.GetByTMDBID(100)
			if m.Monitored != tc.wantMon || m.NoUpgrade != tc.wantNoUpg {
				t.Fatalf("movie monitored %v no-upgrade %v, want %v and %v", m.Monitored, m.NoUpgrade, tc.wantMon, tc.wantNoUpg)
			}
			b, _ := repo.ImportBatchByID(batchID)
			if b.Monitor != tc.monitor {
				t.Fatalf("the batch should remember monitor = %v: %+v", tc.monitor, b)
			}
		})
	}
}

// "Start monitoring these titles" changes exactly the titles the import added.
func TestStartWatchingImport(t *testing.T) {
	setup := func(t *testing.T) (*library.Repo, int64) {
		repo := newRepo(t)
		// One show is in the library already, not monitored and left alone.
		if _, err := repo.AddSeries(library.Series{TMDBID: 8, Title: "Old", Year: 2000, Monitored: false, NoUpgrade: true}, nil); err != nil {
			t.Fatal(err)
		}
		files := []library.ImportFile{{Path: "/tv/a.mkv", Quality: "HDTV-720p", Season: 1, Episodes: []int{1}}}
		batchID, _, err := repo.RegisterImport(library.ImportOptions{Kind: "tv", NoUpgrade: true}, []library.ImportEntry{
			{Kind: "series", TMDBID: 7, Title: "New", Year: 2011, Files: files},
			{Kind: "series", TMDBID: 8, Title: "Old", Year: 2000},
		})
		if err != nil {
			t.Fatal(err)
		}
		episodes := []library.Episode{
			{Season: 1, Episode: 1, Title: "One", AirDate: "2011-01-01"},
			{Season: 1, Episode: 2, Title: "Two", AirDate: "2011-01-08"},
			{Season: 2, Episode: 1, Title: "Three", AirDate: "2999-01-01"},
		}
		due, _ := repo.DueImportItems(time.Now(), 10)
		for _, it := range due {
			if _, err := repo.CompleteSeriesImport(it, library.SeriesDetails{Title: it.Title}, episodes); err != nil {
				t.Fatal(err)
			}
		}
		return repo, batchID
	}
	monitored := func(t *testing.T, repo *library.Repo, tmdb int) (library.Series, map[[2]int]bool) {
		t.Helper()
		s, ok, _ := repo.GetSeriesByTMDBID(tmdb)
		if !ok {
			t.Fatalf("show %d missing", tmdb)
		}
		eps, _ := repo.ListEpisodes(s.ID)
		out := map[[2]int]bool{}
		for _, e := range eps {
			out[[2]int{e.Season, e.Episode}] = e.Monitored
		}
		return s, out
	}

	t.Run("nothing is monitored after a safe import", func(t *testing.T) {
		repo, _ := setup(t)
		s, eps := monitored(t, repo, 7)
		if s.Monitored || !s.NoUpgrade || eps[[2]int{1, 1}] || eps[[2]int{1, 2}] || eps[[2]int{2, 1}] {
			t.Fatalf("expected nothing monitored: %+v %v", s, eps)
		}
	})
	t.Run("start monitoring", func(t *testing.T) {
		repo, batchID := setup(t)
		res, err := repo.StartWatchingImport(batchID, true, false)
		if err != nil || res.Shows != 1 {
			t.Fatalf("result %+v, %v", res, err)
		}
		s, eps := monitored(t, repo, 7)
		if !s.Monitored || s.NoUpgrade || !eps[[2]int{1, 1}] || eps[[2]int{1, 2}] || !eps[[2]int{2, 1}] {
			t.Fatalf("watching should cover what is on disk and what is to come, not the gap: %+v %v", s, eps)
		}
		old, _ := monitored(t, repo, 8)
		if old.Monitored || !old.NoUpgrade {
			t.Fatalf("a show that was in the library before is left as it was: %+v", old)
		}
		b, _ := repo.ImportBatchByID(batchID)
		if !b.Monitor || b.NoUpgrade || b.MonitorMissing {
			t.Fatalf("the batch should say what is on now: %+v", b)
		}
	})
	t.Run("look for missing episodes", func(t *testing.T) {
		repo, batchID := setup(t)
		if _, err := repo.StartWatchingImport(batchID, false, true); err != nil {
			t.Fatal(err)
		}
		s, eps := monitored(t, repo, 7)
		if !s.Monitored || !s.NoUpgrade || eps[[2]int{1, 1}] || !eps[[2]int{1, 2}] || !eps[[2]int{2, 1}] {
			t.Fatalf("missing episodes should be wanted, and only those: %+v %v", s, eps)
		}
		if b, _ := repo.ImportBatchByID(batchID); b.Monitor || !b.MonitorMissing {
			t.Fatalf("the batch should say what is on now: %+v", b)
		}
	})
	t.Run("movies", func(t *testing.T) {
		repo := newRepo(t)
		batchID, _, err := repo.RegisterImport(library.ImportOptions{Kind: "movie", NoUpgrade: true}, []library.ImportEntry{movieEntry(1, "One", time.Time{}), movieEntry(2, "Two", time.Time{})})
		if err != nil {
			t.Fatal(err)
		}
		res, err := repo.StartWatchingImport(batchID, true, false)
		if err != nil || res.Movies != 2 {
			t.Fatalf("result %+v, %v", res, err)
		}
		for _, id := range []int{1, 2} {
			if m, _, _ := repo.GetByTMDBID(id); !m.Monitored || m.NoUpgrade {
				t.Fatalf("movie %d should be watched: %+v", id, m)
			}
		}
	})
}
