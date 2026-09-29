package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// rawRequest sends body as JSON (nil sends nothing) and returns the status
// and the raw answer, so tests can check what never appears in it.
func rawRequest(t *testing.T, client *http.Client, method, target string, body any) (int, string) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, target, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func decodeInto(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return out
}

// fakePlexTVService stands in for plex.tv: PIN 77 is approved once
// approve() is called, and the account has one server reachable at
// serverURL with the token "plex-token" (and a remote address, deadURL,
// where nothing answers).
type fakePlexTVService struct {
	*httptest.Server
	mu        sync.Mutex
	approved  bool
	clientIDs map[string]bool
}

func newFakePlexTVService(t *testing.T, serverURL, deadURL string) *fakePlexTVService {
	t.Helper()
	f := &fakePlexTVService{clientIDs: map[string]bool{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.clientIDs[r.Header.Get("X-Plex-Client-Identifier")] = true
		approved := f.approved
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/pins":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 77, "code": "PINCODE", "expiresIn": 1800})
		case r.URL.Path == "/api/v2/pins/77":
			pin := map[string]any{"id": 77, "code": "PINCODE", "authToken": nil}
			if approved {
				pin["authToken"] = "account-secret-token"
			}
			_ = json.NewEncoder(w).Encode(pin)
		case r.URL.Path == "/api/v2/resources":
			if r.Header.Get("X-Plex-Token") != "account-secret-token" {
				http.Error(w, "", http.StatusUnauthorized)
				return
			}
			u, _ := url.Parse(serverURL)
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"name": "Living Room", "provides": "server", "clientIdentifier": "plex-machine-1", "owned": true,
				"productVersion": "1.41.0", "accessToken": "plex-token",
				"connections": []map[string]any{
					{"protocol": "http", "address": "127.0.0.1", "port": 0, "uri": deadURL, "local": false},
					{"protocol": "http", "address": u.Hostname(), "port": 0, "uri": serverURL, "local": true},
				},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakePlexTVService) approve() {
	f.mu.Lock()
	f.approved = true
	f.mu.Unlock()
}

func TestSignInWithPlex(t *testing.T) {
	server, base, admin, member, _ := familyServer(t)
	plex := newFakePlexServer(t, "plex-token")
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	tv := newFakePlexTVService(t, plex.URL, deadURL)
	server.TestSetPlexTVURL(tv.URL)
	api := base + "/api/media-servers/plex/pin"

	if code, _ := doStatus(t, member, http.MethodPost, api); code != http.StatusForbidden {
		t.Fatalf("member start: %d", code)
	}

	start := postJSON[map[string]any](t, admin, api, nil, http.StatusOK)
	authURL, _ := start["authUrl"].(string)
	if start["pinId"] != float64(77) || start["code"] != "PINCODE" ||
		!strings.HasPrefix(authURL, "https://app.plex.tv/auth#?clientID=mediarium-") || !strings.Contains(authURL, "code=PINCODE") {
		t.Fatalf("start: %+v", start)
	}
	// The same install id every time.
	again := postJSON[map[string]any](t, admin, api, nil, http.StatusOK)
	if again["authUrl"] != authURL || len(tv.clientIDs) != 1 {
		t.Fatalf("client id changed: %v then %v (%v)", authURL, again["authUrl"], tv.clientIDs)
	}

	poll := postJSONMethod[map[string]any](t, admin, http.MethodGet, api+"/77", nil, http.StatusOK)
	if poll["done"] != false {
		t.Fatalf("poll before approval: %+v", poll)
	}
	if code, _ := doStatus(t, admin, http.MethodGet, api+"/12345"); code != http.StatusNotFound {
		t.Fatalf("unknown pin: %d", code)
	}
	if code, body := rawRequest(t, admin, http.MethodPost, api+"/77/add", map[string]any{"machineIdentifier": "plex-machine-1"}); code != http.StatusConflict {
		t.Fatalf("add before approval: %d %s", code, body)
	}

	tv.approve()
	code, raw := rawRequest(t, admin, http.MethodGet, api+"/77", nil)
	if code != http.StatusOK || strings.Contains(raw, "plex-token") || strings.Contains(raw, "account-secret-token") {
		t.Fatalf("poll after approval leaked a token or failed: %d %s", code, raw)
	}
	poll = decodeInto(t, raw)
	servers, _ := poll["servers"].([]any)
	if poll["done"] != true || len(servers) != 1 {
		t.Fatalf("poll: %+v", poll)
	}
	first := servers[0].(map[string]any)
	conns, _ := first["connections"].([]any)
	if first["machineIdentifier"] != "plex-machine-1" || first["owned"] != true || first["alreadyAdded"] != false || len(conns) != 2 ||
		conns[0].(map[string]any)["uri"] != plex.URL || conns[0].(map[string]any)["local"] != true {
		t.Fatalf("server: %+v", first)
	}

	// Adding: an address that isn't the server's is refused; the remote one
	// doesn't answer, so the local one is used.
	if code, body := rawRequest(t, admin, http.MethodPost, api+"/77/add", map[string]any{"machineIdentifier": "plex-machine-1", "uri": "http://10.1.1.1:32400"}); code != http.StatusBadRequest {
		t.Fatalf("foreign uri: %d %s", code, body)
	}
	if code, body := rawRequest(t, admin, http.MethodPost, api+"/77/add", map[string]any{"machineIdentifier": "nope"}); code != http.StatusNotFound {
		t.Fatalf("unknown server: %d %s", code, body)
	}
	code, raw = rawRequest(t, admin, http.MethodPost, api+"/77/add", map[string]any{"machineIdentifier": "plex-machine-1", "uri": deadURL})
	if code != http.StatusCreated || strings.Contains(raw, "plex-token") {
		t.Fatalf("add: %d %s", code, raw)
	}
	added := decodeInto(t, raw)
	if added["baseUrl"] != plex.URL || added["hasToken"] != true || added["machineIdentifier"] != "plex-machine-1" ||
		added["name"] != "Living Room" || added["lastError"] != "" || added["kind"] != "plex" {
		t.Fatalf("added: %+v", added)
	}
	// Adding it again replaces the token instead of adding a second copy.
	again = postJSON[map[string]any](t, admin, api+"/77/add", map[string]any{"machineIdentifier": "plex-machine-1"}, http.StatusOK)
	if again["id"] != added["id"] {
		t.Fatalf("re-add: %+v", again)
	}
	list := getJSON[[]map[string]any](t, admin, base+"/api/media-servers")
	if len(list) != 1 {
		t.Fatalf("list: %+v", list)
	}
	poll = postJSONMethod[map[string]any](t, admin, http.MethodGet, api+"/77", nil, http.StatusOK)
	if poll["servers"].([]any)[0].(map[string]any)["alreadyAdded"] != true {
		t.Fatalf("alreadyAdded after adding: %+v", poll)
	}
}

// fakeJellyfinLogin is a Jellyfin server that signs "ryan"/"pw" in (and
// Quick Connect once approved) and accepts the token it hands out.
type fakeJellyfinLogin struct {
	*httptest.Server
	mu       sync.Mutex
	approved bool
	bodies   []string
}

func newFakeJellyfinLogin(t *testing.T) *fakeJellyfinLogin {
	t.Helper()
	f := &fakeJellyfinLogin{}
	auth := func(w http.ResponseWriter) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"AccessToken": "jf-access", "ServerId": "jf-1",
			"User": map[string]any{"Name": "ryan", "Policy": map[string]any{"IsAdministrator": true}},
		})
	}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, string(b))
		approved := f.approved
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/System/Info/Public":
			_ = json.NewEncoder(w).Encode(map[string]any{"ServerName": "Den", "Version": "10.10.3", "Id": "jf-1", "ProductName": "Jellyfin Server"})
		case "/Users/AuthenticateByName":
			var body struct{ Username, Pw string }
			_ = json.Unmarshal(b, &body)
			if body.Username != "ryan" || body.Pw != "pw" {
				http.Error(w, "", http.StatusUnauthorized)
				return
			}
			auth(w)
		case "/QuickConnect/Enabled":
			_ = json.NewEncoder(w).Encode(true)
		case "/QuickConnect/Initiate":
			_ = json.NewEncoder(w).Encode(map[string]any{"Secret": "the-secret", "Code": "654321"})
		case "/QuickConnect/Connect":
			if r.URL.Query().Get("secret") != "the-secret" {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"Secret": "the-secret", "Code": "654321", "Authenticated": approved})
		case "/Users/AuthenticateWithQuickConnect":
			if !approved {
				http.Error(w, "", http.StatusUnauthorized)
				return
			}
			auth(w)
		default: // everything else needs the token
			if r.Header.Get("X-Emby-Token") != "jf-access" {
				http.Error(w, "", http.StatusUnauthorized)
				return
			}
			switch r.URL.Path {
			case "/System/Info":
				_ = json.NewEncoder(w).Encode(map[string]any{"ServerName": "Den", "Version": "10.10.3", "Id": "jf-1", "ProductName": "Jellyfin Server"})
			case "/Library/VirtualFolders":
				_ = json.NewEncoder(w).Encode([]any{})
			default:
				http.NotFound(w, r)
			}
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func TestMediaServerLogin(t *testing.T) {
	_, base, admin, member, _ := familyServer(t)
	jf := newFakeJellyfinLogin(t)
	api := base + "/api/media-servers/login"

	if code, _ := doStatus(t, member, http.MethodPost, api); code != http.StatusForbidden {
		t.Fatalf("member: %d", code)
	}
	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{"wrong password", map[string]any{"kind": "jellyfin", "baseUrl": jf.URL, "username": "ryan", "password": "nope"}, "Wrong username or password"},
		{"wrong kind", map[string]any{"kind": "emby", "baseUrl": jf.URL, "username": "ryan", "password": "pw"}, "This is a Jellyfin server"},
		{"plex", map[string]any{"kind": "plex", "baseUrl": jf.URL, "username": "ryan", "password": "pw"}, "Sign in with Plex"},
		{"no kind", map[string]any{"baseUrl": jf.URL, "username": "ryan", "password": "pw"}, "server type"},
		{"unreachable", map[string]any{"kind": "jellyfin", "baseUrl": "http://127.0.0.1:1", "username": "ryan", "password": "pw"}, "Could not reach Jellyfin"},
	}
	for _, tc := range cases {
		code, raw := rawRequest(t, admin, http.MethodPost, api, tc.body)
		if code != http.StatusBadRequest || !strings.Contains(raw, tc.want) {
			t.Errorf("%s: %d %s", tc.name, code, raw)
		}
	}

	code, raw := rawRequest(t, admin, http.MethodPost, api, map[string]any{"kind": "jellyfin", "baseUrl": jf.URL + "/", "username": "ryan", "password": "pw"})
	if code != http.StatusCreated || strings.Contains(raw, "jf-access") || strings.Contains(raw, `"pw"`) {
		t.Fatalf("login: %d %s", code, raw)
	}
	saved := decodeInto(t, raw)
	if saved["name"] != "Den" || saved["kind"] != "jellyfin" || saved["baseUrl"] != jf.URL || saved["hasToken"] != true ||
		saved["machineIdentifier"] != "jf-1" || saved["lastError"] != "" {
		t.Fatalf("saved: %+v", saved)
	}
	// The saved token works: a test of the saved server passes.
	res := postJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/media-servers/%v/test", base, saved["id"]), nil, http.StatusOK)
	if res["ok"] != true {
		t.Fatalf("test saved: %+v", res)
	}
	// Discovery marks it as added... and refuses public networks.
	code, raw = rawRequest(t, admin, http.MethodPost, base+"/api/media-servers/discover", map[string]any{"subnets": []string{"8.8.8.0/24"}})
	if code != http.StatusBadRequest || !strings.Contains(raw, "not a private network") {
		t.Fatalf("public subnet: %d %s", code, raw)
	}
}

func TestDiscoverRefusesBadNetworks(t *testing.T) {
	_, base, admin, member, _ := familyServer(t)
	api := base + "/api/media-servers/discover"
	if code, _ := doStatus(t, member, http.MethodPost, api); code != http.StatusForbidden {
		t.Fatalf("member: %d", code)
	}
	for name, subnets := range map[string][]string{
		"public":       {"192.168.1.0/24", "1.1.1.0/24"},
		"too large":    {"10.0.0.0/8"},
		"not an ip":    {"my-lan"},
		"too many":     {"10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24", "10.0.4.0/24", "10.0.5.0/24", "10.0.6.0/24", "10.0.7.0/24", "10.0.8.0/24", "10.0.9.0/24"},
		"loopback":     {"127.0.0.1"},
		"whole world":  {"0.0.0.0/0"},
		"ipv6 private": {"fd00::1"},
	} {
		code, raw := rawRequest(t, admin, http.MethodPost, api, map[string]any{"subnets": subnets})
		if code != http.StatusBadRequest || !strings.Contains(raw, "error") {
			t.Errorf("%s: %d %s", name, code, raw)
		}
	}
}

func TestJellyfinQuickConnect(t *testing.T) {
	_, base, admin, _, _ := familyServer(t)
	jf := newFakeJellyfinLogin(t)
	api := base + "/api/media-servers/jellyfin/quickconnect"

	code, raw := rawRequest(t, admin, http.MethodPost, api, map[string]any{"baseUrl": jf.URL})
	if code != http.StatusOK || strings.Contains(raw, "the-secret") {
		t.Fatalf("start: %d %s", code, raw)
	}
	start := decodeInto(t, raw)
	id, _ := start["id"].(string)
	if start["code"] != "654321" || len(id) != 32 {
		t.Fatalf("start: %+v", start)
	}
	poll := postJSONMethod[map[string]any](t, admin, http.MethodGet, api+"/"+id, nil, http.StatusOK)
	if poll["done"] != false || poll["code"] != "654321" {
		t.Fatalf("poll before approval: %+v", poll)
	}
	jf.mu.Lock()
	jf.approved = true
	jf.mu.Unlock()
	code, raw = rawRequest(t, admin, http.MethodGet, api+"/"+id, nil)
	if code != http.StatusOK || strings.Contains(raw, "jf-access") || strings.Contains(raw, "the-secret") {
		t.Fatalf("poll after approval: %d %s", code, raw)
	}
	poll = decodeInto(t, raw)
	srv, _ := poll["server"].(map[string]any)
	if poll["done"] != true || srv["kind"] != "jellyfin" || srv["hasToken"] != true || srv["machineIdentifier"] != "jf-1" {
		t.Fatalf("poll: %+v", poll)
	}
	// The sign-in is used up.
	if code, _ := doStatus(t, admin, http.MethodGet, api+"/"+id); code != http.StatusNotFound {
		t.Fatalf("poll again: %d", code)
	}
	if code, _ := doStatus(t, admin, http.MethodGet, api+"/unknown"); code != http.StatusNotFound {
		t.Fatalf("unknown id: %d", code)
	}
}
