// Package metadata resolves titles against TMDB and powers
// the Discover panel's trending/popular lists. Talks to TMDB's REST
// API directly over net/http rather than an unofficial third-party Go SDK.
package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
)

const baseURL = "https://api.themoviedb.org/3"

// ErrNoAPIKey is returned by any call when no TMDB API key is configured
// yet — callers should surface this as a "set up metadata in Settings"
// prompt rather than a generic failure.
var ErrNoAPIKey = fmt.Errorf("no TMDB API key has been added yet")

// ErrInvalidKey is returned when TMDB rejects the API key.
var ErrInvalidKey = fmt.Errorf("TMDB rejected this API key")

// StatusError is returned when TMDB answers a request with a status other
// than 200 (401 is ErrInvalidKey). Callers use Code to tell "TMDB does not
// have that" (404) from "try again later" (429, 5xx).
type StatusError struct {
	Path string
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("tmdb request %s: unexpected status %d", e.Path, e.Code)
}

// maxAPIResponse is the biggest answer read from TMDB; real ones are well under a megabyte.
const maxAPIResponse = 16 << 20

// DefaultLanguage is the language TMDB text comes in unless asked otherwise.
// The titles searched for on indexers are always fetched in it. New movie
// and show folders and file names use the language chosen for what is read
// on screen instead (see api.folderTitle); a title that already has files
// keeps its folder.
const DefaultLanguage = "en-US"

type Client struct {
	apiKey     atomic.Pointer[string] // read while requests run, so changed atomically
	baseURL    string
	httpClient *http.Client

	// language is the TMDB language tag ("de-DE") this client asks for; empty
	// means DefaultLanguage. parent is set on the per-language views made by
	// Localized, which share the key, the server and the connection of the
	// client they came from but keep their own caches.
	language string
	parent   *Client
	locMu    sync.Mutex
	loc      map[string]*Client

	// genres caches TMDB's id -> name genre tables ("movie" and "tv"), each
	// fetched at most once successfully per process (see GenreNames).
	genreMu sync.Mutex
	genres  map[string]map[int]string

	// pages caches Discover list and browse pages for a few minutes.
	pages pageCache

	// now is the clock "upcoming" lists count from; nil means time.Now.
	now func() time.Time
}

// New constructs a client. apiKey may be empty; calls will return
// ErrNoAPIKey until SetAPIKey is used or a new Client is built with one.
func New(apiKey string) *Client {
	c := &Client{
		baseURL:    baseURL,
		httpClient: netguard.Client(10 * time.Second),
	}
	c.apiKey.Store(&apiKey)
	return c
}

// NewWithBaseURL is used by tests to point the client at a local fixture
// server instead of the real TMDB API (tests use local fixtures, not live
// network calls).
func NewWithBaseURL(apiKey, base string) *Client {
	c := New(apiKey)
	c.baseURL = base
	return c
}

func (c *Client) SetAPIKey(key string) { c.apiKey.Store(&key) }

// key is the API key in use: a per-language view reads its parent's, so a key
// saved later reaches every view.
func (c *Client) key() string {
	if c.parent != nil {
		return c.parent.key()
	}
	if k := c.apiKey.Load(); k != nil {
		return *k
	}
	return ""
}

// lang is the language this client asks TMDB for.
func (c *Client) lang() string {
	if c.language == "" {
		return DefaultLanguage
	}
	return c.language
}

// Localized returns a view of the client that asks TMDB for titles, plots,
// genres and posters in the given language (a tag like "de-DE"). It shares the
// key and the connection and is made once per language. An empty tag, or the
// default language, gives the client itself.
func (c *Client) Localized(lang string) *Client {
	if lang == "" || lang == c.lang() {
		return c
	}
	root := c
	if c.parent != nil {
		root = c.parent
	}
	root.locMu.Lock()
	defer root.locMu.Unlock()
	if v, ok := root.loc[lang]; ok {
		return v
	}
	if root.loc == nil {
		root.loc = make(map[string]*Client)
	}
	v := &Client{baseURL: root.baseURL, httpClient: root.httpClient, now: root.now, language: lang, parent: root}
	root.loc[lang] = v
	return v
}

// WrapTransport wraps the HTTP transport this client uses, e.g. to count
// requests. Call it before the client is shared between goroutines.
func (c *Client) WrapTransport(wrap func(http.RoundTripper) http.RoundTripper) {
	rt := c.httpClient.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	c.httpClient.Transport = wrap(rt)
}

// WithAPIKey returns a separate client using another key against the same
// server, to test a key before saving it.
func (c *Client) WithAPIKey(key string) *Client {
	n := New(key)
	n.baseURL = c.baseURL
	return n
}

func (c *Client) HasAPIKey() bool { return c.key() != "" }

// Movie is the subset of TMDB's movie fields the rest of the app needs.
type Movie struct {
	TMDBID      int    `json:"id"`
	Title       string `json:"title"`
	Overview    string `json:"overview"`
	ReleaseDate string `json:"release_date"`
	PosterPath  string `json:"poster_path"`
	// BackdropPath is a wide still from the film, for the title page.
	BackdropPath string  `json:"backdrop_path"`
	VoteAverage  float64 `json:"vote_average"`
	VoteCount    int     `json:"vote_count"`
	Popularity   float64 `json:"popularity"`
	GenreIDs     []int   `json:"genre_ids"`
	Genres       []Genre `json:"genres"` // only on the single-movie endpoint
	// OriginalTitle is the title in the film's own language.
	OriginalTitle string `json:"original_title"`
	// OriginalLanguage is the language the film was made in ("ja").
	OriginalLanguage string `json:"original_language"`
	// BelongsToCollection is the collection (a series of films) this film is
	// part of, on the single-movie endpoint; nil for a film that is on its own.
	BelongsToCollection *CollectionRef `json:"belongs_to_collection"`
	// Adult is TMDB's adult flag.
	Adult bool `json:"adult"`
}

// Year extracts the 4-digit release year from ReleaseDate ("2024-03-01"),
// or 0 if unknown/unparsed.
func (m Movie) Year() int { return yearOfDate(m.ReleaseDate) }

// genreAnimation is TMDB's id for the Animation genre.
const genreAnimation = 16

// IsAnime reports whether the film is anime: animated, and made in Japanese.
func (m Movie) IsAnime() bool {
	if m.OriginalLanguage != "ja" {
		return false
	}
	for _, g := range m.Genres {
		if g.ID == genreAnimation {
			return true
		}
	}
	return slices.Contains(m.GenreIDs, genreAnimation)
}

// yearOfDate reads the year from the start of a TMDB date ("2024-03-01"): four
// digits, or 0. (fmt's %d also takes signs and stops at the first non-digit, so
// "-123" and "12ab" gave years that are none.)
func yearOfDate(date string) int {
	if len(date) < 4 {
		return 0
	}
	y := 0
	for i := 0; i < 4; i++ {
		if date[i] < '0' || date[i] > '9' {
			return 0
		}
		y = y*10 + int(date[i]-'0')
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
	return yearOfDate(date)
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

// TrendingMovies powers the Discover panel's "trending" rail.
func (c *Client) TrendingMovies(ctx context.Context) ([]Movie, error) {
	return c.getMovieList(ctx, "/trending/movie/week", nil)
}

// PopularMovies powers the Discover panel's "popular" rail.
func (c *Client) PopularMovies(ctx context.Context) ([]Movie, error) {
	return c.getMovieList(ctx, "/movie/popular", nil)
}

// SimilarMovies powers Discover's "because you added X" recommendations
// ("similar to items in your library").
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
	if c.key() == "" {
		return ErrNoAPIKey
	}
	q := url.Values{}
	for k, v := range extra {
		q[k] = v
	}
	if q.Get("language") == "" {
		q.Set("language", c.lang())
	}
	q.Set("api_key", c.key())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("build tmdb request: %w", netguard.CleanError(err))
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// net/http repeats the request URL in its errors, and TMDB takes the
		// key as a query parameter: keep it out of anything that shows or logs this.
		return fmt.Errorf("tmdb request %s: %w", path, netguard.CleanError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return ErrInvalidKey
	}
	if resp.StatusCode != http.StatusOK {
		return &StatusError{Path: path, Code: resp.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAPIResponse)).Decode(out); err != nil {
		return fmt.Errorf("decode tmdb response %s: %w", path, err)
	}
	return nil
}

// PosterURL builds a full poster image URL from a poster_path fragment
// TMDB returns, using its "w500" size — good enough for a poster-grid
// library view without the app needing to know all TMDB's image
// size variants.
// BackdropURL builds a wide backdrop image URL ("w1280": sharp across a
// laptop screen without the weight of the original).
func BackdropURL(path string) string {
	if path == "" {
		return ""
	}
	return "https://image.tmdb.org/t/p/w1280" + path
}

func PosterURL(posterPath string) string {
	if posterPath == "" {
		return ""
	}
	return "https://image.tmdb.org/t/p/w500" + posterPath
}
