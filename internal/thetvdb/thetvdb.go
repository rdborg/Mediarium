// Package thetvdb reads a show's episodes from TheTVDB (https://thetvdb.com)
// with the person's own API key, as a second source for shows whose TMDB page
// is missing seasons or episodes (Formula 1 has one episode on TMDB and every
// season on TheTVDB). It is off until a key is saved in Settings.
//
// Only the show's TheTVDB id is sent, never anything about the person or the
// library. TheTVDB asks apps that use its data to say so, so the app credits
// it. Requests say who Mediarium is in the User-Agent and go through netguard.
//
// The package depends on nothing else in Mediarium except netguard.
package thetvdb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
)

// DefaultBaseURL is TheTVDB's v4 API.
const DefaultBaseURL = "https://api4.thetvdb.com/v4"

const (
	requestTimeout = 20 * time.Second
	maxBodyBytes   = 16 << 20
	// tokenLife is how long a login is used. TheTVDB's tokens last a month.
	tokenLife = 24 * time.Hour * 25
	// maxPages bounds one show's episode list (500 episodes a page).
	maxPages = 40
)

// UserAgent is what Mediarium calls itself. The app sets it to include its version.
var UserAgent = "Mediarium (+https://mediarium.app)"

var (
	// ErrNoKey is returned when no API key is saved.
	ErrNoKey = errors.New("no TheTVDB API key is saved")
	// ErrBadKey is returned when TheTVDB turns the key (or PIN) down.
	ErrBadKey = errors.New("TheTVDB didn't accept the API key or PIN")
	// ErrNotFound is returned when TheTVDB doesn't know the show.
	ErrNotFound = errors.New("TheTVDB doesn't know that show")
	// ErrRateLimited is returned when TheTVDB asks to slow down.
	ErrRateLimited = errors.New("TheTVDB asked to slow down")
)

// Episode is one episode in TheTVDB's default (aired) order.
type Episode struct {
	Season   int
	Number   int
	Name     string
	Aired    string // YYYY-MM-DD, empty when not known
	Overview string
}

// Client talks to TheTVDB with one API key. It is safe for concurrent use and
// logs in once, keeping the token.
type Client struct {
	base, key, pin string
	http           *http.Client

	mu      sync.Mutex
	token   string
	tokenAt time.Time
}

// New makes a client for key (and the subscriber PIN, which may be empty).
func New(key, pin string) *Client { return NewAt(DefaultBaseURL, key, pin) }

// NewAt is New with another API address (tests).
func NewAt(base, key, pin string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), key: strings.TrimSpace(key), pin: strings.TrimSpace(pin), http: netguard.Client(requestTimeout)}
}

// HasKey reports whether a key is set.
func (c *Client) HasKey() bool { return c != nil && c.key != "" }

func (c *Client) login(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Since(c.tokenAt) < tokenLife {
		return c.token, nil
	}
	body := map[string]string{"apikey": c.key}
	if c.pin != "" {
		body["pin"] = c.pin
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/login", bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("thetvdb login: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := c.do(req, &out); err != nil {
		if errors.Is(err, errUnauthorized) {
			return "", ErrBadKey
		}
		return "", err
	}
	if out.Data.Token == "" {
		return "", ErrBadKey
	}
	c.token, c.tokenAt = out.Data.Token, time.Now()
	return c.token, nil
}

// Check logs in, to test a key.
func (c *Client) Check(ctx context.Context) error {
	if !c.HasKey() {
		return ErrNoKey
	}
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
	_, err := c.login(ctx)
	return err
}

var errUnauthorized = errors.New("unauthorized")

func (c *Client) do(req *http.Request, out any) error {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach TheTVDB: %w", netguard.CleanError(err))
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errUnauthorized
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		return ErrRateLimited
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("TheTVDB answered with status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(out); err != nil {
		return fmt.Errorf("read TheTVDB's answer: %w", err)
	}
	return nil
}

// Episodes lists a show's episodes in TheTVDB's default (aired) order.
func (c *Client) Episodes(ctx context.Context, seriesID int) ([]Episode, error) {
	if !c.HasKey() {
		return nil, ErrNoKey
	}
	var out []Episode
	for page := 0; page < maxPages; page++ {
		token, err := c.login(ctx)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/series/%d/episodes/default?page=%d", c.base, seriesID, page), nil)
		if err != nil {
			return nil, fmt.Errorf("thetvdb episodes: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		var ans struct {
			Data struct {
				Episodes []struct {
					SeasonNumber int    `json:"seasonNumber"`
					Number       int    `json:"number"`
					Name         string `json:"name"`
					Aired        string `json:"aired"`
					Overview     string `json:"overview"`
				} `json:"episodes"`
			} `json:"data"`
			Links struct {
				Next *string `json:"next"`
			} `json:"links"`
		}
		if err := c.do(req, &ans); err != nil {
			if errors.Is(err, errUnauthorized) {
				c.mu.Lock()
				c.token = "" // an old token: log in again next time
				c.mu.Unlock()
				return nil, ErrBadKey
			}
			return nil, err
		}
		for _, e := range ans.Data.Episodes {
			if e.Number <= 0 || e.SeasonNumber < 0 {
				continue
			}
			aired := e.Aired
			if len(aired) > 10 {
				aired = aired[:10]
			}
			out = append(out, Episode{Season: e.SeasonNumber, Number: e.Number, Name: strings.TrimSpace(e.Name), Aired: aired, Overview: strings.TrimSpace(e.Overview)})
		}
		if ans.Links.Next == nil || *ans.Links.Next == "" || len(ans.Data.Episodes) == 0 {
			break
		}
	}
	return out, nil
}
