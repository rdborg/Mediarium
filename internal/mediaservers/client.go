package mediaservers

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
)

// Client makes the HTTP calls to media servers. It holds no per-server
// state; caching lives in Finder and Refresher.
type Client struct {
	HTTP    *http.Client
	Version string // Mediarium's version, sent to Jellyfin in its auth header
	// MaxBytes is the most of an answer that is read; 0 means maxAnswerBytes.
	MaxBytes int64
}

// maxAnswerBytes is the most read of a media server's answer (a library
// listing can be large).
const maxAnswerBytes = 64 << 20

// NewClient returns a client with sensible timeouts.
func NewClient(version string) *Client {
	if version == "" {
		version = "dev"
	}
	return &Client{HTTP: netguard.Client(20 * time.Second), Version: version}
}

// Library is one library (Plex "section", Jellyfin/Emby "virtual folder").
type Library struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Type      string   `json:"type"` // movie | show (Plex); movies | tvshows | ... (Jellyfin/Emby)
	Locations []string `json:"locations"`
}

// TestResult is what a successful connection test learned about the server.
type TestResult struct {
	ServerName string    `json:"serverName,omitempty"`
	Version    string    `json:"version,omitempty"`
	ServerID   string    `json:"serverId,omitempty"` // Plex machineIdentifier / Jellyfin or Emby server Id
	Libraries  []Library `json:"libraries"`
}

// Item is a title found on a server.
type Item struct {
	ID     string // Plex ratingKey / Jellyfin or Emby item Id
	URL    string // where to open it
	AppURL string // Plex only: the same item on app.plex.tv
}

var (
	errUnauthorized = errors.New("unauthorized")
	errNotFound     = errors.New("not found")
	errUnrecognised = errors.New("unrecognised response")
)

// statusError is a non-2xx answer other than 401/403/404.
type statusError struct{ code int }

func (e *statusError) Error() string { return fmt.Sprintf("HTTP %d", e.code) }

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// do sends one request to the server and decodes the answer into out (JSON
// or XML, going by the Content-Type) when out is not nil. The token travels
// in headers only, never in the URL, so it cannot leak into an error message
// or a log line.
func (c *Client) do(ctx context.Context, s Server, method, path string, query url.Values, body any, out any) error {
	base, err := NormalizeURL(s.BaseURL)
	if err != nil {
		return userErr(err, "The server address is not valid: %v", err)
	}
	req, err := newRequest(ctx, s.Kind, method, base, path, query, body)
	if err != nil {
		return err
	}
	c.authorize(req, s)
	return c.exchange(req, s, base, out)
}

// newRequest builds a request to base+path with a JSON body (when body is
// not nil) that asks for a JSON answer.
func newRequest(ctx context.Context, kind Kind, method, base, path string, query url.Values, body any) (*http.Request, error) {
	target := base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode %s request body: %w", kind.Label(), err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", kind.Label(), netguard.CleanError(err))
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// exchange sends req and decodes the answer into out (see do).
func (c *Client) exchange(req *http.Request, s Server, base string, out any) error {
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return unreachable(s, base, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return errUnauthorized
	case resp.StatusCode == http.StatusNotFound:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return errNotFound
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return &statusError{code: resp.StatusCode}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil
	}
	limit := c.MaxBytes
	if limit <= 0 {
		limit = maxAnswerBytes
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return fmt.Errorf("read %s response: %w", s.Kind.Label(), err)
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "xml") || bytes.HasPrefix(bytes.TrimSpace(data), []byte("<")) {
		if err := xml.Unmarshal(data, out); err != nil {
			return fmt.Errorf("%w: %v", errUnrecognised, err)
		}
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: %v", errUnrecognised, err)
	}
	return nil
}

func (c *Client) authorize(req *http.Request, s Server) {
	req.Header.Set("User-Agent", "Mediarium/"+c.Version)
	switch s.Kind {
	case KindPlex:
		req.Header.Set("X-Plex-Product", "Mediarium")
		req.Header.Set("X-Plex-Client-Identifier", "mediarium")
		if s.Token != "" {
			req.Header.Set("X-Plex-Token", s.Token)
		}
	case KindJellyfin:
		if s.Token != "" {
			req.Header.Set("X-Emby-Token", s.Token)
			// Newer Jellyfin versions can switch the legacy header off.
			req.Header.Set("Authorization", mediaBrowserAuth("mediarium", c.Version, s.Token))
		}
	case KindEmby:
		if s.Token != "" {
			req.Header.Set("X-Emby-Token", s.Token)
		}
	}
}

// mediaBrowserAuth is the Authorization header Jellyfin and Emby expect
// from an app: who is asking and, once signed in, the access token.
func mediaBrowserAuth(deviceID, version, token string) string {
	h := fmt.Sprintf(`MediaBrowser Client="Mediarium", Device="Mediarium", DeviceId=%q, Version=%q`, deviceID, version)
	if token != "" {
		h = fmt.Sprintf(`MediaBrowser Token=%q, Client="Mediarium", Device="Mediarium", DeviceId=%q, Version=%q`, token, deviceID, version)
	}
	return h
}

func unreachable(s Server, base string, err error) error {
	base = netguard.RedactURL(base) // an address can hold a user name and password
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return userErr(err, "%s at %s did not answer in time. Check that the server is running and the address and port are right.", s.Kind.Label(), base)
	}
	if strings.Contains(err.Error(), "certificate") || strings.Contains(err.Error(), "tls:") {
		return userErr(err, "Could not make a secure connection to %s at %s (certificate problem). Try the http:// address on your local network instead.", s.Kind.Label(), base)
	}
	return userErr(err, "Could not reach %s at %s. Check the address and port, and that the server is running and reachable from Mediarium.", s.Kind.Label(), base)
}

func tokenWord(k Kind) string {
	if k == KindPlex {
		return "token"
	}
	return "API key"
}

// explain turns a low-level error from do into the message people see.
func explain(s Server, err error) error {
	var ue *UserError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &ue):
		return err
	case errors.Is(err, errUnauthorized):
		if s.Token == "" {
			return userErr(err, "%s needs a %s. Add it and test again.", s.Kind.Label(), tokenWord(s.Kind))
		}
		return userErr(err, "%s refused the %s. Check that it was copied completely and is still valid.", s.Kind.Label(), tokenWord(s.Kind))
	case errors.Is(err, errNotFound), errors.Is(err, errUnrecognised):
		article := "a"
		if s.Kind == KindEmby {
			article = "an"
		}
		return userErr(err, "That address answered, but it does not look like %s %s server. Check the address and port (and the server type).", article, s.Kind.Label())
	}
	var se *statusError
	if errors.As(err, &se) {
		return userErr(err, "%s answered with an error (%s). Check the server's own logs.", s.Kind.Label(), se.Error())
	}
	return err
}

// Test checks the address and token and lists the server's libraries.
func (c *Client) Test(ctx context.Context, s Server) (TestResult, error) {
	switch s.Kind {
	case KindPlex:
		return c.plexTest(ctx, s)
	case KindJellyfin, KindEmby:
		return c.embyTest(ctx, s)
	}
	return TestResult{}, userErr(nil, "Unknown server type %q.", s.Kind)
}

// RefreshAll asks the server to scan every library.
func (c *Client) RefreshAll(ctx context.Context, s Server) error {
	switch s.Kind {
	case KindPlex:
		return c.plexRefreshAll(ctx, s)
	case KindJellyfin, KindEmby:
		return explain(s, c.do(ctx, s, http.MethodPost, "/Library/Refresh", nil, nil, nil))
	}
	return userErr(nil, "Unknown server type %q.", s.Kind)
}

// RefreshFolders asks the server to scan the given folders (as Mediarium
// sees them; the server's path mapping is applied here). It returns a short
// description of each scan it started, for the log.
func (c *Client) RefreshFolders(ctx context.Context, s Server, kind MediaKind, folders []string) ([]string, error) {
	switch s.Kind {
	case KindPlex:
		return c.plexRefreshFolders(ctx, s, kind, folders)
	case KindJellyfin, KindEmby:
		return c.embyRefreshFolders(ctx, s, folders)
	}
	return nil, userErr(nil, "Unknown server type %q.", s.Kind)
}

// Find looks a title up by its TMDB id. found is false when the server
// doesn't have it.
func (c *Client) Find(ctx context.Context, s Server, kind MediaKind, tmdbID int) (item Item, found bool, err error) {
	switch s.Kind {
	case KindPlex:
		return c.plexFind(ctx, s, kind, tmdbID, nil)
	case KindJellyfin, KindEmby:
		return c.embyFind(ctx, s, kind, tmdbID)
	}
	return Item{}, false, userErr(nil, "Unknown server type %q.", s.Kind)
}

// HomeURL is where "Open my media server" goes.
func HomeURL(s Server) string {
	switch s.Kind {
	case KindPlex:
		if strings.TrimSpace(s.PublicURL) != "" {
			return s.WebURL() + "/web/index.html"
		}
		if s.ServerID != "" {
			return "https://app.plex.tv/desktop/#!/media/" + url.PathEscape(s.ServerID) + "/com.plexapp.plugins.library"
		}
		return "https://app.plex.tv/desktop/"
	case KindJellyfin:
		return s.WebURL() + "/web/"
	case KindEmby:
		return s.WebURL() + "/web/index.html"
	}
	return s.WebURL()
}
