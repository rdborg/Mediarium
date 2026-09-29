package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ryanborg/mediarium/internal/organizer"
	"github.com/ryanborg/mediarium/internal/settings"
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

	// Clean-up: the daily automatic clean-up of the downloads working folder
	// (on by default) and how many days finished downloads and activity are
	// kept (90 by default, 0 = forever). Pointers so an omitted field leaves
	// the setting alone.
	CleanupAuto          *bool `json:"cleanupAuto,omitempty"`
	HistoryRetentionDays *int  `json:"historyRetentionDays,omitempty"`
}

// handleGetSettings never echoes the TMDB key back (credentials
// never logged/exposed in plaintext beyond what's needed); HasTMDBAPIKey
// tells the UI whether one is already configured.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	moviesPath := s.moviesRoot()
	downloadsPath, _ := s.Settings.Get(settings.KeyDownloadsPath)
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
	subtitleAuto := s.autoSubtitlesEnabled()
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
	flareSolverr := s.flareSolverrURL()
	cleanupAuto := s.cleanupAutoEnabled()
	retentionDays := s.historyRetentionDays()

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
		LegalAcknowledgedAt:      legalAt,
		DefaultProfileID:         s.defaultProfileID(),
		DefaultSources:           s.defaultSources(),
		SubtitleLanguages:        s.subtitleLanguages(),
		SubtitleAutoDownload:     &subtitleAuto,
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
		RequireVPNForTorrents:    &requireVPNForTorrents,
		ImportConflictPolicy:     importConflictPolicy,
		HasTraktClientID:         s.Trakt().HasClientID(),
		FlareSolverrURL:          &flareSolverr,
		CleanupAuto:              &cleanupAuto,
		HistoryRetentionDays:     &retentionDays,
	})
}

// handlePutSettings applies a partial update — only non-empty fields are
// written, so the frontend can PUT just the field(s) a given wizard
// step/settings form changed.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsPayload
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if p := strings.TrimSpace(req.TorrentListenPort); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 0 || n > 65535 {
			writeError(w, http.StatusBadRequest, "torrentListenPort must be a port number from 1 to 65535 (0 means the default, 58264)")
			return
		}
		req.TorrentListenPort = p
	}
	if d := req.HistoryRetentionDays; d != nil && (*d < 0 || *d > 36500) {
		writeError(w, http.StatusBadRequest, "historyRetentionDays must be 0 (keep forever) or a number of days up to 36500")
		return
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
		var cleaned []string
		for _, l := range req.SubtitleLanguages {
			if l = strings.TrimSpace(l); l != "" && !strings.ContainsAny(l, ", /\\") {
				cleaned = append(cleaned, l)
			}
		}
		if len(cleaned) == 0 {
			writeError(w, http.StatusBadRequest, "no valid subtitle languages given")
			return
		}
		if err := s.Settings.Set(settings.KeySubtitleLanguages, strings.Join(cleaned, ","), false); err != nil {
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
		if !validSourcePref(req.DefaultSources) {
			writeError(w, http.StatusBadRequest, `defaultSources must be "usenet", "torrent" or "both"`)
			return
		}
		if err := s.Settings.Set(settings.KeyDefaultSources, req.DefaultSources, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if req.DefaultProfileID != 0 {
		if err := s.checkProfileChoice(req.DefaultProfileID); err != nil {
			writeProfileError(w, err)
			return
		}
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
		if n < 0 {
			writeError(w, http.StatusBadRequest, "monitorIntervalMinutes must be 0 (off) or at least 5")
			return
		}
		if n > 0 && n < minMonitorMinutes {
			n = minMonitorMinutes
		}
		if err := s.Settings.Set(settings.KeyMonitorIntervalMinutes, strconv.Itoa(n), false); err != nil {
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
		v := strings.TrimRight(strings.TrimSpace(*req.FlareSolverrURL), "/")
		if v != "" {
			u, err := url.Parse(v)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				writeError(w, http.StatusBadRequest, "flareSolverrUrl must be an http(s) address, e.g. http://flaresolverr:8191")
				return
			}
		}
		if err := s.Settings.Set(settings.KeyFlareSolverrURL, v, false); err != nil {
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

// handleNamingPreview renders a naming preset/custom-format string against
// a fixed sample release, so the UI can show "the exact resulting
// filename as they edit the tokens" — a live preview that was missing
// entirely until this endpoint. Uses the
// real organizer.Render/Sanitize the pipeline itself calls, so the
// preview can never drift out of sync with what actually gets produced.
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

func (s *Server) handleFilesystemCheck(w http.ResponseWriter, r *http.Request) {
	a := r.URL.Query().Get("a")
	b := r.URL.Query().Get("b")
	if a == "" || b == "" {
		writeError(w, http.StatusBadRequest, "query params a and b are required")
		return
	}
	same, supported, err := organizer.SameFilesystem(a, b)
	payload := filesystemCheckPayload{SameFilesystem: same, Supported: supported}
	if err != nil {
		payload.Error = err.Error()
	}
	writeJSON(w, http.StatusOK, payload)
}
