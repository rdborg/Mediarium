package api_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/store"
)

// newProxyServer is a signed-in test server whose TRUSTED_PROXIES is trusted.
func newProxyServer(t *testing.T, trusted string) (string, *http.Client) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		ConfigDir: filepath.Join(dir, "config"), DownloadsDir: filepath.Join(dir, "downloads"),
		DownloadsIncomplete: filepath.Join(dir, "downloads", "incomplete"), MoviesDir: filepath.Join(dir, "movies"),
		TrustedProxies: trusted, PauseAutomation: true,
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
	httpSrv := httptest.NewServer(server.Routes())
	t.Cleanup(httpSrv.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	return httpSrv.URL, client
}

// meAs calls /api/auth/me with no session, only the given headers.
func meAs(t *testing.T, base string, headers map[string]string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+"/api/auth/me", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestSignInThroughTheProxy(t *testing.T) {
	base, admin := newProxyServer(t, "private")

	// Off by default: the header means nothing.
	if got := meAs(t, base, map[string]string{"Remote-User": "ryan"}); got != http.StatusUnauthorized {
		t.Fatalf("with the setting off the header signed in (%d)", got)
	}
	st := getJSON[map[string]any](t, admin, base+"/api/auth/proxy-signin")
	if st["header"] != "" || st["viaTrustedProxy"] != false || st["from"] != "127.0.0.1" {
		t.Fatalf("state before: %+v", st)
	}

	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/auth/proxy-signin", map[string]string{"header": "Host"}, http.StatusBadRequest)
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/auth/proxy-signin", map[string]string{"header": "Remote User"}, http.StatusBadRequest)
	// The proxy's own address is required, and a whole network is refused.
	for _, bad := range []string{"", "private", "none", "not-an-address", "10.0.0.0/8,private"} {
		postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/auth/proxy-signin", map[string]string{"header": "Remote-User", "addresses": bad}, http.StatusBadRequest)
	}
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/auth/proxy-signin", map[string]string{"header": "Remote-User", "addresses": "127.0.0.1"}, http.StatusOK)

	cases := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"a known account", map[string]string{"Remote-User": "ryan"}, http.StatusOK},
		{"any case", map[string]string{"remote-user": "RYAN"}, http.StatusOK},
		{"an unknown account", map[string]string{"Remote-User": "mallory"}, http.StatusUnauthorized},
		{"no header", nil, http.StatusUnauthorized},
		{"a bad API key is not rescued by the header", map[string]string{"Remote-User": "ryan", "X-API-Key": "nope"}, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		if got := meAs(t, base, tc.headers); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}

	// Switching it off again.
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/auth/proxy-signin", map[string]string{"header": ""}, http.StatusOK)
	if got := meAs(t, base, map[string]string{"Remote-User": "ryan"}); got != http.StatusUnauthorized {
		t.Fatalf("switched off, the header still signed in (%d)", got)
	}
}

func TestProxySignInIgnoresUntrustedCallers(t *testing.T) {
	// TRUSTED_PROXIES trusts every private address, the test client
	// included, but only the proxy named in the setting may sign people in.
	base, admin := newProxyServer(t, "private")
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/auth/proxy-signin", map[string]string{"header": "Remote-User", "addresses": "10.9.8.7"}, http.StatusOK)
	if got := meAs(t, base, map[string]string{"Remote-User": "ryan"}); got != http.StatusUnauthorized {
		t.Fatalf("a caller that isn't the named proxy signed in with the header (%d)", got)
	}
	st := getJSON[map[string]any](t, admin, base+"/api/auth/proxy-signin")
	if st["viaTrustedProxy"] != false {
		t.Fatalf("state: %+v", st)
	}
}

func TestProxySignInCannotBeSwitchedOnWithAnAPIKey(t *testing.T) {
	base, admin := newProxyServer(t, "private")
	key := postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]string{"name": "script"}, http.StatusCreated)
	raw, _ := key["key"].(string)
	if raw == "" {
		t.Fatalf("no key in %+v", key)
	}
	req, _ := http.NewRequest(http.MethodPut, base+"/api/auth/proxy-signin", strings.NewReader(`{"header":"Remote-User"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", raw)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("an API key switched proxy sign-in on (%d)", resp.StatusCode)
	}
}
