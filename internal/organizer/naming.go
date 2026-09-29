// Package organizer post-processes completed downloads: naming/renaming,
// unpacking, PAR2 verification/repair, and hardlink-first import into the
// organized library.
package organizer

import (
	"fmt"
	"regexp"
	"strings"
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

var TVPresets = map[string]string{
	"plex":     "{Series Title} - S{Season:00}E{Episode:00} - {Episode Title}",
	"jellyfin": "{Series Title} - S{Season:00}E{Episode:00} - {Episode Title} [{Quality}]",
	"kodi":     "{Series Title} - S{Season:00}E{Episode:00} - {Episode Title} [{Quality} {Source}]",
	"minimal":  "S{Season:00}E{Episode:00}",
}

var tokenRe = regexp.MustCompile(`\{([^{}:]+)(?::([^{}]+))?\}`)

// Render expands a naming-token format string against ctx, e.g.
// "{Movie Title} ({Year}) [{Quality}] {Custom Formats}-{Release Group}".
// An unrecognized token is left as an empty string rather than erroring,
// since presets and any future custom tokens must degrade gracefully
// rather than break renaming entirely.
func Render(format string, ctx NamingContext) string {
	result := tokenRe.ReplaceAllStringFunc(format, func(match string) string {
		groups := tokenRe.FindStringSubmatch(match)
		name, pad := groups[1], groups[2]
		return applyPad(tokenValue(name, ctx), pad)
	})
	return collapseWhitespaceAndPunctuation(result)
}

// tokenValue matches token names case-insensitively — users may write
// "{season:00}" while the presets use "{Season:00}", and a user hand-
// typing a custom format shouldn't have to guess which.
func tokenValue(name string, ctx NamingContext) string {
	switch strings.ToLower(name) {
	case "movie title":
		return ctx.MovieTitle
	case "series title":
		return ctx.SeriesTitle
	case "episode title":
		return ctx.EpisodeTitle
	case "year":
		if ctx.Year == 0 {
			return ""
		}
		return fmt.Sprintf("%d", ctx.Year)
	case "season":
		return fmt.Sprintf("%d", ctx.Season)
	case "episode":
		return fmt.Sprintf("%d", ctx.Episode)
	case "quality":
		return ctx.Quality
	case "source":
		return ctx.Source
	case "codec":
		return ctx.Codec
	case "release group":
		return ctx.ReleaseGroup
	case "tmdb id":
		if ctx.TMDBID == 0 {
			return ""
		}
		return fmt.Sprintf("%d", ctx.TMDBID)
	case "custom formats":
		// No full custom-format matching engine yet (that's a Phase 2/3
		// quality-profiles feature) — approximated for now as
		// whatever edition/HDR-style tag is known, so the token still
		// resolves to something meaningful rather than silently vanishing.
		return ctx.Edition
	default:
		return ""
	}
}

// applyPad implements the "{Token:00}" zero-pad convention (e.g.
// "Season {season:00}") for numeric token values.
func applyPad(value, pad string) string {
	if pad == "" || value == "" {
		return value
	}
	width := len(pad)
	for len(value) < width {
		value = "0" + value
	}
	return value
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

// Sanitize removes or replaces characters illegal in filenames on the host
// OS. replacement is only used in SanitizeReplace mode.
func Sanitize(name string, mode SanitizeMode, replacement string) string {
	switch mode {
	case SanitizeReplace:
		return illegalChars.ReplaceAllString(name, replacement)
	default:
		return illegalChars.ReplaceAllString(name, "")
	}
}
