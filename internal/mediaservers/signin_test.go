package mediaservers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeAuthServer stands in for Jellyfin or Emby signing people in.
type fakeAuthServer struct {
	srv     *httptest.Server
	kind    Kind
	user    string
	pass    string
	admin   bool
	qc      string // "on" | "off" | "missing" (no Quick Connect at all)
	getInit bool   // Initiate only answers GET (Jellyfin before 10.8)

	mu       sync.Mutex
	approved bool
	headers  []string // Authorization headers seen
}

func newFakeAuthServer(t *testing.T, kind Kind) *fakeAuthServer {
	t.Helper()
	f := &fakeAuthServer{kind: kind, user: "ryan", pass: "s3cret", admin: true, qc: "on"}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAuthServer) approve() {
	f.mu.Lock()
	f.approved = true
	f.mu.Unlock()
}

func (f *fakeAuthServer) result(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"AccessToken": "access-" + string(f.kind), "ServerId": "srv-" + string(f.kind),
		"User": map[string]any{"Name": f.user, "Policy": map[string]any{"IsAdministrator": f.admin}},
	})
}

func (f *fakeAuthServer) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.headers = append(f.headers, r.Header.Get("Authorization"))
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	qcPath := strings.HasPrefix(r.URL.Path, "/QuickConnect/") || r.URL.Path == "/Users/AuthenticateWithQuickConnect"
	if qcPath && f.qc == "missing" {
		http.NotFound(w, r)
		return
	}
	switch {
	case r.URL.Path == "/System/Info/Public":
		info := map[string]any{"ServerName": "Den " + f.kind.Label(), "Version": "4.8.0", "Id": "srv-" + string(f.kind)}
		if f.kind == KindJellyfin {
			info["Version"], info["ProductName"] = "10.10.3", "Jellyfin Server"
		}
		_ = json.NewEncoder(w).Encode(info)
	case r.URL.Path == "/Users/AuthenticateByName" && r.Method == http.MethodPost:
		var body struct{ Username, Pw string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Username != f.user || body.Pw != f.pass {
			http.Error(w, "Invalid username or password entered.", http.StatusUnauthorized)
			return
		}
		f.result(w)
	case r.URL.Path == "/QuickConnect/Enabled":
		_ = json.NewEncoder(w).Encode(f.qc == "on")
	case r.URL.Path == "/QuickConnect/Initiate":
		if f.getInit != (r.Method == http.MethodGet) {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if f.qc != "on" {
			http.Error(w, "", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Secret": "qc-secret", "Code": "123456", "Authenticated": false})
	case r.URL.Path == "/QuickConnect/Connect":
		if r.URL.Query().Get("secret") != "qc-secret" {
			http.NotFound(w, r)
			return
		}
		f.mu.Lock()
		ok := f.approved
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"Secret": "qc-secret", "Code": "123456", "Authenticated": ok})
	case r.URL.Path == "/Users/AuthenticateWithQuickConnect" && r.Method == http.MethodPost:
		var body struct{ Secret string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		ok := f.approved
		f.mu.Unlock()
		if body.Secret != "qc-secret" || !ok {
			http.Error(w, "", http.StatusUnauthorized)
			return
		}
		f.result(w)
	default:
		http.NotFound(w, r)
	}
}

func TestSignIn(t *testing.T) {
	cases := []struct {
		name     string
		kind     Kind   // server's real kind
		chosen   Kind   // what the person picked
		user     string // default the right one
		pass     string
		admin    bool
		wantErr  string
		wantName string
	}{
		{name: "jellyfin ok", kind: KindJellyfin, chosen: KindJellyfin, admin: true, wantName: "Den Jellyfin"},
		{name: "emby ok", kind: KindEmby, chosen: KindEmby, admin: true, wantName: "Den Emby"},
		{name: "wrong password", kind: KindJellyfin, chosen: KindJellyfin, pass: "nope", admin: true, wantErr: "Wrong username or password"},
		{name: "wrong user", kind: KindEmby, chosen: KindEmby, user: "someone", admin: true, wantErr: "Wrong username or password"},
		{name: "not an administrator", kind: KindJellyfin, chosen: KindJellyfin, wantErr: "not an administrator"},
		{name: "emby chosen for jellyfin", kind: KindJellyfin, chosen: KindEmby, admin: true, wantErr: "This is a Jellyfin server, not Emby"},
		{name: "jellyfin chosen for emby", kind: KindEmby, chosen: KindJellyfin, admin: true, wantErr: "This is an Emby server, not Jellyfin"},
		{name: "plex refused", kind: KindJellyfin, chosen: KindPlex, admin: true, wantErr: "Sign in with Plex"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeAuthServer(t, tc.kind)
			f.admin = tc.admin
			user, pass := f.user, f.pass
			if tc.user != "" {
				user = tc.user
			}
			if tc.pass != "" {
				pass = tc.pass
			}
			c := NewClient("1.2.3")
			s, err := c.SignIn(context.Background(), tc.chosen, f.srv.URL+"/", "install-id", user, pass)
			if tc.wantErr != "" {
				var ue *UserError
				if err == nil || !errors.As(err, &ue) || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want a UserError containing %q", err, tc.wantErr)
				}
				if strings.Contains(err.Error(), pass) {
					t.Fatalf("the error repeats the password: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("sign in: %v", err)
			}
			if s.Token != "access-"+string(tc.kind) || s.Name != tc.wantName || s.BaseURL != f.srv.URL ||
				s.ServerID != "srv-"+string(tc.kind) || !s.Enabled || !s.RefreshAfterImport || s.Kind != tc.kind {
				t.Fatalf("server = %+v", s)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			last := f.headers[len(f.headers)-1]
			if !strings.Contains(last, `DeviceId="install-id"`) || !strings.Contains(last, `Client="Mediarium"`) || !strings.Contains(last, `Version="1.2.3"`) {
				t.Fatalf("authorization header = %q", last)
			}
		})
	}
}

func TestSignInUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close()
	_, err := NewClient("").SignIn(context.Background(), KindJellyfin, addr, "id", "u", "p")
	if err == nil || !strings.Contains(err.Error(), "Could not reach Jellyfin") {
		t.Fatalf("err = %v", err)
	}
}

func TestQuickConnect(t *testing.T) {
	ctx := context.Background()
	for _, getInit := range []bool{false, true} {
		f := newFakeAuthServer(t, KindJellyfin)
		f.getInit = getInit
		c := NewClient("1.0")
		base, code, secret, err := c.QuickConnectStart(ctx, f.srv.URL, "install-id")
		if err != nil || code != "123456" || secret != "qc-secret" || base != f.srv.URL {
			t.Fatalf("start (GET initiate %v): base=%q code=%q secret=%q err=%v", getInit, base, code, secret, err)
		}
		if ok, err := c.QuickConnectCheck(ctx, base, "install-id", secret); ok || err != nil {
			t.Fatalf("check before approval: %v %v", ok, err)
		}
		if _, err := c.QuickConnectFinish(ctx, base, "install-id", secret); err == nil {
			t.Fatal("finishing before approval should fail")
		}
		f.approve()
		if ok, err := c.QuickConnectCheck(ctx, base, "install-id", secret); !ok || err != nil {
			t.Fatalf("check after approval: %v %v", ok, err)
		}
		s, err := c.QuickConnectFinish(ctx, base, "install-id", secret)
		if err != nil || s.Token != "access-jellyfin" || s.Kind != KindJellyfin || s.ServerID != "srv-jellyfin" {
			t.Fatalf("finish: %+v %v", s, err)
		}
		if _, err := c.QuickConnectCheck(ctx, base, "install-id", "other"); !errors.Is(err, ErrQuickConnectExpired) {
			t.Fatalf("unknown secret: %v", err)
		}
	}
}

func TestQuickConnectUnavailable(t *testing.T) {
	cases := []struct {
		name string
		kind Kind
		qc   string
		want string
	}{
		{"turned off", KindJellyfin, "off", "Dashboard, General, Quick Connect"},
		{"old jellyfin", KindJellyfin, "missing", "no Quick Connect"},
		{"emby", KindEmby, "on", "Quick Connect is a Jellyfin feature"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeAuthServer(t, tc.kind)
			f.qc = tc.qc
			_, _, _, err := NewClient("").QuickConnectStart(context.Background(), f.srv.URL, "id")
			var ue *UserError
			if !errors.As(err, &ue) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPublicInfoKind(t *testing.T) {
	cases := []struct {
		info embyPublicInfo
		want Kind
	}{
		{embyPublicInfo{ProductName: "Jellyfin Server", Version: "10.9.0"}, KindJellyfin},
		{embyPublicInfo{Version: "10.8.13"}, KindJellyfin},
		{embyPublicInfo{Version: "4.8.10.0"}, KindEmby},
		{embyPublicInfo{ProductName: "Emby Server", Version: "4.9.0"}, KindEmby},
	}
	for _, tc := range cases {
		if got := tc.info.kind(); got != tc.want {
			t.Errorf("%+v: kind %q, want %q", tc.info, got, tc.want)
		}
	}
}
