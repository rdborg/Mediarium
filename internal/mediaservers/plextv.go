package mediaservers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Sign in with Plex uses plex.tv's PIN flow, the one other Plex apps use:
// Mediarium asks plex.tv for a PIN, the person approves it on app.plex.tv
// while signed in to their Plex account, and Mediarium then receives a token
// for that account. With it, it lists the account's servers; each server
// comes with its own access token, and that one is what gets saved. The
// account token is only held in memory for the duration of the sign-in.

// PlexTVURL is plex.tv's address.
const PlexTVURL = "https://plex.tv"

// PlexTV talks to plex.tv.
type PlexTV struct {
	HTTP     *http.Client
	BaseURL  string // PlexTVURL; tests point it at a fake
	ClientID string // this install's X-Plex-Client-Identifier; must stay the same for a PIN's lifetime
	Version  string
}

// PlexPIN is a PIN as plex.tv reports it.
type PlexPIN struct {
	ID        int64  `json:"id"`
	Code      string `json:"code"`
	AuthToken string `json:"authToken"` // empty until the person approves the PIN
	ExpiresIn int    `json:"expiresIn"` // seconds
}

// PlexConnection is one address a Plex server can be reached at.
type PlexConnection struct {
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	URI      string `json:"uri"`
	Local    bool   `json:"local"`
	Relay    bool   `json:"relay"`
	IPv6     bool   `json:"IPv6"`
}

// PlexResource is one server on a Plex account.
type PlexResource struct {
	Name             string           `json:"name"`
	Product          string           `json:"product"`
	ProductVersion   string           `json:"productVersion"`
	ClientIdentifier string           `json:"clientIdentifier"` // the server's machineIdentifier
	Provides         string           `json:"provides"`
	Owned            bool             `json:"owned"`
	AccessToken      string           `json:"accessToken"`
	HTTPSRequired    bool             `json:"httpsRequired"`
	Connections      []PlexConnection `json:"connections"`
}

// ErrPlexPINExpired means the PIN is gone: it timed out or was never ours.
var ErrPlexPINExpired = errors.New("plex pin expired")

func (p *PlexTV) httpClient() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

// call sends one request to plex.tv. The account token travels in a header
// only, so it can't end up in an error message or a log line.
func (p *PlexTV) call(ctx context.Context, method, path string, query url.Values, token string, out any) error {
	base := strings.TrimRight(p.BaseURL, "/")
	if base == "" {
		base = PlexTVURL
	}
	target := base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return fmt.Errorf("build plex.tv request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mediarium/"+p.Version)
	req.Header.Set("X-Plex-Product", "Mediarium")
	req.Header.Set("X-Plex-Version", p.Version)
	req.Header.Set("X-Plex-Client-Identifier", p.ClientID)
	req.Header.Set("X-Plex-Device-Name", "Mediarium")
	if token != "" {
		req.Header.Set("X-Plex-Token", token)
	}
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return userErr(err, "Could not reach plex.tv. Check that Mediarium can reach the internet, then try again.")
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return errNotFound
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return userErr(errUnauthorized, "plex.tv refused the sign-in. Start again.")
	case resp.StatusCode == http.StatusTooManyRequests:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return userErr(&statusError{code: resp.StatusCode}, "plex.tv is asking Mediarium to slow down. Wait a minute and try again.")
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return userErr(&statusError{code: resp.StatusCode}, "plex.tv answered with an error (HTTP %d). Try again in a moment.", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
		return userErr(fmt.Errorf("%w: %v", errUnrecognised, err), "plex.tv sent an answer Mediarium did not understand. Try again in a moment.")
	}
	return nil
}

// CreatePIN asks plex.tv for a new PIN.
func (p *PlexTV) CreatePIN(ctx context.Context) (PlexPIN, error) {
	var pin PlexPIN
	if err := p.call(ctx, http.MethodPost, "/api/v2/pins", url.Values{"strong": {"true"}}, "", &pin); err != nil {
		return PlexPIN{}, err
	}
	if pin.ID == 0 || pin.Code == "" {
		return PlexPIN{}, userErr(errUnrecognised, "plex.tv sent an answer Mediarium did not understand. Try again in a moment.")
	}
	return pin, nil
}

// CheckPIN reads a PIN back; AuthToken is set once it was approved. A PIN
// plex.tv no longer knows is ErrPlexPINExpired.
func (p *PlexTV) CheckPIN(ctx context.Context, id int64) (PlexPIN, error) {
	var pin PlexPIN
	err := p.call(ctx, http.MethodGet, "/api/v2/pins/"+strconv.FormatInt(id, 10), nil, "", &pin)
	if errors.Is(err, errNotFound) {
		return PlexPIN{}, ErrPlexPINExpired
	}
	if err != nil {
		return PlexPIN{}, err
	}
	return pin, nil
}

// Servers lists the Plex servers the account can use, owned or shared with it.
func (p *PlexTV) Servers(ctx context.Context, accountToken string) ([]PlexResource, error) {
	var all []PlexResource
	q := url.Values{"includeHttps": {"1"}, "includeRelay": {"0"}}
	if err := p.call(ctx, http.MethodGet, "/api/v2/resources", q, accountToken, &all); err != nil {
		if errors.Is(err, errNotFound) {
			return nil, userErr(err, "plex.tv did not list your servers. Try again in a moment.")
		}
		return nil, err
	}
	out := []PlexResource{}
	for _, r := range all {
		if r.ClientIdentifier == "" || !provides(r.Provides, "server") {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func provides(list, what string) bool {
	for _, p := range strings.Split(list, ",") {
		if strings.EqualFold(strings.TrimSpace(p), what) {
			return true
		}
	}
	return false
}

// PlexAuthURL is the app.plex.tv page where the person approves a PIN.
func PlexAuthURL(clientID, code string) string {
	return "https://app.plex.tv/auth#?clientID=" + url.QueryEscape(clientID) +
		"&code=" + url.QueryEscape(code) +
		"&context%5Bdevice%5D%5Bproduct%5D=Mediarium"
}

// Candidates lists the addresses to try for this server, best first:
// preferred (when it is one of the server's addresses), then the local ones
// (the plex.direct https address, then plain http on the same IP, which
// works where DNS rebinding protection blocks plex.direct), then the remote
// ones. Relays are never used.
func (r PlexResource) Candidates(preferred string) []string {
	preferred = strings.TrimRight(strings.TrimSpace(preferred), "/")
	var out []string
	for _, a := range r.Addresses() {
		if a.URI == preferred {
			out = append([]string{a.URI}, out...)
		} else {
			out = append(out, a.URI)
		}
	}
	return out
}

// PlexAddress is one address to reach a Plex server at.
type PlexAddress struct {
	URI   string `json:"uri"`
	Local bool   `json:"local"`
}

// Addresses lists the server's addresses in the order Candidates tries
// them without a preference: local ones first, relays left out.
func (r PlexResource) Addresses() []PlexAddress {
	var local, remote []PlexAddress
	seen := map[string]bool{}
	add := func(list *[]PlexAddress, u string, isLocal bool) {
		u = strings.TrimRight(strings.TrimSpace(u), "/")
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		*list = append(*list, PlexAddress{URI: u, Local: isLocal})
	}
	for _, c := range r.Connections {
		if c.Relay {
			continue
		}
		list := &remote
		if c.Local {
			list = &local
		}
		add(list, c.URI, c.Local)
		if c.Local && !r.HTTPSRequired && c.Port > 0 && net.ParseIP(c.Address) != nil {
			add(list, "http://"+net.JoinHostPort(c.Address, strconv.Itoa(c.Port)), true)
		}
	}
	return append(local, remote...)
}

// ConnectPlex tries a server's addresses in Candidates order with its access
// token and returns the first one that works, as a server ready to save.
func (c *Client) ConnectPlex(ctx context.Context, r PlexResource, preferred string) (Server, TestResult, error) {
	if r.AccessToken == "" {
		return Server{}, TestResult{}, userErr(nil, "plex.tv did not give Mediarium access to %q. Only servers you own or that are shared with you can be added.", r.Name)
	}
	cands := r.Candidates(preferred)
	if len(cands) == 0 {
		return Server{}, TestResult{}, userErr(nil, "plex.tv lists no address for %q. Make sure the server is running and signed in to your Plex account.", r.Name)
	}
	var tried []string
	var lastErr error
	for _, u := range cands {
		if ctx.Err() != nil {
			break
		}
		s := Server{
			Name: r.Name, Kind: KindPlex, BaseURL: u, Token: r.AccessToken,
			Enabled: true, RefreshAfterImport: true, ServerID: r.ClientIdentifier, PathMap: []PathMapping{},
		}
		tctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		res, err := c.Test(tctx, s)
		cancel()
		if err == nil {
			if s.Name == "" {
				s.Name = res.ServerName
			}
			if res.ServerID != "" {
				s.ServerID = res.ServerID
			}
			return s, res, nil
		}
		tried, lastErr = append(tried, u), err
	}
	return Server{}, TestResult{}, userErr(lastErr, "Could not connect to %q at any of its addresses (%s). Check that Mediarium can reach it on your network, or add it by address instead.", r.Name, strings.Join(tried, ", "))
}

// HasAddress reports whether u is one of the addresses Candidates lists.
func (r PlexResource) HasAddress(u string) bool {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	for _, c := range r.Candidates("") {
		if c == u {
			return true
		}
	}
	return false
}
