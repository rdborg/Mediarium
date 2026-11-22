package api

import (
	"fmt"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/organizer"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/settings"
)

// Limits for what Settings accepts.
const (
	maxPathLen          = 4096
	maxNameFormatLen    = 500
	maxHistoryDays      = 36500
	maxMonitorMinutes   = 10080 // a week
	maxSeedRatio        = 1000
	maxSeedHours        = 87600 // ten years
	maxSubtitleLangs    = 50
	maxServiceAccountLn = 100
)

var (
	namingPresets    = []string{"plex", "jellyfin", "kodi", "minimal", "custom"}
	illegalCharModes = []string{"strip", "replace"}
	conflictPolicies = []string{"skip", "overwrite", "overwrite_if_better", "ask"}

	// A subtitle language code such as en, pt-BR or zh-CN.
	languageCode = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})?$`)
	// Characters that can't go in a file name on Windows, and control characters.
	badNameChar = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
)

// changed reports whether v differs from what is saved under key. A value
// that is already saved is never re-checked, so an old install whose saved
// value would fail today's rules can still save other settings.
func (s *Server) changed(key, v string) bool {
	old, _ := s.Settings.Get(key)
	return strings.TrimSpace(old) != v
}

// checkLibraryFolder refuses a media folder that would put Mediarium's own
// files at risk: the whole disk, the settings folder (holding the database and
// the encryption key), a folder that contains it or one inside it. Clean-up and
// "delete the files too" work on these folders, and the settings folder is
// where the encryption key lives.
func (s *Server) checkLibraryFolder(v, label string) string {
	clean := path.Clean(strings.ReplaceAll(v, "\\", "/"))
	if clean == "/" || (len(clean) <= 3 && strings.HasSuffix(clean, ":/")) || clean == "." {
		return label + " can't be the whole disk. Pick a folder of its own, for example /data/media."
	}
	cfg := path.Clean(strings.ReplaceAll(s.cfg.ConfigDir, "\\", "/"))
	if cfg == "" || cfg == "." {
		return ""
	}
	if clean == cfg || strings.HasPrefix(clean+"/", cfg+"/") || strings.HasPrefix(cfg+"/", clean+"/") {
		return label + " can't be, hold or sit inside Mediarium's own settings folder (" + s.cfg.ConfigDir + "). Pick a different folder."
	}
	return ""
}

// validateSettings checks every value of a settings update before anything is
// written, so a bad value in the middle can't leave the update half applied.
// It also tidies the values it checks (trims spaces). It returns a message for
// the first problem, or "". A blank value means "leave this as it is", the way
// handlePutSettings has always treated it.
func (s *Server) validateSettings(req *settingsPayload) string {
	// Folders.
	req.MoviesPath = strings.TrimSpace(req.MoviesPath)
	req.TVPath = strings.TrimSpace(req.TVPath)
	req.DownloadsPath = strings.TrimSpace(req.DownloadsPath)
	req.MusicPath = strings.TrimSpace(req.MusicPath)
	req.EbooksPath = strings.TrimSpace(req.EbooksPath)
	req.AudiobooksPath = strings.TrimSpace(req.AudiobooksPath)
	req.OtherPath = strings.TrimSpace(req.OtherPath)
	for _, f := range []struct {
		key, value, label, example string
	}{
		{settings.KeyMoviesPath, req.MoviesPath, "The movies folder", "/media/movies"},
		{settings.KeyTVPath, req.TVPath, "The TV folder", "/media/tv"},
		{settings.KeyDownloadsPath, req.DownloadsPath, "The downloads folder", "/downloads"},
		{settings.KeyMusicPath, req.MusicPath, "The music folder", "/media/music"},
		{settings.KeyEbooksPath, req.EbooksPath, "The ebooks folder", "/media/ebooks"},
		{settings.KeyAudiobooksPath, req.AudiobooksPath, "The audiobooks folder", "/media/audiobooks"},
		{settings.KeyOtherPath, req.OtherPath, "The Other folder", "/media/other"},
	} {
		if f.value == "" || !s.changed(f.key, f.value) {
			continue
		}
		if m := firstProblem(checkAbsPath(f.value, f.example), checkMaxLen(f.value, f.label, maxPathLen), s.checkLibraryFolder(f.value, f.label)); m != "" {
			return m
		}
	}

	// File names.
	req.NamingPreset = strings.TrimSpace(req.NamingPreset)
	if m := checkChoice(req.NamingPreset, "Pick one of the naming styles from the list: Plex, Jellyfin, Kodi, Minimal or Custom.", namingPresets...); m != "" {
		return m
	}
	// The full token check only matters once a format is the one in use; while
	// another style is picked the box is just a saved draft. Switching to
	// Custom puts the saved draft in use, so it is checked then too, even when
	// it isn't edited in the same save.
	preset := req.NamingPreset
	if preset == "" {
		preset, _ = s.Settings.Get(settings.KeyNamingPreset)
	}
	toCustom := preset == "custom" && s.changed(settings.KeyNamingPreset, preset)
	for _, f := range []struct {
		value *string
		key   string
		label string
		kind  organizer.TokenKind
	}{
		{&req.MovieNameFormat, settings.KeyMovieNameFormat, "The file name format", organizer.TokenMovie},
		{&req.EpisodeNameFormat, settings.KeyEpisodeNameFormat, "The episode file name format", organizer.TokenTV},
	} {
		*f.value = strings.TrimSpace(*f.value)
		format := *f.value
		edited := format != "" && s.changed(f.key, format)
		if edited {
			if m := firstProblem(checkMaxLen(format, f.label, maxNameFormatLen), checkNoControl(format, f.label)); m != "" {
				return m
			}
		}
		if preset != "custom" || (!edited && !toCustom) {
			continue
		}
		if format == "" {
			format, _ = s.Settings.Get(f.key)
		}
		if m := organizer.CheckFormat(format, f.kind); m != "" {
			return m
		}
	}
	// The folder formats follow the same rules as the file name formats.
	for _, f := range []struct {
		value *string
		key   string
		label string
		kind  organizer.TokenKind
	}{
		{&req.MovieFolderFormat, settings.KeyMovieFolderFormat, "The movie folder format", organizer.TokenMovie},
		{&req.SeriesFolderFormat, settings.KeySeriesFolderFormat, "The show folder format", organizer.TokenTV},
	} {
		*f.value = strings.TrimSpace(*f.value)
		format := *f.value
		edited := format != "" && s.changed(f.key, format)
		if edited {
			if m := firstProblem(checkMaxLen(format, f.label, maxNameFormatLen), checkNoControl(format, f.label)); m != "" {
				return m
			}
		}
		if preset != "custom" || (!edited && !toCustom) {
			continue
		}
		if format == "" {
			format, _ = s.Settings.Get(f.key)
		}
		if m := organizer.CheckFolderFormat(format, f.kind); m != "" {
			return m
		}
	}
	if m := checkChoice(req.IllegalCharMode, `Choose "strip" to remove characters that aren't allowed in file names, or "replace" to swap them for another character.`, illegalCharModes...); m != "" {
		return m
	}
	if r := req.IllegalCharReplacement; r != "" && s.changed(settings.KeyIllegalCharReplacement, r) {
		if utf8.RuneCountInString(r) > 3 {
			return "The replacement can be at most 3 characters long, for example - or _."
		}
		if badNameChar.MatchString(r) {
			return "That character isn't allowed in file names either. Try - or _."
		}
	}
	if m := checkChoice(req.ImportConflictPolicy, `Choose what to do when a file with that name already exists: "skip", "overwrite", "overwrite_if_better" or "ask".`, conflictPolicies...); m != "" {
		return m
	}
	// Audio languages: one or several names, best first, and "Original" for the
	// language each title was made in.
	if l := req.QualityLanguage; l != "" {
		names := quality.ParseLanguages(l)
		if len(names) == 0 {
			return "Choose one of the languages in the list."
		}
		for _, n := range names {
			if n != quality.OriginalLanguage && !slices.Contains(parser.LanguageNames(), n) {
				return "Choose one of the languages in the list."
			}
		}
	}
	if l := req.InfoLanguage; l != "" && !metadata.IsInfoLanguage(l) {
		return "Choose one of the languages in the list."
	}
	if req.ReleaseCountry != nil {
		c := strings.ToUpper(strings.TrimSpace(*req.ReleaseCountry))
		if c != "" && !metadata.IsReleaseCountry(c) {
			return "Choose one of the countries in the list."
		}
	}
	if m := checkChoice(req.DefaultSources, `Choose where to download from: "usenet", "torrent" or "both".`, sourcesUsenet, sourcesTorrent, sourcesBoth); m != "" {
		return m
	}

	// Torrents.
	req.TorrentListenPort = strings.TrimSpace(req.TorrentListenPort)
	if p := req.TorrentListenPort; p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 0 || n > 65535 || strings.ContainsAny(p, "+-") {
			return "The torrent port must be a number between 1 and 65535, or 0 to use the default (58264)."
		}
	}
	// Empty is unlimited and 0 is do not seed; both are saved as they are.
	if v := req.TorrentSeedRatioLimit; v != nil {
		*v = strings.TrimSpace(*v)
		if s.changed(settings.KeyTorrentSeedRatioLimit, *v) {
			if m := checkDecimal(*v, "The seed ratio limit", 0, maxSeedRatio); m != "" {
				return m
			}
		}
	}
	if v := req.TorrentSeedTimeLimitH; v != nil {
		*v = strings.TrimSpace(*v)
		if s.changed(settings.KeyTorrentSeedTimeLimitH, *v) {
			if m := checkDecimal(*v, "The seed time limit", 0, maxSeedHours); m != "" {
				return m
			}
		}
	}

	// Numbers.
	if d := req.MonitorIntervalMinutes; d != nil && (*d < 0 || *d > maxMonitorMinutes) {
		return fmt.Sprintf("The check interval must be 0 (off) or a number of minutes, at least %d and at most %d.", minMonitorMinutes, maxMonitorMinutes)
	}
	if d := req.HuntIntervalHours; d != nil && (*d < minHuntHours || *d > maxHuntHours) {
		return fmt.Sprintf("Look for missing items every … hours needs a number from %d to %d.", minHuntHours, maxHuntHours)
	}
	if d := req.ReleaseCheckMinutes; d != nil && (*d < minReleaseCheckMinutes || *d > maxReleaseCheckMinutes) {
		return fmt.Sprintf("Check for new releases every … minutes needs a number from %d to %d.", minReleaseCheckMinutes, maxReleaseCheckMinutes)
	}
	if d := req.HistoryRetentionDays; d != nil && (*d < 0 || *d > maxHistoryDays) {
		return fmt.Sprintf("History retention must be a number of days between 0 and %d. Use 0 to keep everything.", maxHistoryDays)
	}
	if d := req.UsenetRetentionDays; d != nil && (*d < 0 || *d > maxRetentionDays) {
		return fmt.Sprintf("The Usenet retention must be a number of days from 1 to %d, or 0 for no limit.", maxRetentionDays)
	}
	if d := req.DownloadsAtOnce; d != nil && (*d < minDownloadsAtOnce || *d > maxDownloadsAtOnce) {
		return fmt.Sprintf("Downloads at the same time must be a number from %d to %d.", minDownloadsAtOnce, maxDownloadsAtOnce)
	}
	if req.DefaultProfileID < 0 || req.MusicDefaultProfileID < 0 || (req.DefaultTVProfileID != nil && *req.DefaultTVProfileID < 0) {
		return "That profile doesn't exist. Pick one from the list."
	}

	// Subtitles.
	if len(req.SubtitleLanguages) > 0 {
		var cleaned []string
		for _, l := range req.SubtitleLanguages {
			if l = strings.TrimSpace(l); l != "" {
				if !languageCode.MatchString(l) {
					return fmt.Sprintf("%q isn't a language code Mediarium can use. Pick your languages from the list.", l)
				}
				cleaned = append(cleaned, l)
			}
		}
		if len(cleaned) == 0 {
			return "Pick at least one subtitle language from the list."
		}
		if len(cleaned) > maxSubtitleLangs {
			return fmt.Sprintf("Pick at most %d subtitle languages.", maxSubtitleLangs)
		}
		req.SubtitleLanguages = cleaned
	}

	// A subtitle source can't be switched on before it has a key or an add-on.
	if m := s.checkSourceSwitches(req); m != "" {
		return m
	}

	// Legal notice date.
	req.LegalAcknowledgedAt = strings.TrimSpace(req.LegalAcknowledgedAt)
	if v := req.LegalAcknowledgedAt; v != "" && s.changed(settings.KeyLegalAcknowledgedAt, v) {
		if _, err := time.Parse(time.RFC3339, v); err != nil {
			return "That date doesn't look right. It should look like 2026-01-31T09:30:00Z."
		}
	}

	// Service keys and accounts.
	req.TMDBAPIKey = strings.TrimSpace(req.TMDBAPIKey)
	if m := checkAPIKey(req.TMDBAPIKey); m != "" {
		return m
	}
	if req.OpenSubtitlesAPIKey != nil {
		if m := checkAPIKey(*req.OpenSubtitlesAPIKey); m != "" {
			return m
		}
	}
	if req.TraktClientID != nil {
		if m := checkAPIKey(*req.TraktClientID); m != "" {
			return m
		}
	}
	if req.SimklClientID != nil {
		if m := checkAPIKey(*req.SimklClientID); m != "" {
			return m
		}
	}
	if req.OpenSubtitlesUsername != nil {
		u := strings.TrimSpace(*req.OpenSubtitlesUsername)
		if m := firstProblem(checkMaxLen(u, "The OpenSubtitles username", maxServiceAccountLn), checkNoControl(u, "The OpenSubtitles username")); m != "" {
			return m
		}
		if m := firstProblem(checkMaxLen(req.OpenSubtitlesPassword, "The OpenSubtitles password", 200), checkNoControl(req.OpenSubtitlesPassword, "The OpenSubtitles password")); m != "" {
			return m
		}
		// A blank password keeps the saved one, which belongs to the saved
		// username. For a different username it would pair the old password
		// with the new name, so a password is needed.
		if u != "" && req.OpenSubtitlesPassword == "" {
			if saved, _ := s.Settings.Get(settings.KeyOpenSubtitlesUsername); saved != u {
				return "Enter the password for that OpenSubtitles account."
			}
		}
	}

	// Cloudflare helper address.
	if req.FlareSolverrURL != nil {
		v := strings.TrimRight(strings.TrimSpace(*req.FlareSolverrURL), "/")
		if m := firstProblem(
			checkHTTPURL(v, "http://flaresolverr:8191", true),
			checkMaxLen(v, "The Cloudflare helper address", 500),
		); m != "" {
			return m
		}
		*req.FlareSolverrURL = v
	}

	// The address Mediarium is opened at, for links in notifications.
	if req.PublicURL != nil {
		v := strings.TrimRight(strings.TrimSpace(*req.PublicURL), "/")
		if v != "" {
			if m := firstProblem(
				checkHTTPURL(v, "https://mediarium.example.com", true),
				checkMaxLen(v, "The Mediarium address", 500),
			); m != "" {
				return m
			}
		}
		*req.PublicURL = v
	}
	return s.checkSettingsLimits(req)
}

// checkSettingsLimits is the last part of validateSettings: the numbers, hours
// and schedules. It has to run before any value is written, like the rest.
func (s *Server) checkSettingsLimits(req *settingsPayload) string {
	if req.SubDLAPIKey != nil {
		if m := checkAPIKey(strings.TrimSpace(*req.SubDLAPIKey)); m != "" {
			return m
		}
	}
	if req.SpeedLimitMB != nil && (*req.SpeedLimitMB < 0 || *req.SpeedLimitMB > 10000) {
		return "The speed limit is 0 (no limit) to 10000 MB/s."
	}
	if req.SpeedLimitHours != nil {
		if h := strings.TrimSpace(*req.SpeedLimitHours); h != "" {
			if _, _, ok := parseHours(h); !ok {
				return `Give the hours as "from-to", for example "8-23", or leave it empty for all day.`
			}
		}
	}
	if req.SpeedSchedule != nil {
		rules := *req.SpeedSchedule
		if len(rules) > maxSpeedRules {
			return fmt.Sprintf("The speed schedule can have up to %d lines.", maxSpeedRules)
		}
		for i, r := range rules {
			if !r.valid() {
				return fmt.Sprintf("Line %d of the speed schedule isn't right: days are 0 (Sunday) to 6, hours 0 to 23, the limit 0 to 10000 MB/s.", i+1)
			}
		}
	}
	if req.DownloadHours != nil {
		if h := strings.TrimSpace(*req.DownloadHours); h != "" {
			if _, _, ok := parseHours(h); !ok {
				return `Give the download hours as "from-to", for example "1-7", or leave it empty for any time.`
			}
		}
	}
	if req.DigestDay != nil || req.DigestHour != nil {
		day, hour := s.digestWhen()
		if req.DigestDay != nil {
			day = *req.DigestDay
		}
		if req.DigestHour != nil {
			hour = *req.DigestHour
		}
		if day < -1 || day > 6 || hour < 0 || hour > 23 {
			return "The weekly summary day is 0 (Sunday) to 6 (Saturday), or -1 for none, and the hour is 0 to 23."
		}
	}
	if req.NotifyQuietHours != nil {
		if h := strings.TrimSpace(*req.NotifyQuietHours); h != "" {
			if _, _, ok := parseHours(h); !ok {
				return `Give the quiet hours as "from-to", for example "23-7", or leave it empty for none.`
			}
		}
	}
	if req.TorrentStallHours != nil && (*req.TorrentStallHours < -1 || *req.TorrentStallHours > maxStallHours) {
		return "Give up after 1 to 72 hours, or 0 for never."
	}
	if req.MinFreeGB != nil && (*req.MinFreeGB < 0 || *req.MinFreeGB > 10000) {
		return "Keep 0 (off) to 10000 GB free."
	}
	if req.MaxReleaseGB != nil && (*req.MaxReleaseGB < 0 || *req.MaxReleaseGB > 10000) {
		return "Give the largest release as 0 (no limit) to 10000 GB."
	}
	if req.BackupKeep != nil && (*req.BackupKeep < 1 || *req.BackupKeep > 60) {
		return "Keep between 1 and 60 backups."
	}
	if req.TrashDays != nil && (*req.TrashDays < 0 || *req.TrashDays > 365) {
		return "Keep removed files for 0 to 365 days."
	}
	return ""
}
