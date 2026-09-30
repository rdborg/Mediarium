package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/quality"
)

func results(titles ...string) []indexers.Result {
	out := make([]indexers.Result, len(titles))
	for i, t := range titles {
		out[i] = indexers.Result{Title: t, DownloadURL: "http://x/" + t, Protocol: indexers.ProtocolUsenet}
	}
	return out
}

func pickedTitle(r *indexers.Result) string {
	if r == nil {
		return ""
	}
	return r.Title
}

func profileWithLanguage(language string) quality.Profile {
	p := quality.Presets()[quality.PresetAny]
	p.Language = language
	return p
}

func TestPickMovieByLanguage(t *testing.T) {
	const (
		plain1080  = "Weapons.2025.1080p.WEB-DL.x264-GRP"
		plain720   = "Weapons.2025.720p.WEB-DL.x264-GRP"
		iTaEng     = "Weapons.2025.iTA-ENG.Bluray.1080p.x264-CYBER"
		italian    = "Weapons.2025.ITALIAN.Bluray.1080p.x264-CYBER"
		german     = "Weapons.2025.GERMAN.Bluray.1080p.x264-GRP"
		french     = "Weapons.2025.TRUEFRENCH.Bluray.1080p.x264-GRP"
		multiEng   = "Weapons.2025.MULTi.ENG.ITA.Bluray.1080p.x264-GRP"
		bareMulti  = "Weapons.2025.MULTi.Bluray.1080p.x264-GRP"
		italian4k  = "Weapons.2025.ITA.2160p.WEB-DL.x265-GRP"
		iTaEngRmx  = "Weapons.2025.iTA-ENG.1080p.BluRay.REMUX-GRP"
		englishRmx = "Weapons.2025.ENG.1080p.BluRay.REMUX-GRP"
	)
	tests := []struct {
		name     string
		language string
		titles   []string
		want     string
	}{
		{"an Italian-only release is never taken", "English", []string{italian}, ""},
		{"German only", "English", []string{german}, ""},
		{"French only", "English", []string{french}, ""},
		{"plain English beats Italian and English of higher quality", "English", []string{iTaEng, plain1080}, plain1080},
		{"plain English beats Italian and English, either order", "English", []string{plain1080, iTaEng}, plain1080},
		{"plain English in a lower tier still beats mixed", "English", []string{iTaEng, plain720}, plain720},
		{"Italian and English is taken when it is all there is", "English", []string{italian, german, iTaEng}, iTaEng},
		{"multi listing English is taken when nothing plain exists", "English", []string{italian, multiEng}, multiEng},
		{"bare multi comes after Italian and English", "English", []string{bareMulti, iTaEng}, iTaEng},
		{"bare multi is taken over nothing", "English", []string{german, bareMulti}, bareMulti},
		{"tagged English against untagged is a tie, so quality decides", "English", []string{englishRmx, plain1080}, englishRmx},
		{"quality still decides between plain releases", "English", []string{plain720, plain1080}, plain1080},
		{"no preference at all takes the best quality", "", []string{italian, plain720}, italian},
		{"Italian wanted takes Italian", "Italian", []string{plain1080, italian}, italian},
		{"Italian wanted takes Italian and English over plain English", "Italian", []string{plain1080, iTaEng}, iTaEng},
		{"Italian wanted still refuses German", "Italian", []string{german}, ""},
		{"the 4K release in another language is refused too", "English", []string{italian4k}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pickBestResult(results(tc.titles...), profileWithLanguage(tc.language), 2025)
			if pickedTitle(got) != tc.want {
				t.Fatalf("picked %q, want %q", pickedTitle(got), tc.want)
			}
		})
	}

	// The fallback profiles judge the language too.
	t.Run("a fallback profile does not let a foreign release through", func(t *testing.T) {
		p := profileWithLanguage("English")
		fb := quality.Presets()[quality.PresetAny]
		fb.Language = "English"
		p.FallbackProfiles = []quality.Profile{fb}
		if got := pickBestResult(results(italian), p, 2025); got != nil {
			t.Fatalf("picked %q", got.Title)
		}
	})

	// Upgrades over what is on disk.
	upgrades := []struct {
		name     string
		language string
		titles   []string
		want     string
	}{
		{"an Italian-only upgrade is refused", "English", []string{italian}, ""},
		{"plain English upgrade wins over Italian and English", "English", []string{iTaEngRmx, plain1080}, plain1080},
		{"Italian and English upgrade is taken when it is the only one", "English", []string{iTaEngRmx}, iTaEngRmx},
	}
	for _, tc := range upgrades {
		t.Run("upgrade: "+tc.name, func(t *testing.T) {
			p := profileWithLanguage(tc.language)
			p.Cutoff = quality.TierRemux1080p
			p.UpgradeAllowed = true
			got := pickUpgradeResult(results(tc.titles...), p, quality.TierWebDL720p, 2025)
			if pickedTitle(got) != tc.want {
				t.Fatalf("picked %q, want %q", pickedTitle(got), tc.want)
			}
		})
	}
}

func TestPickEpisodeByLanguage(t *testing.T) {
	series := library.Series{Title: "Show Name", Year: 2020}
	const (
		plain   = "Show.Name.S01E02.720p.HDTV.x264-GRP"
		iTaEng  = "Show.Name.S01E02.iTA.ENG.1080p.WEB-DL.x264-GRP"
		italian = "Show.Name.S01E02.ITA.1080p.WEB-DL.x264-GRP"
		german  = "Show.Name.S01E02.GERMAN.1080p.WEB-DL.x264-GRP"
		packGer = "Show.Name.S01.GERMAN.DL.1080p.WEB-DL.x264-GRP"
		packEng = "Show.Name.S01.1080p.WEB-DL.x264-GRP"
	)
	p := profileWithLanguage("English")

	episodes := []struct {
		name   string
		titles []string
		want   string
	}{
		{"foreign only episode is refused", []string{italian, german}, ""},
		{"plain English wins over Italian and English", []string{iTaEng, plain}, plain},
		{"Italian and English is taken when nothing plain exists", []string{italian, iTaEng}, iTaEng},
	}
	for _, tc := range episodes {
		t.Run(tc.name, func(t *testing.T) {
			got := pickTVResult(results(tc.titles...), series, 1, 2, p, nil)
			if pickedTitle(got) != tc.want {
				t.Fatalf("picked %q, want %q", pickedTitle(got), tc.want)
			}
		})
	}

	t.Run("a season pack with German dual language ranks behind a plain pack", func(t *testing.T) {
		got := pickTVResult(results(packGer, packEng), series, 1, 0, p, nil)
		if pickedTitle(got) != packEng {
			t.Fatalf("picked %q, want %q", pickedTitle(got), packEng)
		}
	})

	t.Run("an episode upgrade refuses a foreign release", func(t *testing.T) {
		current := quality.TierHDTV720p
		if got := pickTVResult(results(italian), series, 1, 2, p, &current); got != nil {
			t.Fatalf("picked %q", got.Title)
		}
	})
}
