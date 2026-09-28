package subtitles_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ryanborg/mediarium/internal/subtitles"
)

type fakeOS struct {
	srv       *httptest.Server
	logins    atomic.Int32
	downloads atomic.Int32
	lastAuth  atomic.Value // string
	lastUA    atomic.Value // string
	badToken  atomic.Bool  // reject the first token it issued
	wrongPass atomic.Bool
	badKey    atomic.Bool
}

func newFakeOS(t *testing.T) *fakeOS {
	t.Helper()
	f := &fakeOS{}
	f.lastAuth.Store("")
	f.lastUA.Store("")
	issued := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		f.logins.Add(1)
		f.lastUA.Store(r.Header.Get("User-Agent"))
		if f.wrongPass.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		issued++
		json.NewEncoder(w).Encode(map[string]any{"token": "token-" + string(rune('0'+issued)), "status": 200})
	})
	mux.HandleFunc("/subtitles", func(w http.ResponseWriter, r *http.Request) {
		f.lastUA.Store(r.Header.Get("User-Agent"))
		if f.badKey.Load() {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		f.downloads.Add(1)
		auth := r.Header.Get("Authorization")
		f.lastAuth.Store(auth)
		if f.badToken.Load() && auth == "Bearer token-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"link": "http://" + r.Host + "/file.srt"})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func TestDownloadWithAccountLogsInOnceAndSendsTheToken(t *testing.T) {
	f := newFakeOS(t)
	c := subtitles.NewWithBaseURL("key", f.srv.URL)
	c.SetCredentials("ryan", "pw")

	for i := 0; i < 2; i++ {
		if _, err := c.RequestDownload(context.Background(), 1); err != nil {
			t.Fatalf("download %d: %v", i, err)
		}
	}
	if f.logins.Load() != 1 {
		t.Fatalf("expected one login for two downloads, got %d", f.logins.Load())
	}
	if got := f.lastAuth.Load().(string); got != "Bearer token-1" {
		t.Fatalf("expected the login token on the download, got %q", got)
	}
	if got := f.lastUA.Load().(string); got != "Mediarium v1" {
		t.Fatalf("OpenSubtitles requires a User-Agent, got %q", got)
	}
}

func TestDownloadWithoutAccountIsAnonymous(t *testing.T) {
	f := newFakeOS(t)
	c := subtitles.NewWithBaseURL("key", f.srv.URL)
	if _, err := c.RequestDownload(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if f.logins.Load() != 0 || f.lastAuth.Load().(string) != "" {
		t.Fatalf("anonymous use must not log in or send a token (logins %d, auth %q)", f.logins.Load(), f.lastAuth.Load())
	}
}

func TestStaleTokenTriggersOneRelogin(t *testing.T) {
	f := newFakeOS(t)
	f.badToken.Store(true)
	c := subtitles.NewWithBaseURL("key", f.srv.URL)
	c.SetCredentials("ryan", "pw")
	if _, err := c.RequestDownload(context.Background(), 1); err != nil {
		t.Fatalf("a stale token should be replaced transparently: %v", err)
	}
	if f.logins.Load() != 2 || f.downloads.Load() != 2 {
		t.Fatalf("expected login->401->login->ok, got %d logins / %d downloads", f.logins.Load(), f.downloads.Load())
	}
}

func TestWrongPasswordAndBadKeyAreDistinctErrors(t *testing.T) {
	f := newFakeOS(t)
	f.wrongPass.Store(true)
	c := subtitles.NewWithBaseURL("key", f.srv.URL)
	c.SetCredentials("ryan", "nope")
	if _, err := c.RequestDownload(context.Background(), 1); err != subtitles.ErrLoginFailed {
		t.Fatalf("expected ErrLoginFailed, got %v", err)
	}

	f.badKey.Store(true)
	if _, err := subtitles.NewWithBaseURL("bad", f.srv.URL).TestConnection(context.Background()); err != subtitles.ErrInvalidKey {
		t.Fatalf("expected ErrInvalidKey, got %v", err)
	}
}

func TestTestConnectionDescribesWhatIsSetUp(t *testing.T) {
	f := newFakeOS(t)
	c := subtitles.NewWithBaseURL("key", f.srv.URL)
	msg, err := c.TestConnection(context.Background())
	if err != nil || msg == "" {
		t.Fatalf("key-only test: %q %v", msg, err)
	}
	c.SetCredentials("ryan", "pw")
	if _, err := c.TestConnection(context.Background()); err != nil {
		t.Fatalf("key+account test: %v", err)
	}
	if f.logins.Load() != 1 {
		t.Fatalf("the account test should log in once, got %d", f.logins.Load())
	}
}
