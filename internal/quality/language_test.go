package quality_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

func TestFitLanguage(t *testing.T) {
	tests := []struct {
		name      string
		title     string
		preferred string
		accepted  bool
		rank      int
		label     string
	}{
		{"untagged is English", "Movie.2020.1080p.BluRay.x264-GRP", "English", true, 3, ""},
		{"English tag", "Movie.2020.ENG.1080p.BluRay.x264-GRP", "English", true, 3, "English"},
		{"Italian only", "Movie.2020.ITA.1080p.BluRay.x264-GRP", "English", false, 0, "Italian"},
		{"German only", "Movie.2020.GERMAN.1080p.BluRay.x264-GRP", "English", false, 0, "German"},
		{"French only", "Movie.2020.FRENCH.1080p.BluRay.x264-GRP", "English", false, 0, "French"},
		{"TrueFrench only", "Movie.2020.TRUEFRENCH.1080p.BluRay.x264-GRP", "English", false, 0, "French"},
		{"two foreign languages, nothing that includes English", "Movie.2020.ITA.FRENCH.1080p.BluRay.x264-GRP", "English", false, 0, "Italian + French"},
		{"Italian and English is fine but behind plain English", "Weapons.2025.iTA-ENG.Bluray.1080p.x264-CYBER", "English", true, 2, "Italian + English"},
		{"multi listing English", "Movie.2020.MULTi.ITA.ENG.1080p.BluRay.x264-GRP", "English", true, 2, "Italian + English (multi)"},
		{"bare multi", "Movie.2020.MULTi.1080p.BluRay.x264-GRP", "English", true, 1, "Multi"},
		{"bare dual", "Movie.2020.DUAL.1080p.BluRay.x264-GRP", "English", true, 1, "Multi"},
		{"German dual language", "Movie.2020.German.DL.1080p.BluRay.x264-GRP", "English", true, 1, "German (multi)"},
		{"subtitled French", "Movie.2020.VOSTFR.1080p.BluRay.x264-GRP", "English", true, 3, ""},
		{"a language in the title only", "The.Italian.Job.2003.1080p.BluRay.x264-GRP", "English", true, 3, ""},

		{"Italian wanted, Italian tagged", "Movie.2020.ITA.1080p.BluRay.x264-GRP", "Italian", true, 3, "Italian"},
		{"Italian wanted, Italian and English", "Movie.2020.iTA-ENG.1080p.BluRay.x264-GRP", "Italian", true, 2, "Italian + English"},
		{"Italian wanted, untagged English", "Movie.2020.1080p.BluRay.x264-GRP", "Italian", true, 2, ""},
		{"Italian wanted, English tagged", "Movie.2020.ENG.1080p.BluRay.x264-GRP", "Italian", true, 2, "English"},
		{"Italian wanted, German only", "Movie.2020.GERMAN.1080p.BluRay.x264-GRP", "Italian", false, 0, "German"},
		{"Italian wanted, multi with Italian", "Movie.2020.MULTi.ITA.1080p.BluRay.x264-GRP", "Italian", true, 2, "Italian (multi)"},

		{"no preference accepts everything", "Movie.2020.GERMAN.1080p.BluRay.x264-GRP", "", true, 3, "German"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := quality.FitLanguage(parser.Parse(tc.title), tc.preferred)
			if got.Accepted != tc.accepted || got.Rank != tc.rank || got.Label != tc.label {
				t.Fatalf("fit = %+v, want accepted=%v rank=%d label=%q", got, tc.accepted, tc.rank, tc.label)
			}
		})
	}
}

// The profile only turns a release away for its language when it was given a
// preferred language, and says why in plain words.
func TestProfileLanguage(t *testing.T) {
	p := quality.Presets()[quality.Preset1080p]
	italian := parser.Parse("Weapons.2025.ITA.Bluray.1080p.x264-CYBER")
	plain := parser.Parse("Weapons.2025.Bluray.1080p.x264-CYBER")

	if !p.Accepts(italian) || p.LanguageReason(italian) != "" {
		t.Fatal("a profile with no language accepts any language")
	}

	p.Language = "English"
	if p.Accepts(italian) {
		t.Error("an Italian-only release should not be accepted when English is wanted")
	}
	if got, want := p.LanguageReason(italian), "Italian audio, not English"; got != want {
		t.Errorf("reason = %q, want %q", got, want)
	}
	if !p.Accepts(plain) || p.LanguageReason(plain) != "" {
		t.Error("an untagged release should be accepted")
	}
	if p.IsUpgradeOverTier(quality.TierWebDL720p, italian) {
		t.Error("an Italian-only release should not be an upgrade")
	}
	if !p.IsUpgradeOverTier(quality.TierWebDL720p, plain) {
		t.Error("an untagged better release should be an upgrade")
	}
	if p.LanguageRank(plain) <= p.LanguageRank(parser.Parse("Weapons.2025.iTA-ENG.Bluray.1080p.x264-CYBER")) {
		t.Error("plain English should rank above Italian and English")
	}
}

func TestFitLanguageSeveralLanguages(t *testing.T) {
	rank := func(title, preferred string) (int, bool) {
		fit := quality.FitLanguage(parser.Parse(title), preferred)
		return fit.Rank, fit.Accepted
	}
	// German first, Italian second: each is taken, German ranks above Italian,
	// and a language that is on neither list is not.
	de, okDE := rank("Movie.2020.GERMAN.1080p.BluRay.x264-GRP", "German,Italian")
	it, okIT := rank("Movie.2020.ITA.1080p.BluRay.x264-GRP", "German,Italian")
	fr, okFR := rank("Movie.2020.FRENCH.1080p.BluRay.x264-GRP", "German,Italian")
	if !okDE || !okIT || okFR || fr != 0 || de <= it {
		t.Errorf("German %d/%v, Italian %d/%v, French %d/%v", de, okDE, it, okIT, fr, okFR)
	}
	// A multi release with one of the languages counts.
	if _, ok := rank("Movie.2020.MULTi.ITA.ENG.1080p.BluRay.x264-GRP", "German,Italian"); !ok {
		t.Error("a multi release holding Italian should be accepted")
	}
	// One language keeps the old numbers.
	if r, ok := rank("Movie.2020.1080p.BluRay.x264-GRP", "English"); r != 3 || !ok {
		t.Errorf("single language: %d/%v", r, ok)
	}
}

// With "Only take releases in these languages" on, a release has to say it is in
// a wanted language. With it off, nothing changes from FitLanguage.
func TestFitLanguageOnly(t *testing.T) {
	const (
		untagged  = "Movie.2020.1080p.BluRay.x264-GRP"
		english   = "Movie.2020.ENG.1080p.BluRay.x264-GRP"
		german    = "Movie.2020.GERMAN.1080p.BluRay.x264-GRP"
		germanEng = "Movie.2020.GERMAN.ENG.1080p.BluRay.x264-GRP"
		italian   = "Movie.2020.ITA.1080p.BluRay.x264-GRP"
		bareMulti = "Movie.2020.MULTi.1080p.BluRay.x264-GRP"
		bareDual  = "Movie.2020.DUAL.1080p.BluRay.x264-GRP"
		multiDeEn = "Movie.2020.MULTi.GERMAN.ENG.1080p.BluRay.x264-GRP"
		multiItEn = "Movie.2020.MULTi.ITA.ENG.1080p.BluRay.x264-GRP"
		germanDL  = "Movie.2020.German.DL.1080p.BluRay.x264-GRP"

		germanBDRipDL = "Movie.2020.German.BDRip.DL.1080p.x265.AAC-2BA"
		subtitled     = "Movie.2020.VOSTFR.1080p.BluRay.x264-GRP"
		titleWords    = "The.Italian.Job.2003.1080p.BluRay.x264-GRP"
	)
	tests := []struct {
		name      string
		title     string
		preferred string
		only      bool
		accepted  bool
		rank      int
	}{
		// German only: nothing but a release that says it is German is taken.
		{"German only, off: untagged is a fallback", untagged, "German", false, true, 2},
		{"German only, on: untagged is turned down", untagged, "German", true, false, 0},
		{"German only, off: English tag is a fallback", english, "German", false, true, 2},
		{"German only, on: English tag is turned down", english, "German", true, false, 0},
		{"German only, off: bare multi is a fallback", bareMulti, "German", false, true, 1},
		{"German only, on: bare multi is turned down", bareMulti, "German", true, false, 0},
		{"German only, on: bare dual is turned down", bareDual, "German", true, false, 0},
		{"German only, off: multi without German is a fallback", multiItEn, "German", false, true, 1},
		{"German only, on: multi without German is turned down", multiItEn, "German", true, false, 0},
		{"German only, on: multi naming German is taken", multiDeEn, "German", true, true, 2},
		{"German only, on: German.DL is taken", germanDL, "German", true, true, 2},
		{"German only, on: German is taken", german, "German", true, true, 3},
		{"German only, off: German is taken", german, "German", false, true, 3},
		{"German only, on: German and English is taken", germanEng, "German", true, true, 2},
		{"German only, on: Italian is still turned down", italian, "German", true, false, 0},
		{"German only, on: a subtitle tag is not a language", subtitled, "German", true, false, 0},
		{"German only, on: a word in the title is not a language", titleWords, "German", true, false, 0},

		// English wanted: untagged and English are what it asks for.
		{"English, on: untagged is taken first", untagged, "English", true, true, 3},
		{"English, on: English tag is taken first", english, "English", true, true, 3},
		{"English, on: Italian is turned down", italian, "English", true, false, 0},
		{"English, off: bare multi is a fallback", bareMulti, "English", false, true, 1},
		{"English, on: bare multi is turned down", bareMulti, "English", true, false, 0},
		{"English, on: multi naming English is taken", multiItEn, "English", true, true, 2},

		// Several languages: a release has to suit one of them.
		{"German then English, on: untagged is taken as English", untagged, "German,English", true, true, 3 + 4},
		{"German then English, on: German ranks above English", german, "German,English", true, true, 3 + 8},
		{"German then English, on: English tag is taken", english, "German,English", true, true, 3 + 4},
		{"German then English, on: Italian is turned down", italian, "German,English", true, false, 0},
		{"German then English, on: bare multi is turned down", bareMulti, "German,English", true, false, 0},
		{"German then Italian, on: untagged is turned down", untagged, "German,Italian", true, false, 0},
		{"German then Italian, on: English tag is turned down", english, "German,Italian", true, false, 0},
		{"German then Italian, off: untagged is a fallback", untagged, "German,Italian", false, true, 2 + 8},
		{"German then Italian, on: multi naming Italian is taken, above Italian alone", multiItEn, "German,Italian", true, true, 4 + 4},

		// Several languages: a multi release that names one also carries the others.
		{"German then English: German.DL goes above plain German", germanDL, "German,English", true, true, 4 + 8},
		{"German then English: German.BDRip.DL goes above plain German", germanBDRipDL, "German,English", false, true, 4 + 8},
		{"German then English: multi German+English goes above plain German", multiDeEn, "German,English", true, true, 4 + 8},
		{"German then English: plain German", german, "German,English", false, true, 3 + 8},
		{"English then German: German.DL is not turned down", germanDL, "English,German", false, true, 1 + 8},
		{"English then German, on: German.DL needs German named", germanDL, "English,German", true, true, 4 + 4},
		{"English then German: plain German", german, "English,German", true, true, 3 + 4},
		{"English then German: untagged English first", untagged, "English,German", true, true, 3 + 8},
		{"German then English: multi without either is turned down when only", bareMulti, "German,English", true, false, 0},
		{"one language: German.BDRip.DL stays behind plain German", germanBDRipDL, "German", true, true, 2},

		// No language wanted: the switch has nothing to hold against a release.
		{"no language, on: everything is taken", german, "", true, true, 3},
		{"no language, on: untagged is taken", untagged, "", true, true, 3},
		{"only the original, still unknown, on: everything is taken", bareMulti, "Original", true, true, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := quality.FitLanguageOnly(parser.Parse(tc.title), tc.preferred, tc.only)
			if got.Accepted != tc.accepted || got.Rank != tc.rank {
				t.Fatalf("fit = %+v, want accepted=%v rank=%d", got, tc.accepted, tc.rank)
			}
			// With the switch off it is exactly FitLanguage.
			if plain := quality.FitLanguage(parser.Parse(tc.title), tc.preferred); !tc.only && plain != got {
				t.Errorf("FitLanguage = %+v, but FitLanguageOnly(off) = %+v", plain, got)
			}
		})
	}
}

// The profile carries the switch the way it carries the language: the
// decisions automation makes (accept, rank, upgrade) and the reason shown in the
// release list all follow it.
func TestProfileLanguageOnly(t *testing.T) {
	p := quality.Presets()[quality.Preset1080p]
	p.Language = "German"
	untagged := parser.Parse("Weapons.2025.Bluray.1080p.x264-CYBER")
	english := parser.Parse("Weapons.2025.ENG.Bluray.1080p.x264-CYBER")
	bareMulti := parser.Parse("Weapons.2025.MULTi.Bluray.1080p.x264-CYBER")
	multi := parser.Parse("Weapons.2025.MULTi.ITA.ENG.Bluray.1080p.x264-CYBER")
	german := parser.Parse("Weapons.2025.GERMAN.Bluray.1080p.x264-CYBER")
	italian := parser.Parse("Weapons.2025.ITA.Bluray.1080p.x264-CYBER")

	for _, rel := range []parser.Release{untagged, english, bareMulti, multi, german} {
		if !p.Accepts(rel) || p.LanguageReason(rel) != "" {
			t.Fatalf("with the switch off %+v should be accepted (%q)", rel.Languages, p.LanguageReason(rel))
		}
	}

	p.LanguageOnly = true
	reasons := []struct {
		name string
		rel  parser.Release
		want string
	}{
		{"untagged", untagged, "Untagged release, taken to be English, not German"},
		{"English only", english, "English audio, not German"},
		{"bare multi", bareMulti, "Multi-language release that doesn't name German"},
		{"multi without German", multi, "Multi-language release with Italian and English audio, not German"},
		{"another language", italian, "Italian audio, not German"},
		{"German", german, ""},
	}
	for _, tc := range reasons {
		if got := p.LanguageReason(tc.rel); got != tc.want {
			t.Errorf("%s: reason = %q, want %q", tc.name, got, tc.want)
		}
		if p.LanguageOK(tc.rel) != (tc.want == "") || p.Accepts(tc.rel) != (tc.want == "") {
			t.Errorf("%s: accepted = %v, want %v", tc.name, p.Accepts(tc.rel), tc.want == "")
		}
		if (tc.want == "") != (p.LanguageRank(tc.rel) > 0) {
			t.Errorf("%s: rank = %d", tc.name, p.LanguageRank(tc.rel))
		}
	}
	if p.IsUpgradeOverTier(quality.TierWebDL720p, untagged) {
		t.Error("an untagged release should not be an upgrade when only German is wanted")
	}
	if !p.IsUpgradeOverTier(quality.TierWebDL720p, german) {
		t.Error("a German release should still be an upgrade")
	}

	// A list that names English keeps untagged releases.
	p.Language = "German,English"
	if !p.Accepts(untagged) || !p.Accepts(english) || p.Accepts(bareMulti) {
		t.Error("with English on the list, untagged and English are taken and a bare multi is not")
	}
	// A reason never mentions "Original", which stands for a language not yet known.
	p.Language = "Original,German"
	if got, want := p.LanguageReason(italian), "Italian audio, not German"; got != want {
		t.Errorf("reason = %q, want %q", got, want)
	}
}

func TestResolveOriginal(t *testing.T) {
	for _, tc := range []struct{ list, orig, want string }{
		{"Original,English", "Japanese", "Japanese,English"},
		{"English,Original", "English", "English"},
		{"Original,English", "", "English"},
		{"Original", "", ""},
		{"English", "Japanese", "English"},
	} {
		if got := quality.ResolveOriginal(tc.list, tc.orig); got != tc.want {
			t.Errorf("ResolveOriginal(%q, %q) = %q, want %q", tc.list, tc.orig, got, tc.want)
		}
	}
	if quality.LanguageName("ja") != "Japanese" || quality.LanguageName("xx") != "" {
		t.Error("LanguageName")
	}
	for iso, want := range map[string]string{"hr": "Croatian", "sr": "Serbian", "bs": "Bosnian", "sl": "Slovenian", "bg": "Bulgarian"} {
		if got := quality.LanguageName(iso); got != want {
			t.Errorf("LanguageName(%q) = %q, want %q", iso, got, want)
		}
	}
}

// With "Extra languages don't change the ranking" on, a release that has a
// wanted language ranks the same whether it names only that language, adds
// others or is a multi-language release holding it. The order of the wanted
// languages still counts.
func TestFitLanguageIgnoreExtra(t *testing.T) {
	const (
		plain   = "Movie.2020.GERMAN.1080p.BluRay.x264-GRP"
		dual    = "Movie.2020.German.DL.1080p.BluRay.x264-GRP"
		multi   = "Movie.2020.MULTi.GERMAN.ENG.1080p.BluRay.x264-GRP"
		english = "Movie.2020.ENG.1080p.BluRay.x264-GRP"
		italian = "Movie.2020.ITA.1080p.BluRay.x264-GRP"
	)
	rank := func(title, preferred string, ignore bool) int {
		return quality.FitLanguageWith(parser.Parse(title), preferred, false, ignore).Rank
	}
	tests := []struct {
		name      string
		preferred string
		a, b      string
		ignore    bool
		// want is the sign of rank(a) - rank(b): 1, 0 or -1.
		want int
	}{
		{"one language, off: plain above multi", "German", plain, multi, false, 1},
		{"one language, on: plain and multi tie", "German", plain, multi, true, 0},
		{"one language, on: plain and dual tie", "German", plain, dual, true, 0},
		{"several, off: multi above plain", "German,English", multi, plain, false, 1},
		{"several, on: multi and plain tie", "German,English", multi, plain, true, 0},
		{"several, on: dual and plain tie", "German,English", dual, plain, true, 0},
		{"several, on: the first language still wins", "German,English", plain, english, true, 1},
		{"several, on: a later language ranks below", "German,Italian", italian, multi, true, -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, b := rank(tc.a, tc.preferred, tc.ignore), rank(tc.b, tc.preferred, tc.ignore)
			got := 0
			if a > b {
				got = 1
			} else if a < b {
				got = -1
			}
			if got != tc.want {
				t.Fatalf("rank(%q)=%d rank(%q)=%d, want sign %d", tc.a, a, tc.b, b, tc.want)
			}
		})
	}
	// The switch changes ranking only: what is accepted stays the same.
	for _, title := range []string{plain, dual, multi, english, italian} {
		on := quality.FitLanguageWith(parser.Parse(title), "German,English", false, true).Accepted
		off := quality.FitLanguageWith(parser.Parse(title), "German,English", false, false).Accepted
		if on != off {
			t.Errorf("%q accepted %v with the switch on, %v off", title, on, off)
		}
	}
	p := quality.Profile{Language: "German,English", IgnoreExtraLanguages: true}
	if p.LanguageRank(parser.Parse(multi)) != p.LanguageRank(parser.Parse(plain)) {
		t.Error("Profile.LanguageRank ignores the switch")
	}
}
