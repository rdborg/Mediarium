package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// setupRequest posts the first-run sign-up. host, when set, replaces the Host
// header the way a browser using another name would.
func setupRequest(t *testing.T, base, host string, headers map[string]string, body map[string]string) (int, string) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, base+"/api/onboarding/admin", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if host != "" {
		req.Host = host
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	return resp.StatusCode, out.String()
}

func setupStatus(t *testing.T, base string, headers map[string]string) (needed, codeRequired bool) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+"/api/onboarding/status", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st struct {
		FirstRunNeeded    bool `json:"firstRunNeeded"`
		SetupCodeRequired bool `json:"setupCodeRequired"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	return st.FirstRunNeeded, st.SetupCodeRequired
}

var goodAdmin = map[string]string{"username": "ryan", "password": "correct-horse-battery-staple"}

func withCode(code string) map[string]string {
	m := map[string]string{}
	for k, v := range goodAdmin {
		m[k] = v
	}
	m["setupCode"] = code
	return m
}

// From the home network, the first account is made without a code.
func TestFirstRunFromHomeNeedsNoCode(t *testing.T) {
	_, base := newSecurityServer(t, "", "")
	if needed, code := setupStatus(t, base, nil); !needed || code {
		t.Fatalf("status from home: firstRunNeeded=%v setupCodeRequired=%v, want true/false", needed, code)
	}
	// A private caller behind a proxy counts as home too.
	if status, body := setupRequest(t, base, "", map[string]string{"X-Forwarded-For": "192.168.1.50"}, goodAdmin); status != http.StatusCreated {
		t.Fatalf("home sign-up: %d %s", status, body)
	}
}

// From the internet (a public address behind the trusted proxy) the code is
// needed; without it, or with a wrong one, nothing is created.
func TestFirstRunFromInternetNeedsCode(t *testing.T) {
	server, base := newSecurityServer(t, "", "")
	remote := map[string]string{"X-Forwarded-For": "198.51.100.7"}
	if needed, code := setupStatus(t, base, remote); !needed || !code {
		t.Fatalf("status from the internet: firstRunNeeded=%v setupCodeRequired=%v, want true/true", needed, code)
	}
	if status, _ := setupRequest(t, base, "", remote, goodAdmin); status != http.StatusForbidden {
		t.Fatalf("no code: %d, want 403", status)
	}
	if status, body := setupRequest(t, base, "", remote, withCode("AAAA-AAAA-AAAA")); status != http.StatusForbidden || !strings.Contains(body, "setup code") {
		t.Fatalf("wrong code: %d %s, want 403 mentioning the setup code", status, body)
	}
	if needed, _ := setupStatus(t, base, nil); !needed {
		t.Fatal("a refused sign-up must not create the account")
	}
	// The code from the log works, typed in lower case and without dashes too.
	code := strings.ToLower(strings.ReplaceAll(server.TestSetupCode(), "-", ""))
	if status, body := setupRequest(t, base, "", remote, withCode(code)); status != http.StatusCreated {
		t.Fatalf("right code: %d %s", status, body)
	}
	// After that the sign-up is closed for good, code or not.
	if status, _ := setupRequest(t, base, "", remote, withCode(server.TestSetupCode())); status != http.StatusConflict {
		t.Fatalf("second sign-up: %d, want 409", status)
	}
	if _, codeReq := setupStatus(t, base, remote); codeReq {
		t.Fatal("no code is asked for once an account exists")
	}
}

// Requests that only look like they come from home still need the code.
func TestFirstRunSpoofingHomeDoesNotSkipTheCode(t *testing.T) {
	tests := []struct {
		name    string
		trusted string
		host    string
		headers map[string]string
	}{
		{"public name for a private address (DNS rebinding)", "", "evil.example.com", nil},
		{"public name with a port", "", "evil.example.com:8264", nil},
		{"proxy passed the request on without the caller's address", "", "", map[string]string{"X-Forwarded-Proto": "https"}},
		{"forwarded host without the caller's address", "", "", map[string]string{"X-Forwarded-Host": "media.example.com"}},
		{"caller invented a private X-Forwarded-For but X-Real-IP is public", "", "", map[string]string{"X-Forwarded-For": "192.168.1.5", "X-Real-IP": "198.51.100.7"}},
		{"private entry on the right, public on the left is still a stranger behind a proxy", "", "", map[string]string{"X-Forwarded-For": "198.51.100.7, 192.168.1.5"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, base := newSecurityServer(t, tt.trusted, "")
			if status, body := setupRequest(t, base, tt.host, tt.headers, goodAdmin); status != http.StatusForbidden {
				t.Fatalf("got %d %s, want 403", status, body)
			}
		})
	}
}

// With no trusted proxy, forwarding headers mean nothing: the caller is
// whoever connected.
func TestFirstRunIgnoresForwardingHeadersFromStrangers(t *testing.T) {
	_, base := newSecurityServer(t, "none", "")
	// The connection is from 127.0.0.1 (home): the header cannot make it less so...
	if status, body := setupRequest(t, base, "", map[string]string{"X-Forwarded-For": "198.51.100.7"}, goodAdmin); status != http.StatusCreated {
		t.Fatalf("got %d %s, want 201", status, body)
	}
}

// Guessing the code is slowed down like guessing a password.
func TestFirstRunCodeGuessesAreLimited(t *testing.T) {
	server, base := newSecurityServer(t, "", "")
	remote := map[string]string{"X-Forwarded-For": "198.51.100.7"}
	for i := 0; i < 5; i++ {
		if status, _ := setupRequest(t, base, "", remote, withCode("BBBB-BBBB-BBBB")); status != http.StatusForbidden {
			t.Fatalf("guess %d: %d, want 403", i, status)
		}
	}
	// Even the right code is refused for now.
	if status, _ := setupRequest(t, base, "", remote, withCode(server.TestSetupCode())); status != http.StatusTooManyRequests {
		t.Fatalf("after five wrong guesses: %d, want 429", status)
	}
}

// The pages that need no sign-in do not read megabytes from strangers.
func TestPublicPagesTakeOnlySmallBodies(t *testing.T) {
	_, base := newSecurityServer(t, "", "")
	huge := map[string]string{"username": "ryan", "password": strings.Repeat("x", 200<<10)}
	if status, _ := setupRequest(t, base, "", nil, huge); status != http.StatusBadRequest {
		t.Errorf("sign-up with a 200 KB body: %d, want 400", status)
	}
	if r := rawPost(t, &http.Client{}, base+"/api/auth/login", huge, nil); r.StatusCode != http.StatusBadRequest {
		t.Errorf("sign-in with a 200 KB body: %d, want 400", r.StatusCode)
	}
}
