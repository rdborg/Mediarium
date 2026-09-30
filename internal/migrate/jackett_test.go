package migrate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
)

const jackettKey = "jackett-fixture-key-0007"

// fakeJackett serves the configured indexers list, checking the apikey
// query parameter, and records every request.
type fakeJackett struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
}

func newFakeJackett(t *testing.T) *fakeJackett {
	t.Helper()
	f := &fakeJackett{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		if r.Header.Get("X-Api-Key") != "" {
			t.Errorf("Jackett takes its key in the query, not a header")
		}
		if r.URL.Query().Get("apikey") != jackettKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/api/v2.0/indexers" || r.URL.Query().Get("configured") != "true" {
			http.NotFound(w, r)
			return
		}
		serveFixture(t, w, "jackett/indexers.json")
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeJackett) assertOnlyGET(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		t.Fatal("Jackett was never called")
	}
	for _, r := range f.requests {
		if !strings.HasPrefix(r, "GET ") {
			t.Fatalf("the importer sent %q to Jackett", r)
		}
	}
}

func jackettItem(t *testing.T, items []IndexerItem, id string) IndexerItem {
	t.Helper()
	for _, it := range items {
		if it.SourceID == id {
			return it
		}
	}
	t.Fatalf("no Jackett indexer %q in %+v", id, items)
	return IndexerItem{}
}

func TestJackettDefaultsToTheTorznabFeed(t *testing.T) {
	h := newHarness(t)
	jk := newFakeJackett(t)
	src := Sources{Jackett: &JackettConn{Conn: Conn{URL: jk.URL + "/", APIKey: jackettKey}}}
	ctx := context.Background()

	pv, err := h.im.Preview(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	j := pv.Jackett
	if j == nil || !j.OK || j.Summary != (Summary{Total: 3, Add: 3}) {
		t.Fatalf("jackett preview: %+v", j)
	}
	x := jackettItem(t, j.Items, "1337x")
	if x.Action != ActionAdd || x.Implementation != "Torznab" || x.Protocol != "torrent" || x.BaseURL != jk.URL+"/api/v2.0/indexers/1337x/results/torznab" ||
		!x.CanAddDirectly || x.Direct || x.DefinitionID != "" || len(x.Categories) != 3 || !strings.Contains(x.Reason, "jackett.direct") {
		t.Fatalf("1337x: %+v", x)
	}
	if n := jackettItem(t, j.Items, "some new tracker"); n.CanAddDirectly || !strings.Contains(n.Reason, "503") || !strings.Contains(n.BaseURL, "some%20new%20tracker") {
		t.Fatalf("indexer only Jackett has: %+v", n)
	}
	if strings.Contains(mustMarshal(t, pv), jackettKey) {
		t.Fatal("the preview must not echo the API key")
	}
	if list, _ := h.idx.List(); len(list) != 0 {
		t.Fatalf("preview added indexers: %+v", list)
	}

	st, err := h.im.Run(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	if st.Step != StepDone || st.Results.Indexers != (Counts{Added: 3}) || len(st.Errors) != 0 {
		t.Fatalf("jackett import: %+v errors=%v", st.Results.Indexers, st.Errors)
	}
	list, _ := h.idx.List()
	byName := map[string]indexers.Instance{}
	for _, inst := range list {
		byName[inst.Name] = inst
	}
	if x := byName["1337x"]; x.Kind != indexers.KindTorznab || x.APIKey != jackettKey || !x.Enabled || x.Protocol != indexers.ProtocolTorrent || x.BaseURL != jk.URL+"/api/v2.0/indexers/1337x/results/torznab" {
		t.Fatalf("1337x instance: %+v", x)
	}
	var raw string
	if err := h.db.QueryRow(`SELECT COALESCE(group_concat(api_key_encrypted, '|'), '') FROM indexers`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, jackettKey) {
		t.Fatal("Jackett API key stored in plain text")
	}

	// A second run adds nothing.
	st2, err := h.im.Run(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	if st2.Results.Indexers != (Counts{Existing: 3}) {
		t.Fatalf("second run: %+v", st2.Results.Indexers)
	}
	if list, _ := h.idx.List(); len(list) != 3 {
		t.Fatalf("indexers after two runs: %d", len(list))
	}
	jk.assertOnlyGET(t)
	if strings.Contains(h.logs.String(), jackettKey) {
		t.Fatal("the Jackett API key was logged")
	}
}

func TestJackettAddSiteDirectly(t *testing.T) {
	h := newHarness(t)
	jk := newFakeJackett(t)
	src := Sources{Jackett: &JackettConn{
		Conn:   Conn{URL: jk.URL, APIKey: jackettKey},
		Direct: []string{"1337X", "privatesite", "some new tracker", "not-in-jackett"},
	}}
	ctx := context.Background()

	pv, err := h.im.Preview(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	x := jackettItem(t, pv.Jackett.Items, "1337x")
	if x.Action != ActionAdd || !x.Direct || x.Implementation != "Cardigann" || x.DefinitionID != "1337x" || x.BaseURL != "https://1337x.to/" || !x.CanAddDirectly {
		t.Fatalf("1337x direct: %+v", x)
	}
	if p := jackettItem(t, pv.Jackett.Items, "privatesite"); !p.Direct || !strings.Contains(p.Reason, "switched off") {
		t.Fatalf("private site direct: %+v", p)
	}
	if n := jackettItem(t, pv.Jackett.Items, "some new tracker"); n.Direct || n.Implementation != "Torznab" || !strings.Contains(n.Reason, "stays on Jackett") {
		t.Fatalf("no site of that id: %+v", n)
	}

	if _, err := h.im.Run(ctx, Options{Sources: src}); err != nil {
		t.Fatal(err)
	}
	list, _ := h.idx.List()
	byName := map[string]indexers.Instance{}
	for _, inst := range list {
		byName[inst.Name] = inst
	}
	if len(list) != 3 {
		t.Fatalf("indexers: %+v", list)
	}
	if x := byName["1337x"]; x.Kind != indexers.KindCardigann || x.DefinitionID != "1337x" || !x.Enabled || x.APIKey != "" {
		t.Fatalf("1337x should be the site itself: %+v", x)
	}
	if p := byName["PrivateSite"]; p.Kind != indexers.KindCardigann || p.Enabled {
		t.Fatalf("a private tracker needs its login first: %+v", p)
	}
	if n := byName["Some New Tracker"]; n.Kind != indexers.KindTorznab || n.APIKey != jackettKey {
		t.Fatalf("stays a Torznab indexer: %+v", n)
	}

	// The same choice again adds nothing; so does going back to the feed for
	// a site that is already there.
	st, err := h.im.Run(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	if st.Results.Indexers.Added != 0 || st.Results.Indexers.Existing != 3 {
		t.Fatalf("second run: %+v", st.Results.Indexers)
	}
	jk.assertOnlyGET(t)
}

func TestJackettErrors(t *testing.T) {
	h := newHarness(t)
	login := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>Jackett login</html>"))
	}))
	t.Cleanup(login.Close)
	jk := newFakeJackett(t)
	tests := []struct {
		name    string
		conn    JackettConn
		wantErr string
	}{
		{"wrong key", JackettConn{Conn: Conn{URL: jk.URL, APIKey: "nope-secret"}}, "the API key was not accepted"},
		{"no key", JackettConn{Conn: Conn{URL: jk.URL}}, "enter the API key"},
		{"a web page instead", JackettConn{Conn: Conn{URL: login.URL, APIKey: jackettKey}}, "did not answer with the expected data"},
		{"nothing listening", JackettConn{Conn: Conn{URL: "http://127.0.0.1:1", APIKey: "secret-in-query"}}, "could not reach 127.0.0.1:1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pv, err := h.im.Preview(context.Background(), Options{Sources: Sources{Jackett: &tt.conn}})
			if err != nil {
				t.Fatal(err)
			}
			if pv.Jackett.OK || !strings.Contains(pv.Jackett.Error, tt.wantErr) {
				t.Fatalf("got %+v, want error containing %q", pv.Jackett.AppStatus, tt.wantErr)
			}
			for _, secret := range []string{"nope-secret", "secret-in-query", "apikey="} {
				if strings.Contains(pv.Jackett.Error, secret) {
					t.Fatalf("error leaks %q: %s", secret, pv.Jackett.Error)
				}
			}
		})
	}
}
