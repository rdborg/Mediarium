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

var languageSplit = regexp.MustCompile(`[\s._\-\[\]()+,{}]+`)

// detectLanguages reads the language tags out of the part of a release name
// that follows the title. It returns the audio languages named (each once, in
// the order they appear), whether the release says it holds several audio
// languages (MULTI, DUAL, or "DL" after a language, as in German releases),
// and whether it is subtitled.
func detectLanguages(tail string) (langs []string, multi, subbed bool) {
	seen := map[string]bool{}
	prevWasLanguage := false
	for _, raw := range languageSplit.Split(tail, -1) {
		if raw == "" {
			continue
		}
		tag := strings.ToUpper(raw)
		switch {
		case subbedTags[tag]:
			subbed = true
			prevWasLanguage = false
		case multiTags[tag]:
			multi = true
			prevWasLanguage = false
		case tag == "DL" && prevWasLanguage:
			multi = true
			prevWasLanguage = false
		case languageTags[tag] != "":
			name := languageTags[tag]
			if !seen[name] {
				seen[name] = true
				langs = append(langs, name)
			}
			prevWasLanguage = true
		default:
			prevWasLanguage = false
		}
	}
	return langs, multi, subbed
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
