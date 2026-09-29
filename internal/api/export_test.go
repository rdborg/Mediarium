package api

import (
	"context"
	"time"

	"github.com/ryanborg/mediarium/internal/metadata"
	"github.com/ryanborg/mediarium/internal/subtitles"
	"github.com/ryanborg/mediarium/internal/trakt"
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
// fixture server; TestSubtitleSweepJob runs the scheduled sweep once.
func (s *Server) TestSetSubtitlesBaseURL(apiKey, baseURL string) {
	s.subsMu.Lock()
	s.subs = subtitles.NewWithBaseURL(apiKey, baseURL)
	s.instrumentSubtitles(s.subs)
	s.subsMu.Unlock()
}

func (s *Server) TestSubtitleSweepJob(ctx context.Context) { s.subtitleSweepJob(ctx) }

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

// OfferedURL turns a search result reference back into its download URL.
func (s *Server) OfferedURL(ref string) string { return s.offered.resolve(ref) }
