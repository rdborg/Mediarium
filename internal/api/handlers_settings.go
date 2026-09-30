package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/settings"
)

type settingsPayload struct {
	MoviesPath      string `json:"moviesPath"`
	TVPath          string `json:"tvPath"`
	DownloadsPath   string `json:"downloadsPath"`
	NamingPreset    string `json:"namingPreset"`
	MovieNameFormat string `json:"movieNameFormat"`
	TMDBAPIKey      string `json:"tmdbApiKey,omitempty"`
	HasTMDBAPIKey   bool   `json:"hasTmdbApiKey"`
	OnboardingDone  bool   `json:"onboardingDone"`

	// TorrentEnabled is the torrent on/off switch. A pointer so a partial PUT
	// that leaves it out doesn't switch torrents off (or on).
	TorrentEnabled *bool `json:"torrentEnabled,omitempty"`

	// MonitorIntervalMinutes is how often the connectivity monitor checks every
	// Usenet server and indexer: 0 turns it off, otherwise at least 5. Pointer so
	// an omitted field leaves it alone.
	MonitorIntervalMinutes *int `json:"monitorIntervalMinutes,omitempty"`

	// HuntIntervalHours is how often Mediarium looks for missing items and
	// better versions (1 to 168); ReleaseCheckMinutes is how often it checks each
	// indexer's newest releases (5 to 1440). Pointers so an omitted field
	// leaves them alone.
	HuntIntervalHours   *int `json:"huntIntervalHours,omitempty"`
	ReleaseCheckMinutes *int `json:"releaseCheckMinutes,omitempty"`

	// Legal notice: LegalAcknowledgedAt (RFC 3339) is when it was accepted, ""
	// if never. Sending legalAcknowledged:true stamps it with the server's
	// clock; any other non-empty legalAcknowledgedAt is stored as sent.
	LegalAcknowledgedAt string `json:"legalAcknowledgedAt"`
	LegalAcknowledged   *bool  `json:"legalAcknowledged,omitempty"`

	// Phase 2 torrent engine settings.
	TorrentListenPort     string `json:"torrentListenPort,omitempty"`
	TorrentSeedRatioLimit string `json:"torrentSeedRatioLimit,omitempty"`
	TorrentSeedTimeLimitH string `json:"torrentSeedTimeLimitH,omitempty"`

	// Phase 2 automation + quality profile settings.
	// AutomationEnabled is a pointer in requests so "field omitted" (leave
	// as-is) is distinguishable from "explicitly set to false" — a plain
	// bool would make every partial PUT that doesn't mention it silently
	// disable automation.
	AutomationEnabled *bool  `json:"automationEnabled,omitempty"`
	DefaultProfileID  int64  `json:"defaultProfileId,omitempty"`
	DefaultSources    string `json:"defaultSources,omitempty"` // usenet | torrent | both

	// Subtitles: languages to keep (OpenSubtitles codes) and whether to fetch
	// them automatically after import and on a schedule.
	SubtitleLanguages    []string `json:"subtitleLanguages,omitempty"`
	SubtitleAutoDownload *bool    `json:"subtitleAutoDownload,omitempty"`
	// SubtitlesEnabled is the master switch. While it is off nothing searches
	// for or downloads subtitles and the app stops mentioning them.
	SubtitlesEnabled *bool `json:"subtitlesEnabled,omitempty"`

	// Phase 4 subtitles module.
	// A pointer so that sending "" (clear my own key, go back to the one that
	// ships with the app) is different from leaving the field out.
	OpenSubtitlesAPIKey    *string `json:"openSubtitlesApiKey,omitempty"`
	HasOpenSubtitlesAPIKey bool    `json:"hasOpenSubtitlesApiKey"`

	// Optional OpenSubtitles.com account (raises the daily download limit).
	OpenSubtitlesUsername    *string `json:"openSubtitlesUsername,omitempty"`
	OpenSubtitlesPassword    string  `json:"openSubtitlesPassword,omitempty"`
	HasOpenSubtitlesAccount  bool    `json:"hasOpenSubtitlesAccount"`
	OpenSubtitlesAccountName string  `json:"openSubtitlesAccountName,omitempty"`

	// True when an official release ships its own key, so the UI can hide
	// the field: users don't need to sign up for these services themselves.
	TMDBKeyBuiltIn          bool `json:"tmdbKeyBuiltIn"`
	OpenSubtitlesKeyBuiltIn bool `json:"openSubtitlesKeyBuiltIn"`
	TraktClientIDBuiltIn    bool `json:"traktClientIdBuiltIn"`

	// True when the person saved their own key, which takes precedence over
	// the built-in (shared) one.
	TMDBUsingOwnKey          bool `json:"tmdbUsingOwnKey"`
	OpenSubtitlesUsingOwnKey bool `json:"openSubtitlesUsingOwnKey"`
	TraktUsingOwnKey         bool `json:"traktUsingOwnKey"`

	// Illegal filename character handling.
	IllegalCharMode        string `json:"illegalCharMode,omitempty"`        // "strip" | "replace"
	IllegalCharReplacement string `json:"illegalCharReplacement,omitempty"` // used when mode=replace

	// The audio language wanted, as a name like "English" (the default). A
	// release clearly in another language only is not picked automatically.
	QualityLanguage string `json:"qualityLanguage,omitempty"`

	// Manual-import naming-collision conflict policy.
	ImportConflictPolicy string `json:"importConflictPolicy,omitempty"` // "skip" | "overwrite" | "overwrite_if_better" | "ask"

	// VPN kill switch. Pointer for the same reason as
	// AutomationEnabled above — "omitted" (leave as-is) must be
	// distinguishable from "explicitly turned off".
	RequireVPNForTorrents *bool `json:"requireVpnForTorrents,omitempty"`

	// Curated/public list import — Trakt's public
	// list-items endpoint only needs an app client ID, no user OAuth.
	// A pointer for the same reason as OpenSubtitlesAPIKey.
	TraktClientID    *string `json:"traktClientId,omitempty"`
	HasTraktClientID bool    `json:"hasTraktClientId"`

	// Address of the person's FlareSolverr (e.g. http://flaresolverr:8191),
	// used for indexer sites behind a Cloudflare check. A pointer so ""
	// (remove it) differs from leaving the field out. None by default.
	FlareSolverrURL *string `json:"flareSolverrUrl,omitempty"`
	// The address you open Mediarium at (like https://mediarium.example.com).
	// Buttons and links in notification messages use it; without one they
	// carry none. A pointer so "" (remove it) differs from leaving it out.
	PublicURL *string `json:"publicUrl,omitempty"`
	// True in the "-full" image, where FlareSolverr is built in and used
	// automatically when no address is saved above. Read-only.
	FlareSolverrBundled bool `json:"flareSolverrBundled"`

	// Clean-up: the daily automatic clean-up of the downloads working folder
	// (on by default) and how many days finished downloads and activity are
	// kept (90 by default, 0 = forever). Pointers so an omitted field leaves
	// the setting alone.
	CleanupAuto          *bool `json:"cleanupAuto,omitempty"`
	HistoryRetentionDays *int  `json:"historyRetentionDays,omitempty"`

	// DownloadsAtOnce is how many downloads may run at the same time, Usenet
	// and torrents together (1 to 5, one by default). The rest wait in line. A
	// pointer so an omitted field leaves it alone.
	DownloadsAtOnce *int `json:"downloadsAtOnce,omitempty"`

	// Music module: on/off (off by default; a pointer so an omitted field
	// leaves it alone) and the music library folder.
	MusicEnabled *bool  `json:"musicEnabled,omitempty"`
	MusicPath    string `json:"musicPath,omitempty"`
	// MusicDefaultProfileID is the music quality profile artists without one
	// of their own use (a music profile id; unset or 0 leaves it as it is).
	MusicDefaultProfileID int64 `json:"musicDefaultProfileId,omitempty"`

	// Ebooks and audiobooks folders. Those modules are not built yet, so the
	// folders are only remembered (and shown greyed in Settings).
	EbooksPath     string `json:"ebooksPath,omitempty"`
	AudiobooksPath string `json:"audiobooksPath,omitempty"`

	// ContainerFolders is what the container itself maps: the folders named
	// by MOVIES_DIR, TV_DIR, DOWNLOADS_DIR, MUSIC_DIR, EBOOKS_DIR and
	// AUDIOBOOKS_DIR (or their defaults). Read-only. The setup wizard starts
	// from these, and shows a note when a saved folder differs from them.
	ContainerFolders *containerFolders `json:"containerFolders,omitempty"`
}

// containerFolders are the folder paths from the container's environment.
type containerFolders struct {
	Movies     string `json:"movies"`
	TV         string `json:"tv"`
	Downloads  string `json:"downloads"`
	Music      string `json:"music"`
	Ebooks     string `json:"ebooks"`
	Audiobooks string `json:"audiobooks"`
}

func (s *Server) containerFolders() *containerFolders {
	return &containerFolders{
		Movies:     s.cfg.MoviesDir,
		TV:         s.cfg.TVDir,
		Downloads:  s.cfg.DownloadsDir,
		Music:      s.cfg.MusicDir,
		Ebooks:     s.cfg.EbooksDir,
		Audiobooks: s.cfg.AudiobooksDir,
	}
}

// handleGetSettings never echoes the TMDB key back (credentials
// never logged/exposed in plaintext beyond what's needed); HasTMDBAPIKey
// tells the UI whether one is already configured.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	moviesPath := s.moviesRoot()
	downloadsPath := s.downloadsRoot()
	namingPreset, _ := s.Settings.Get(settings.KeyNamingPreset)
	movieFormat, _ := s.Settings.Get(settings.KeyMovieNameFormat)
	onboardingDone, _ := s.Settings.GetBool(settings.KeyOnboardingDone)
	torrentPort := strconv.Itoa(s.torrentListenPort()) // the port in use, 58264 unless changed
	torrentRatio, _ := s.Settings.Get(settings.KeyTorrentSeedRatioLimit)
	torrentTime, _ := s.Settings.Get(settings.KeyTorrentSeedTimeLimitH)
	automationEnabled := s.automationEnabled()
	torrentEnabled := s.torrentsEnabled()
	monitorMinutes := s.monitorMinutes()
	legalAt, _ := s.Settings.Get(settings.KeyLegalAcknowledgedAt)
	subtitleAuto := s.autoSubtitleSetting()
	subtitlesOn := s.subtitlesEnabled()
	osUser, _ := s.Settings.Get(settings.KeyOpenSubtitlesUsername)
	illegalCharMode, _ := s.Settings.Get(settings.KeyIllegalCharMode)
	if illegalCharMode == "" {
		illegalCharMode = "strip"
	}
	illegalCharReplacement, _ := s.Settings.Get(settings.KeyIllegalCharReplacement)
	requireVPNForTorrents := s.vpnRequiredForTorrents()
	importConflictPolicy, _ := s.Settings.Get(settings.KeyImportConflictPolicy)
	if importConflictPolicy == "" {
		importConflictPolicy = "skip"
	}
	flareSolverr := s.flareSolverrSetting()
	publicURL := s.publicLinks().Base
	cleanupAuto := s.cleanupAutoEnabled()
	retentionDays := s.historyRetentionDays()
	musicEnabled := s.musicEnabled()
	downloadsAtOnce := s.downloadsAtOnce()
	huntHours, releaseMinutes := s.huntHours(), s.releaseCheckMinutes()

	writeJSON(w, http.StatusOK, settingsPayload{
		MoviesPath:               moviesPath,
		TVPath:                   s.tvRoot(),
		DownloadsPath:            downloadsPath,
		NamingPreset:             namingPreset,
		MovieNameFormat:          movieFormat,
		HasTMDBAPIKey:            s.TMDB().HasAPIKey(),
		OnboardingDone:           onboardingDone,
		TorrentListenPort:        torrentPort,
		TorrentSeedRatioLimit:    torrentRatio,
		TorrentSeedTimeLimitH:    torrentTime,
		AutomationEnabled:        &automationEnabled,
		TorrentEnabled:           &torrentEnabled,
		MonitorIntervalMinutes:   &monitorMinutes,
		HuntIntervalHours:        &huntHours,
		ReleaseCheckMinutes:      &releaseMinutes,
		LegalAcknowledgedAt:      legalAt,
		DefaultProfileID:         s.defaultProfileID(),
		DefaultSources:           s.defaultSources(),
		SubtitleLanguages:        s.subtitleLanguages(),
		SubtitleAutoDownload:     &subtitleAuto,
		SubtitlesEnabled:         &subtitlesOn,
		HasOpenSubtitlesAPIKey:   s.Subtitles().HasAPIKey(),
		HasOpenSubtitlesAccount:  s.Subtitles().HasCredentials(),
		OpenSubtitlesAccountName: osUser,
		TMDBKeyBuiltIn:           s.builtin.TMDB != "",
		OpenSubtitlesKeyBuiltIn:  s.builtin.OpenSubtitles != "",
		TraktClientIDBuiltIn:     s.builtin.TraktClientID != "",
		TMDBUsingOwnKey:          s.usingOwnKey(settings.KeyTMDBAPIKey),
		OpenSubtitlesUsingOwnKey: s.usingOwnKey(settings.KeyOpenSubtitlesAPIKey),
		TraktUsingOwnKey:         s.usingOwnKey(settings.KeyTraktClientID),
		IllegalCharMode:          illegalCharMode,
		IllegalCharReplacement:   illegalCharReplacement,
		QualityLanguage:          s.preferredLanguage(),
		RequireVPNForTorrents:    &requireVPNForTorrents,
		ImportConflictPolicy:     importConflictPolicy,
		HasTraktClientID:         s.Trakt().HasClientID(),
		FlareSolverrURL:          &flareSolverr,
		PublicURL:                &publicURL,
		FlareSolverrBundled:      s.cfg.BundledFlareSolverr,
		CleanupAuto:              &cleanupAuto,
		HistoryRetentionDays:     &retentionDays,
		DownloadsAtOnce:          &downloadsAtOnce,
		MusicEnabled:             &musicEnabled,
		MusicPath:                s.musicRoot(),
		MusicDefaultProfileID:    s.musicDefaultProfileIDNow(),
		EbooksPath:               s.ebooksRoot(),
		AudiobooksPath:           s.audiobooksRoot(),
		ContainerFolders:         s.containerFolders(),
	})
}

// handlePutSettings applies a partial update — only non-empty fields are
// written, so the frontend can PUT just the field(s) a given wizard
// step/settings form changed.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsPayload
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if rejectBad(w, s.validateSettings(&req)) {
		return
	}
	// Choices that point at another record are checked before anything is
	// written, so a bad one can't leave the update half applied.
	if req.DefaultProfileID != 0 {
		if err := s.checkProfileChoice(req.DefaultProfileID); err != nil {
			writeProfileError(w, err)
			return
		}
	}
	if req.MusicDefaultProfileID != 0 {
		if err := s.checkMusicProfileChoice(req.MusicDefaultProfileID); err != nil {
			writeMusicProfileError(w, err)
			return
		}
	}

	sets := []struct {
		key, value string
	}{
		{settings.KeyMoviesPath, req.MoviesPath},
		{settings.KeyTVPath, req.TVPath},
		{settings.KeyDownloadsPath, req.DownloadsPath},
		{settings.KeyNamingPreset, req.NamingPreset},
		{settings.KeyMovieNameFormat, req.MovieNameFormat},
		{settings.KeyTorrentListenPort, req.TorrentListenPort},
		{settings.KeyTorrentSeedRatioLimit, req.TorrentSeedRatioLimit},
		{settings.KeyTorrentSeedTimeLimitH, req.TorrentSeedTimeLimitH},
		{settings.KeyIllegalCharMode, req.IllegalCharMode},
		{settings.KeyIllegalCharReplacement, req.IllegalCharReplacement},
		{settings.KeyImportConflictPolicy, req.ImportConflictPolicy},
		{settings.KeyQualityLanguage, req.QualityLanguage},
		{settings.KeyMusicPath, req.MusicPath},
		{settings.KeyEbooksPath, req.EbooksPath},
		{settings.KeyAudiobooksPath, req.AudiobooksPath},
	}
	for _, set := range sets {
		if set.value == "" {
			continue
		}
		if err := s.Settings.Set(set.key, set.value, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if len(req.SubtitleLanguages) > 0 {
		if err := s.Settings.Set(settings.KeySubtitleLanguages, strings.Join(req.SubtitleLanguages, ","), false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.SubtitlesEnabled != nil {
		value := "1"
		if !*req.SubtitlesEnabled {
			value = "0"
		}
		if err := s.Settings.Set(settings.KeySubtitlesEnabled, value, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.SubtitleAutoDownload != nil {
		value := "1"
		if !*req.SubtitleAutoDownload {
			value = "0"
		}
		if err := s.Settings.Set(settings.KeySubtitleAutoDownload, value, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if req.DefaultSources != "" {
		if err := s.Settings.Set(settings.KeyDefaultSources, req.DefaultSources, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if req.DefaultProfileID != 0 {
		if err := s.Settings.Set(settings.KeyDefaultProfileID, strconv.FormatInt(req.DefaultProfileID, 10), false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if req.AutomationEnabled != nil {
		value := "1"
		if !*req.AutomationEnabled {
			value = "0"
		}
		if err := s.Settings.Set(settings.KeyAutomationEnabled, value, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if req.TorrentEnabled != nil {
		if err := s.setTorrentsEnabled(*req.TorrentEnabled); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if req.MonitorIntervalMinutes != nil {
		n := *req.MonitorIntervalMinutes
		if n > 0 && n < minMonitorMinutes {
			n = minMonitorMinutes
		}
		if err := s.Settings.Set(settings.KeyMonitorIntervalMinutes, strconv.Itoa(n), false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	for _, n := range []struct {
		key string
		val *int
	}{{settings.KeyHuntIntervalHours, req.HuntIntervalHours}, {settings.KeyReleaseCheckMinutes, req.ReleaseCheckMinutes}} {
		if n.val == nil {
			continue
		}
		if err := s.Settings.Set(n.key, strconv.Itoa(*n.val), false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if req.LegalAcknowledged != nil && *req.LegalAcknowledged {
		req.LegalAcknowledgedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if req.LegalAcknowledgedAt != "" {
		if err := s.Settings.Set(settings.KeyLegalAcknowledgedAt, req.LegalAcknowledgedAt, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if req.RequireVPNForTorrents != nil {
		value := "1"
		if !*req.RequireVPNForTorrents {
			value = "0"
		}
		if err := s.Settings.Set(settings.KeyVPNRequireForTorrents, value, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if req.TMDBAPIKey != "" {
		if err := s.SetTMDBAPIKey(req.TMDBAPIKey); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.OpenSubtitlesUsername != nil {
		user := strings.TrimSpace(*req.OpenSubtitlesUsername)
		pass := req.OpenSubtitlesPassword
		if user == "" {
			pass = "" // clearing the username removes the account
		} else if pass == "" {
			// Same username with the password left blank keeps the saved one.
			pass, _ = s.Settings.Get(settings.KeyOpenSubtitlesPassword)
		}
		if err := s.SetOpenSubtitlesAccount(user, pass); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.OpenSubtitlesAPIKey != nil {
		if err := s.SetOpenSubtitlesAPIKey(strings.TrimSpace(*req.OpenSubtitlesAPIKey)); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.TraktClientID != nil {
		if err := s.SetTraktClientID(strings.TrimSpace(*req.TraktClientID)); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.FlareSolverrURL != nil {
		if err := s.Settings.Set(settings.KeyFlareSolverrURL, *req.FlareSolverrURL, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.PublicURL != nil {
		if err := s.Settings.Set(settings.KeyPublicURL, *req.PublicURL, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.CleanupAuto != nil {
		value := "1"
		if !*req.CleanupAuto {
			value = "0"
		}
		if err := s.Settings.Set(settings.KeyCleanupAuto, value, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.HistoryRetentionDays != nil {
		if err := s.Settings.Set(settings.KeyHistoryRetentionDays, strconv.Itoa(*req.HistoryRetentionDays), false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.DownloadsAtOnce != nil {
		if err := s.Settings.Set(settings.KeyDownloadsConcurrent, strconv.Itoa(*req.DownloadsAtOnce), false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// More places may be free now. Fewer never stops a running download:
		// nothing new starts until enough of them have finished.
		s.dispatch.Kick()
	}
	if req.MusicEnabled != nil {
		if err := s.setModules(moduleChanges{Music: req.MusicEnabled}); err != nil {
			writeModulesError(w, err)
			return
		}
	}
	if req.MusicDefaultProfileID != 0 {
		if err := s.Settings.Set(settings.KeyMusicDefaultProfileID, strconv.FormatInt(req.MusicDefaultProfileID, 10), false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.OnboardingDone {
		if err := s.Settings.Set(settings.KeyOnboardingDone, "1", false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	s.handleGetSettings(w, r)
}

type filesystemCheckPayload struct {
	SameFilesystem bool   `json:"sameFilesystem"`
	Supported      bool   `json:"supported"`
	Error          string `json:"error,omitempty"`
}

// handleFilesystemCheck is the onboarding wizard / Settings library-paths
// check: warn the user up front if
// their downloads and library paths won't support hardlinking, rather
// than letting them discover double storage use later. Supported=false
// (e.g. running natively on Windows during dev) means "couldn't
// determine" — the frontend should say so rather than asserting either
// way.
type namingPreviewPayload struct {
	Folder   string `json:"folder"`
	Filename string `json:"filename"`
}

// handleNamingPreview shows a naming preset or custom format applied to a
// fixed sample release, so the page can display the exact file name while
// someone edits the tokens. It uses the same organizer.Render and Sanitize
// the pipeline calls, so the preview always matches what is really produced.
func (s *Server) handleNamingPreview(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		if preset, ok := organizer.Presets[r.URL.Query().Get("preset")]; ok {
			format = preset
		} else {
			format = organizer.Presets["plex"]
		}
	}

	sample := organizer.NamingContext{
		MovieTitle: "Example Movie", Year: 2024, Quality: "1080p", Source: "BluRay",
		Codec: "x264", Edition: "Extended", ReleaseGroup: "GROUP",
	}
	mode, replacement := s.illegalCharSettings()
	folder := organizer.Sanitize(organizer.Render(organizer.Presets["plex"], sample), mode, replacement)
	filename := organizer.Sanitize(organizer.Render(format, sample), mode, replacement) + ".mkv"

	writeJSON(w, http.StatusOK, namingPreviewPayload{Folder: folder, Filename: filename})
}

// nearestExisting walks up from path to the closest folder that exists, and
// gives path back unchanged when nothing above it does.
func nearestExisting(path string) string {
	p := filepath.Clean(path)
	for {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		up := filepath.Dir(p)
		if up == p {
			return path
		}
		p = up
	}
}

func (s *Server) handleFilesystemCheck(w http.ResponseWriter, r *http.Request) {
	a := r.URL.Query().Get("a")
	b := r.URL.Query().Get("b")
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if rejectBad(w,
		checkRequired(a, "Enter both folder paths to compare."), checkRequired(b, "Enter both folder paths to compare."),
		checkAbsPath(a, "/downloads"), checkAbsPath(b, "/media/movies"),
	) {
		return
	}
	// A folder Mediarium has not made yet is judged by the folder it will be made in.
	same, supported, err := organizer.SameFilesystem(nearestExisting(a), nearestExisting(b))
	payload := filesystemCheckPayload{SameFilesystem: same, Supported: supported}
	if err != nil {
		payload.Error = plainerror.Message(err)
	}
	writeJSON(w, http.StatusOK, payload)
}
