package qbit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeTransmission is the part of Transmission's RPC Mediarium uses: the
// session id handshake, basic authentication and a few torrents with labels.
type fakeTransmission struct {
	*httptest.Server
	mu       sync.Mutex
	torrents map[string]*tmTorrent
	added    []map[string]any
	sets     []map[string]any
	removed  []string
	wipe     bool
}

const fakeSession = "session-123"

func newFakeTransmission(t *testing.T) *fakeTransmission {
	t.Helper()
	f := &fakeTransmission{torrents: map[string]*tmTorrent{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transmission/rpc" {
			http.NotFound(w, r)
			return
		}
		if u, p, ok := r.BasicAuth(); !ok || u != "me" || p != "secret" {
			w.Header().Set("WWW-Authenticate", `Basic realm="Transmission"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-Transmission-Session-Id") != fakeSession {
			w.Header().Set("X-Transmission-Session-Id", fakeSession)
			http.Error(w, "409: Conflict", http.StatusConflict)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method    string         `json:"method"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(body, &req)
		f.mu.Lock()
		defer f.mu.Unlock()
		out := map[string]any{"result": "success"}
		ids := func() []string {
			var l []string
			for _, v := range req.Arguments["ids"].([]any) {
				l = append(l, v.(string))
			}
			return l
		}
		switch req.Method {
		case "session-get":
			out["arguments"] = map[string]any{"version": "4.0.5 (a6fe2f7be1)"}
		case "torrent-add":
			f.added = append(f.added, req.Arguments)
			hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			var labels []string
			if raw, ok := req.Arguments["labels"].([]any); ok {
				for _, l := range raw {
					labels = append(labels, l.(string))
				}
			}
			f.torrents[hash] = &tmTorrent{Hash: hash, Name: "Film.2024.1080p", Status: tmDownloading, PercentDone: 0.4, DownloadDir: "/downloads", SizeWhenDone: 8000, Labels: labels, Connected: 5, SendingToUs: 3, RateDown: 1000, ETA: 60}
			out["arguments"] = map[string]any{"torrent-added": map[string]any{"hashString": hash}}
		case "torrent-get":
			var list []*tmTorrent
			if _, ok := req.Arguments["ids"]; ok {
				for _, id := range ids() {
					if t := f.torrents[id]; t != nil {
						list = append(list, t)
					}
				}
			} else {
				for _, t := range f.torrents {
					list = append(list, t)
				}
			}
			out["arguments"] = map[string]any{"torrents": list}
		case "torrent-set":
			f.sets = append(f.sets, req.Arguments)
		case "torrent-stop":
			f.torrents[ids()[0]].Status = tmStopped
		case "torrent-start":
			f.torrents[ids()[0]].Status = tmDownloading
		case "torrent-verify":
			f.torrents[ids()[0]].Status = tmCheckWait
		case "torrent-remove":
			f.removed = append(f.removed, ids()[0])
			f.wipe, _ = req.Arguments["delete-local-data"].(bool)
			delete(f.torrents, ids()[0])
		default:
			out["result"] = "no method name"
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(f.Close)
	return f
}

func TestTransmissionRPCAddress(t *testing.T) {
	for in, want := range map[string]string{
		"http://nas:9091":                   "http://nas:9091/transmission/rpc",
		"http://nas:9091/":                  "http://nas:9091/transmission/rpc",
		"http://nas:9091/transmission":      "http://nas:9091/transmission/rpc",
		"http://nas:9091/transmission/web/": "http://nas:9091/transmission/rpc",
		"http://nas:9091/transmission/rpc":  "http://nas:9091/transmission/rpc",
		"http://nas/tr/transmission/rpc":    "http://nas/tr/transmission/rpc",
	} {
		if got := NewTransmission(in, "", "", http.DefaultClient).rpc; got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestTransmissionSignInAndVersion(t *testing.T) {
	f := newFakeTransmission(t)
	ctx := context.Background()
	c := NewTransmission(f.URL, "me", "secret", f.Client())
	v, err := c.Version(ctx)
	if err != nil || !strings.HasPrefix(v, "4.0.5") {
		t.Fatalf("version = %q, %v (the session id handshake should be handled)", v, err)
	}
	if err := c.Login(ctx); err != nil {
		t.Fatal(err)
	}
	bad := NewTransmission(f.URL, "me", "wrong", f.Client())
	if err := bad.Login(ctx); !errors.Is(err, ErrAuth) {
		t.Fatalf("a wrong password should be ErrAuth, got %v", err)
	}
	if err := NewTransmission(f.URL+"/nothing", "me", "secret", f.Client()).Login(ctx); err == nil {
		t.Fatal("a wrong address should fail")
	}
}

func TestTransmissionAddListInfoControl(t *testing.T) {
	f := newFakeTransmission(t)
	ctx := context.Background()
	c := NewTransmission(f.URL, "me", "secret", f.Client())

	if err := c.Add(ctx, AddOptions{}); err == nil {
		t.Fatal("adding nothing should fail")
	}
	if err := c.Add(ctx, AddOptions{Magnet: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Category: "mediarium", Tag: "mediarium-q7"}); err != nil {
		t.Fatal(err)
	}
	if got := f.added[0]["filename"]; got == nil || !strings.HasPrefix(got.(string), "magnet:") {
		t.Fatalf("a magnet link goes in filename: %v", f.added[0])
	}
	if err := c.Add(ctx, AddOptions{TorrentFile: []byte("d4:infod4:name1:xee"), Tag: "t2", Category: "mediarium"}); err != nil {
		t.Fatal(err)
	}
	if got := f.added[1]["metainfo"]; got != base64.StdEncoding.EncodeToString([]byte("d4:infod4:name1:xee")) {
		t.Fatalf("a torrent file goes in as base64 metainfo: %v", got)
	}

	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	byTag, err := c.InfoByTag(ctx, "t2")
	if err != nil || len(byTag) != 1 || byTag[0].Hash != hash {
		t.Fatalf("by tag: %+v, %v", byTag, err)
	}
	if none, _ := c.InfoByTag(ctx, "no-such-tag"); len(none) != 0 {
		t.Fatalf("an unknown tag has no torrents: %+v", none)
	}
	list, err := c.List(ctx, "mediarium")
	if err != nil || len(list) != 1 || list[0].Category != "mediarium" || !list[0].HasTag("t2") {
		t.Fatalf("list: %+v, %v", list, err)
	}
	if other, _ := c.List(ctx, "someone-elses"); len(other) != 0 {
		t.Fatalf("another category is not listed: %+v", other)
	}

	tor, ok, err := c.Info(ctx, strings.ToUpper(hash))
	if err != nil || !ok {
		t.Fatalf("info: %v %v", ok, err)
	}
	if tor.State != "downloading" || tor.Progress != 0.4 || tor.Size != 8000 || tor.Content() != "/downloads/Film.2024.1080p" || tor.DLSpeed != 1000 {
		t.Fatalf("torrent mapped wrong: %+v", tor)
	}
	if tor.Done() || tor.Failed() {
		t.Fatal("a download in progress is neither done nor failed")
	}
	if _, ok, _ := c.Info(ctx, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); ok {
		t.Fatal("an unknown hash is not there")
	}

	if err := c.Pause(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if tor, _, _ := c.Info(ctx, hash); tor.State != "pausedDL" {
		t.Fatalf("paused: %s", tor.State)
	}
	if err := c.Resume(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if err := c.Recheck(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if tor, _, _ := c.Info(ctx, hash); tor.State != "checkingDL" {
		t.Fatalf("rechecking: %s", tor.State)
	}
	if err := c.Delete(ctx, hash, true); err != nil {
		t.Fatal(err)
	}
	if len(f.removed) != 1 || !f.wipe {
		t.Fatalf("removed %v with files %v", f.removed, f.wipe)
	}
}

func TestTransmissionStates(t *testing.T) {
	cases := []struct {
		name  string
		tm    tmTorrent
		state string
		done  bool
		fail  bool
	}{
		{"downloading", tmTorrent{Status: tmDownloading, PercentDone: 0.5, Connected: 3, SendingToUs: 2, RateDown: 10}, "downloading", false, false},
		{"stalled", tmTorrent{Status: tmDownloading, PercentDone: 0.5, Connected: 0}, "stalledDL", false, false},
		{"waiting to download", tmTorrent{Status: tmDownloadWait, PercentDone: 0.1}, "queuedDL", false, false},
		{"seeding", tmTorrent{Status: tmSeeding, PercentDone: 1, RateUp: 5}, "uploading", true, false},
		{"seeding with nobody asking", tmTorrent{Status: tmSeeding, PercentDone: 1}, "stalledUP", true, false},
		{"waiting to seed", tmTorrent{Status: tmSeedWait, PercentDone: 1}, "queuedUP", true, false},
		{"stopped after finishing", tmTorrent{Status: tmStopped, PercentDone: 1}, "pausedUP", true, false},
		{"stopped half way", tmTorrent{Status: tmStopped, PercentDone: 0.3}, "pausedDL", false, false},
		{"checking a finished one", tmTorrent{Status: tmChecking, PercentDone: 1}, "checkingUP", false, false},
		{"a tracker error does not fail it", tmTorrent{Status: tmDownloading, PercentDone: 0.2, Error: 2, Connected: 1, SendingToUs: 1, RateDown: 1}, "downloading", false, false},
		{"a local error does", tmTorrent{Status: tmStopped, PercentDone: 0.2, Error: 3}, "error", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.tm.torrent("")
			if got.State != tc.state || got.Done() != tc.done || got.Failed() != tc.fail {
				t.Errorf("state %q done %v failed %v, want %q %v %v", got.State, got.Done(), got.Failed(), tc.state, tc.done, tc.fail)
			}
		})
	}
}

func TestKindsIncludeTransmission(t *testing.T) {
	if !ValidKind(Transmission) || Label(Transmission) != "Transmission" {
		t.Fatal("Transmission is a kind")
	}
	if _, ok := NewAPI(Transmission, "http://x", "", "", http.DefaultClient).(*TransmissionClient); !ok {
		t.Fatal("the Transmission kind gets the Transmission client")
	}
	if _, ok := NewAPI(QBittorrent, "http://x", "", "", http.DefaultClient).(*Client); !ok {
		t.Fatal("the other kinds get the qBittorrent client")
	}
}

func TestTransmissionAddSetsRatioLimitOnly(t *testing.T) {
	cases := []struct {
		name      string
		share     ShareLimits
		wantSet   bool
		wantRatio float64
	}{
		{"ratio is passed on", ShareLimits{Ratio: 2, Time: 48 * time.Hour}, true, 2},
		{"time alone has no Transmission equivalent", ShareLimits{Time: 48 * time.Hour}, false, 0},
		{"no limits sends nothing", ShareLimits{}, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeTransmission(t)
			c := NewTransmission(f.URL, "me", "secret", f.Client())
			hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			err := c.Add(context.Background(), AddOptions{Magnet: "magnet:?xt=urn:btih:" + hash, Hash: hash, Share: tc.share})
			if err != nil {
				t.Fatal(err)
			}
			if (len(f.sets) == 1) != tc.wantSet {
				t.Fatalf("torrent-set calls = %d, want set=%v", len(f.sets), tc.wantSet)
			}
			if tc.wantSet {
				a := f.sets[0]
				if a["seedRatioLimit"] != tc.wantRatio || a["seedRatioMode"] != float64(1) {
					t.Errorf("args = %v", a)
				}
				if _, ok := a["seedIdleLimit"]; ok {
					t.Error("the idle limit must not be used for a seeding time")
				}
			}
		})
	}
}

// Transmission 4 answers 421 for a host name that is not in its whitelist; the
// message says what to do instead of showing a bare status code.
func TestTransmissionHostWhitelistAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMisdirectedRequest)
	}))
	defer srv.Close()
	_, err := NewTransmission(srv.URL, "", "", srv.Client()).Version(context.Background())
	if err == nil || !strings.Contains(err.Error(), "rpc-host-whitelist") {
		t.Fatalf("got %v", err)
	}
}
