package migrate

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

// Conn is how to reach one of the other apps: its address as the browser
// would open it (with any URL base, e.g. http://nas:7878 or
// http://nas:8080/sabnzbd) and its API key.
type Conn struct {
	URL    string `json:"url"`
	APIKey string `json:"apiKey"`
}

// LoginConn is how to reach an app that uses a username and password
// instead of an API key (NZBGet).
type LoginConn struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// JackettConn is Jackett's address and API key, plus the Jackett indexer
// ids to add as sites directly (from Mediarium's own site list) instead of
// through Jackett.
type JackettConn struct {
	Conn
	Direct []string `json:"direct,omitempty"`
}

// HydraConn is NZBHydra2's address and API key; Torznab also adds its
// torrent endpoint (it doesn't say whether it has torrent indexers).
type HydraConn struct {
	Conn
	Torznab bool `json:"torznab,omitempty"`
}

// Sources are the apps to read from; nil ones are left out.
type Sources struct {
	Radarr    *Conn        `json:"radarr,omitempty"`
	Sonarr    *Conn        `json:"sonarr,omitempty"`
	Prowlarr  *Conn        `json:"prowlarr,omitempty"`
	SABnzbd   *Conn        `json:"sabnzbd,omitempty"`
	NZBGet    *LoginConn   `json:"nzbget,omitempty"`
	Jackett   *JackettConn `json:"jackett,omitempty"`
	NZBHydra  *HydraConn   `json:"nzbhydra,omitempty"`
	Overseerr *Conn        `json:"overseerr,omitempty"`
	Ombi      *Conn        `json:"ombi,omitempty"`
	Bazarr    *Conn        `json:"bazarr,omitempty"`
	Medusa    *Conn        `json:"medusa,omitempty"`
	SickChill *Conn        `json:"sickchill,omitempty"`
}

// Any reports whether at least one app is given.
func (s Sources) Any() bool {
	return s.Radarr != nil || s.Sonarr != nil || s.Prowlarr != nil || s.SABnzbd != nil || s.NZBGet != nil ||
		s.Jackett != nil || s.NZBHydra != nil || s.Overseerr != nil || s.Ombi != nil || s.Bazarr != nil ||
		s.Medusa != nil || s.SickChill != nil
}

// maxResponseBytes bounds one response: a Radarr library of several
// thousand movies with all their metadata is tens of megabytes.
const maxResponseBytes = 256 << 20

const requestTimeout = 2 * time.Minute

// newHTTPClient refuses redirects to another host, so an API key sent as a
// header never follows a redirect somewhere else.
//
// Connecting is limited to a few seconds so a wrong address fails fast; the
// overall limit stays long because a big library's list takes a while.
func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = netguard.Dialer(5 * time.Second).DialContext
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ResponseHeaderTimeout = 60 * time.Second
	return &http.Client{
		Timeout:   requestTimeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("the address redirects to %s; enter that address instead", req.URL.Host)
			}
			return nil
		},
	}
}

// baseURL checks and normalises a Conn's address.
func (c Conn) baseURL() (*url.URL, error) {
	raw := strings.TrimSpace(c.URL)
	if raw == "" {
		return nil, errors.New("enter the address, for example http://192.168.1.10:7878")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("%q is not a web address (use http://host:port)", c.URL)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery, u.Fragment, u.User = "", "", nil
	return u, nil
}

// call is one read request to another app.
type call struct {
	// path is appended to the app's address. "{key}" in it is replaced by
	// the API key (SickChill carries the key in the path); error messages
	// show the path without it.
	path  string
	query url.Values
	// keyHeader and keyQuery name the header or query parameter that
	// carries the API key.
	keyHeader string
	keyQuery  string
	// xml: the answer is XML (Newznab/Torznab), not JSON.
	xml bool
}

// get reads from Radarr, Sonarr, Prowlarr or SABnzbd: the API key goes in
// the X-Api-Key header, or in the query string for SABnzbd (keyInQuery),
// which has no header option.
func get(ctx context.Context, hc *http.Client, conn Conn, path string, query url.Values, keyInQuery bool, out any) error {
	c := call{path: path, query: query, keyHeader: "X-Api-Key"}
	if keyInQuery {
		c = call{path: path, query: query, keyQuery: "apikey"}
	}
	return read(ctx, hc, conn, c, out)
}

// read and rpc are the only functions that send anything to the other
// apps. read only ever sends GET; rpc sends NZBGet's JSON-RPC POST, but only
// for the read-only methods in nzbgetReadOnly. The importer must never
// change those apps. Errors never contain the key or the full request URL.
func read(ctx context.Context, hc *http.Client, conn Conn, c call, out any) error {
	base, err := conn.baseURL()
	if err != nil {
		return err
	}
	key := strings.TrimSpace(conn.APIKey)
	if key == "" {
		return errors.New("enter the API key")
	}
	u := *base
	shown := strings.ReplaceAll(c.path, "{key}", "...")
	u.Path = base.Path + strings.ReplaceAll(c.path, "{key}", key)
	q := url.Values{}
	for k, v := range c.query {
		q[k] = v
	}
	if c.keyQuery != "" {
		q.Set(c.keyQuery, key)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("build request for %s: %w", base.Host, redact(err))
	}
	if c.xml {
		req.Header.Set("Accept", "application/xml, text/xml")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	if c.keyHeader != "" {
		req.Header.Set(c.keyHeader, key)
	}
	return send(hc, req, base.Host, shown, c.xml, "the API key was not accepted", out)
}

// send runs a request built by read or rpc and decodes the answer.
func send(hc *http.Client, req *http.Request, host, shown string, isXML bool, authMsg string, out any) error {
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach %s: %w", host, redact(err))
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return errors.New(authMsg)
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%s answered 404 for %s: check the address (and its URL base, if you set one)", host, shown)
	case resp.StatusCode >= 520 && resp.StatusCode <= 530, resp.StatusCode >= 500 && resp.Header.Get("Cf-Ray") != "":
		// Cloudflare 52x: the address goes through Cloudflare (a tunnel, usually) and it cannot reach the app.
		return fmt.Errorf("%s is behind Cloudflare, which answered %d for %s because it can't reach the app behind it. Use the app's address on your own network instead, for example http://192.168.1.20:8989", host, resp.StatusCode, shown)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("%s answered %s for %s", host, resp.Status, shown)
	}
	body := io.LimitReader(resp.Body, maxResponseBytes)
	if isXML {
		return decodeXML(body, host, shown, authMsg, out)
	}
	if err := json.NewDecoder(body).Decode(out); err != nil {
		return fmt.Errorf("%s did not answer with the expected data for %s (is this the right app?): %w", host, shown, err)
	}
	return nil
}

// newznabError is the error answer of Newznab and Torznab APIs.
type newznabError struct {
	XMLName     xml.Name `xml:"error"`
	Code        int      `xml:"code,attr"`
	Description string   `xml:"description,attr"`
}

// decodeXML decodes a Newznab/Torznab answer, turning an <error> answer
// into a readable error (codes 100-102 are a wrong or missing API key).
func decodeXML(body io.Reader, host, shown, authMsg string, out any) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("could not read the reply from %s for %s: %w", host, shown, redact(err))
	}
	var apiErr newznabError
	if xml.Unmarshal(data, &apiErr) == nil {
		if apiErr.Code >= 100 && apiErr.Code <= 102 {
			return errors.New(authMsg)
		}
		return fmt.Errorf("%s answered with an error for %s: %s", host, shown, apiErr.Description)
	}
	if err := xml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s did not answer with the expected data for %s (is this the right app?): %w", host, shown, err)
	}
	return nil
}

// nzbgetReadOnly are the NZBGet JSON-RPC methods the importer may call:
// they only read. Anything else (editqueue, append, saveconfig, shutdown,
// ...) is refused before a request is made.
var nzbgetReadOnly = map[string]bool{"version": true, "config": true, "status": true}

// rpc calls one read-only NZBGet JSON-RPC method (JSON-RPC is POST by
// protocol) with basic authentication, and decodes its result.
func rpc(ctx context.Context, hc *http.Client, conn LoginConn, method string, out any) error {
	if !nzbgetReadOnly[method] {
		return fmt.Errorf("refusing to call NZBGet method %q: the importer only reads", method)
	}
	base, err := Conn{URL: conn.URL}.baseURL()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"version": "1.1", "method": method, "params": []any{}, "id": 1})
	if err != nil {
		return fmt.Errorf("encode NZBGet request: %w", err)
	}
	u := *base
	u.Path = base.Path + "/jsonrpc"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request for %s: %w", base.Host, redact(err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if conn.Username != "" || conn.Password != "" {
		req.SetBasicAuth(conn.Username, conn.Password)
	}
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := send(hc, req, base.Host, "/jsonrpc "+method, false, "the username or password was not accepted", &env); err != nil {
		return err
	}
	if env.Error != nil {
		return fmt.Errorf("NZBGet refused %s: %s", method, env.Error.Message)
	}
	if len(env.Result) == 0 {
		return fmt.Errorf("%s did not answer with the expected data for %s (is this NZBGet?)", base.Host, method)
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return fmt.Errorf("%s did not answer with the expected data for %s (is this NZBGet?): %w", base.Host, method, err)
	}
	return nil
}

// redact strips the request URL (which may carry an API key in its query)
// from a transport error, keeping the underlying cause.
func redact(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return errors.New("timed out")
	}
	return err
}
