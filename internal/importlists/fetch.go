package importlists

import (
	"context"
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

// ErrBlocked is returned when a site answers a program with a check meant for
// people (a "prove you are human" page, a sign-in page). Mediarium doesn't try
// to get past those.
var ErrBlocked = errors.New("That site only lets people in with a browser, not programs. Export the list in your browser and upload the file here instead")

// RateLimitError is returned when a service asks to slow down.
type RateLimitError struct {
	Service    string
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("%s asked to slow down. Try again in %d seconds", e.Service, int(e.RetryAfter.Seconds()))
	}
	return e.Service + " asked to slow down. Try again in a few minutes"
}

// newClient is the HTTP client for list addresses: no connections to
// metadata services, redirects checked, a timeout.
func newClient() *http.Client { return netguard.Client(30 * time.Second) }

// retryAfter reads a Retry-After header given in seconds.
func retryAfter(h http.Header) time.Duration {
	secs, _ := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After")))
	if secs < 0 {
		secs = 0
	}
	// A day at most: a huge number would overflow the duration into nonsense.
	secs = min(secs, 24*60*60)
	return time.Duration(secs) * time.Second
}

// looksBlocked reports whether an answer is a bot check or a sign-in page
// rather than the file that was asked for.
func looksBlocked(resp *http.Response, head []byte) bool {
	if strings.Contains(strings.ToLower(resp.Header.Get("x-amzn-waf-action")), "challenge") ||
		strings.Contains(strings.ToLower(resp.Header.Get("cf-mitigated")), "challenge") {
		return true
	}
	if resp.StatusCode == http.StatusAccepted && len(head) == 0 {
		return true // IMDb answers a program with an empty 202 and a challenge header
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "text/html") {
		low := strings.ToLower(string(head))
		return strings.Contains(low, "just a moment") || strings.Contains(low, "captcha") || strings.Contains(low, "sign in") ||
			strings.Contains(low, "<html")
	}
	return false
}

var imdbListID = regexp.MustCompile(`^ls\d{3,}$`)

// CSVAddress turns the address a person typed into the one to download: an
// IMDb list page becomes its export address, any other http or https address
// is used as it is. A bare IMDb list id (ls055592025) is accepted too.
func CSVAddress(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if imdbListID.MatchString(raw) {
		return "https://www.imdb.com/list/" + raw + "/export", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("That isn't a web address")
	}
	if h := strings.ToLower(u.Hostname()); h == "imdb.com" || h == "www.imdb.com" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 2 && parts[0] == "list" && imdbListID.MatchString(parts[1]) {
			return "https://www.imdb.com/list/" + parts[1] + "/export", nil
		}
		if len(parts) >= 3 && parts[0] == "user" && parts[2] == "watchlist" {
			return "", errors.New("IMDb doesn't give a watchlist an export address Mediarium can use. Export the watchlist in your browser and upload the file here instead")
		}
		return "", errors.New("That isn't the address of an IMDb list. It looks like https://www.imdb.com/list/ls055592025/")
	}
	return u.String(), nil
}

// DownloadCSV fetches a list file from an address and returns its text. A
// site that answers with a bot check or a sign-in page gives ErrBlocked.
func DownloadCSV(ctx context.Context, address string) (string, error) {
	return downloadCSV(ctx, newClient(), address)
}

func downloadCSV(ctx context.Context, client *http.Client, address string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", fmt.Errorf("build list request: %w", err)
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "text/csv, text/plain;q=0.9, */*;q=0.1")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download list: %w", netguard.CleanError(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxCSVBytes+1))
	if err != nil {
		return "", fmt.Errorf("download list: %w", err)
	}
	if len(body) > MaxCSVBytes {
		return "", errors.New("That file is too big to be a list")
	}
	head := body
	if len(head) > 2048 {
		head = head[:2048]
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return "", &RateLimitError{Service: "That site", RetryAfter: retryAfter(resp.Header)}
	case looksBlocked(resp, head):
		return "", ErrBlocked
	case resp.StatusCode == http.StatusNotFound:
		return "", errors.New("There is nothing at that address")
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return "", ErrBlocked
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("download list: unexpected status %d", resp.StatusCode)
	}
	return string(body), nil
}
