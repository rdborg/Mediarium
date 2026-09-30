package metadata

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Browse sort orders accepted by DiscoverQuery.Sort.
const (
	SortPopular = "popular" // most popular first (the default)
	SortRating  = "rating"  // best rated first, among titles with enough votes
	SortNewest  = "newest"  // latest release or first-air date first
	SortOldest  = "oldest"
)

// minVotesForRating keeps a handful of five-star votes from putting an
// unknown title at the top of a "best rated" list.
const minVotesForRating = 200

// maxDiscoverPages is the deepest page TMDB's discover endpoints serve.
const maxDiscoverPages = 500

// DiscoverQuery filters a browse of TMDB's catalogue. Zero values mean "any".
// Year is an exact year and wins over the YearFrom/YearTo range.
type DiscoverQuery struct {
	Genre    int
	Year     int
	YearFrom int
	YearTo   int
	Sort     string
	Page     int
}

// ValidSort reports whether sort is empty or one of the Sort* constants.
func ValidSort(sort string) bool {
	switch sort {
	case "", SortPopular, SortRating, SortNewest, SortOldest:
		return true
	}
	return false
}

// MoviePage is one page of a movie browse or list. TotalPages is capped at
// the deepest page TMDB serves, and TotalResults at what those pages hold.
type MoviePage struct {
	Page         int
	TotalPages   int
	TotalResults int
	Results      []Movie
}

// ShowPage is one page of a TV browse or list.
type ShowPage struct {
	Page         int
	TotalPages   int
	TotalResults int
	Results      []Show
}

// params builds TMDB's discover query string. dateField is the date filter's
// name ("primary_release_date" for movies, "first_air_date" for TV) and
// yearParam the exact-year filter ("primary_release_year" or
// "first_air_date_year").
func (q DiscoverQuery) params(dateField, yearParam string) url.Values {
	v := url.Values{"include_adult": {"false"}, "language": {"en-US"}}
	if q.Genre > 0 {
		v.Set("with_genres", strconv.Itoa(q.Genre))
	}
	switch {
	case q.Year > 0:
		v.Set(yearParam, strconv.Itoa(q.Year))
	default:
		if q.YearFrom > 0 {
			v.Set(dateField+".gte", fmt.Sprintf("%04d-01-01", q.YearFrom))
		}
		if q.YearTo > 0 {
			v.Set(dateField+".lte", fmt.Sprintf("%04d-12-31", q.YearTo))
		}
	}
	switch q.Sort {
	case SortRating:
		v.Set("sort_by", "vote_average.desc")
		v.Set("vote_count.gte", strconv.Itoa(minVotesForRating))
	case SortNewest:
		v.Set("sort_by", dateField+".desc")
	case SortOldest:
		v.Set("sort_by", dateField+".asc")
	default:
		v.Set("sort_by", "popularity.desc")
	}
	v.Set("page", strconv.Itoa(ClampPage(q.Page)))
	return v
}

// ClampPage keeps a requested page number between 1 and the deepest page
// TMDB serves.
func ClampPage(page int) int {
	if page < 1 {
		return 1
	}
	if page > maxDiscoverPages {
		return maxDiscoverPages
	}
	return page
}

// pageMeta is the paging part of every TMDB list response.
type pageMeta struct {
	Page         int `json:"page"`
	TotalPages   int `json:"total_pages"`
	TotalResults int `json:"total_results"`
}

// clamped caps the page count at maxDiscoverPages, and the result count at
// what those pages can hold, since TMDB refuses anything deeper.
func (p pageMeta) clamped() pageMeta {
	if p.TotalPages > maxDiscoverPages {
		p.TotalPages = maxDiscoverPages
		if limit := maxDiscoverPages * tmdbPageSize; p.TotalResults > limit {
			p.TotalResults = limit
		}
	}
	return p
}

// tmdbPageSize is how many results TMDB puts on each list page.
const tmdbPageSize = 20

// moviePage fetches one page of any TMDB movie list, through the page cache.
func (c *Client) moviePage(ctx context.Context, path string, params url.Values) (*MoviePage, error) {
	key := path + "?" + params.Encode()
	if v, ok := c.pages.get(key); ok {
		return v.(*MoviePage), nil
	}
	var resp struct {
		pageMeta
		Results []Movie `json:"results"`
	}
	if err := c.get(ctx, path, params, &resp); err != nil {
		return nil, err
	}
	m := resp.pageMeta.clamped()
	out := &MoviePage{Page: m.Page, TotalPages: m.TotalPages, TotalResults: m.TotalResults, Results: resp.Results}
	if out.Results == nil {
		out.Results = []Movie{}
	}
	c.pages.put(key, out)
	return out, nil
}

// showPage is moviePage for shows.
func (c *Client) showPage(ctx context.Context, path string, params url.Values) (*ShowPage, error) {
	key := path + "?" + params.Encode()
	if v, ok := c.pages.get(key); ok {
		return v.(*ShowPage), nil
	}
	var resp struct {
		pageMeta
		Results []Show `json:"results"`
	}
	if err := c.get(ctx, path, params, &resp); err != nil {
		return nil, err
	}
	m := resp.pageMeta.clamped()
	out := &ShowPage{Page: m.Page, TotalPages: m.TotalPages, TotalResults: m.TotalResults, Results: resp.Results}
	if out.Results == nil {
		out.Results = []Show{}
	}
	c.pages.put(key, out)
	return out, nil
}

// DiscoverMovies browses TMDB's movies by genre, year and sort order.
func (c *Client) DiscoverMovies(ctx context.Context, q DiscoverQuery) (*MoviePage, error) {
	return c.moviePage(ctx, "/discover/movie", q.params("primary_release_date", "primary_release_year"))
}

// DiscoverTV is DiscoverMovies for shows.
func (c *Client) DiscoverTV(ctx context.Context, q DiscoverQuery) (*ShowPage, error) {
	return c.showPage(ctx, "/discover/tv", q.params("first_air_date", "first_air_date_year"))
}

// Discover list names accepted by MovieList and TVList.
const (
	ListTrending = "trending" // TMDB's trending this week
	ListPopular  = "popular"  // TMDB's popular list
	ListUpcoming = "upcoming" // not released (or first aired) yet, most popular first
)

// ValidList reports whether list is one of the List* names.
func ValidList(list string) bool {
	switch list {
	case ListTrending, ListPopular, ListUpcoming:
		return true
	}
	return false
}

// today is the date "upcoming" lists start from, as TMDB's YYYY-MM-DD.
func (c *Client) today() string {
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	return now().UTC().Format("2006-01-02")
}

// upcomingParams asks a discover endpoint for titles dated today or later,
// most popular first. dateField is "primary_release_date" or
// "first_air_date"; primary release dates are worldwide, so the list does
// not depend on a region.
func (c *Client) upcomingParams(dateField string, page int) url.Values {
	return url.Values{
		"include_adult": {"false"}, "language": {"en-US"},
		dateField + ".gte": {c.today()},
		"sort_by":          {"popularity.desc"},
		"page":             {strconv.Itoa(ClampPage(page))},
	}
}

// MovieList fetches one page (TMDB serves 20 per page, up to page 500) of
// the trending, popular or upcoming movies.
func (c *Client) MovieList(ctx context.Context, list string, page int) (*MoviePage, error) {
	paged := url.Values{"page": {strconv.Itoa(ClampPage(page))}}
	switch list {
	case ListTrending:
		return c.moviePage(ctx, "/trending/movie/week", paged)
	case ListPopular:
		return c.moviePage(ctx, "/movie/popular", paged)
	case ListUpcoming:
		return c.moviePage(ctx, "/discover/movie", c.upcomingParams("primary_release_date", page))
	}
	return nil, fmt.Errorf("unknown discover list %q", list)
}

// TVList is MovieList for shows; "upcoming" means shows whose first episode
// has not aired yet.
func (c *Client) TVList(ctx context.Context, list string, page int) (*ShowPage, error) {
	paged := url.Values{"page": {strconv.Itoa(ClampPage(page))}}
	switch list {
	case ListTrending:
		return c.showPage(ctx, "/trending/tv/week", paged)
	case ListPopular:
		return c.showPage(ctx, "/tv/popular", paged)
	case ListUpcoming:
		return c.showPage(ctx, "/discover/tv", c.upcomingParams("first_air_date", page))
	}
	return nil, fmt.Errorf("unknown discover list %q", list)
}
