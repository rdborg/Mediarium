package api

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/quality"
)

// Limits for quality profiles (the same ones the quality package enforces).
const (
	profileNameMax  = 60
	profileTermMax  = 60
	profileMaxTerms = 50
	profileMaxScore = 10000
)

// checkProfileBasics checks the parts every quality profile has: a name, the
// qualities it allows and a cutoff among them. validTier says whether a name
// is one of the qualities the app knows.
func checkProfileBasics(name string, allowed []string, cutoff string, validTier func(string) bool) string {
	if m := firstProblem(
		checkRequired(name, "Give this profile a name, for example Movies 1080p."),
		checkMaxLen(name, "The name", profileNameMax),
		checkNoControl(name, "The name"),
	); m != "" {
		return m
	}
	if len(allowed) == 0 {
		return "Pick at least one quality this profile can download."
	}
	seen := map[string]bool{}
	for _, t := range allowed {
		if !validTier(t) {
			return fmt.Sprintf("%q isn't a quality Mediarium knows. Pick from the list.", t)
		}
		seen[t] = true
	}
	if strings.TrimSpace(cutoff) == "" {
		return "Pick the quality to stop upgrading at (the cutoff)."
	}
	if !seen[cutoff] {
		return "The cutoff has to be one of the qualities this profile allows."
	}
	return ""
}

// checkProfileTerms checks the word lists of a movie or TV profile: terms a
// release must or must not contain, and preferred words with a score.
func checkProfileTerms(mustContain, mustNotContain []string, preferred []preferredPayload) string {
	term := func(v, label string) string {
		v = strings.TrimSpace(v)
		if utf8.RuneCountInString(v) > profileTermMax || len(v) > profileTermMax {
			return fmt.Sprintf("Each word in %s can be at most %d characters long.", label, profileTermMax)
		}
		if strings.ContainsAny(v, "|\n\r") {
			return fmt.Sprintf("The words in %s can't contain | or line breaks.", label)
		}
		return ""
	}
	for _, l := range []struct {
		terms []string
		label string
	}{{mustContain, "the required words"}, {mustNotContain, "the excluded words"}} {
		if len(l.terms) > profileMaxTerms {
			return fmt.Sprintf("Use at most %d words in %s.", profileMaxTerms, l.label)
		}
		for _, t := range l.terms {
			if m := term(t, l.label); m != "" {
				return m
			}
		}
	}
	if len(preferred) > profileMaxTerms {
		return fmt.Sprintf("Use at most %d preferred words.", profileMaxTerms)
	}
	for _, p := range preferred {
		if m := term(p.Term, "the preferred words"); m != "" {
			return m
		}
		if p.Score < -profileMaxScore || p.Score > profileMaxScore {
			return fmt.Sprintf("The score for %q must be a number between -%d and %d.", p.Term, profileMaxScore, profileMaxScore)
		}
	}
	return ""
}

func checkQualityProfileRequest(req profileRequest) string {
	return firstProblem(
		checkProfileBasics(req.Name, req.Allowed, req.Cutoff, func(t string) bool {
			for _, v := range quality.AllTiers() {
				if string(v) == t {
					return true
				}
			}
			return false
		}),
		checkProfileTerms(req.MustContain, req.MustNotContain, req.Preferred),
	)
}

func checkMusicProfileRequest(req musicProfileRequest) string {
	return checkProfileBasics(req.Name, req.Allowed, req.Cutoff, music.ValidTier)
}
