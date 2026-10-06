package api

import (
	"context"
	"path/filepath"
	"time"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/subtitles"
	"github.com/rdborg/mediarium/internal/trakt"
)

// TestHunt and TestRSSSync re-export the unexported hunt/rssSync jobs for
// automation_hunt_test.go (package api_test) to call directly, instead of
// waiting on the real 30-minute/15-minute scheduler intervals.
func (s *Server) TestHunt(ctx context.Context)    { s.hunt(ctx) }
func (s *Server) TestRSSSync(ctx context.Context) { s.rssSync(ctx) }

// TestWaitBackground blocks until every download pipeline and automatic
// retry started so far (and any they start in turn) has finished, so a test
// can assert on the end state without sleeping, and no goroutine outlives
// the test's database.
func (s *Server) TestWaitBackground() { s.bg.Wait() }

// TestRunCleanup runs the clean-up as if the time were now, and
// TestCleanupJob the automation loop's hourly clean-up check.
func (s *Server) TestRunCleanup(now time.Time) (removed int, prunedQueue, prunedActivity int64) {
	res := s.runCleanup(now)
	return len(res.Removed), res.PrunedQueue, res.PrunedActivity
}
func (s *Server) TestCleanupJob(ctx context.Context) { s.cleanupJob(ctx) }

// TestWorkDir is the downloads working folder, TestMoviesRoot the movie
// library folder.
func (s *Server) TestWorkDir() string    { return s.downloadsIncompleteDir() }
func (s *Server) TestMoviesRoot() string { return s.moviesRoot() }

// TestSeedingGoalMet reports queue item id's torrent, saved in its working
// folder, as having reached its seeding goal.
func (s *Server) TestSeedingGoalMet(id int64) { s.seedingGoalMet(id, s.workDirFor(id)) }

// TestSetTraktBaseURL and TestSetTMDBBaseURL point the live Trakt/TMDB
// clients at a local fixture server for import_list_test.go (package
// api_test), instead of the real third-party APIs (tests use local
// fixtures, not live network calls).
func (s *Server) TestSetTraktBaseURL(clientID, baseURL string) {
	s.traktMu.Lock()
	s.trakt = trakt.NewWithBaseURL(clientID, baseURL)
	s.instrumentTrakt(s.trakt)
	s.traktMu.Unlock()
}

func (s *Server) TestSetTMDBBaseURL(apiKey, baseURL string) {
	s.tmdbMu.Lock()
	s.tmdb = metadata.NewWithBaseURL(apiKey, baseURL)
	s.instrumentTMDB(s.tmdb)
	s.tmdbMu.Unlock()
}

// TestSetSubtitlesBaseURL points the live OpenSubtitles client at a local
// fixture server and switches subtitles on (they are off by default); a test
// of the off state turns them off again through the settings. TestSubtitleSweepJob
// runs the scheduled sweep once.
func (s *Server) TestSetSubtitlesBaseURL(apiKey, baseURL string) {
	_ = s.Settings.Set(settings.KeySubtitlesEnabled, "1", false)
	s.subsMu.Lock()
	s.subs = subtitles.NewWithBaseURL(apiKey, baseURL)
	s.instrumentSubtitles(s.subs)
	s.subsMu.Unlock()
}

func (s *Server) TestSubtitleSweepJob(ctx context.Context) { s.subtitleSweepJob(ctx) }

// TestScriptsDir is the folder scripts are picked from; TestNotifyImported
// sends an "imported" event as an import does (and runs the chosen script).
func (s *Server) TestScriptsDir() string            { return s.scriptsDir() }
func (s *Server) TestNotifyImported(it notify.Item) { s.notifyItem("imported", it) }

// TestSetMovieAddedAt changes when a movie joined the library.
func (s *Server) TestSetMovieAddedAt(id int64, at time.Time) {
	_, _ = s.db.Exec(`UPDATE movies SET added_at = ? WHERE id = ?`, at.UTC().Format(time.RFC3339), id)
}

// TestAgeSubtitleFiles makes the saved subtitles look downloaded and checked d ago.
func (s *Server) TestAgeSubtitleFiles(d time.Duration) {
	at := time.Now().Add(-d).UTC().Format("2006-01-02T15:04:05.000Z")
	_, _ = s.db.Exec(`UPDATE subtitle_files SET downloaded_at = ?, checked_at = ?`, at, at)
}

// TestRouteAccess reports every signed-in route pattern with its access
// level ("member" or "admin"), for the role tests in accounts_test.go.
func (s *Server) TestRouteAccess() map[string]string {
	out := map[string]string{}
	for p, level := range s.protectedRoutes().access {
		out[p] = string(level)
	}
	return out
}

// TestBackfillGenres runs the scheduled genre backfill once.
func (s *Server) TestBackfillGenres(ctx context.Context) { s.backfillGenres(ctx) }

// TestKickImportWorker starts the import details worker (as the scheduled
// job does, also after a restart) and TestBackfillImportedAddedDates runs
// the one-off added-date fix. TestSetImportWorker overrides how many titles
// are looked up at once and how long a failed title waits before its next try.
func (s *Server) TestKickImportWorker()           { s.kickImportWorker() }
func (s *Server) TestBackfillImportedAddedDates() { s.backfillImportedAddedDates() }

// TestBackfillEpisodeQuality runs the one-off fix for episodes imported as Unknown.
func (s *Server) TestBackfillEpisodeQuality() { s.backfillEpisodeQuality() }
func (s *Server) TestSetImportWorker(parallel int, backoff func(attempt int, permanent bool) time.Duration) {
	s.importWork.parallel, s.importWork.backoff = parallel, backoff
}

// OfferedURL turns a search result reference back into its download URL.
func (s *Server) OfferedURL(ref string) string { return s.offered.resolve(ref) }

// TestReopen builds a second server on the same database and folders, as if
// the app had been stopped and started again.
func (s *Server) TestReopen() (*Server, error) {
	box, err := crypto.LoadOrCreateKey(filepath.Join(s.cfg.ConfigDir, "secret.key"))
	if err != nil {
		return nil, err
	}
	return New(s.db, s.cfg, box, "", "test")
}

// TestAutoSubtitlesForMovie runs what happens after a movie is imported: the
// automatic subtitle download, when it is on.
func (s *Server) TestAutoSubtitlesForMovie(m library.Movie) { s.autoSubtitlesFor(movieSubtitleItem(m)) }

// TestSetHardcover points the Hardcover client at a fake server.
func (s *Server) TestSetHardcover(url string) {
	s.hc.mu.Lock()
	defer s.hc.mu.Unlock()
	s.hc.base, s.hc.client = url, nil
}

// TestCheckFollowedSeries runs the twice-a-day series check now.
func (s *Server) TestCheckFollowedSeries(ctx context.Context) {
	s.seriesCheck.mu.Lock()
	s.seriesCheck.last = time.Time{}
	s.seriesCheck.mu.Unlock()
	s.huntSeries(ctx)
}

// TestSetBookReleaseDate sets a book's release date.
func (s *Server) TestSetBookReleaseDate(id int64, date string) {
	_ = s.BookRepo.SetReleaseDate(id, date)
}
