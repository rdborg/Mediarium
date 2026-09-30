package quality

import (
	"strings"

	"github.com/rdborg/mediarium/internal/parser"
)

// DefaultLanguage is the audio language wanted when none has been chosen.
const DefaultLanguage = parser.English

// LanguageFit says how well a release's audio language suits the language
// the person wants.
type LanguageFit struct {
	// Rank orders acceptable releases, higher is better: 3 is the wanted
	// language alone (or nothing tagged when English is wanted), 2 is the
	// wanted language together with another one, 1 is a multi-language
	// release that does not say it holds the wanted language. Rejected
	// releases have rank 0.
	Rank int
	// Accepted is false for a release that is clearly in another language
	// only (ITALIAN, GERMAN, FRENCH... with nothing that includes the wanted
	// language). Automation never takes such a release; a manual grab is
	// still allowed.
	Accepted bool
	// Label names the languages the release says it has, in words for the
	// release list: "Italian + English", "Multi", "English". Empty when the
	// release carries no language tag at all.
	Label string
}

// FitLanguage judges a release for a person who wants preferred audio. An
// empty preferred language accepts everything. A release with no language tag
// is taken to be in English, the language nearly everything untagged is in.
func FitLanguage(r parser.Release, preferred string) LanguageFit {
	fit := LanguageFit{Accepted: true, Rank: 3, Label: languageLabel(r)}
	if preferred == "" {
		return fit
	}
	has, other := false, false
	for _, l := range r.Languages {
		if strings.EqualFold(l, preferred) {
			has = true
		} else {
			other = true
		}
	}
	untagged := len(r.Languages) == 0
	assumedEnglish := strings.EqualFold(preferred, parser.English)

	switch {
	case r.Multi && has:
		fit.Rank = 2
	case r.Multi:
		// Several audio tracks, and the original is nearly always one of
		// them. Fine, but behind a release that says so.
		fit.Rank = 1
	case untagged && assumedEnglish:
		fit.Rank = 3
	case untagged:
		fit.Rank = 2 // taken to be English, not the wanted language
	case has && !other:
		fit.Rank = 3
	case has:
		fit.Rank = 2
	case !assumedEnglish && onlyEnglish(r.Languages):
		fit.Rank = 2 // tagged English, and English is what untagged releases are assumed to be
	default:
		fit.Rank, fit.Accepted = 0, false
	}
	return fit
}

func onlyEnglish(langs []string) bool {
	return len(langs) == 1 && strings.EqualFold(langs[0], parser.English)
}

// languageLabel words the language tags of a release.
func languageLabel(r parser.Release) string {
	if len(r.Languages) == 0 {
		if r.Multi {
			return "Multi"
		}
		return ""
	}
	label := strings.Join(r.Languages, " + ")
	if r.Multi {
		label += " (multi)"
	}
	return label
}

// LanguageRank is FitLanguage(r, p.Language).Rank.
func (p Profile) LanguageRank(r parser.Release) int {
	return FitLanguage(r, p.Language).Rank
}

// LanguageOK reports whether a release's audio language suits the language
// this profile was loaded with (Language). It is true when none is set.
func (p Profile) LanguageOK(r parser.Release) bool {
	return FitLanguage(r, p.Language).Accepted
}

// LanguageReason explains, in plain words, why a release was turned down for
// its language, or "" when it was not.
func (p Profile) LanguageReason(r parser.Release) string {
	fit := FitLanguage(r, p.Language)
	if fit.Accepted {
		return ""
	}
	return strings.Join(r.Languages, " and ") + " audio, not " + p.Language
}
