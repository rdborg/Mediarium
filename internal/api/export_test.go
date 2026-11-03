package api

import (
	"context"
	"path/filepath"
	"time"

	"github.com/rdborg/mediarium/internal/animemap"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/simkl"
	"github.com/rdborg/mediarium/internal/subtitles"
	"github.com/rdborg/mediarium/internal/trakt"
	"github.com/rdborg/mediarium/internal/tvmaze"
)

// TestHunt and TestRSSSync re-export the unexported hunt/rssSync jobs for
// automation_hunt_test.go (package api_test) to call directly, instead of
// waiting on the real 30-minute/15-minute scheduler intervals.
func (s *Server) TestHunt(ctx context.Context)    { s.hunt(ctx) }
func (s *Server) TestRSSSync(ctx context.Context) { s.rssSync(ctx) }

// SetTVmazeServer points the TVmaze client at a stand-in server.
func (s *Server) SetTVmazeServer(baseURL string) {
	s.tvmaze = tvmaze.New("test", tvmaze.WithBaseURL(baseURL), tvmaze.WithLimiter(tvmaze.NewLimiter(1000, time.Second)))
}

// TestSyncAirTimes runs the air-time look-up for one show.
func (s *Server) TestSyncAirTimes(ctx context.Context, sr library.Series) { s.syncAirTimes(ctx, sr) }

// TestTitleNamesJob runs the job that picks the other titles of each movie and show.
func (s *Server) TestTitleNamesJob(ctx context.Context) { s.titleNamesJob(ctx) }

// ResetTitleNamesState forgets the TMDB back-off and the titles the job
// skips after a failure, which are shared by the whole process.
func ResetTitleNamesState() {
	ResetTitleNamesPause()
	titleNamesFailed.Range(func(k, _ any) bool { titleNamesFailed.Delete(k); return true })
}

// ResetTitleNamesPause ends the wait the job keeps after TMDB failed.
func ResetTitleNamesPause() { titleNamesPausedUntil.Store(0) }

// TestWaitBackground blocks until every download pipeline and automatic
// retry started so far (and any they start in turn) has finished, so a test
// can assert on the end state without sleeping, and no goroutine outlives
// the test's database.
func (s *Server) TestWaitBackground() { s.bg.Wait() }

// TestStreamPauseRound makes one check of the pause-while-streaming loop.
func (s *Server) TestStreamPauseRound(ctx context.Context) { s.streamPauseRound(ctx) }

// TestHoldForStreaming hands downloads to the paused-for-streaming list the way
// a resumed-after-restart download does, TestForgetStreamPaused takes some off
// it the way giving them back does, and TestPausedForStreaming reads it.
func (s *Server) TestHoldForStreaming(ids []int64)   { s.holdForStreaming(ids) }
func (s *Server) TestForgetStreamPaused(ids []int64) { s.forgetPausedForStreaming(ids) }
func (s *Server) TestPausedForStreaming() []int64    { return s.pausedForStreaming() }

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

// TestSetSimklBaseURL points the live Simkl client at a local fixture server.
func (s *Server) TestSetSimklBaseURL(clientID, baseURL string) {
	s.simklMu.Lock()
	s.simkl = simkl.NewWithBaseURL(clientID, baseURL)
	s.instrumentSimkl(s.simkl)
	s.simklMu.Unlock()
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

// TestSetSubDLBaseURL points the SubDL client at a local fixture server
// (searches and downloads both) and switches subtitles on.
func (s *Server) TestSetSubDLBaseURL(apiKey, baseURL string) {
	_ = s.Settings.Set(settings.KeySubtitlesEnabled, "1", false)
	s.subsMu.Lock()
	s.subdl = subtitles.NewSubDLWithBaseURL(apiKey, baseURL, baseURL)
	s.subsMu.Unlock()
}

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

// TestBackfillDisplayFaces runs the job that stores translated titles and posters.
func (s *Server) TestBackfillDisplayFaces(ctx context.Context) { s.backfillDisplayFaces(ctx) }

// TestBackfillGenres runs the scheduled genre backfill once.
func (s *Server) TestBackfillGenres(ctx context.Context) { s.backfillGenres(ctx) }

// TestAgeSessions makes every stored browser session look as if it was last
// used d ago.
func (s *Server) TestAgeSessions(d time.Duration) {
	_, _ = s.db.Exec(`UPDATE sessions SET last_used_at = ?`, time.Now().Add(-d).UTC().Format(time.RFC3339Nano))
}

// TestBackfillCountries runs the scheduled origin-country backfill once.
func (s *Server) TestBackfillCountries(ctx context.Context) { s.backfillCountries(ctx) }

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

// TestSetGoogleBooks points the Google Books client at a fake server.
func (s *Server) TestSetGoogleBooks(url string) {
	s.googleBooksBase = url
}

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

// Private reports: point them at a test server and start the hourly count
// afresh. The returned func puts the real address back.
func SetFeedbackEndpointForTest(url string) func() {
	old := feedbackEndpoint
	feedbackEndpoint = url
	feedbackLimit.Lock()
	feedbackLimit.sent = nil
	feedbackLimit.byPerson = nil
	feedbackLimit.Unlock()
	return func() { feedbackEndpoint = old }
}

func LeadingZeroBitsForTest(b []byte) int { return leadingZeroBits(b) }

const (
	FeedbackPowBits        = feedbackPowBits
	FeedbackPerHour        = feedbackPerHour
	FeedbackPerHourInstall = feedbackPerHourInstall
)

// SetDebridURLForTest points the debrid services at a fake; the returned
// function puts it back.
func SetDebridURLForTest(u string) func() {
	old := debridTestURL
	debridTestURL = u
	return func() { debridTestURL = old }
}

// SetDebridPollForTest makes the debrid status checks quick.
func SetDebridPollForTest(d time.Duration) func() {
	old := debridPoll
	debridPoll = d
	return func() { debridPoll = old }
}

// SetDownloadClientPollForTest makes the download client status checks quick.
func SetDownloadClientPollForTest(d time.Duration) func() {
	old := downloadClientPoll.Swap(int64(d))
	return func() { downloadClientPoll.Store(old) }
}

// TestAddonRoundTrip searches the subtitle add-ons for a movie by its TMDB id
// and downloads the first result, the way a person picking it would.
func (s *Server) TestAddonRoundTrip(ctx context.Context, tmdbID int, lang string) (results []subtitles.Result, data []byte, err error) {
	q := subtitles.Query{TMDBID: tmdbID, Type: "movie", Language: lang}
	results, err = s.findSubtitles(ctx, q, false)
	if err != nil || len(results) == 0 {
		return results, nil, err
	}
	it := subtitleItem{kind: "movie", tmdbID: tmdbID}
	data, err = s.subtitleData(ctx, it, subtitlePick{source: results[0].Source, link: results[0].Link, lang: lang})
	return results, data, err
}

// TestSetAnimeServices points the scene and anime numbering services at local
// fixture servers (TheXEM, the anime list, AniList).
func (s *Server) TestSetAnimeServices(xemURL, listsURL, anilistURL string) {
	s.anime.once.Do(func() {})
	svc := animemap.NewService(s.db, "Mediarium/test", filepath.Join(s.cfg.ConfigDir, "anime-cache"))
	svc.Norm = normalizeTitle
	svc.XEM.SetBase(xemURL)
	svc.Lists.SetURL(listsURL)
	svc.AniList.SetURL(anilistURL)
	s.anime.svc = svc
}
