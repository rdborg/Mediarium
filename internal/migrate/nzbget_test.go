package migrate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const (
	nzbgetUser = "nzbget"
	nzbgetPass = "nzbget-login-secret"
)

// fakeNZBGet serves NZBGet's JSON-RPC from the fixtures behind basic
// authentication and records every method called.
type fakeNZBGet struct {
	*httptest.Server
	mu      sync.Mutex
	methods []string
}

func newFakeNZBGet(t *testing.T) *fakeNZBGet {
	t.Helper()
	f := &fakeNZBGet{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != nzbgetUser || pass != nzbgetPass {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/jsonrpc" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.methods = append(f.methods, req.Method)
		f.mu.Unlock()
		switch req.Method {
		case "version", "config":
			serveFixture(t, w, "nzbget/"+req.Method+".json")
		default:
			t.Errorf("the importer called NZBGet method %q, which is not read-only", req.Method)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"1.1","id":1,"error":{"code":1,"message":"not allowed"}}`))
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// assertOnlyReadMethods fails unless NZBGet was called, and only with
// methods the importer allows itself.
func (f *fakeNZBGet) assertOnlyReadMethods(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.methods) == 0 {
		t.Fatal("NZBGet was never called")
	}
	for _, m := range f.methods {
		if !nzbgetReadOnly[m] {
			t.Fatalf("the importer called NZBGet method %q", m)
		}
	}
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestNZBGetPreviewAndImport(t *testing.T) {
	h := newHarness(t)
	nz := newFakeNZBGet(t)
	src := Sources{NZBGet: &LoginConn{URL: nz.URL, Username: nzbgetUser, Password: nzbgetPass}}
	ctx := context.Background()

	pv, err := h.im.Preview(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	n := pv.NZBGet
	if n == nil || !n.OK || n.Version != "21.1" || n.Summary != (Summary{Total: 4, Add: 2, Exists: 1, Skip: 1}) {
		t.Fatalf("nzbget preview: %+v", n)
	}
	if n.CompleteDir != "${MainDir}/dst" || len(n.Categories) != 2 {
		t.Fatalf("nzbget folders: %+v", n)
	}
	first, backup, dup, empty := n.Items[0], n.Items[1], n.Items[2], n.Items[3]
	if first.Action != ActionAdd || first.Host != "news.eweka.nl" || first.Port != 563 || !first.SSL || first.Connections != 20 || first.Priority != 0 || !first.Enabled || !first.HasPassword {
		t.Fatalf("first server: %+v", first)
	}
	if backup.Action != ActionAdd || backup.Enabled || backup.Priority != 1 || !backup.Optional || backup.Port != 119 || !strings.Contains(backup.Reason, "switched off in NZBGet") {
		t.Fatalf("backup server: %+v", backup)
	}
	if dup.Action != ActionExists || !strings.Contains(dup.Reason, "same host and username") {
		t.Fatalf("duplicate server: %+v", dup)
	}
	if empty.Action != ActionSkip || empty.Reason != "no usable host name" {
		t.Fatalf("server without a host: %+v", empty)
	}
	if body := mustMarshal(t, pv); strings.Contains(body, "nzbget-eweka-password") || strings.Contains(body, nzbgetPass) {
		t.Fatal("the preview must not echo passwords")
	}
	if list, _ := h.servers.ListAll(); len(list) != 0 {
		t.Fatalf("preview added servers: %+v", list)
	}

	st, err := h.im.Run(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	if st.Step != StepDone || st.Results.UsenetServers != (Counts{Added: 2, Existing: 1, Skipped: 1, Disabled: 1}) || len(st.Errors) != 0 {
		t.Fatalf("nzbget import: %+v errors=%v", st.Results.UsenetServers, st.Errors)
	}
	servers, _ := h.servers.ListAll()
	if len(servers) != 2 || servers[0].Config.Host != "news.eweka.nl" || servers[0].Config.Password != "nzbget-eweka-password" || !servers[0].Enabled ||
		servers[1].Priority != 1 || servers[1].Enabled {
		t.Fatalf("servers: %+v", servers)
	}
	var raw string
	if err := h.db.QueryRow(`SELECT COALESCE(group_concat(password_encrypted, '|'), '') FROM download_clients`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "nzbget-eweka-password") {
		t.Fatal("server password stored in plain text")
	}

	// A second run adds nothing.
	st2, err := h.im.Run(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	if st2.Results.UsenetServers.Added != 0 || st2.Results.UsenetServers.Existing != 3 {
		t.Fatalf("second run: %+v", st2.Results.UsenetServers)
	}
	if list, _ := h.servers.ListAll(); len(list) != 2 {
		t.Fatalf("servers after two runs: %d", len(list))
	}
	nz.assertOnlyReadMethods(t)
	for _, secret := range []string{nzbgetPass, "nzbget-eweka-password", "nzbget-backup-pass"} {
		if strings.Contains(h.logs.String(), secret) {
			t.Fatalf("a secret (%s...) was logged", secret[:6])
		}
	}
}

func TestNZBGetSameServerAsSABnzbdIsAddedOnce(t *testing.T) {
	h := newHarness(t)
	nz := newFakeNZBGet(t)
	src := Sources{
		SABnzbd: &Conn{URL: h.sab.URL, APIKey: sabKey},
		NZBGet:  &LoginConn{URL: nz.URL, Username: nzbgetUser, Password: nzbgetPass},
	}
	pv, err := h.im.Preview(context.Background(), Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	// Both fixtures list news.eweka.nl (with different usernames, so the
	// dedupe is by host and username): only the same pair counts as a copy.
	adds := map[string]int{}
	for _, it := range append(append([]ServerItem{}, pv.SABnzbd.Items...), pv.NZBGet.Items...) {
		if it.Action == ActionAdd {
			adds[strings.ToLower(it.Host)+"|"+it.Username]++
		}
	}
	for k, n := range adds {
		if n != 1 {
			t.Fatalf("%s would be added %d times", k, n)
		}
	}
}

func TestNZBGetErrorsAndRefusedMethods(t *testing.T) {
	h := newHarness(t)
	nz := newFakeNZBGet(t)
	ctx := context.Background()
	tests := []struct {
		name    string
		conn    LoginConn
		wantErr string
	}{
		{"wrong password", LoginConn{URL: nz.URL, Username: nzbgetUser, Password: "nope-secret"}, "the username or password was not accepted"},
		{"not NZBGet", LoginConn{URL: h.radarr.URL, Username: "a", Password: "b"}, "the username or password was not accepted"},
		{"not a web address", LoginConn{URL: "ftp://nas"}, "is not a web address"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pv, err := h.im.Preview(ctx, Options{Sources: Sources{NZBGet: &tt.conn}})
			if err != nil {
				t.Fatal(err)
			}
			if pv.NZBGet.OK || !strings.Contains(pv.NZBGet.Error, tt.wantErr) {
				t.Fatalf("got %+v, want error containing %q", pv.NZBGet.AppStatus, tt.wantErr)
			}
			if strings.Contains(pv.NZBGet.Error, "nope-secret") {
				t.Fatalf("error leaks the password: %s", pv.NZBGet.Error)
			}
		})
	}

	// Methods that change NZBGet are refused before any request is made.
	before := len(nz.methods)
	for _, method := range []string{"editqueue", "append", "saveconfig", "shutdown", "reload", "scan", "history"} {
		var out any
		err := rpc(ctx, h.im.hc, LoginConn{URL: nz.URL, Username: nzbgetUser, Password: nzbgetPass}, method, &out)
		if err == nil || !strings.Contains(err.Error(), "refusing") {
			t.Fatalf("%s was not refused: %v", method, err)
		}
	}
	nz.mu.Lock()
	defer nz.mu.Unlock()
	if len(nz.methods) != before {
		t.Fatalf("a refused method still reached NZBGet: %v", nz.methods)
	}
}
