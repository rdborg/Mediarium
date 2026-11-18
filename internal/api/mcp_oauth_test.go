package api_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// oauthEnv is an MCP environment with the address switched on, plus the helpers
// to walk the sign-in the way an MCP client does.
type oauthEnv struct {
	*mcpEnv
	plain *http.Client // no cookies, does not follow redirects: a program, not a browser
}

func newOAuthEnv(t *testing.T) *oauthEnv {
	t.Helper()
	e := newMCPEnv(t)
	e.enable(t)
	e.allowDownloads(t, true)
	return &oauthEnv{mcpEnv: e, plain: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func pkcePair(seed string) (verifier, challenge string) {
	verifier = strings.Repeat(seed, 50)[:60]
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func (o *oauthEnv) do(t *testing.T, c *http.Client, method, u, body string, headers ...string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, u, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, raw
}

func asMap(raw []byte) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m
}

// register makes a public client the way an MCP client does.
func (o *oauthEnv) register(t *testing.T, redirect string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"client_name": "Test assistant", "redirect_uris": []string{redirect}})
	st, _, raw := o.do(t, o.plain, http.MethodPost, o.base+"/oauth/register", string(body), "Content-Type", "application/json")
	if st != http.StatusCreated {
		t.Fatalf("register: %d %s", st, raw)
	}
	return asMap(raw)["client_id"].(string)
}

// authorize starts the sign-in and returns the id of the waiting request, or the raw response when it did not get that far.
func (o *oauthEnv) authorize(t *testing.T, params url.Values) (id string, status int, location string) {
	t.Helper()
	st, h, _ := o.do(t, o.plain, http.MethodGet, o.base+"/oauth/authorize?"+params.Encode(), "")
	loc := h.Get("Location")
	if st == http.StatusFound && strings.HasPrefix(loc, "/connect-app?r=") {
		return strings.TrimPrefix(loc, "/connect-app?r="), st, loc
	}
	return "", st, loc
}

func authParams(clientID, redirect, scope, challenge string) url.Values {
	v := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect}, "state": {"xyz"}}
	if scope != "" {
		v.Set("scope", scope)
	}
	if challenge != "" {
		v.Set("code_challenge", challenge)
		v.Set("code_challenge_method", "S256")
	}
	return v
}

// decide is the Allow or Deny click of the consent page.
func (o *oauthEnv) decide(t *testing.T, c *http.Client, id string, allow, write bool) (int, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]bool{"allow": allow, "write": write})
	st, _, raw := o.do(t, c, http.MethodPost, o.base+"/api/oauth/requests/"+id, string(body), "Content-Type", "application/json")
	return st, asMap(raw)["redirect"].(string)
}

func queryOf(t *testing.T, redirect string) url.Values {
	t.Helper()
	u, err := url.Parse(redirect)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func (o *oauthEnv) token(t *testing.T, form url.Values, headers ...string) (int, map[string]any) {
	t.Helper()
	headers = append(headers, "Content-Type", "application/x-www-form-urlencoded")
	st, _, raw := o.do(t, o.plain, http.MethodPost, o.base+"/oauth/token", form.Encode(), headers...)
	return st, asMap(raw)
}

const testRedirect = "http://localhost:6274/oauth/callback"

// signIn runs the whole flow with PKCE and returns the token answer.
func (o *oauthEnv) signIn(t *testing.T, scope string, write bool) (clientID string, tokens map[string]any) {
	t.Helper()
	clientID = o.register(t, testRedirect)
	verifier, challenge := pkcePair("a1b2")
	id, st, loc := o.authorize(t, authParams(clientID, testRedirect, scope, challenge))
	if id == "" {
		t.Fatalf("authorize: %d %s", st, loc)
	}
	dst, redirect := o.decide(t, o.admin, id, true, write)
	if dst != http.StatusOK {
		t.Fatalf("decision: %d", dst)
	}
	q := queryOf(t, redirect)
	if q.Get("state") != "xyz" || q.Get("code") == "" {
		t.Fatalf("redirect: %s", redirect)
	}
	tst, tok := o.token(t, url.Values{"grant_type": {"authorization_code"}, "code": {q.Get("code")}, "redirect_uri": {testRedirect}, "code_verifier": {verifier}, "client_id": {clientID}})
	if tst != http.StatusOK {
		t.Fatalf("token: %d %v", tst, tok)
	}
	return clientID, tok
}

func TestOAuthDiscoveryDocuments(t *testing.T) {
	o := newOAuthEnv(t)
	tests := []struct {
		name    string
		path    string
		headers []string
		status  int
		check   func(t *testing.T, m map[string]any)
	}{
		{"protected resource", "/.well-known/oauth-protected-resource", nil, 200, func(t *testing.T, m map[string]any) {
			if m["resource"] != o.base+"/mcp" || m["authorization_servers"].([]any)[0] != o.base || m["bearer_methods_supported"].([]any)[0] != "header" {
				t.Fatalf("%v", m)
			}
			if len(m["scopes_supported"].([]any)) != 2 {
				t.Fatalf("%v", m)
			}
		}},
		{"protected resource, path suffix", "/.well-known/oauth-protected-resource/mcp", nil, 200, func(t *testing.T, m map[string]any) {
			if m["resource"] != o.base+"/mcp" {
				t.Fatalf("%v", m)
			}
		}},
		{"protected resource of the api path", "/.well-known/oauth-protected-resource/api/mcp", nil, 200, func(t *testing.T, m map[string]any) {
			if m["resource"] != o.base+"/api/mcp" {
				t.Fatalf("%v", m)
			}
		}},
		{"authorization server", "/.well-known/oauth-authorization-server", nil, 200, func(t *testing.T, m map[string]any) {
			want := map[string]string{"issuer": o.base, "authorization_endpoint": o.base + "/oauth/authorize", "token_endpoint": o.base + "/oauth/token", "registration_endpoint": o.base + "/oauth/register"}
			for k, v := range want {
				if m[k] != v {
					t.Fatalf("%s = %v, want %s", k, m[k], v)
				}
			}
			if m["code_challenge_methods_supported"].([]any)[0] != "S256" || len(m["token_endpoint_auth_methods_supported"].([]any)) != 3 || len(m["grant_types_supported"].([]any)) != 2 || m["response_types_supported"].([]any)[0] != "code" {
				t.Fatalf("%v", m)
			}
		}},
		{"authorization server, path suffix", "/.well-known/oauth-authorization-server/mcp", nil, 200, func(t *testing.T, m map[string]any) {
			if m["issuer"] != o.base {
				t.Fatalf("%v", m)
			}
		}},
		{"openid style address", "/.well-known/openid-configuration", nil, 200, func(t *testing.T, m map[string]any) {
			if m["token_endpoint"] != o.base+"/oauth/token" {
				t.Fatalf("%v", m)
			}
		}},
		{"behind an https proxy", "/.well-known/oauth-authorization-server", []string{"X-Forwarded-Proto", "https", "X-Forwarded-Host", "media.example.com"}, 200, func(t *testing.T, m map[string]any) {
			if m["issuer"] != "https://media.example.com" || m["token_endpoint"] != "https://media.example.com/oauth/token" {
				t.Fatalf("%v", m)
			}
		}},
		{"unknown well-known", "/.well-known/security.txt", nil, 404, nil},
		{"unknown nested well-known", "/.well-known/acme-challenge/abc", nil, 404, nil},
		{"well-known root", "/.well-known/", nil, 404, nil},
		{"unknown oauth path", "/oauth/nothing", nil, 404, nil},
		{"unknown mcp path", "/mcp/.well-known/x", nil, 404, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, h, raw := o.do(t, o.plain, http.MethodGet, o.base+tc.path, "", tc.headers...)
			if st != tc.status {
				t.Fatalf("status %d, want %d: %s", st, tc.status, raw)
			}
			if !strings.HasPrefix(h.Get("Content-Type"), "application/json") || !json.Valid(raw) {
				t.Fatalf("not JSON: %s %q", h.Get("Content-Type"), raw)
			}
			if tc.check != nil {
				tc.check(t, asMap(raw))
			}
		})
	}
}

func TestOAuthDiscoveryUnderAURLBaseAndWhenOff(t *testing.T) {
	// MCP off: every route is a JSON 404.
	e := newMCPEnv(t)
	for _, p := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-authorization-server", "/oauth/register", "/oauth/authorize", "/oauth/token"} {
		resp, err := http.Get(e.base + p)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound || !json.Valid(raw) || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
			t.Fatalf("%s while off: %d %q", p, resp.StatusCode, raw)
		}
	}

	// Under a sub-path the issuer carries it.
	const prefix = "/mediarium"
	root, client := newBaseServer(t, prefix)
	if resp, body := basePost(t, client, root+prefix+"/api/onboarding/admin", `{"username":"ryan","password":"correct-horse-battery-staple","firstName":"R","lastName":"T"}`); resp.StatusCode != http.StatusCreated {
		t.Fatalf("onboarding: %d %s", resp.StatusCode, body)
	}
	postJSONMethod[map[string]any](t, client, http.MethodPut, root+prefix+"/api/system/mcp", map[string]any{"enabled": true}, http.StatusOK)
	for _, p := range []string{prefix + "/.well-known/oauth-authorization-server", "/.well-known/oauth-authorization-server"} {
		m := getJSON[map[string]any](t, client, root+p)
		if m["issuer"] != root+prefix || m["authorization_endpoint"] != root+prefix+"/oauth/authorize" {
			t.Fatalf("%s: %v", p, m)
		}
	}
	resp, err := http.Post(root+prefix+"/mcp", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, `resource_metadata="`+root+prefix+`/.well-known/oauth-protected-resource"`) {
		t.Fatalf("challenge under a base: %q", got)
	}
}

func TestMCPUnauthorizedPointsToTheMetadata(t *testing.T) {
	o := newOAuthEnv(t)
	meta := `resource_metadata="` + o.base + `/.well-known/oauth-protected-resource"`
	tests := []struct {
		name, method, key string
		headers           []string
		wantInvalid       bool
	}{
		{"no credentials", http.MethodPost, "", nil, false},
		{"an unknown key", http.MethodPost, "not-a-key", nil, true},
		{"an unknown token", http.MethodPost, "mcpat_nothing", nil, true},
		{"behind a proxy", http.MethodPost, "", []string{"X-Forwarded-Proto", "https", "X-Forwarded-Host", "m.example.com"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hs := tc.headers
			if tc.key != "" {
				hs = append(append([]string{}, hs...), "Authorization", "Bearer "+tc.key)
			}
			st, h, _ := o.do(t, o.plain, tc.method, o.base+"/mcp", rpcBody(1, "ping", nil), append(hs, "Content-Type", "application/json")...)
			if st != http.StatusUnauthorized {
				t.Fatalf("status %d", st)
			}
			got := h.Get("WWW-Authenticate")
			want := meta
			if len(tc.headers) > 0 {
				want = `resource_metadata="https://m.example.com/.well-known/oauth-protected-resource"`
			}
			if !strings.HasPrefix(got, "Bearer ") || !strings.Contains(got, want) || strings.Contains(got, "invalid_token") != tc.wantInvalid {
				t.Fatalf("WWW-Authenticate = %q", got)
			}
		})
	}
	// An API key still works as before, and a signed-in GET is the usual 405.
	if st, _, _ := o.rpc(t, o.adminKey, rpcBody(1, "ping", nil)); st != http.StatusOK {
		t.Fatalf("API key: %d", st)
	}
	if st, _, _ := o.rpc(t, "", rpcBody(1, "ping", nil), "X-Api-Key", o.adminKey); st != http.StatusOK {
		t.Fatalf("X-Api-Key: %d", st)
	}
	if st, _, _ := o.do(t, o.plain, http.MethodGet, o.base+"/mcp", "", "Authorization", "Bearer "+o.adminKey); st != http.StatusMethodNotAllowed {
		t.Fatalf("GET with a key: %d", st)
	}
}

func TestOAuthRegistration(t *testing.T) {
	o := newOAuthEnv(t)
	tests := []struct {
		name   string
		body   string
		status int
		errors string
	}{
		{"https redirect", `{"client_name":"HA","redirect_uris":["https://my.home-assistant.io/redirect/oauth"]}`, 201, ""},
		{"localhost http", `{"client_name":"Inspector","redirect_uris":["http://localhost:6274/cb","http://127.0.0.1:9/cb"]}`, 201, ""},
		{"custom scheme", `{"client_name":"Phone","redirect_uris":["com.example.app://callback"]}`, 201, ""},
		{"client secret asked for", `{"client_name":"Conf","redirect_uris":["https://a.example/cb"],"token_endpoint_auth_method":"client_secret_post"}`, 201, ""},
		{"no name still works", `{"redirect_uris":["https://a.example/cb"]}`, 201, ""},
		{"no redirect", `{"client_name":"x"}`, 400, "invalid_redirect_uri"},
		{"plain http elsewhere", `{"client_name":"x","redirect_uris":["http://evil.example/cb"]}`, 400, "invalid_redirect_uri"},
		{"javascript scheme", `{"client_name":"x","redirect_uris":["javascript:alert(1)"]}`, 400, "invalid_redirect_uri"},
		{"data scheme", `{"client_name":"x","redirect_uris":["data:text/html,hi"]}`, 400, "invalid_redirect_uri"},
		{"fragment", `{"client_name":"x","redirect_uris":["https://a.example/cb#x"]}`, 400, "invalid_redirect_uri"},
		{"user info", `{"client_name":"x","redirect_uris":["https://u:p@a.example/cb"]}`, 400, "invalid_redirect_uri"},
		{"no host", `{"client_name":"x","redirect_uris":["https:///cb"]}`, 400, "invalid_redirect_uri"},
		{"space in it", `{"client_name":"x","redirect_uris":["https://a.example/c b"]}`, 400, "invalid_redirect_uri"},
		{"listed twice", `{"client_name":"x","redirect_uris":["https://a.example/cb","https://a.example/cb"]}`, 400, "invalid_redirect_uri"},
		{"too many", `{"client_name":"x","redirect_uris":["https://a/1","https://a/2","https://a/3","https://a/4","https://a/5","https://a/6","https://a/7","https://a/8","https://a/9","https://a/10","https://a/11"]}`, 400, "invalid_redirect_uri"},
		{"unknown grant", `{"client_name":"x","redirect_uris":["https://a.example/cb"],"grant_types":["password"]}`, 400, "invalid_client_metadata"},
		{"unknown auth method", `{"client_name":"x","redirect_uris":["https://a.example/cb"],"token_endpoint_auth_method":"private_key_jwt"}`, 400, "invalid_client_metadata"},
		{"not JSON", `nope`, 400, "invalid_client_metadata"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, h, raw := o.do(t, o.plain, http.MethodPost, o.base+"/oauth/register", tc.body, "Content-Type", "application/json")
			if st != tc.status {
				t.Fatalf("status %d, want %d: %s", st, tc.status, raw)
			}
			m := asMap(raw)
			if tc.status == 201 {
				if !strings.HasPrefix(m["client_id"].(string), "mcpc_") || h.Get("Cache-Control") != "no-store" {
					t.Fatalf("%v", m)
				}
				_, hasSecret := m["client_secret"]
				if hasSecret != strings.Contains(tc.body, "client_secret_post") {
					t.Fatalf("secret handed out wrongly: %v", m)
				}
			} else if m["error"] != tc.errors {
				t.Fatalf("error %v, want %s", m["error"], tc.errors)
			}
		})
	}
}

func TestOAuthRegistrationIsRateLimited(t *testing.T) {
	o := newOAuthEnv(t)
	body := `{"client_name":"x","redirect_uris":["https://a.example/cb"]}`
	limited := false
	for i := 0; i < 15; i++ {
		st, h, _ := o.do(t, o.plain, http.MethodPost, o.base+"/oauth/register", body, "Content-Type", "application/json")
		if st == http.StatusTooManyRequests && h.Get("Retry-After") != "" {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("fifteen registrations in a row were never slowed down")
	}
}

func TestOAuthFullFlow(t *testing.T) {
	o := newOAuthEnv(t)
	clientID, tok := o.signIn(t, "mcp:read mcp:write", true)
	if tok["token_type"] != "Bearer" || tok["expires_in"].(float64) != 3600 || tok["scope"] != "mcp:read mcp:write" {
		t.Fatalf("%v", tok)
	}
	access := tok["access_token"].(string)
	if tok["refresh_token"] == "" {
		t.Fatal("no refresh token")
	}
	// The token opens the MCP address.
	st, out, _ := o.rpc(t, access, rpcBody(1, "tools/list", nil))
	if st != http.StatusOK || out["result"] == nil {
		t.Fatalf("tools/list: %d %v", st, out)
	}
	if _, text, parsed := o.call(t, access, "library_stats", nil); text == "" || parsed == nil {
		t.Fatalf("library_stats gave %q", text)
	}
	// The connected apps list shows it, and the API key still works beside it.
	apps := getJSON[map[string]any](t, o.admin, o.base+"/api/system/mcp/apps")
	list := apps["apps"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["clientName"] != "Test assistant" || list[0].(map[string]any)["account"] != "ryan" {
		t.Fatalf("apps: %v", apps)
	}
	if list[0].(map[string]any)["lastUsedAt"] == "" {
		t.Fatalf("last used not set: %v", list[0])
	}
	if st, _, _ := o.rpc(t, o.adminKey, rpcBody(1, "ping", nil)); st != http.StatusOK {
		t.Fatalf("API key: %d", st)
	}
	// Revoking ends it at once, refresh token included.
	id := int64(list[0].(map[string]any)["id"].(float64))
	postJSONMethod[map[string]any](t, o.admin, http.MethodDelete, o.base+"/api/system/mcp/apps/"+oauthItoa(id), nil, http.StatusNoContent)
	if st, _, _ := o.rpc(t, access, rpcBody(1, "ping", nil)); st != http.StatusUnauthorized {
		t.Fatalf("after revoking: %d", st)
	}
	if st, _ := o.token(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok["refresh_token"].(string)}, "client_id": {clientID}}); st != http.StatusBadRequest {
		t.Fatalf("refresh after revoking: %d", st)
	}
}

func oauthItoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestOAuthAuthorizeRefusals(t *testing.T) {
	o := newOAuthEnv(t)
	client := o.register(t, testRedirect)
	_, challenge := pkcePair("q9")
	good := func() url.Values { return authParams(client, testRedirect, "", challenge) }
	tests := []struct {
		name     string
		params   func() url.Values
		wantPage bool   // the browser stays on a page here: nothing is trusted enough to redirect to
		errCode  string // when redirecting back with an error
	}{
		{"unknown client", func() url.Values { v := good(); v.Set("client_id", "mcpc_unknown"); return v }, true, ""},
		{"redirect address that was not registered", func() url.Values { v := good(); v.Set("redirect_uri", "http://localhost:1/other"); return v }, true, ""},
		{"redirect address with a path added", func() url.Values { v := good(); v.Set("redirect_uri", testRedirect+"/x"); return v }, true, ""},
		{"wrong response type", func() url.Values { v := good(); v.Set("response_type", "token"); return v }, false, "unsupported_response_type"},
		{"unknown scope", func() url.Values { v := good(); v.Set("scope", "mcp:read admin"); return v }, false, "invalid_scope"},
		{"no PKCE", func() url.Values { return authParams(client, testRedirect, "", "") }, false, "invalid_request"},
		{"plain PKCE", func() url.Values { v := good(); v.Set("code_challenge_method", "plain"); return v }, false, "invalid_request"},
		{"a challenge that is too short", func() url.Values { v := good(); v.Set("code_challenge", "abc"); return v }, false, "invalid_request"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, st, loc := o.authorize(t, tc.params())
			if id != "" {
				t.Fatalf("accepted: %s", loc)
			}
			if tc.wantPage {
				if st != http.StatusBadRequest {
					t.Fatalf("status %d, want a page", st)
				}
				return
			}
			if st != http.StatusFound {
				t.Fatalf("status %d", st)
			}
			q := queryOf(t, loc)
			if !strings.HasPrefix(loc, testRedirect) || q.Get("error") != tc.errCode || q.Get("state") != "xyz" {
				t.Fatalf("redirect %s", loc)
			}
		})
	}
	// The good one is accepted.
	if id, _, _ := o.authorize(t, good()); id == "" {
		t.Fatal("a good request was refused")
	}
}

func TestOAuthConsent(t *testing.T) {
	o := newOAuthEnv(t)
	client := o.register(t, testRedirect)
	_, challenge := pkcePair("c3")
	start := func() string {
		id, _, loc := o.authorize(t, authParams(client, testRedirect, "mcp:read", challenge))
		if id == "" {
			t.Fatalf("authorize: %s", loc)
		}
		return id
	}
	// The page asks what it is about.
	id := start()
	info := getJSON[map[string]any](t, o.admin, o.base+"/api/oauth/requests/"+id)
	if info["clientName"] != "Test assistant" || info["canApprove"] != true || info["account"] != "ryan" || info["redirectHost"] != "localhost:6274" {
		t.Fatalf("info: %v", info)
	}
	// Without being signed in it can't be read or decided.
	if st, _, _ := o.do(t, o.plain, http.MethodGet, o.base+"/api/oauth/requests/"+id, ""); st != http.StatusUnauthorized {
		t.Fatalf("anonymous info: %d", st)
	}
	if st, _, _ := o.do(t, o.plain, http.MethodPost, o.base+"/api/oauth/requests/"+id, `{"allow":true}`, "Content-Type", "application/json"); st != http.StatusUnauthorized {
		t.Fatalf("anonymous decision: %d", st)
	}
	// An API key can't say yes.
	if st, _, _ := o.do(t, o.plain, http.MethodPost, o.base+"/api/oauth/requests/"+id, `{"allow":true}`, "Content-Type", "application/json", "X-Api-Key", o.adminKey); st != http.StatusForbidden {
		t.Fatalf("API key decision: %d", st)
	}
	// A member sees why not and can't approve; they can say no.
	jar, _ := cookiejar.New(nil)
	sam := &http.Client{Jar: jar, Timeout: 20 * time.Second}
	postJSON[map[string]any](t, sam, o.base+"/api/auth/login", map[string]string{"username": "sam", "password": "kids-password"}, http.StatusOK)
	if got := getJSON[map[string]any](t, sam, o.base+"/api/oauth/requests/"+id); got["canApprove"] != false {
		t.Fatalf("member info: %v", got)
	}
	if st, _, raw := o.do(t, sam, http.MethodPost, o.base+"/api/oauth/requests/"+id, `{"allow":true}`, "Content-Type", "application/json"); st != http.StatusForbidden || !strings.Contains(string(raw), "administrator") {
		t.Fatalf("member decision: %d %s", st, raw)
	}
	// The administrator says no: the app hears access_denied and state.
	if _, redirect := o.decide(t, o.admin, id, false, false); queryOf(t, redirect).Get("error") != "access_denied" || queryOf(t, redirect).Get("state") != "xyz" {
		t.Fatalf("deny: %s", redirect)
	}
	// The request is gone after an answer.
	if st, _, _ := o.do(t, o.admin, http.MethodGet, o.base+"/api/oauth/requests/"+id, ""); st != http.StatusNotFound {
		t.Fatalf("after the answer: %d", st)
	}
	if st, _, _ := o.do(t, o.admin, http.MethodGet, o.base+"/api/oauth/requests/nothing", ""); st != http.StatusNotFound {
		t.Fatalf("unknown request: %d", st)
	}
}

func TestOAuthTokenRefusals(t *testing.T) {
	o := newOAuthEnv(t)
	client := o.register(t, testRedirect)
	other := o.register(t, testRedirect)
	verifier, challenge := pkcePair("t5")
	code := func() string {
		id, _, loc := o.authorize(t, authParams(client, testRedirect, "mcp:read", challenge))
		if id == "" {
			t.Fatalf("authorize: %s", loc)
		}
		_, redirect := o.decide(t, o.admin, id, true, true)
		return queryOf(t, redirect).Get("code")
	}
	form := func(c string) url.Values {
		return url.Values{"grant_type": {"authorization_code"}, "code": {c}, "redirect_uri": {testRedirect}, "code_verifier": {verifier}, "client_id": {client}}
	}
	tests := []struct {
		name   string
		build  func() url.Values
		status int
		errMsg string
	}{
		{"wrong verifier", func() url.Values { f := form(code()); f.Set("code_verifier", strings.Repeat("z", 60)); return f }, 400, "invalid_grant"},
		{"no verifier", func() url.Values { f := form(code()); f.Del("code_verifier"); return f }, 400, "invalid_grant"},
		{"wrong redirect address", func() url.Values { f := form(code()); f.Set("redirect_uri", testRedirect+"/x"); return f }, 400, "invalid_grant"},
		{"code of another client", func() url.Values { f := form(code()); f.Set("client_id", other); return f }, 400, "invalid_grant"},
		{"unknown code", func() url.Values { return form("not-a-code") }, 400, "invalid_grant"},
		{"unknown client", func() url.Values { f := form(code()); f.Set("client_id", "mcpc_none"); return f }, 401, "invalid_client"},
		{"unsupported grant", func() url.Values { f := form(code()); f.Set("grant_type", "password"); return f }, 400, "unsupported_grant_type"},
		{"unknown refresh token", func() url.Values {
			return url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"mcprt_x"}, "client_id": {client}}
		}, 400, "invalid_grant"},
		{"a good one", func() url.Values { return form(code()) }, 200, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, m := o.token(t, tc.build())
			if st != tc.status || (tc.errMsg != "" && m["error"] != tc.errMsg) {
				t.Fatalf("%d %v, want %d %s", st, m, tc.status, tc.errMsg)
			}
		})
	}

	// A code works once; a second try takes the grant it made down.
	c := code()
	st, first := o.token(t, form(c))
	if st != 200 {
		t.Fatalf("first use: %d %v", st, first)
	}
	if st, m := o.token(t, form(c)); st != 400 || m["error"] != "invalid_grant" {
		t.Fatalf("second use: %d %v", st, m)
	}
	if st, _, _ := o.rpc(t, first["access_token"].(string), rpcBody(1, "ping", nil)); st != http.StatusUnauthorized {
		t.Fatalf("token of a reused code still works: %d", st)
	}
	// A code runs out after 60 seconds: not waited for here, the unit is the
	// constant; an expired request is covered by the lookup tests.
}

func TestOAuthRefreshRotation(t *testing.T) {
	o := newOAuthEnv(t)
	client, tok := o.signIn(t, "", true)
	refresh1 := tok["refresh_token"].(string)
	access1 := tok["access_token"].(string)
	refreshForm := func(r string) url.Values {
		return url.Values{"grant_type": {"refresh_token"}, "refresh_token": {r}, "client_id": {client}}
	}
	st, second := o.token(t, refreshForm(refresh1))
	if st != 200 || second["refresh_token"] == refresh1 || second["access_token"] == access1 {
		t.Fatalf("refresh: %d %v", st, second)
	}
	if st, _, _ := o.rpc(t, access1, rpcBody(1, "ping", nil)); st != http.StatusUnauthorized {
		t.Fatalf("the old access token still works: %d", st)
	}
	access2, refresh2 := second["access_token"].(string), second["refresh_token"].(string)
	if st, _, _ := o.rpc(t, access2, rpcBody(1, "ping", nil)); st != http.StatusOK {
		t.Fatalf("new access token: %d", st)
	}
	// The old refresh token again means it leaked: the whole grant goes.
	if st, m := o.token(t, refreshForm(refresh1)); st != 400 || m["error"] != "invalid_grant" {
		t.Fatalf("reuse: %d %v", st, m)
	}
	if st, _, _ := o.rpc(t, access2, rpcBody(1, "ping", nil)); st != http.StatusUnauthorized {
		t.Fatalf("access token after a reuse: %d", st)
	}
	if st, _ := o.token(t, refreshForm(refresh2)); st != 400 {
		t.Fatalf("refresh token after a reuse: %d", st)
	}
	if apps := getJSON[map[string]any](t, o.admin, o.base+"/api/system/mcp/apps"); len(apps["apps"].([]any)) != 0 {
		t.Fatalf("the grant is still listed: %v", apps)
	}
}

func TestOAuthScopeIsEnforced(t *testing.T) {
	o := newOAuthEnv(t)
	names := func(token string) map[string]bool {
		_, out, _ := o.rpc(t, token, rpcBody(1, "tools/list", nil))
		got := map[string]bool{}
		for _, tl := range out["result"].(map[string]any)["tools"].([]any) {
			got[tl.(map[string]any)["name"].(string)] = true
		}
		return got
	}
	// Read only: asked for it, or narrowed on the consent page.
	for _, tc := range []struct {
		name, scope string
		write       bool
	}{{"asked for read only", "mcp:read", true}, {"narrowed on the page", "mcp:read mcp:write", false}} {
		t.Run(tc.name, func(t *testing.T) {
			_, tok := o.signIn(t, tc.scope, tc.write)
			if tok["scope"] != "mcp:read" {
				t.Fatalf("scope %v", tok["scope"])
			}
			access := tok["access_token"].(string)
			got := names(access)
			if !got["library_stats"] || got["add_movie"] || got["set_movie_profile"] {
				t.Fatalf("tools: %v", got)
			}
			isErr, text, _ := o.call(t, access, "add_movie", map[string]any{"tmdb_id": 603, "confirm": true})
			if !isErr || !strings.Contains(text, "read-only") {
				t.Fatalf("a write tool with a read scope: %v %q", isErr, text)
			}
		})
	}
	// Write scope: the write tools are there and ask for confirmation as usual.
	_, tok := o.signIn(t, "", true)
	access := tok["access_token"].(string)
	if got := names(access); !got["library_stats"] || !got["add_movie"] {
		t.Fatalf("tools: %v", got)
	}
	_, text, _ := o.call(t, access, "add_movie", map[string]any{"tmdb_id": 603})
	if strings.Contains(text, "read-only") {
		t.Fatalf("a write tool with write scope should not be refused as read-only: %q", text)
	}
}

func TestOAuthPreregisteredClient(t *testing.T) {
	o := newOAuthEnv(t)
	// Only an administrator's browser session can register one.
	if st, _, _ := o.do(t, o.plain, http.MethodPost, o.base+"/api/system/mcp/clients", `{"name":"HA","redirectUri":"https://my.home-assistant.io/redirect/oauth"}`, "Content-Type", "application/json", "X-Api-Key", o.adminKey); st != http.StatusForbidden {
		t.Fatalf("with an API key: %d", st)
	}
	for _, bad := range []string{`{"name":"","redirectUri":"https://a.example/cb"}`, `{"name":"HA","redirectUri":"javascript:x"}`, `{"name":"HA","redirectUri":""}`} {
		if st, _, _ := o.do(t, o.admin, http.MethodPost, o.base+"/api/system/mcp/clients", bad, "Content-Type", "application/json"); st != http.StatusBadRequest {
			t.Fatalf("%s: %d", bad, st)
		}
	}
	made := postJSON[map[string]any](t, o.admin, o.base+"/api/system/mcp/clients", map[string]string{"name": "Home Assistant", "redirectUri": "http://homeassistant.local:8123/auth/external/callback"}, http.StatusCreated)
	id, secret := made["clientId"].(string), made["clientSecret"].(string)
	if id == "" || secret == "" {
		t.Fatalf("%v", made)
	}
	if listed := getJSON[map[string]any](t, o.admin, o.base+"/api/system/mcp/apps")["clients"].([]any); len(listed) != 1 || listed[0].(map[string]any)["clientId"] != id || listed[0].(map[string]any)["clientSecret"] != nil {
		t.Fatalf("clients: %v", listed)
	}
	redirect := "http://homeassistant.local:8123/auth/external/callback"
	// It signs in with its secret (here without PKCE, like a server-side client may).
	reqID, st, loc := o.authorize(t, authParams(id, redirect, "mcp:read", ""))
	if reqID == "" {
		t.Fatalf("authorize: %d %s", st, loc)
	}
	_, back := o.decide(t, o.admin, reqID, true, true)
	code := queryOf(t, back).Get("code")
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}}
	// Wrong secret: refused, and it doesn't burn the right one's chance.
	if st, m := o.token(t, form, "Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(id+":wrong"))); st != http.StatusUnauthorized || m["error"] != "invalid_client" {
		t.Fatalf("wrong secret: %d %v", st, m)
	}
	if st, m := o.token(t, form); st != http.StatusUnauthorized {
		t.Fatalf("no secret: %d %v", st, m)
	}
	post := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}, "client_id": {id}, "client_secret": {secret}}
	st2, tok := o.token(t, post)
	if st2 != 200 {
		t.Fatalf("with the secret: %d %v", st2, tok)
	}
	if st, _, _ := o.rpc(t, tok["access_token"].(string), rpcBody(1, "ping", nil)); st != http.StatusOK {
		t.Fatalf("token: %d", st)
	}
	// Basic works for the refresh.
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte(id+":"+secret))
	if st, m := o.token(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok["refresh_token"].(string)}}, "Authorization", basic); st != 200 {
		t.Fatalf("refresh with Basic: %d %v", st, m)
	}
	// Removing the client ends its tokens.
	postJSONMethod[map[string]any](t, o.admin, http.MethodDelete, o.base+"/api/system/mcp/clients/"+id, nil, http.StatusNoContent)
	if st, _, _ := o.rpc(t, tok["access_token"].(string), rpcBody(1, "ping", nil)); st != http.StatusUnauthorized {
		t.Fatalf("after removing the client: %d", st)
	}
}

func TestOAuthEndsWhenTheAdministratorChanges(t *testing.T) {
	o := newOAuthEnv(t)
	// Another administrator approves, then is made a member.
	created := postJSON[map[string]any](t, o.admin, o.base+"/api/users", map[string]any{"username": "kim", "password": "kims-password-1", "name": "Kim", "role": "admin"}, http.StatusCreated)
	jar, _ := cookiejar.New(nil)
	kim := &http.Client{Jar: jar, Timeout: 20 * time.Second}
	postJSON[map[string]any](t, kim, o.base+"/api/auth/login", map[string]string{"username": "kim", "password": "kims-password-1"}, http.StatusOK)
	connect := func() string {
		client := o.register(t, testRedirect)
		verifier, challenge := pkcePair("k7")
		id, _, _ := o.authorize(t, authParams(client, testRedirect, "", challenge))
		_, redirect := o.decide(t, kim, id, true, true)
		_, tok := o.token(t, url.Values{"grant_type": {"authorization_code"}, "code": {queryOf(t, redirect).Get("code")}, "redirect_uri": {testRedirect}, "code_verifier": {verifier}, "client_id": {client}})
		return tok["access_token"].(string)
	}
	ping := func(token string) int { st, _, _ := o.rpc(t, token, rpcBody(1, "ping", nil)); return st }

	tokenA := connect()
	if ping(tokenA) != http.StatusOK {
		t.Fatal("not working to begin with")
	}
	// A password change ends them.
	postJSON[map[string]any](t, kim, o.base+"/api/auth/change-password", map[string]string{"currentPassword": "kims-password-1", "newPassword": "kims-password-2"}, http.StatusOK)
	if got := ping(tokenA); got != http.StatusUnauthorized {
		t.Fatalf("after a password change: %d", got)
	}
	postJSON[map[string]any](t, kim, o.base+"/api/auth/login", map[string]string{"username": "kim", "password": "kims-password-2"}, http.StatusOK)
	tokenB := connect()
	// Being made a member ends them, and they stay ended if made an administrator again.
	postJSONMethod[map[string]any](t, o.admin, http.MethodPut, o.base+"/api/users/"+oauthItoa(int64(created["id"].(float64))), map[string]any{"role": "member"}, http.StatusOK)
	if got := ping(tokenB); got != http.StatusUnauthorized {
		t.Fatalf("after being made a member: %d", got)
	}
	postJSONMethod[map[string]any](t, o.admin, http.MethodPut, o.base+"/api/users/"+oauthItoa(int64(created["id"].(float64))), map[string]any{"role": "admin"}, http.StatusOK)
	if got := ping(tokenB); got != http.StatusUnauthorized {
		t.Fatalf("after being made an administrator again: %d", got)
	}
}

func TestOAuthSwitchOffStopsTokens(t *testing.T) {
	o := newOAuthEnv(t)
	_, tok := o.signIn(t, "", true)
	access := tok["access_token"].(string)
	postJSONMethod[map[string]any](t, o.admin, http.MethodPut, o.base+"/api/system/mcp", map[string]any{"enabled": false}, http.StatusOK)
	if st, _, _ := o.rpc(t, access, rpcBody(1, "ping", nil)); st != http.StatusNotFound {
		t.Fatalf("token with MCP off: %d", st)
	}
	if st, m := o.token(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok["refresh_token"].(string)}}); st != http.StatusNotFound {
		t.Fatalf("refresh with MCP off: %d %v", st, m)
	}
}

func TestOAuthPreflight(t *testing.T) {
	o := newOAuthEnv(t)
	for _, p := range []string{"/.well-known/oauth-authorization-server", "/oauth/token", "/oauth/register", "/mcp"} {
		st, h, _ := o.do(t, o.plain, http.MethodOptions, o.base+p, "", "Origin", "https://inspector.example", "Access-Control-Request-Method", "POST")
		if st != http.StatusNoContent || h.Get("Access-Control-Allow-Origin") != "*" || !strings.Contains(h.Get("Access-Control-Allow-Headers"), "Authorization") {
			t.Fatalf("%s: %d %v", p, st, h)
		}
	}
	// A browser-based client calls the token address from another origin: no cookie rides along, so it is allowed.
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"mcprt_x"}, "client_id": {"mcpc_x"}}
	if st, _, _ := o.do(t, o.plain, http.MethodPost, o.base+"/oauth/token", form.Encode(), "Content-Type", "application/x-www-form-urlencoded", "Origin", "https://inspector.example", "Sec-Fetch-Site", "cross-site"); st == http.StatusForbidden {
		t.Fatal("the token address was blocked as a cross-site request")
	}
}
