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

	"github.com/ryanborg/mediarium/internal/auth"
	"github.com/ryanborg/mediarium/internal/blocklist"
	"github.com/ryanborg/mediarium/internal/config"
	"github.com/ryanborg/mediarium/internal/crypto"
	"github.com/ryanborg/mediarium/internal/download"
	"github.com/ryanborg/mediarium/internal/indexers"
	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/mediaservers"
	"github.com/ryanborg/mediarium/internal/metadata"
	"github.com/ryanborg/mediarium/internal/monitor"
	"github.com/ryanborg/mediarium/internal/notify"
	"github.com/ryanborg/mediarium/internal/quality"
	"github.com/ryanborg/mediarium/internal/queue"
	"github.com/ryanborg/mediarium/internal/settings"
	"github.com/ryanborg/mediarium/internal/subtitles"
	"github.com/ryanborg/mediarium/internal/trakt"
	"github.com/ryanborg/mediarium/internal/usage"
	"github.com/ryanborg/mediarium/internal/vpn"
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
	SubtitleDismissed *subtitles.DismissRepo
	SubtitleQuota     *subtitles.QuotaRepo
	Usage             *usage.Tracker // requests and limit hits per third-party service
	MediaServers      *mediaservers.Repo

	mediaClient    *mediaservers.Client
	mediaRefresher *mediaservers.Refresher // rescans Plex/Jellyfin/Emby after imports
	mediaFinder    *mediaservers.Finder    // "Watch in" lookups, cached briefly
	mediaSignIn    mediaSignIn             // discovery and sign-ins in progress (handlers_media_servers_signin.go)

	LoginLimiter *auth.LoginLimiter
	version      string

	tmdbMu sync.RWMutex
	tmdb   *metadata.Client

	subsMu sync.RWMutex
	subs   *subtitles.Client

	traktMu sync.RWMutex
	trakt   *trakt.Client

	statsCache statsCache
	builtin    BuiltinKeys // app-wide keys baked into official releases (see keys.go)

	importMu   sync.Mutex
	importJobs map[string]*importJob

	torrents torrentRegistry // live torrent clients, so the on/off switch can stop them

	grabMu sync.Mutex     // makes "is this already being downloaded?" and "queue it" one step (see grab_claim.go)
	bg     sync.WaitGroup // download pipelines and automatic retries still running (see grab_claim.go)

	pipelines pipelineRegistry // running download pipelines, so removing a title can cancel its downloads

	offered offeredURLs // download URLs recently handed out in search results (see grab_urls.go)

	Monitor *monitor.Monitor // connectivity monitor (Usenet servers, indexers)

	restoreMu    sync.Mutex    // one restore upload at a time
	exitFn       func()        // what to do once a restore is staged (see SetExitFunc)
	restartDelay time.Duration // zero means defaultRestartDelay
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
	if err := s.initProfiles(); err != nil {
		return nil, fmt.Errorf("init quality profiles: %w", err)
	}
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

	public.Handle("/api/", s.Auth.Middleware(s.protectedRoutes()))

	root := http.NewServeMux()
	root.Handle("/api/", noStore(public))
	root.Handle("/", frontendHandler())
	return root
}

// protectedRoutes is the one table of every signed-in route and who may call
// it. member routes are open to every account (family members included):
// finding, adding and following titles. admin routes are everything that
// shows or changes how Mediarium is set up. A new route has to be put in one
// of the two lists; routes_access_test.go fails if the member list changes
// without the test being updated too.
func (s *Server) protectedRoutes() *routeTable {
	t := newRouteTable()
	member := t.group(accessMember)
	admin := t.group(accessAdmin)

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
	member.HandleFunc("GET /api/wanted", s.handleWanted)
	member.HandleFunc("GET /api/activity", s.handleListActivity)

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
	member.HandleFunc("GET /api/search", s.handleSearch)
	member.HandleFunc("POST /api/search/grab", s.handleSearchGrab)
	// The add dialog lists the profiles to choose from.
	member.HandleFunc("GET /api/quality-profiles", s.handleListProfiles)

	// Movies: browse, add (choosing profile and downloaders), search, grab, monitor.
	member.HandleFunc("GET /api/movies", s.handleListMovies)
	member.HandleFunc("POST /api/movies", s.handleAddMovie)
	member.HandleFunc("GET /api/movies/{id}", s.handleGetMovie)
	member.HandleFunc("GET /api/movies/{id}/similar", s.handleSimilarMovies)
	member.HandleFunc("GET /api/movies/{id}/search", s.handleMovieSearch)
	member.HandleFunc("POST /api/movies/{id}/search-now", s.handleMovieSearchNow)
	member.HandleFunc("POST /api/movies/{id}/grab", s.handleGrab)
	member.HandleFunc("PUT /api/movies/{id}/monitored", s.handleSetMovieMonitored)

	// Shows: the same.
	member.HandleFunc("GET /api/series", s.handleListSeries)
	member.HandleFunc("POST /api/series", s.handleAddSeries)
	member.HandleFunc("GET /api/series/{id}", s.handleGetSeries)
	member.HandleFunc("POST /api/series/{id}/refresh", s.handleRefreshSeries)
	member.HandleFunc("GET /api/series/{id}/search", s.handleSeriesSearch)
	member.HandleFunc("POST /api/series/{id}/search-now", s.handleSeriesSearchNow)
	member.HandleFunc("POST /api/series/{id}/grab", s.handleGrabSeries)
	member.HandleFunc("PUT /api/series/{id}/monitored", s.handleSetSeriesMonitored)
	member.HandleFunc("PUT /api/series/{id}/seasons/{season}/monitored", s.handleSetSeasonMonitored)
	member.HandleFunc("PUT /api/episodes/{id}/monitored", s.handleSetEpisodeMonitored)

	// Files on disk, and playing them in the browser.
	member.HandleFunc("GET /api/movies/{id}/files", s.handleMovieFiles)
	member.HandleFunc("GET /api/series/{id}/files", s.handleSeriesFiles)
	member.HandleFunc("GET /api/movies/{id}/events", s.handleMovieEvents)
	member.HandleFunc("GET /api/series/{id}/events", s.handleSeriesEvents)
	member.HandleFunc("GET /api/files/stream", s.handleStreamFile)

	// "Watch in Plex/Jellyfin/Emby" and "Open my media server" links.
	member.HandleFunc("GET /api/media-servers/links", s.handleMediaServerLinks)

	// Downloads: watch and retry.
	member.HandleFunc("GET /api/queue", s.handleListQueue)
	member.HandleFunc("POST /api/queue/{id}/retry", s.handleRetryQueueItem)

	// Subtitles for titles in the library.
	member.HandleFunc("GET /api/subtitles/wanted", s.handleSubtitlesWanted)
	member.HandleFunc("POST /api/subtitles/get", s.handleSubtitlesGet)
	member.HandleFunc("GET /api/movies/{id}/subtitles", s.handleSearchMovieSubtitles)
	member.HandleFunc("POST /api/movies/{id}/subtitles/download", s.handleDownloadMovieSubtitle)
	member.HandleFunc("GET /api/movies/{id}/subtitles/status", s.handleMovieSubtitleStatus)
	member.HandleFunc("GET /api/episodes/{id}/subtitles", s.handleSearchEpisodeSubtitles)
	member.HandleFunc("POST /api/episodes/{id}/subtitles/download", s.handleDownloadEpisodeSubtitle)
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
	admin.HandleFunc("GET /api/usenet-servers", s.handleListUsenetServers)
	admin.HandleFunc("POST /api/usenet-servers", s.handleCreateUsenetServer)
	admin.HandleFunc("PUT /api/usenet-servers/{id}", s.handleUpdateUsenetServer)
	admin.HandleFunc("DELETE /api/usenet-servers/{id}", s.handleDeleteUsenetServer)
	admin.HandleFunc("POST /api/usenet-servers/test", s.handleTestUsenetServerConfig)
	admin.HandleFunc("POST /api/usenet-servers/{id}/test", s.handleTestUsenetServer)
	admin.HandleFunc("GET /api/downloads/status", s.handleDownloadsStatus)

	// Library management: removing titles, per-title profile and downloaders, imports.
	admin.HandleFunc("DELETE /api/movies/{id}", s.handleDeleteMovie)
	admin.HandleFunc("PUT /api/movies/{id}/sources", s.handleSetMovieSources)
	admin.HandleFunc("PUT /api/movies/{id}/profile", s.handleSetMovieProfile)
	admin.HandleFunc("DELETE /api/series/{id}", s.handleDeleteSeries)
	admin.HandleFunc("PUT /api/series/{id}/sources", s.handleSetSeriesSources)
	admin.HandleFunc("PUT /api/series/{id}/profile", s.handleSetSeriesProfile)
	admin.HandleFunc("POST /api/library/scan", s.handleScanLibrary)
	admin.HandleFunc("GET /api/library/scan/{id}", s.handleGetImportJob)
	admin.HandleFunc("POST /api/library/import", s.handleRunImport)

	// Quality profiles (changing them).
	admin.HandleFunc("POST /api/quality-profiles", s.handleCreateProfile)
	admin.HandleFunc("PUT /api/quality-profiles/{id}", s.handleUpdateProfile)
	admin.HandleFunc("DELETE /api/quality-profiles/{id}", s.handleDeleteProfile)

	// Queue housekeeping and the blocklist.
	admin.HandleFunc("DELETE /api/queue/{id}", s.handleDeleteQueueItem)
	admin.HandleFunc("DELETE /api/queue", s.handleClearFinishedQueue)
	admin.HandleFunc("POST /api/queue/{id}/blocklist", s.handleBlocklistQueueItem)
	admin.HandleFunc("POST /api/queue/{id}/resolve-conflict", s.handleResolveQueueConflict)
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
