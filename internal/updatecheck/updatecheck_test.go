package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func rel(tag string, pre bool) githubRelease {
	return githubRelease{TagName: tag, Name: "Mediarium " + tag, Body: "Notes for " + tag, Prerelease: pre, PublishedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
}

func TestPickAndNewer(t *testing.T) {
	draft := rel("v9.0.0", false)
	draft.Draft = true
	cases := []struct {
		name        string
		running     string
		list        []githubRelease
		want        string // version picked, "" for none
		wantOffered bool   // Newer(running, picked)
	}{
		{"1.10 is newer than 1.9", "1.9.0", []githubRelease{rel("v1.10.0", false), rel("v1.9.5", false)}, "1.10.0", true},
		{"1.9 is not newer than 1.10", "1.10.0", []githubRelease{rel("v1.9.5", false)}, "1.9.5", false},
		{"same version is not an update", "1.2.0", []githubRelease{rel("v1.2.0", false)}, "1.2.0", false},
		{"newest wins whatever the list order", "1.0.0", []githubRelease{rel("v1.1.0", false), rel("v1.3.0", false), rel("v1.2.0", false)}, "1.3.0", true},
		{"a stable install ignores a pre-release", "1.2.0", []githubRelease{rel("v1.3.0-rc.1", true), rel("v1.2.1", false)}, "1.2.1", true},
		{"a stable install with only a newer pre-release is offered nothing", "1.2.0", []githubRelease{rel("v1.3.0-rc.1", true)}, "", false},
		{"a pre-release flag counts even if the tag looks stable", "1.2.0", []githubRelease{rel("v1.3.0", true)}, "", false},
		{"a pre-release install is offered a newer pre-release", "1.3.0-beta.1", []githubRelease{rel("v1.3.0-rc.1", true), rel("v1.2.0", false)}, "1.3.0-rc.1", true},
		{"a pre-release install is offered the final release", "1.3.0-rc.1", []githubRelease{rel("v1.3.0", false), rel("v1.3.0-rc.2", true)}, "1.3.0", true},
		{"a pre-release install is not offered an older one", "1.4.0-beta.1", []githubRelease{rel("v1.3.0", false)}, "1.3.0", false},
		{"drafts are skipped", "1.0.0", []githubRelease{draft, rel("v1.0.1", false)}, "1.0.1", true},
		{"tags that are not versions are skipped", "1.0.0", []githubRelease{rel("nightly", false), rel("latest", false), rel("v1.0.1", false), rel("", false)}, "1.0.1", true},
		{"nothing usable", "1.0.0", []githubRelease{rel("nightly", false)}, "", false},
		{"empty list", "1.0.0", nil, "", false},
		{"a dev build is never told to update", "dev", []githubRelease{rel("v1.0.1", false)}, "1.0.1", false},
		{"an unreadable running version is never told to update", "", []githubRelease{rel("v1.0.1", false)}, "1.0.1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := pick("rdborg/Mediarium", tc.running, tc.list)
			if tc.want == "" {
				if ok {
					t.Fatalf("picked %q, want nothing", got.Version)
				}
				return
			}
			if !ok || got.Version != tc.want {
				t.Fatalf("picked %q (ok %v), want %q", got.Version, ok, tc.want)
			}
			if Newer(tc.running, got) != tc.wantOffered {
				t.Fatalf("Newer(%q, %q) = %v, want %v", tc.running, got.Version, !tc.wantOffered, tc.wantOffered)
			}
		})
	}
}

func TestPickBuildsOwnURL(t *testing.T) {
	g := rel("v1.2.0", false)
	got, _ := pick("rdborg/Mediarium", "1.0.0", []githubRelease{g})
	if got.URL != "https://github.com/rdborg/Mediarium/releases/tag/v1.2.0" {
		t.Fatalf("URL = %q", got.URL)
	}
	if got.Tag != "v1.2.0" {
		t.Fatalf("Tag = %q", got.Tag)
	}
}

func TestPickKeepsOnlySafeAssetNames(t *testing.T) {
	g := rel("v1.2.0", false)
	json.Unmarshal([]byte(`{"assets":[{"name":"sha256sums.txt","size":1},{"name":"../evil","size":1},{"name":"a b","size":1},{"name":"mediarium_1.2.0_linux_amd64.tar.gz","size":9}]}`), &g)
	got, _ := pick("rdborg/Mediarium", "1.0.0", []githubRelease{g})
	if len(got.Assets) != 2 || got.Assets[0].Name != "sha256sums.txt" || got.Assets[1].Size != 9 {
		t.Fatalf("assets = %+v", got.Assets)
	}
}

func TestPlainNotes(t *testing.T) {
	long := strings.Repeat("word ", 100)
	cases := []struct {
		name     string
		body     string
		lines    int
		want     string
		wantMore bool
	}{
		{"empty", "", 10, "", false},
		{"only blanks", "\n\n  \n", 10, "", false},
		{"heading and bullets", "## What's new\n\n- **Faster** search\n- A `new` page\n", 10, "What's new\n\n- Faster search\n- A new page", false},
		{"windows line ends", "one\r\ntwo\r\nthree", 10, "one\ntwo\nthree", false},
		{"link kept as words", "See [the guide](https://example.com/x) now, ![logo](a.png)", 10, "See the guide now,", false},
		{"image dropped", "![logo](a.png)Logo above", 10, "Logo above", false},
		{"html tags dropped", "Hello <b>world</b><script>alert(1)</script>", 10, "Hello worldalert(1)", false},
		{"control characters dropped", "a\x00b\x1b[31mc‮d", 10, "ab[31mcd", false},
		{"ruler lines dropped", "one\n---\ntwo\n***\n", 10, "one\ntwo", false},
		{"cut after the limit", "1\n2\n3\n4\n5", 3, "1\n2\n3", true},
		{"exactly the limit is not cut", "1\n2\n3", 3, "1\n2\n3", false},
		{"blank runs are squeezed", "a\n\n\n\n\nb", 10, "a\n\nb", false},
		{"long line shortened", long, 10, strings.TrimSpace(long)[:299] + "…", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, more := PlainNotes(tc.body, tc.lines)
			if tc.name == "long line shortened" {
				if !strings.HasSuffix(got, "…") || len([]rune(got)) > maxLineRunes+1 {
					t.Fatalf("long line = %d runes: %q", len([]rune(got)), got)
				}
				return
			}
			if got != tc.want || more != tc.wantMore {
				t.Fatalf("PlainNotes = %q, %v; want %q, %v", got, more, tc.want, tc.wantMore)
			}
		})
	}
}

// fakeGitHub is a release server that records what it was asked.
type fakeGitHub struct {
	mu       sync.Mutex
	srv      *httptest.Server
	status   int
	body     string
	etag     string
	requests []*http.Request
	headers  []http.Header
	hits     int
}

func newFakeGitHub(t *testing.T, body string) *fakeGitHub {
	f := &fakeGitHub{status: 200, body: body, etag: `"v1"`}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.hits++
		f.headers = append(f.headers, r.Header.Clone())
		if r.URL.Path != "/repos/rdborg/Mediarium/releases" {
			http.NotFound(w, r)
			return
		}
		if f.status == 200 && r.Header.Get("If-None-Match") == f.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if f.etag != "" {
			w.Header().Set("ETag", f.etag)
		}
		if f.status == http.StatusForbidden {
			w.Header().Set("X-RateLimit-Remaining", "0")
		}
		w.WriteHeader(f.status)
		fmt.Fprint(w, f.body)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

const releaseList = `[
 {"tag_name":"v1.10.0","name":"Mediarium 1.10.0","body":"## Added\n- Faster search\n- Nicer pages","draft":false,"prerelease":false,"published_at":"2026-10-01T10:00:00Z","assets":[{"name":"sha256sums.txt","size":10}]},
 {"tag_name":"v1.9.0","name":"Mediarium 1.9.0","body":"old","draft":false,"prerelease":false,"published_at":"2026-09-01T10:00:00Z"},
 {"tag_name":"v2.0.0-rc.1","name":"rc","body":"soon","draft":false,"prerelease":true,"published_at":"2026-10-02T10:00:00Z"}
]`

func checkerFor(f *fakeGitHub, version string) *Checker {
	return &Checker{Repo: "rdborg/Mediarium", Version: version, BaseURL: f.srv.URL}
}

func TestLatestFromFakeServer(t *testing.T) {
	f := newFakeGitHub(t, releaseList)
	c := checkerFor(f, "1.9.0")
	got, ok, err := c.Latest(context.Background(), "1.9.0")
	if err != nil || !ok {
		t.Fatalf("Latest: %v (ok %v)", err, ok)
	}
	if got.Version != "1.10.0" || !Newer("1.9.0", got) {
		t.Fatalf("got %+v", got)
	}
	if got.Notes != "Added\n- Faster search\n- Nicer pages" {
		t.Fatalf("notes = %q", got.Notes)
	}
}

func TestRequestCarriesNothingIdentifying(t *testing.T) {
	f := newFakeGitHub(t, releaseList)
	if _, _, err := checkerFor(f, "1.9.0").Latest(context.Background(), "1.9.0"); err != nil {
		t.Fatal(err)
	}
	h := f.headers[0]
	if h.Get("User-Agent") != "Mediarium/1.9.0" {
		t.Fatalf("User-Agent = %q", h.Get("User-Agent"))
	}
	for _, name := range []string{"Authorization", "Cookie", "X-Api-Key", "X-Forwarded-For", "Referer", "From"} {
		if v := h.Get(name); v != "" {
			t.Errorf("header %s = %q was sent", name, v)
		}
	}
}

func TestUserAgentIsSanitised(t *testing.T) {
	c := &Checker{Version: "1.0.0\r\nX-Evil: 1 é"}
	if got := c.userAgent(); got != "Mediarium/1.0.0X-Evil:1" {
		t.Fatalf("userAgent = %q", got)
	}
	if got := (&Checker{}).userAgent(); got != "Mediarium/dev" {
		t.Fatalf("userAgent = %q", got)
	}
}

func TestSecondCheckUsesETag(t *testing.T) {
	f := newFakeGitHub(t, releaseList)
	c := checkerFor(f, "1.9.0")
	for i := 0; i < 2; i++ {
		got, ok, err := c.Latest(context.Background(), "1.9.0")
		if err != nil || !ok || got.Version != "1.10.0" {
			t.Fatalf("check %d: %+v %v %v", i, got, ok, err)
		}
	}
	if f.headers[1].Get("If-None-Match") != `"v1"` {
		t.Fatalf("second request had If-None-Match %q", f.headers[1].Get("If-None-Match"))
	}
}

func TestFailuresAreQuietErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"rate limited 429", 429, `{"message":"slow down"}`, ErrRateLimited},
		{"rate limited 403", 403, `{"message":"API rate limit exceeded"}`, ErrRateLimited},
		{"not found", 404, `{"message":"Not Found"}`, ErrUnavailable},
		{"server error", 502, `bad gateway`, ErrUnavailable},
		{"garbage", 200, `<html>not json</html>`, ErrUnavailable},
		{"wrong shape", 200, `{"tag_name":"v1.0.0"}`, ErrUnavailable},
		{"empty", 200, ``, ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGitHub(t, tc.body)
			f.status = tc.status
			f.etag = ""
			_, _, err := checkerFor(f, "1.0.0").Latest(context.Background(), "1.0.0")
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNoNetworkIsAnError(t *testing.T) {
	f := newFakeGitHub(t, releaseList)
	c := checkerFor(f, "1.0.0")
	f.srv.Close()
	_, _, err := c.Latest(context.Background(), "1.0.0")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestSlowServerTimesOut(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-block }))
	defer srv.Close()
	defer close(block)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	c := &Checker{Repo: "rdborg/Mediarium", BaseURL: srv.URL}
	if _, _, err := c.Latest(ctx, "1.0.0"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestHugeAnswerIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("["))
		chunk := strings.Repeat(" ", 1<<16)
		for i := 0; i < 80; i++ {
			w.Write([]byte(chunk))
		}
		w.Write([]byte("]"))
	}))
	defer srv.Close()
	c := &Checker{Repo: "rdborg/Mediarium", BaseURL: srv.URL}
	if _, _, err := c.Latest(context.Background(), "1.0.0"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestBadRepoNameIsRefused(t *testing.T) {
	for _, repo := range []string{"a", "a/b/c", "a b/c", "../x/y", "a/b?x=1", "/a/b"} {
		c := &Checker{Repo: repo, BaseURL: "http://127.0.0.1:1"}
		if _, _, err := c.Latest(context.Background(), "1.0.0"); !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "not valid") {
			t.Errorf("repo %q: error = %v", repo, err)
		}
	}
	if !ValidRepo("rdborg/Mediarium") || !ValidRepo("a.b-c_d/e.f") {
		t.Error("a good repo name was refused")
	}
}
