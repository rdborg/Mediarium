package mediainfo

import "strings"

// languages lists the common languages: the two-letter code the rest of the
// app uses, then the three-letter code files use (the "B" form first, which is
// what Radarr and Sonarr show, then the "T" form when it differs).
var languages = [][]string{
	{"en", "eng"}, {"fr", "fre", "fra"}, {"de", "ger", "deu"}, {"es", "spa"}, {"it", "ita"},
	{"pt", "por"}, {"nl", "dut", "nld"}, {"ru", "rus"}, {"uk", "ukr"}, {"pl", "pol"}, {"ja", "jpn"},
	{"ko", "kor"}, {"zh", "chi", "zho", "cmn"}, {"ar", "ara"}, {"hi", "hin"}, {"tr", "tur"},
	{"sv", "swe"}, {"no", "nor", "nob", "nno"}, {"da", "dan"}, {"fi", "fin"}, {"cs", "cze", "ces"},
	{"sk", "slo", "slk"}, {"hu", "hun"}, {"ro", "rum", "ron"}, {"bg", "bul"}, {"el", "gre", "ell"},
	{"he", "heb"}, {"th", "tha"}, {"vi", "vie"}, {"id", "ind"}, {"ms", "may", "msa"}, {"hr", "hrv"},
	{"sr", "srp"}, {"sl", "slv"}, {"lt", "lit"}, {"lv", "lav"}, {"et", "est"}, {"is", "ice", "isl"},
	{"ca", "cat"}, {"eu", "baq", "eus"}, {"gl", "glg"}, {"fa", "per", "fas"}, {"bn", "ben"},
	{"ta", "tam"}, {"te", "tel"}, {"ur", "urd"}, {"af", "afr"}, {"sq", "alb", "sqi"}, {"mk", "mac", "mkd"},
	{"bs", "bos"}, {"ka", "geo", "kat"}, {"hy", "arm", "hye"}, {"la", "lat"}, {"ga", "gle"},
	{"cy", "wel", "cym"}, {"tl", "fil", "tgl"},
}

var (
	twoLetter   = map[string]string{} // any three-letter code to the two-letter one
	threeLetter = map[string]string{} // a two-letter code to the three-letter one
)

func init() {
	for _, l := range languages {
		threeLetter[l[0]] = l[1]
		for _, c := range l[1:] {
			twoLetter[c] = l[0]
		}
	}
}

// language turns a track's language tag ("eng", "pt-BR", "en") into a short
// lower-case code; "" for undetermined. A tag with a region keeps only the
// language ("pt-BR" is "pt"), as a filter for "Portuguese audio" wants.
func language(tags ...string) string {
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if i := strings.IndexAny(tag, "-_"); i > 0 {
			tag = tag[:i]
		}
		switch tag {
		case "", "und", "zxx", "mul", "mis", "qaa":
			continue
		}
		if c, ok := twoLetter[tag]; ok {
			return c
		}
		return tag
	}
	return ""
}

var languageNames = map[string]string{
	"english": "en", "french": "fr", "german": "de", "spanish": "es", "italian": "it", "portuguese": "pt",
	"dutch": "nl", "russian": "ru", "ukrainian": "uk", "polish": "pl", "japanese": "ja", "korean": "ko",
	"chinese": "zh", "mandarin": "zh", "arabic": "ar", "hindi": "hi", "turkish": "tr", "swedish": "sv",
	"norwegian": "no", "danish": "da", "finnish": "fi", "czech": "cs", "hungarian": "hu", "romanian": "ro",
	"greek": "el", "hebrew": "he", "thai": "th", "vietnamese": "vi", "indonesian": "id",
	"persian": "fa", "farsi": "fa",
}

// LanguageCode turns what a person typed ("German", "ger", "de", "pt-BR")
// into the code Info uses ("de"); "" when it is empty.
func LanguageCode(s string) string {
	if c, ok := languageNames[strings.ToLower(strings.TrimSpace(s))]; ok {
		return c
	}
	return language(s)
}

// ISO3 is the three-letter code for one of the codes Info uses, as Radarr and
// Sonarr show languages ("de" is "ger"); a code it doesn't know comes back as it is.
func ISO3(code string) string {
	if c, ok := threeLetter[code]; ok {
		return c
	}
	return code
}

// languageEnglish names the codes Info uses, as the release names of the
// naming tokens write languages ("English", "German").
var languageEnglish = map[string]string{
	"en": "English", "fr": "French", "de": "German", "es": "Spanish", "it": "Italian", "pt": "Portuguese",
	"nl": "Dutch", "ru": "Russian", "uk": "Ukrainian", "pl": "Polish", "ja": "Japanese", "ko": "Korean",
	"zh": "Chinese", "ar": "Arabic", "hi": "Hindi", "tr": "Turkish", "sv": "Swedish", "no": "Norwegian",
	"da": "Danish", "fi": "Finnish", "cs": "Czech", "sk": "Slovak", "hu": "Hungarian", "ro": "Romanian",
	"bg": "Bulgarian", "el": "Greek", "he": "Hebrew", "th": "Thai", "vi": "Vietnamese", "id": "Indonesian",
	"ms": "Malay", "hr": "Croatian", "sr": "Serbian", "sl": "Slovenian", "lt": "Lithuanian", "lv": "Latvian",
	"et": "Estonian", "is": "Icelandic", "ca": "Catalan", "eu": "Basque", "gl": "Galician", "fa": "Persian",
	"bn": "Bengali", "ta": "Tamil", "te": "Telugu", "ur": "Urdu", "af": "Afrikaans", "sq": "Albanian",
	"mk": "Macedonian", "bs": "Bosnian", "ka": "Georgian", "hy": "Armenian", "la": "Latin", "ga": "Irish",
	"cy": "Welsh", "tl": "Filipino",
}

// LanguageName is the English name of one of the codes Info uses ("de" is
// "German"); a code it doesn't know comes back as it is, in capitals.
func LanguageName(code string) string {
	if n, ok := languageEnglish[code]; ok {
		return n
	}
	return strings.ToUpper(code)
}
