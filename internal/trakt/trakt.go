// Package trakt resolves a public Trakt list URL into TMDB movie IDs (PRD
// §7 Phase 3 — "curated/public list import (e.g. IMDb/Trakt lists)"). Only
// Trakt is implemented: IMDb has no public API for lists (its CSV export
// requires being logged in as the list owner, even for "public" lists,
// which isn't something this app can do without asking for IMDb
// credentials) — Trakt's list-items endpoint is public, documented, and
// stable, and every item it returns already carries a tmdb id, which is
// all this app actually needs (TMDB is the metadata source of truth
// everywhere else — see internal/metadata).
package trakt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const apiBaseURL = "https://api.trakt.tv"

// ErrNoClientID is returned by any call when no Trakt API client ID is
// configured yet — callers should surface this as a "set up Trakt in
// Settings" prompt, same convention as metadata.ErrNoAPIKey.
var ErrNoClientID = fmt.Errorf("no Trakt API client ID configured")

// ErrInvalidClientID is returned when Trakt rejects the client ID.
var ErrInvalidClientID = fmt.Errorf("Trakt rejected this client ID")

type Client struct {
	clientID   string
	baseURL    string
	httpClient *http.Client
}

// New constructs a client. clientID may be empty; calls return
// ErrNoClientID until SetClientID is used or a new Client is built with
// one.
func New(clientID string) *Client {
	return &Client{
		clientID:   clientID,
		baseURL:    apiBaseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// NewWithBaseURL is used by tests to point the client at a local fixture
// server instead of the real Trakt API (CLAUDE.md: local fixtures, not
// live network calls, for tests).
func NewWithBaseURL(clientID, base string) *Client {
	c := New(clientID)
	c.baseURL = base
	return c
}

func (c *Client) SetClientID(id string) { c.clientID = id }

// WithClientID returns a separate client using another client ID against
// the same server, to test an ID before saving it.
func (c *Client) WithClientID(id string) *Client {
	n := New(id)
	n.baseURL = c.baseURL
	return n
}

// Ping checks the client ID with a tiny public request (trending movies).
func (c *Client) Ping(ctx context.Context) error {
	if c.clientID == "" {
		return ErrNoClientID
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/movies/trending?limit=1", nil)
	if err != nil {
		return fmt.Errorf("build trakt request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("trakt-api-version", "2")
	req.Header.Set("trakt-api-key", c.clientID)
	req.Header.Set("User-Agent", "Mediarium (+https://github.com/ryanborg/mediarium)")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("trakt request: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrInvalidClientID
	}
	return fmt.Errorf("trakt request: unexpected status %d", resp.StatusCode)
}

func (c *Client) HasClientID() bool { return c.clientID != "" }

// ListItem is the subset of a Trakt list entry this app needs — just
// enough to look the movie up in TMDB (internal/metadata.Client.GetMovie),
// which is where poster/overview/etc. actually come from.
type ListItem struct {
	Title  string
	Year   int
	TMDBID int
}

// ParseListURL extracts the user slug and list slug/id from a Trakt list
// URL, e.g. "https://trakt.tv/users/garycrawfordgeorge/lists/imdb-top-250"
// (with or without a scheme, trailing slash, or query string) -> ("garycrawfordgeorge", "imdb-top-250").
func ParseListURL(rawURL string) (user, listID string, err error) {
	trimmed := strings.TrimSpace(rawURL)
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", "", fmt.Errorf("parse list URL: %w", err)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	// Expect: users/{user}/lists/{list_id}
	if len(parts) < 4 || parts[0] != "users" || parts[2] != "lists" {
		return "", "", fmt.Errorf("not a Trakt list URL — expected .../users/<user>/lists/<list>, got %q", rawURL)
	}
	if parts[1] == "" || parts[3] == "" {
		return "", "", fmt.Errorf("not a Trakt list URL — missing user or list segment in %q", rawURL)
	}
	return parts[1], parts[3], nil
}

// listItem mirrors Trakt's list-items response shape
// (https://api.trakt.tv/users/{id}/lists/{list_id}/items/movie) — only the
// fields this app reads.
type listItem struct {
	Movie struct {
		Title string `json:"title"`
		Year  int    `json:"year"`
		IDs   struct {
			TMDB int `json:"tmdb"`
		} `json:"ids"`
	} `json:"movie"`
}

// ListMovies fetches every movie item on a public Trakt list. Items
// without a TMDB id (Trakt couldn't match them to TMDB) are skipped —
// there's nothing this app can do with them since TMDB is the only
// metadata source it knows how to enrich from.
func (c *Client) ListMovies(ctx context.Context, user, listID string) ([]ListItem, error) {
	if c.clientID == "" {
		return nil, ErrNoClientID
	}
	path := fmt.Sprintf("/users/%s/lists/%s/items/movie", url.PathEscape(user), url.PathEscape(listID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?limit=all", nil)
	if err != nil {
		return nil, fmt.Errorf("build trakt request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("trakt-api-version", "2")
	req.Header.Set("trakt-api-key", c.clientID)
	req.Header.Set("User-Agent", "Mediarium (+https://github.com/ryanborg/mediarium)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trakt request %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trakt request %s: unexpected status %d", path, resp.StatusCode)
	}
	var items []listItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("decode trakt response %s: %w", path, err)
	}

	out := make([]ListItem, 0, len(items))
	for _, it := range items {
		if it.Movie.IDs.TMDB == 0 {
			continue
		}
		out = append(out, ListItem{Title: it.Movie.Title, Year: it.Movie.Year, TMDBID: it.Movie.IDs.TMDB})
	}
	return out, nil
}
