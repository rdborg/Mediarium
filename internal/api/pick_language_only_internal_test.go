package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/quality"
)

// With "Only take releases in these languages" on, the picks leave out what
// does not say it is in a wanted language; with it off, they still take it.
func TestPickWithOnlyTheseLanguages(t *testing.T) {
	const (
		german    = "Weapons.2025.GERMAN.Bluray.1080p.x264-GRP"
		germanDL  = "Weapons.2025.German.DL.1080p.WEB-DL.x264-GRP"
		untagged  = "Weapons.2025.2160p.WEB-DL.x265-GRP"
		english   = "Weapons.2025.ENG.2160p.WEB-DL.x265-GRP"
		bareMulti = "Weapons.2025.MULTi.2160p.WEB-DL.x265-GRP"
		multiDeEn = "Weapons.2025.MULTi.GERMAN.ENG.2160p.WEB-DL.x265-GRP"
		italian   = "Weapons.2025.ITA.2160p.WEB-DL.x265-GRP"
	)
	only := func(language string, on bool) quality.Profile {
		p := profileWithLanguage(language)
		p.LanguageOnly = on
		return p
	}
	tests := []struct {
		name   string
		p      quality.Profile
		titles []string
		want   string
	}{
		{"off: untagged is still a fallback for German", only("German", false), []string{untagged}, untagged},
		{"on: untagged is refused for German", only("German", true), []string{untagged}, ""},
		{"on: English only is refused for German", only("German", true), []string{english, untagged}, ""},
		{"on: a bare multi is refused for German", only("German", true), []string{bareMulti}, ""},
		{"on: Italian is refused for German", only("German", true), []string{italian}, ""},
		{"on: German is taken over better quality that does not say German", only("German", true), []string{untagged, english, german}, german},
		{"on: a multi that names German is taken", only("German", true), []string{untagged, multiDeEn}, multiDeEn},
		{"on: German then English takes untagged as English", only("German,English", true), []string{untagged}, untagged},
		{"on: German then English still prefers German", only("German,English", true), []string{untagged, germanDL}, germanDL},
		{"on: English wanted keeps untagged", only("English", true), []string{untagged}, untagged},
		{"on: English wanted refuses a bare multi", only("English", true), []string{bareMulti}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pickBestResult(results(tc.titles...), tc.p, 2025)
			if pickedTitle(got) != tc.want {
				t.Fatalf("picked %q, want %q", pickedTitle(got), tc.want)
			}
		})
	}

	t.Run("an upgrade follows the switch", func(t *testing.T) {
		p := only("German", true)
		p.Cutoff = quality.TierRemux1080p
		p.UpgradeAllowed = true
		if got := pickUpgradeResult(results(untagged), p, quality.TierWebDL720p, "", 2025); got != nil {
			t.Fatalf("picked %q", got.Title)
		}
		if got := pickUpgradeResult(results(untagged, germanDL), p, quality.TierWebDL720p, "", 2025); pickedTitle(got) != germanDL {
			t.Fatalf("picked %q, want %q", pickedTitle(got), germanDL)
		}
	})

	t.Run("a fallback profile follows the switch too", func(t *testing.T) {
		p := only("German", true)
		p.FallbackProfiles = []quality.Profile{only("German", true)}
		if got := pickBestResult(results(untagged), p, 2025); got != nil {
			t.Fatalf("picked %q", got.Title)
		}
	})

	t.Run("episodes and season packs follow the switch", func(t *testing.T) {
		series := library.Series{Title: "Show Name", Year: 2020}
		p := only("German", true)
		const (
			plainEp = "Show.Name.S01E02.1080p.WEB-DL.x264-GRP"
			deEp    = "Show.Name.S01E02.GERMAN.720p.HDTV.x264-GRP"
			plainSn = "Show.Name.S01.1080p.WEB-DL.x264-GRP"
			dePack  = "Show.Name.S01.German.DL.1080p.WEB-DL.x264-GRP"
		)
		if got := pickTVResult(results(plainEp), series, 1, 2, p, nil); got != nil {
			t.Fatalf("picked %q", got.Title)
		}
		if got := pickTVResult(results(plainEp, deEp), series, 1, 2, p, nil); pickedTitle(got) != deEp {
			t.Fatalf("picked %q, want %q", pickedTitle(got), deEp)
		}
		if got := pickTVResult(results(plainSn, dePack), series, 1, 0, p, nil); pickedTitle(got) != dePack {
			t.Fatalf("picked %q, want %q", pickedTitle(got), dePack)
		}
	})
}

// With "Extra languages don't change the ranking" on, a multi-language release
// no longer goes above (or below) a plain one in the wanted language, so the
// better quality or the preferred terms decide. Off, it picks as before.
func TestPickIgnoringExtraLanguages(t *testing.T) {
	const (
		plain1080 = "Weapons.2025.German.1080p.BluRay.x265-2BA"
		dual720   = "Weapons.2025.German.DL.720p.WEB-DL.x264-GRP"
		dual1080  = "Weapons.2025.German.DL.1080p.BluRay.x265-FuN"
	)
	with := func(language string, ignore bool) quality.Profile {
		p := profileWithLanguage(language)
		p.IgnoreExtraLanguages = ignore
		return p
	}
	tests := []struct {
		name   string
		p      quality.Profile
		titles []string
		want   string
	}{
		{"off, German and English: the dual release is first", with("German,English", false), []string{plain1080, dual720}, dual720},
		{"on: the better quality wins over the dual release", with("German,English", true), []string{plain1080, dual720}, plain1080},
		{"on: equal quality, the preferred term decides", func() quality.Profile {
			p := with("German,English", true)
			p.Preferred = []quality.Preferred{{Term: "FuN", Score: 50}}
			return p
		}(), []string{plain1080, dual1080}, dual1080},
		{"on, one language: the dual release is not behind the plain one", with("German", true), []string{dual1080}, dual1080},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pickBestResult(results(tc.titles...), tc.p, 2025)
			if pickedTitle(got) != tc.want {
				t.Fatalf("picked %q, want %q", pickedTitle(got), tc.want)
			}
		})
	}
}
