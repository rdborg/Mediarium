package api

import (
	"context"
	"errors"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
)

// "More like your library" on Discover: titles that TMDB relates to what is in
// the library, merged across several library titles and ranked so that recent,
// popular titles that more than one of them point to come first. It is a
// paged list like the others (list=similar), so the rail and its "Browse more"
// page share one source.
// listSimilar is the name of this list in /api/discover/list.
const listSimilar = "similar"

const (
	// similarSeedCount library titles are asked about per request. They are
	// picked from the similarSeedPool most recently added, rotating daily so
	// the rail does not show the same picks for weeks.
	similarSeedCount = 10
	similarSeedPool  = 40
	// similarRecentYears is how far back titles go unless the person asks for
	// older ones.
	similarRecentYears = 15
	// similarMaxResults caps the ranked list (10 pages of 20).
	similarMaxResults = 200
	similarPageSize   = 20
	similarParallel   = 6
)

// similarCandidate is one title suggested for the library, with what the
// ranking needs.
type similarCandidate struct {
	ID         int
	Year       int
	HasPoster  bool
	Popularity float64
	VoteCount  int
	// Votes is how many library titles suggested it.
	Votes int

	order int // first seen, to keep the ranking stable
	movie *metadata.Movie
	show  *metadata.Show
}

func movieCandidate(m metadata.Movie) similarCandidate {
	return similarCandidate{ID: m.TMDBID, Year: m.Year(), HasPoster: m.PosterPath != "", Popularity: m.Popularity, VoteCount: m.VoteCount, movie: &m}
}

func showCandidate(sh metadata.Show) similarCandidate {
	return similarCandidate{ID: sh.TMDBID, Year: sh.Year(), HasPoster: sh.PosterPath != "", Popularity: sh.Popularity, VoteCount: sh.VoteCount, show: &sh}
}

// mergeSuggestions combines what each library title suggested. A title
// suggested more than once by the same library title counts once for it, so
// Votes is the number of library titles that point to it.
func mergeSuggestions(perSeed [][]similarCandidate) []similarCandidate {
	byID := map[int]int{}
	var out []similarCandidate
	for _, list := range perSeed {
		counted := map[int]bool{}
		for _, c := range list {
			if c.ID <= 0 || counted[c.ID] {
				continue
			}
			counted[c.ID] = true
			if at, ok := byID[c.ID]; ok {
				out[at].Votes++
				continue
			}
			c.Votes = 1
			c.order = len(out)
			byID[c.ID] = len(out)
			out = append(out, c)
		}
	}
	return out
}

type similarOptions struct {
	Now time.Time
	// Older lets titles from before the last similarRecentYears years in.
	Older bool
	// Library holds the TMDB ids already in the library; they are never listed.
	Library map[int]bool
	Limit   int
}

// similarScore favours recent titles, then popular ones, then those that
// several library titles point to. Each part is between 0 and its weight.
func similarScore(c similarCandidate, now time.Time) float64 {
	age := now.Year() - c.Year
	if c.Year <= 0 || age > 30 {
		age = 30
	}
	if age < 0 {
		age = 0 // not out yet counts as brand new
	}
	recency := 1 - float64(age)/30

	pop := c.Popularity
	popularity := math.Min(1, math.Log10(1+pop)/3)
	if pop <= 0 {
		popularity = math.Min(1, math.Log10(1+float64(c.VoteCount))/4)
	}

	agreement := float64(min(c.Votes, 4)-1) / 3
	if agreement < 0 {
		agreement = 0
	}
	return 3*recency + 2*popularity + 1.5*agreement
}

// rankSimilar filters and orders the suggestions: nothing that is in the
// library, nothing without a release year, and (unless Older) nothing from
// before the last similarRecentYears years. Titles with a poster come before
// titles without one; inside each group the best score wins.
func rankSimilar(cands []similarCandidate, opt similarOptions) []similarCandidate {
	oldest := opt.Now.Year() - similarRecentYears
	out := make([]similarCandidate, 0, len(cands))
	for _, c := range cands {
		if opt.Library[c.ID] || c.Year <= 0 || (!opt.Older && c.Year < oldest) {
			continue
		}
		out = append(out, c)
	}
	scores := make(map[int]float64, len(out))
	for _, c := range out {
		scores[c.ID] = similarScore(c, opt.Now)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.HasPoster != b.HasPoster {
			return a.HasPoster
		}
		if scores[a.ID] != scores[b.ID] {
			return scores[a.ID] > scores[b.ID]
		}
		if a.Year != b.Year {
			return a.Year > b.Year
		}
		return a.order < b.order
	})
	if opt.Limit > 0 && len(out) > opt.Limit {
		out = out[:opt.Limit]
	}
	return out
}

// pickSeeds chooses n of the ids (newest first) to ask TMDB about: from the
// first similarSeedPool, in an order that changes with the day but is the
// same all day, so pages of one list agree with each other.
func pickSeeds(ids []int, n int, day int) []int {
	pool := ids
	if len(pool) > similarSeedPool {
		pool = pool[:similarSeedPool]
	}
	if len(pool) <= n {
		return append([]int(nil), pool...)
	}
	picked := append([]int(nil), pool...)
	key := func(id int) uint32 {
		x := uint32(id)*2654435761 + uint32(day)*40503
		x ^= x >> 16
		x *= 0x85ebca6b
		x ^= x >> 13
		x *= 0xc2b2ae35
		return x ^ x>>16
	}
	sort.SliceStable(picked, func(i, j int) bool { return key(picked[i]) < key(picked[j]) })
	return picked[:n]
}

// libraryIDs returns the TMDB ids of the library's movies or shows, newest
// added first.
func (s *Server) libraryIDs(kind string) ([]int, error) {
	var ids []int
	if kind == "tv" {
		list, err := s.MovieRepo.ListSeries()
		if err != nil {
			return nil, err
		}
		for _, sr := range list {
			ids = append(ids, sr.TMDBID)
		}
		return ids, nil
	}
	list, err := s.MovieRepo.List()
	if err != nil {
		return nil, err
	}
	for _, m := range list {
		ids = append(ids, m.TMDBID)
	}
	return ids, nil
}

// suggestions asks TMDB what it relates to each seed. A seed that fails is
// left out; the error is returned only when every one failed.
func (s *Server) suggestions(ctx context.Context, kind string, seeds []int) ([][]similarCandidate, error) {
	perSeed := make([][]similarCandidate, len(seeds))
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		failed  int
		lastErr error
		sem     = make(chan struct{}, similarParallel)
	)
	for i, id := range seeds {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			var list []similarCandidate
			var firstErr error
			for _, related := range []string{metadata.RelatedRecommended, metadata.RelatedSimilar} {
				var err error
				if kind == "tv" {
					var shows []metadata.Show
					if shows, err = s.TMDB().RelatedShows(ctx, id, related); err == nil {
						for _, sh := range shows {
							list = append(list, showCandidate(sh))
						}
					}
				} else {
					var movies []metadata.Movie
					if movies, err = s.TMDB().RelatedMovies(ctx, id, related); err == nil {
						for _, m := range movies {
							list = append(list, movieCandidate(m))
						}
					}
				}
				if err != nil && firstErr == nil {
					firstErr = err
				}
			}
			mu.Lock()
			defer mu.Unlock()
			perSeed[i] = list
			if len(list) == 0 && firstErr != nil {
				failed++
				lastErr = firstErr
			}
		}()
	}
	wg.Wait()
	if failed == len(seeds) && lastErr != nil {
		return nil, lastErr
	}
	return perSeed, nil
}

// rankedSimilar builds the whole ranked list for a kind ("movie" or "tv").
func (s *Server) rankedSimilar(ctx context.Context, kind string, older bool) ([]similarCandidate, error) {
	ids, err := s.libraryIDs(kind)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	now := time.Now()
	perSeed, err := s.suggestions(ctx, kind, pickSeeds(ids, similarSeedCount, int(now.Unix()/86400)))
	if err != nil {
		return nil, err
	}
	lib := make(map[int]bool, len(ids))
	for _, id := range ids {
		lib[id] = true
	}
	return rankSimilar(mergeSuggestions(perSeed), similarOptions{Now: now, Older: older, Library: lib, Limit: similarMaxResults}), nil
}

// queryFlag reads an optional true/false parameter written 1 or true.
func queryFlag(r *http.Request, name string) bool {
	v := r.URL.Query().Get(name)
	return v == "1" || v == "true"
}

// handleDiscoverSimilar answers list=similar of /api/discover/list: one page
// of the ranked "More like your library" titles for kind, 20 to a page.
func (s *Server) handleDiscoverSimilar(w http.ResponseWriter, r *http.Request, kind string, page int) {
	empty := browsePayload{Page: page, TotalPages: 1, Results: []browseItem{}}
	if !s.TMDB().HasAPIKey() {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	ranked, err := s.rankedSimilar(r.Context(), kind, queryFlag(r, "older"))
	if err != nil {
		writeUpstreamError(w, "load the titles like your library", err)
		return
	}
	total := len(ranked)
	start := (page - 1) * similarPageSize
	if start >= total {
		empty.TotalResults = total
		empty.TotalPages = max(1, (total+similarPageSize-1)/similarPageSize)
		writeJSON(w, http.StatusOK, empty)
		return
	}
	end := min(start+similarPageSize, total)
	pages := (total + similarPageSize - 1) / similarPageSize
	ctx := r.Context()
	if kind == "tv" {
		shows := make([]metadata.Show, 0, end-start)
		for _, c := range ranked[start:end] {
			shows = append(shows, *c.show)
		}
		writeJSON(w, http.StatusOK, s.showPagePayload(ctx, &metadata.ShowPage{Page: page, TotalPages: pages, TotalResults: total, Results: shows}))
		return
	}
	movies := make([]metadata.Movie, 0, end-start)
	for _, c := range ranked[start:end] {
		movies = append(movies, *c.movie)
	}
	writeJSON(w, http.StatusOK, s.moviePagePayload(ctx, &metadata.MoviePage{Page: page, TotalPages: pages, TotalResults: total, Results: movies}))
}

// handleForYou is the first page of the movie list, kept for older clients
// that call /api/discover/for-you.
func (s *Server) handleForYou(w http.ResponseWriter, r *http.Request) {
	if !s.TMDB().HasAPIKey() {
		writeJSON(w, http.StatusOK, []discoverPayload{})
		return
	}
	ranked, err := s.rankedSimilar(r.Context(), "movie", queryFlag(r, "older"))
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		writeUpstreamError(w, "load the titles like your library", err)
		return
	}
	out := make([]discoverPayload, 0, similarPageSize)
	for _, c := range ranked {
		if len(out) == similarPageSize {
			break
		}
		out = append(out, s.movieDiscover(r.Context(), *c.movie))
	}
	writeJSON(w, http.StatusOK, out)
}
