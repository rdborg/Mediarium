// Package parser extracts structured metadata from release filenames
// (PRD.md §4.4 "Release parser" — PTN-style logic as a base). It works by
// repeatedly matching and stripping known tokens (season/episode, quality,
// codec, group, edition, proper/repack) from the raw name; whatever is left
// before the first stripped token is the title candidate.
//
// This does not import github.com/ryanborg/mediarium/internal/download (or
// any other module) — CLAUDE.md's package-boundary rule keeps the parser
// usable standalone.
package parser

import (
	"regexp"
	"strconv"
	"strings"
)

// Release is the structured result of parsing a release filename. Season/
// Episode fields are zero for movie releases; they exist now (rather than
// being added in Phase 2) so the same struct serves both without a rework.
type Release struct {
	Title      string
	Year       int
	Season     int   // 0 = not a TV release
	Episode    int   // 0 = season pack / not episode-specific
	Episodes   []int // populated for multi-episode releases (e.g. E01E02)
	Resolution string
	Source     string // WEB-DL, WEBRip, BluRay, REMUX, HDTV, DVDRip, CAM, ...
	Codec      string // x264, x265, XviD, ...
	AudioCodec string // AAC, AC3, DTS, TrueHD, Atmos, ...
	HDR        string // HDR10, HDR10+, DV
	Edition    string // Extended, Director's Cut, Unrated, IMAX, ...
	Group      string
	Proper     bool
	Repack     bool
	Is3D       bool
}

type token struct {
	re     *regexp.Regexp
	assign func(*Release, []string)
}

var (
	yearRe = `((?:19|20)\d{2})`

	tokens = []token{
		// Release group: trailing "-GROUPNAME" at the very end of the name.
		{regexp.MustCompile(`(?i)-([A-Za-z0-9][A-Za-z0-9_.]*)$`), func(r *Release, m []string) { r.Group = m[1] }},

		{regexp.MustCompile(`(?i)\bproper\b`), func(r *Release, _ []string) { r.Proper = true }},
		{regexp.MustCompile(`(?i)\brepack\b`), func(r *Release, _ []string) { r.Repack = true }},
		{regexp.MustCompile(`(?i)\b3D\b`), func(r *Release, _ []string) { r.Is3D = true }},

		{regexp.MustCompile(`(?i)\b(HDR10\+)\b`), func(r *Release, m []string) { r.HDR = "HDR10+" }},
		{regexp.MustCompile(`(?i)\b(HDR10|HDR)\b`), func(r *Release, m []string) { r.HDR = "HDR10" }},
		{regexp.MustCompile(`(?i)\b(DV|Dolby ?Vision)\b`), func(r *Release, m []string) { r.HDR = "Dolby Vision" }},

		{regexp.MustCompile(`(?i)\b(Extended|Director'?s ?Cut|Unrated|IMAX|Theatrical|Remastered|Criterion)\b`), func(r *Release, m []string) { r.Edition = normalizeSpace(m[1]) }},

		{regexp.MustCompile(`(?i)\b(2160p|1080p|720p|480p|4K)\b`), func(r *Release, m []string) {
			res := strings.ToLower(m[1])
			if res == "4k" {
				res = "2160p"
			}
			r.Resolution = res
		}},

		{regexp.MustCompile(`(?i)\b(WEB-?DL|WEBRip|WEB)\b`), func(r *Release, m []string) { r.Source = normalizeSource(m[1]) }},
		{regexp.MustCompile(`(?i)\b(BluRay|BDRip|BRRip)\b`), func(r *Release, m []string) { r.Source = normalizeSource(m[1]) }},
		{regexp.MustCompile(`(?i)\b(HDTV|PDTV|SDTV)\b`), func(r *Release, m []string) { r.Source = normalizeSource(m[1]) }},
		{regexp.MustCompile(`(?i)\b(DVDRip|DVDR|DVD)\b`), func(r *Release, m []string) { r.Source = normalizeSource(m[1]) }},
		{regexp.MustCompile(`(?i)\b(CAM|TS|TC|SCR|R5)\b`), func(r *Release, m []string) { r.Source = strings.ToUpper(m[1]) }},
		// Remux is its own token (not an alternative in the BluRay regex
		// above) because a filename can contain both words separately
		// ("...BluRay.REMUX...") — Remux is the more specific quality tier
		// and should win when both are present.
		{regexp.MustCompile(`(?i)\bRemux\b`), func(r *Release, _ []string) { r.Source = "Remux" }},

		{regexp.MustCompile(`(?i)\b(x264|x265|h ?264|h ?265|hevc|avc|xvid|divx)\b`), func(r *Release, m []string) { r.Codec = normalizeCodec(m[1]) }},

		{regexp.MustCompile(`(?i)\b(TrueHD|Atmos|DTS-?HD|DTS|DDP?5\.1|DD5\.1|AC3|EAC3|AAC2?\.0|AAC|FLAC|MP3)\b`), func(r *Release, m []string) { r.AudioCodec = normalizeAudio(m[1]) }},

		// Season+episode(s): S01E01, S01E01E02E03 (concatenated), S01E01-E03
		// or S01E01-03 (range). A single regex handles all three shapes by
		// capturing the whole episode-tag run as one group and expanding it
		// in parseEpisodeRun — Go's RE2 engine (unlike PCRE) only keeps the
		// *last* iteration of a repeated capture group, so matching each
		// episode number as its own capture and reading m[3] (as an earlier
		// version of this code did) silently dropped every episode but the
		// last one in a multi-episode release.
		{regexp.MustCompile(`(?i)[Ss](\d{1,2})((?:[Ee]\d{1,3})+(?:-[Ee]?\d{1,3})?)`), func(r *Release, m []string) {
			r.Season = atoi(m[1])
			r.Episodes = parseEpisodeRun(m[2])
			if len(r.Episodes) > 0 {
				r.Episode = r.Episodes[0]
			}
		}},
		// "1x05" style season/episode, common in older TV libraries.
		{regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{2,3})\b`), func(r *Release, m []string) {
			r.Season = atoi(m[1])
			r.Episode = atoi(m[2])
			r.Episodes = []int{r.Episode}
		}},
		// Season pack: S01, Season 1
		{regexp.MustCompile(`(?i)\bSeason ?(\d{1,2})\b`), func(r *Release, m []string) { r.Season = atoi(m[1]) }},
		{regexp.MustCompile(`(?i)\bS(\d{1,2})\b`), func(r *Release, m []string) { r.Season = atoi(m[1]) }},
		// Airdate style: 2021.05.14
		{regexp.MustCompile(`\b(19|20)\d{2}\.\d{2}\.\d{2}\b`), func(r *Release, m []string) {}},
	}

	yearPattern    = regexp.MustCompile(yearRe)
	episodeTagRe   = regexp.MustCompile(`(?i)[Ee](\d{1,3})`)
	leadingDigitRe = regexp.MustCompile(`^(\d{1,3})`)
)

// parseEpisodeRun expands a captured episode-tag run (e.g. "E01E02E03",
// "E01-E03", or "E01-03") into the full list of episode numbers it
// represents.
func parseEpisodeRun(s string) []int {
	if dashIdx := strings.IndexByte(s, '-'); dashIdx >= 0 {
		before, after := s[:dashIdx], s[dashIdx+1:]
		beforeTags := episodeTagRe.FindAllStringSubmatch(before, -1)
		if len(beforeTags) == 0 {
			return nil
		}
		start := atoi(beforeTags[len(beforeTags)-1][1])

		var end int
		if afterTag := episodeTagRe.FindStringSubmatch(after); afterTag != nil {
			end = atoi(afterTag[1])
		} else if digits := leadingDigitRe.FindStringSubmatch(after); digits != nil {
			end = atoi(digits[1])
		} else {
			return []int{start}
		}
		if end < start {
			return []int{start}
		}
		out := make([]int, 0, end-start+1)
		for e := start; e <= end; e++ {
			out = append(out, e)
		}
		return out
	}

	matches := episodeTagRe.FindAllStringSubmatch(s, -1)
	out := make([]int, 0, len(matches))
	for _, m := range matches {
		out = append(out, atoi(m[1]))
	}
	return out
}

// Parse extracts a Release from a raw filename or release title. The input
// need not have an extension; if present it's stripped first.
func Parse(name string) Release {
	name = stripExtension(name)
	working := strings.ReplaceAll(name, "_", ".")
	working = strings.ReplaceAll(working, " ", ".")

	var r Release
	titleEnd := len(working)

	for _, t := range tokens {
		loc := t.re.FindStringSubmatchIndex(working)
		if loc == nil {
			continue
		}
		groups := make([]string, len(loc)/2)
		for i := range groups {
			if loc[2*i] < 0 {
				continue
			}
			groups[i] = working[loc[2*i]:loc[2*i+1]]
		}
		t.assign(&r, groups)
		if loc[0] < titleEnd {
			titleEnd = loc[0]
		}
	}

	// Year is handled separately from the generic token loop: a title can
	// itself contain a year-like number ("Blade Runner 2049"), so instead of
	// taking the first (leftmost) match we take the last one — the actual
	// release year reliably sits right before the quality/source metadata
	// block, whereas a year embedded in the title sits earlier.
	if allYears := yearPattern.FindAllStringSubmatchIndex(working, -1); len(allYears) > 0 {
		last := allYears[len(allYears)-1]
		r.Year = atoi(working[last[2]:last[3]])
		if last[0] < titleEnd {
			titleEnd = last[0]
		}
	}

	if titleEnd < 0 || titleEnd > len(working) {
		titleEnd = len(working)
	}
	r.Title = cleanTitle(working[:titleEnd])
	if len(r.Episodes) == 0 && r.Episode != 0 {
		r.Episodes = []int{r.Episode}
	}
	return r
}

func stripExtension(name string) string {
	if idx := strings.LastIndex(name, "."); idx > 0 {
		ext := strings.ToLower(name[idx+1:])
		switch ext {
		case "mkv", "mp4", "avi", "mov", "wmv", "ts", "m2ts", "nzb", "torrent":
			return name[:idx]
		}
	}
	return name
}

func cleanTitle(s string) string {
	s = strings.ReplaceAll(s, ".", " ")
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "-_ ")
	// A parenthesised year ("Inception (2010)") leaves its opening bracket
	// dangling once the year token is cut off.
	s = strings.TrimSpace(strings.TrimRight(s, "([{"))
	s = strings.Trim(s, "-_ ")
	// Collapse repeated whitespace left behind by removed tokens.
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

func normalizeSpace(s string) string {
	s = strings.ReplaceAll(s, ".", " ")
	return strings.TrimSpace(s)
}

func normalizeSource(s string) string {
	switch strings.ToLower(strings.ReplaceAll(s, "-", "")) {
	case "webdl":
		return "WEB-DL"
	case "webrip":
		return "WEBRip"
	case "web":
		return "WEB"
	case "bluray":
		return "BluRay"
	case "bdrip":
		return "BDRip"
	case "brrip":
		return "BRRip"
	case "remux":
		return "Remux"
	case "hdtv":
		return "HDTV"
	case "pdtv":
		return "PDTV"
	case "sdtv":
		return "SDTV"
	case "dvdrip":
		return "DVDRip"
	case "dvdr":
		return "DVDR"
	case "dvd":
		return "DVD"
	default:
		return s
	}
}

func normalizeCodec(s string) string {
	switch strings.ToLower(strings.ReplaceAll(s, " ", "")) {
	case "x264", "h264", "avc":
		return "x264"
	case "x265", "h265", "hevc":
		return "x265"
	case "xvid":
		return "XviD"
	case "divx":
		return "DivX"
	default:
		return s
	}
}

func normalizeAudio(s string) string {
	upper := strings.ToUpper(s)
	switch {
	case strings.HasPrefix(upper, "DTSHD"), upper == "DTS-HD":
		return "DTS-HD"
	case strings.HasPrefix(upper, "DDP") || strings.HasPrefix(upper, "DD5"):
		return "DD5.1"
	case strings.HasPrefix(upper, "AAC"):
		return "AAC"
	default:
		return upper
	}
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
