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
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
	"github.com/rdborg/mediarium/internal/netproxy"
	"golang.org/x/net/html/charset"
	"golang.org/x/net/publicsuffix"
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

// cardigannFlareSolverrTimeout is the same limit for a site that may need
// FlareSolverr: passing a Cloudflare check alone can take a minute.
const cardigannFlareSolverrTimeout = 150 * time.Second

// SearchBudget is how long one whole search of a site may take.
func (m *CardigannManager) SearchBudget() time.Duration {
	if m != nil && m.FlareSolverrURL != nil && strings.TrimSpace(m.FlareSolverrURL()) != "" {
		return cardigannFlareSolverrTimeout
	}
	return cardigannTimeout
}

const defaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"

// ErrCloudflare is returned when a site shows a Cloudflare browser check
// and no FlareSolverr is configured to pass it.
var ErrCloudflare = errors.New("This site is behind a Cloudflare check. Set up FlareSolverr (see Settings > Finding releases > Indexers & Search) to use it.")

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

// TestResult is what a test found: how many releases came back, and whether
// FlareSolverr had to pass a Cloudflare check on the way.
type TestResult struct {
	Releases        int
	ViaFlareSolverr bool
}

// Test signs in if needed (running the definition's login test) and runs a
// search without keywords, falling back to the keyword "test".
func (m *CardigannManager) Test(ctx context.Context, inst Instance) (TestResult, error) {
	sess, err := m.session(ctx, inst)
	if err != nil {
		return TestResult{}, err
	}
	// A signed-in session is checked by the search itself (a lost session
	// signs in again); changed settings always start a new session.
	cats := []int{2000, 5000}
	results, err := sess.search(ctx, "", cats)
	if err != nil {
		return TestResult{}, err
	}
	if len(results) == 0 {
		results, err = sess.search(ctx, "test", cats)
		if err != nil {
			return TestResult{}, err
		}
	}
	via := sess.viaFlareSolverr.Load()
	return TestResult{Releases: len(results), ViaFlareSolverr: via}, nil
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
		return nil, fmt.Errorf("the site list is not available")
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

	hostMu      sync.Mutex
	hostVerdict map[string]hostVerdict // see checkForeignHost

	decode func([]byte) ([]byte, error)
	encode func(string) string

	// flow lets one flow (login/search/download) run at a time. It is a
	// channel rather than a mutex so a search waiting its turn gives up when
	// its own time runs out, instead of queueing behind a stuck one for ever.
	flow            chan struct{}
	lastRequest     time.Time
	loggedIn        bool
	userAgent       string      // set by FlareSolverr
	viaFlareSolverr atomic.Bool // a Cloudflare check was passed by FlareSolverr
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
		flow:     make(chan struct{}, 1),
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
		transport = netproxy.Transport(netproxy.Indexers)
	}
	s.client = &http.Client{Transport: &cgTransport{s: s, site: transport, injected: m.Transport != nil}, Jar: jar, Timeout: 30 * time.Second}
	return s, nil
}

// lookupAddrs resolves a host name for the checks on addresses that are not
// the site's own. Tests replace it.
var lookupAddrs = func(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// cgTransport sends a site's requests. A request to the site itself (or a
// host of its registered domain) goes the normal way. A request to any other
// host is only made when the host is a public address: the name is resolved
// and refused if any address it gives is on this machine or the home network,
// and a direct connection is checked again when it is made, so a name that
// answers differently the second time (DNS rebinding) cannot get through.
type cgTransport struct {
	s        *cgSession
	site     http.RoundTripper
	injected bool // the manager was given a transport (tests): use it for every host

	once    sync.Once
	foreign *http.Transport
}

func (t *cgTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s := t.s
	host := req.URL.Hostname()
	if s.siteRelated(host) || localHost(s.base.Hostname()) {
		return t.site.RoundTrip(req)
	}
	if err := s.checkForeignHost(req.Context(), host); err != nil {
		return nil, err
	}
	if t.injected || netproxy.Routed(netproxy.Indexers) {
		// a proxy or the VPN makes the connection: it can only be checked here
		return t.site.RoundTrip(req)
	}
	t.once.Do(func() { t.foreign = netguard.PublicTransport() })
	return t.foreign.RoundTrip(req)
}

func (t *cgTransport) CloseIdleConnections() {
	if c, ok := t.site.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
	t.once.Do(func() { t.foreign = netguard.PublicTransport() })
	t.foreign.CloseIdleConnections()
}

// checkForeignHost refuses a host that is not the site's own when it is, or
// resolves to, an address on this machine or the home network. A name that
// cannot be looked up here is left to the connection, which is checked as it
// is made (or to the proxy).
func (s *cgSession) checkForeignHost(ctx context.Context, host string) error {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	refused := fmt.Errorf("%s: the site wants to connect to %s, which is on your own network, so Mediarium refused", s.name, host)
	if host == "" || localHost(host) {
		return refused
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return nil // an address that is public (localHost judged it above)
	}
	// A result list can link many files on one host: the answer is kept briefly.
	s.hostMu.Lock()
	v, ok := s.hostVerdict[host]
	s.hostMu.Unlock()
	if ok && time.Now().Before(v.until) {
		if v.refuse {
			return refused
		}
		return nil
	}
	lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := lookupAddrs(lctx, host)
	if err != nil {
		return nil // not kept: a lookup that failed is tried again
	}
	refuse := false
	for _, a := range addrs {
		if netguard.NotPublic(a) || netguard.Blocked(a) {
			refuse = true
		}
	}
	s.hostMu.Lock()
	if s.hostVerdict == nil || len(s.hostVerdict) > 200 {
		s.hostVerdict = map[string]hostVerdict{}
	}
	s.hostVerdict[host] = hostVerdict{refuse: refuse, until: time.Now().Add(time.Minute)}
	s.hostMu.Unlock()
	if refuse {
		return refused
	}
	return nil
}

type hostVerdict struct {
	refuse bool
	until  time.Time
}

// lock waits for the session's turn, giving up when ctx ends.
func (s *cgSession) lock(ctx context.Context) error {
	select {
	case s.flow <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: waited too long for the site's previous search to finish: %w", s.name, ctx.Err())
	}
}

func (s *cgSession) unlock() { <-s.flow }

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

// customUserAgent returns the User-Agent the person typed into the site's
// "useragent" setting, if the definition has one. Sites that tie the session
// cookie to the browser need every request, the sign-in check included, to
// say the same thing as that browser.
func (s *cgSession) customUserAgent() string {
	for _, st := range s.def.Settings {
		if st.Name != "useragent" || st.Type != "text" {
			continue
		}
		val, ok := s.settings[st.Name]
		if !ok {
			val = st.Default
		}
		// a header value can't hold line breaks or other control characters
		val = strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, val)
		return strings.TrimSpace(val)
	}
	return ""
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

// maxRequestDelaySeconds is the longest pause between two requests a
// definition can ask for.
const maxRequestDelaySeconds = 10

// errMagnetRedirect carries a redirect to a magnet link, which net/http
// can't follow.
type errMagnetRedirect struct{ uri string }

func (e errMagnetRedirect) Error() string { return "redirect to magnet link" }

func (s *cgSession) wait(ctx context.Context) error {
	gap := s.m.DefaultDelay
	if d := s.def.RequestDelay; d > 0 {
		// a definition asks for its own pause; not longer than a search may last
		if d > maxRequestDelaySeconds {
			d = maxRequestDelaySeconds
		}
		if own := time.Duration(d * float64(time.Second)); own > gap {
			gap = own
		}
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
		return nil, &FlareSolverrError{Site: s.name, Address: SafeAddress(fsURL), Err: err}
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
		return nil, fmt.Errorf("%s: build request: %w", s.name, netguard.CleanError(err))
	}
	ua := s.m.UserAgent
	if ua == "" {
		ua = defaultUserAgent
	}
	if custom := s.customUserAgent(); custom != "" {
		ua = custom
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
		if err := s.checkPageAddress(next.URL.String()); err != nil {
			return err
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
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%s: read response: %w", s.name, err)
	}
	if len(raw) > maxResponseBytes {
		// a page cut short reads as fewer results, or a file that is broken
		return nil, fmt.Errorf("%s: the site sent more than %d MB, so Mediarium stopped reading", s.name, maxResponseBytes>>20)
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
		return nil, fmt.Errorf("bad url: %w", netguard.CleanError(err))
	}
	// FlareSolverr's browser loads the page itself, out of reach of the
	// dial-time check, so the page is checked here first.
	if err := netguard.CheckHost(ctx, u.Host, true); err != nil {
		return nil, err
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
	client := netguard.Client(time.Duration(flareSolverrMaxTimeout)*time.Millisecond + 15*time.Second)
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		var nerr net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &nerr) && nerr.Timeout()) {
			return nil, fmt.Errorf("FlareSolverr didn't finish the Cloudflare check after %d seconds. Check that FlareSolverr is running and has internet access, then try again.", int(time.Since(started).Round(time.Second)/time.Second))
		}
		return nil, &flareContactError{err: err}
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
		if strings.Contains(strings.ToLower(msg), "timeout") {
			return nil, fmt.Errorf("FlareSolverr couldn't pass the Cloudflare check within %d seconds. Check that FlareSolverr is running and has internet access, then try again.", flareSolverrMaxTimeout/1000)
		}
		return nil, errors.New(msg)
	}
	sol := out.Solution
	// The browser follows redirects on its own: a page that ended on a
	// blocked address is thrown away.
	if end, err := url.Parse(sol.URL); err == nil && end.Host != "" {
		if err := netguard.CheckHost(ctx, end.Host, true); err != nil {
			return nil, fmt.Errorf("FlareSolverr was sent elsewhere: %w", err)
		}
	}
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
		s.viaFlareSolverr.Store(true)
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
		// the link can carry a passkey, and so can net/url's message about it
		return "", fmt.Errorf("bad link %q: %w", netguard.RedactURL(ref), netguard.CleanError(err))
	}
	return u.String(), nil
}

// resolvePath resolves a definition path (relative to the site root). A path
// that is a full address must lead to the site itself: the definitions come
// from a public repository anyone can send changes to, and these requests
// carry the person's login, cookie or passkey. An address on a host that has
// nothing to do with the site would hand those to a stranger. (A site that
// sends no credentials may use another public host: see checkSiteAddress.)
func (s *cgSession) resolvePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	out, err := s.resolve(p, s.base)
	if err != nil {
		return "", fmt.Errorf("%s: bad address in the site definition: %w", s.name, netguard.CleanError(err))
	}
	// Judge the address that results, not how it was written: "//host/x",
	// "HTTPS://host/x" and "http:host" all leave the site as well.
	if err := s.checkSiteAddress(out); err != nil {
		return "", err
	}
	if strings.HasPrefix(strings.ToLower(p), "http://") || strings.HasPrefix(strings.ToLower(p), "https://") {
		return p, nil
	}
	return out, nil
}

// checkSiteAddress refuses an address that is not a web address of the site
// itself (see resolvePath). A site that sends no login, cookie, key or
// passkey has nothing to hand over, so its definition may name another public
// host (a movie site whose search runs on a separate API host). The address
// still may not point at this machine or the home network, and the request
// goes through the same network checks as every other.
func (s *cgSession) checkSiteAddress(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: bad address in the site definition: %w", s.name, netguard.CleanError(err))
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s: the site definition wants to use an address that is not a web address, so Mediarium refused", s.name)
	}
	if s.siteRelated(u.Hostname()) {
		return nil
	}
	if !s.sendsCredentials() {
		if localHost(s.base.Hostname()) {
			return nil // a site the person runs on their own network
		}
		// The name is looked up here, not only read: a name that looks public
		// can point at the home network. The connection itself is checked
		// again when it is made (see cgTransport).
		if err := s.checkForeignHost(context.Background(), u.Hostname()); err != nil {
			return fmt.Errorf("%s: the site definition wants to connect to %s, which is on your own network, so Mediarium refused", s.name, u.Hostname())
		}
		return nil
	}
	return fmt.Errorf("%s: the site definition wants to connect to %s, which is not one of the site's own addresses, so Mediarium refused", s.name, u.Hostname())
}

// credentialNames are setting names that hold a sign-in, beyond the secret
// ones SecretSetting already knows.
var credentialNames = []string{"username", "user", "uid", "login", "email", "userid", "user_id"}

// sendsCredentials reports whether requests to this site can carry something
// the person would not want a stranger to have: the definition signs in, or a
// setting that holds a login, cookie, key or passkey is filled in, or a
// header the definition adds carries an authorisation or cookie.
func (s *cgSession) sendsCredentials() bool {
	if s.def == nil {
		return true // nothing known about the site: stay strict
	}
	if s.def.Login != nil {
		return true
	}
	for _, st := range s.def.Settings {
		v := strings.TrimSpace(s.settings[st.Name])
		if v == "" {
			continue
		}
		if SecretSetting(st.Type, st.Name) {
			return true
		}
		// A value that is just what the definition itself suggests (the API
		// address YTS and The Pirate Bay save with the form) is not something
		// the person typed in, so it cannot be a sign-in.
		if d := strings.TrimSpace(st.Default); d != "" && v == d && !credentialSettingName(st.Name) {
			continue
		}
		n := strings.ToLower(st.Name)
		if credentialSettingName(st.Name) {
			return true
		}
		// Any other filled-in text or password setting is taken for a credential
		// too (a session id, a hash, an "auth" value), apart from the few that
		// are plain preferences. Choices and check boxes hold nothing secret.
		if t := strings.ToLower(strings.TrimSpace(st.Type)); (t == "" || t == "text" || t == "password") && !plainSettings[n] {
			return true
		}
	}
	headerBlocks := []map[string][]string{s.def.Search.Headers}
	if s.def.Download != nil {
		headerBlocks = append(headerBlocks, s.def.Download.Headers)
	}
	for _, h := range headerBlocks {
		for k := range h {
			if credentialHeader(k) {
				return true
			}
		}
	}
	return false
}

// credentialSettingName reports whether a setting is named like a sign-in.
func credentialSettingName(name string) bool {
	n := strings.ToLower(name)
	for _, c := range credentialNames {
		if n == c {
			return true
		}
	}
	return false
}

// plainSettings are text settings that are preferences, not sign-ins.
var plainSettings = map[string]bool{
	"useragent": true, "user-agent": true, "user_agent": true,
	"language": true, "lang": true, "sort": true, "sortby": true, "sort_by": true,
	"order": true, "orderby": true, "order_by": true, "category": true, "categories": true,
	"cat": true, "sitelink": true, "apiurl": true, "api_url": true, "uploader": true,
}

// credentialHeader reports whether a header the definition adds can carry a
// sign-in: its name speaks of authorisation, a token, a key, a session or a
// cookie.
func credentialHeader(name string) bool {
	n := strings.ToLower(name)
	for _, p := range []string{"auth", "token", "key", "session", "cookie"} {
		if strings.Contains(n, p) {
			return true
		}
	}
	return false
}

// siteRelated reports whether host is the site's address (the one the person
// configured) or shares its registered domain, such as api.example.org for
// www.example.org. The definition's other links don't count: a host added
// to that list would otherwise be handed the login, cookie or API key.
func (s *cgSession) siteRelated(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return false
	}
	k := strings.ToLower(strings.TrimSuffix(s.base.Hostname(), "."))
	if k == host {
		return true
	}
	hostDomain, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return false // an IP address or a name without a registered domain: only an exact match counts
	}
	d, err := publicsuffix.EffectiveTLDPlusOne(k)
	return err == nil && d == hostDomain
}

// checkPageAddress refuses an address a page handed out (a redirect, a
// result's link, a download link) that is not a web address, or that points
// at this machine or the home network when the site itself is on the
// internet. Such a page is trying to make Mediarium call something it was
// never meant to reach. Other hosts are fine: sites link to file mirrors.
func (s *cgSession) checkPageAddress(raw string) error {
	if strings.HasPrefix(raw, "magnet:") {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: bad link from the site: %w", s.name, netguard.CleanError(err))
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s: the site sent a link that is not a web address, so Mediarium refused it", s.name)
	}
	if s.siteRelated(u.Hostname()) || localHost(s.base.Hostname()) {
		return nil
	}
	if err := s.checkForeignHost(context.Background(), u.Hostname()); err == nil {
		return nil
	}
	return fmt.Errorf("%s: the site sent a link to %s, which is on your own network, so Mediarium refused it", s.name, u.Hostname())
}

// localHost reports whether host names this machine or the home network: a
// private, loopback or shared address, or a name such as localhost.
func localHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if addr, err := netip.ParseAddr(host); err == nil {
		return netguard.NotPublic(addr) || netguard.Blocked(addr)
	}
	if host == "localhost" || !strings.Contains(host, ".") {
		return true // a bare name only the local network can resolve
	}
	for _, suffix := range []string{".localhost", ".local", ".lan", ".home", ".internal", ".home.arpa", ".localdomain"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
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
