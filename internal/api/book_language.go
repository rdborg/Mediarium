package api

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// The language of a book release (V4 D17, #128). Book release names rarely
// say their language, but many indexers file them under a category of their
// own per language ("EBook (DE)" as 7120 next to 7020). So a release's
// language is read from its name first, and when the name says nothing, from
// the name of the category the indexer filed it under. The wanted languages
// (Settings > Library > Quality, "Languages you want") then apply to books the
// way they apply to films: a release in another language is not taken by
// itself, and one in a wanted language goes first.

// standardBookCategories are the Newznab categories every indexer uses for
// books and audiobooks; their names say nothing about a language.
var standardBookCategories = map[int]bool{3000: true, 3030: true, 7000: true, 7010: true, 7020: true, 7030: true, 7040: true, 7050: true, 7060: true}

const (
	categoryNamesKeep  = 24 * time.Hour
	categoryNamesRetry = time.Hour
)

// categoryNames remembers each indexer's category names (from t=caps).
type categoryNames struct {
	mu sync.Mutex
	m  map[int64]categoryNamesEntry
}

type categoryNamesEntry struct {
	names map[int]string
	until time.Time
}

// categoryName is the name an indexer gives a category, read from its
// capabilities at most once a day. "" when it isn't known.
func (s *Server) categoryName(ctx context.Context, inst indexers.Instance, cat int) string {
	if inst.IsCardigann() || inst.SecretError != "" {
		return ""
	}
	c := &s.catNames
	c.mu.Lock()
	e, ok := c.m[inst.ID]
	c.mu.Unlock()
	if !ok || time.Now().After(e.until) {
		e = categoryNamesEntry{names: map[int]string{}, until: time.Now().Add(categoryNamesRetry)}
		inst.Tracker.CountRequest(inst.ID, 1)
		cctx, cancel := context.WithTimeout(ctx, indexers.FeedWait(inst.BaseURL))
		cats, err := indexers.NewNewznabClient(inst.Name, inst.BaseURL, inst.APIKey).Categories(cctx)
		cancel()
		if err == nil {
			for _, cc := range cats {
				e.names[cc.ID] = cc.Name
			}
			e.until = time.Now().Add(categoryNamesKeep)
		}
		c.mu.Lock()
		if c.m == nil {
			c.m = map[int64]categoryNamesEntry{}
		}
		c.m[inst.ID] = e
		c.mu.Unlock()
	}
	return e.names[cat]
}

// languageWords are the words a category name uses for a language: the
// English name, the language's own name and its two-letter code.
var languageWords = func() map[string]string {
	out := map[string]string{}
	own := map[string][]string{
		"German": {"deutsch", "deutsche"}, "French": {"français", "francais"}, "Spanish": {"español", "espanol"},
		"Italian": {"italiano"}, "Dutch": {"nederlands"}, "Polish": {"polski"}, "Russian": {"русский"},
		"Portuguese": {"português", "portugues"}, "Swedish": {"svenska"}, "Danish": {"dansk"}, "Norwegian": {"norsk"},
		"Finnish": {"suomi"}, "Hungarian": {"magyar"}, "Czech": {"čeština", "cestina"}, "Turkish": {"türkçe", "turkce"},
	}
	for _, code := range []string{"en", "it", "de", "fr", "es", "ru", "pt", "nl", "pl", "sv", "da", "nb", "fi", "tr", "el", "hu", "cs", "ro", "uk", "ja", "ko", "zh"} {
		name := quality.LanguageName(code)
		if name == "" {
			continue
		}
		out[strings.ToLower(name)] = name
		out["code:"+code] = name
		for _, w := range own[name] {
			out[w] = name
		}
	}
	out["code:ger"], out["code:eng"], out["code:fre"], out["code:spa"] = "German", "English", "French", "Spanish"
	return out
}()

// languageInCategory reads a language from a category name such as
// "EBook (DE)", "Hörbücher Deutsch" or "Books/French". A two-letter code
// counts only in brackets ("(DE)"), since "IT Books" is about computers; a
// three-letter one (GER, ENG) also in capitals. "" when the name names no
// language.
func languageInCategory(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) })
	for _, w := range words {
		if l, ok := languageWords[strings.ToLower(w)]; ok && len([]rune(w)) > 3 {
			return l
		}
		bracketed := strings.Contains(name, "("+w+")") || strings.Contains(name, "["+w+"]")
		if (len(w) == 2 && bracketed) || (len(w) == 3 && (bracketed || strings.ToUpper(w) == w)) {
			if l, ok := languageWords["code:"+strings.ToLower(w)]; ok {
				return l
			}
		}
	}
	return ""
}

// bookRelease is a book release read for its language: the name's own
// language tags, or else the language of the category it was filed under.
func (s *Server) bookRelease(ctx context.Context, res indexers.Result, byName map[string]indexers.Instance) parser.Release {
	rel := parser.Parse(res.Title)
	if len(rel.Languages) > 0 || rel.Multi {
		return rel
	}
	inst, ok := byName[res.IndexerName]
	if !ok {
		return rel
	}
	for _, cat := range res.Categories {
		if standardBookCategories[cat] {
			continue
		}
		if l := languageInCategory(s.categoryName(ctx, inst, cat)); l != "" {
			rel.Languages = []string{l}
			return rel
		}
	}
	return rel
}

// bookLanguageFits judges each release for the wanted languages: Rank orders
// them (higher first) and Accepted is false for one clearly in another
// language, which automation leaves alone.
func (s *Server) bookLanguageFits(ctx context.Context, results []indexers.Result, instances []indexers.Instance) []quality.LanguageFit {
	byName := map[string]indexers.Instance{}
	for _, inst := range instances {
		byName[inst.Name] = inst
	}
	preferred, only := s.preferredLanguage(), s.languageOnly()
	out := make([]quality.LanguageFit, len(results))
	for i, res := range results {
		out[i] = quality.FitLanguageOnly(s.bookRelease(ctx, res, byName), quality.ResolveOriginal(preferred, ""), only)
	}
	return out
}

// sortBooksByLanguage puts the releases in a wanted language first, keeping
// the order they had otherwise, and returns their fits in the new order.
func sortBooksByLanguage(results []indexers.Result, fits []quality.LanguageFit) ([]indexers.Result, []quality.LanguageFit) {
	idx := make([]int, len(results))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int { return fits[b].Rank - fits[a].Rank })
	rs := make([]indexers.Result, len(results))
	fs := make([]quality.LanguageFit, len(results))
	for i, j := range idx {
		rs[i], fs[i] = results[j], fits[j]
	}
	return rs, fs
}
