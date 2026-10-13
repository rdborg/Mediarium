package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/subtitles"
)

type settingsPayload struct {
	MoviesPath string `json:"moviesPath"`
	TVPath     string `json:"tvPath"`
	// Where second copies go ("" turns them off); pointers so "" clears.
	// MoviesExtraPaths and TVExtraPaths are more library folders next to the
	// main ones; each title says which one its files go to. Sent whole: the
	// list replaces the saved one.
	MoviesExtraPaths *[]string `json:"moviesExtraPaths,omitempty"`
	TVExtraPaths     *[]string `json:"tvExtraPaths,omitempty"`
	MovieCopiesPath  *string   `json:"movieCopiesPath,omitempty"`
	TVCopiesPath     *string   `json:"tvCopiesPath,omitempty"`
	DownloadsPath    string    `json:"downloadsPath"`
	NamingPreset     string    `json:"namingPreset"`
	MovieNameFormat  string    `json:"movieNameFormat"`
	// EpisodeNameFormat is the custom file name format for episodes.
	EpisodeNameFormat string `json:"episodeNameFormat"`
	TMDBAPIKey        string `json:"tmdbApiKey,omitempty"`
	HasTMDBAPIKey     bool   `json:"hasTmdbApiKey"`
	OnboardingDone    bool   `json:"onboardingDone"`

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
	SubtitleLanguages []string `json:"subtitleLanguages,omitempty"`
	// SubtitleProfiles says, per language code, which kind to look for: "full" is
	// "either" (normal or SDH, the default), "normal", "sdh" or "none", and
	// "forced" adds a forced track for the foreign-language parts. A language
	// that isn't listed has the default. Sending {} puts every language back to it.
	SubtitleProfiles     map[string]subtitles.Profile `json:"subtitleProfiles,omitempty"`
	SubtitleAutoDownload *bool                        `json:"subtitleAutoDownload,omitempty"`
	SubtitleUpgrade      *bool                        `json:"subtitleUpgrade,omitempty"`
	// SubtitlesEnabled is the master switch. While it is off nothing searches
	// for or downloads subtitles and the app stops mentioning them.
	SubtitlesEnabled *bool `json:"subtitlesEnabled,omitempty"`

	// Phase 4 subtitles module.
	// A pointer so that sending "" (clear my own key, go back to the one that
	// ships with the app) is different from leaving the field out.
	OpenSubtitlesAPIKey    *string `json:"openSubtitlesApiKey,omitempty"`
	HasOpenSubtitlesAPIKey bool    `json:"hasOpenSubtitlesApiKey"`
	// SubDL, the second place subtitles come from ("" removes the key).
	SubDLAPIKey       *string `json:"subdlApiKey,omitempty"`
	HasSubDLAPIKey    bool    `json:"hasSubdlApiKey"`
	HasSubtitleAddons bool    `json:"hasSubtitleAddons"`
	// The on/off switch of each subtitle source (on unless switched off). A
	// source is searched only when its switch is on and it has a key (the
	// add-ons: at least one added), and a switch can't be turned on before then.
	OpenSubtitlesEnabled  *bool `json:"openSubtitlesEnabled,omitempty"`
	SubDLEnabled          *bool `json:"subdlEnabled,omitempty"`
	SubtitleAddonsEnabled *bool `json:"subtitleAddonsEnabled,omitempty"`
	// SubtitleSources is read only: where each source stands (on, off, needs a
	// key, or its daily limit is used up).
	SubtitleSources []subtitleSourceStatus `json:"subtitleSources,omitempty"`
	// SubtitleCountEmbedded counts subtitle tracks inside the video as there.
	SubtitleCountEmbedded *bool `json:"subtitleCountEmbedded,omitempty"`
	// SubtitleWizdom also looks for Hebrew subtitles on Wizdom (off by default).
	SubtitleWizdom *bool `json:"subtitleWizdom,omitempty"`
	// NumberingLookup lets Mediarium read scene and anime numbering (TheXEM, the
	// anime lists and AniList) for anime on its own. On unless switched off.
	NumberingLookup *bool `json:"numberingLookup,omitempty"`
	// UnmonitorDownloaded stops monitoring a movie once it is downloaded;
	// UnmonitorFinished stops monitoring a show once it has ended and every
	// episode is in.
	UnmonitorDownloaded *bool `json:"unmonitorDownloaded,omitempty"`
	// ShowAdvanced shows custom formats and format scores on the Quality page.
	ShowAdvanced *bool `json:"showAdvanced,omitempty"`
	// TrailerBackground plays the trailer behind movie and show pages for every
	// account (a browser can still switch it off for itself).
	TrailerBackground *bool `json:"trailerBackground,omitempty"`
	// TrailerSound starts that trailer with its sound on (a browser that does
	// not allow it keeps it muted, and each browser can choose for itself).
	TrailerSound      *bool `json:"trailerSound,omitempty"`
	UnmonitorFinished *bool `json:"unmonitorFinished,omitempty"`

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

	// The language titles, plots, genres and posters are shown in, as a TMDB
	// tag like "de-DE" (English by default), and the choices. File and folder
	// names always use the English title, whatever is picked.
	InfoLanguage  string              `json:"infoLanguage,omitempty"`
	InfoLanguages []metadata.Language `json:"infoLanguages,omitempty"`

	// Manual-import naming-collision conflict policy.
	ImportConflictPolicy string `json:"importConflictPolicy,omitempty"` // "skip" | "overwrite" | "overwrite_if_better" | "ask"

	// VPN kill switch. Pointer for the same reason as
	// AutomationEnabled above — "omitted" (leave as-is) must be
	// distinguishable from "explicitly turned off".
	RequireVPNForTorrents *bool `json:"requireVpnForTorrents,omitempty"`

	// Indexer traffic through the VPN. A pointer for the same reason.
	RouteIndexersThroughVPN *bool `json:"routeIndexersThroughVpn,omitempty"`

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
	// TrashDays is how many days removed titles' files stay in the recycle
	// bin (7 by default, 0 = delete straight away).
	TrashDays *int `json:"trashDays,omitempty"`
	// BackupAuto is the nightly backup into /config/backups (on by default);
	// BackupKeep how many of them are kept (7 by default).
	BackupAuto *bool `json:"backupAuto,omitempty"`
	BackupKeep *int  `json:"backupKeep,omitempty"`
	// SpeedLimitMB is the download speed limit in MB/s (0 = none);
	// SpeedLimitHours when it applies ("8-23", "" = all day); MinFreeGB the
	// free space below which no new download starts (5 by default, 0 = off).
	SpeedLimitMB    *int    `json:"speedLimitMB,omitempty"`
	SpeedLimitHours *string `json:"speedLimitHours,omitempty"`
	MinFreeGB       *int    `json:"minFreeGB,omitempty"`
	// TorrentStallHours: hours a torrent may get no data before it is given up on (0 = never, absent = the default of each client).
	TorrentStallHours *int `json:"torrentStallHours,omitempty"`
	// DownloadHours is when new downloads may start ("1-7", "" = any time).
	DownloadHours *string `json:"downloadHours,omitempty"`
	// NotifyQuietHours is when everyday messages wait ("23-7", "" = never).
	NotifyQuietHours *string `json:"notifyQuietHours,omitempty"`

	// DownloadsAtOnce is how many downloads may run at the same time, Usenet
	// and torrents together (1 to 5, one by default). The rest wait in line. A
	// pointer so an omitted field leaves it alone.
	DownloadsAtOnce *int `json:"downloadsAtOnce,omitempty"`

	// Checks on releases (Settings > Downloading). UsenetRetentionDays: Usenet
	// posts older than this many days are not taken (0 = no limit).
	// RejectExecutables: refuse releases that hold programs or scripts (on by
	// default). SkipDuplicates: do not download the same release twice (off by
	// default). UnsafeExtensions is the list RejectExecutables looks for;
	// read-only.
	UsenetRetentionDays *int  `json:"usenetRetentionDays,omitempty"`
	RejectExecutables   *bool `json:"rejectExecutables,omitempty"`
	SkipDuplicates      *bool `json:"skipDuplicates,omitempty"`
	// UsenetPrecheck: ask the news servers whether the articles of an NZB exist
	// before downloading it (off by default).
	UsenetPrecheck   *bool    `json:"usenetPrecheck,omitempty"`
	UnsafeExtensions []string `json:"unsafeExtensions,omitempty"`

	// Music module: on/off (off by default; a pointer so an omitted field
	// leaves it alone) and the music library folder.
	MusicEnabled *bool  `json:"musicEnabled,omitempty"`
	MusicPath    string `json:"musicPath,omitempty"`
	// MusicDefaultProfileID is the music quality profile artists without one
	// of their own use (a music profile id; unset or 0 leaves it as it is).
	MusicDefaultProfileID int64 `json:"musicDefaultProfileId,omitempty"`

	// Ebooks and audiobooks library folders.
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
	episodeFormat, _ := s.Settings.Get(settings.KeyEpisodeNameFormat)
	onboardingDone, _ := s.Settings.GetBool(settings.KeyOnboardingDone)
	torrentPort := strconv.Itoa(s.torrentListenPort()) // the port in use, 58264 unless changed
	torrentRatio, _ := s.Settings.Get(settings.KeyTorrentSeedRatioLimit)
	torrentTime, _ := s.Settings.Get(settings.KeyTorrentSeedTimeLimitH)
	automationEnabled := s.automationEnabled()
	torrentEnabled := s.torrentsEnabled()
	monitorMinutes := s.monitorMinutes()
	legalAt, _ := s.Settings.Get(settings.KeyLegalAcknowledgedAt)
	subtitleAuto := s.autoSubtitleSetting()
	subtitleUpgrade := s.subtitleUpgradeSetting()
	numberingLookup := s.numberingLookupOn()
	countEmbedded := s.countEmbeddedSubtitles()
	subtitleWizdom := s.wizdomOn()
	sources := s.subtitleSources()
	osEnabled, subdlEnabled, addonsEnabled := sources.OpenSubtitles.Switch, sources.SubDL.Switch, sources.Addons.Switch
	unmonitorDownloaded, unmonitorFinished := s.unmonitorDownloaded(), s.unmonitorFinished()
	showAdvanced := s.showAdvanced()
	trailerBackground := s.trailerBackground()
	trailerSound := s.trailerSound()
	subtitlesOn := s.subtitlesEnabled()
	osUser, _ := s.Settings.Get(settings.KeyOpenSubtitlesUsername)
	illegalCharMode, _ := s.Settings.Get(settings.KeyIllegalCharMode)
	if illegalCharMode == "" {
		illegalCharMode = "strip"
	}
	illegalCharReplacement, _ := s.Settings.Get(settings.KeyIllegalCharReplacement)
	requireVPNForTorrents := s.vpnRequiredForTorrents()
	routeIndexersThroughVPN := s.vpnForIndexers()
	importConflictPolicy, _ := s.Settings.Get(settings.KeyImportConflictPolicy)
	if importConflictPolicy == "" {
		importConflictPolicy = "skip"
	}
	flareSolverr := s.flareSolverrSetting()
	publicURL := s.publicLinks().Base
	cleanupAuto := s.cleanupAutoEnabled()
	retentionDays := s.historyRetentionDays()
	trashDays := s.trashDays()
	backupAuto, backupKeep := s.backupAutoEnabled(), s.backupKeep()
	speedMB, minFree := s.speedLimitMB(), s.minFreeGB()
	stallHours := s.stallHours()
	speedHours, quiet := "", ""
	if from, to, ok := s.quietHours(); ok {
		quiet = quietHoursText(from, to)
	}
	if from, to, ok := s.speedLimitHours(); ok {
		speedHours = strconv.Itoa(from) + "-" + strconv.Itoa(to)
	}
	dlHours := ""
	if from, to, ok := s.downloadHours(); ok {
		dlHours = strconv.Itoa(from) + "-" + strconv.Itoa(to)
	}
	musicEnabled := s.musicEnabled()
	downloadsAtOnce := s.downloadsAtOnce()
	huntHours, releaseMinutes := s.huntHours(), s.releaseCheckMinutes()
	retention, rejectExe, skipDupes, precheck := s.retentionDays(), s.rejectExecutables(), s.skipDuplicates(), s.precheckEnabled()

	writeJSON(w, http.StatusOK, settingsPayload{
		MoviesPath:               moviesPath,
		TVPath:                   s.tvRoot(),
		MoviesExtraPaths:         ptr(nonNil(s.extraFolders(settings.KeyMoviesExtraPaths, moviesPath))),
		TVExtraPaths:             ptr(nonNil(s.extraFolders(settings.KeyTVExtraPaths, s.tvRoot()))),
		MovieCopiesPath:          ptr(s.movieCopiesRoot()),
		TVCopiesPath:             ptr(s.tvCopiesRoot()),
		DownloadsPath:            downloadsPath,
		NamingPreset:             namingPreset,
		MovieNameFormat:          movieFormat,
		EpisodeNameFormat:        episodeFormat,
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
		SubtitleProfiles:         s.subtitleProfiles(),
		SubtitleAutoDownload:     &subtitleAuto,
		SubtitleUpgrade:          &subtitleUpgrade,
		SubtitlesEnabled:         &subtitlesOn,
		HasOpenSubtitlesAPIKey:   s.Subtitles().HasAPIKey(),
		HasSubDLAPIKey:           s.SubDL().HasAPIKey(),
		HasSubtitleAddons:        sources.Addons.Ready,
		OpenSubtitlesEnabled:     &osEnabled,
		SubDLEnabled:             &subdlEnabled,
		SubtitleAddonsEnabled:    &addonsEnabled,
		SubtitleSources:          s.subtitleSourceStatuses(),
		SubtitleCountEmbedded:    &countEmbedded,
		SubtitleWizdom:           &subtitleWizdom,
		NumberingLookup:          &numberingLookup,
		UnmonitorDownloaded:      &unmonitorDownloaded,
		ShowAdvanced:             &showAdvanced,
		TrailerBackground:        &trailerBackground,
		TrailerSound:             &trailerSound,
		UnmonitorFinished:        &unmonitorFinished,
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
		InfoLanguage:             s.infoLanguage(),
		InfoLanguages:            metadata.InfoLanguages,
		RequireVPNForTorrents:    &requireVPNForTorrents,
		RouteIndexersThroughVPN:  &routeIndexersThroughVPN,
		ImportConflictPolicy:     importConflictPolicy,
		HasTraktClientID:         s.Trakt().HasClientID(),
		FlareSolverrURL:          &flareSolverr,
		PublicURL:                &publicURL,
		FlareSolverrBundled:      s.cfg.BundledFlareSolverr,
		CleanupAuto:              &cleanupAuto,
		HistoryRetentionDays:     &retentionDays,
		TrashDays:                &trashDays,
		BackupAuto:               &backupAuto,
		BackupKeep:               &backupKeep,
		SpeedLimitMB:             &speedMB,
		SpeedLimitHours:          &speedHours,
		DownloadHours:            &dlHours,
		MinFreeGB:                &minFree,
		TorrentStallHours:        &stallHours,
		NotifyQuietHours:         &quiet,
		DownloadsAtOnce:          &downloadsAtOnce,
		UsenetRetentionDays:      &retention,
		RejectExecutables:        &rejectExe,
		SkipDuplicates:           &skipDupes,
		UsenetPrecheck:           &precheck,
		UnsafeExtensions:         download.UnsafeExtensions,
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

	moviesMain, tvMain := firstNonEmpty(req.MoviesPath, s.moviesRoot()), firstNonEmpty(req.TVPath, s.tvRoot())
	// More folders next to the main ones.
	var extraLists = []struct {
		key  string
		val  *[]string
		main string
		what string
	}{{settings.KeyMoviesExtraPaths, req.MoviesExtraPaths, moviesMain, "movies"}, {settings.KeyTVExtraPaths, req.TVExtraPaths, tvMain, "TV"}}
	for _, l := range extraLists {
		if l.val == nil {
			continue
		}
		clean, problem := s.checkExtraFolders(*l.val, l.main, moviesMain, tvMain, l.what)
		if problem != "" {
			writeError(w, http.StatusBadRequest, problem)
			return
		}
		raw, _ := json.Marshal(clean)
		if len(clean) == 0 {
			raw = nil
		}
		if err := s.Settings.Set(l.key, string(raw), false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	for _, c := range []struct {
		key string
		val *string
	}{{settings.KeyMovieCopiesPath, req.MovieCopiesPath}, {settings.KeyTVCopiesPath, req.TVCopiesPath}} {
		if c.val == nil {
			continue
		}
		v := strings.TrimSpace(*c.val)
		if err := checkCopiesFolder(v, moviesMain, tvMain); err != nil {
			writeError(w, http.StatusBadRequest, capitalise(err.Error())+".")
			return
		}
		if v != "" {
			v = filepath.Clean(v)
		}
		if err := s.Settings.Set(c.key, v, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Wanted audio languages: names separated by commas, best first.
	if req.QualityLanguage != "" {
		req.QualityLanguage = strings.Join(quality.ParseLanguages(req.QualityLanguage), ",")
		if len(req.QualityLanguage) > 300 {
			writeError(w, http.StatusBadRequest, "That is too many audio languages.")
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
		{settings.KeyEpisodeNameFormat, req.EpisodeNameFormat},
		{settings.KeyTorrentListenPort, req.TorrentListenPort},
		{settings.KeyTorrentSeedRatioLimit, req.TorrentSeedRatioLimit},
		{settings.KeyTorrentSeedTimeLimitH, req.TorrentSeedTimeLimitH},
		{settings.KeyIllegalCharMode, req.IllegalCharMode},
		{settings.KeyIllegalCharReplacement, req.IllegalCharReplacement},
		{settings.KeyImportConflictPolicy, req.ImportConflictPolicy},
		{settings.KeyQualityLanguage, req.QualityLanguage},
		{settings.KeyInfoLanguage, req.InfoLanguage},
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
	if req.SubtitleProfiles != nil {
		profiles := subtitles.Profiles{}
		for lang, p := range req.SubtitleProfiles {
			lang = strings.TrimSpace(lang)
			if !languageCode.MatchString(lang) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("%q isn't a language code Mediarium can use.", lang))
				return
			}
			switch p.Full {
			case "", subtitles.FullEither, subtitles.FullNormal, subtitles.FullSDH, subtitles.FullNone:
			default:
				writeError(w, http.StatusBadRequest, `The full subtitle must be "either", "normal", "sdh" or "none".`)
				return
			}
			profiles[strings.ToLower(lang)] = p.Normalize()
		}
		if err := s.Settings.Set(settings.KeySubtitleProfiles, profiles.Encode(), false); err != nil {
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
	if req.SubtitleUpgrade != nil {
		value := "1"
		if !*req.SubtitleUpgrade {
			value = "0"
		}
		if err := s.Settings.Set(settings.KeySubtitleUpgrade, value, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.NumberingLookup != nil {
		if err := setBool(s.Settings, settings.KeyNumberingLookup, *req.NumberingLookup); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := s.saveSourceSwitches(&req); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.SubtitleCountEmbedded != nil {
		if err := setBool(s.Settings, settings.KeySubtitleCountEmbedded, *req.SubtitleCountEmbedded); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.SubtitleWizdom != nil {
		if err := setBool(s.Settings, settings.KeySubtitleWizdom, *req.SubtitleWizdom); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.TrailerSound != nil {
		if err := setBool(s.Settings, settings.KeyTrailerSound, *req.TrailerSound); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.TrailerBackground != nil {
		if err := setBool(s.Settings, settings.KeyTrailerBackground, *req.TrailerBackground); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.ShowAdvanced != nil {
		if err := setBool(s.Settings, settings.KeyShowAdvanced, *req.ShowAdvanced); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.UnmonitorDownloaded != nil {
		if err := setBool(s.Settings, settings.KeyUnmonitorDownloaded, *req.UnmonitorDownloaded); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.UnmonitorFinished != nil {
		if err := setBool(s.Settings, settings.KeyUnmonitorFinished, *req.UnmonitorFinished); err != nil {
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
	if req.RouteIndexersThroughVPN != nil {
		value := "1"
		if !*req.RouteIndexersThroughVPN {
			value = "0"
		}
		if err := s.Settings.Set(settings.KeyVPNRouteIndexers, value, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.applyVPNRoutes()
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
	if req.SubDLAPIKey != nil {
		key := strings.TrimSpace(*req.SubDLAPIKey)
		if rejectBad(w, checkAPIKey(key)) {
			return
		}
		if err := s.SetSubDLAPIKey(key); err != nil {
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
	if req.SpeedLimitMB != nil || req.SpeedLimitHours != nil {
		if req.SpeedLimitMB != nil {
			if *req.SpeedLimitMB < 0 || *req.SpeedLimitMB > 10000 {
				writeError(w, http.StatusBadRequest, "The speed limit is 0 (no limit) to 10000 MB/s.")
				return
			}
			if err := s.Settings.Set(settings.KeySpeedLimitMB, strconv.Itoa(*req.SpeedLimitMB), false); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		if req.SpeedLimitHours != nil {
			h := strings.TrimSpace(*req.SpeedLimitHours)
			if _, _, ok := parseHours(h); h != "" && !ok {
				writeError(w, http.StatusBadRequest, `Give the hours as "from-to", for example "8-23", or leave it empty for all day.`)
				return
			}
			if err := s.Settings.Set(settings.KeySpeedLimitHours, h, false); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		s.applySpeedLimit(time.Now())
	}
	if req.DownloadHours != nil {
		h := strings.TrimSpace(*req.DownloadHours)
		if _, _, ok := parseHours(h); h != "" && !ok {
			writeError(w, http.StatusBadRequest, `Give the download hours as "from-to", for example "1-7", or leave it empty for any time.`)
			return
		}
		if err := s.Settings.Set(settings.KeyDownloadHours, h, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.kickDownloads()
	}
	if req.NotifyQuietHours != nil {
		h := strings.TrimSpace(*req.NotifyQuietHours)
		if _, _, ok := parseHours(h); h != "" && !ok {
			writeError(w, http.StatusBadRequest, `Give the quiet hours as "from-to", for example "23-7", or leave it empty for none.`)
			return
		}
		if err := s.Settings.Set(settings.KeyNotifyQuietHours, h, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.TorrentStallHours != nil {
		h := *req.TorrentStallHours
		if h < -1 || h > maxStallHours {
			writeError(w, http.StatusBadRequest, "Give up after 1 to 72 hours, or 0 for never.")
			return
		}
		v := strconv.Itoa(h)
		if h < 0 {
			v = "" // back to each client's own default
		}
		if err := s.Settings.Set(settings.KeyStallHours, v, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.MinFreeGB != nil {
		if *req.MinFreeGB < 0 || *req.MinFreeGB > 10000 {
			writeError(w, http.StatusBadRequest, "Keep 0 (off) to 10000 GB free.")
			return
		}
		if err := s.Settings.Set(settings.KeyMinFreeGB, strconv.Itoa(*req.MinFreeGB), false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.dispatch.Kick()
	}
	if req.BackupAuto != nil {
		v := "1"
		if !*req.BackupAuto {
			v = "0"
		}
		if err := s.Settings.Set(settings.KeyBackupAuto, v, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.BackupKeep != nil {
		if *req.BackupKeep < 1 || *req.BackupKeep > 60 {
			writeError(w, http.StatusBadRequest, "Keep between 1 and 60 backups.")
			return
		}
		if err := s.Settings.Set(settings.KeyBackupKeep, strconv.Itoa(*req.BackupKeep), false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.TrashDays != nil {
		if *req.TrashDays < 0 || *req.TrashDays > 365 {
			writeError(w, http.StatusBadRequest, "Keep removed files for 0 to 365 days.")
			return
		}
		if err := s.Settings.Set(settings.KeyTrashDays, strconv.Itoa(*req.TrashDays), false); err != nil {
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
	if req.UsenetRetentionDays != nil {
		if err := s.Settings.Set(settings.KeyRetentionDays, strconv.Itoa(*req.UsenetRetentionDays), false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	for _, f := range []struct {
		key string
		on  *bool
		off string // what is stored for "off"
	}{{settings.KeyRejectExecutables, req.RejectExecutables, "0"}, {settings.KeySkipDuplicates, req.SkipDuplicates, "0"}, {settings.KeyPrecheck, req.UsenetPrecheck, "0"}} {
		if f.on == nil {
			continue
		}
		v := "1"
		if !*f.on {
			v = f.off
		}
		if err := s.Settings.Set(f.key, v, false); err != nil {
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
	// Problem says what's wrong with a custom format, empty when it's fine.
	Problem string `json:"problem,omitempty"`
}

// namingSamples are the made-up releases the preview names.
var (
	namingMovieSample = organizer.NamingContext{
		MovieTitle: "Example Movie", Year: 2024, Quality: "1080p", Source: "BluRay",
		Codec: "x265", AudioCodec: "DTS", HDR: "HDR10", Edition: "Extended", ReleaseGroup: "GROUP", TMDBID: 12345,
	}
	namingEpisodeSample = organizer.NamingContext{
		SeriesTitle: "Example Show", Year: 2023, Season: 1, Episode: 2, EpisodeTitle: "The Second One", AirDate: "2023-03-14",
		Quality: "1080p", Source: "WEB-DL", Codec: "x264", AudioCodec: "AAC", ReleaseGroup: "GROUP", TMDBID: 67890,
	}
)

// handleNamingPreview shows a naming preset or custom format applied to a
// fixed sample release (kind=movie, the default, or kind=tv), so the page can
// display the exact file name while someone edits the tokens. It uses the
// same organizer.Render and Sanitize the pipeline calls, so the preview always
// matches what is really produced.
func (s *Server) handleNamingPreview(w http.ResponseWriter, r *http.Request) {
	tv := r.URL.Query().Get("kind") == "tv"
	presets, sample, kind := organizer.Presets, namingMovieSample, organizer.TokenMovie
	if tv {
		presets, sample, kind = organizer.TVPresets, namingEpisodeSample, organizer.TokenTV
	}
	format := r.URL.Query().Get("format")
	problem := ""
	if format == "" {
		if preset, ok := presets[r.URL.Query().Get("preset")]; ok {
			format = preset
		} else {
			format = presets["plex"]
		}
	} else {
		problem = organizer.CheckFormat(format, kind)
	}
	mode, replacement := s.illegalCharSettings()
	clean := func(f string) string { return organizer.Sanitize(organizer.Render(f, sample), mode, replacement) }
	out := namingPreviewPayload{Folder: clean(organizer.Presets["plex"]), Filename: clean(format) + ".mkv", Problem: problem}
	if tv {
		out.Folder = clean(organizer.TVSeriesFolder) + "/" + clean(organizer.TVSeasonFolder)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleNamingTokens lists the tokens a custom format can use and the
// ready-made formats to start from, for the builder in Settings.
func (s *Server) handleNamingTokens(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tokens": organizer.Tokens(), "schemes": organizer.Schemes})
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
