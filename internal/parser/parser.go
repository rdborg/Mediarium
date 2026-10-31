// Package parser extracts structured metadata from release filenames,
// using PTN-style logic as a base. It works by repeatedly matching and
// stripping known tokens (season/episode, quality,
// codec, group, edition, proper/repack) from the raw name; whatever is left
// before the first stripped token is the title candidate.
//
// This does not import github.com/rdborg/mediarium/internal/download (or
// any other module) — the project's package-boundary rule keeps the parser
// usable standalone.
package parser

import (
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// parseCalls counts calls to Parse, so a test can show that a run parses each
// release once.
var parseCalls atomic.Int64

// ParseCalls is how many times Parse has been called in this process.
func ParseCalls() int64 { return parseCalls.Load() }

// Release is the structured result of parsing a release filename. Season/
// Episode fields are zero for movie releases; they exist now (rather than
// being added in Phase 2) so the same struct serves both without a rework.
type Release struct {
	Title string
	Year  int
	// YearLate is set when Year stands after the season or episode marker
	// ("Show.S01E05.2015.WEB-DL"). That is as likely an air year as the show's
	// own, so a check against the show's year should not rely on it.
	YearLate   bool
	Season     int   // 0 = not a TV release
	SeasonTo   int   // last season of a pack of several (S01-S03 gives 3), else 0
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
	// Languages are the audio languages the release name tags (English,
	// Italian...), each once. Empty means untagged, which is usually English.
	Languages []string
	// Multi is set when the release says it holds several audio languages
	// (MULTI, DUAL, or DL after a language).
	Multi bool
	// Subbed is set for releases tagged with subtitles (VOSTFR, SUBBED...).
	Subbed bool
	// Absolute holds episode numbers counted from the start of the show
	// rather than per season ("One Piece - 1085", "Show E1085"), the way
	// anime is usually named. Only set when there is no season marker.
	Absolute []int
	// AirDate is the date a daily show's episode aired ("2024-03-15"), when
	// the name carries one and no season marker.
	AirDate string
	// DailyTitle is what comes between the air date and the quality tags,
	// usually the guest or the episode's title ("Show.2024.03.15.Tom.Hanks.
	// 1080p" gives "Tom Hanks"). It tells apart two episodes on one day.
	DailyTitle string
	// DailyPart is N for "Part N" or "PtN" after the air date, 0 otherwise.
	DailyPart int
	// Special is set when the name says season 0 outright (S00E05, 0x05,
	// "Season 0"): a special, not a name without a season.
	Special bool
	// Name is the name that was parsed, without its file extension.
	Name string
}

type token struct {
	re     *regexp.Regexp
	assign func(*Release, []string)
}

var (
	yearRe = `((?:19|20)\d{2})`

	tokens = []token{
		{regexp.MustCompile(`(?i)\bproper\b`), func(r *Release, _ []string) { r.Proper = true }},
		{regexp.MustCompile(`(?i)\brepack\b`), func(r *Release, _ []string) { r.Repack = true }},
		{regexp.MustCompile(`(?i)\b3D\b`), func(r *Release, _ []string) { r.Is3D = true }},

		{regexp.MustCompile(`(?i)\b(HDR10\+)\b`), func(r *Release, m []string) { r.HDR = "HDR10+" }},
		{regexp.MustCompile(`(?i)\b(HDR10|HDR)\b`), func(r *Release, m []string) { r.HDR = "HDR10" }},
		{regexp.MustCompile(`(?i)\b(DV|Dolby[. ]?Vision)\b`), func(r *Release, m []string) { r.HDR = "Dolby Vision" }},

		{regexp.MustCompile(`(?i)\b(Extended|Director'?s[. ]?Cut|Unrated|IMAX|Theatrical|Remastered|Criterion)\b`), func(r *Release, m []string) { r.Edition = normalizeSpace(m[1]) }},

		{regexp.MustCompile(`(?i)\b(2160p|1080p|720p|480p|4K)\b`), func(r *Release, m []string) {
			res := strings.ToLower(m[1])
			if res == "4k" {
				res = "2160p"
			}
			r.Resolution = res
		}},

		// An HDRip is a re-encode of something that is not a clean web or disc
		// copy, usually a TV capture. It comes first so a name that also says
		// WEB-DL or BluRay is read by those, which follow.
		{regexp.MustCompile(`(?i)\b(HD-?Rip)\b`), func(r *Release, m []string) { r.Source = normalizeSource(m[1]) }},
		{regexp.MustCompile(`(?i)\b(WEB-?DL|WEBRip|WEB)\b`), func(r *Release, m []string) { r.Source = normalizeSource(m[1]) }},
		{regexp.MustCompile(`(?i)\b(BluRay|BDRip|BRRip)\b`), func(r *Release, m []string) { r.Source = normalizeSource(m[1]) }},
		{regexp.MustCompile(`(?i)\b(HDTV|PDTV|SDTV)\b`), func(r *Release, m []string) { r.Source = normalizeSource(m[1]) }},
		{regexp.MustCompile(`(?i)\b(DVDRip|DVDR|DVD)\b`), func(r *Release, m []string) { r.Source = normalizeSource(m[1]) }},
		// Pre-release copies (recorded in a cinema, or screener discs). They
		// often also carry "1080p", so this must win over the resolution: the
		// picture is still a camera or telesync recording.
		{regexp.MustCompile(`(?i)\b(CAM(?:-?Rip)?|HD-?CAM(?:-?Rip)?|HQ-?CAM(?:-?Rip)?|TS(?:-?Rip)?|HD-?TS(?:-?Rip)?|HQ-?TS(?:-?Rip)?|TELE-?SYNC|PDVD|PreDVD(?:-?Rip)?|TC(?:-?Rip)?|HD-?TC(?:-?Rip)?|TELE-?CINE|SCR|SCREENER|DVD-?SCR(?:-?Rip)?|BD-?SCR|WEB-?SCR|R5)\b`), func(r *Release, m []string) {
			r.Source = normalizePreRelease(m[1])
		}},
		// Remux is its own token (not an alternative in the BluRay regex
		// above) because a filename can contain both words separately
		// ("...BluRay.REMUX...") — Remux is the more specific quality tier
		// and should win when both are present.
		{regexp.MustCompile(`(?i)\bRemux\b`), func(r *Release, _ []string) { r.Source = "Remux" }},

		{regexp.MustCompile(`(?i)\b(x264|x265|h[. ]?264|h[. ]?265|hevc|avc|xvid|divx)\b`), func(r *Release, m []string) { r.Codec = normalizeCodec(m[1]) }},

		{regexp.MustCompile(`(?i)\b(TrueHD|Atmos|DTS-?HD|DTS|DDP?5\.1|DD5\.1|AC3|EAC3|AAC2?\.0|AAC|FLAC|MP3)\b`), func(r *Release, m []string) { r.AudioCodec = normalizeAudio(m[1]) }},

		// Season+episode(s): S01E01, S01E01E02E03 or S01E01.E02 (concatenated),
		// S01E01-E03 or S01E01-03 (range). A single regex handles all these
		// shapes by capturing the whole episode-tag run as one group and
		// expanding it in parseEpisodeRun — Go's RE2 engine (unlike PCRE) only
		// keeps the *last* iteration of a repeated capture group, so matching
		// each episode number as its own capture and reading m[3] (as an
		// earlier version of this code did) silently dropped every episode but
		// the last one in a multi-episode release. "S01 E01" and "S1.E1" may
		// have a separator before the E, and a season can be a year (S2024E01).
		{seasonEpisodeRe, func(r *Release, m []string) {
			r.Season = atoi(m[1])
			r.Special = r.Season == 0
			r.Episodes = parseEpisodeRun(m[2])
			if len(r.Episodes) > 0 {
				r.Episode = r.Episodes[0]
			}
		}},
		// "1x05" style season/episode, common in older TV libraries, and the
		// ranges "1x01-1x03" and "1x01-03".
		{crossEpisodeRe, func(r *Release, m []string) {
			r.Season = atoi(m[1])
			r.Special = r.Season == 0
			r.Episode = atoi(m[2])
			r.Episodes = []int{r.Episode}
			if m[4] != "" && (m[3] == "" || atoi(m[3]) == r.Season) {
				for e := r.Episode + 1; e <= atoi(m[4]); e++ {
					r.Episodes = append(r.Episodes, e)
				}
			}
		}},
		// Season pack: S01, Season 1 (spaces are already dots by now), or
		// several seasons: S01-S03, S01-03, Season 1-3. A name that already
		// gave an episode keeps that episode's season.
		{seasonWordRe, assignSeasonPack},
		{seasonPackRe, assignSeasonPack},
		// Airdate style: 2021.05.14 or 2021-05-14
		{airDateRe, func(r *Release, m []string) {
			if r.Season == 0 && !r.Special {
				r.AirDate = m[1] + "-" + m[2] + "-" + m[3]
			}
		}},
	}

	// The season and episode markers, described where tokens uses them. A
	// title can come after one of these ("Season.2.Show.1080p").
	seasonEpisodeRe = regexp.MustCompile(`(?i)[Ss]((?:19|20)\d{2}|\d{1,2})[._-]?([Ee]\d{1,3}(?:\.?[Ee]\d{1,3})*(?:-[Ee]?\d{1,3}\b)?)`)
	crossEpisodeRe  = regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{2,3})(?:-(?:(\d{1,2})x)?(\d{2,3}))?\b`)
	seasonWordRe    = regexp.MustCompile(`(?i)\bSeason[. ]?(\d{1,2})(?:-(\d{1,2}))?\b`)
	seasonPackRe    = regexp.MustCompile(`(?i)\bS(\d{1,2})(?:-S?(\d{1,2}))?\b`)

	// junkSuffixRe is what posting tools add after the group ("-GRP-postbot",
	// "-GRP-Obfuscated"); trailingTagRe a site's tag after it ("-GRP[rarbg]").
	// Both are cut off before the group is read.
	junkSuffixRe  = regexp.MustCompile(`(?i)(?:-(?:postbot|obfuscated|xpost|scrambled|buymore|whiterev|asrequested|chaos|rp|nzbgeek|sample))+$`)
	trailingTagRe = regexp.MustCompile(`(?:\[[^\[\]]{1,40}\])+$`)

	airDateRe   = regexp.MustCompile(`\b((?:19|20)\d{2})[.-](\d{2})[.-](\d{2})\b`)
	dailyPartRe = regexp.MustCompile(`(?i)\b(?:part|pt)\.?(\d{1,2})\b`)

	// groupRe is the release group: a trailing "-GROUPNAME" at the very end
	// of the name. notGroupRe lists what such an ending is not a group: the
	// end of "WEB-DL", "DTS-HD" or "Blu-Ray", or an episode or resolution.
	groupRe     = regexp.MustCompile(`-([A-Za-z0-9]+)$`)
	notGroupRe  = regexp.MustCompile(`(?i)^(?:dl|hd|ray|(?:[se]\d{1,4})+|\d{1,4}p?)$`)
	plainWordRe = regexp.MustCompile(`^[A-Za-z]+$`)
	// The year must stand alone: "1920x1080" and "12019" hold no year.
	yearPattern  = regexp.MustCompile(`\b` + yearRe + `\b`)
	episodeTagRe = regexp.MustCompile(`(?i)[Ee](\d{1,3})`)
	// Anime: a leading "[Group]", then "Title - 1085" (or a batch "- 01-12"),
	// or "Title.E1085" without a season.
	leadingGroupRe = regexp.MustCompile(`^\[([^\]]{1,40})\][.\s_]*`)
	absoluteDashRe = regexp.MustCompile(`\.-\.(\d{1,4})(?:v\d)?(?:-(\d{1,4})(?:v\d)?)?(?:\.|\[|\(|$)`)
	// seasonDashRe is the anime way of naming a later season's episode:
	// "Show S2 - 05" (spaces are dots by then).
	seasonDashRe   = regexp.MustCompile(`(?i)\bS(\d{1,2})\.-\.(\d{1,3})(?:v\d)?(?:-(\d{1,3})(?:v\d)?)?(?:\.|\[|\(|$)`)
	absoluteERe    = regexp.MustCompile(`(?i)\.E(\d{2,4})(?:v\d)?(?:\.|\[|\(|$)`)
	leadingDigitRe = regexp.MustCompile(`^(\d{1,3})`)
)

// pickYear finds the release year in a name: the last year that could be one.
// It returns where the year starts (-1 for none), its value, and whether it
// stands after the season or episode marker.
//
// A number is not a release year when
//   - it is the first thing in the name ("1917.1080p", "2012 (2009)"),
//   - it is more than a year ahead of today ("Blade.Runner.2049.1080p"), or
//   - the name has a title before its season or episode marker and the number
//     comes after the marker but not right behind it: that is the episode's
//     title ("Show.S02E05.2012.Olympics.Special", "Show.S03E10.The.Year.2000").
func pickYear(working string, seasonSpans [][2]int) (at, year int, late bool) {
	all := yearPattern.FindAllStringSubmatchIndex(working, -1)
	if len(all) == 0 {
		return -1, 0, false
	}
	// The first marker with a title in front of it.
	markerAt := -1
	for _, sp := range seasonSpans {
		if (markerAt < 0 || sp[0] < markerAt) && cleanTitle(working[:sp[0]]) != "" {
			markerAt = sp[0]
		}
	}
	maxYear := time.Now().Year() + 1
	for i := len(all) - 1; i >= 0; i-- {
		m := all[i]
		if m[0] == 0 {
			break // earlier ones can only be further left
		}
		y := atoi(working[m[2]:m[3]])
		if y > maxYear {
			continue
		}
		if markerAt >= 0 && m[0] > markerAt {
			if !rightAfterMarker(working, seasonSpans, m[0]) || !endsWithTags(working[m[1]:]) {
				continue
			}
			late = true
		}
		return m[0], y, late
	}
	return -1, 0, false
}

// rightAfterMarker reports whether only separators lie between the end of a
// season or episode marker and pos.
func rightAfterMarker(working string, seasonSpans [][2]int, pos int) bool {
	for _, sp := range seasonSpans {
		if sp[1] <= pos && strings.Trim(working[sp[1]:pos], "._-([ ") == "" {
			return true
		}
	}
	return false
}

// endsWithTags reports whether rest (what follows a year) is nothing, a group,
// or starts with a quality tag, as opposed to more of a title.
func endsWithTags(rest string) bool {
	rest = strings.TrimLeft(rest, ")]")
	if rest == "" || rest[0] == '-' {
		return true
	}
	rest = strings.TrimLeft(rest, "._ [(")
	if rest == "" {
		return true
	}
	if len(rest) > 40 {
		rest = rest[:40]
	}
	for _, t := range tokens {
		if loc := t.re.FindStringIndex(rest); loc != nil && loc[0] == 0 {
			return true
		}
	}
	return false
}

// assignSeasonPack reads a season pack marker: m[1] is the season, m[2] the
// last season of a range, if any.
func assignSeasonPack(r *Release, m []string) {
	if len(r.Episodes) > 0 {
		return
	}
	r.Season = atoi(m[1])
	r.Special = r.Special || r.Season == 0
	if to := atoi(m[2]); to > r.Season {
		r.SeasonTo = to
	}
}

// isSeasonMarker reports whether a token regex is a season or episode marker.
func isSeasonMarker(re *regexp.Regexp) bool {
	return re == seasonEpisodeRe || re == crossEpisodeRe || re == seasonWordRe || re == seasonPackRe
}

// parseEpisodeRun expands a captured episode-tag run (e.g. "E01E02E03",
// "E01-E03", or "E01-03") into the full list of episode numbers it
// represents.
func parseEpisodeRun(s string) []int {
	if dashIdx := strings.IndexByte(s, '-'); dashIdx >= 0 {
		before, after := s[:dashIdx], s[dashIdx+1:]
		// Only the last tag counts, and a tag is at most four characters.
		beforeTags := episodeTagRe.FindAllStringSubmatch(before[max(0, len(before)-4):], -1)
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

	// Cap the count so a name of nothing but episode tags cannot build a huge list.
	matches := episodeTagRe.FindAllStringSubmatch(s, 999)
	out := make([]int, 0, len(matches))
	for _, m := range matches {
		out = append(out, atoi(m[1]))
	}
	return out
}

// Parse extracts a Release from a raw filename or release title. The input
// need not have an extension; if present it's stripped first.
func Parse(name string) Release {
	parseCalls.Add(1)
	// Names come from indexers and disks; bytes that are not UTF-8 would
	// otherwise flow into the title as they are.
	name = strings.ToValidUTF8(name, "")
	name = repairMojibake(name)
	name = stripExtension(name)
	r := Release{Name: name}
	// "[SubsPlease] Show - 01 (1080p) [ABCD1234]": the group comes first.
	// A group at the end ("-ETHEL") still wins: a leading bracket can also
	// be a site's tag ("[ www.example.org ] - Show.S01E01...-ETHEL").
	leading := ""
	if m := leadingGroupRe.FindStringSubmatch(name); m != nil {
		leading = strings.TrimSpace(m[1])
		name = strings.TrimLeft(name[len(m[0]):], "-. _")
	}
	working := strings.ReplaceAll(name, "_", ".")
	working = strings.ReplaceAll(working, " ", ".")

	titleEnd := len(working)
	var tagStarts []int
	var seasonSpans [][2]int

	for _, t := range tokens {
		loc := t.re.FindStringSubmatchIndex(working)
		if loc == nil {
			continue
		}
		tagStarts = append(tagStarts, loc[0])
		// A plain word right at the start with more after it ("Cam.2018",
		// "Proper.Manners.2019") is the start of the title, not a tag.
		if loc[0] == 0 && loc[1] < len(working) && plainWordRe.MatchString(working[:loc[1]]) {
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
		if isSeasonMarker(t.re) {
			seasonSpans = append(seasonSpans, [2]int{loc[0], loc[1]})
		}
	}

	if r.AirDate != "" && !r.Special {
		r.DailyTitle, r.DailyPart = dailyTitle(working, tagStarts)
	}

	// Year is handled separately from the generic token loop: a title can
	// itself contain a year-like number ("Blade Runner 2049"), so instead of
	// taking the first (leftmost) match we take the last plausible one — the
	// actual release year reliably sits right before the quality/source
	// metadata block, whereas a year embedded in the title sits earlier. A
	// year at the very start is the title ("1917.1080p.BluRay"), not a release
	// year.
	if at, year, late := pickYear(working, seasonSpans); at >= 0 {
		tagStarts = append(tagStarts, at)
		r.Year, r.YearLate = year, late
		if at < titleEnd {
			titleEnd = at
		}
	}

	// "Show S2 - 05": season 2, episode 5, not a season pack.
	if r.Season > 0 && len(r.Episodes) == 0 {
		if m := seasonDashRe.FindStringSubmatch(working); m != nil && atoi(m[1]) == r.Season {
			first, last := atoi(m[2]), atoi(m[2])
			if m[3] != "" && atoi(m[3]) >= first && atoi(m[3])-first < 500 {
				last = atoi(m[3])
			}
			for e := first; e <= last && first > 0; e++ {
				r.Episodes = append(r.Episodes, e)
			}
			if len(r.Episodes) > 0 {
				r.Episode = r.Episodes[0]
				r.SeasonTo = 0
			}
		}
	}

	// Anime numbering, only when the name has no season marker.
	if r.Season == 0 && r.AirDate == "" && !r.Special {
		if loc := absoluteDashRe.FindStringSubmatchIndex(working); loc != nil && halfEpisode(working, loc[1]) {
			// A recap ("- 12.5"): no episode number, but the title still ends here.
			if loc[0] < titleEnd {
				titleEnd = loc[0]
			}
		} else if loc != nil {
			first := atoi(working[loc[2]:loc[3]])
			last := first
			if loc[4] >= 0 {
				last = atoi(working[loc[4]:loc[5]])
			}
			// "Title - 2019" is a year, not episode 2019.
			if !(loc[3]-loc[2] == 4 && first >= 1900 && first <= 2099) && first > 0 {
				if last < first || last-first > 2000 {
					last = first
				}
				for e := first; e <= last; e++ {
					r.Absolute = append(r.Absolute, e)
				}
				if loc[0] < titleEnd {
					titleEnd = loc[0]
				}
			}
		} else if loc := absoluteERe.FindStringSubmatchIndex(working); loc != nil && loc[0] > 0 {
			r.Absolute = []int{atoi(working[loc[2]:loc[3]])}
			if loc[0] < titleEnd {
				titleEnd = loc[0]
			}
		}
	}

	// The release group is the last thing in the name, and only counts after
	// something else was read: "Spider-Man" has no group. A site's tag and
	// what posting tools add after the group are not part of it.
	groupEnd := len(working)
	if loc := trailingTagRe.FindStringIndex(working); loc != nil && loc[0] > 0 {
		groupEnd = loc[0]
	}
	if loc := junkSuffixRe.FindStringIndex(working[:groupEnd]); loc != nil && loc[0] > 0 {
		groupEnd = loc[0]
		if groupEnd < titleEnd {
			titleEnd = groupEnd
		}
	}
	if loc := groupRe.FindStringSubmatchIndex(working[:groupEnd]); loc != nil && titleEnd < loc[0] {
		if g := working[loc[2]:loc[3]]; !notGroupRe.MatchString(g) {
			r.Group = g
			tagStarts = append(tagStarts, loc[0])
		}
	}
	if r.Group == "" {
		r.Group = leading
	}

	if titleEnd < 0 || titleEnd > len(working) {
		titleEnd = len(working)
	}
	r.Title = cleanTitle(working[:titleEnd])
	afterTitle := titleEnd
	// "Season.2.Show.1080p": nothing before the season marker, so the title
	// is what comes after it, up to the next tag.
	if r.Title == "" {
		start := -1
		for _, sp := range seasonSpans {
			if sp[1] > start && cleanTitle(working[:sp[0]]) == "" {
				start = sp[1]
			}
		}
		if start >= 0 {
			end := groupEnd
			for _, st := range tagStarts {
				if st >= start && st < end {
					end = st
				}
			}
			r.Title = cleanTitle(working[start:end])
			afterTitle = end
		}
	}
	// Language tags are only read after the title, so "The Italian Job" is
	// not an Italian release.
	tail := working[afterTitle:]
	if r.Season > 0 || r.Special || len(r.Episodes) > 0 || r.AirDate != "" {
		tail = withoutEpisodeTitle(tail)
	}
	r.Languages, r.Multi, r.Subbed = detectLanguages(tail)
	if len(r.Episodes) == 0 && r.Episode != 0 {
		r.Episodes = []int{r.Episode}
	}
	return r
}

func stripExtension(name string) string {
	if idx := strings.LastIndex(name, "."); idx > 0 {
		ext := strings.ToLower(name[idx+1:])
		switch ext {
		case "mkv", "mp4", "m4v", "avi", "mov", "wmv", "webm", "mpg", "mpeg", "flv", "ts", "m2ts", "nzb", "torrent":
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

// normalizePreRelease names the kind of pre-release copy: CAM, TELESYNC,
// TELECINE, SCREENER or R5.
func normalizePreRelease(s string) string {
	// "Rip" on the end ("HDCAMRip", "HDTSRip") names the same copy.
	u := strings.TrimSuffix(strings.ReplaceAll(strings.ToUpper(s), "-", ""), "RIP")
	switch u {
	case "CAM", "HDCAM", "HQCAM":
		return "CAM"
	case "TS", "HDTS", "HQTS", "TELESYNC", "PDVD", "PREDVD":
		return "TELESYNC"
	case "TC", "HDTC", "TELECINE":
		return "TELECINE"
	case "R5":
		return "R5"
	}
	return "SCREENER"
}

// IsPreRelease reports whether a source is a cinema recording or screener
// rather than a proper release.
func IsPreRelease(source string) bool {
	switch source {
	case "CAM", "TELESYNC", "TELECINE", "SCREENER", "R5":
		return true
	}
	return false
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
	case "hdrip":
		return "HDRip"
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
	switch strings.ToLower(strings.NewReplacer(" ", "", ".", "").Replace(s)) {
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

// halfEpisode reports whether the number a match ended on goes on with a
// decimal part: "- 12.5" is a recap between episodes 12 and 13, not 12.
func halfEpisode(working string, end int) bool {
	return end > 0 && end < len(working) && working[end-1] == '.' && working[end] >= '0' && working[end] <= '9'
}

// dailyTitle is the text between the air date and the first tag after it
// (or the release group at the end), with any "Part N" read out of it.
func dailyTitle(working string, tagStarts []int) (string, int) {
	loc := airDateRe.FindStringIndex(working)
	if loc == nil {
		return "", 0
	}
	end := len(working)
	for _, st := range tagStarts {
		if st >= loc[1] && st < end {
			end = st
		}
	}
	rest := working[loc[1]:end]
	if end == len(working) {
		if m := groupRe.FindStringIndex(rest); m != nil {
			rest = rest[:m[0]]
		}
	}
	part := 0
	if m := dailyPartRe.FindStringSubmatch(rest); m != nil {
		part = atoi(m[1])
	}
	return normalizeSpace(strings.Trim(strings.ReplaceAll(rest, ".", " "), " -")), part
}
