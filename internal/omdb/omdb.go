// Package omdb reads a film's or show's scores from OMDb (https://www.omdbapi.com):
// IMDb's rating, Rotten Tomatoes' critics score and Metacritic's, with the
// person's own OMDb key (#169). It is off until a key is saved in Settings.
//
// Only the title's IMDb id is sent, never anything about the person or the
// library. Requests go through netguard.
//
// The package depends on nothing else in Mediarium except netguard.
package omdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
)

// DefaultBaseURL is OMDb's API.
const DefaultBaseURL = "https://www.omdbapi.com/"

const (
	requestTimeout = 15 * time.Second
	maxBodyBytes   = 1 << 20
)

var (
	// ErrBadKey is returned when OMDb turns the key down (or it isn't
	// activated yet: OMDb mails a link that has to be opened first).
	ErrBadKey = errors.New("OMDb didn't accept the API key")
	// ErrLimit is returned when the key has used its lookups for the day.
	ErrLimit = errors.New("the OMDb key has used its lookups for today")
	// ErrNotFound is returned when OMDb doesn't know the title.
	ErrNotFound = errors.New("OMDb doesn't know that title")
)

// Scores are what OMDb knows about one title. A zero value means OMDb has no
// such score.
type Scores struct {
	IMDb           float64 // 0 to 10
	IMDbVotes      int
	RottenTomatoes int // critics, percent
	Metacritic     int // 0 to 100
}

// imdbID is what an IMDb id looks like: tt and digits.
var imdbID = regexp.MustCompile(`^tt\d{5,10}$`)

// ValidID reports whether id looks like an IMDb title id.
func ValidID(id string) bool { return imdbID.MatchString(id) }

// Client asks OMDb with one key. It is safe for concurrent use.
type Client struct {
	base, key string
	http      *http.Client
}

// New makes a client for key.
func New(key string) *Client { return NewAt(DefaultBaseURL, key) }

// NewAt is New with another API address (tests).
func NewAt(base, key string) *Client {
	return &Client{base: base, key: strings.TrimSpace(key), http: netguard.Client(requestTimeout)}
}

type answer struct {
	Response   string `json:"Response"`
	Error      string `json:"Error"`
	IMDbRating string `json:"imdbRating"`
	IMDbVotes  string `json:"imdbVotes"`
	Metascore  string `json:"Metascore"`
	Ratings    []struct {
		Source string `json:"Source"`
		Value  string `json:"Value"`
	} `json:"Ratings"`
}

// Get reads the scores of the title with IMDb id id.
func (c *Client) Get(ctx context.Context, id string) (Scores, error) {
	if !ValidID(id) {
		return Scores{}, ErrNotFound
	}
	q := url.Values{"apikey": {c.key}, "i": {id}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"?"+q.Encode(), nil)
	if err != nil {
		return Scores{}, fmt.Errorf("omdb: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		// The address has the key in it: never pass it on in the error.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return Scores{}, fmt.Errorf("omdb: ask for %s: %w", id, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return Scores{}, fmt.Errorf("omdb: read the answer for %s: %w", id, err)
	}
	var a answer
	if err := json.Unmarshal(body, &a); err != nil {
		if resp.StatusCode == http.StatusUnauthorized {
			return Scores{}, ErrBadKey
		}
		return Scores{}, fmt.Errorf("omdb: answer for %s (status %d) isn't JSON", id, resp.StatusCode)
	}
	if a.Response != "True" {
		return Scores{}, classify(a.Error, resp.StatusCode)
	}
	return parse(a), nil
}

// Check asks for one well-known title, to see that the key works.
func (c *Client) Check(ctx context.Context) error {
	_, err := c.Get(ctx, "tt0111161")
	return err
}

func classify(msg string, status int) error {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "limit"):
		return ErrLimit
	case strings.Contains(m, "invalid api key"), strings.Contains(m, "no api key"), strings.Contains(m, "not activated"), status == http.StatusUnauthorized:
		return ErrBadKey
	case strings.Contains(m, "not found"), strings.Contains(m, "incorrect imdb id"):
		return ErrNotFound
	}
	return fmt.Errorf("omdb: %s (status %d)", msg, status)
}

func parse(a answer) Scores {
	var s Scores
	if v, err := strconv.ParseFloat(a.IMDbRating, 64); err == nil {
		s.IMDb = v
	}
	if v, err := strconv.Atoi(strings.ReplaceAll(a.IMDbVotes, ",", "")); err == nil {
		s.IMDbVotes = v
	}
	if v, err := strconv.Atoi(a.Metascore); err == nil {
		s.Metacritic = v
	}
	for _, r := range a.Ratings {
		switch r.Source {
		case "Rotten Tomatoes":
			if v, err := strconv.Atoi(strings.TrimSuffix(r.Value, "%")); err == nil {
				s.RottenTomatoes = v
			}
		case "Metacritic":
			if s.Metacritic == 0 {
				if v, err := strconv.Atoi(strings.TrimSuffix(r.Value, "/100")); err == nil {
					s.Metacritic = v
				}
			}
		}
	}
	return s
}
