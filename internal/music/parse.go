package music

import (
	"regexp"
	"strconv"
	"strings"
)

// Format is an audio file format named in a release title.
type Format string

const (
	FormatFLAC Format = "FLAC"
	FormatALAC Format = "ALAC"
	FormatMP3  Format = "MP3"
	FormatAAC  Format = "AAC"
)

// Release is what a music release title says about itself.
type Release struct {
	Artist      string
	Album       string // for a discography, what the title calls it ("Discography")
	Year        int    // 0 if none (or a range, as in a discography)
	Format      Format // "" if not named
	Bitrate     string // "320", "256", "192", "V0", "V2", ... ; "" if not named
	BitDepth    int    // 24 or 16; 0 if not named
	Source      string // "WEB", "CD", "Vinyl"; "" if not named
	Discography bool   // a discography, collection, anthology or box set rather than one album
	Group       string // scene release group, when the title has one
}

// Tier is the quality tier this release would be.
func (r Release) Tier() Tier { return Classify(r) }

var (
	reYear      = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
	reYearRange = regexp.MustCompile(`\b(?:19|20)\d{2}\s*[-–~]\s*(?:19|20)\d{2}\b`)
	reFormat    = regexp.MustCompile(`(?i)\b(flac|alac|mp3|aac|m4a)\b`)
	reBitrateK  = regexp.MustCompile(`(?i)\b(320|256|224|192|160|128)\s?(?:k|kbps|kbit|kbits|kb|kbs)\b`)
	reBitrate   = regexp.MustCompile(`\b(320|256|192)\b`)
	reVBR       = regexp.MustCompile(`(?i)\b(v0|v1|v2)\b`)
	reBitDepth  = regexp.MustCompile(`(?i)\b(24|16)\s?-?\s?bits?\b|\b(24|16)[-/ ](?:44|48|88|96|176|192)(?:[.,]\d)?\s?(?:khz)?\b`)
	reHiRes     = regexp.MustCompile(`(?i)\bhi[- ]?res\b`)
	reWeb       = regexp.MustCompile(`(?i)\bweb(?:[- ]?(?:dl|flac|rip))?\b`)
	reVinyl     = regexp.MustCompile(`(?i)\b(?:vinyl|vinylrip|lp[- ]?rip)\b`)
	reCD        = regexp.MustCompile(`(?i)\b(?:\d?cdr?|cdda|cd[- ]?rip)\b`)
	reDiscog    = regexp.MustCompile(`(?i)\b(?:discography|discografia|diskografie|collection|anthology|complete (?:albums|discography|studio albums)|box ?set)\b`)
	reBracket   = regexp.MustCompile(`[(\[{]([^)\]}]*)[)\]}]`)
	reLeading   = regexp.MustCompile(`^[(\[]([^)\]]*)[)\]]\s*`)
	reGroup     = regexp.MustCompile(`^[A-Za-z0-9]+$`)
	reCatalog   = regexp.MustCompile(`^\(?[A-Z]{2,}[ -]?\d{2,}[A-Z]?\)?$`)
	reSpaces    = regexp.MustCompile(`\s+`)
	reExt       = regexp.MustCompile(`(?i)\.(nzb|torrent)$`)
)

// ParseRelease reads a music release title: the artist, the album, the
// year, and what the title says about the audio (format, bitrate, bit
// depth, source). It understands the common shapes:
//
//	Artist - Album (2020) [FLAC 24bit-96kHz]          (P2P, with spaces)
//	Artist_Name-Album_Title-(CAT001)-WEB-2020-GROUP   (scene, no spaces)
//	Artist.Name-Album.Title-24BIT-WEB-FLAC-2021-GRP   (scene with dots)
//	Artist - Discography (1990-2020) [MP3 320]        (a discography)
//
// Anything it cannot make out is left empty; it never guesses a format.
func ParseRelease(title string) Release {
	title = strings.TrimSpace(reExt.ReplaceAllString(strings.TrimSpace(title), ""))
	var r Release

	// A leading "(1997)" or "[FLAC]" is metadata, not part of the artist.
	lead := ""
	if m := reLeading.FindStringSubmatch(title); m != nil && isMeta(m[1]) {
		lead = m[1]
		title = strings.TrimSpace(title[len(m[0]):])
	}

	tokens := normalizeTokens(title + " " + lead)
	readQuality(tokens, &r)
	r.Discography = reDiscog.MatchString(tokens)

	switch {
	case strings.Contains(title, " - "):
		artist, rest, _ := strings.Cut(title, " - ")
		r.Artist = cleanName(artist)
		r.Album = cutAlbum(rest)
		r.Year = yearFrom(rest, r.Album)
	case !strings.Contains(title, " ") && strings.Count(title, "-") >= 1:
		parseScene(title, &r)
	default:
		// No separator at all: all we can say is the album-ish text.
		r.Album = cutAlbum(strings.NewReplacer("_", " ", ".", " ").Replace(title))
		r.Year = yearFrom(title, r.Album)
	}
	if r.Year == 0 && lead != "" {
		r.Year = pureYear(lead)
	}
	return r
}

// parseScene reads "Artist-Album-TAG-TAG-YEAR-GROUP": underscores or dots
// stand for spaces, fields are separated by dashes.
func parseScene(title string, r *Release) {
	raw := strings.Split(title, "-")
	parts := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(strings.NewReplacer("_", " ", ".", " ").Replace(p))
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return
	}
	r.Artist = cleanName(parts[0])
	if len(parts) == 1 {
		return
	}
	r.Album = cutAlbum(parts[1])
	for _, p := range parts[2:] {
		if y := pureYear(p); y != 0 {
			r.Year = y
			break
		}
	}
	if r.Year == 0 {
		r.Year = yearFrom(parts[1], r.Album)
	}
	if len(parts) >= 3 {
		last := parts[len(parts)-1]
		if reGroup.MatchString(last) && pureYear(last) == 0 && !isMeta(last) {
			r.Group = last
		}
	}
}

// normalizeTokens makes a title searchable for tags: separators become
// spaces.
func normalizeTokens(s string) string {
	s = strings.NewReplacer("_", " ", ".", " ", "[", " ", "]", " ", "(", " ", ")", " ", "{", " ", "}", " ").Replace(s)
	return reSpaces.ReplaceAllString(s, " ")
}

func readQuality(tokens string, r *Release) {
	formats := map[string]bool{}
	for _, m := range reFormat.FindAllString(tokens, -1) {
		formats[strings.ToUpper(m)] = true
	}
	switch {
	case formats["FLAC"]:
		r.Format = FormatFLAC
	case formats["ALAC"]:
		r.Format = FormatALAC
	case formats["AAC"] || formats["M4A"]:
		r.Format = FormatAAC
	case formats["MP3"]:
		r.Format = FormatMP3
	}

	if m := reVBR.FindString(tokens); m != "" {
		r.Bitrate = strings.ToUpper(m)
	} else if m := reBitrateK.FindStringSubmatch(tokens); m != nil {
		r.Bitrate = m[1]
	} else if r.Format == FormatMP3 || r.Format == FormatAAC {
		if m := reBitrate.FindStringSubmatch(tokens); m != nil {
			r.Bitrate = m[1]
		}
	}
	if r.Format == "" && r.Bitrate != "" {
		r.Format = FormatMP3 // "320kbps" or "V0" alone is an MP3 release
	}

	if m := reBitDepth.FindStringSubmatch(tokens); m != nil {
		depth := m[1]
		if depth == "" {
			depth = m[2]
		}
		r.BitDepth, _ = strconv.Atoi(depth)
	} else if reHiRes.MatchString(tokens) && (r.Format == FormatFLAC || r.Format == FormatALAC) {
		r.BitDepth = 24
	}

	switch {
	case reVinyl.MatchString(tokens):
		r.Source = "Vinyl"
	case reWeb.MatchString(tokens):
		r.Source = "WEB"
	case reCD.MatchString(tokens):
		r.Source = "CD"
	}
}

// isMeta reports whether a bracketed group (or a scene field) is release
// metadata rather than part of a name: a year, a format, a bitrate, a bit
// depth, a source or a catalogue number.
func isMeta(s string) bool {
	t := normalizeTokens(s)
	if reYear.MatchString(t) || reFormat.MatchString(t) || reBitrateK.MatchString(t) || reVBR.MatchString(t) ||
		reBitDepth.MatchString(t) || reWeb.MatchString(t) || reVinyl.MatchString(t) || reCD.MatchString(t) ||
		strings.Contains(strings.ToLower(t), "khz") || reHiRes.MatchString(t) {
		return true
	}
	return reCatalog.MatchString(strings.TrimSpace(s))
}

// cutAlbum takes the album name from the text after the artist: it ends at
// the first bracketed group holding metadata ("(2020)", "[FLAC]"); without
// one, at the first year or quality word after its first word. Bracketed
// words that are not metadata ("(Deluxe Edition)") stay in the name.
func cutAlbum(s string) string {
	s = strings.TrimSpace(s)
	cut := -1
	for _, loc := range reBracket.FindAllStringSubmatchIndex(s, -1) {
		if loc[0] > 0 && isMeta(s[loc[2]:loc[3]]) {
			cut = loc[0]
			break
		}
	}
	if cut < 0 {
		for _, re := range []*regexp.Regexp{reYearRange, reYear, reFormat, reBitrateK, reVBR, reBitDepth, reWeb, reVinyl} {
			for _, loc := range re.FindAllStringIndex(s, -1) {
				if loc[0] > 0 && (cut < 0 || loc[0] < cut) {
					cut = loc[0]
					break
				}
			}
		}
	}
	if cut >= 0 {
		s = s[:cut]
	}
	return cleanName(s)
}

// yearFrom finds the release year in the text after the artist: a
// bracketed year on its own ("(1997)") first, then any year after the
// album name. A range ("1990-2020") is not a year.
func yearFrom(rest, album string) int {
	rest = reYearRange.ReplaceAllString(rest, " ")
	for _, m := range reBracket.FindAllStringSubmatch(rest, -1) {
		if y := pureYear(m[1]); y != 0 {
			return y
		}
	}
	after := rest
	if album != "" {
		if i := strings.Index(rest, album); i >= 0 {
			after = rest[i+len(album):]
		}
	}
	if m := reYear.FindString(after); m != "" {
		y, _ := strconv.Atoi(m)
		return y
	}
	return 0
}

// pureYear returns s as a year when s is nothing but a year.
func pureYear(s string) int {
	s = strings.TrimSpace(s)
	if len(s) == 4 && reYear.MatchString(s) {
		y, _ := strconv.Atoi(s)
		return y
	}
	return 0
}

// cleanName trims separators and doubled spaces left around a name.
func cleanName(s string) string {
	s = reSpaces.ReplaceAllString(strings.TrimSpace(s), " ")
	return strings.TrimSpace(strings.TrimRight(strings.TrimLeft(s, "-–,:; "), "-–,:;([{ "))
}
