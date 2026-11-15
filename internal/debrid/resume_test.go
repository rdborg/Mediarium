package debrid

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// linkServer plays a debrid download host. /old links die after cut bytes (the
// connection drops) or answer deadStatus at once, and every later call to them
// answers deadStatus; /new links are good. ignoreRange makes it answer 200
// with the whole file whatever is asked.
type linkServer struct {
	content     string
	cut         int
	deadStatus  int
	ignoreRange bool

	mu      sync.Mutex
	ranges  []string
	usedOld int
}

func (l *linkServer) handler(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	l.ranges = append(l.ranges, r.Header.Get("Range"))
	l.mu.Unlock()
	old := strings.HasPrefix(r.URL.Path, "/old")
	start := 0
	if rg := r.Header.Get("Range"); rg != "" && !l.ignoreRange {
		fmt.Sscanf(rg, "bytes=%d-", &start)
	}
	if old {
		l.mu.Lock()
		l.usedOld++
		first := l.usedOld == 1
		l.mu.Unlock()
		if !first || l.cut == 0 {
			w.WriteHeader(l.deadStatus)
			return
		}
	}
	body := l.content[start:]
	if start > 0 {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(l.content)-1, len(l.content)))
		w.WriteHeader(http.StatusPartialContent)
	}
	if old {
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		_, _ = io.WriteString(w, body[:l.cut])
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
		return
	}
	_, _ = io.WriteString(w, body)
}

func TestFetchResumeWithFreshLink(t *testing.T) {
	retryWait = 0
	content := strings.Repeat("0123456789", 5000)
	tests := []struct {
		name        string
		cut         int
		deadStatus  int
		ignoreRange bool
		prePart     int // bytes already on disk before the first try
		wantRange   string
		wantKept    int64 // -1: whatever the dropped connection had delivered, more than nothing and less than the cut
	}{
		{name: "link dies half way, range honoured", cut: 12000, deadStatus: 403, wantKept: -1},
		{name: "link already expired with a part on disk (403)", deadStatus: 403, prePart: 20000, wantRange: "bytes=20000-", wantKept: 20000},
		{name: "expired link answers 410", deadStatus: 410, prePart: 7000, wantRange: "bytes=7000-", wantKept: 7000},
		{name: "expired link answers 404", deadStatus: 404, prePart: 7000, wantRange: "bytes=7000-", wantKept: 7000},
		{name: "expired link answers 401", deadStatus: 401, prePart: 7000, wantRange: "bytes=7000-", wantKept: 7000},
		{name: "server ignores range and sends it all again", cut: 12000, deadStatus: 403, ignoreRange: true, wantKept: -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ls := &linkServer{content: content, cut: tc.cut, deadStatus: tc.deadStatus, ignoreRange: tc.ignoreRange}
			srv := httptest.NewServer(http.HandlerFunc(ls.handler))
			defer srv.Close()
			dir := t.TempDir()
			f := File{Name: "Show/ep1.mkv", Size: int64(len(content)), URL: srv.URL + "/old/a"}
			if tc.prePart > 0 {
				if err := os.MkdirAll(filepath.Join(dir, "Show"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "Show", "ep1.mkv.part"), []byte(content[:tc.prePart]), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var refreshes int
			var kept []int64
			err := FetchResume(context.Background(), srv.Client(), f, dir, func(int64) {}, FetchOptions{
				Refresh: func(ctx context.Context) (string, error) {
					refreshes++
					return srv.URL + "/new/b", nil
				},
				Note: func(k int64) { kept = append(kept, k) },
			})
			if err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(filepath.Join(dir, "Show", "ep1.mkv"))
			if string(got) != content {
				t.Fatalf("got %d bytes, want %d, or the bytes differ", len(got), len(content))
			}
			if _, err := os.Stat(filepath.Join(dir, "Show", "ep1.mkv.part")); err == nil {
				t.Fatal("the part should be gone")
			}
			if refreshes != 1 || len(kept) != 1 {
				t.Fatalf("refreshes=%d kept=%v, want 1 each", refreshes, kept)
			}
			if tc.wantKept == -1 {
				if kept[0] <= 0 || kept[0] > int64(tc.cut) {
					t.Fatalf("kept %d bytes, want some of the first %d", kept[0], tc.cut)
				}
				if !tc.ignoreRange {
					tc.wantRange = fmt.Sprintf("bytes=%d-", kept[0])
				}
			} else if kept[0] != tc.wantKept {
				t.Fatalf("kept %d, want %d", kept[0], tc.wantKept)
			}
			if tc.wantRange != "" {
				if lastRange := ls.ranges[len(ls.ranges)-1]; lastRange != tc.wantRange {
					t.Fatalf("the fresh link was asked for %q, want %q", lastRange, tc.wantRange)
				}
			}
		})
	}
}

// A link that never gets any further is given up on after a few fresh tries,
// and the error does not carry the link.
func TestFetchResumeGivesUp(t *testing.T) {
	retryWait = 0
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	var refreshes int
	f := File{Name: "a.mkv", Size: 100, URL: srv.URL + "/secret-token"}
	err := FetchResume(context.Background(), srv.Client(), f, t.TempDir(), func(int64) {}, FetchOptions{
		Refresh: func(ctx context.Context) (string, error) { refreshes++; return srv.URL + "/secret-token", nil },
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if refreshes != maxStalledRetries || hits != maxStalledRetries+1 {
		t.Fatalf("refreshes=%d hits=%d", refreshes, hits)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("the error leaks the link: %v", err)
	}
}

// Without a way to get a fresh link a dead link is simply reported.
func TestFetchWithoutRefresherReportsTheError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer srv.Close()
	err := Fetch(context.Background(), srv.Client(), File{Name: "a.mkv", Size: 10, URL: srv.URL}, t.TempDir(), func(int64) {})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("got %v", err)
	}
}

// When asking for a new link fails the download stops and says so.
func TestFetchResumeRefreshFails(t *testing.T) {
	retryWait = 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusGone) }))
	defer srv.Close()
	err := FetchResume(context.Background(), srv.Client(), File{Name: "a.mkv", Size: 10, URL: srv.URL}, t.TempDir(), func(int64) {}, FetchOptions{
		Refresh: func(ctx context.Context) (string, error) { return "", fmt.Errorf("gone from the account") },
	})
	if err == nil || !strings.Contains(err.Error(), "new link failed") {
		t.Fatalf("got %v", err)
	}
}

// A stopped download is not retried.
func TestFetchResumeStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { cancel(); w.WriteHeader(http.StatusForbidden) }))
	defer srv.Close()
	var refreshes int
	_ = FetchResume(ctx, srv.Client(), File{Name: "a.mkv", Size: 10, URL: srv.URL}, t.TempDir(), func(int64) {}, FetchOptions{
		Refresh: func(ctx context.Context) (string, error) { refreshes++; return srv.URL, nil },
	})
	if refreshes != 0 {
		t.Fatalf("asked for a new link after being stopped: %d", refreshes)
	}
}

// A 206 for the wrong part of the file is not appended to the part.
func TestFetchResumeWrongRange(t *testing.T) {
	retryWait = 0
	content := strings.Repeat("abcdefghij", 1000)
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		bad := n == 1
		mu.Unlock()
		if bad {
			w.Header().Set("Content-Range", "bytes 0-9999/10000")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(w, content)
			return
		}
		_, _ = io.WriteString(w, content)
	}))
	defer srv.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.mkv.part"), []byte(content[:500]), 0o644); err != nil {
		t.Fatal(err)
	}
	err := FetchResume(context.Background(), srv.Client(), File{Name: "a.mkv", Size: int64(len(content)), URL: srv.URL}, dir, func(int64) {}, FetchOptions{
		Refresh: func(ctx context.Context) (string, error) { return srv.URL, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "a.mkv"))
	if string(got) != content {
		t.Fatal("the file is not the one served")
	}
}
