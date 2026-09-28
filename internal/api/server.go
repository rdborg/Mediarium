// Package api serves the REST API the web UI talks to (PRD.md §4.4 —
// "REST API served to the web UI") and, in production, the embedded
// frontend build alongside it so the whole app is one binary/one port.
package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ryanborg/mediarium/internal/auth"
	"github.com/ryanborg/mediarium/internal/blocklist"
	"github.com/ryanborg/mediarium/internal/config"
	"github.com/ryanborg/mediarium/internal/crypto"
	"github.com/ryanborg/mediarium/internal/download"
	"github.com/ryanborg/mediarium/internal/indexers"
	"github.com/ryanborg/mediarium/internal/library"
	"github.com/ryanborg/mediarium/internal/metadata"
	"github.com/ryanborg/mediarium/internal/monitor"
	"github.com/ryanborg/mediarium/internal/notify"
	"github.com/ryanborg/mediarium/internal/quality"
	"github.com/ryanborg/mediarium/internal/queue"
	"github.com/ryanborg/mediarium/internal/settings"
	"github.com/ryanborg/mediarium/internal/subtitles"
	"github.com/ryanborg/mediarium/internal/trakt"
	"github.com/ryanborg/mediarium/internal/vpn"
)

type Server struct {
	cfg config.Config
	db  *sql.DB // for backups (VACUUM INTO); repos hold their own handles

	Auth             *auth.Service
	Settings         *settings.Store
	IndexerRepo      *indexers.Repo
	ClientRepo       *download.Repo
	MovieRepo        *library.Repo
	QueueRepo        *queue.Repo
	VPNRepo          *vpn.Repo
	VPNManager       *vpn.Manager
	NotifyRepo       *notify.Repo
	QualityRepo      *quality.Repo
	Blocklist        *blocklist.Repo
	SubtitleAttempts *subtitles.AttemptRepo

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

	Monitor *monitor.Monitor // connectivity monitor (Usenet servers, indexers)

	restoreMu    sync.Mutex    // one restore upload at a time
	exitFn       func()        // what to do once a restore is staged (see SetExitFunc)
	restartDelay time.Duration // zero means defaultRestartDelay
}

// New wires every module's repo/service together (CLAUDE.md — api depends
// on the feature packages, not the reverse) and loads the current TMDB key
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

	s := &Server{
		cfg:              cfg,
		db:               db,
		Auth:             auth.New(db),
		Settings:         settingsStore,
		IndexerRepo:      indexers.NewRepo(db, box),
		ClientRepo:       download.NewRepo(db, box),
		MovieRepo:        library.NewRepo(db),
		QueueRepo:        queue.NewRepo(db),
		VPNRepo:          vpn.NewRepo(db, box),
		VPNManager:       vpn.NewManager(),
		NotifyRepo:       notify.NewRepo(db, box),
		QualityRepo:      quality.NewRepo(db),
		Blocklist:        blocklist.NewRepo(db),
		SubtitleAttempts: subtitles.NewAttemptRepo(db),
		tmdb:             metadata.New(tmdbKey),
		subs:             subtitles.New(subsKey),
		trakt:            trakt.New(traktClientID),
		// "A handful of attempts per IP/window" (PRD §5.1) — 5 tries per 15
		// minutes blunts brute-force without punishing a genuine typo.
		LoginLimiter: auth.NewLoginLimiter(5, 15*time.Minute),
		version:      version,
	}
	s.Monitor = s.newMonitor()
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

// SetTraktClientID updates both the persisted setting and the live client.
func (s *Server) SetTraktClientID(id string) error {
	if err := s.Settings.Set(settings.KeyTraktClientID, id, true); err != nil {
		return err
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

	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/auth/me", s.handleMe)
	protected.HandleFunc("PUT /api/auth/profile", s.handleUpdateUserProfile)
	protected.HandleFunc("POST /api/auth/change-password", s.handleChangePassword)
	protected.HandleFunc("GET /api/auth/api-keys", s.handleListAPIKeys)
	protected.HandleFunc("POST /api/auth/api-keys", s.handleCreateAPIKey)
	protected.HandleFunc("DELETE /api/auth/api-keys/{id}", s.handleRevokeAPIKey)

	protected.HandleFunc("GET /api/settings", s.handleGetSettings)
	protected.HandleFunc("PUT /api/settings", s.handlePutSettings)
	protected.HandleFunc("GET /api/settings/filesystem-check", s.handleFilesystemCheck)
	protected.HandleFunc("POST /api/settings/test-service", s.handleTestService)
	protected.HandleFunc("GET /api/settings/folder-check", s.handleFolderCheck)
	protected.HandleFunc("GET /api/health", s.handleHealth)
	protected.HandleFunc("GET /api/dashboard", s.handleDashboard)
	protected.HandleFunc("GET /api/settings/naming-preview", s.handleNamingPreview)

	protected.HandleFunc("GET /api/indexers", s.handleListIndexers)
	protected.HandleFunc("POST /api/indexers", s.handleCreateIndexer)
	protected.HandleFunc("DELETE /api/indexers/{id}", s.handleDeleteIndexer)
	protected.HandleFunc("POST /api/indexers/test", s.handleTestIndexerConfig)
	protected.HandleFunc("POST /api/indexers/{id}/test", s.handleTestIndexer)
	protected.HandleFunc("PUT /api/indexers/{id}/enabled", s.handleSetIndexerEnabled)

	protected.HandleFunc("GET /api/usenet-servers", s.handleListUsenetServers)
	protected.HandleFunc("POST /api/usenet-servers", s.handleCreateUsenetServer)
	protected.HandleFunc("PUT /api/usenet-servers/{id}", s.handleUpdateUsenetServer)
	protected.HandleFunc("DELETE /api/usenet-servers/{id}", s.handleDeleteUsenetServer)
	protected.HandleFunc("POST /api/usenet-servers/test", s.handleTestUsenetServerConfig)
	protected.HandleFunc("POST /api/usenet-servers/{id}/test", s.handleTestUsenetServer)
	protected.HandleFunc("GET /api/downloads/status", s.handleDownloadsStatus)

	protected.HandleFunc("GET /api/search", s.handleSearch)
	protected.HandleFunc("POST /api/search/grab", s.handleSearchGrab)

	protected.HandleFunc("GET /api/movies", s.handleListMovies)
	protected.HandleFunc("POST /api/movies", s.handleAddMovie)
	protected.HandleFunc("GET /api/movies/{id}", s.handleGetMovie)
	protected.HandleFunc("POST /api/movies/{id}/grab", s.handleGrab)

	protected.HandleFunc("DELETE /api/queue/{id}", s.handleDeleteQueueItem)
	protected.HandleFunc("DELETE /api/queue", s.handleClearFinishedQueue)
	protected.HandleFunc("POST /api/queue/{id}/retry", s.handleRetryQueueItem)
	protected.HandleFunc("POST /api/queue/{id}/blocklist", s.handleBlocklistQueueItem)
	protected.HandleFunc("GET /api/queue", s.handleListQueue)
	protected.HandleFunc("POST /api/queue/{id}/resolve-conflict", s.handleResolveQueueConflict)
	protected.HandleFunc("GET /api/activity", s.handleListActivity)

	protected.HandleFunc("GET /api/discover/trending", s.handleTrending)
	protected.HandleFunc("GET /api/discover/popular", s.handlePopular)
	protected.HandleFunc("GET /api/tv/search", s.handleSearchTV)
	protected.HandleFunc("GET /api/discover/tv/trending", s.handleTrendingTV)
	protected.HandleFunc("GET /api/discover/tv/popular", s.handlePopularTV)
	protected.HandleFunc("DELETE /api/movies/{id}", s.handleDeleteMovie)
	protected.HandleFunc("PUT /api/movies/{id}/sources", s.handleSetMovieSources)
	protected.HandleFunc("PUT /api/series/{id}/sources", s.handleSetSeriesSources)
	protected.HandleFunc("GET /api/discover/search", s.handleTitleSearch)
	protected.HandleFunc("PUT /api/movies/{id}/profile", s.handleSetMovieProfile)
	protected.HandleFunc("PUT /api/series/{id}/profile", s.handleSetSeriesProfile)
	protected.HandleFunc("GET /api/episodes/{id}/subtitles", s.handleSearchEpisodeSubtitles)
	protected.HandleFunc("POST /api/episodes/{id}/subtitles/download", s.handleDownloadEpisodeSubtitle)
	protected.HandleFunc("GET /api/episodes/{id}/subtitles/status", s.handleEpisodeSubtitleStatus)
	protected.HandleFunc("GET /api/movies/{id}/subtitles/status", s.handleMovieSubtitleStatus)
	protected.HandleFunc("GET /api/subtitles/wanted", s.handleSubtitlesWanted)
	protected.HandleFunc("POST /api/subtitles/sweep", s.handleSubtitleSweep)
	protected.HandleFunc("GET /api/movies/{id}/search", s.handleMovieSearch)
	protected.HandleFunc("POST /api/movies/{id}/search-now", s.handleMovieSearchNow)
	protected.HandleFunc("POST /api/series/{id}/search-now", s.handleSeriesSearchNow)
	protected.HandleFunc("PUT /api/movies/{id}/monitored", s.handleSetMovieMonitored)
	protected.HandleFunc("PUT /api/series/{id}/monitored", s.handleSetSeriesMonitored)
	protected.HandleFunc("PUT /api/series/{id}/seasons/{season}/monitored", s.handleSetSeasonMonitored)
	protected.HandleFunc("PUT /api/episodes/{id}/monitored", s.handleSetEpisodeMonitored)
	protected.HandleFunc("GET /api/wanted", s.handleWanted)
	protected.HandleFunc("GET /api/blocklist", s.handleListBlocklist)
	protected.HandleFunc("DELETE /api/blocklist/{id}", s.handleRemoveBlocklistEntry)
	protected.HandleFunc("DELETE /api/blocklist", s.handleClearBlocklist)
	protected.HandleFunc("GET /api/quality-profiles", s.handleListProfiles)
	protected.HandleFunc("POST /api/quality-profiles", s.handleCreateProfile)
	protected.HandleFunc("PUT /api/quality-profiles/{id}", s.handleUpdateProfile)
	protected.HandleFunc("DELETE /api/quality-profiles/{id}", s.handleDeleteProfile)
	protected.HandleFunc("GET /api/series", s.handleListSeries)
	protected.HandleFunc("POST /api/series", s.handleAddSeries)
	protected.HandleFunc("GET /api/series/{id}", s.handleGetSeries)
	protected.HandleFunc("POST /api/series/{id}/refresh", s.handleRefreshSeries)
	protected.HandleFunc("POST /api/series/{id}/grab", s.handleGrabSeries)
	protected.HandleFunc("GET /api/series/{id}/search", s.handleSeriesSearch)
	protected.HandleFunc("DELETE /api/series/{id}", s.handleDeleteSeries)
	protected.HandleFunc("GET /api/discover/import-list", s.handleImportList)
	protected.HandleFunc("GET /api/discover/for-you", s.handleForYou)
	protected.HandleFunc("POST /api/library/scan", s.handleScanLibrary)
	protected.HandleFunc("GET /api/library/scan/{id}", s.handleGetImportJob)
	protected.HandleFunc("POST /api/library/import", s.handleRunImport)
	protected.HandleFunc("GET /api/tmdb/search", s.handleTMDBSearch)

	protected.HandleFunc("GET /api/vpn/configs", s.handleListVPNConfigs)
	protected.HandleFunc("POST /api/vpn/configs", s.handleCreateVPNConfig)
	protected.HandleFunc("DELETE /api/vpn/configs/{id}", s.handleDeleteVPNConfig)
	protected.HandleFunc("POST /api/vpn/configs/{id}/activate", s.handleActivateVPNConfig)
	protected.HandleFunc("POST /api/vpn/deactivate", s.handleDeactivateVPN)
	protected.HandleFunc("GET /api/vpn/status", s.handleVPNStatus)
	protected.HandleFunc("GET /api/vpn/egress-ip", s.handleVPNEgressIP)

	protected.HandleFunc("GET /api/notifications", s.handleListNotifyTargets)
	protected.HandleFunc("POST /api/notifications", s.handleCreateNotifyTarget)
	protected.HandleFunc("PUT /api/notifications/{id}", s.handleUpdateNotifyTarget)
	protected.HandleFunc("DELETE /api/notifications/{id}", s.handleDeleteNotifyTarget)
	protected.HandleFunc("GET /api/notifications/types", s.handleNotifyTypes)
	protected.HandleFunc("GET /api/notifications/events", s.handleNotifyEvents)
	protected.HandleFunc("POST /api/notifications/test", s.handleTestNotifyTarget)

	protected.HandleFunc("GET /api/monitor/status", s.handleMonitorStatus)
	protected.HandleFunc("POST /api/monitor/run", s.handleMonitorRun)

	protected.HandleFunc("GET /api/calendar", s.handleCalendar)
	protected.HandleFunc("GET /api/metrics", s.handleMetrics)

	// Backup and restore (administrators only; checked in the handlers).
	protected.HandleFunc("GET /api/system/info", s.handleSystemInfo)
	protected.HandleFunc("GET /api/system/backup", s.handleBackup)
	protected.HandleFunc("POST /api/system/restore", s.handleRestore)
	protected.HandleFunc("GET /api/movies/{id}/similar", s.handleSimilarMovies)
	protected.HandleFunc("GET /api/tmdb/movies/{tmdbId}", s.handleTMDBMovieDetail)
	protected.HandleFunc("GET /api/tmdb/movies/{tmdbId}/similar", s.handleTMDBSimilarMovies)
	protected.HandleFunc("GET /api/tmdb/tv/{tmdbId}", s.handleTMDBTVDetail)

	protected.HandleFunc("GET /api/movies/{id}/subtitles", s.handleSearchMovieSubtitles)
	protected.HandleFunc("POST /api/movies/{id}/subtitles/download", s.handleDownloadMovieSubtitle)

	public.Handle("/api/", s.Auth.Middleware(protected))

	root := http.NewServeMux()
	root.Handle("/api/", noStore(public))
	root.Handle("/", frontendHandler())
	return root
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
