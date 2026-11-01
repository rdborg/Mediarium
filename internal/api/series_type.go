package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/rdborg/mediarium/internal/animemap"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/parser"
)

// Anime and daily shows. Anime releases count episodes from the start of the
// show ("[Group] Show - 105"); daily shows (talk shows, news) are named by
// air date ("Show.2024.03.15"). A show's type says which, and releases are
// mapped to season and episode with its episode list, so searching, picking
// and importing work as for any show.

// guessSeriesType picks a type for a newly added show: Japanese animation is
// anime; talk shows and news are daily.
func guessSeriesType(show metadata.Show) string {
	ids := make([]int, 0, len(show.Genres))
	names := make([]string, 0, len(show.Genres))
	for _, g := range show.Genres {
		ids = append(ids, g.ID)
		names = append(names, g.Name)
	}
	return library.GuessSeriesType(ids, names, show.OriginCountry, show.Type)
}

// mapRelease gives an anime or daily release the season and episodes it is
// for, from the show's episode list. A release with a season marker, or of a
// standard show, comes back as it was.
func mapRelease(rel parser.Release, seriesType string, episodes []library.Episode) parser.Release {
	if rel.Season > 0 || rel.Special {
		return rel
	}
	switch seriesType {
	case library.SeriesAnime:
		if len(rel.Absolute) == 0 {
			return rel
		}
		order := library.AbsoluteOrder(episodes)
		var eps []int
		season := 0
		for _, n := range rel.Absolute {
			if n < 1 || n > len(order) {
				continue
			}
			e := order[n-1]
			if season == 0 {
				season = e.Season
			}
			if e.Season != season {
				break // a batch running into the next season: its first season only
			}
			eps = append(eps, e.Episode)
		}
		if season > 0 && len(eps) > 0 {
			rel.Season, rel.Episodes, rel.Episode = season, eps, eps[0]
			// A batch of every episode of the season ("Show - 01-12" for a
			// twelve-episode season) is that season's pack, the way a season
			// pack search looks for one.
			if len(eps) > 1 && coversWholeSeason(eps, season, episodes) {
				rel.Episodes, rel.Episode = nil, 0
			}
		}
	case library.SeriesDaily:
		if rel.AirDate == "" {
			return rel
		}
		var sameDay []library.Episode
		for _, e := range episodes {
			if e.Season > 0 && e.AirDate == rel.AirDate {
				sameDay = append(sameDay, e)
			}
		}
		if e, ok := pickSameDay(sameDay, rel); ok {
			rel.Season, rel.Episode, rel.Episodes = e.Season, e.Episode, []int{e.Episode}
		}
	}
	return rel
}

// coversWholeSeason reports whether eps holds every episode the library lists
// for season.
func coversWholeSeason(eps []int, season int, episodes []library.Episode) bool {
	have := map[int]bool{}
	for _, e := range eps {
		have[e] = true
	}
	n := 0
	for _, e := range episodes {
		if e.Season != season || e.Episode <= 0 {
			continue
		}
		if !have[e.Episode] {
			return false
		}
		n++
	}
	return n == len(have)
}

// pickSameDay picks the episode a dated release is for when a daily show
// has more than one on that day: by "Part 2" in the name, or by the words
// after the date matching one episode's title better than the others. When
// nothing tells them apart the release is left unmatched rather than filling
// the wrong episode; choosing a release by hand still works.
func pickSameDay(sameDay []library.Episode, rel parser.Release) (library.Episode, bool) {
	switch len(sameDay) {
	case 0:
		return library.Episode{}, false
	case 1:
		return sameDay[0], true
	}
	sort.Slice(sameDay, func(i, j int) bool {
		if sameDay[i].Season != sameDay[j].Season {
			return sameDay[i].Season < sameDay[j].Season
		}
		return sameDay[i].Episode < sameDay[j].Episode
	})
	words := titleWords(rel.DailyTitle)
	best, bestScore, tie := 0, 0, false
	for i, e := range sameDay {
		score := 0
		for w := range titleWords(e.Title) {
			if words[w] {
				score++
			}
		}
		switch {
		case score > bestScore:
			best, bestScore, tie = i, score, false
		case score == bestScore && score > 0:
			tie = true
		}
	}
	if bestScore > 0 && !tie {
		return sameDay[best], true
	}
	if rel.DailyPart >= 1 && rel.DailyPart <= len(sameDay) {
		return sameDay[rel.DailyPart-1], true
	}
	return library.Episode{}, false
}

// titleWords are the words of a title that say something: lower case,
// three letters or more, without the common little ones.
func titleWords(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(w)) < 3 || commonTitleWords[w] {
			continue
		}
		out[w] = true
	}
	return out
}

var commonTitleWords = map[string]bool{
	"the": true, "and": true, "with": true, "for": true, "from": true, "part": true, "episode": true, "show": true, "special": true,
}

// releaseMapper maps release names for one show to the episodes they are for
// under the show's own numbering: names in scene or anime numbering through
// the show's numbering (see mapNumbered; the person's own rules for the show,
// see mapCustom, come before that), anime and daily names without a
// season marker through the episode list, and a special named by its title to
// that special. The episode list and the numbering are read once, on first use.
func (s *Server) releaseMapper(series library.Series) func(parser.Release) parser.Release {
	var episodes []library.Episode
	loaded := false
	var num *animemap.Numbering
	numLoaded := false
	var custom *animemap.Custom
	customLoaded := false
	return func(rel parser.Release) parser.Release {
		if rel.Special {
			return rel
		}
		// The person's own rules come first.
		if !customLoaded {
			custom, customLoaded = s.customFor(series), true
		}
		if mappedRel, ok := mapCustom(rel, custom); ok {
			return mappedRel
		}
		if !numLoaded {
			num, numLoaded = s.numberingFor(series), true
		}
		if !num.Empty() {
			if (len(num.Entries) > 0 || (rel.Season == 0 && len(rel.Absolute) > 0)) && !loaded {
				episodes, _ = s.MovieRepo.ListEpisodes(series.ID)
				loaded = true
			}
			if mapped, ok := mapNumbered(rel, num, episodes); ok {
				return mapped
			}
		}
		if rel.Season > 0 {
			return rel
		}
		if !loaded {
			episodes, _ = s.MovieRepo.ListEpisodes(series.ID)
			loaded = true
		}
		if series.SeriesType == library.SeriesAnime || series.SeriesType == library.SeriesDaily {
			if mapped := mapRelease(rel, series.SeriesType, episodes); mapped.Season > 0 {
				return mapped
			}
		}
		return mapSpecial(rel, series.Title, episodes)
	}
}

// mapSpecial reads a release with no season marker as one of the show's
// specials when its name carries that special's title after the show's
// ("Doctor.Who.2005.The.Christmas.Invasion.720p" is the special called "The
// Christmas Invasion"), or, for a dated name, when one special aired that
// day. Every telling word of the title must be there, a one-word title only
// counts when the word is long enough not to turn up by chance, and two
// specials matching equally well leave the release as it was.
func mapSpecial(rel parser.Release, showTitle string, episodes []library.Episode) parser.Release {
	if len(rel.Absolute) > 0 {
		return rel
	}
	var specials []library.Episode
	for _, e := range episodes {
		if e.Season == 0 && e.Episode > 0 {
			specials = append(specials, e)
		}
	}
	pick := func(e library.Episode) parser.Release {
		rel.Special, rel.Season, rel.Episode, rel.Episodes = true, 0, e.Episode, []int{e.Episode}
		return rel
	}
	if rel.AirDate != "" {
		var same []library.Episode
		for _, e := range specials {
			if e.AirDate == rel.AirDate {
				same = append(same, e)
			}
		}
		if len(same) == 1 {
			return pick(same[0])
		}
		return rel
	}
	name := titleWords(rel.Name)
	show := titleWords(showTitle)
	phrase := " " + metadata.TitleKey(rel.Name) + " "
	best, bestScore, tie := -1, 0, false
	for i, e := range specials {
		need := 0
		all := true
		longest := 0
		for w := range titleWords(e.Title) {
			if show[w] {
				continue
			}
			need++
			longest = max(longest, len([]rune(w)))
			if !name[w] {
				all = false
			}
		}
		if !all || need == 0 || (need == 1 && longest < 6) {
			continue
		}
		// The whole title, small words and order included, beats the same
		// words scattered: "Planet of the Dead" over "Dead Planet".
		score := need * 2
		if title := metadata.TitleKey(e.Title); title != "" && strings.Contains(phrase, " "+title+" ") {
			score++
		}
		switch {
		case score > bestScore:
			best, bestScore, tie = i, score, false
		case score == bestScore:
			tie = true
		}
	}
	if best >= 0 && !tie {
		return pick(specials[best])
	}
	return rel
}

// parseFor parses a release name for series (mapped when it is anime or daily).
func parseFor(mapper func(parser.Release) parser.Release, title string) parser.Release {
	return mapped(mapper, parser.Parse(title))
}

// mapped is a parsed release as the series' mapper reads it (anime or daily).
func mapped(mapper func(parser.Release) parser.Release, rel parser.Release) parser.Release {
	if mapper != nil {
		rel = mapper(rel)
	}
	return rel
}

// extraTVQueries are the searches a show needs besides "Show S01E05": the
// absolute number ("Show 105") or the air date ("Show 2024 03 15") of the
// episode, and for a show with scene or anime numbering the names releases
// use for it (see numberingQueries), and the same search under each of the
// show's other titles (a German title for a person who wants German audio).
func (s *Server) extraTVQueries(series library.Series, season, episode int) []string {
	return append(s.extraNumberingQueries(series, season, episode), altTVQueries(series, season, episode)...)
}

func (s *Server) extraNumberingQueries(series library.Series, season, episode int) []string {
	own := customQueries(series.Title, s.customFor(series), season, episode)
	out := append(own, s.extraTVQueriesFor(series, season, episode)...)
	if season <= 0 || episode <= 0 {
		return out
	}
	num := s.numberingFor(series)
	if num.Empty() {
		return out
	}
	absolute := 0
	if episodes, err := s.MovieRepo.ListEpisodes(series.ID); err == nil {
		absolute = library.AbsoluteNumber(episodes, season, episode)
	}
	seen := map[string]bool{}
	for _, q := range out {
		seen[strings.ToLower(q)] = true
	}
	for _, q := range numberingQueries(series.Title, num, season, episode, absolute) {
		if !seen[strings.ToLower(q)] {
			seen[strings.ToLower(q)] = true
			out = append(out, q)
		}
	}
	return out
}

func (s *Server) extraTVQueriesFor(series library.Series, season, episode int) []string {
	if season == 0 && episode > 0 {
		// Specials are often posted under their title rather than S00E05.
		if ep, err := s.MovieRepo.GetEpisode(series.ID, 0, episode); err == nil && strings.TrimSpace(ep.Title) != "" {
			return []string{series.Title + " " + ep.Title}
		}
		return nil
	}
	if episode <= 0 || (series.SeriesType != library.SeriesAnime && series.SeriesType != library.SeriesDaily) {
		return nil
	}
	episodes, err := s.MovieRepo.ListEpisodes(series.ID)
	if err != nil {
		return nil
	}
	if series.SeriesType == library.SeriesAnime {
		if n := library.AbsoluteNumber(episodes, season, episode); n > 0 {
			return []string{fmt.Sprintf("%s %02d", series.Title, n)}
		}
		return nil
	}
	for _, e := range episodes {
		if e.Season == season && e.Episode == episode && len(e.AirDate) == 10 {
			return []string{series.Title + " " + strings.ReplaceAll(e.AirDate, "-", " ")}
		}
	}
	return nil
}

// PUT /api/series/{id}/type {"type": "standard" | "anime" | "daily"}
func (s *Server) handleSetSeriesType(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid show ID.")
		return
	}
	var req struct {
		Type string `json:"type"`
	}
	if err := decodeJSON(r, &req); err != nil || (req.Type != library.SeriesStandard && req.Type != library.SeriesAnime && req.Type != library.SeriesDaily) {
		writeError(w, http.StatusBadRequest, `Choose "standard", "anime" or "daily".`)
		return
	}
	if _, err := s.MovieRepo.GetSeries(id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That show isn't in your library.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.MovieRepo.SetSeriesType(id, req.Type); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sr, err := s.MovieRepo.GetSeries(id); err == nil {
		s.noteSeriesNumbering(sr) // an anime gets its numbering read now
	}
	writeJSON(w, http.StatusOK, map[string]string{"type": req.Type})
}
