// Package api serves the REST API the web UI talks to
// ("REST API served to the web UI") and, in production, the embedded
// frontend build alongside it so the whole app is one binary/one port.
package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/migrate"
	"github.com/rdborg/mediarium/internal/monitor"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/musicbrainz"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/problems"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/requests"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/subtitles"
	"github.com/rdborg/mediarium/internal/trakt"
	"github.com/rdborg/mediarium/internal/usage"
	"github.com/rdborg/mediarium/internal/vpn"
)

type Server struct {
	cfg config.Config
	db  *sql.DB // for backups (VACUUM INTO); repos hold their own handles

	Auth              *auth.Service
	Settings          *settings.Store
	IndexerRepo       *indexers.Repo
	Definitions       *indexers.DefinitionStore  // cached community indexer definitions
	Cardigann         *indexers.CardigannManager // runs definition-based indexers
	ClientRepo        *download.Repo
	MovieRepo         *library.Repo
	QueueRepo         *queue.Repo
	VPNRepo           *vpn.Repo
	VPNManager        *vpn.Manager
	NotifyRepo        *notify.Repo
	QualityRepo       *quality.Repo
	Blocklist         *blocklist.Repo
	SubtitleAttempts  *subtitles.AttemptRepo
	SubtitleFiles     *subtitles.FileRepo
	SubtitleDismissed *subtitles.DismissRepo
	SubtitleQuota     *subtitles.QuotaRepo
	Usage             *usage.Tracker // requests and limit hits per third-party service
	Problems          *problems.Log  // the log of real problems shown under Settings > System > Logs and errors (handlers_problems.go)
	MediaServers      *mediaservers.Repo
	MusicRepo         *music.Repo // artists, albums, tracks (the music module, music*.go)
	BookRepo          *books.Repo // ebooks and audiobooks (books_api.go)
	OpenLibrary       *books.Client
	Requests          *requests.Repo
	Audnexus          *books.Audnexus
	bookImport        bookImports    // the "import books already on disk" job (books_import.go)
	authorCheck       authorCheck    // when followed authors were last checked (books_authors.go)
	seriesCheck       authorCheck    // when followed series were last checked (books_series.go)
	hc                hardcoverState // the Hardcover client for the saved token (books_series.go)

	mediaClient    *mediaservers.Client
	mediaRefresher *mediaservers.Refresher // rescans Plex/Jellyfin/Emby after imports
	mediaFinder    *mediaservers.Finder    // "Watch in" lookups, cached briefly
	mediaSignIn    mediaSignIn             // discovery and sign-ins in progress (handlers_media_servers_signin.go)

	LoginLimiter *auth.LoginLimiter
	version      string
	security     securityState // trusted proxies, allowed origins, per-account sign-in limit (security.go)

	tmdbMu sync.RWMutex
	tmdb   *metadata.Client

	subsMu sync.RWMutex
	subs   *subtitles.Client

	traktMu sync.RWMutex
	trakt   *trakt.Client

	mbMu  sync.RWMutex
	mb    *musicbrainz.Client // one client, so its one-request-per-second limit is shared
	music musicState          // music hunt position and library scan jobs (music*.go)

	statsCache statsCache
	builtin    BuiltinKeys // app-wide keys baked into official releases (see keys.go)

	importMu         sync.Mutex
	importJobs       map[string]*importJob
	importRegisterMu sync.Mutex  // one import is confirmed at a time (handleRunImport)
	importWork       importState // the worker that fills in details after an import (import_worker.go)

	migrator *migrate.Importer // moving over from Radarr/Sonarr/Prowlarr/SABnzbd (handlers_migrate.go)
	held     heldNotices       // messages waiting for the quiet hours to end

	torrents torrentRegistry // live torrent clients, so the on/off switch can stop them

	grabMu sync.Mutex     // makes "is this already being downloaded?" and "queue it" one step (see grab_claim.go)
	bg     sync.WaitGroup // download pipelines and automatic retries still running (see grab_claim.go)

	pipelines pipelineRegistry  // running download pipelines, so removing a title can cancel its downloads
	dispatch  *queue.Dispatcher // the download line: starts the next waiting download when a place is free (dispatch.go)
	bulk      bulkState         // whether a batch search from the Library page is running (handlers_bulk.go)

	offered    offeredURLs       // download URLs recently handed out in search results (see grab_urls.go)
	flareProbe flareSolverrProbe // last answer from the Cloudflare helper (see flaresolverr.go)
	stats      sysinfoSampler    // CPU and memory readings for the server card (see handlers_stats.go)

	Monitor *monitor.Monitor // connectivity monitor (Usenet servers, indexers)

	restoreMu    sync.Mutex    // one restore upload at a time
	exitFn       func()        // what to do once a restore is staged (see SetExitFunc)
	restartDelay time.Duration // zero means defaultRestartDelay

	upd updateRuntime // the new-version notice, pushed and downloaded updates, and the self-restart marker (update_*.go, watchdog.go)
}

// New wires every module's repo/service together (api depends on the
// feature packages, not the reverse) and loads the current TMDB key
// from settings, if one's been configured.
//
// defaultTMDBAPIKey is a fallback used only when the user hasn't set one
// in Settings — see cmd/app/main.go's buildDefaultTMDBAPIKey doc comment
// for why this exists (matching Radarr/Sonarr's "just works" experience
// without committing a real key to source control).
func New(db *sql.DB, cfg config.Config, box *crypto.Box, defaultTMDBAPIKey, version string) (*Server, error) {
	settingsStore := settings.New(db, box)
	tmdbKey, err := settingsStore.Get(settings.KeyTMDBAPIKey)
	if err != nil {
		return nil, err
	}
	if tmdbKey == "" {
		tmdbKey = defaultTMDBAPIKey
	}
	subsKey, err := settingsStore.Get(settings.KeyOpenSubtitlesAPIKey)
	if err != nil {
		return nil, err
	}
	traktClientID, err := settingsStore.Get(settings.KeyTraktClientID)
	if err != nil {
		return nil, err
	}

	tracker, err := usage.NewTracker(db)
	if err != nil {
		return nil, err
	}

	s := &Server{
		cfg:               cfg,
		db:                db,
		Auth:              auth.New(db),
		Settings:          settingsStore,
		IndexerRepo:       indexers.NewRepo(db, box),
		ClientRepo:        download.NewRepo(db, box),
		MovieRepo:         library.NewRepo(db),
		QueueRepo:         queue.NewRepo(db),
		VPNRepo:           vpn.NewRepo(db, box),
		VPNManager:        vpn.NewManager(),
		NotifyRepo:        notify.NewRepo(db, box),
		QualityRepo:       quality.NewRepo(db),
		Blocklist:         blocklist.NewRepo(db),
		SubtitleAttempts:  subtitles.NewAttemptRepo(db),
		SubtitleFiles:     subtitles.NewFileRepo(db),
		SubtitleDismissed: subtitles.NewDismissRepo(db),
		SubtitleQuota:     subtitles.NewQuotaRepo(db),
		tmdb:              metadata.New(tmdbKey),
		subs:              subtitles.New(subsKey),
		trakt:             trakt.New(traktClientID),
		// "A handful of attempts per IP/window" — 5 tries per 15
		// minutes blunts brute-force without punishing a genuine typo.
		LoginLimiter: auth.NewLoginLimiter(5, 15*time.Minute),
		version:      version,
		Usage:        tracker,
	}
	s.QueueRepo.SetBox(box) // download addresses carry indexer keys: kept encrypted
	s.startProblems()
	s.Definitions = indexers.NewDefinitionStore(filepath.Join(cfg.ConfigDir, "indexer-definitions"))
	s.Cardigann = indexers.NewCardigannManager(s.Definitions, s.flareSolverrURL)
	s.IndexerRepo.SetCardigann(s.Cardigann)
	s.instrumentTMDB(s.tmdb)
	s.instrumentTrakt(s.trakt)
	s.Monitor = s.newMonitor()
	s.MediaServers = mediaservers.NewRepo(db, box)
	s.mediaClient = mediaservers.NewClient(version)
	s.mediaFinder = mediaservers.NewFinder(s.mediaClient)
	s.mediaRefresher = mediaservers.NewRefresher(s.MediaServers, s.mediaClient)
	s.mediaRefresher.OnDone = s.mediaFinder.Invalidate
	s.rebuildSubtitles() // picks up a saved OpenSubtitles account
	s.migrator = s.newMigrator()
	if err := s.initProfiles(); err != nil {
		return nil, fmt.Errorf("init quality profiles: %w", err)
	}
	s.MusicRepo = music.NewRepo(db)
	s.BookRepo = books.NewRepo(db)
	s.OpenLibrary = books.NewClient("Mediarium/" + version + " (https://mediarium.app)")
	s.Audnexus = books.NewAudnexus(s.OpenLibrary.UserAgent)
	s.Requests = requests.NewRepo(db)
	s.mb = musicbrainz.New(version)
	if err := s.MusicRepo.SeedPresets(); err != nil {
		return nil, fmt.Errorf("init music profiles: %w", err)
	}
	s.dispatch = s.newDispatcher()
	s.recoverAtStartup() // downloads that were running come back paused; stuck titles are put right
	return s, nil
}

// TMDB returns the current metadata client, safe to call concurrently with
// SetTMDBAPIKey (settings changes take effect immediately without a
// restart).
func (s *Server) TMDB() *metadata.Client {
	s.tmdbMu.RLock()
	defer s.tmdbMu.RUnlock()
	return s.tmdb
}

// SetTMDBAPIKey updates both the persisted setting and the live client.
func (s *Server) SetTMDBAPIKey(key string) error {
	if err := s.Settings.Set(settings.KeyTMDBAPIKey, key, true); err != nil {
		return err
	}
	s.tmdbMu.Lock()
	s.tmdb.SetAPIKey(key)
	s.tmdbMu.Unlock()
	return nil
}

// Subtitles returns the current OpenSubtitles client, safe to call
// concurrently with SetOpenSubtitlesAPIKey.
func (s *Server) Subtitles() *subtitles.Client {
	s.subsMu.RLock()
	defer s.subsMu.RUnlock()
	return s.subs
}

// SetOpenSubtitlesAPIKey updates both the persisted setting and the live client.
func (s *Server) SetOpenSubtitlesAPIKey(key string) error {
	if err := s.Settings.Set(settings.KeyOpenSubtitlesAPIKey, key, true); err != nil {
		return err
	}
	s.rebuildSubtitles()
	return nil
}

// Trakt returns the current Trakt client, safe to call concurrently with
// SetTraktClientID.
func (s *Server) Trakt() *trakt.Client {
	s.traktMu.RLock()
	defer s.traktMu.RUnlock()
	return s.trakt
}

// SetTraktClientID updates both the persisted setting and the live client. An
// empty id clears the person's own client ID and puts the built-in one (if this
// release ships one) back in use.
func (s *Server) SetTraktClientID(id string) error {
	if err := s.Settings.Set(settings.KeyTraktClientID, id, true); err != nil {
		return err
	}
	if id == "" {
		id = s.builtin.TraktClientID
	}
	s.traktMu.Lock()
	s.trakt.SetClientID(id)
	s.traktMu.Unlock()
	return nil
}

// Routes builds the full handler: public onboarding/auth endpoints, then
// everything else behind session/API-key auth, then (in production) the
// embedded frontend for anything not under /api/.
func (s *Server) Routes() http.Handler {
	public := http.NewServeMux()
	public.HandleFunc("GET /api/version", s.handleVersion)
	public.HandleFunc("GET /api/onboarding/status", s.handleOnboardingStatus)
	public.HandleFunc("POST /api/onboarding/admin", s.handleCreateAdmin)
	public.HandleFunc("POST /api/auth/login", s.handleLogin)
	public.HandleFunc("POST /api/auth/logout", s.handleLogout)
	// Calendar apps read the feed without signing in; the secret address is the key.
	public.HandleFunc("GET /api/calendar/feed/{file}", s.handleCalendarFeed)

	public.Handle("/api/", s.signedIn(s.protectedRoutes()))

	root := http.NewServeMux()
	root.Handle("/api/", s.busyGuard(noStore(public)))
	root.HandleFunc("GET /robots.txt", robotsTxt)
	root.Handle("/", frontendHandler())
	return s.harden(root)
}

// protectedRoutes is the one table of every signed-in route and who may call
// it. member routes are open to every account (family members included):
// finding, adding and following titles. admin routes are everything that
// shows or changes how Mediarium is set up. A new route has to be put in one
// of the two lists; routes_access_test.go fails if the member list changes
// without the test being updated too.
func (s *Server) protectedRoutes() *routeTable {
	t := newRouteTable()
	t.gate = s.moduleRouteOpen // /api/music/ and /api/books/ answer 404 while their module is off
	t.permsOf = s.Auth.PermissionsOf
	member := t.group(accessMember)
	admin := t.group(accessAdmin)
	// What a basic account needs for a route (Settings > Accounts).
	releases := member.Need(auth.PermReleases)
	manage := member.Need(auth.PermManage)
	play := member.Need(auth.PermPlay)
	subs := member.Need(auth.PermSubtitles)

	// ---- Members and administrators ----

	// Their own account.
	member.HandleFunc("GET /api/auth/me", s.handleMe)
	member.HandleFunc("PUT /api/auth/profile", s.handleUpdateUserProfile)
	member.HandleFunc("POST /api/auth/change-password", s.handleChangePassword)
	member.HandleFunc("GET /api/auth/api-keys", s.handleListAPIKeys)
	member.HandleFunc("POST /api/auth/api-keys", s.handleCreateAPIKey)
	member.HandleFunc("DELETE /api/auth/api-keys/{id}", s.handleRevokeAPIKey)

	// Overview (members get a trimmed view: no setup problems, no folder paths).
	member.HandleFunc("GET /api/health", s.handleHealth)
	member.HandleFunc("GET /api/dashboard", s.handleDashboard)
	member.HandleFunc("GET /api/calendar", s.handleCalendar)
	member.HandleFunc("GET /api/stats/library", s.handleLibraryStats)
	member.HandleFunc("GET /api/exclusions", s.handleListExclusions)
	member.HandleFunc("POST /api/exclusions", s.handleAddExclusion)
	member.HandleFunc("DELETE /api/exclusions/{kind}/{tmdbId}", s.handleRemoveExclusion)
	member.HandleFunc("GET /api/calendar/feed", s.handleGetCalendarFeed)
	member.HandleFunc("POST /api/calendar/feed", s.handleNewCalendarFeed)
	member.HandleFunc("DELETE /api/calendar/feed", s.handleRemoveCalendarFeed)
	member.HandleFunc("GET /api/wanted", s.handleWanted)
	member.HandleFunc("GET /api/activity", s.handleListActivity)
	member.HandleFunc("GET /api/modules", s.handleModules)
	member.HandleFunc("GET /api/watched", s.handleWatched)
	member.HandleFunc("GET /api/requests", s.handleListRequests)
	member.HandleFunc("DELETE /api/requests/{id}", s.handleDeleteRequest)

	// Finding things.
	member.HandleFunc("GET /api/discover/trending", s.handleTrending)
	member.HandleFunc("GET /api/discover/popular", s.handlePopular)
	member.HandleFunc("GET /api/discover/tv/trending", s.handleTrendingTV)
	member.HandleFunc("GET /api/discover/tv/popular", s.handlePopularTV)
	member.HandleFunc("GET /api/discover/search", s.handleTitleSearch)
	member.HandleFunc("GET /api/discover/genres", s.handleDiscoverGenres)
	member.HandleFunc("GET /api/discover/browse", s.handleDiscoverBrowse)
	member.HandleFunc("GET /api/discover/list", s.handleDiscoverList)
	member.HandleFunc("GET /api/discover/import-list", s.handleImportList)
	member.HandleFunc("GET /api/discover/for-you", s.handleForYou)
	member.HandleFunc("GET /api/tv/search", s.handleSearchTV)
	member.HandleFunc("GET /api/tmdb/search", s.handleTMDBSearch)
	member.HandleFunc("GET /api/tmdb/movies/{tmdbId}", s.handleTMDBMovieDetail)
	member.HandleFunc("GET /api/tmdb/movies/{tmdbId}/similar", s.handleTMDBSimilarMovies)
	member.HandleFunc("GET /api/tmdb/tv/{tmdbId}", s.handleTMDBTVDetail)
	releases.HandleFunc("GET /api/search", s.handleSearch)
	releases.HandleFunc("POST /api/search/grab", s.handleSearchGrab)
	// The add dialog lists the profiles to choose from.
	member.HandleFunc("GET /api/quality-profiles", s.handleListProfiles)

	// Movies: browse, add (choosing profile and downloaders), search, grab, monitor.
	member.HandleFunc("GET /api/movies", revalidated(s.handleListMovies))
	member.Need(auth.PermMovies).HandleFunc("POST /api/movies", s.handleAddMovie)
	member.HandleFunc("GET /api/movies/{id}", s.handleGetMovie)
	member.HandleFunc("GET /api/movies/{id}/similar", s.handleSimilarMovies)
	releases.HandleFunc("GET /api/movies/{id}/search", s.handleMovieSearch)
	manage.HandleFunc("POST /api/movies/{id}/search-now", s.handleMovieSearchNow)
	releases.HandleFunc("POST /api/movies/{id}/grab", s.handleGrab)
	member.HandleFunc("GET /api/tags", s.handleListTags)
	manage.HandleFunc("PUT /api/movies/{id}/tags", s.handleSetMovieTags)
	manage.HandleFunc("PUT /api/series/{id}/tags", s.handleSetSeriesTags)
	manage.HandleFunc("PUT /api/movies/{id}/monitored", s.handleSetMovieMonitored)

	// Shows: the same.
	member.HandleFunc("GET /api/series", revalidated(s.handleListSeries))
	member.Need(auth.PermTV).HandleFunc("POST /api/series", s.handleAddSeries)
	member.HandleFunc("GET /api/series/{id}", s.handleGetSeries)
	member.HandleFunc("POST /api/series/{id}/refresh", s.handleRefreshSeries)
	releases.HandleFunc("GET /api/series/{id}/search", s.handleSeriesSearch)
	manage.HandleFunc("POST /api/series/{id}/search-now", s.handleSeriesSearchNow)
	releases.HandleFunc("POST /api/series/{id}/grab", s.handleGrabSeries)
	manage.HandleFunc("PUT /api/series/{id}/monitored", s.handleSetSeriesMonitored)
	manage.HandleFunc("PUT /api/series/{id}/type", s.handleSetSeriesType)
	manage.HandleFunc("PUT /api/series/{id}/seasons/{season}/monitored", s.handleSetSeasonMonitored)
	manage.HandleFunc("PUT /api/episodes/{id}/monitored", s.handleSetEpisodeMonitored)

	// Books: ebooks and audiobooks (404 while both are off).
	member.HandleFunc("GET /api/books", s.handleListBooks)
	member.Need(auth.PermBooks).HandleFunc("POST /api/books", s.handleAddBook)
	member.HandleFunc("GET /api/books/search", s.handleBookSearch)
	member.HandleFunc("GET /api/books/discover", s.handleBookDiscover)
	member.HandleFunc("GET /api/books/subjects", s.handleBookSubjects)
	member.HandleFunc("GET /api/books/{id}/links", s.handleBookLinks)
	member.HandleFunc("GET /api/books/progress", s.handleListBookProgress)
	member.HandleFunc("GET /api/book-authors", s.handleFollowedAuthors)
	member.HandleFunc("GET /api/book-works/{key}", s.handleBookWork)
	member.HandleFunc("GET /api/book-authors/{key}/works", s.handleAuthorWorks)
	member.Need(auth.PermBooks, auth.PermManage).HandleFunc("PUT /api/book-authors/{key}/follow", s.handleFollowAuthor)
	manage.HandleFunc("DELETE /api/book-authors/{key}/follow", s.handleUnfollowAuthor)
	member.HandleFunc("GET /api/book-works/{key}/series", s.handleBookSeries)
	member.HandleFunc("GET /api/book-series", s.handleFollowedSeries)
	member.Need(auth.PermBooks, auth.PermManage).HandleFunc("PUT /api/book-series/{source}/{key}/follow", s.handleFollowSeries)
	manage.HandleFunc("DELETE /api/book-series/{source}/{key}/follow", s.handleUnfollowSeries)
	member.HandleFunc("GET /api/books/{id}/progress", s.handleGetBookProgress)
	member.HandleFunc("PUT /api/books/{id}/progress", s.handleSetBookProgress)
	play.HandleFunc("GET /api/books/{id}/read", s.handleReadBook)
	play.HandleFunc("GET /api/books/{id}/tracks", s.handleBookTracks)
	play.HandleFunc("GET /api/books/{id}/listen/{n}", s.handleListenBook)
	admin.HandleFunc("GET /api/books/import", s.handleBookImportStatus)
	admin.HandleFunc("POST /api/books/import", s.handleStartBookImport)
	member.HandleFunc("GET /api/books/{id}", s.handleGetBook)
	manage.HandleFunc("PUT /api/books/{id}/want", s.handleSetBookWant)
	manage.HandleFunc("POST /api/books/{id}/search", s.handleSearchBookNow)
	admin.HandleFunc("DELETE /api/books/{id}", s.handleDeleteBook)
	releases.HandleFunc("GET /api/books/{id}/releases", s.handleBookReleases)
	releases.HandleFunc("POST /api/books/{id}/grab", s.handleGrabBook)

	// Music: the same for artists and albums (404 while the music module is off).
	member.HandleFunc("GET /api/music/profiles", s.handleMusicProfiles)
	member.HandleFunc("GET /api/music/tiers", s.handleMusicTiers)
	member.HandleFunc("GET /api/music/search", s.handleMusicSearchArtists)
	member.HandleFunc("GET /api/music/discover", s.handleMusicDiscover)
	member.HandleFunc("GET /api/music/discover/artists", s.handleMusicDiscoverArtists)
	member.HandleFunc("GET /api/music/covers/release-group/{mbid}", s.handleReleaseGroupCover)
	member.HandleFunc("GET /api/music/artists", s.handleListArtists)
	member.Need(auth.PermMusic).HandleFunc("POST /api/music/artists", s.handleAddArtist)
	member.HandleFunc("GET /api/music/artists/{id}", s.handleGetArtist)
	member.HandleFunc("GET /api/music/albums/{id}", s.handleGetAlbum)
	member.HandleFunc("GET /api/music/albums/{id}/files", s.handleAlbumFiles)
	member.HandleFunc("GET /api/music/albums/{id}/cover", s.handleAlbumCover)
	member.HandleFunc("GET /api/music/artists/{id}/cover", s.handleArtistCover)
	manage.HandleFunc("PUT /api/music/albums/{id}/monitored", s.handleSetAlbumMonitored)
	releases.HandleFunc("POST /api/music/albums/{id}/search", s.handleAlbumSearch)
	manage.HandleFunc("POST /api/music/albums/{id}/search-now", s.handleAlbumSearchNow)
	releases.HandleFunc("POST /api/music/albums/{id}/grab", s.handleAlbumGrab)
	member.HandleFunc("GET /api/music/albums/{id}/events", s.handleAlbumEvents)
	member.HandleFunc("GET /api/music/wanted", s.handleMusicWanted)

	// Files on disk, and playing them in the browser.
	member.HandleFunc("GET /api/movies/{id}/files", s.handleMovieFiles)
	member.HandleFunc("GET /api/series/{id}/files", s.handleSeriesFiles)
	member.HandleFunc("GET /api/movies/{id}/events", s.handleMovieEvents)
	member.HandleFunc("GET /api/series/{id}/events", s.handleSeriesEvents)
	play.HandleFunc("GET /api/files/stream", s.handleStreamFile)

	// "Watch in Plex/Jellyfin/Emby" and "Open my media server" links.
	member.HandleFunc("GET /api/media-servers/links", s.handleMediaServerLinks)

	// Downloads: watch and retry.
	member.HandleFunc("GET /api/queue", s.handleListQueue)
	member.Need(auth.PermRetry).HandleFunc("POST /api/queue/{id}/retry", s.handleRetryQueueItem)

	// Subtitles for titles in the library.
	member.HandleFunc("GET /api/subtitles/wanted", s.handleSubtitlesWanted)
	subs.HandleFunc("POST /api/subtitles/get", s.handleSubtitlesGet)
	subs.HandleFunc("POST /api/subtitles/timing", s.handleSubtitleTiming)
	subs.HandleFunc("GET /api/movies/{id}/subtitles", s.handleSearchMovieSubtitles)
	subs.HandleFunc("POST /api/movies/{id}/subtitles/download", s.handleDownloadMovieSubtitle)
	member.HandleFunc("GET /api/movies/{id}/subtitles/status", s.handleMovieSubtitleStatus)
	subs.HandleFunc("GET /api/episodes/{id}/subtitles", s.handleSearchEpisodeSubtitles)
	subs.HandleFunc("POST /api/episodes/{id}/subtitles/download", s.handleDownloadEpisodeSubtitle)
	member.HandleFunc("GET /api/episodes/{id}/subtitles/status", s.handleEpisodeSubtitleStatus)

	// ---- Administrators only ----

	// Accounts.
	admin.HandleFunc("GET /api/users", s.handleListUsers)
	admin.HandleFunc("POST /api/users", s.handleCreateUser)
	admin.HandleFunc("PUT /api/users/{id}", s.handleUpdateUser)
	admin.HandleFunc("DELETE /api/users/{id}", s.handleDeleteUser)

	// Settings.
	admin.HandleFunc("GET /api/settings", s.handleGetSettings)
	admin.HandleFunc("PUT /api/settings", s.handlePutSettings)
	admin.HandleFunc("GET /api/settings/filesystem-check", s.handleFilesystemCheck)
	admin.HandleFunc("POST /api/settings/test-service", s.handleTestService)
	admin.HandleFunc("GET /api/settings/folder-check", s.handleFolderCheck)
	admin.HandleFunc("POST /api/settings/folder-create", s.handleCreateFolder)
	admin.HandleFunc("GET /api/settings/naming-preview", s.handleNamingPreview)

	// Indexers and Usenet servers.
	admin.HandleFunc("GET /api/indexers", s.handleListIndexers)
	admin.HandleFunc("POST /api/indexers", s.handleCreateIndexer)
	admin.HandleFunc("PUT /api/indexers/{id}", s.handleUpdateIndexer)
	admin.HandleFunc("DELETE /api/indexers/{id}", s.handleDeleteIndexer)
	admin.HandleFunc("GET /api/indexer-definitions", s.handleListIndexerDefinitions)
	admin.HandleFunc("POST /api/indexers/test", s.handleTestIndexerConfig)
	admin.HandleFunc("POST /api/indexers/{id}/test", s.handleTestIndexer)
	admin.HandleFunc("PUT /api/indexers/{id}/enabled", s.handleSetIndexerEnabled)
	admin.HandleFunc("PUT /api/indexers/{id}/priority", s.handleSetIndexerPriority)
	admin.HandleFunc("GET /api/usenet-servers", s.handleListUsenetServers)
	admin.HandleFunc("POST /api/usenet-servers", s.handleCreateUsenetServer)
	admin.HandleFunc("PUT /api/usenet-servers/{id}", s.handleUpdateUsenetServer)
	admin.HandleFunc("DELETE /api/usenet-servers/{id}", s.handleDeleteUsenetServer)
	admin.HandleFunc("POST /api/usenet-servers/test", s.handleTestUsenetServerConfig)
	admin.HandleFunc("POST /api/usenet-servers/{id}/test", s.handleTestUsenetServer)
	admin.HandleFunc("GET /api/downloads/status", s.handleDownloadsStatus)

	// Library management: removing titles, per-title profile and downloaders, imports.
	admin.HandleFunc("DELETE /api/movies/{id}", s.handleDeleteMovie)
	admin.HandleFunc("GET /api/movies/{id}/disk-usage", s.handleMovieDiskUsage)
	admin.HandleFunc("PUT /api/movies/{id}/sources", s.handleSetMovieSources)
	admin.HandleFunc("PUT /api/movies/{id}/profile", s.handleSetMovieProfile)
	admin.HandleFunc("PUT /api/movies/{id}/no-upgrade", s.handleSetMovieNoUpgrade)
	admin.HandleFunc("DELETE /api/series/{id}", s.handleDeleteSeries)
	admin.HandleFunc("GET /api/series/{id}/disk-usage", s.handleSeriesDiskUsage)
	admin.HandleFunc("PUT /api/series/{id}/sources", s.handleSetSeriesSources)
	admin.HandleFunc("PUT /api/series/{id}/profile", s.handleSetSeriesProfile)
	admin.HandleFunc("PUT /api/series/{id}/no-upgrade", s.handleSetSeriesNoUpgrade)
	admin.HandleFunc("PUT /api/library/bulk/monitored", s.handleBulkMonitored)
	admin.HandleFunc("PUT /api/library/bulk/no-upgrade", s.handleBulkNoUpgrade)
	admin.HandleFunc("PUT /api/library/bulk/profile", s.handleBulkProfile)
	admin.HandleFunc("PUT /api/library/bulk/tags", s.handleBulkTags)
	admin.HandleFunc("PUT /api/library/bulk/sources", s.handleBulkSources)
	admin.HandleFunc("POST /api/library/bulk/search-now", s.handleBulkSearchNow)
	admin.HandleFunc("POST /api/library/bulk/remove", s.handleBulkRemove)
	admin.HandleFunc("POST /api/library/scan", s.handleScanLibrary)
	admin.HandleFunc("GET /api/library/scan/{id}", s.handleGetImportJob)
	admin.HandleFunc("POST /api/library/import", s.handleRunImport)
	admin.HandleFunc("GET /api/library/import/active", s.handleActiveImports)
	admin.HandleFunc("GET /api/library/import/batches/{id}", s.handleGetImportBatch)
	admin.HandleFunc("POST /api/library/import/batches/{id}/dismiss", s.handleDismissImportBatch)
	admin.HandleFunc("POST /api/library/import/batches/{id}/watch", s.handleWatchImportBatch)
	admin.HandleFunc("PUT /api/music/artists/{id}", s.handleUpdateArtist)
	admin.HandleFunc("PUT /api/music/bulk/follow", s.handleBulkArtistFollow)
	admin.HandleFunc("PUT /api/music/bulk/profile", s.handleBulkArtistProfile)
	admin.HandleFunc("POST /api/music/bulk/remove", s.handleBulkArtistRemove)
	admin.HandleFunc("DELETE /api/music/artists/{id}", s.handleDeleteArtist)
	admin.HandleFunc("GET /api/music/artists/{id}/disk-usage", s.handleArtistDiskUsage)
	admin.HandleFunc("PUT /api/modules", s.handlePutModules)
	admin.HandleFunc("POST /api/music/profiles", s.handleCreateMusicProfile)
	admin.HandleFunc("PUT /api/music/profiles/{id}", s.handleUpdateMusicProfile)
	admin.HandleFunc("DELETE /api/music/profiles/{id}", s.handleDeleteMusicProfile)
	admin.HandleFunc("POST /api/music/import/scan", s.handleMusicImportScan)
	admin.HandleFunc("GET /api/music/import/scan/{id}", s.handleMusicImportResults)

	// Moving over from Radarr, Sonarr, Prowlarr and SABnzbd (read-only on their side).
	admin.HandleFunc("GET /api/system/backups", s.handleListSavedBackups)
	admin.HandleFunc("POST /api/system/backups", s.handleSaveBackupNow)
	admin.HandleFunc("GET /api/system/backups/{name}", s.handleDownloadSavedBackup)
	admin.HandleFunc("GET /api/rename", s.handleRenamePreview)
	admin.HandleFunc("POST /api/rename", s.handleRename)
	admin.HandleFunc("GET /api/manual-import", s.handleManualImportList)
	admin.HandleFunc("POST /api/manual-import", s.handleManualImport)
	admin.HandleFunc("GET /api/trash", s.handleListTrash)
	admin.HandleFunc("POST /api/trash/{kind}/{id}/restore", s.handleRestoreTrash)
	admin.HandleFunc("DELETE /api/trash/{kind}/{id}", s.handleDeleteTrash)
	admin.HandleFunc("DELETE /api/trash", s.handleEmptyTrash)
	admin.HandleFunc("POST /api/migrate/preview", s.handleMigratePreview)
	admin.HandleFunc("POST /api/migrate/run", s.handleMigrateRun)
	admin.HandleFunc("GET /api/migrate/status", s.handleMigrateStatus)

	// Quality profiles (changing them).
	admin.HandleFunc("POST /api/quality-profiles", s.handleCreateProfile)
	admin.HandleFunc("PUT /api/quality-profiles/{id}", s.handleUpdateProfile)
	admin.HandleFunc("DELETE /api/quality-profiles/{id}", s.handleDeleteProfile)

	// Queue housekeeping and the blocklist.
	admin.HandleFunc("DELETE /api/queue/{id}", s.handleDeleteQueueItem)
	admin.HandleFunc("DELETE /api/queue", s.handleClearFinishedQueue)
	admin.HandleFunc("POST /api/queue/{id}/blocklist", s.handleBlocklistQueueItem)
	admin.HandleFunc("POST /api/queue/{id}/resolve-conflict", s.handleResolveQueueConflict)
	admin.HandleFunc("POST /api/queue/{id}/pause", s.handlePauseQueueItem)
	admin.HandleFunc("POST /api/queue/{id}/resume", s.handleResumeQueueItem)
	admin.HandleFunc("POST /api/queue/{id}/stop", s.handleStopQueueItem)
	admin.HandleFunc("POST /api/queue/pause-all", s.handlePauseAllQueue)
	admin.HandleFunc("POST /api/queue/resume-all", s.handleResumeAllQueue)
	admin.HandleFunc("GET /api/blocklist", s.handleListBlocklist)
	admin.HandleFunc("DELETE /api/blocklist/{id}", s.handleRemoveBlocklistEntry)
	admin.HandleFunc("DELETE /api/blocklist", s.handleClearBlocklist)

	// Subtitle housekeeping and the shared-service quotas.
	admin.HandleFunc("POST /api/subtitles/sweep", s.handleSubtitleSweep)
	admin.HandleFunc("GET /api/subtitles/quota", s.handleSubtitleQuota)
	admin.HandleFunc("POST /api/subtitles/dismiss", s.handleSubtitlesDismiss)
	admin.HandleFunc("DELETE /api/subtitles/dismiss", s.handleSubtitlesRestore)
	admin.HandleFunc("GET /api/usage", s.handleUsage)

	// VPN.
	admin.HandleFunc("GET /api/vpn/configs", s.handleListVPNConfigs)
	admin.HandleFunc("POST /api/vpn/configs", s.handleCreateVPNConfig)
	admin.HandleFunc("DELETE /api/vpn/configs/{id}", s.handleDeleteVPNConfig)
	admin.HandleFunc("POST /api/vpn/configs/{id}/activate", s.handleActivateVPNConfig)
	admin.HandleFunc("POST /api/vpn/deactivate", s.handleDeactivateVPN)
	admin.HandleFunc("GET /api/vpn/status", s.handleVPNStatus)
	admin.HandleFunc("GET /api/vpn/egress-ip", s.handleVPNEgressIP)

	// Notifications.
	admin.HandleFunc("GET /api/notifications", s.handleListNotifyTargets)
	admin.HandleFunc("POST /api/notifications", s.handleCreateNotifyTarget)
	admin.HandleFunc("PUT /api/notifications/{id}", s.handleUpdateNotifyTarget)
	admin.HandleFunc("DELETE /api/notifications/{id}", s.handleDeleteNotifyTarget)
	admin.HandleFunc("GET /api/notifications/types", s.handleNotifyTypes)
	admin.HandleFunc("GET /api/notifications/events", s.handleNotifyEvents)
	admin.HandleFunc("POST /api/notifications/test", s.handleTestNotifyTarget)

	// Media servers (Plex, Jellyfin, Emby).
	admin.HandleFunc("GET /api/media-servers", s.handleListMediaServers)
	admin.HandleFunc("POST /api/media-servers", s.handleCreateMediaServer)
	admin.HandleFunc("PUT /api/media-servers/{id}", s.handleUpdateMediaServer)
	admin.HandleFunc("DELETE /api/media-servers/{id}", s.handleDeleteMediaServer)
	admin.HandleFunc("POST /api/media-servers/test", s.handleTestMediaServerConfig)
	admin.HandleFunc("POST /api/media-servers/{id}/test", s.handleTestMediaServer)
	admin.HandleFunc("POST /api/media-servers/{id}/refresh", s.handleRefreshMediaServer)
	admin.HandleFunc("POST /api/media-servers/discover", s.handleDiscoverMediaServers)
	admin.HandleFunc("POST /api/media-servers/plex/pin", s.handleStartPlexSignIn)
	admin.HandleFunc("GET /api/media-servers/plex/pin/{pinId}", s.handlePollPlexSignIn)
	admin.HandleFunc("POST /api/media-servers/plex/pin/{pinId}/add", s.handleAddPlexServer)
	admin.HandleFunc("POST /api/media-servers/jellyfin/quickconnect", s.handleStartQuickConnect)
	admin.HandleFunc("GET /api/media-servers/jellyfin/quickconnect/{id}", s.handlePollQuickConnect)
	admin.HandleFunc("POST /api/media-servers/login", s.handleMediaServerLogin)

	// Connectivity monitor, metrics, backup and restore.
	admin.HandleFunc("GET /api/monitor/status", s.handleMonitorStatus)
	admin.HandleFunc("POST /api/monitor/run", s.handleMonitorRun)
	admin.HandleFunc("GET /api/metrics", s.handleMetrics)
	admin.HandleFunc("GET /api/system/info", s.handleSystemInfo)
	admin.HandleFunc("GET /api/system/backup", s.handleBackup)
	admin.HandleFunc("POST /api/system/restore", s.handleRestore)
	admin.HandleFunc("GET /api/system/stats", s.handleSystemStats)
	admin.HandleFunc("GET /api/system/diagnostics", s.handleDiagnostics)
	admin.HandleFunc("GET /api/system/problems", s.handleProblems)
	admin.HandleFunc("POST /api/system/problems/read", s.handleProblemsRead)
	admin.HandleFunc("GET /api/system/problems/export", s.handleProblemsExport)
	admin.HandleFunc("PUT /api/system/problems/notify", s.handleProblemsNotify)
	admin.HandleFunc("GET /api/system/update", s.handleUpdateState)
	admin.HandleFunc("POST /api/system/update", s.handlePushUpdate)
	admin.HandleFunc("DELETE /api/system/update", s.handleRemoveUpdate)
	admin.HandleFunc("GET /api/system/update/latest", s.handleUpdateLatest)
	admin.HandleFunc("POST /api/system/update/check", s.handleUpdateCheck)
	admin.HandleFunc("GET /api/system/update/install", s.handleInstallStatus)
	admin.HandleFunc("POST /api/system/update/install", s.handleInstallUpdate)
	admin.HandleFunc("GET /api/system/options", s.handleGetOptions)
	admin.HandleFunc("PUT /api/system/options", s.handlePutOptions)
	admin.HandleFunc("GET /api/auth/proxy-signin", s.handleGetProxySignIn)
	admin.HandleFunc("PUT /api/auth/proxy-signin", s.handlePutProxySignIn)
	admin.HandleFunc("GET /api/scripts", s.handleGetScripts)
	admin.HandleFunc("PUT /api/scripts", s.handlePutScripts)
	admin.HandleFunc("POST /api/scripts/test", s.handleTestScript)
	admin.HandleFunc("POST /api/requests/{id}/approve", s.handleApproveRequest)
	admin.HandleFunc("POST /api/requests/{id}/decline", s.handleDeclineRequest)
	admin.HandleFunc("GET /api/watched/settings", s.handleGetWatchedSettings)
	admin.HandleFunc("PUT /api/watched/settings", s.handlePutWatchedSettings)
	admin.HandleFunc("POST /api/watched/sync", s.handleSyncWatched)
	admin.HandleFunc("GET /api/watched/cleanup/preview", s.handleLibraryCleanupPreview)
	admin.HandleFunc("POST /api/watched/cleanup/run", s.handleLibraryCleanupRun)
	admin.HandleFunc("GET /api/settings/hardcover", s.handleGetHardcover)
	admin.HandleFunc("PUT /api/settings/hardcover", s.handlePutHardcover)
	admin.HandleFunc("POST /api/system/restart", s.handleRestart)
	admin.HandleFunc("POST /api/system/shutdown", s.handleShutdown)
	admin.HandleFunc("GET /api/flaresolverr/status", s.handleFlareSolverrStatus)
	admin.HandleFunc("GET /api/system/cleanup", s.handleCleanupStatus)
	admin.HandleFunc("POST /api/system/cleanup", s.handleCleanupRun)

	return t
}

// noStore tells browsers and any proxy in front of the app never to keep an
// API response: settings, queue and library data change constantly, and a
// stale copy would show a switch or a list as it used to be, not as the
// server has it now.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
