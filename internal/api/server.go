// Package api serves the REST API the web UI talks to
// ("REST API served to the web UI") and, in production, the embedded
// frontend build alongside it so the whole app is one binary/one port.
package api

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/announce"
	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/delay"
	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/ereader"
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
	"github.com/rdborg/mediarium/internal/simkl"
	"github.com/rdborg/mediarium/internal/subtitles"
	"github.com/rdborg/mediarium/internal/torrentopts"
	"github.com/rdborg/mediarium/internal/trakt"
	"github.com/rdborg/mediarium/internal/tvmaze"
	"github.com/rdborg/mediarium/internal/usage"
	"github.com/rdborg/mediarium/internal/vpn"
)

type Server struct {
	resumeStop   context.CancelFunc // ends the resume of interrupted downloads (resume_interrupted.go)
	streamPause  streamPauseState   // pause while someone is watching (stream_pause.go)
	libStats     libraryStatsCache  // the Statistics page's answer, kept for a few minutes (stats_library.go)
	displayFaces displayFaceCache   // titles and posters in the info language (display_face.go)
	mediaInfo    mediaInfoState     // what the video files hold, read from their headers (media_info.go)
	appFrames    atomic.Value       // []string: the origins of add-ons shown in a frame, kept in memory so the security headers never wait for the database (app_links.go)
	cfg          config.Config
	db           *sql.DB // for backups (VACUUM INTO); repos hold their own handles

	Auth              *auth.Service
	Settings          *settings.Store
	IndexerRepo       *indexers.Repo
	Definitions       *indexers.DefinitionStore  // cached community indexer definitions
	Cardigann         *indexers.CardigannManager // runs definition-based indexers
	ClientRepo        *download.Repo
	MovieRepo         *library.Repo
	QueueRepo         *queue.Repo
	TorrentOpts       *torrentopts.Repo // per-torrent choices, categories and seeding rules of the built-in torrent client (torrent_options.go)
	VPNRepo           *vpn.Repo
	VPNManager        *vpn.Manager
	NotifyRepo        *notify.Repo
	QualityRepo       *quality.Repo
	FormatRepo        *quality.FormatRepo
	Blocklist         *blocklist.Repo
	SubtitleAttempts  *subtitles.AttemptRepo
	SubtitleFiles     *subtitles.FileRepo
	SubtitleDismissed *subtitles.DismissRepo
	SubtitleQuota     *subtitles.QuotaRepo
	Usage             *usage.Tracker // requests and limit hits per third-party service
	delays            *delay.Store   // releases the delay profile is holding back (search_limits.go)
	debridCache       cachedMemo     // what the debrid service said about info hashes (debrid_cache.go)
	Problems          *problems.Log  // the log of real problems shown under Settings > System > Logs and errors (handlers_problems.go)
	MediaServers      *mediaservers.Repo
	AnnounceRepo      *announce.Repo    // IRC announce connections (irc_announce.go)
	Announce          *announce.Manager // runs them
	MusicRepo         *music.Repo       // artists, albums, tracks (the music module, music*.go)
	BookRepo          *books.Repo       // ebooks and audiobooks (books_api.go)
	EReader           *ereader.Repo     // e-reader password and Kobo sync address (ereader_*.go)
	ereader           ereaderRuntime    // limits and store address for the e-reader routes (ereader_auth.go)
	OpenLibrary       *books.Client
	Requests          *requests.Repo
	Audnexus          *books.Audnexus
	bookImport        bookImports    // the "import books already on disk" job (books_import.go)
	authorCheck       authorCheck    // when followed authors were last checked (books_authors.go)
	seriesCheck       authorCheck    // when followed series were last checked (books_series.go)
	hc                hardcoverState // the Hardcover client for the saved token (books_series.go)
	coverLink         *http.Client   // downloads a book cover from a pasted link; nil = the public-internet-only client (books_owncover.go)

	mediaClient    *mediaservers.Client
	mediaRefresher *mediaservers.Refresher // rescans Plex/Jellyfin/Emby after imports
	mediaFinder    *mediaservers.Finder    // "Watch in" lookups, cached briefly
	mediaSignIn    mediaSignIn             // discovery and sign-ins in progress (handlers_media_servers_signin.go)

	LoginLimiter *auth.LoginLimiter
	version      string
	security     securityState // trusted proxies, allowed origins, per-account sign-in limit (security.go)

	arrRecs      arrRecommended
	tmdbMu       sync.RWMutex
	tmdb         *metadata.Client
	folderTitles folderTitleCache // localized titles for new movie and show folders (folder_title.go)
	tvmaze       *tvmaze.Client   // episode air times (tvmaze_airtimes.go); never contacted while the switch is off

	subsMu sync.RWMutex
	subs   *subtitles.Client
	subdl  *subtitles.SubDL

	addons *subtitles.AddonClient // asks subtitle add-ons for subtitles
	wizdom *subtitles.Wizdom      // Hebrew subtitles from wizdom.xyz (subtitles_wizdom.go)

	traktMu sync.RWMutex
	trakt   *trakt.Client

	simklMu   sync.RWMutex
	simkl     *simkl.Client
	simklPins *mediaservers.Pending[simkl.SignIn] // Simkl sign-in codes handed out and not yet entered (simkl_connect.go)
	// simklRefreshMu keeps two lists from refreshing the Simkl sign-in at once
	// (Simkl may hand out a new refresh token each time).
	simklRefreshMu sync.Mutex

	proxyTestAddr string // where a proxy test connects to (tests point it elsewhere)

	trashMu sync.Mutex
	trash   *trashSource // the TRaSH Guides import, made when first used

	mbMu  sync.RWMutex
	mb    *musicbrainz.Client // one client, so its one-request-per-second limit is shared
	music musicState          // music hunt position and library scan jobs (music*.go)

	huntAt huntPosition // where the next scheduled movie and book searches start (automation.go, books_api.go)

	statsCache   statsCache
	metricsCache metricsCache // slow-to-count numbers the metrics page reuses between scrapes (handlers_metrics.go)
	// The library's totals and the TV list's per-show numbers, kept until the
	// library changes (library_overview.go).
	overviewCache   sharedCache[libraryOverview]
	seriesListCache sharedCache[seriesListData]
	ownedCache      sharedCache[ownedData]
	builtin         BuiltinKeys // app-wide keys baked into official releases (see keys.go)

	importMu         sync.Mutex
	importJobs       map[string]*importJob
	importRegisterMu sync.Mutex  // one import is confirmed at a time (handleRunImport)
	importWork       importState // the worker that fills in details after an import (import_worker.go)

	migrator *migrate.Importer // moving over from Radarr/Sonarr/Prowlarr/SABnzbd (handlers_migrate.go)
	held     heldNotices       // messages waiting for the quiet hours to end
	box      *crypto.Box       // the server key, for secrets kept outside the settings (two-step sign-in)
	twoStep  twoStepChallenges // sign-ins waiting for their app code (two_step.go)
	ssoS     ssoState          // single sign-on, directory sign-in and passkeys (signin_methods.go)

	torrents torrentRegistry // live torrent clients, so the on/off switch can stop them
	irc      ircLibrary      // the titles IRC announcements are matched against (irc_announce_decide.go)

	bookWait     time.Duration      // how long a book release search may take; 0 means bookSearchWait (books_api.go)
	clientWatch  notify.ClientWatch // when a download client goes down and comes back (client_watch.go)
	usenetSlots  slotGate           // how many built-in Usenet downloads run at once (builtin_slots.go)
	torrentSlots slotGate           // the same for built-in torrents

	grabMu sync.Mutex     // makes "is this already being downloaded?" and "queue it" one step (see grab_claim.go)
	bg     sync.WaitGroup // download pipelines and automatic retries still running (see grab_claim.go)

	unpacking    unpackTracker     // how far the running unpacks have come, for the Activity page (unpack_progress.go)
	pipelines    pipelineRegistry  // running download pipelines, so removing a title can cancel its downloads
	dispatch     *queue.Dispatcher // the download line: starts the next waiting download when a place is free (dispatch.go)
	bulk         bulkState         // whether a batch search from the Library page is running (handlers_bulk.go)
	lists        importListState   // the import lists being read (import_lists.go)
	portal       portalState       // messages to the people who asked (request_portal.go)
	arrFillState                   // reading missing TVDB ids for the Sonarr-style API (arr_series.go)
	anime        animeState        // scene and anime numbering of shows (anime_numbering.go)

	offered       offeredURLs       // download URLs recently handed out in search results (see grab_urls.go)
	flareProbe    flareSolverrProbe // last answer from the Cloudflare helper (see flaresolverr.go)
	statsEndpoint string            // where the anonymous usage numbers go; empty means usagestats.URL (tests set it)
	stats         sysinfoSampler    // CPU and memory readings for the server card (see handlers_stats.go)

	Monitor *monitor.Monitor // connectivity monitor (Usenet servers, indexers)

	restoreMu sync.Mutex // one restore upload at a time
	exitFn    func()     // what to do once a restore is staged (see SetExitFunc)
	// restartInPlace is set when the app exits to start again (RestartWanted).
	restartInPlace atomic.Bool
	restartDelay   time.Duration // zero means defaultRestartDelay

	moveJob moveJob      // moving the files of titles to another library folder (move_files.go)
	metaJob metaFilesJob // writing NFO and artwork files for the whole library (metadata_files.go)

	mcp   mcpState   // AI assistants connecting over MCP (mcp.go)
	oauth oauthState // their OAuth sign-in (mcp_oauth.go)

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
// discoverCacheSeconds is how long a browser keeps a Discover list without asking again.
const discoverCacheSeconds = 300

func New(db *sql.DB, cfg config.Config, box *crypto.Box, defaultTMDBAPIKey, version string) (*Server, error) {
	settingsStore := settings.New(db, box)
	tmdbKey := optionalKey(settingsStore, settings.KeyTMDBAPIKey)
	if tmdbKey == "" {
		tmdbKey = defaultTMDBAPIKey
	}
	subsKey := optionalKey(settingsStore, settings.KeyOpenSubtitlesAPIKey)
	traktClientID := optionalKey(settingsStore, settings.KeyTraktClientID)
	simklClientID := optionalKey(settingsStore, settings.KeySimklClientID)

	tracker, err := usage.NewTracker(db)
	if err != nil {
		return nil, err
	}

	s := &Server{
		box:               box,
		cfg:               cfg,
		db:                db,
		Auth:              auth.New(db),
		Settings:          settingsStore,
		IndexerRepo:       indexers.NewRepo(db, box),
		ClientRepo:        download.NewRepo(db, box),
		MovieRepo:         library.NewRepo(db),
		QueueRepo:         queue.NewRepo(db),
		TorrentOpts:       torrentopts.NewRepo(db),
		VPNRepo:           vpn.NewRepo(db, box),
		VPNManager:        vpn.NewManager(),
		NotifyRepo:        notify.NewRepo(db, box),
		QualityRepo:       quality.NewRepo(db),
		FormatRepo:        quality.NewFormatRepo(db),
		Blocklist:         blocklist.NewRepo(db),
		SubtitleAttempts:  subtitles.NewAttemptRepo(db),
		SubtitleFiles:     subtitles.NewFileRepo(db),
		SubtitleDismissed: subtitles.NewDismissRepo(db),
		SubtitleQuota:     subtitles.NewQuotaRepo(db),
		tmdb:              metadata.New(tmdbKey),
		subs:              subtitles.New(subsKey),
		addons:            subtitles.NewAddonClient(),
		wizdom:            subtitles.NewWizdom(),
		trakt:             trakt.New(traktClientID),
		simkl:             simkl.New(simklClientID),
		simklPins:         &mediaservers.Pending[simkl.SignIn]{TTL: simklPinTTL},
		// "A handful of attempts per IP/window" — 5 tries per 15
		// minutes blunts brute-force without punishing a genuine typo.
		LoginLimiter: auth.NewLoginLimiter(5, 15*time.Minute),
		version:      version,
		Usage:        tracker,
	}
	s.QueueRepo.SetBox(box) // download addresses carry indexer keys: kept encrypted
	s.startProblems()
	s.applyDebugLogging()
	s.Definitions = indexers.NewDefinitionStore(filepath.Join(cfg.ConfigDir, "indexer-definitions"))
	s.Definitions.CustomDir = filepath.Join(cfg.ConfigDir, "indexer-definitions-custom")
	s.Cardigann = indexers.NewCardigannManager(s.Definitions, s.flareSolverrURL)
	s.IndexerRepo.SetCardigann(s.Cardigann)
	s.setupSearchExtras()
	s.instrumentTMDB(s.tmdb)
	s.applyProxy()
	s.applyVPNRoutes()
	s.setupPush()
	s.instrumentTrakt(s.trakt)
	s.instrumentSimkl(s.simkl)
	s.Monitor = s.newMonitor()
	s.MediaServers = mediaservers.NewRepo(db, box)
	s.mediaClient = mediaservers.NewClient(version)
	s.mediaClient.SeriesIDs = func(tmdbID int) (int, string) {
		if s.MovieRepo == nil {
			return 0, ""
		}
		sr, ok, err := s.MovieRepo.GetSeriesByTMDBID(tmdbID)
		if err != nil || !ok {
			return 0, ""
		}
		return sr.TVDBID, sr.IMDBID
	}
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
	if box != nil {
		s.EReader = ereader.NewRepo(db, box)
	} else {
		s.EReader = ereader.NewRepo(db, nil)
	}
	s.OpenLibrary = books.NewClient("Mediarium/" + version + " (https://mediarium.app)")
	s.Audnexus = books.NewAudnexus(s.OpenLibrary.UserAgent)
	s.Requests = requests.NewRepo(db)
	s.mb = musicbrainz.New(version)
	s.tvmaze = tvmaze.New(version)
	if err := s.MusicRepo.SeedPresets(); err != nil {
		return nil, fmt.Errorf("init music profiles: %w", err)
	}
	s.AnnounceRepo = announce.NewRepo(db, box)
	s.Announce = announce.NewManager(s.AnnounceRepo, ircDecider{s}, slog.Default())
	s.torrents.noPortMapping = !s.torrentPortMapping()
	s.torrents.allowLocal = s.torrentAllowLocal()
	s.torrents.discover = s.vpnUDPDiscovery()
	queue.SetNewestFirst(s.newestFirstOn())
	s.refreshAppFrames()
	s.dispatch = s.newDispatcher()
	s.applyFilePerms()   // the saved file and folder modes for new library files
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
	t := &settingsSave{}
	s.stageTMDBAPIKey(t, key)
	return s.commitSettings(t)
}

// Subtitles returns the current OpenSubtitles client, safe to call
// concurrently with SetOpenSubtitlesAPIKey.
func (s *Server) Subtitles() *subtitles.Client {
	s.subsMu.RLock()
	defer s.subsMu.RUnlock()
	return s.subs
}

// SubDL returns the current SubDL client (it may have no key).
func (s *Server) SubDL() *subtitles.SubDL {
	s.subsMu.RLock()
	defer s.subsMu.RUnlock()
	if s.subdl == nil {
		return subtitles.NewSubDL("")
	}
	return s.subdl
}

// SetSubDLAPIKey saves the SubDL key ("" removes it) and applies it.
func (s *Server) SetSubDLAPIKey(key string) error {
	t := &settingsSave{}
	s.stageSubDLAPIKey(t, key)
	return s.commitSettings(t)
}

// SetOpenSubtitlesAPIKey updates both the persisted setting and the live client.
func (s *Server) SetOpenSubtitlesAPIKey(key string) error {
	t := &settingsSave{}
	s.stageOpenSubtitlesAPIKey(t, key)
	return s.commitSettings(t)
}

// Trakt returns the current Trakt client, safe to call concurrently with
// SetTraktClientID.
func (s *Server) Trakt() *trakt.Client {
	s.traktMu.RLock()
	defer s.traktMu.RUnlock()
	return s.trakt
}

// Simkl returns the current Simkl client, safe to call concurrently with a
// change of the Client ID.
func (s *Server) Simkl() *simkl.Client {
	s.simklMu.RLock()
	defer s.simklMu.RUnlock()
	return s.simkl
}

// SetTraktClientID updates both the persisted setting and the live client. An
// empty id clears the person's own client ID and puts the built-in one (if this
// release ships one) back in use.
func (s *Server) SetTraktClientID(id string) error {
	t := &settingsSave{}
	s.stageTraktClientID(t, id)
	return s.commitSettings(t)
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
	public.HandleFunc("POST /api/auth/login/code", s.handleLoginCode)
	public.HandleFunc("POST /api/auth/logout", s.handleLogout)
	// Other ways to sign in (signin_methods.go): what the sign-in page offers, single sign-on and passkeys.
	public.HandleFunc("GET /api/auth/methods", s.handleSignInMethods)
	public.HandleFunc("GET /api/auth/oidc/start", s.handleOIDCStart)
	public.HandleFunc("GET /api/auth/oidc/callback", s.handleOIDCCallback)
	public.HandleFunc("POST /api/auth/passkey/login/begin", s.handlePasskeyLoginBegin)
	public.HandleFunc("POST /api/auth/passkey/login/finish", s.handlePasskeyLoginFinish)
	// Calendar apps read the feed without signing in; the secret address is the key.
	public.HandleFunc("GET /api/calendar/feed/{file}", s.handleCalendarFeed)

	// The MCP address for AI assistants signs in with an API key of its own (mcp.go).
	routes := s.protectedRoutes()
	s.mcp.table = routes
	public.HandleFunc("/api/mcp", s.handleMCP)
	public.Handle("/api/", s.signedIn(routes))

	root := http.NewServeMux()
	root.Handle("/api/", s.busyGuard(noStore(gzipJSON(metricsBearerOnly(public)))))
	// Prometheus' default path. It is the same protected page as /api/metrics.
	root.Handle("GET /metrics", s.busyGuard(noStore(metricsAlias(public))))
	root.Handle("/mcp", s.busyGuard(noStore(http.HandlerFunc(s.handleMCP))))
	// Sign-in for MCP clients that find it themselves (mcp_oauth.go). Anything else
	// under these paths answers JSON, never the web app's page: a client looking for
	// OAuth metadata can't read HTML.
	root.Handle("/.well-known/", s.busyGuard(noStore(http.HandlerFunc(s.handleWellKnown))))
	root.Handle("/oauth/", s.busyGuard(noStore(http.HandlerFunc(s.handleOAuth))))
	root.Handle("/mcp/", s.busyGuard(noStore(http.HandlerFunc(handleMCPOther))))
	// Radarr, Sonarr and Lidarr-style endpoints for other apps (arr.go).
	s.arrRoutes(root)
	// E-readers: the OPDS catalogue and Kobo sync sign in their own way (ereader_auth.go).
	s.opdsRoutes(root)
	s.koboRoutes(root)
	root.HandleFunc("GET /robots.txt", robotsTxt)
	root.Handle("/", frontendHandlerBase(s.cfg.URLBase))
	return withBodyDeadline(s.harden(withURLBase(s.cfg.URLBase, root)))
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
	member.HandleFunc("GET /api/auth/sessions", s.handleListSessions)
	member.HandleFunc("DELETE /api/auth/sessions", s.handleEndOtherSessions)
	member.HandleFunc("GET /api/auth/api-keys", s.handleListAPIKeys)
	member.HandleFunc("POST /api/auth/api-keys", s.handleCreateAPIKey)
	member.HandleFunc("DELETE /api/auth/api-keys/{id}", s.handleRevokeAPIKey)
	member.HandleFunc("GET /api/auth/ereader", s.handleGetEReader)
	member.HandleFunc("POST /api/auth/ereader/opds", s.handleNewOPDSPassword)
	member.HandleFunc("DELETE /api/auth/ereader/opds", s.handleRemoveOPDSPassword)
	member.HandleFunc("POST /api/auth/ereader/kobo", s.handleNewKoboToken)
	member.HandleFunc("DELETE /api/auth/ereader/kobo", s.handleRemoveKoboToken)
	member.HandleFunc("POST /api/auth/ereader/kobo/resync", s.handleKoboResync)
	member.HandleFunc("GET /api/auth/passkeys", s.handleListPasskeys)
	member.HandleFunc("POST /api/auth/passkeys/register/begin", s.handlePasskeyRegisterBegin)
	member.HandleFunc("POST /api/auth/passkeys/register/finish", s.handlePasskeyRegisterFinish)
	member.HandleFunc("PUT /api/auth/passkeys/{id}", s.handleRenamePasskey)
	member.HandleFunc("DELETE /api/auth/passkeys/{id}", s.handleDeletePasskey)
	member.HandleFunc("POST /api/auth/oidc/link", s.handleOIDCLinkStart)
	member.HandleFunc("DELETE /api/auth/oidc/link", s.handleOIDCUnlink)

	// The page where an administrator lets an AI assistant connect (mcp_oauth.go).
	member.HandleFunc("GET /api/oauth/requests/{id}", s.handleOAuthRequestInfo)
	member.HandleFunc("POST /api/oauth/requests/{id}", s.handleOAuthDecision)

	// Overview (members get a trimmed view: no setup problems, no folder paths).
	member.HandleFunc("GET /api/health", s.handleHealth)
	member.HandleFunc("GET /api/dashboard", revalidated(s.handleDashboard))
	member.HandleFunc("GET /api/calendar", revalidated(s.handleCalendar))
	member.HandleFunc("GET /api/stats/library", s.handleLibraryStats)
	member.HandleFunc("GET /api/stats/history", revalidated(s.handleStatsHistory))
	member.HandleFunc("GET /api/exclusions", s.handleListExclusions)
	member.HandleFunc("POST /api/exclusions", s.handleAddExclusion)
	member.HandleFunc("DELETE /api/exclusions/{kind}/{tmdbId}", s.handleRemoveExclusion)
	member.HandleFunc("GET /api/calendar/feed", s.handleGetCalendarFeed)
	member.HandleFunc("POST /api/calendar/feed", s.handleNewCalendarFeed)
	member.HandleFunc("POST /api/feedback", s.handleSendFeedback)
	member.HandleFunc("DELETE /api/calendar/feed", s.handleRemoveCalendarFeed)
	member.HandleFunc("GET /api/wanted", revalidated(s.handleWanted))
	member.HandleFunc("GET /api/activity", revalidated(s.handleListActivity))
	member.HandleFunc("GET /api/modules", s.handleModules)
	member.HandleFunc("GET /api/watched", s.handleWatched)
	member.HandleFunc("GET /api/requests", s.handleListRequests)
	member.HandleFunc("DELETE /api/requests/{id}", s.handleDeleteRequest)
	member.HandleFunc("GET /api/requests/status", s.handleRequestStatus)
	member.HandleFunc("GET /api/issues", s.handleListIssues)
	member.HandleFunc("POST /api/issues", s.handleCreateIssue)
	member.HandleFunc("GET /api/auth/request-notices", s.handleGetRequestNotices)
	member.HandleFunc("PUT /api/auth/request-notices", s.handlePutRequestNotices)
	member.HandleFunc("POST /api/auth/request-notices/devices", s.handleAddNoticeDevice)
	member.HandleFunc("DELETE /api/auth/request-notices/devices/{id}", s.handleRemoveNoticeDevice)

	// Finding things.
	member.HandleFunc("GET /api/discover/trending", revalidatedFor(discoverCacheSeconds, s.handleTrending))
	member.HandleFunc("GET /api/discover/popular", revalidatedFor(discoverCacheSeconds, s.handlePopular))
	member.HandleFunc("GET /api/discover/tv/trending", revalidatedFor(discoverCacheSeconds, s.handleTrendingTV))
	member.HandleFunc("GET /api/discover/tv/popular", revalidatedFor(discoverCacheSeconds, s.handlePopularTV))
	member.HandleFunc("GET /api/discover/search", s.handleTitleSearch)
	member.HandleFunc("GET /api/discover/genres", s.handleDiscoverGenres)
	member.HandleFunc("GET /api/discover/browse", revalidatedFor(discoverCacheSeconds, s.handleDiscoverBrowse))
	member.HandleFunc("GET /api/discover/list", revalidatedFor(discoverCacheSeconds, s.handleDiscoverList))
	member.HandleFunc("GET /api/discover/import-list", s.handleImportList)
	member.HandleFunc("GET /api/discover/for-you", revalidated(s.handleForYou))
	member.HandleFunc("GET /api/tv/search", s.handleSearchTV)
	member.HandleFunc("GET /api/tmdb/search", s.handleTMDBSearch)
	member.HandleFunc("GET /api/tmdb/movies/{tmdbId}", s.handleTMDBMovieDetail)
	member.HandleFunc("GET /api/tmdb/movies/{tmdbId}/similar", s.handleTMDBSimilarMovies)
	member.HandleFunc("GET /api/tmdb/tv/{tmdbId}", s.handleTMDBTVDetail)
	member.HandleFunc("GET /api/tmdb/people/{personId}", s.handleTMDBPerson)
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
	manage.HandleFunc("POST /api/movies/{id}/replace-smaller", s.handleMovieReplaceSmaller)
	member.HandleFunc("GET /api/tags", s.handleListTags)
	manage.HandleFunc("PUT /api/movies/{id}/tags", s.handleSetMovieTags)
	manage.HandleFunc("PUT /api/series/{id}/tags", s.handleSetSeriesTags)
	manage.HandleFunc("PUT /api/movies/{id}/monitored", s.handleSetMovieMonitored)
	manage.HandleFunc("PUT /api/movies/{id}/folder", s.handleSetMovieFolder)
	manage.HandleFunc("PUT /api/series/{id}/folder", s.handleSetSeriesFolder)
	member.HandleFunc("GET /api/library/folders", s.handleLibraryFolders)
	member.HandleFunc("GET /api/library/counts", s.handleLibraryCounts)
	member.HandleFunc("GET /api/library/owned", revalidated(s.handleLibraryOwned))
	member.HandleFunc("GET /api/library/file-details", s.handleMediaInfoStatus)

	// Shows: the same.
	member.HandleFunc("GET /api/series", revalidated(s.handleListSeries))
	member.Need(auth.PermTV).HandleFunc("POST /api/series", s.handleAddSeries)
	member.HandleFunc("GET /api/series/{id}", s.handleGetSeries)
	member.HandleFunc("POST /api/series/{id}/refresh", s.handleRefreshSeries)
	releases.HandleFunc("GET /api/series/{id}/search", s.handleSeriesSearch)
	manage.HandleFunc("POST /api/series/{id}/search-now", s.handleSeriesSearchNow)
	releases.HandleFunc("POST /api/series/{id}/grab", s.handleGrabSeries)
	manage.HandleFunc("PUT /api/series/{id}/monitored", s.handleSetSeriesMonitored)
	manage.HandleFunc("POST /api/series/{id}/monitor-mode", s.handleSetSeriesMonitorMode)
	manage.HandleFunc("PUT /api/series/{id}/type", s.handleSetSeriesType)
	member.HandleFunc("GET /api/series/{id}/numbering", s.handleSeriesNumbering)
	member.HandleFunc("GET /api/series/{id}/numbering/custom", s.handleGetCustomNumbering)
	manage.HandleFunc("PUT /api/series/{id}/numbering/custom", s.handlePutCustomNumbering)
	manage.HandleFunc("POST /api/series/{id}/numbering/refresh", s.handleRefreshSeriesNumbering)
	manage.HandleFunc("PUT /api/series/{id}/seasons/{season}/monitored", s.handleSetSeasonMonitored)
	manage.HandleFunc("PUT /api/episodes/{id}/monitored", s.handleSetEpisodeMonitored)

	// Books: ebooks and audiobooks (404 while both are off).
	member.HandleFunc("GET /api/books", revalidated(s.handleListBooks))
	member.Need(auth.PermBooks).HandleFunc("POST /api/books", s.handleAddBook)
	member.HandleFunc("GET /api/books/search", s.handleBookSearch)
	member.HandleFunc("GET /api/book-covers/{file}", s.handleBookCover)
	member.HandleFunc("GET /api/books/discover", s.handleBookDiscover)
	member.HandleFunc("GET /api/books/subjects", s.handleBookSubjects)
	member.HandleFunc("GET /api/books/{id}/links", s.handleBookLinks)
	member.HandleFunc("GET /api/books/progress", s.handleListBookProgress)
	member.HandleFunc("GET /api/book-authors", s.handleFollowedAuthors)
	member.HandleFunc("GET /api/book-works/{key}", s.handleBookWork)
	member.HandleFunc("GET /api/book-authors/{key}/works", s.handleAuthorWorks)
	// Following adds books by itself, so it needs "add titles themselves".
	member.Need(auth.PermBooks, auth.PermManage, auth.PermAddDirect).HandleFunc("PUT /api/book-authors/{key}/follow", s.handleFollowAuthor)
	manage.HandleFunc("DELETE /api/book-authors/{key}/follow", s.handleUnfollowAuthor)
	member.HandleFunc("GET /api/book-works/{key}/series", s.handleBookSeries)
	member.HandleFunc("GET /api/book-series", s.handleFollowedSeries)
	member.Need(auth.PermBooks, auth.PermManage, auth.PermAddDirect).HandleFunc("PUT /api/book-series/{source}/{key}/follow", s.handleFollowSeries)
	manage.HandleFunc("DELETE /api/book-series/{source}/{key}/follow", s.handleUnfollowSeries)
	member.HandleFunc("GET /api/books/{id}/progress", s.handleGetBookProgress)
	member.HandleFunc("PUT /api/books/{id}/progress", s.handleSetBookProgress)
	play.HandleFunc("GET /api/books/{id}/read", s.handleReadBook)
	play.HandleFunc("GET /api/books/{id}/comic", s.handleComic)
	play.HandleFunc("GET /api/books/{id}/comic/{n}", s.handleComicPage)
	member.HandleFunc("GET /api/books/{id}/file-cover", s.handleBookFileCover)
	member.HandleFunc("GET /api/books/{id}/own-cover", s.handleBookOwnCover)
	manage.HandleFunc("POST /api/books/{id}/cover", s.handleUploadBookCover)
	manage.HandleFunc("POST /api/books/{id}/cover/link", s.handleBookCoverLink)
	manage.HandleFunc("GET /api/books/{id}/cover/editions", s.handleBookCoverEditions)
	manage.HandleFunc("PUT /api/books/{id}/cover/edition", s.handlePickBookCover)
	manage.HandleFunc("DELETE /api/books/{id}/cover", s.handleRemoveBookCover)
	play.HandleFunc("GET /api/books/{id}/tracks", s.handleBookTracks)
	play.HandleFunc("GET /api/books/{id}/listen/{n}", s.handleListenBook)
	admin.HandleFunc("GET /api/books/import", s.handleBookImportStatus)
	admin.HandleFunc("POST /api/books/import", s.handleStartBookImport)
	admin.HandleFunc("POST /api/books/import/retry", s.handleRetryBookImport)
	admin.HandleFunc("POST /api/books/import/match", s.handleMatchBookImport)
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
	member.HandleFunc("GET /api/music/discover", revalidated(s.handleMusicDiscover))
	member.HandleFunc("GET /api/music/discover/artists", revalidated(s.handleMusicDiscoverArtists))
	member.HandleFunc("GET /api/music/preview/{mbid}", s.handleMusicPreview)
	member.HandleFunc("GET /api/music/covers/release-group/{mbid}", s.handleReleaseGroupCover)
	member.HandleFunc("GET /api/music/artists", revalidated(s.handleListArtists))
	member.Need(auth.PermMusic).HandleFunc("POST /api/music/artists", s.handleAddArtist)
	member.HandleFunc("GET /api/music/artists/{id}", s.handleGetArtist)
	member.HandleFunc("GET /api/music/artists/{id}/discography", s.handleArtistDiscography)
	member.HandleFunc("GET /api/music/filters", s.handleGetMusicFilters)
	manage.HandleFunc("POST /api/music/artists/{id}/albums", s.handleAddArtistAlbum)
	member.HandleFunc("GET /api/music/albums/{id}", s.handleGetAlbum)
	member.HandleFunc("GET /api/music/albums/{id}/files", s.handleAlbumFiles)
	member.HandleFunc("GET /api/music/albums/{id}/cover", s.handleAlbumCover)
	play.HandleFunc("GET /api/music/tracks/{id}/play", s.handlePlayTrack)
	member.HandleFunc("GET /api/music/artists/{id}/cover", s.handleArtistCover)
	manage.HandleFunc("PUT /api/music/albums/{id}/monitored", s.handleSetAlbumMonitored)
	releases.HandleFunc("POST /api/music/albums/{id}/search", s.handleAlbumSearch)
	manage.HandleFunc("POST /api/music/albums/{id}/search-now", s.handleAlbumSearchNow)
	releases.HandleFunc("POST /api/music/albums/{id}/grab", s.handleAlbumGrab)
	member.HandleFunc("GET /api/music/albums/{id}/events", s.handleAlbumEvents)
	member.HandleFunc("GET /api/music/wanted", revalidated(s.handleMusicWanted))
	s.musicPlayerRoutes(member, play, admin) // playlists, lyrics, ReplayGain, scrobbling (music_player_routes.go)

	// Files on disk, and playing them in the browser.
	member.HandleFunc("GET /api/movies/{id}/files", s.handleMovieFiles)
	member.HandleFunc("GET /api/movies/{id}/copies", s.handleListMovieCopies)
	member.HandleFunc("GET /api/series/{id}/copies", s.handleListSeriesCopies)
	member.HandleFunc("GET /api/series/{id}/files", s.handleSeriesFiles)
	member.HandleFunc("GET /api/movies/{id}/events", s.handleMovieEvents)
	member.HandleFunc("GET /api/series/{id}/events", s.handleSeriesEvents)
	play.HandleFunc("GET /api/files/stream", s.handleStreamFile)

	// "Watch in Plex/Jellyfin/Emby" and "Open my media server" links.
	member.HandleFunc("GET /api/media-servers/links", s.handleMediaServerLinks)
	member.HandleFunc("GET /api/apps", s.handleListAppLinks)

	// Downloads: watch and retry.
	member.HandleFunc("GET /api/queue", revalidated(s.handleListQueue))
	member.HandleFunc("GET /api/queue/count", s.handleQueueCount)
	member.HandleFunc("GET /api/plex-watchlist", s.handleWatchlistStatus)
	member.HandleFunc("PUT /api/plex-watchlist", s.handleUpdateWatchlist)
	member.HandleFunc("DELETE /api/plex-watchlist", s.handleDisconnectWatchlist)
	member.HandleFunc("POST /api/plex-watchlist/pin", s.handleStartWatchlistPin)
	member.HandleFunc("GET /api/plex-watchlist/pin/{pinId}", s.handlePollWatchlistPin)
	member.HandleFunc("POST /api/plex-watchlist/check", s.handleCheckWatchlist)
	member.HandleFunc("GET /api/plex-watchlist/items", s.handleWatchlistItems)
	member.Need(auth.PermRetry).HandleFunc("POST /api/queue/{id}/retry", s.handleRetryQueueItem)

	// Subtitles for titles in the library.
	member.HandleFunc("GET /api/subtitles/wanted", s.handleSubtitlesWanted)
	admin.HandleFunc("GET /api/subtitles/addons", s.handleListSubtitleAddons)
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
	admin.HandleFunc("POST /api/subtitles/addons", s.handleAddSubtitleAddon)
	admin.HandleFunc("DELETE /api/subtitles/addons/{id}", s.handleDeleteSubtitleAddon)
	admin.HandleFunc("POST /api/subtitles/addons/{id}/test", s.handleTestSubtitleAddon)
	admin.HandleFunc("GET /api/settings/folder-check", s.handleFolderCheck)
	admin.HandleFunc("POST /api/settings/folder-create", s.handleCreateFolder)
	admin.HandleFunc("POST /api/settings/library-marker", s.handleCreateRootMarker)
	admin.HandleFunc("GET /api/settings/naming-preview", s.handleNamingPreview)
	admin.HandleFunc("GET /api/settings/naming-tokens", s.handleNamingTokens)

	// Indexers and Usenet servers.
	admin.HandleFunc("GET /api/indexers", s.handleListIndexers)
	admin.HandleFunc("POST /api/indexers", s.handleCreateIndexer)
	admin.HandleFunc("PUT /api/indexers/{id}", s.handleUpdateIndexer)
	admin.HandleFunc("DELETE /api/indexers/{id}", s.handleDeleteIndexer)
	admin.HandleFunc("GET /api/indexer-definitions", s.handleListIndexerDefinitions)
	admin.HandleFunc("GET /api/debrid", s.handleGetDebrid)
	admin.HandleFunc("GET /api/debrid/torrents", s.handleDebridTorrents)
	admin.HandleFunc("POST /api/debrid/torrents/{id}/remove", s.handleDebridRemove)
	admin.HandleFunc("PUT /api/debrid", s.handlePutDebrid)
	admin.HandleFunc("POST /api/debrid/test", s.handleTestDebrid)
	admin.HandleFunc("GET /api/download-client", s.handleGetDownloadClient)
	admin.HandleFunc("GET /api/download-client/torrents", s.handleClientTorrents)
	admin.HandleFunc("POST /api/download-client/torrents/{hash}/{action}", s.handleClientTorrentAction)
	admin.HandleFunc("PUT /api/download-client", s.handlePutDownloadClient)
	admin.HandleFunc("POST /api/download-client/test", s.handleTestDownloadClient)
	admin.HandleFunc("GET /api/usenet-client", s.handleGetUsenetClient)
	admin.HandleFunc("PUT /api/usenet-client", s.handlePutUsenetClient)
	admin.HandleFunc("POST /api/usenet-client/test", s.handleTestUsenetClient)
	admin.HandleFunc("GET /api/usenet-client/jobs", s.handleUsenetJobs)
	admin.HandleFunc("POST /api/usenet-client/jobs/{id}/{action}", s.handleUsenetJobAction)
	admin.HandleFunc("POST /api/indexer-definitions/custom", s.handleAddCustomDefinition)
	admin.HandleFunc("DELETE /api/indexer-definitions/custom/{id}", s.handleRemoveCustomDefinition)
	admin.HandleFunc("POST /api/indexers/test", s.handleTestIndexerConfig)
	admin.HandleFunc("POST /api/indexers/{id}/test", s.handleTestIndexer)
	admin.HandleFunc("GET /api/indexers/{id}/categories", s.handleIndexerCategories)
	admin.HandleFunc("PUT /api/indexers/{id}/enabled", s.handleSetIndexerEnabled)
	admin.HandleFunc("PUT /api/indexers/{id}/priority", s.handleSetIndexerPriority)
	admin.HandleFunc("GET /api/usenet-servers", s.handleListUsenetServers)
	admin.HandleFunc("POST /api/usenet-servers", s.handleCreateUsenetServer)
	admin.HandleFunc("PUT /api/usenet-servers/{id}", s.handleUpdateUsenetServer)
	admin.HandleFunc("DELETE /api/usenet-servers/{id}", s.handleDeleteUsenetServer)
	admin.HandleFunc("POST /api/usenet-servers/test", s.handleTestUsenetServerConfig)
	admin.HandleFunc("POST /api/usenet-servers/{id}/test", s.handleTestUsenetServer)
	admin.HandleFunc("POST /api/usenet-servers/{id}/probe", s.handleProbeUsenetServer)
	admin.HandleFunc("GET /api/downloads/status", s.handleDownloadsStatus)

	// IRC announce: grab releases the moment a tracker announces them.
	admin.HandleFunc("GET /api/irc-announce", s.handleListIRC)
	admin.HandleFunc("POST /api/irc-announce", s.handleCreateIRC)
	admin.HandleFunc("POST /api/irc-announce/test", s.handleTestIRC)
	admin.HandleFunc("PUT /api/irc-announce/{id}", s.handleUpdateIRC)
	admin.HandleFunc("DELETE /api/irc-announce/{id}", s.handleDeleteIRC)
	admin.HandleFunc("PUT /api/irc-announce/{id}/enabled", s.handleSetIRCEnabled)
	admin.HandleFunc("GET /api/irc-announce/{id}/log", s.handleIRCLog)

	// Library management: removing titles, per-title profile and downloaders, imports.
	admin.HandleFunc("POST /api/movies/{id}/rescan", s.handleRescanMovie)
	admin.HandleFunc("POST /api/series/{id}/rescan", s.handleRescanSeries)
	admin.HandleFunc("GET /api/library/browse", s.handleBrowse)
	admin.HandleFunc("POST /api/movies/{id}/locate-file", s.handleLocateMovieFile)
	admin.HandleFunc("POST /api/movies/{id}/locate-folder", s.handleLocateMovieFolder)
	admin.HandleFunc("POST /api/episodes/{id}/locate-file", s.handleLocateEpisodeFile)
	admin.HandleFunc("POST /api/series/{id}/change-folder", s.handleChangeSeriesFolder)
	admin.HandleFunc("DELETE /api/movies/{id}/file", s.handleDeleteMovieFile)
	admin.HandleFunc("POST /api/library/incomplete/redo", s.handleRedoIncomplete)
	admin.HandleFunc("DELETE /api/episodes/{id}/file", s.handleDeleteEpisodeFile)
	admin.HandleFunc("DELETE /api/series/{id}/seasons/{season}/files", s.handleDeleteSeasonFiles)
	admin.HandleFunc("DELETE /api/movies/{id}", s.handleDeleteMovie)
	admin.HandleFunc("POST /api/movies/{id}/copies", s.handleAddMovieCopy)
	admin.HandleFunc("POST /api/series/{id}/copies", s.handleAddSeriesCopy)
	admin.HandleFunc("GET /api/movies/{id}/disk-usage", s.handleMovieDiskUsage)
	admin.HandleFunc("PUT /api/movies/{id}/sources", s.handleSetMovieSources)
	admin.HandleFunc("PUT /api/movies/{id}/profile", s.handleSetMovieProfile)
	admin.HandleFunc("PUT /api/movies/{id}/no-upgrade", s.handleSetMovieNoUpgrade)
	admin.HandleFunc("DELETE /api/series/{id}", s.handleDeleteSeries)
	admin.HandleFunc("GET /api/series/{id}/disk-usage", s.handleSeriesDiskUsage)
	admin.HandleFunc("PUT /api/series/{id}/sources", s.handleSetSeriesSources)
	admin.HandleFunc("PUT /api/series/{id}/profile", s.handleSetSeriesProfile)
	admin.HandleFunc("PUT /api/series/{id}/no-upgrade", s.handleSetSeriesNoUpgrade)
	admin.HandleFunc("PUT /api/series/{id}/season-pack", s.handleSetSeriesSeasonPack)
	admin.HandleFunc("PUT /api/library/bulk/monitored", s.handleBulkMonitored)
	admin.HandleFunc("PUT /api/library/bulk/no-upgrade", s.handleBulkNoUpgrade)
	admin.HandleFunc("PUT /api/library/bulk/profile", s.handleBulkProfile)
	admin.HandleFunc("PUT /api/library/bulk/tags", s.handleBulkTags)
	admin.HandleFunc("PUT /api/library/bulk/sources", s.handleBulkSources)
	admin.HandleFunc("POST /api/library/bulk/search-now", s.handleBulkSearchNow)
	admin.HandleFunc("POST /api/library/bulk/replace-smaller", s.handleBulkReplaceSmaller)
	admin.HandleFunc("POST /api/library/bulk/remove", s.handleBulkRemove)
	admin.HandleFunc("POST /api/library/scan", s.handleScanLibrary)
	admin.HandleFunc("GET /api/library/scan/{id}", s.handleGetImportJob)
	admin.HandleFunc("POST /api/library/import", s.handleRunImport)
	admin.HandleFunc("GET /api/library/import/active", s.handleActiveImports)
	admin.HandleFunc("GET /api/library/import/batches/{id}", s.handleGetImportBatch)
	admin.HandleFunc("POST /api/library/import/batches/{id}/dismiss", s.handleDismissImportBatch)
	admin.HandleFunc("POST /api/library/import/batches/{id}/watch", s.handleWatchImportBatch)
	admin.HandleFunc("PUT /api/music/artists/{id}", s.handleUpdateArtist)
	admin.HandleFunc("PUT /api/music/filters", s.handleSetMusicFilters)
	admin.HandleFunc("PUT /api/music/bulk/follow", s.handleBulkArtistFollow)
	admin.HandleFunc("PUT /api/music/bulk/profile", s.handleBulkArtistProfile)
	admin.HandleFunc("POST /api/music/bulk/remove", s.handleBulkArtistRemove)
	admin.HandleFunc("DELETE /api/music/artists/{id}", s.handleDeleteArtist)
	admin.HandleFunc("GET /api/music/artists/{id}/disk-usage", s.handleArtistDiskUsage)
	admin.HandleFunc("PUT /api/modules", s.handlePutModules)
	admin.HandleFunc("POST /api/music/profiles", s.handleCreateMusicProfile)
	admin.HandleFunc("PUT /api/music/profiles/{id}", s.handleUpdateMusicProfile)
	admin.HandleFunc("DELETE /api/music/profiles/{id}", s.handleDeleteMusicProfile)
	admin.HandleFunc("POST /api/music/albums/{id}/rescan", s.handleRescanAlbum)
	admin.HandleFunc("DELETE /api/music/albums/{id}/files", s.handleDeleteAlbumFile)
	admin.HandleFunc("POST /api/music/import/scan", s.handleMusicImportScan)
	admin.HandleFunc("GET /api/music/import/scan", s.handleMusicImportLatest)
	admin.HandleFunc("GET /api/music/import/scan/{id}", s.handleMusicImportResults)
	admin.HandleFunc("DELETE /api/music/import/scan/{id}", s.handleMusicImportCancel)

	// Moving over from Radarr, Sonarr, Prowlarr and SABnzbd (read-only on their side).
	admin.HandleFunc("GET /api/system/backups", s.handleListSavedBackups)
	admin.HandleFunc("POST /api/system/backups", s.handleSaveBackupNow)
	admin.HandleFunc("GET /api/system/backups/{name}", s.handleDownloadSavedBackup)
	admin.HandleFunc("GET /api/system/backups/cloud", s.handleGetCloudBackup)
	admin.HandleFunc("PUT /api/system/backups/cloud", s.handlePutCloudBackup)
	admin.HandleFunc("POST /api/system/backups/cloud/test", s.handleTestCloudBackup)
	admin.HandleFunc("POST /api/system/backups/cloud/upload", s.handleUploadCloudBackup)
	admin.HandleFunc("GET /api/system/backups/cloud/list", s.handleListCloudBackups)
	admin.HandleFunc("GET /api/rename", s.handleRenamePreview)
	admin.HandleFunc("POST /api/rename", s.handleRename)
	admin.HandleFunc("POST /api/downloads/manual", s.handleManualDownload)
	admin.HandleFunc("GET /api/manual-import", s.handleManualImportList)
	admin.HandleFunc("POST /api/manual-import", s.handleManualImport)
	admin.HandleFunc("GET /api/manual-import/folders", s.handleManualImportFolders)
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

	// Custom formats and the TRaSH Guides import.
	member.HandleFunc("GET /api/custom-formats", s.handleListCustomFormats)
	member.HandleFunc("POST /api/custom-formats/test", s.handleTestCustomFormats)
	admin.HandleFunc("POST /api/custom-formats", s.handleImportCustomFormats)
	admin.HandleFunc("PUT /api/custom-formats/{id}", s.handleRenameCustomFormat)
	admin.HandleFunc("DELETE /api/custom-formats/{id}", s.handleDeleteCustomFormat)
	admin.HandleFunc("POST /api/custom-formats/assign", s.handleAssignCustomFormats)
	admin.HandleFunc("POST /api/custom-formats/default-scores", s.handleTrashDefaultScores)
	admin.HandleFunc("GET /api/custom-formats/trash-update", s.handleTrashUpdatePreview)
	admin.HandleFunc("POST /api/custom-formats/trash-update", s.handleTrashUpdate)
	admin.HandleFunc("GET /api/custom-formats/trash-update/status", s.handleTrashUpdateStatus)
	admin.HandleFunc("GET /api/trash-guides/{app}", s.handleTrashList)
	admin.HandleFunc("POST /api/trash-guides/{app}/formats", s.handleTrashImportFormats)
	admin.HandleFunc("POST /api/trash-guides/{app}/profiles", s.handleTrashImportProfile)

	// Queue housekeeping and the blocklist.
	admin.HandleFunc("DELETE /api/queue/{id}", s.handleDeleteQueueItem)
	admin.HandleFunc("DELETE /api/queue", s.handleClearFinishedQueue)
	admin.HandleFunc("POST /api/queue/{id}/blocklist", s.handleBlocklistQueueItem)
	admin.HandleFunc("POST /api/queue/{id}/resolve-conflict", s.handleResolveQueueConflict)
	admin.HandleFunc("POST /api/queue/{id}/pause", s.handlePauseQueueItem)
	admin.HandleFunc("POST /api/queue/{id}/resume", s.handleResumeQueueItem)
	admin.HandleFunc("POST /api/queue/{id}/stop", s.handleStopQueueItem)
	admin.HandleFunc("POST /api/queue/{id}/front", s.handleFrontQueueItem)
	admin.HandleFunc("POST /api/queue/bulk", s.handleBulkQueue)
	admin.HandleFunc("GET /api/queue/{id}/torrent", s.handleGetTorrentDetail)
	admin.HandleFunc("PUT /api/queue/{id}/torrent", s.handlePutTorrentDetail)
	admin.HandleFunc("PUT /api/queue/{id}/torrent/files", s.handlePutTorrentFiles)
	admin.HandleFunc("GET /api/downloads/torrent-options", s.handleGetTorrentOptions)
	admin.HandleFunc("PUT /api/downloads/torrent-options", s.handlePutTorrentOptions)
	admin.HandleFunc("GET /api/indexers/seed-rules", s.handleListSeedRules)
	admin.HandleFunc("PUT /api/indexers/{id}/seed-rules", s.handlePutSeedRule)
	admin.HandleFunc("GET /api/downloads/stream-pause", s.handleGetStreamPause)
	admin.HandleFunc("PUT /api/downloads/stream-pause", s.handlePutStreamPause)
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
	admin.HandleFunc("POST /api/vpn/test", s.handleVPNTest)
	admin.HandleFunc("GET /api/proxy", s.handleGetProxy)
	admin.HandleFunc("PUT /api/proxy", s.handlePutProxy)
	admin.HandleFunc("POST /api/proxy/test", s.handleTestProxy)
	admin.HandleFunc("GET /api/vpn/egress-ip", s.handleVPNEgressIP)

	// Notifications.
	admin.HandleFunc("GET /api/notifications", s.handleListNotifyTargets)
	admin.HandleFunc("POST /api/notifications", s.handleCreateNotifyTarget)
	admin.HandleFunc("PUT /api/notifications/{id}", s.handleUpdateNotifyTarget)
	admin.HandleFunc("DELETE /api/notifications/{id}", s.handleDeleteNotifyTarget)
	admin.HandleFunc("GET /api/notifications/types", s.handleNotifyTypes)
	admin.HandleFunc("GET /api/notifications/events", s.handleNotifyEvents)
	admin.HandleFunc("POST /api/notifications/test", s.handleTestNotifyTarget)
	admin.HandleFunc("POST /api/notifications/digest", s.handleSendDigest)
	admin.HandleFunc("GET /api/automation/quiet", s.handleGetQuiet)
	admin.HandleFunc("PUT /api/apps", s.handlePutAppLinks)
	admin.HandleFunc("PUT /api/automation/quiet", s.handlePutQuiet)
	admin.HandleFunc("GET /api/notifications/push-key", s.handlePushKey)

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
	admin.HandleFunc("POST /api/stats/reset", s.handleResetStats)
	admin.HandleFunc("GET /api/system/diagnostics", s.handleDiagnostics)
	admin.HandleFunc("GET /api/usage-stats", s.handleGetStats)
	admin.HandleFunc("PUT /api/usage-stats", s.handlePutStats)
	admin.HandleFunc("POST /api/usage-stats/reset-id", s.handleResetStatsID)
	admin.HandleFunc("GET /api/system/problems", s.handleProblems)
	admin.HandleFunc("POST /api/system/problems/read", s.handleProblemsRead)
	admin.HandleFunc("GET /api/system/problems/export", s.handleProblemsExport)
	admin.HandleFunc("PUT /api/system/problems/notify", s.handleProblemsNotify)
	admin.HandleFunc("PUT /api/system/problems/debug", s.handleDebugLogging)
	admin.HandleFunc("GET /api/system/update", s.handleUpdateState)
	admin.HandleFunc("POST /api/system/update", s.handlePushUpdate)
	admin.HandleFunc("DELETE /api/system/update", s.handleRemoveUpdate)
	admin.HandleFunc("GET /api/system/update/latest", s.handleUpdateLatest)
	admin.HandleFunc("POST /api/system/update/check", s.handleUpdateCheck)
	admin.HandleFunc("GET /api/system/update/install", s.handleInstallStatus)
	admin.HandleFunc("POST /api/system/update/install", s.handleInstallUpdate)
	admin.HandleFunc("GET /api/system/options", s.handleGetOptions)
	admin.HandleFunc("PUT /api/system/options", s.handlePutOptions)
	admin.HandleFunc("GET /api/system/mcp", s.handleGetMCP)
	admin.HandleFunc("PUT /api/system/mcp", s.handlePutMCP)
	admin.HandleFunc("GET /api/system/mcp/apps", s.handleListMCPApps)
	admin.HandleFunc("DELETE /api/system/mcp/apps/{id}", s.handleRevokeMCPApp)
	admin.HandleFunc("POST /api/system/mcp/clients", s.handleCreateMCPClient)
	admin.HandleFunc("DELETE /api/system/mcp/clients/{id}", s.handleDeleteMCPClient)
	admin.HandleFunc("GET /api/auth/sign-in", s.handleGetSignIn)
	admin.HandleFunc("PUT /api/auth/sign-in", s.handlePutSignIn)
	admin.HandleFunc("GET /api/auth/oidc", s.handleGetOIDC)
	admin.HandleFunc("PUT /api/auth/oidc", s.handlePutOIDC)
	admin.HandleFunc("POST /api/auth/oidc/test", s.handleTestOIDC)
	admin.HandleFunc("GET /api/auth/ldap", s.handleGetLDAP)
	admin.HandleFunc("PUT /api/auth/ldap", s.handlePutLDAP)
	admin.HandleFunc("POST /api/auth/ldap/test", s.handleTestLDAP)
	admin.HandleFunc("GET /api/auth/proxy-signin", s.handleGetProxySignIn)
	admin.HandleFunc("PUT /api/auth/proxy-signin", s.handlePutProxySignIn)
	s.registerIdleSignOut(admin) // GET and PUT /api/auth/idle-signout
	admin.HandleFunc("DELETE /api/users/{id}/two-step", s.handleUserTwoStepOff)
	member.HandleFunc("GET /api/auth/two-step", s.handleTwoStepStatus)
	member.HandleFunc("POST /api/auth/two-step/setup", s.handleTwoStepSetup)
	member.HandleFunc("POST /api/auth/two-step/enable", s.handleTwoStepEnable)
	member.HandleFunc("POST /api/auth/two-step/disable", s.handleTwoStepDisable)
	member.HandleFunc("POST /api/auth/two-step/recovery", s.handleTwoStepRecovery)
	admin.HandleFunc("GET /api/scripts", s.handleGetScripts)
	admin.HandleFunc("PUT /api/scripts", s.handlePutScripts)
	admin.HandleFunc("POST /api/scripts/test", s.handleTestScript)
	admin.HandleFunc("POST /api/requests/{id}/approve", s.handleApproveRequest)
	admin.HandleFunc("POST /api/requests/{id}/decline", s.handleDeclineRequest)
	admin.HandleFunc("GET /api/requests/settings", s.handleGetRequestSettings)
	admin.HandleFunc("PUT /api/requests/settings", s.handlePutRequestSettings)
	admin.HandleFunc("POST /api/issues/{id}/resolve", s.handleResolveIssue)
	admin.HandleFunc("POST /api/issues/{id}/reopen", s.handleReopenIssue)
	admin.HandleFunc("DELETE /api/issues/{id}", s.handleDeleteIssue)
	admin.HandleFunc("GET /api/import-lists", s.handleListImportLists)
	admin.HandleFunc("POST /api/import-lists", s.handleCreateImportList)
	admin.HandleFunc("POST /api/import-lists/preview", s.handlePreviewImportList)
	admin.HandleFunc("PUT /api/import-lists/{id}", s.handleUpdateImportList)
	admin.HandleFunc("DELETE /api/import-lists/{id}", s.handleDeleteImportList)
	admin.HandleFunc("PUT /api/import-lists/{id}/enabled", s.handleSetImportListEnabled)
	admin.HandleFunc("POST /api/import-lists/{id}/run", s.handleRunImportList)
	admin.HandleFunc("POST /api/import-lists/{id}/forget", s.handleForgetImportList)
	admin.HandleFunc("POST /api/import-lists/plex/{userId}/run", s.handleRunPlexWatchlist)
	admin.HandleFunc("GET /api/watched/settings", s.handleGetWatchedSettings)
	admin.HandleFunc("PUT /api/watched/settings", s.handlePutWatchedSettings)
	admin.HandleFunc("POST /api/watched/sync", s.handleSyncWatched)
	admin.HandleFunc("GET /api/watched/cleanup/preview", s.handleLibraryCleanupPreview)
	admin.HandleFunc("POST /api/watched/cleanup/run", s.handleLibraryCleanupRun)
	admin.HandleFunc("GET /api/settings/hardcover", s.handleGetHardcover)
	admin.HandleFunc("PUT /api/settings/hardcover", s.handlePutHardcover)
	admin.HandleFunc("GET /api/settings/simkl", s.handleGetSimkl)
	admin.HandleFunc("DELETE /api/settings/simkl", s.handleDisconnectSimkl)
	admin.HandleFunc("POST /api/settings/simkl/test", s.handleTestSimkl)
	admin.HandleFunc("POST /api/settings/simkl/pin", s.handleStartSimklPin)
	admin.HandleFunc("GET /api/settings/simkl/pin/{code}", s.handlePollSimklPin)
	admin.HandleFunc("POST /api/system/restart", s.handleRestart)
	admin.HandleFunc("POST /api/system/shutdown", s.handleShutdown)
	admin.HandleFunc("GET /api/flaresolverr/status", s.handleFlareSolverrStatus)
	admin.HandleFunc("GET /api/system/cleanup", s.handleCleanupStatus)
	admin.HandleFunc("POST /api/system/cleanup", s.handleCleanupRun)

	s.searchRoutes(admin, member)
	s.fileRoutes(member, manage, admin) // permissions, folder moves, collections, NFO and artwork (files_*.go)

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
