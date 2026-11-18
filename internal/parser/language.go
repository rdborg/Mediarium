package parser

import (
	"regexp"
	"sort"
	"strings"
)

// English is the name Languages uses for English, the language a release
// with no language tag is assumed to be in.
const English = "English"

// languageTags maps the tags releases carry to the language they name. Tags
// are compared without case. Short ones (FR, GER, ESP, RUS) are only read
// after the title, so a film called "The Italian Job" is not taken for an
// Italian release.
var languageTags = map[string]string{
	"ENG": English, "ENGLISH": English,

	"ITA": "Italian", "ITALIAN": "Italian", "ITALIANO": "Italian",

	"GER": "German", "GERMAN": "German", "DEUTSCH": "German",

	"FR": "French", "FRA": "French", "FRE": "French", "FRENCH": "French", "TRUEFRENCH": "French",
	"VFF": "French", "VFQ": "French", "VFI": "French", "VF2": "French", "VF": "French",

	"ESP": "Spanish", "SPANISH": "Spanish", "CASTELLANO": "Spanish", "LATINO": "Spanish",

	"RUS": "Russian", "RUSSIAN": "Russian",

	"PORTUGUESE": "Portuguese", "BRAZILIAN": "Portuguese",
	"DUTCH":  "Dutch",
	"POLISH": "Polish",
	"NORDIC": "Nordic", "SWEDISH": "Swedish", "DANISH": "Danish", "NORWEGIAN": "Norwegian", "FINNISH": "Finnish",
	"TURKISH": "Turkish", "GREEK": "Greek", "HUNGARIAN": "Hungarian", "CZECH": "Czech", "ROMANIAN": "Romanian", "UKRAINIAN": "Ukrainian",
	"ARABIC": "Arabic", "HEBREW": "Hebrew",
	"HINDI": "Hindi", "TAMIL": "Tamil", "TELUGU": "Telugu",
	"JAPANESE": "Japanese", "JPN": "Japanese",
	"KOREAN": "Korean", "KOR": "Korean",
	"CHINESE": "Chinese", "MANDARIN": "Chinese", "CANTONESE": "Chinese",
	"THAI": "Thai", "VIETNAMESE": "Vietnamese",
	// The three-letter codes (ISO 639-2) that multi-language names list.
	"SPA": "Spanish", "POR": "Portuguese", "POL": "Polish", "HUN": "Hungarian", "CZE": "Czech", "CES": "Czech",
	"DUT": "Dutch", "NLD": "Dutch", "SWE": "Swedish", "NOR": "Norwegian", "FIN": "Finnish", "TUR": "Turkish",
	"ARA": "Arabic", "HIN": "Hindi", "GRE": "Greek", "UKR": "Ukrainian", "JAP": "Japanese", "DAN": "Danish",

	"CROATIAN": "Croatian", "HRV": "Croatian", "SERBIAN": "Serbian", "SRB": "Serbian", "BOSNIAN": "Bosnian",
	"SLOVENIAN": "Slovenian", "SLOVENE": "Slovenian", "BULGARIAN": "Bulgarian", "BUL": "Bulgarian",
}

// multiTags say a release holds several audio languages.
var multiTags = map[string]bool{
	"MULTI": true, "MULTIAUDIO": true, "DUAL": true, "DUALAUDIO": true,
}

// subbedTags say a release has subtitles burnt in or added, which changes
// nothing about the spoken language.
var subbedTags = map[string]bool{
	"VOSTFR": true, "SUBBED": true, "SUBFRENCH": true, "NLSUBBED": true, "MULTISUB": true, "MULTISUBS": true, "HARDSUB": true, "HARDSUBS": true,
}

// subtitleWords say that what stands before them is about subtitles.
var subtitleWords = map[string]bool{"SUB": true, "SUBS": true, "SUBTITLE": true, "SUBTITLES": true}

func dropLang(langs []string, name string) []string {
	out := langs[:0]
	for _, l := range langs {
		if l != name {
			out = append(out, l)
		}
	}
	return out
}

var languageSplit = regexp.MustCompile(`[\s._\-\[\]()+,{}]+`)

// detectLanguages reads the language tags out of the part of a release name
// that follows the title. It returns the audio languages named (each once, in
// the order they appear), whether the release says it holds several audio
// languages (MULTI, DUAL, or "DL" next to a language, as in German releases),
// and whether it is subtitled.
func detectLanguages(tail string) (langs []string, multi, subbed bool) {
	seen := map[string]bool{}
	dual, prev := false, ""
	multiBefore, pendingLang := false, ""
	for _, raw := range languageSplit.Split(tail, -1) {
		if raw == "" {
			continue
		}
		tag := strings.ToUpper(raw)
		switch {
		case subbedTags[tag]:
			subbed = true
		case subtitleWords[tag]:
			// "Multi-Subs", "ENG.SUBS": the languages before it are for reading.
			subbed = true
			if multiBefore {
				multi = false
				multiBefore = false
			}
			if pendingLang != "" {
				delete(seen, pendingLang)
				langs = dropLang(langs, pendingLang)
			}
		case multiTags[tag]:
			multi = true
			multiBefore = tag == "MULTI"
		case tag == "DL":
			// WEB-DL is a source, not "dual language".
			if prev != "WEB" && prev != "WEBDL" {
				dual = true
			}
		case languageTags[tag] != "":
			name := languageTags[tag]
			if !seen[name] {
				seen[name] = true
				langs = append(langs, name)
			}
		}
		pendingLang = ""
		if name := languageTags[tag]; name != "" && !subbedTags[tag] && !multiTags[tag] {
			pendingLang = name
		}
		if !multiTags[tag] {
			multiBefore = false
		}
		prev = tag
	}
	// "DL" is dual language: the named language plus the original one, wherever
	// it sits in the name (German.DL.AC3, German.BDRip.DL, DL.German).
	if dual && len(langs) > 0 {
		multi = true
	}
	return langs, multi, subbed
}

// firstTechTokenRe finds the first resolution, source, codec or audio tag of a
// release name: where the episode title of a TV release ends.
var firstTechTokenRe = regexp.MustCompile(`(?i)\b(?:2160p|1080p|720p|576p|480p|4K|WEB-?DL|WEBRip|WEB|BluRay|BDRip|BRRip|HDTV|PDTV|SDTV|HD-?Rip|DVDRip|DVD|Remux|x264|x265|h[. ]?26[45]|hevc|avc|xvid|AC3|EAC3|DDP?5\.1|DTS|AAC|Atmos)\b`)

// withoutEpisodeTitle takes the episode title out of the tail of a TV release
// name ("S03E04.French.Toast.1080p.WEB-DL"): the words between the episode
// marker and the first resolution, source or codec tag are the title, not tags.
// Only language tags that sit right against that first tag ("S01.GERMAN.DL.720p",
// "S01E02.FRENCH.720p") are kept. A name with no such tag is left as it was.
func withoutEpisodeTitle(tail string) string {
	loc := firstTechTokenRe.FindStringIndex(tail)
	if loc == nil {
		return tail
	}
	words := languageSplit.Split(tail[:loc[0]], -1)
	keep := 0
	for i := len(words) - 1; i >= 0; i-- {
		w := strings.ToUpper(words[i])
		if w == "" {
			continue
		}
		if languageTags[w] == "" && !multiTags[w] && !subbedTags[w] && w != "DL" {
			break
		}
		keep = len(words) - i
	}
	return strings.Join(words[len(words)-keep:], " ") + " " + tail[loc[0]:]
}

// LanguageNames lists every language a release tag can name, English first
// and the rest alphabetically.
func LanguageNames() []string {
	seen := map[string]bool{English: true}
	out := []string{English}
	var rest []string
	for _, name := range languageTags {
		if !seen[name] {
			seen[name] = true
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}
