// Package organizer post-processes completed downloads: naming/renaming,
// unpacking, PAR2 verification/repair, and hardlink-first import into the
// organized library.
package organizer

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// NamingContext supplies token values for Render. Season/Episode exist now
// (zero for movies) so the same engine serves Phase 2 TV naming without a
// rework.
type NamingContext struct {
	MovieTitle   string
	Year         int
	Quality      string // resolution, e.g. "1080p"
	Source       string // WEB-DL, BluRay, Remux, ...
	Codec        string
	Edition      string
	ReleaseGroup string
	TMDBID       int

	SeriesTitle  string
	EpisodeTitle string
	Season       int
	Episode      int
	// MoreEpisodes are the other episodes of the same season in the same file
	// (S01E01E02), in order: {Episode} then reads "01-E02".
	MoreEpisodes []int
	// Absolute is the episode's number counted from the show's first episode
	// (anime are released by it), 0 when not known.
	Absolute int
	AirDate  string // YYYY-MM-DD, empty when unknown

	// From the release name, for the Radarr/Sonarr-style tokens (tokens.go).
	AudioCodec string
	HDR        string // HDR10, HDR10+, DV
	Is3D       bool
	Proper     bool
	Repack     bool
	Languages  []string
	IMDBID     string
	TVDBID     int

	// OriginalFilename is the name the file came with, without its folder
	// or extension; OriginalTitle is the release's own name.
	OriginalFilename string
	OriginalTitle    string

	editionTagged bool // set by Render when the format has {Edition Tags}
}

// Presets ship a few common naming schemes so most users never need to
// hand-build a token string.
var Presets = map[string]string{
	"plex":     "{Movie Title} ({Year})",
	"jellyfin": "{Movie Title} ({Year}) [{Quality}]",
	"kodi":     "{Movie Title} ({Year}) [{Quality} {Source}]",
	"minimal":  "{Movie Title}",
}

// TV naming ("Season/episode folder support with the same token
// system"). The series/season folder structure is fixed (Plex/Jellyfin/
// Kodi/Emby all agree on it); only the episode filename is configurable,
// via the same preset names movies use so one "naming preset" setting
// covers both. Sonarr-style series folders often carry a TVDB id
// ("[tvdb-{TvdbId}]"); this app's metadata source is TMDB, so
// there's no TVDB id to put there — the year disambiguates same-titled
// shows well enough.
const (
	TVSeriesFolder = "{Series Title} ({Year})"
	TVSeasonFolder = "Season {Season:00}"
)

// AnimeFormat is the suggested file name of an anime episode: the season and
// episode for the media servers, and the number from the first episode the
// way anime releases count.
const AnimeFormat = "{Series Title} - S{Season:00}E{Episode:00} - {Absolute:000} - {Episode Title} [{Quality}]"

var TVPresets = map[string]string{
	"plex":     "{Series Title} - S{Season:00}E{Episode:00} - {Episode Title}",
	"jellyfin": "{Series Title} - S{Season:00}E{Episode:00} - {Episode Title} [{Quality}]",
	"kodi":     "{Series Title} - S{Season:00}E{Episode:00} - {Episode Title} [{Quality} {Source}]",
	"minimal":  "S{Season:00}E{Episode:00}",
}

// maxPadWidth caps "{Token:000...}": no name needs more, and a format with a
// million zeros must not stall the renamer.
const maxPadWidth = 32

// applyPad implements the "{Token:00}" zero-pad convention (e.g.
// "Season {season:00}") for numeric token values.
func applyPad(value, pad string) string {
	if pad == "" || value == "" {
		return value
	}
	width := min(len(pad), maxPadWidth)
	if len(value) >= width {
		return value
	}
	return strings.Repeat("0", width-len(value)) + value
}

var (
	multiSpace = regexp.MustCompile(` {2,}`)
	// An empty {Episode Title} leaves "Show - S01E02 - " behind.
	trailingDash = regexp.MustCompile(`\s+-+\s*$`)
	emptyParen   = regexp.MustCompile(`\(\s*\)`)
	emptyBrack   = regexp.MustCompile(`\[\s*\]`)
)

// collapseWhitespaceAndPunctuation cleans up artifacts left behind when a
// token resolves to "" (e.g. "{Movie Title} ()" when Year is unknown).
func collapseWhitespaceAndPunctuation(s string) string {
	s = emptyParen.ReplaceAllString(s, "")
	s = emptyBrack.ReplaceAllString(s, "")
	s = multiSpace.ReplaceAllString(s, " ")
	// An empty token between two " - " leaves "Title - - Extended".
	for strings.Contains(s, " - - ") {
		s = strings.ReplaceAll(s, " - - ", " - ")
	}
	s = strings.TrimSpace(s)
	return trailingDash.ReplaceAllString(s, "")
}

var illegalChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// SanitizeMode controls how illegal filesystem characters are handled
// ("configurable (replace vs. strip)").
type SanitizeMode int

const (
	SanitizeStrip SanitizeMode = iota
	SanitizeReplace
)

// MaxNameBytes is the longest name Sanitize returns. File systems allow 255
// bytes in one name; the room left over is for the extension the caller adds
// and for the temporary suffix used while a file is being replaced.
const MaxNameBytes = 230

// windowsReserved are the device names Windows (and SMB shares served to
// it) refuse as a file name, with or without an extension. Windows also
// counts the superscript digits ¹ ² ³ as digits here.
var windowsReserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true, "CONIN$": true, "CONOUT$": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"COM¹": true, "COM²": true, "COM³": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
	"LPT¹": true, "LPT²": true, "LPT³": true,
}

// Sanitize removes or replaces characters illegal in filenames on the host
// OS. replacement is only used in SanitizeReplace mode. The result can never
// climb out of the folder it is joined onto: path separators never survive
// (not even inside the replacement text), and a name made only of dots and
// spaces (".." would be the parent folder) becomes "_". The result is valid
// UTF-8, has no leading or trailing dots or spaces (a leading dot hides the
// file or folder; Windows and SMB shares drop trailing ones), does not start
// a Windows device name ("CON", "NUL"...) and is at most MaxNameBytes long.
// An empty name stays empty: the caller decides what an unnamed title is called.
func Sanitize(name string, mode SanitizeMode, replacement string) string {
	name = strings.ToValidUTF8(name, "")
	var out string
	switch mode {
	case SanitizeReplace:
		// Literal: "$1" in a replacement is text, not a group reference.
		out = illegalChars.ReplaceAllLiteralString(name, illegalChars.ReplaceAllString(strings.ToValidUTF8(replacement, ""), ""))
	default:
		out = illegalChars.ReplaceAllString(name, "")
	}
	out = strings.TrimLeft(out, ". ")
	// A device name is fixed before the cut: it adds a byte, and the cut
	// only ever removes from the end, so the answer is the same the second time.
	stem := out
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	if windowsReserved[strings.ToUpper(strings.TrimRight(stem, " "))] {
		out = stem + "_" + out[len(stem):]
	}
	out = strings.TrimRight(truncateName(out, MaxNameBytes), ". ")
	if out == "" && name != "" {
		return "_"
	}
	return out
}

// OriginalName is the name a file keeps under "Keep the original file names":
// its own name without the folder, with the characters the file system
// refuses handled like in every other name. ok is false when that name is no
// use (it looks obfuscated, or nothing is left of it), and the caller then
// names the file the normal way.
func OriginalName(file string, mode SanitizeMode, replacement string) (name string, ok bool) {
	base := filepath.Base(file)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if LooksObfuscated(stem) {
		return "", false
	}
	clean := Sanitize(stem, mode, replacement)
	if clean == "" || clean == "_" || LooksObfuscated(clean) {
		return "", false
	}
	return clean + illegalChars.ReplaceAllString(ext, ""), true
}

// ReleaseFileName is the name a video takes from the release it came in, for
// when the file's own name is random (an obfuscated Usenet post): the release
// title, with the video's real extension and the same character handling as
// every other name. ok is false when the release title is no use either (it
// looks random, or nothing is left of it), and the caller then names the file
// by the naming setting.
func ReleaseFileName(release, videoFile string, mode SanitizeMode, replacement string) (name string, ok bool) {
	stem := strings.TrimSpace(release)
	// A title that still ends in a video extension ("Show.S01E02-GRP.mkv")
	// would get it twice.
	if IsVideoFile(stem) {
		stem = strings.TrimSuffix(stem, filepath.Ext(stem))
	}
	if stem == "" || LooksObfuscated(stem) {
		return "", false
	}
	clean := Sanitize(stem, mode, replacement)
	if clean == "" || clean == "_" || LooksObfuscated(clean) {
		return "", false
	}
	return clean + illegalChars.ReplaceAllString(filepath.Ext(videoFile), ""), true
}

// LooksObfuscated reports whether a file name (without its extension) is a
// random string rather than a real name, the way obfuscated Usenet posts
// come: "a8f7d6e5c4b3", "wFvDJHhDrbESDBLq", a hash, or too short to say
// anything ("abc"). A name with words in it is never flagged, and neither is
// a plain number like "1917".
func LooksObfuscated(stem string) bool {
	s := strings.TrimSpace(stem)
	var letters, digits, upperRun, longestUpperRun int
	hexOnly, lower := true, false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r):
			letters++
			if unicode.IsUpper(r) {
				upperRun++
				longestUpperRun = max(longestUpperRun, upperRun)
			} else {
				upperRun = 0
				lower = true
			}
			if !strings.ContainsRune("abcdefABCDEF", r) {
				hexOnly = false
			}
		case unicode.IsDigit(r):
			digits++
			upperRun = 0
		default:
			upperRun = 0
			if r != '-' {
				hexOnly = false
			}
		}
	}
	alnum := letters + digits
	if alnum < 4 {
		return true // "abc", "01", "-.-"
	}
	if hexOnly && letters > 0 && digits > 0 && alnum >= 16 {
		return true // a hash or a UUID
	}
	if strings.ContainsAny(s, " ._-()[]") {
		return false // words: a real name
	}
	switch {
	case letters == 0:
		return digits >= 8 // a long number; "1917" is a film
	case digits > 0 && alnum >= 8:
		return true // letters and digits mixed: "a8f7d6e5c4b3"
	case alnum >= 12 && lower && longestUpperRun >= 3:
		return true // random case: "wFvDJHhDrbESDBLq"
	case alnum >= 24:
		return true
	}
	return false
}

// truncateName cuts s to at most max bytes without splitting a character.
func truncateName(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
