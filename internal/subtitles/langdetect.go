package subtitles

import (
	"path/filepath"
	"strings"
	"unicode"
)

// languageNames maps a canonical language code (the OpenSubtitles style used
// in the "subtitles.languages" setting and in "<video>.<code>.srt" file names)
// to the ways a subtitle file or folder may name that language: ISO 639-1 and
// 639-2 codes and English and native names. Every entry is lower case with
// accents removed (see fold).
var languageNames = []struct {
	code  string
	names []string
}{
	{"en", []string{"en", "eng", "english", "inglese", "anglais", "ingles", "englisch"}},
	{"es", []string{"es", "spa", "spanish", "espanol", "castellano", "castilian", "spagnolo", "espagnol"}},
	{"fr", []string{"fr", "fre", "fra", "french", "francais", "francese", "frances"}},
	{"de", []string{"de", "ger", "deu", "german", "deutsch", "tedesco", "allemand"}},
	{"it", []string{"it", "ita", "italian", "italiano", "italien"}},
	{"pt", []string{"pt", "por", "portuguese", "portugues", "portoghese"}},
	{"pt-PT", []string{"pt-pt", "ptpt"}},
	{"pt-BR", []string{"pt-br", "ptbr", "pob", "brazilian", "brasileiro", "brazilian-portuguese", "portuguese-brazilian", "portuguese-brazil", "portugues-brasil"}},
	{"nl", []string{"nl", "dut", "nld", "dutch", "nederlands", "olandese"}},
	{"sv", []string{"sv", "swe", "swedish", "svenska"}},
	{"da", []string{"da", "dan", "danish", "dansk"}},
	{"fi", []string{"fi", "fin", "finnish", "suomi"}},
	{"no", []string{"no", "nor", "nob", "nno", "nb", "nn", "norwegian", "norsk"}},
	{"pl", []string{"pl", "pol", "polish", "polski"}},
	{"cs", []string{"cs", "cze", "ces", "czech", "cesky"}},
	{"sk", []string{"sk", "slo", "slk", "slovak", "slovensky"}},
	{"sl", []string{"sl", "slv", "slovenian", "slovene"}},
	{"hu", []string{"hu", "hun", "hungarian", "magyar"}},
	{"ro", []string{"ro", "rum", "ron", "romanian", "romana"}},
	{"bg", []string{"bg", "bul", "bulgarian"}},
	{"hr", []string{"hr", "hrv", "scr", "croatian", "hrvatski"}},
	{"sr", []string{"sr", "srp", "scc", "serbian", "srpski"}},
	{"uk", []string{"uk", "ukr", "ukrainian"}},
	{"el", []string{"el", "gre", "ell", "greek", "ellinika", "ελληνικα", "ελληνικά"}},
	{"tr", []string{"tr", "tur", "turkish", "turkce"}},
	{"ru", []string{"ru", "rus", "russian", "russkij", "russkiy", "русский"}},
	{"ar", []string{"ar", "ara", "arabic", "العربية"}},
	{"he", []string{"he", "iw", "heb", "hebrew", "עברית"}},
	{"fa", []string{"fa", "per", "fas", "persian", "farsi"}},
	{"hi", []string{"hin", "hindi"}},
	{"ja", []string{"ja", "jpn", "japanese", "nihongo", "日本語"}},
	{"ko", []string{"ko", "kor", "korean", "한국어"}},
	{"zh", []string{"zh", "chi", "zho", "chinese", "中文"}},
	{"zh-CN", []string{"zh-cn", "zh-hans", "chs", "simplified", "simplified-chinese", "chinese-simplified", "简体", "简体中文"}},
	{"zh-TW", []string{"zh-tw", "zh-hant", "cht", "big5", "traditional", "traditional-chinese", "chinese-traditional", "繁體", "繁體中文"}},
	{"th", []string{"th", "tha", "thai"}},
	{"vi", []string{"vi", "vie", "vietnamese"}},
	{"id", []string{"id", "ind", "indonesian"}},
	{"ms", []string{"ms", "may", "msa", "malay"}},
	{"is", []string{"is", "ice", "isl", "icelandic"}},
	{"et", []string{"et", "est", "estonian"}},
	{"lv", []string{"lv", "lav", "latvian"}},
	{"lt", []string{"lt", "lit", "lithuanian"}},
	{"ca", []string{"ca", "cat", "catalan"}},
}

var languageByName = func() map[string]string {
	m := map[string]string{}
	for _, l := range languageNames {
		for _, n := range l.names {
			if _, dup := m[n]; !dup {
				m[n] = l.code
			}
		}
	}
	return m
}()

// flagWords mark a subtitle as something other than a plain full-dialogue
// track. "hi" (hearing impaired) is only a flag when it trails the name, as it
// is also Hindi's code.
var (
	forcedWords = map[string]bool{"forced": true, "foreign": true}
	sdhWords    = map[string]bool{"sdh": true, "cc": true, "hearing": true, "impaired": true}
	// fillerWords say "this is a subtitle file" and carry no information.
	fillerWords = map[string]bool{"sub": true, "subs": true, "subtitle": true, "subtitles": true, "default": true, "full": true, "text": true, "srt": true}
)

var foldReplacer = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a", "å", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o", "ø", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n", "ý", "y", "ÿ", "y", "š", "s", "ž", "z", "č", "c", "ř", "r", "ł", "l",
)

// fold lower-cases s and removes common Latin accents ("Português" -> "portugues").
func fold(s string) string { return foldReplacer.Replace(strings.ToLower(s)) }

func tokenize(s string) []string {
	return strings.FieldsFunc(fold(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// Detected is the language and flags read from a subtitle's name.
type Detected struct {
	Lang   string // canonical code, e.g. "en", "pt-BR"; empty when unknown
	Forced bool   // only foreign-language dialogue
	SDH    bool   // for the deaf and hard of hearing
}

// detectInName reads a single name (a file's base name without extension, or a
// folder name). The language is expected at the end ("Movie.en.srt",
// "2_English", "Movie.English.SDH"), optionally followed by flag words; only
// the tail is inspected so a release name that merely mentions a language
// ("Movie.2020.FRENCH.1080p") is never mistaken for a subtitle language.
func detectInName(name string) Detected {
	tokens := tokenize(name)
	var d Detected
	for _, t := range tokens {
		switch {
		case forcedWords[t]:
			d.Forced = true
		case sdhWords[t]:
			d.SDH = true
		}
	}
	end := len(tokens)
	for end > 0 {
		t := tokens[end-1]
		switch {
		case forcedWords[t], sdhWords[t], fillerWords[t], isNumber(t):
			end--
			continue
		case t == "hi" && end > 1: // trailing "hearing impaired" flag, not Hindi
			d.SDH = true
			end--
			continue
		}
		break
	}
	if end == 0 {
		return d
	}
	if end >= 2 {
		if code, ok := languageByName[tokens[end-2]+"-"+tokens[end-1]]; ok {
			d.Lang = code
			return d
		}
	}
	if code, ok := languageByName[tokens[end-1]]; ok {
		d.Lang = code
	}
	return d
}

// DetectLanguage works out which language a subtitle file is in from its path
// relative to the download folder: the file name first, then the two folders
// above it ("Subs/English/forced.srt"). ok is false when no language can be
// told, and such a file is not imported.
func DetectLanguage(relPath string) (d Detected, ok bool) {
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	file := parts[len(parts)-1]
	file = strings.TrimSuffix(file, filepath.Ext(file))
	d = detectInName(file)
	if d.Lang != "" {
		return d, true
	}
	for i := len(parts) - 2; i >= 0 && i >= len(parts)-3; i-- {
		if fd := detectInName(parts[i]); fd.Lang != "" {
			fd.Forced = fd.Forced || d.Forced
			fd.SDH = fd.SDH || d.SDH
			return fd, true
		}
	}
	return d, false
}

// baseLanguage is the part of a code before the region: "pt-BR" -> "pt".
func baseLanguage(code string) string {
	if i := strings.IndexAny(code, "-_"); i > 0 {
		return code[:i]
	}
	return code
}

// LanguageSatisfied reports whether the languages found next to a video (keys
// lower-cased, as languagesOnDisk builds them) include wanted. A file whose
// name gives only the base language ("pt") counts for any regional variant
// ("pt-BR").
func LanguageSatisfied(have map[string]bool, wanted string) bool {
	w := strings.ToLower(wanted)
	if have[w] {
		return true
	}
	if b := baseLanguage(w); b != w {
		return have[b]
	}
	return false
}
