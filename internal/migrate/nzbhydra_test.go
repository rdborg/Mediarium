package migrate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
)

const hydraKey = "nzbhydra-fixture-key-0008"

// fakeHydra answers t=caps on /api and /torznab/api and checks the key; a
// wrong key gets Newznab's <error code="100"> with a 200, as Hydra does.
type fakeHydra struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
}

func newFakeHydra(t *testing.T) *fakeHydra {
	t.Helper()
	f := &fakeHydra{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path+" t="+r.URL.Query().Get("t"))
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Path != "/api" && r.URL.Path != "/torznab/api" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("apikey") != hydraKey {
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><error code="100" description="Wrong API key"/>`))
			return
		}
		data, err := os.ReadFile(filepath.Join("testdata", "nzbhydra", "caps.xml"))
		if err != nil {
			t.Errorf("read fixture: %v", err)
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeHydra) assertOnlyCaps(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		t.Fatal("NZBHydra2 was never called")
	}
	for _, r := range f.requests {
		if !strings.HasPrefix(r, "GET ") || !strings.HasSuffix(r, " t=caps") {
			t.Fatalf("the importer sent %q to NZBHydra2", r)
		}
	}
}

func TestNZBHydraImportsOneEndpoint(t *testing.T) {
	h := newHarness(t)
	hy := newFakeHydra(t)
	// The address may be entered as the API address itself.
	src := Sources{NZBHydra: &HydraConn{Conn: Conn{URL: hy.URL + "/api", APIKey: hydraKey}}}
	ctx := context.Background()

	pv, err := h.im.Preview(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	n := pv.NZBHydra
	if n == nil || !n.OK || n.Summary != (Summary{Total: 2, Add: 1, Skip: 1}) || len(n.Items) != 2 {
		t.Fatalf("hydra preview: %+v", n)
	}
	nzb, tor := n.Items[0], n.Items[1]
	if nzb.Action != ActionAdd || nzb.Implementation != "Newznab" || nzb.Protocol != "usenet" || nzb.BaseURL != hy.URL || len(nzb.Categories) != 5 {
		t.Fatalf("newznab endpoint: %+v", nzb)
	}
	if tor.Action != ActionSkip || tor.BaseURL != hy.URL+"/torznab" || !strings.Contains(tor.Reason, "nzbhydra.torznab") {
		t.Fatalf("torznab endpoint: %+v", tor)
	}
	if strings.Contains(mustMarshal(t, pv), hydraKey) {
		t.Fatal("the preview must not echo the API key")
	}

	st, err := h.im.Run(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	if st.Results.Indexers != (Counts{Added: 1, Skipped: 1}) || len(st.Errors) != 0 {
		t.Fatalf("hydra import: %+v errors=%v", st.Results.Indexers, st.Errors)
	}
	list, _ := h.idx.List()
	if len(list) != 1 || list[0].Kind != indexers.KindNewznab || list[0].APIKey != hydraKey || list[0].BaseURL != hy.URL || !list[0].Enabled {
		t.Fatalf("indexers: %+v", list)
	}
	hy.assertOnlyCaps(t)
	if strings.Contains(h.logs.String(), hydraKey) {
		t.Fatal("the NZBHydra2 API key was logged")
	}
}

func TestNZBHydraWithTorznabIsIdempotent(t *testing.T) {
	h := newHarness(t)
	hy := newFakeHydra(t)
	src := Sources{NZBHydra: &HydraConn{Conn: Conn{URL: hy.URL, APIKey: hydraKey}, Torznab: true}}
	ctx := context.Background()

	st, err := h.im.Run(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	if st.Results.Indexers != (Counts{Added: 2}) {
		t.Fatalf("first run: %+v errors=%v", st.Results.Indexers, st.Errors)
	}
	list, _ := h.idx.List()
	kinds := map[indexers.Kind]indexers.Instance{}
	for _, inst := range list {
		kinds[inst.Kind] = inst
	}
	if k := kinds[indexers.KindTorznab]; k.BaseURL != hy.URL+"/torznab" || k.Protocol != indexers.ProtocolTorrent || k.APIKey != hydraKey {
		t.Fatalf("torznab instance: %+v", k)
	}
	st2, err := h.im.Run(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	if st2.Results.Indexers != (Counts{Existing: 2}) {
		t.Fatalf("second run: %+v", st2.Results.Indexers)
	}
	if list, _ := h.idx.List(); len(list) != 2 {
		t.Fatalf("indexers after two runs: %d", len(list))
	}
	hy.assertOnlyCaps(t)
}

func TestNZBHydraErrors(t *testing.T) {
	h := newHarness(t)
	hy := newFakeHydra(t)
	tests := []struct {
		name    string
		conn    HydraConn
		wantErr string
	}{
		{"wrong key", HydraConn{Conn: Conn{URL: hy.URL, APIKey: "nope-secret"}}, "the API key was not accepted"},
		{"no key", HydraConn{Conn: Conn{URL: hy.URL}}, "enter the API key"},
		{"wrong address", HydraConn{Conn: Conn{URL: hy.URL + "/hydra", APIKey: hydraKey}}, "answered 404"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pv, err := h.im.Preview(context.Background(), Options{Sources: Sources{NZBHydra: &tt.conn}})
			if err != nil {
				t.Fatal(err)
			}
			if pv.NZBHydra.OK || !strings.Contains(pv.NZBHydra.Error, tt.wantErr) {
				t.Fatalf("got %+v, want error containing %q", pv.NZBHydra.AppStatus, tt.wantErr)
			}
			if strings.Contains(pv.NZBHydra.Error, "nope-secret") || strings.Contains(pv.NZBHydra.Error, "apikey=") {
				t.Fatalf("error leaks the key: %s", pv.NZBHydra.Error)
			}
		})
	}
}
