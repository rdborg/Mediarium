package api

import (
	"fmt"
	"github.com/rdborg/mediarium/internal/organizer"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/settings"
)

// Limits for what Settings accepts.
const (
	maxPathLen          = 4096
	maxNameFormatLen    = 200
	maxHistoryDays      = 36500
	maxMonitorMinutes   = 10080 // a week
	maxSeedRatio        = 1000
	maxSeedHours        = 87600 // ten years
	maxSubtitleLangs    = 30
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

// checkNamingFormat checks a custom movie file name format; the rules live
// with the naming engine (organizer.CheckFormat), which the Settings page's
// preview uses too.
func checkNamingFormat(v string) string { return organizer.CheckFormat(v, organizer.TokenMovie) }

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
	for _, f := range []struct {
		key, value, label, example string
	}{
		{settings.KeyMoviesPath, req.MoviesPath, "The movies folder", "/media/movies"},
		{settings.KeyTVPath, req.TVPath, "The TV folder", "/media/tv"},
		{settings.KeyDownloadsPath, req.DownloadsPath, "The downloads folder", "/downloads"},
		{settings.KeyMusicPath, req.MusicPath, "The music folder", "/media/music"},
		{settings.KeyEbooksPath, req.EbooksPath, "The ebooks folder", "/media/ebooks"},
		{settings.KeyAudiobooksPath, req.AudiobooksPath, "The audiobooks folder", "/media/audiobooks"},
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
	req.MovieNameFormat = strings.TrimSpace(req.MovieNameFormat)
	if req.MovieNameFormat != "" && s.changed(settings.KeyMovieNameFormat, req.MovieNameFormat) {
		preset := req.NamingPreset
		if preset == "" {
			preset, _ = s.Settings.Get(settings.KeyNamingPreset)
		}
		if m := firstProblem(checkMaxLen(req.MovieNameFormat, "The file name format", maxNameFormatLen), checkNoControl(req.MovieNameFormat, "The file name format")); m != "" {
			return m
		}
		// The full token check only matters once the format is the one in use;
		// while another style is picked the box is just a saved draft.
		if preset == "custom" {
			if m := checkNamingFormat(req.MovieNameFormat); m != "" {
				return m
			}
		}
	}
	req.EpisodeNameFormat = strings.TrimSpace(req.EpisodeNameFormat)
	if req.EpisodeNameFormat != "" && s.changed(settings.KeyEpisodeNameFormat, req.EpisodeNameFormat) {
		if m := firstProblem(checkMaxLen(req.EpisodeNameFormat, "The episode file name format", maxNameFormatLen), checkNoControl(req.EpisodeNameFormat, "The episode file name format")); m != "" {
			return m
		}
		preset := req.NamingPreset
		if preset == "" {
			preset, _ = s.Settings.Get(settings.KeyNamingPreset)
		}
		if preset == "custom" {
			if m := organizer.CheckFormat(req.EpisodeNameFormat, organizer.TokenTV); m != "" {
				return m
			}
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
	if l := req.QualityLanguage; l != "" && !slices.Contains(parser.LanguageNames(), l) {
		return "Choose one of the languages in the list."
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
	req.TorrentSeedRatioLimit = strings.TrimSpace(req.TorrentSeedRatioLimit)
	req.TorrentSeedTimeLimitH = strings.TrimSpace(req.TorrentSeedTimeLimitH)
	if s.changed(settings.KeyTorrentSeedRatioLimit, req.TorrentSeedRatioLimit) {
		if m := checkDecimal(req.TorrentSeedRatioLimit, "The seed ratio limit", 0, maxSeedRatio); m != "" {
			return m
		}
	}
	if s.changed(settings.KeyTorrentSeedTimeLimitH, req.TorrentSeedTimeLimitH) {
		if m := checkDecimal(req.TorrentSeedTimeLimitH, "The seed time limit", 0, maxSeedHours); m != "" {
			return m
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
	if d := req.DownloadsAtOnce; d != nil && (*d < minDownloadsAtOnce || *d > maxDownloadsAtOnce) {
		return fmt.Sprintf("Downloads at the same time must be a number from %d to %d.", minDownloadsAtOnce, maxDownloadsAtOnce)
	}
	if req.DefaultProfileID < 0 || req.MusicDefaultProfileID < 0 {
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
	return ""
}
