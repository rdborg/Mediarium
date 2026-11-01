package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// A person who wants German audio finds German releases, and those are named by
// the German title ("Der Herr der Ringe 2001 German ..."), not the English one
// a film is added under. So for each movie and show the other titles TMDB knows
// in the wanted languages are kept (library.Movie.AltTitles), the indexers are
// asked for each of them besides the main title, and a release named by one of
// them is taken for the title. Nothing of this happens while English is the
// only audio language wanted.

const (
	titleNamesInterval = 20 * time.Minute
	titleNamesBatch    = 25                     // titles asked about in one run
	titleNamesPause    = 400 * time.Millisecond // between two TMDB calls in the job
	titleNamesMaxAge   = 30 * 24 * time.Hour    // titles are looked at again after this
	titleNamesTimeout  = 10 * time.Second
	titleNamesBackoff  = 5 * time.Minute // no new asks for this long after TMDB fails

	// maxTVExtraSearches is how many other titles are searched for one season or
	// episode. A show is searched once per wanted episode, so it gets fewer than
	// a film; all kept titles still count when a release is matched.
	maxTVExtraSearches = 2

	// maxAutoExtraSearches is how many other titles an automatic movie search
	// tries, and only when the main title found nothing it would take.
	maxAutoExtraSearches = 2
)

// titleNamesPausedUntil is when TMDB may be asked again after it failed
// (unix nanoseconds, 0 = not paused), so a TMDB that is down costs one wait
// rather than one per title.
var titleNamesPausedUntil atomic.Int64

// titleNamesFailed remembers when a title last failed in the background job
// ("kind:id" to the time), so the job skips it for titleNamesRetry.
var titleNamesFailed sync.Map

// titleNamesRetry is how long the background job leaves a title alone after it failed.
const titleNamesRetry = 6 * time.Hour

var errTitleNamesPaused = errors.New("TMDB did not answer a moment ago")

// wantsOtherLanguage reports whether the audio language list names a language
// other than English, which is when other titles matter.
func wantsOtherLanguage(list string) bool {
	for _, l := range quality.ParseLanguages(list) {
		if !strings.EqualFold(l, quality.DefaultLanguage) {
			return true
		}
	}
	return false
}

// nameLanguageCodes turns the audio language list into TMDB language codes,
// English left out. "Original" stands for the title's own language
// (originalCode, "" when not known).
func nameLanguageCodes(list, originalCode string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(code string) {
		code = strings.ToLower(code)
		if code != "" && code != "en" && !seen[code] {
			seen[code] = true
			out = append(out, code)
		}
	}
	for _, l := range quality.ParseLanguages(list) {
		if strings.EqualFold(l, quality.OriginalLanguage) {
			add(originalCode)
			continue
		}
		for _, c := range quality.LanguageCodes(l) {
			add(c)
		}
	}
	return out
}

// audioLanguageList is the audio language setting as it is stored with the
// names that were picked for it.
func (s *Server) audioLanguageList() string {
	return strings.Join(quality.ParseLanguages(s.preferredLanguage()), ",")
}

// refreshTitleNames asks TMDB for a title's other names, keeps the few that
// match the wanted languages and saves them. It returns the kept names.
func (s *Server) refreshTitleNames(ctx context.Context, kind string, tmdbID int, english, list string) ([]string, error) {
	c := s.TMDB()
	if c == nil || !c.HasAPIKey() {
		return nil, metadata.ErrNoAPIKey
	}
	if until := titleNamesPausedUntil.Load(); until != 0 && time.Now().UnixNano() < until {
		return nil, errTitleNamesPaused
	}
	cctx, cancel := context.WithTimeout(ctx, titleNamesTimeout)
	defer cancel()
	src, err := c.TitleNameSources(cctx, kind, tmdbID)
	if err != nil {
		var se *metadata.StatusError
		if errors.As(err, &se) && se.Code == 404 {
			// TMDB does not have the title: remember that, it will not start having it.
			src = metadata.NameSources{}
		} else {
			if ctx.Err() == nil {
				titleNamesPausedUntil.Store(time.Now().Add(titleNamesBackoff).UnixNano())
			}
			return nil, err
		}
	}
	names := metadata.WithUmlautSpellings(metadata.ExtraTitles(src, english, nameLanguageCodes(list, src.OriginalLanguage), metadata.MaxExtraTitles))
	if err := s.MovieRepo.SaveTitleNames(kind, tmdbID, names, list, time.Now()); err != nil {
		return names, err
	}
	return names, nil
}

// altTitlesFor returns a movie's or show's other titles for a search, asking
// TMDB first when they have not been picked yet, were picked for other audio
// languages, or are old. loaded is what the title came with. Nothing is
// returned while English is the only language wanted.
func (s *Server) altTitlesFor(ctx context.Context, kind string, tmdbID int, english string, loaded []string) []string {
	list := s.audioLanguageList()
	if tmdbID == 0 || !wantsOtherLanguage(list) {
		return nil
	}
	rec, found, err := s.MovieRepo.TitleNames(kind, tmdbID)
	if err != nil {
		slog.Warn("titles: couldn't read the saved other titles", "kind", kind, "tmdb", tmdbID, "err", err)
		return loaded
	}
	if found && rec.Languages == list && time.Since(rec.CheckedAt) < titleNamesMaxAge {
		return rec.Names
	}
	names, err := s.refreshTitleNames(ctx, kind, tmdbID, english, list)
	if err != nil {
		if !errors.Is(err, errTitleNamesPaused) && !errors.Is(err, metadata.ErrNoAPIKey) {
			slog.Debug("titles: couldn't fetch the other titles", "kind", kind, "tmdb", tmdbID, "err", err)
		}
		if found && rec.Languages == list {
			return rec.Names // old, but still the right languages
		}
		return nil
	}
	return names
}

// refreshOtherTitles picks a title's other names again, for when the title is
// refreshed from TMDB. A problem is only logged: the names saved before stay.
func (s *Server) refreshOtherTitles(ctx context.Context, kind string, tmdbID int, english string) {
	list := s.audioLanguageList()
	if tmdbID == 0 || !wantsOtherLanguage(list) {
		return
	}
	if _, err := s.refreshTitleNames(ctx, kind, tmdbID, english, list); err != nil && !errors.Is(err, errTitleNamesPaused) {
		slog.Debug("titles: couldn't refresh the other titles", "kind", kind, "tmdb", tmdbID, "err", err)
	}
}

// movieWithAltTitles is m with its other titles up to date for a search.
func (s *Server) movieWithAltTitles(ctx context.Context, m library.Movie) library.Movie {
	m.AltTitles = s.altTitlesFor(ctx, "movie", m.TMDBID, m.Title, m.AltTitles)
	return m
}

// seriesWithAltTitles is the same for a show.
func (s *Server) seriesWithAltTitles(ctx context.Context, sr library.Series) library.Series {
	sr.AltTitles = s.altTitlesFor(ctx, "tv", sr.TMDBID, sr.Title, sr.AltTitles)
	return sr
}

// searchMovieTitles searches the indexers for a movie by its title and then by
// each of its other titles (one search each, so as many as the movie has, at
// most four). Each is a whole search through the indexers' own pacing, so no
// indexer is asked more often than it is for any other search, and an indexer
// that has just said "too many requests" is skipped for the rest.
//
// enough is nil for a search a person asked for: every title is tried. An
// automatic search passes a check of whether what was found so far would be
// taken, and then the other titles are only tried (at most maxAutoExtraSearches
// of them) while it says no, so a title that is found under its main name costs
// one search a cycle, not five.
func (s *Server) searchMovieTitles(ctx context.Context, instances []indexers.Instance, m library.Movie, enough func([]indexers.Outcome) bool) []indexers.Outcome {
	if enough == nil {
		ctx = indexers.AsManual(ctx) // a person asked for this search
	}
	outcomes := indexers.SearchAll(ctx, instances, indexerQuery(m.Title), movieCategory)
	// A popular title ("Dune", "Halloween") can have more releases than a
	// search reads, and the right film's may never be among them: then the
	// title is searched again with its year.
	if m.Year > 0 && ctx.Err() == nil && slices.ContainsFunc(outcomes, func(o indexers.Outcome) bool { return indexers.Filled(ctx, o) }) {
		outcomes = append(outcomes, indexers.SearchAll(ctx, instances, indexerQuery(m.Title)+" "+strconv.Itoa(m.Year), movieCategory)...)
	}
	extra := 0
	for _, t := range m.AltTitles {
		if ctx.Err() != nil {
			break
		}
		if enough != nil {
			if enough(outcomes) || extra == maxAutoExtraSearches {
				break
			}
			extra++
		}
		outcomes = append(outcomes, indexers.SearchAll(ctx, instances, indexerQuery(t), movieCategory)...)
	}
	return outcomes
}

// altTVQueries are the searches for a season or episode under the show's other
// titles, in the same shape as the main one.
func altTVQueries(series library.Series, season, episode int) []string {
	var out []string
	for _, t := range series.AltTitles {
		if len(out) == maxTVExtraSearches {
			break
		}
		out = append(out, tvSearchQuery(t, season, episode))
	}
	return out
}

// altKeyLongEnough says whether another title is long enough to take a release
// for it: names in Latin letters need four (a three-letter word is too often
// the name of some other film), other alphabets keep the usual two.
func altKeyLongEnough(key string) bool {
	if !metadata.SearchableKey(key) {
		return false
	}
	letters := 0
	for _, r := range key {
		if r > unicode.MaxASCII {
			return true
		}
		if r != ' ' {
			letters++
		}
	}
	return letters >= 4
}

// matchingAltTitle returns the one of a movie's or show's other titles that a
// release's parsed title is, or "" when it is none of them. Spelling does not
// matter (any alphabet, accents and punctuation ignored, umlauts written either
// way), but a title too short to tell from other things never counts.
func matchingAltTitle(parsedTitle string, alts []string) string {
	if len(alts) == 0 {
		return ""
	}
	keys := metadata.TitleKeys(parsedTitle)
	if len(keys) == 0 {
		return ""
	}
	for _, alt := range alts {
		for _, w := range metadata.TitleKeys(alt) {
			if !altKeyLongEnough(w) {
				continue
			}
			for _, r := range keys {
				if r == w {
					return alt
				}
			}
		}
	}
	return ""
}

// matchedOtherTitle says which of the other titles of a movie or show a release
// is named by, "" when it is named by the main title or by none: what the
// release list shows.
func matchedOtherTitle(releaseTitle, mainTitle string, alts []string) string {
	if len(alts) == 0 {
		return ""
	}
	parsed := parser.Parse(releaseTitle).Title
	if sameTitle(parsed, mainTitle) {
		return ""
	}
	return matchingAltTitle(parsed, alts)
}

// titleNamesJob picks the other titles of the movies and shows in the library
// that have none picked yet (or were picked for other audio languages, or long
// ago), a few at a time, so searching them later needs no wait for TMDB.
func (s *Server) titleNamesJob(ctx context.Context) {
	list := s.audioLanguageList()
	if !wantsOtherLanguage(list) || !s.TMDB().HasAPIKey() {
		return
	}
	type title struct {
		kind    string
		tmdbID  int
		english string
	}
	var todo []title
	type key struct {
		kind string
		id   int
	}
	seen := map[key]bool{}
	add := func(kind string, id int, english string) {
		k := key{kind, id}
		if id == 0 || seen[k] {
			return
		}
		seen[k] = true
		if rec, found, err := s.MovieRepo.TitleNames(kind, id); err == nil && found && rec.Languages == list && time.Since(rec.CheckedAt) < titleNamesMaxAge {
			return
		}
		if at, ok := titleNamesFailed.Load(fmt.Sprintf("%s:%d", kind, id)); ok && time.Since(at.(time.Time)) < titleNamesRetry {
			return
		}
		todo = append(todo, title{kind, id, english})
	}
	if s.moviesEnabled() {
		movies, err := s.MovieRepo.List()
		if err != nil {
			return
		}
		for _, m := range movies {
			add("movie", m.TMDBID, m.Title)
		}
	}
	if s.tvEnabled() {
		shows, err := s.MovieRepo.ListSeries()
		if err != nil {
			return
		}
		for _, sr := range shows {
			add("tv", sr.TMDBID, sr.Title)
		}
	}
	for i, t := range todo {
		if i >= titleNamesBatch || ctx.Err() != nil {
			return
		}
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(titleNamesPause):
			}
		}
		if _, err := s.refreshTitleNames(ctx, t.kind, t.tmdbID, t.english, list); err != nil {
			if errors.Is(err, errTitleNamesPaused) {
				return
			}
			if !errors.Is(err, metadata.ErrNoAPIKey) && ctx.Err() == nil {
				// Leave this title alone for a while, so one that always fails does
				// not stand first in line and end every run before the rest.
				titleNamesFailed.Store(fmt.Sprintf("%s:%d", t.kind, t.tmdbID), time.Now())
			}
			slog.Debug("titles: couldn't fetch the other titles", "kind", t.kind, "tmdb", t.tmdbID, "err", err)
		}
	}
}
