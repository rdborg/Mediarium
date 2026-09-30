package subtitles_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/subtitles"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		lang   string // "" means undetectable
		forced bool
		sdh    bool
	}{
		// ISO 639-1
		{"two letter code", "Movie.2020.en.srt", "en", false, false},
		{"two letter upper case", "Movie.2020.FR.srt", "fr", false, false},
		// ISO 639-2 (terminologic and bibliographic)
		{"three letter eng", "Movie.eng.srt", "en", false, false},
		{"three letter fre", "Movie.fre.srt", "fr", false, false},
		{"three letter fra", "Movie.fra.srt", "fr", false, false},
		{"three letter ger", "Movie.ger.srt", "de", false, false},
		{"three letter deu", "Movie.deu.srt", "de", false, false},
		{"three letter chi", "Movie.chi.srt", "zh", false, false},
		// English and native names
		{"english name", "English.srt", "en", false, false},
		{"number prefix", "2_English.srt", "en", false, false},
		{"folder and number", "Subs/2_English.srt", "en", false, false},
		{"french name", "Subs/French.srt", "fr", false, false},
		{"native accented", "Subs/Português.srt", "pt", false, false},
		{"native name", "Subs/Deutsch.srt", "de", false, false},
		{"native cyrillic", "Subs/Русский.srt", "ru", false, false},
		{"spanish name", "Spanish.srt", "es", false, false},
		{"name at end of release name", "Movie.2020.1080p.BluRay.x264.Italian.srt", "it", false, false},
		// regions
		{"pt-BR code", "Movie.pt-BR.srt", "pt-BR", false, false},
		{"pt_br underscore", "Movie.pt_br.srt", "pt-BR", false, false},
		{"brazilian", "Movie.Brazilian.srt", "pt-BR", false, false},
		{"pt-PT code", "Movie.pt-PT.srt", "pt-PT", false, false},
		{"zh-CN", "Movie.zh-CN.srt", "zh-CN", false, false},
		{"zh-TW", "Movie.zh-TW.srt", "zh-TW", false, false},
		{"traditional chinese", "Movie.Chinese.Traditional.srt", "zh-TW", false, false},
		// flags
		{"forced", "movie.en.forced.srt", "en", true, false},
		{"forced word first", "Movie.Forced.English.srt", "en", true, false},
		{"foreign parts", "English (Foreign).srt", "en", true, false},
		{"sdh", "Movie.English.SDH.srt", "en", false, true},
		{"hearing impaired", "English (Hearing Impaired).srt", "en", false, true},
		{"trailing hi", "Movie.en.hi.srt", "en", false, true},
		{"cc", "Movie.en.cc.srt", "en", false, true},
		{"subs filler word", "Movie.English.Subs.srt", "en", false, false},
		{"trailing index", "Movie.English.2.srt", "en", false, false},
		// language from the folder
		{"parent folder", "Subs/English/forced.srt", "en", true, false},
		{"parent folder plain", "Subs/French/movie.srt", "fr", false, false},
		{"grandparent folder", "Subs/Dutch/extras/track.srt", "nl", false, false},
		// Hindi is only Hindi when spelled out
		{"hindi", "Movie.Hindi.srt", "hi", false, false},
		// not detectable
		{"no language", "Movie.2020.1080p.BluRay-GRP.srt", "", false, false},
		{"only a year", "Movie.2020.srt", "", false, false},
		{"subs folder only", "Subs/track.srt", "", false, false},
		{"bare hi flag", "movie.hi.srt", "", false, true},
		// A release name that mentions a language is not a subtitle language.
		{"language in the middle", "Movie.2020.FRENCH.1080p.BluRay-GRP.srt", "", false, false},
		{"release folder", "Movie.2020.FRENCH.1080p/track.srt", "", false, false},
		{"language too far up", "Dutch/a/b/track.srt", "", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := subtitles.DetectLanguage(tc.path)
			if ok != (tc.lang != "") {
				t.Fatalf("DetectLanguage(%q) ok = %v, want %v (%+v)", tc.path, ok, tc.lang != "", got)
			}
			if got.Lang != tc.lang || got.Forced != tc.forced || got.SDH != tc.sdh {
				t.Fatalf("DetectLanguage(%q) = %+v, want lang=%q forced=%v sdh=%v", tc.path, got, tc.lang, tc.forced, tc.sdh)
			}
		})
	}
}

func TestLanguageSatisfied(t *testing.T) {
	tests := []struct {
		name   string
		have   []string
		wanted string
		want   bool
	}{
		{"exact", []string{"en"}, "en", true},
		{"case", []string{"pt-br"}, "pt-BR", true},
		{"missing", []string{"en"}, "fr", false},
		{"base language covers a region", []string{"pt"}, "pt-BR", true},
		{"other region does not cover", []string{"pt-pt"}, "pt-BR", false},
		{"region does not cover base", []string{"pt-br"}, "pt", false},
		{"nothing there", nil, "en", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			have := map[string]bool{}
			for _, h := range tc.have {
				have[h] = true
			}
			if got := subtitles.LanguageSatisfied(have, tc.wanted); got != tc.want {
				t.Fatalf("LanguageSatisfied(%v, %q) = %v, want %v", tc.have, tc.wanted, got, tc.want)
			}
		})
	}
}
