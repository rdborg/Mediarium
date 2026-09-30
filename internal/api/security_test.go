package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/store"
)

// newSecurityServer is a server with the given proxy settings and nothing set
// up yet (no account).
func newSecurityServer(t *testing.T, trusted, origins string) (*api.Server, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		ConfigDir:           filepath.Join(dir, "config"),
		DownloadsDir:        filepath.Join(dir, "downloads"),
		DownloadsIncomplete: filepath.Join(dir, "downloads", "incomplete"),
		DownloadsComplete:   filepath.Join(dir, "downloads", "complete"),
		MoviesDir:           filepath.Join(dir, "movies"),
		TrustedProxies:      trusted,
		AllowedOrigins:      origins,
	}
	for _, d := range []string{cfg.ConfigDir, cfg.DownloadsIncomplete, cfg.MoviesDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.Open(filepath.Join(cfg.ConfigDir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(cfg.ConfigDir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := api.New(db, cfg, box, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { waitBackground(t, server) })
	srv := httptest.NewServer(server.Routes())
	t.Cleanup(srv.Close)
	return server, srv.URL
}

// rawPost sends a JSON POST with the given headers and returns the response.
func rawPost(t *testing.T, client *http.Client, url string, body any, headers map[string]string) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestLoginLimitCannotBeEvadedWithForwardedForHeader(t *testing.T) {
	// No trusted proxy: the header must be ignored completely.
	_, base := newSecurityServer(t, "none", "")
	client := &http.Client{}
	admin := map[string]string{"username": "ryan", "password": "correct-horse-battery-staple"}
	if r := rawPost(t, client, base+"/api/onboarding/admin", admin, nil); r.StatusCode != http.StatusCreated {
		t.Fatalf("setup: %d", r.StatusCode)
	}
	bad := map[string]string{"username": "ryan", "password": "wrong-password-here"}
	for i := 0; i < 5; i++ {
		r := rawPost(t, client, base+"/api/auth/login", bad, map[string]string{"X-Forwarded-For": "10.9.8." + string(rune('1'+i))})
		if r.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, r.StatusCode)
		}
	}
	// A sixth attempt with yet another spoofed address is still refused, and
	// so is the right password.
	for _, pw := range []string{"wrong-password-here", "correct-horse-battery-staple"} {
		r := rawPost(t, client, base+"/api/auth/login", map[string]string{"username": "ryan", "password": pw}, map[string]string{"X-Forwarded-For": "203.0.113.77", "X-Real-IP": "203.0.113.78"})
		if r.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("expected 429 despite the spoofed address, got %d", r.StatusCode)
		}
	}
}

func TestLoginLimitUsesRealClientBehindTrustedProxy(t *testing.T) {
	// The test client connects from 127.0.0.1, which the default trusts.
	_, base := newSecurityServer(t, "", "")
	client := &http.Client{}
	rawPost(t, client, base+"/api/onboarding/admin", map[string]string{"username": "ryan", "password": "correct-horse-battery-staple"}, nil)
	bad := map[string]string{"username": "ryan", "password": "wrong-password-here"}
	for i := 0; i < 5; i++ {
		// The caller invents a new left-hand entry every time; the proxy
		// appended the real address on the right.
		xff := "1.1.1." + string(rune('1'+i)) + ", 198.51.100.7"
		if r := rawPost(t, client, base+"/api/auth/login", bad, map[string]string{"X-Forwarded-For": xff}); r.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, r.StatusCode)
		}
	}
	r := rawPost(t, client, base+"/api/auth/login", bad, map[string]string{"X-Forwarded-For": "9.9.9.9, 198.51.100.7"})
	if r.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected the real client to be limited, got %d", r.StatusCode)
	}
	// Someone else behind the same proxy is not affected.
	other := rawPost(t, client, base+"/api/auth/login", map[string]string{"username": "ryan", "password": "correct-horse-battery-staple"}, map[string]string{"X-Forwarded-For": "198.51.100.8"})
	if other.StatusCode != http.StatusOK {
		t.Fatalf("a different client should sign in, got %d", other.StatusCode)
	}
}

func TestLoginLimitPerAccountAcrossAddresses(t *testing.T) {
	_, base := newSecurityServer(t, "", "")
	client := &http.Client{}
	rawPost(t, client, base+"/api/onboarding/admin", map[string]string{"username": "ryan", "password": "correct-horse-battery-staple"}, nil)
	bad := map[string]string{"username": "Ryan", "password": "wrong-password-here"}
	last := 0
	for i := 0; i < 40 && last != http.StatusTooManyRequests; i++ {
		// A different address every time: only the per-account count sees this.
		xff := "198.51." + string(rune('0'+i/10)) + "." + string(rune('0'+i%10))
		last = rawPost(t, client, base+"/api/auth/login", bad, map[string]string{"X-Forwarded-For": xff}).StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Fatal("a password guessed from many addresses was never slowed down")
	}
}

func TestFirstAdminCannotBeRaced(t *testing.T) {
	_, base := newSecurityServer(t, "", "")
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := map[string]string{"username": "admin" + string(rune('a'+i)), "password": "correct-horse-battery-staple"}
			codes <- rawPost(t, &http.Client{}, base+"/api/onboarding/admin", body, nil).StatusCode
		}(i)
	}
	wg.Wait()
	close(codes)
	created := 0
	for c := range codes {
		if c == http.StatusCreated {
			created++
		} else if c != http.StatusConflict {
			t.Errorf("unexpected status %d", c)
		}
	}
	if created != 1 {
		t.Fatalf("%d administrators were created, want exactly 1", created)
	}
}

func TestSessionCookieFlags(t *testing.T) {
	_, base := newSecurityServer(t, "", "")
	client := &http.Client{}
	setup := rawPost(t, client, base+"/api/onboarding/admin", map[string]string{"username": "ryan", "password": "correct-horse-battery-staple"}, nil)
	c := sessionCookie(t, setup)
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Secure {
		t.Errorf("plain HTTP cookie: HttpOnly=%v SameSite=%v Secure=%v", c.HttpOnly, c.SameSite, c.Secure)
	}
	if setup.Header.Get("Strict-Transport-Security") != "" {
		t.Error("HSTS sent over plain HTTP")
	}

	login := map[string]string{"username": "ryan", "password": "correct-horse-battery-staple"}
	behind := rawPost(t, client, base+"/api/auth/login", login, map[string]string{"X-Forwarded-Proto": "https"})
	if c := sessionCookie(t, behind); !c.Secure {
		t.Error("cookie is not Secure behind an HTTPS proxy")
	}
	if behind.Header.Get("Strict-Transport-Security") == "" {
		t.Error("no HSTS behind an HTTPS proxy")
	}
}

func TestForwardedProtoIgnoredFromUntrustedPeer(t *testing.T) {
	_, base := newSecurityServer(t, "none", "")
	client := &http.Client{}
	r := rawPost(t, client, base+"/api/onboarding/admin", map[string]string{"username": "ryan", "password": "correct-horse-battery-staple"}, map[string]string{"X-Forwarded-Proto": "https"})
	if c := sessionCookie(t, r); c.Secure {
		t.Error("X-Forwarded-Proto was believed from an untrusted peer")
	}
}

func sessionCookie(t *testing.T, resp *http.Response) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == "mediarium_session" {
			return c
		}
	}
	t.Fatalf("no session cookie in response (status %d)", resp.StatusCode)
	return nil
}

func TestCrossSiteRequestsAreRefused(t *testing.T) {
	_, base := newSecurityServer(t, "", "")
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	host := strings.TrimPrefix(base, "http://")
	rawPost(t, client, base+"/api/onboarding/admin", map[string]string{"username": "ryan", "password": "correct-horse-battery-staple"}, nil)

	// A page on another site can make the browser send the cookie only if
	// SameSite is bypassed; the Origin check still refuses it.
	evil := rawPost(t, client, base+"/api/auth/api-keys", map[string]string{"name": "x"}, map[string]string{"Origin": "https://evil.example"})
	if evil.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin POST: %d, want 403", evil.StatusCode)
	}
	fetch := rawPost(t, client, base+"/api/auth/api-keys", map[string]string{"name": "x"}, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if fetch.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site fetch: %d, want 403", fetch.StatusCode)
	}
	same := rawPost(t, client, base+"/api/auth/api-keys", map[string]string{"name": "x"}, map[string]string{"Origin": "http://" + host, "Sec-Fetch-Site": "same-origin"})
	if same.StatusCode != http.StatusCreated {
		t.Fatalf("same-origin POST: %d, want 201", same.StatusCode)
	}
	// The login form is protected too (login CSRF).
	if r := rawPost(t, client, base+"/api/auth/login", map[string]string{"username": "ryan", "password": "x"}, map[string]string{"Origin": "https://evil.example"}); r.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin login: %d, want 403", r.StatusCode)
	}
}

func TestAPIKeyClientsAreNotSubjectToOriginCheck(t *testing.T) {
	_, base := newSecurityServer(t, "", "")
	client := &http.Client{Jar: mustJar()}
	rawPost(t, client, base+"/api/onboarding/admin", map[string]string{"username": "ryan", "password": "correct-horse-battery-staple"}, nil)
	var created struct {
		Key string `json:"key"`
	}
	resp := rawPost(t, client, base+"/api/auth/api-keys", map[string]string{"name": "script"}, nil)
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil || created.Key == "" {
		t.Fatalf("create key: %v %+v", err, created)
	}
	r := rawPost(t, &http.Client{}, base+"/api/queue/pause-all", map[string]string{}, map[string]string{"X-API-Key": created.Key, "Origin": "https://tool.example"})
	if r.StatusCode != http.StatusOK {
		t.Fatalf("API key client blocked: %d", r.StatusCode)
	}
}

func mustJar() *cookiejar.Jar { j, _ := cookiejar.New(nil); return j }

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	_, base := newSecurityServer(t, "", "")
	for _, path := range []string{"/", "/api/version", "/api/onboarding/status", "/api/auth/me", "/does/not/exist"} {
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "Permissions-Policy", "X-Robots-Tag"} {
			if resp.Header.Get(h) == "" {
				t.Errorf("%s: missing %s", path, h)
			}
		}
		if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "image.tmdb.org") {
			t.Errorf("%s: unexpected CSP %q", path, csp)
		}
	}
}

func TestChangingPasswordSignsOtherSessionsOut(t *testing.T) {
	_, base := newSecurityServer(t, "", "")
	first := &http.Client{Jar: mustJar()}
	rawPost(t, first, base+"/api/onboarding/admin", map[string]string{"username": "ryan", "password": "correct-horse-battery-staple"}, nil)
	second := &http.Client{Jar: mustJar()}
	if r := rawPost(t, second, base+"/api/auth/login", map[string]string{"username": "ryan", "password": "correct-horse-battery-staple"}, nil); r.StatusCode != http.StatusOK {
		t.Fatalf("second login: %d", r.StatusCode)
	}
	r := rawPost(t, first, base+"/api/auth/change-password", map[string]string{"currentPassword": "correct-horse-battery-staple", "newPassword": "a-brand-new-passphrase"}, nil)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("change password: %d", r.StatusCode)
	}
	get := func(c *http.Client) int {
		resp, err := c.Get(base + "/api/auth/me")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := get(second); got != http.StatusUnauthorized {
		t.Errorf("the other session still works after the password change: %d", got)
	}
	if got := get(first); got != http.StatusOK {
		t.Errorf("the browser that changed the password was signed out: %d", got)
	}
}

func TestPasswordLengthLimit(t *testing.T) {
	_, base := newSecurityServer(t, "", "")
	long := strings.Repeat("a", 73)
	r := rawPost(t, &http.Client{}, base+"/api/onboarding/admin", map[string]string{"username": "ryan", "password": long}, nil)
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("73-byte password: %d, want 400", r.StatusCode)
	}
}
