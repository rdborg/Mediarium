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
