package indexers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html/charset"
)

// CardigannManager runs definition-based ("Cardigann") indexers: sites
// without a Newznab/Torznab API, driven by a community definition that says
// how to log in, search, scrape the results and fetch a download.
//
// It keeps one session per configured indexer (cookie jar, login state,
// rate limit, Cloudflare clearance), so logging in happens once, not on
// every search.
type CardigannManager struct {
	Store *DefinitionStore
	// FlareSolverrURL returns the FlareSolverr address from Settings, or ""
	// when none is set up.
	FlareSolverrURL func() string
	// Transport is used for every site request; nil means
	// http.DefaultTransport. Injectable for tests.
	Transport http.RoundTripper
	// DefaultDelay is the polite minimum gap between two requests to the
	// same site, used when the definition's own requestDelay is smaller.
	DefaultDelay time.Duration
	// UserAgent is sent to sites unless FlareSolverr supplied its own.
	UserAgent string

	mu       sync.Mutex
	sessions map[int64]*cgSession
}

// DefaultPoliteDelay is the minimum gap between requests to one site.
const DefaultPoliteDelay = time.Second

// cardigannTimeout bounds one whole search (login, several paths, polite
// delays) against a definition-based site.
const cardigannTimeout = 60 * time.Second

const defaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"

// ErrCloudflare is returned when a site shows a Cloudflare browser check
// and no FlareSolverr is configured to pass it.
var ErrCloudflare = errors.New("This site is behind a Cloudflare check. Set up FlareSolverr (see Settings > Indexers) to use it.")

// NewCardigannManager returns a manager reading definitions from store.
func NewCardigannManager(store *DefinitionStore, flareSolverrURL func() string) *CardigannManager {
	return &CardigannManager{
		Store:           store,
		FlareSolverrURL: flareSolverrURL,
		DefaultDelay:    DefaultPoliteDelay,
		UserAgent:       defaultUserAgent,
		sessions:        map[int64]*cgSession{},
	}
}

// Forget drops an indexer's session (cookies, login), e.g. after its
// settings changed or it was deleted.
func (m *CardigannManager) Forget(id int64) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

// Search runs a keyword search on a definition-based indexer.
func (m *CardigannManager) Search(ctx context.Context, inst Instance, query string, categories []int) ([]Result, error) {
	sess, err := m.session(ctx, inst)
	if err != nil {
		return nil, err
	}
	return sess.search(ctx, query, categories)
}

// Test signs in if needed (running the definition's login test) and runs a
// search without keywords, falling back to the keyword "test". It returns
// how many releases came back.
func (m *CardigannManager) Test(ctx context.Context, inst Instance) (int, error) {
	sess, err := m.session(ctx, inst)
	if err != nil {
		return 0, err
	}
	// A signed-in session is checked by the search itself (a lost session
	// signs in again); changed settings always start a new session.
	cats := []int{2000, 5000}
	results, err := sess.search(ctx, "", cats)
	if err != nil {
		return 0, err
	}
	if len(results) == 0 {
		results, err = sess.search(ctx, "test", cats)
		if err != nil {
			return 0, err
		}
	}
	return len(results), nil
}

// Download resolves a result link (from EncodeLinkRef) into a magnet link
// or the bytes of a .torrent/.nzb file, logging in and following the
// definition's download rules as needed.
func (m *CardigannManager) Download(ctx context.Context, inst Instance, link string) (*Download, error) {
	sess, err := m.session(ctx, inst)
	if err != nil {
		return nil, err
	}
	return sess.download(ctx, link)
}

// Download is a resolved release: either a magnet link or file contents.
type Download struct {
	Magnet string
	Data   []byte
}

func (m *CardigannManager) session(ctx context.Context, inst Instance) (*cgSession, error) {
	if m.Store == nil {
		return nil, fmt.Errorf("indexer definitions are not available")
	}
	key := sessionKey(m.Store.Generation(), inst)
	m.mu.Lock()
	s, ok := m.sessions[inst.ID]
	m.mu.Unlock()
	if ok && s.key == key && inst.ID != 0 {
		return s, nil
	}

	def, err := m.Store.Load(ctx, inst.DefinitionID)
	if err != nil {
		return nil, err
	}
	key = sessionKey(m.Store.Generation(), inst) // Load may have downloaded the catalogue
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions == nil {
		m.sessions = map[int64]*cgSession{}
	}
	if s, ok := m.sessions[inst.ID]; ok && s.key == key && inst.ID != 0 {
		return s, nil
	}
	s, err = newSession(m, def, inst)
	if err != nil {
		return nil, err
	}
	s.key = key
	if inst.ID != 0 {
		m.sessions[inst.ID] = s
	}
	return s, nil
}

func sessionKey(gen string, inst Instance) string {
	keys := make([]string, 0, len(inst.Settings))
	for k := range inst.Settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00", gen, inst.DefinitionID, inst.BaseURL)
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%s\x00", k, inst.Settings[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// cgSession is one configured indexer's live state.
type cgSession struct {
	m        *CardigannManager
	id       int64 // the configured indexer's id, for result link references
	key      string
	def      *Definition
	name     string
	base     *url.URL
	settings map[string]string
	jar      http.CookieJar
	client   *http.Client

	decode func([]byte) ([]byte, error)
	encode func(string) string

	mu          sync.Mutex // one flow (login/search/download) at a time
	lastRequest time.Time
	loggedIn    bool
	userAgent   string // set by FlareSolverr
}

func newSession(m *CardigannManager, def *Definition, inst Instance) (*cgSession, error) {
	baseStr := strings.TrimSpace(inst.BaseURL)
	if baseStr == "" && len(def.Links) > 0 {
		baseStr = def.Links[0]
	}
	base, err := url.Parse(baseStr)
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, fmt.Errorf("%s: invalid site address %q", inst.Name, baseStr)
	}
	if !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}
	s := &cgSession{
		m: m, id: inst.ID, def: def, name: inst.Name, base: base, jar: jar,
		settings: map[string]string{},
		decode:   func(b []byte) ([]byte, error) { return b, nil },
		encode:   func(v string) string { return v },
	}
	if s.name == "" {
		s.name = def.Name
	}
	for k, v := range inst.Settings {
		s.settings[k] = v
	}
	if dec, enc, err := lookupEncoding(def.Encoding); err != nil {
		return nil, err
	} else if dec != nil {
		s.decode, s.encode = dec, enc
	}
	transport := m.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	s.client = &http.Client{Transport: transport, Jar: jar, Timeout: 30 * time.Second}
	return s, nil
}

// lookupEncoding returns converters for a definition's page encoding, or
// nils for UTF-8.
func lookupEncoding(name string) (decode func([]byte) ([]byte, error), encode func(string) string, err error) {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" || n == "utf-8" || n == "utf8" {
		return nil, nil, nil
	}
	enc, _ := charset.Lookup(n)
	if enc == nil {
		return nil, nil, fmt.Errorf("page encoding %q is not supported", name)
	}
	decode = func(b []byte) ([]byte, error) { return enc.NewDecoder().Bytes(b) }
	encode = func(v string) string {
		out, err := enc.NewEncoder().String(v)
		if err != nil {
			return v
		}
		return out
	}
	return decode, encode, nil
}

// vars returns the template variables every request sees.
func (s *cgSession) vars() *tmplVars {
	v := newTmplVars()
	v.Config["sitelink"] = s.base.String()
	for _, st := range s.def.Settings {
		val, ok := s.settings[st.Name]
		if !ok {
			val = st.Default
		}
		switch st.Type {
		case "checkbox":
			if parseBoolSetting(val) {
				val = checkboxTrue
			} else {
				val = ""
			}
		case "info", "info_cookie", "info_flaresolverr", "info_useragent", "info_category_8000":
			continue
		}
		v.Config[st.Name] = val
	}
	return v
}

// --- HTTP ---

type cgRequest struct {
	method   string
	url      string
	form     string // encoded body for POST
	headers  map[string]string
	noFollow bool // return redirects instead of following them
	binary   bool // expect a file, not a page
}

type cgResponse struct {
	status   int
	url      *url.URL
	header   http.Header
	raw      []byte // bytes as received
	body     []byte // decoded to UTF-8
	location string // redirect target when noFollow
}

const maxResponseBytes = 16 << 20

// errMagnetRedirect carries a redirect to a magnet link, which net/http
// can't follow.
type errMagnetRedirect struct{ uri string }

func (e errMagnetRedirect) Error() string { return "redirect to magnet link" }

func (s *cgSession) wait(ctx context.Context) error {
	gap := time.Duration(s.def.RequestDelay * float64(time.Second))
	if gap < s.m.DefaultDelay {
		gap = s.m.DefaultDelay
	}
	if !s.lastRequest.IsZero() && gap > 0 {
		if d := time.Until(s.lastRequest.Add(gap)); d > 0 {
			t := time.NewTimer(d)
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
			}
		}
	}
	s.lastRequest = time.Now()
	return nil
}

// sameSite reports whether u belongs to the configured site, so the
// definition's headers (which may carry API keys) are only sent there.
func (s *cgSession) sameSite(u *url.URL) bool {
	return strings.EqualFold(u.Hostname(), s.base.Hostname())
}

func (s *cgSession) do(ctx context.Context, r cgRequest) (*cgResponse, error) {
	resp, err := s.doOnce(ctx, r)
	if err != nil {
		return nil, err
	}
	if !isCloudflareChallenge(resp) {
		return resp, nil
	}
	fsURL := ""
	if s.m.FlareSolverrURL != nil {
		fsURL = strings.TrimSpace(s.m.FlareSolverrURL())
	}
	if fsURL == "" {
		return nil, fmt.Errorf("%s: %w", s.name, ErrCloudflare)
	}
	sol, err := s.solveChallenge(ctx, fsURL, r)
	if err != nil {
		return nil, fmt.Errorf("%s: FlareSolverr could not pass the Cloudflare check: %w", s.name, err)
	}
	if !r.binary && sol.Response != "" && sol.Status != 0 {
		u, _ := url.Parse(sol.URL)
		if u == nil {
			u, _ = url.Parse(r.url)
		}
		body := []byte(sol.Response)
		return &cgResponse{status: sol.Status, url: u, header: http.Header{}, raw: body, body: body}, nil
	}
	// files can't come through FlareSolverr: retry directly with the
	// clearance cookies and user agent it obtained
	resp, err = s.doOnce(ctx, r)
	if err != nil {
		return nil, err
	}
	if isCloudflareChallenge(resp) {
		return nil, fmt.Errorf("%s: %w", s.name, ErrCloudflare)
	}
	return resp, nil
}

func (s *cgSession) doOnce(ctx context.Context, r cgRequest) (*cgResponse, error) {
	if err := s.wait(ctx); err != nil {
		return nil, err
	}
	method := strings.ToUpper(r.method)
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader(r.form)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.url, body)
	if err != nil {
		return nil, fmt.Errorf("%s: build request: %w", s.name, err)
	}
	ua := s.m.UserAgent
	if ua == "" {
		ua = defaultUserAgent
	}
	if s.userAgent != "" {
		ua = s.userAgent
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,application/json;q=0.8,*/*;q=0.7")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if s.sameSite(req.URL) {
		for k, v := range r.headers {
			req.Header.Set(k, v)
		}
	}

	client := *s.client
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if next.URL.Scheme == "magnet" {
			return errMagnetRedirect{uri: next.URL.String()}
		}
		if r.noFollow {
			return http.ErrUseLastResponse
		}
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		// keep the definition's headers on the same site only
		if !strings.EqualFold(next.URL.Hostname(), s.base.Hostname()) {
			for k := range r.headers {
				next.Header.Del(k)
			}
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		var mr errMagnetRedirect
		if errors.As(err, &mr) {
			return &cgResponse{status: http.StatusFound, url: req.URL, header: http.Header{}, location: mr.uri}, nil
		}
		// url.Error repeats the full URL, which may hold a passkey
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("%s: request failed: %w", s.name, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("%s: read response: %w", s.name, err)
	}
	out := &cgResponse{status: resp.StatusCode, url: resp.Request.URL, header: resp.Header, raw: raw, body: raw}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if loc := resp.Header.Get("Location"); loc != "" {
			if u, err := resp.Request.URL.Parse(loc); err == nil {
				out.location = u.String()
			} else {
				out.location = loc
			}
		}
	}
	if !r.binary {
		if dec, err := s.decode(raw); err == nil {
			out.body = dec
		}
	}
	return out, nil
}

// isCloudflareChallenge recognises the pages Cloudflare (and DDoS-Guard)
// show instead of the site while checking for a real browser.
func isCloudflareChallenge(r *cgResponse) bool {
	if r.status != http.StatusForbidden && r.status != http.StatusServiceUnavailable && r.status != http.StatusTooManyRequests {
		return false
	}
	server := strings.ToLower(r.header.Get("Server"))
	body := string(r.body)
	if len(body) > 64<<10 {
		body = body[:64<<10]
	}
	markers := []string{
		"<title>Just a moment...</title>", "cf-browser-verification", "cf_chl_opt", "challenge-platform",
		"_cf_chl", "cf-challenge-running", "<title>Attention Required! | Cloudflare</title>",
		"<title>DDoS-Guard</title>", "Checking your browser before accessing", "ddos-guard",
	}
	for _, m := range markers {
		if strings.Contains(body, m) {
			return true
		}
	}
	return strings.Contains(server, "cloudflare") && strings.EqualFold(r.header.Get("cf-mitigated"), "challenge")
}

// --- FlareSolverr ---

type fsSolution struct {
	URL       string            `json:"url"`
	Status    int               `json:"status"`
	Response  string            `json:"response"`
	UserAgent string            `json:"userAgent"`
	Cookies   []fsCookie        `json:"cookies"`
	Headers   map[string]string `json:"headers"`
}

type fsCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
}

const flareSolverrMaxTimeout = 60000 // ms

// solveChallenge asks FlareSolverr to load the page in a real browser and
// keeps the clearance cookies and user agent for later requests.
func (s *cgSession) solveChallenge(ctx context.Context, fsURL string, r cgRequest) (*fsSolution, error) {
	u, err := url.Parse(r.url)
	if err != nil {
		return nil, fmt.Errorf("bad url: %w", err)
	}
	payload := map[string]any{
		"cmd":        "request.get",
		"url":        r.url,
		"maxTimeout": flareSolverrMaxTimeout,
	}
	if strings.EqualFold(r.method, http.MethodPost) {
		payload["cmd"] = "request.post"
		payload["postData"] = r.form
	}
	var cookies []map[string]string
	for _, c := range s.jar.Cookies(u) {
		cookies = append(cookies, map[string]string{"name": c.Name, "value": c.Value})
	}
	if len(cookies) > 0 {
		payload["cookies"] = cookies
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	endpoint := strings.TrimRight(fsURL, "/") + "/v1"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: time.Duration(flareSolverrMaxTimeout)*time.Millisecond + 15*time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contact FlareSolverr: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		Status   string      `json:"status"`
		Message  string      `json:"message"`
		Solution *fsSolution `json:"solution"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&out); err != nil {
		return nil, fmt.Errorf("read FlareSolverr reply (status %d): %w", resp.StatusCode, err)
	}
	if out.Status != "ok" || out.Solution == nil {
		msg := out.Message
		if msg == "" {
			msg = fmt.Sprintf("status %q", out.Status)
		}
		return nil, errors.New(msg)
	}
	sol := out.Solution
	var jarCookies []*http.Cookie
	for _, c := range sol.Cookies {
		hc := &http.Cookie{Name: c.Name, Value: c.Value, Path: c.Path, Domain: strings.TrimPrefix(c.Domain, "."), HttpOnly: c.HTTPOnly, Secure: c.Secure}
		if c.Expires > 0 {
			hc.Expires = time.Unix(int64(c.Expires), 0)
		}
		if hc.Path == "" {
			hc.Path = "/"
		}
		jarCookies = append(jarCookies, hc)
	}
	if len(jarCookies) > 0 {
		s.jar.SetCookies(&url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}, jarCookies)
	}
	if sol.UserAgent != "" {
		s.userAgent = sol.UserAgent
	}
	log.Printf("cardigann: indexer=%q passed a Cloudflare check via FlareSolverr cookies=%d", s.name, len(jarCookies))
	return sol, nil
}

// --- URLs ---

// resolve turns a (possibly relative) link from a page into an absolute
// URL against base (the page it came from, or the site root).
func (s *cgSession) resolve(ref string, base *url.URL) (string, error) {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "magnet:") {
		return ref, nil
	}
	if base == nil {
		base = s.base
	}
	u, err := base.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("bad link %q: %w", ref, err)
	}
	return u.String(), nil
}

// resolvePath resolves a definition path (relative to the site root).
func (s *cgSession) resolvePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p, nil
	}
	return s.resolve(p, s.base)
}

// templatedHeaders renders a definition header block.
func templatedHeaders(h map[string][]string, vars *tmplVars) (map[string]string, error) {
	out := map[string]string{}
	for k, vals := range h {
		if len(vals) == 0 {
			continue
		}
		v, err := applyTemplate(vals[0], vars, nil)
		if err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}

// encodeForm url-encodes an ordered input list (plus an optional raw
// suffix) in the site's encoding.
func (s *cgSession) encodeForm(keys []string, vals map[string]string, raw string) string {
	var sb strings.Builder
	for _, k := range keys {
		if sb.Len() > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(url.QueryEscape(s.encode(k)))
		sb.WriteByte('=')
		sb.WriteString(url.QueryEscape(s.encode(vals[k])))
	}
	if raw = strings.Trim(raw, "&"); raw != "" {
		if sb.Len() > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(raw)
	}
	return sb.String()
}

func appendQuery(u, q string) string {
	if q == "" {
		return u
	}
	if strings.Contains(u, "?") {
		if strings.HasSuffix(u, "?") || strings.HasSuffix(u, "&") {
			return u + q
		}
		return u + "&" + q
	}
	return u + "?" + q
}
