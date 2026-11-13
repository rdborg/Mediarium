package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/settings"
)

const secretSlskdKey = "secret-key-123"

// slskdWith is a fake slskd: its Soulseek connection, the answer to a search,
// and what it does to a request for one.
type slskdWith struct {
	server    string // answer to GET /server; "" for an slskd without that call
	responses string // answer to the search
	failPost  int    // status for starting a search, 0 for fine
}

func (f slskdWith) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/api/v0")
	switch {
	case p == "/server":
		if f.server == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(f.server))
	case p == "/searches" && r.Method == http.MethodPost:
		if f.failPost != 0 {
			w.WriteHeader(f.failPost)
			_, _ = w.Write([]byte("boom from slskd"))
			return
		}
		_, _ = w.Write([]byte(`{"id":"s","isComplete":true}`))
	case strings.HasSuffix(p, "/responses"):
		_, _ = w.Write([]byte(f.responses))
	case strings.HasPrefix(p, "/searches/"):
		_, _ = w.Write([]byte(`{"id":"s","isComplete":true}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

const (
	slskdOnline  = `{"isConnected":true,"isLoggedIn":true,"state":"Connected, LoggedIn"}`
	slskdOffline = `{"isConnected":true,"isLoggedIn":false,"state":"Connected"}`
)

// Every way an album search of Soulseek can end says so in its line of the
// table of who was asked, and in the log.
func TestSoulseekAlbumRunSaysWhatHappened(t *testing.T) {
	tests := []struct {
		name      string
		fake      *slskdWith // nil: no slskd is running
		music     string
		url       string // "" uses the fake's address, "-" leaves it empty
		key       string
		folder    string // "" leaves it empty
		wantShow  bool
		wantState string
		wantCode  string
		wantText  string // in the reason
		wantCount int
		wantLog   string
	}{
		{name: "found", fake: &slskdWith{server: slskdOnline, responses: soulseekAnswer}, music: "1", key: secretSlskdKey, folder: "x",
			wantShow: true, wantState: searchFound, wantCount: 2, wantLog: "soulseek: album search done"},
		{name: "found by an slskd without the server call", fake: &slskdWith{responses: soulseekAnswer}, music: "1", key: secretSlskdKey, folder: "x",
			wantShow: true, wantState: searchFound, wantCount: 2, wantLog: "soulseek: album search done"},
		{name: "empty", fake: &slskdWith{server: slskdOnline, responses: `[]`}, music: "1", key: secretSlskdKey, folder: "x",
			wantShow: true, wantState: searchEmpty, wantLog: "responses=0"},
		{name: "not logged in to Soulseek", fake: &slskdWith{server: slskdOffline, responses: `[]`}, music: "1", key: secretSlskdKey, folder: "x",
			wantShow: true, wantState: searchFailed, wantCode: "not_connected", wantText: "not connected to the Soulseek network (state: Connected)", wantLog: "soulseek: album search failed"},
		{name: "search error", fake: &slskdWith{server: slskdOnline, failPost: 500}, music: "1", key: secretSlskdKey, folder: "x",
			wantShow: true, wantState: searchFailed, wantCode: "error", wantText: "it returned an error", wantLog: "soulseek: album search failed"},
		{name: "key refused", fake: &slskdWith{server: slskdOnline, failPost: 401}, music: "1", key: secretSlskdKey, folder: "x",
			wantShow: true, wantState: searchFailed, wantCode: "auth", wantText: "API key", wantLog: "soulseek: album search failed"},
		{name: "slskd not running", fake: nil, music: "1", key: secretSlskdKey, folder: "x",
			wantShow: true, wantState: searchFailed, wantCode: "unreachable", wantText: "could not be reached", wantLog: "soulseek: album search failed"},
		{name: "folder missing", fake: &slskdWith{server: slskdOnline}, music: "1", key: secretSlskdKey,
			wantShow: true, wantState: searchSkipped, wantCode: "not_set_up", wantText: "the downloads folder is missing", wantLog: "soulseek: album search skipped"},
		{name: "address and key missing", fake: &slskdWith{}, music: "1", url: "-", folder: "x",
			wantShow: true, wantState: searchSkipped, wantCode: "not_set_up", wantText: "the slskd address and the API key is missing", wantLog: "soulseek: album search skipped"},
		{name: "music off", fake: &slskdWith{}, music: "0", key: secretSlskdKey, folder: "x",
			wantShow: true, wantState: searchSkipped, wantCode: "music_off", wantText: "Music is switched off", wantLog: "soulseek: album search skipped"},
		{name: "never set up", fake: &slskdWith{}, music: "1", url: "-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newBareServer(t)
			addr := "http://127.0.0.1:1" // nothing listens here
			if tt.fake != nil {
				srv := httptest.NewServer(tt.fake)
				defer srv.Close()
				addr = srv.URL
			}
			set := map[string]string{settings.KeyModulesMusic: tt.music}
			if tt.url != "-" {
				set[settings.KeySoulseekURL] = addr
			}
			if tt.folder != "" {
				set[settings.KeySoulseekFolder] = t.TempDir()
			}
			for k, v := range set {
				if err := s.Settings.Set(k, v, false); err != nil {
					t.Fatal(err)
				}
			}
			if tt.key != "" {
				if err := s.Settings.Set(settings.KeySoulseekAPIKey, tt.key, true); err != nil {
					t.Fatal(err)
				}
			}
			var logs bytes.Buffer
			old := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
			defer slog.SetDefault(old)

			run := s.soulseekAlbumRun(context.Background(), music.Artist{Name: "Low Roar"}, music.Album{Title: "0", ReleaseDate: "2014-11-04"}, music.Profile{})
			if run.Show != tt.wantShow {
				t.Fatalf("Show = %v, want %v (%+v)", run.Show, tt.wantShow, run.Line)
			}
			if !tt.wantShow {
				if len(run.extra()) != 0 {
					t.Errorf("a Soulseek that is not used adds a line: %+v", run.extra())
				}
				return
			}
			l := run.Line
			if l.Name != "Soulseek (slskd)" || l.Kind != "soulseek" {
				t.Errorf("name and kind: %+v", l)
			}
			if l.Status != tt.wantState || l.Code != tt.wantCode || l.Count != tt.wantCount || len(run.Releases) != tt.wantCount {
				t.Errorf("line = %+v, releases %d; want status %s code %q count %d", l, len(run.Releases), tt.wantState, tt.wantCode, tt.wantCount)
			}
			if !strings.Contains(l.Reason, tt.wantText) {
				t.Errorf("reason %q should mention %q", l.Reason, tt.wantText)
			}
			if tt.wantState == searchFound || tt.wantState == searchEmpty {
				if l.Ms < 0 {
					t.Errorf("time: %d", l.Ms)
				}
			}
			if !strings.Contains(logs.String(), tt.wantLog) {
				t.Errorf("the log should have %q:\n%s", tt.wantLog, logs.String())
			}
			if tt.wantState == searchFound {
				for _, r := range run.Releases {
					if r.PeerSpeed <= 0 || (!r.PeerFree && r.PeerQueue <= 0) {
						t.Errorf("release %q should say the user's speed and queue: speed %d queue %d free %v", r.Title, r.PeerSpeed, r.PeerQueue, r.PeerFree)
					}
				}
			}
			if tt.wantState == searchFound && !strings.Contains(logs.String(), "query=") {
				t.Errorf("the search log has no query:\n%s", logs.String())
			}
			// The key is never in the answer or the log.
			b, _ := json.Marshal(run.Line)
			if strings.Contains(string(b), secretSlskdKey) || strings.Contains(logs.String(), secretSlskdKey) {
				t.Errorf("the API key leaked:\n%s\n%s", b, logs.String())
			}
		})
	}
}
