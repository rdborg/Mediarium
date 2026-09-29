package mediaservers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePlexTV stands in for plex.tv's PIN and resources API.
type fakePlexTV struct {
	srv       *httptest.Server
	clientID  string
	resources []map[string]any

	mu       sync.Mutex
	approved bool
	pinCalls int
}

const fakeAccountToken = "account-token"

func newFakePlexTV(t *testing.T, clientID string) *fakePlexTV {
	t.Helper()
	f := &fakePlexTV{clientID: clientID}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakePlexTV) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Plex-Client-Identifier") != f.clientID || r.Header.Get("X-Plex-Product") != "Mediarium" {
		http.Error(w, "bad client", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	f.mu.Lock()
	approved := f.approved
	f.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v2/pins":
		if r.URL.Query().Get("strong") != "true" {
			http.Error(w, "want strong", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 4242, "code": "ABCDEFGHIJKLMNOPQRSTUVWXY", "authToken": nil, "expiresIn": 1800})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v2/pins/4242":
		f.mu.Lock()
		f.pinCalls++
		f.mu.Unlock()
		pin := map[string]any{"id": 4242, "code": "ABCDEFGHIJKLMNOPQRSTUVWXY", "authToken": nil, "expiresIn": 1700}
		if approved {
			pin["authToken"] = fakeAccountToken
		}
		_ = json.NewEncoder(w).Encode(pin)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v2/pins/"):
		http.NotFound(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v2/resources":
		if r.Header.Get("X-Plex-Token") != fakeAccountToken {
			http.Error(w, "", http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("includeHttps") != "1" || r.URL.Query().Get("includeRelay") != "0" {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(f.resources)
	default:
		http.NotFound(w, r)
	}
}

func TestPlexPINFlow(t *testing.T) {
	ctx := context.Background()
	tv := newFakePlexTV(t, "install-1")
	tv.resources = []map[string]any{
		{"name": "Living Room", "provides": "server", "clientIdentifier": "m1", "owned": true, "accessToken": "srv-token",
			"connections": []map[string]any{{"protocol": "http", "address": "192.168.1.5", "port": 32400, "uri": "http://192.168.1.5:32400", "local": true}}},
		{"name": "iPhone", "provides": "client,player", "clientIdentifier": "c1"},
		{"name": "Friend's", "provides": "server", "clientIdentifier": "m2", "owned": false, "accessToken": "shared-token"},
	}
	p := &PlexTV{BaseURL: tv.srv.URL, ClientID: "install-1", Version: "1.0"}

	pin, err := p.CreatePIN(ctx)
	if err != nil || pin.ID != 4242 || pin.Code == "" {
		t.Fatalf("create pin: %+v %v", pin, err)
	}
	got, err := p.CheckPIN(ctx, pin.ID)
	if err != nil || got.AuthToken != "" {
		t.Fatalf("check before approval: %+v %v", got, err)
	}
	tv.mu.Lock()
	tv.approved = true
	tv.mu.Unlock()
	got, err = p.CheckPIN(ctx, pin.ID)
	if err != nil || got.AuthToken != fakeAccountToken {
		t.Fatalf("check after approval: %+v %v", got, err)
	}
	if _, err := p.CheckPIN(ctx, 1); !errors.Is(err, ErrPlexPINExpired) {
		t.Fatalf("unknown pin: %v", err)
	}
	servers, err := p.Servers(ctx, got.AuthToken)
	if err != nil || len(servers) != 2 || servers[0].ClientIdentifier != "m1" || servers[0].AccessToken != "srv-token" || servers[1].Owned {
		t.Fatalf("servers: %+v %v", servers, err)
	}
	if _, err := p.Servers(ctx, "wrong"); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("wrong account token: %v", err)
	}
	// A different client id is a different app to plex.tv.
	other := &PlexTV{BaseURL: tv.srv.URL, ClientID: "someone-else"}
	if _, err := other.CreatePIN(ctx); err == nil {
		t.Fatal("pin with another client id should fail")
	}
}

func TestPlexTVUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	_, err := (&PlexTV{BaseURL: u, ClientID: "x", HTTP: &http.Client{Timeout: 2 * time.Second}}).CreatePIN(context.Background())
	var ue *UserError
	if !errors.As(err, &ue) || !strings.Contains(err.Error(), "Could not reach plex.tv") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlexAuthURL(t *testing.T) {
	got := PlexAuthURL("id 1", "CODE")
	want := "https://app.plex.tv/auth#?clientID=id+1&code=CODE&context%5Bdevice%5D%5Bproduct%5D=Mediarium"
	if got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

func TestPlexCandidates(t *testing.T) {
	r := PlexResource{Connections: []PlexConnection{
		{Protocol: "https", Address: "81.2.3.4", Port: 32400, URI: "https://81-2-3-4.abc.plex.direct:32400", Local: false},
		{Protocol: "https", Address: "192.168.1.5", Port: 32400, URI: "https://192-168-1-5.abc.plex.direct:32400", Local: true},
		{Protocol: "https", Address: "10.0.0.9", Port: 32400, URI: "https://relay.plex.direct:8443", Relay: true},
	}}
	cases := []struct {
		name      string
		preferred string
		https     bool
		want      []string
	}{
		{"local first", "", false, []string{"https://192-168-1-5.abc.plex.direct:32400", "http://192.168.1.5:32400", "https://81-2-3-4.abc.plex.direct:32400"}},
		{"preferred first", "https://81-2-3-4.abc.plex.direct:32400/", false, []string{"https://81-2-3-4.abc.plex.direct:32400", "https://192-168-1-5.abc.plex.direct:32400", "http://192.168.1.5:32400"}},
		{"unknown preferred ignored", "http://evil.example", false, []string{"https://192-168-1-5.abc.plex.direct:32400", "http://192.168.1.5:32400", "https://81-2-3-4.abc.plex.direct:32400"}},
		{"https required: no plain http", "", true, []string{"https://192-168-1-5.abc.plex.direct:32400", "https://81-2-3-4.abc.plex.direct:32400"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r.HTTPSRequired = tc.https
			if got := r.Candidates(tc.preferred); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q\nwant %q", got, tc.want)
			}
		})
	}
	r.HTTPSRequired = false
	if !r.HasAddress("http://192.168.1.5:32400/") || r.HasAddress("https://relay.plex.direct:8443") {
		t.Fatal("HasAddress")
	}
}

func TestConnectPlex(t *testing.T) {
	plex := newFakePlex(t, "srv-token")
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	r := PlexResource{Name: "Living Room", ClientIdentifier: "abc123machine", AccessToken: "srv-token", Connections: []PlexConnection{
		{URI: deadURL, Local: true},
		{URI: plex.srv.URL, Local: true},
	}}
	c := NewClient("")
	s, res, err := c.ConnectPlex(context.Background(), r, deadURL)
	if err != nil || s.BaseURL != plex.srv.URL || s.Token != "srv-token" || s.ServerID != "abc123machine" || s.Name != "Living Room" || res.ServerName != "Living Room" {
		t.Fatalf("connect: %+v %+v %v", s, res, err)
	}
	r.AccessToken = "wrong"
	if _, _, err := c.ConnectPlex(context.Background(), r, ""); err == nil || !strings.Contains(err.Error(), "Could not connect to \"Living Room\"") {
		t.Fatalf("wrong token: %v", err)
	}
	r.AccessToken = ""
	if _, _, err := c.ConnectPlex(context.Background(), r, ""); err == nil || !strings.Contains(err.Error(), "did not give Mediarium access") {
		t.Fatalf("no token: %v", err)
	}
}

func TestPending(t *testing.T) {
	now := time.Unix(1000, 0)
	p := &Pending[string]{TTL: time.Minute, Now: func() time.Time { return now }, Max: 2}
	p.Put("a", "1")
	if v, ok := p.Get("a"); !ok || v != "1" {
		t.Fatalf("get: %q %v", v, ok)
	}
	now = now.Add(time.Second)
	p.Put("b", "2")
	now = now.Add(time.Second)
	p.Put("c", "3") // over Max: the oldest ("a") goes
	if _, ok := p.Get("a"); ok {
		t.Fatal("oldest entry should have been dropped")
	}
	if v, ok := p.Take("b"); !ok || v != "2" {
		t.Fatalf("take: %q %v", v, ok)
	}
	if _, ok := p.Take("b"); ok {
		t.Fatal("take twice")
	}
	now = now.Add(2 * time.Minute)
	if _, ok := p.Get("c"); ok {
		t.Fatal("expired entry returned")
	}
	id1, _ := NewID()
	id2, _ := NewID()
	if len(id1) != 32 || id1 == id2 {
		t.Fatalf("ids %q %q", id1, id2)
	}
}
