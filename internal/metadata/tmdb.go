// Package metadata resolves titles against TMDB (PRD.md §4.4) and powers
// the Discover panel's trending/popular lists (§7). Talks to TMDB's REST
// API directly over net/http rather than an unofficial third-party Go SDK.
package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const baseURL = "https://api.themoviedb.org/3"

// ErrNoAPIKey is returned by any call when no TMDB API key is configured
// yet — callers should surface this as a "set up metadata in Settings"
// prompt rather than a generic failure.
var ErrNoAPIKey = fmt.Errorf("no TMDB API key configured")

// ErrInvalidKey is returned when TMDB rejects the API key.
var ErrInvalidKey = fmt.Errorf("TMDB rejected this API key")

type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client

	// genres caches TMDB's id -> name genre tables ("movie" and "tv"), each
	// fetched at most once successfully per process (see GenreNames).
	genreMu sync.Mutex
	genres  map[string]map[int]string
}

// New constructs a client. apiKey may be empty; calls will return
// ErrNoAPIKey until SetAPIKey is used or a new Client is built with one.
func New(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// NewWithBaseURL is used by tests to point the client at a local fixture
// server instead of the real TMDB API (CLAUDE.md: local fixtures, not live
// network calls, for tests).
func NewWithBaseURL(apiKey, base string) *Client {
	c := New(apiKey)
	c.baseURL = base
	return c
}

func (c *Client) SetAPIKey(key string) { c.apiKey = key }

// WithAPIKey returns a separate client using another key against the same
// server, to test a key before saving it.
func (c *Client) WithAPIKey(key string) *Client {
	n := New(key)
	n.baseURL = c.baseURL
	return n
}

func (c *Client) HasAPIKey() bool { return c.apiKey != "" }

// Movie is the subset of TMDB's movie fields the rest of the app needs.
type Movie struct {
	TMDBID      int     `json:"id"`
	Title       string  `json:"title"`
	Overview    string  `json:"overview"`
	ReleaseDate string  `json:"release_date"`
	PosterPath  string  `json:"poster_path"`
	VoteAverage float64 `json:"vote_average"`
	VoteCount   int     `json:"vote_count"`
	GenreIDs    []int   `json:"genre_ids"`
	Genres      []Genre `json:"genres"` // only on the single-movie endpoint
}

// Year extracts the 4-digit release year from ReleaseDate ("2024-03-01"),
// or 0 if unknown/unparsed.
func (m Movie) Year() int {
	if len(m.ReleaseDate) < 4 {
		return 0
	}
	var y int
	if _, err := fmt.Sscanf(m.ReleaseDate[:4], "%d", &y); err != nil {
		return 0
	}
	return y
}

type pagedMovies struct {
	Page    int     `json:"page"`
	Results []Movie `json:"results"`
}

// SearchMovies queries TMDB's movie search endpoint.
func (c *Client) SearchMovies(ctx context.Context, query string) ([]Movie, error) {
	return c.getMovieList(ctx, "/search/movie", url.Values{"query": {query}})
}

// MultiResult is one hit from TMDB's combined movie + TV search.
type MultiResult struct {
	MediaType    string  `json:"media_type"` // "movie" or "tv"
	ID           int     `json:"id"`
	Title        string  `json:"title"` // movies
	Name         string  `json:"name"`  // shows
	ReleaseDate  string  `json:"release_date"`
	FirstAirDate string  `json:"first_air_date"`
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"poster_path"`
	Popularity   float64 `json:"popularity"`
	VoteAverage  float64 `json:"vote_average"`
	VoteCount    int     `json:"vote_count"`
	GenreIDs     []int   `json:"genre_ids"`
}

// DisplayTitle is the movie title or show name.
func (m MultiResult) DisplayTitle() string {
	if m.MediaType == "tv" {
		return m.Name
	}
	return m.Title
}

// Year is the release or first-air year, or 0.
func (m MultiResult) Year() int {
	date := m.ReleaseDate
	if m.MediaType == "tv" {
		date = m.FirstAirDate
	}
	if len(date) < 4 {
		return 0
	}
	var y int
	if _, err := fmt.Sscanf(date[:4], "%d", &y); err != nil {
		return 0
	}
	return y
}

// SearchMulti searches movies and TV shows together, ranked by TMDB's own
// relevance. People results are dropped.
func (c *Client) SearchMulti(ctx context.Context, query string) ([]MultiResult, error) {
	var page struct {
		Results []MultiResult `json:"results"`
	}
	if err := c.get(ctx, "/search/multi", url.Values{"query": {query}, "include_adult": {"false"}}, &page); err != nil {
		return nil, err
	}
	out := page.Results[:0]
	for _, r := range page.Results {
		if r.MediaType == "movie" || r.MediaType == "tv" {
			out = append(out, r)
		}
	}
	return out, nil
}

// GetMovie fetches full details for a single TMDB movie ID.
func (c *Client) GetMovie(ctx context.Context, tmdbID int) (*Movie, error) {
	var m Movie
	if err := c.get(ctx, fmt.Sprintf("/movie/%d", tmdbID), nil, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// TrendingMovies powers the Discover panel's "trending" rail (PRD §7).
func (c *Client) TrendingMovies(ctx context.Context) ([]Movie, error) {
	return c.getMovieList(ctx, "/trending/movie/week", nil)
}

// PopularMovies powers the Discover panel's "popular" rail (PRD §7).
func (c *Client) PopularMovies(ctx context.Context) ([]Movie, error) {
	return c.getMovieList(ctx, "/movie/popular", nil)
}

// SimilarMovies powers Discover's "because you added X" recommendations
// (PRD §7 Phase 3 — "similar to items in your library").
func (c *Client) SimilarMovies(ctx context.Context, tmdbID int) ([]Movie, error) {
	return c.getMovieList(ctx, fmt.Sprintf("/movie/%d/similar", tmdbID), nil)
}

func (c *Client) getMovieList(ctx context.Context, path string, extra url.Values) ([]Movie, error) {
	var page pagedMovies
	if err := c.get(ctx, path, extra, &page); err != nil {
		return nil, err
	}
	return page.Results, nil
}

func (c *Client) get(ctx context.Context, path string, extra url.Values, out any) error {
	if c.apiKey == "" {
		return ErrNoAPIKey
	}
	q := url.Values{}
	for k, v := range extra {
		q[k] = v
	}
	q.Set("api_key", c.apiKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("build tmdb request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("tmdb request %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return ErrInvalidKey
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tmdb request %s: unexpected status %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode tmdb response %s: %w", path, err)
	}
	return nil
}

// PosterURL builds a full poster image URL from a poster_path fragment
// TMDB returns, using its "w500" size — good enough for a poster-grid
// library view (PRD §6) without the app needing to know all TMDB's image
// size variants.
func PosterURL(posterPath string) string {
	if posterPath == "" {
		return ""
	}
	return "https://image.tmdb.org/t/p/w500" + posterPath
}
