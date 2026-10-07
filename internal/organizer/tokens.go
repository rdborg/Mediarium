package organizer

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Naming tokens. The names follow Radarr's and Sonarr's, so a format copied
// from either works here. A token may carry text around its name inside the
// braces; that text is only written when the token has a value:
//
//	{ - Edition Tags}  → " - Extended", or nothing when there is no edition
//	{[Quality Full]}   → "[Bluray-1080p]"
//	{edition-{Edition Tags}} → "{edition-Extended}" (Plex's edition folder tag)
//
// A dot or underscore between the words ({Movie.CleanTitle}) puts that
// character between the words of the value too. Token names are not case
// sensitive. A known token with nothing to say (an IMDb id Mediarium doesn't
// have) simply disappears.

// TokenKind says which formats a token belongs in.
type TokenKind string

const (
	TokenMovie TokenKind = "movie"
	TokenTV    TokenKind = "tv"
	TokenBoth  TokenKind = "both"
)

// TokenInfo describes one token for the token list in Settings.
type TokenInfo struct {
	Token string    `json:"token"` // as written in a format, e.g. "{Movie CleanTitle}"
	Kind  TokenKind `json:"kind"`
	Group string    `json:"group"`
}

type tokenDef struct {
	name  string // canonical, as shown
	kind  TokenKind
	group string
	value func(NamingContext) string
}

var tokenDefs = []tokenDef{
	{"Movie Title", TokenMovie, "Movie", func(c NamingContext) string { return c.MovieTitle }},
	{"Movie CleanTitle", TokenMovie, "Movie", func(c NamingContext) string { return cleanTitle(c.MovieTitle) }},
	{"Movie TitleThe", TokenMovie, "Movie", func(c NamingContext) string { return titleThe(c.MovieTitle) }},
	{"Movie CleanTitleThe", TokenMovie, "Movie", func(c NamingContext) string { return cleanTitle(titleThe(c.MovieTitle)) }},
	{"Movie TitleFirstCharacter", TokenMovie, "Movie", func(c NamingContext) string { return firstCharacter(c.MovieTitle) }},
	{"Release Year", TokenBoth, "Movie", func(c NamingContext) string { return yearText(c.Year) }},
	{"Movie Year", TokenMovie, "Movie", func(c NamingContext) string { return yearText(c.Year) }},
	{"Year", TokenBoth, "Movie", func(c NamingContext) string { return yearText(c.Year) }},
	{"Edition Tags", TokenMovie, "Movie", func(c NamingContext) string { return c.Edition }},
	{"TmdbId", TokenBoth, "Ids", func(c NamingContext) string { return idText(c.TMDBID) }},
	{"ImdbId", TokenBoth, "Ids", func(c NamingContext) string { return c.IMDBID }},
	{"TvdbId", TokenTV, "Ids", func(c NamingContext) string { return idText(c.TVDBID) }},

	{"Series Title", TokenTV, "Show", func(c NamingContext) string { return c.SeriesTitle }},
	{"Series CleanTitle", TokenTV, "Show", func(c NamingContext) string { return cleanTitle(c.SeriesTitle) }},
	{"Series TitleYear", TokenTV, "Show", func(c NamingContext) string { return withYear(c.SeriesTitle, c.Year) }},
	{"Series CleanTitleYear", TokenTV, "Show", func(c NamingContext) string { return withYear(cleanTitle(c.SeriesTitle), c.Year) }},
	{"Series TitleThe", TokenTV, "Show", func(c NamingContext) string { return titleThe(c.SeriesTitle) }},
	{"Season", TokenTV, "Episode", func(c NamingContext) string { return fmt.Sprintf("%d", c.Season) }},
	{"Episode", TokenTV, "Episode", func(c NamingContext) string { return fmt.Sprintf("%d", c.Episode) }},
	{"Episode Title", TokenTV, "Episode", func(c NamingContext) string { return c.EpisodeTitle }},
	{"Episode CleanTitle", TokenTV, "Episode", func(c NamingContext) string { return cleanTitle(c.EpisodeTitle) }},
	{"Air-Date", TokenTV, "Episode", func(c NamingContext) string { return c.AirDate }},

	{"Quality Full", TokenBoth, "Quality", qualityFull},
	{"Quality Title", TokenBoth, "Quality", qualityTitle},
	{"Quality Proper", TokenBoth, "Quality", func(c NamingContext) string { return properText(c) }},
	{"Quality", TokenBoth, "Quality", func(c NamingContext) string { return c.Quality }},
	{"Source", TokenBoth, "Quality", func(c NamingContext) string { return c.Source }},

	{"MediaInfo Simple", TokenBoth, "Media info", func(c NamingContext) string { return joinNonEmpty(" ", c.Codec, c.AudioCodec) }},
	{"MediaInfo Full", TokenBoth, "Media info", func(c NamingContext) string { return joinNonEmpty(" ", c.Codec, c.AudioCodec) }},
	{"MediaInfo VideoCodec", TokenBoth, "Media info", func(c NamingContext) string { return c.Codec }},
	{"MediaInfo AudioCodec", TokenBoth, "Media info", func(c NamingContext) string { return c.AudioCodec }},
	{"MediaInfo AudioChannels", TokenBoth, "Media info", func(c NamingContext) string { return "" }},
	{"MediaInfo AudioLanguages", TokenBoth, "Media info", func(c NamingContext) string { return strings.Join(c.Languages, "+") }},
	{"MediaInfo SubtitleLanguages", TokenBoth, "Media info", func(c NamingContext) string { return "" }},
	{"MediaInfo VideoDynamicRange", TokenBoth, "Media info", func(c NamingContext) string {
		if c.HDR != "" {
			return "HDR"
		}
		return ""
	}},
	{"MediaInfo VideoDynamicRangeType", TokenBoth, "Media info", func(c NamingContext) string { return c.HDR }},
	{"MediaInfo VideoBitDepth", TokenBoth, "Media info", func(c NamingContext) string { return "" }},
	{"MediaInfo 3D", TokenBoth, "Media info", func(c NamingContext) string {
		if c.Is3D {
			return "3D"
		}
		return ""
	}},
	{"Codec", TokenBoth, "Media info", func(c NamingContext) string { return c.Codec }},

	{"Release Group", TokenBoth, "Release", func(c NamingContext) string { return c.ReleaseGroup }},
	// No custom formats are matched yet, so this stands in for the edition,
	// unless the format has {Edition Tags} for that already.
	{"Custom Formats", TokenBoth, "Release", func(c NamingContext) string {
		if c.editionTagged {
			return ""
		}
		return c.Edition
	}},
	{"Preferred Words", TokenTV, "Release", func(c NamingContext) string { return "" }},
}

// aliases are other spellings people use for the same tokens.
var aliases = map[string]string{
	"tmdb id": "tmdbid", "imdb id": "imdbid", "tvdb id": "tvdbid",
	"air date": "air-date", "releasegroup": "release group",
}

var tokensByKey = func() map[string]tokenDef {
	m := map[string]tokenDef{}
	for _, d := range tokenDefs {
		m[strings.ToLower(d.name)] = d
	}
	return m
}()

// Tokens lists every token, in the order Settings shows them.
func Tokens() []TokenInfo {
	out := make([]TokenInfo, 0, len(tokenDefs))
	for _, d := range tokenDefs {
		out = append(out, TokenInfo{Token: "{" + d.name + "}", Kind: d.kind, Group: d.group})
	}
	return out
}

// tokenPart is one {…} in a format, taken apart.
type tokenPart struct {
	prefix, suffix string
	def            tokenDef
	sep            string // " " (normal), "." or "_"
	pad            string // "00" in {Season:00}
	known          bool
	raw            string // the name as written
}

func parseToken(inner string) tokenPart {
	body, pad := inner, ""
	if i := strings.LastIndex(inner, ":"); i >= 0 {
		body, pad = inner[:i], inner[i+1:]
	}
	start := strings.IndexFunc(body, unicode.IsLetter)
	if start < 0 {
		return tokenPart{raw: inner}
	}
	end := strings.LastIndexFunc(body, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) })
	p := tokenPart{prefix: body[:start], suffix: body[end+1:], pad: pad, raw: body[start : end+1], sep: " "}
	name := p.raw
	switch {
	case strings.Contains(name, "."):
		p.sep, name = ".", strings.ReplaceAll(name, ".", " ")
	case strings.Contains(name, "_"):
		p.sep, name = "_", strings.ReplaceAll(name, "_", " ")
	}
	key := strings.ToLower(strings.Join(strings.Fields(name), " "))
	if a, ok := aliases[key]; ok {
		key = a
	}
	p.def, p.known = tokensByKey[key]
	return p
}

func (p tokenPart) render(ctx NamingContext) string {
	if !p.known {
		return ""
	}
	v := applyPad(p.def.value(ctx), p.pad)
	if v == "" {
		return ""
	}
	if p.sep != " " {
		v = strings.Join(strings.Fields(v), p.sep)
	}
	return p.prefix + v + p.suffix
}

var (
	// {prefix{Token}suffix}: Plex's {edition-…} and {imdb-…} tags keep their braces.
	nestedRe = regexp.MustCompile(`\{([^{}]*)\{([^{}]+)\}([^{}]*)\}`)
	tokenRe  = regexp.MustCompile(`\{([^{}]+)\}`)
)

// Render expands a naming format against ctx, e.g.
// "{Movie CleanTitle} ({Release Year}){ - Edition Tags} [{Quality Full}]".
// An unknown token renders as nothing rather than failing: CheckFormat keeps
// unknown tokens out of saved formats.
func Render(format string, ctx NamingContext) string {
	for _, m := range tokenRe.FindAllStringSubmatch(format, -1) {
		if p := parseToken(m[1]); p.known && p.def.name == "Edition Tags" {
			ctx.editionTagged = true
		}
	}
	result := nestedRe.ReplaceAllStringFunc(format, func(m string) string {
		g := nestedRe.FindStringSubmatch(m)
		v := parseToken(g[2]).render(ctx)
		if v == "" {
			return ""
		}
		return "\x00" + g[1] + v + g[3] + "\x01" // braces put back after the next pass
	})
	result = tokenRe.ReplaceAllStringFunc(result, func(m string) string {
		return parseToken(m[1 : len(m)-1]).render(ctx)
	})
	result = strings.NewReplacer("\x00", "{", "\x01", "}").Replace(result)
	return collapseWhitespaceAndPunctuation(result)
}

// CheckFormat checks a custom file name format before it is saved and says
// what's wrong in a sentence, or "" when it's fine. kind is TokenMovie or
// TokenTV.
func CheckFormat(format string, kind TokenKind) string {
	v := strings.TrimSpace(format)
	if v == "" {
		return ""
	}
	if strings.ContainsAny(v, `/\`) {
		return `A file name can't contain / or \. The folders are chosen for you, so only write the name of the file itself.`
	}
	if strings.Count(v, "{") != strings.Count(v, "}") {
		return "A { or } is missing. Each token is wrapped in curly braces, like {Movie Title}."
	}
	var names []string
	check := func(inner string) string {
		p := parseToken(inner)
		if !p.known {
			return fmt.Sprintf("{%s} isn't a token Mediarium knows. Pick one from the token list next to the box.", strings.TrimSpace(inner))
		}
		if p.pad != "" && strings.Trim(p.pad, "0") != "" {
			return fmt.Sprintf(`After the colon, use zeros to set padding, like {Season:00}. "%s" won't work.`, p.pad)
		}
		if p.def.kind != TokenBoth && p.def.kind != kind {
			if kind == TokenMovie {
				return fmt.Sprintf("{%s} is for episodes, not movies.", p.def.name)
			}
			return fmt.Sprintf("{%s} is for movies, not episodes.", p.def.name)
		}
		names = append(names, strings.ToLower(p.def.name))
		return ""
	}
	rest := v
	for _, m := range nestedRe.FindAllStringSubmatch(v, -1) {
		if msg := check(m[2]); msg != "" {
			return msg
		}
	}
	rest = nestedRe.ReplaceAllString(rest, "")
	for _, m := range tokenRe.FindAllStringSubmatch(rest, -1) {
		if msg := check(m[1]); msg != "" {
			return msg
		}
	}
	has := func(want ...string) bool {
		for _, n := range names {
			for _, w := range want {
				if n == w {
					return true
				}
			}
		}
		return false
	}
	if kind == TokenMovie && !has("movie title", "movie cleantitle", "movie titlethe", "movie cleantitlethe") {
		return "Include the movie's title, for example {Movie Title} or {Movie CleanTitle}, so every file says which movie it is."
	}
	if kind == TokenTV && (!has("season") || !has("episode")) && !has("air-date") {
		return "Include {Season} and {Episode} (for example S{Season:00}E{Episode:00}) so two episodes never get the same name."
	}
	return ""
}

// Scheme is a ready-made format to start from in Settings.
type Scheme struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Movie   string `json:"movie"`
	Episode string `json:"episode"`
}

// Schemes are starting points for a custom format; each can be changed.
var Schemes = []Scheme{
	{"plex", "Plex", Presets["plex"], TVPresets["plex"]},
	{"jellyfin", "Jellyfin / Emby", Presets["jellyfin"], TVPresets["jellyfin"]},
	{"kodi", "Kodi", Presets["kodi"], TVPresets["kodi"]},
	{"detailed", "Detailed, like Radarr and Sonarr",
		"{Movie CleanTitle} ({Release Year}){ - Edition Tags} [{Quality Full}]{[MediaInfo VideoDynamicRangeType]}{[MediaInfo AudioCodec]}{[MediaInfo VideoCodec]}{-Release Group}",
		"{Series TitleYear} - S{Season:00}E{Episode:00} - {Episode CleanTitle} [{Quality Full}]{[MediaInfo VideoDynamicRangeType]}{[MediaInfo AudioCodec]}{[MediaInfo VideoCodec]}{-Release Group}"},
	{"plex-editions", "Plex with editions",
		"{Movie CleanTitle} ({Release Year}) {edition-{Edition Tags}} [{Quality Full}]{-Release Group}",
		"{Series TitleYear} - S{Season:00}E{Episode:00} - {Episode CleanTitle} [{Quality Full}]{-Release Group}"},
}

// --- values ---

func yearText(y int) string {
	if y == 0 {
		return ""
	}
	return fmt.Sprintf("%d", y)
}

func idText(id int) string {
	if id == 0 {
		return ""
	}
	return fmt.Sprintf("%d", id)
}

func withYear(title string, year int) string {
	if title == "" || year == 0 {
		return title
	}
	return fmt.Sprintf("%s (%d)", title, year)
}

func joinNonEmpty(sep string, parts ...string) string {
	var keep []string
	for _, p := range parts {
		if p != "" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, sep)
}

var cleanDrop = regexp.MustCompile(`[^\p{L}\p{N}\s.\-]`)

// cleanTitle is Radarr's CleanTitle: "&" becomes "and", punctuation that
// file systems or players stumble over goes, spaces are tidied.
func cleanTitle(s string) string {
	s = strings.ReplaceAll(s, "&", "and")
	s = cleanDrop.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(s), " ")
}

// titleThe moves a leading article to the end: "The Matrix" → "Matrix, The".
func titleThe(s string) string {
	for _, a := range []string{"The ", "A ", "An "} {
		if len(s) > len(a) && strings.EqualFold(s[:len(a)], a) {
			return s[len(a):] + ", " + strings.TrimSpace(s[:len(a)])
		}
	}
	return s
}

func firstCharacter(s string) string {
	t := cleanTitle(titleThe(s))
	for _, r := range t {
		if unicode.IsLetter(r) {
			return string(unicode.ToUpper(r))
		}
		if unicode.IsDigit(r) {
			return "0-9"
		}
	}
	return ""
}

// qualityTitle is Radarr's quality name: "Bluray-1080p", "WEBDL-2160p".
func qualityTitle(c NamingContext) string {
	src := map[string]string{"bluray": "Bluray", "web-dl": "WEBDL", "webdl": "WEBDL", "webrip": "WEBRip", "remux": "Remux", "hdtv": "HDTV", "dvdrip": "DVD", "dvd": "DVD"}[strings.ToLower(c.Source)]
	if src == "" {
		src = c.Source
	}
	return joinNonEmpty("-", src, c.Quality)
}

func properText(c NamingContext) string {
	switch {
	case c.Repack:
		return "Repack"
	case c.Proper:
		return "Proper"
	}
	return ""
}

func qualityFull(c NamingContext) string { return joinNonEmpty(" ", qualityTitle(c), properText(c)) }
