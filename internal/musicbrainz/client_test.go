package musicbrainz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeMB serves the testdata fixtures by path and records every request.
type fakeMB struct {
	mu       sync.Mutex
	requests []*http.Request
	// status, when set, is answered for the next N requests (N = len).
	statuses []int
}

func (f *fakeMB) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Clone(context.Background()))
		var status int
		if len(f.statuses) > 0 {
			status, f.statuses = f.statuses[0], f.statuses[1:]
		}
		f.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		fixture := ""
		switch {
		case r.URL.Path == "/ws/2/artist":
			fixture = "artist_search.json"
		case r.URL.Path == "/ws/2/release-group":
			fixture = "release_groups.json"
		case r.URL.Path == "/ws/2/release":
			fixture = "releases.json"
		case r.URL.Path == "/ws/2/release/rel-gb-cd":
			fixture = "release.json"
		case r.URL.Path == "/ws/2/artist/a74b1b7f-71a5-4011-9441-d0b5e4122711":
			w.Write([]byte(`{"id":"a74b1b7f-71a5-4011-9441-d0b5e4122711","name":"Radiohead","sort-name":"Radiohead","type":"Group","country":"GB"}`))
			return
		default:
			http.NotFound(w, r)
			return
		}
		body, err := os.ReadFile(filepath.Join("testdata", fixture))
		if err != nil {
			t.Errorf("read fixture %s: %v", fixture, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}
}

func (f *fakeMB) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeMB) last() *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func newTestClient(t *testing.T, opts ...Option) (*Client, *fakeMB) {
	t.Helper()
	f := &fakeMB{}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	base := []Option{WithBaseURL(srv.URL), WithRateLimit(0), WithBackoff(func(int) time.Duration { return 0 })}
	return New("1.2.3", append(base, opts...)...), f
}

func TestSearchArtistsIdentifiesItselfAndEscapesTheQuery(t *testing.T) {
	c, f := newTestClient(t)
	artists, err := c.SearchArtists(context.Background(), "AC/DC", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(artists) != 3 || artists[0].Name != "Radiohead" || artists[0].Score != 100 || artists[2].SortName != "Head, Radio" {
		t.Fatalf("unexpected artists: %+v", artists)
	}
	r := f.last()
	if got := r.Header.Get("User-Agent"); got != "Mediarium/1.2.3 ( https://github.com/rdborg/Mediarium )" {
		t.Fatalf("User-Agent = %q", got)
	}
	q := r.URL.Query()
	if q.Get("query") != `AC\/DC` || q.Get("fmt") != "json" || q.Get("limit") != "10" {
		t.Fatalf("query = %v", q)
	}
}

func TestArtistReleaseGroupsLeavesOutLiveAndCompilationsAndSortsOldestFirst(t *testing.T) {
	c, f := newTestClient(t)
	groups, err := c.ArtistReleaseGroups(context.Background(), "a74b1b7f-71a5-4011-9441-d0b5e4122711", []string{TypeAlbum, TypeSingle}, false)
	if err != nil {
		t.Fatalf("release groups: %v", err)
	}
	var titles []string
	for _, g := range groups {
		titles = append(titles, g.Title)
	}
	want := "Creep|Pablo Honey|OK Computer|Untitled Future Album"
	if strings.Join(titles, "|") != want {
		t.Fatalf("titles = %v, want %s", titles, want)
	}
	if groups[2].Year() != 1997 || groups[3].Year() != 0 {
		t.Fatalf("years: %d %d", groups[2].Year(), groups[3].Year())
	}
	q := f.last().URL.Query()
	if q.Get("artist") != "a74b1b7f-71a5-4011-9441-d0b5e4122711" || q.Get("type") != "album|single" {
		t.Fatalf("browse query = %v", q)
	}

	withLive, err := c.ArtistReleaseGroups(context.Background(), "a74b1b7f-71a5-4011-9441-d0b5e4122711", []string{TypeAlbum, TypeSingle}, true)
	if err != nil || len(withLive) != 5 {
		t.Fatalf("includeSecondary: %d groups, %v", len(withLive), err)
	}
}

func TestCanonicalTracklistPicksTheEarliestOfficialReleaseAndFlattensDiscs(t *testing.T) {
	c, f := newTestClient(t)
	rel, err := c.CanonicalTracklist(context.Background(), "b1392450-e666-3926-a536-22c65f834433")
	if err != nil {
		t.Fatalf("tracklist: %v", err)
	}
	if rel.ID != "rel-gb-cd" {
		t.Fatalf("picked %s", rel.ID)
	}
	if got := f.last().URL.RawQuery; !strings.Contains(got, "inc=recordings+media") {
		t.Fatalf("release lookup query = %s", got)
	}
	tracks := rel.Tracklist()
	if len(tracks) != 4 {
		t.Fatalf("tracks: %+v", tracks)
	}
	if tracks[2].Title != "Subterranean Homesick Alien" || tracks[2].LengthMs != 267000 {
		t.Fatalf("a track without its own title takes the recording's: %+v", tracks[2])
	}
	if tracks[3].Disc != 2 || tracks[3].Position != 1 || tracks[3].Title != "Lucky (Live)" {
		t.Fatalf("second disc: %+v", tracks[3])
	}
}

func TestCanonicalRelease(t *testing.T) {
	cases := []struct {
		name     string
		releases []Release
		want     string
	}{
		{"empty", nil, ""},
		{"only one", []Release{{ID: "a"}}, "a"},
		{"official beats an earlier promo", []Release{
			{ID: "promo", Status: "Promotion", Date: "1999-01-01"},
			{ID: "official", Status: "Official", Date: "1999-03-01"},
		}, "official"},
		{"no official: earliest of all", []Release{
			{ID: "bootleg", Status: "Bootleg", Date: "2003"},
			{ID: "promo", Status: "Promotion", Date: "2002-11-01"},
		}, "promo"},
		{"same date: the most common country", []Release{
			{ID: "de", Status: "Official", Country: "DE", Date: "2010-02-02"},
			{ID: "us-1", Status: "Official", Country: "US", Date: "2010-02-02"},
			{ID: "us-2", Status: "Official", Country: "US", Date: "2012"},
		}, "us-1"},
		{"a precise date before the bare year", []Release{
			{ID: "year", Status: "Official", Date: "2001"},
			{ID: "day", Status: "Official", Date: "2001-06-01"},
		}, "day"},
		{"unknown dates last", []Release{
			{ID: "undated", Status: "Official"},
			{ID: "dated", Status: "Official", Date: "2020-01-01"},
		}, "dated"},
		{"full tie: lowest id", []Release{
			{ID: "b", Status: "Official", Date: "2000"},
			{ID: "a", Status: "Official", Date: "2000"},
		}, "a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := CanonicalRelease(tc.releases)
			if ok != (tc.want != "") || got.ID != tc.want {
				t.Fatalf("got %q (ok=%v), want %q", got.ID, ok, tc.want)
			}
		})
	}
}

func TestRetriesAfter503ThenSucceeds(t *testing.T) {
	c, f := newTestClient(t)
	f.statuses = []int{http.StatusServiceUnavailable, http.StatusServiceUnavailable}
	a, err := c.GetArtist(context.Background(), "a74b1b7f-71a5-4011-9441-d0b5e4122711")
	if err != nil || a.Name != "Radiohead" {
		t.Fatalf("get artist: %+v, %v", a, err)
	}
	if f.count() != 3 {
		t.Fatalf("want 3 requests (2 retries), got %d", f.count())
	}
}

func TestGivesUpAfterTooMany503s(t *testing.T) {
	c, f := newTestClient(t)
	f.statuses = []int{503, 503, 503, 503, 503}
	_, err := c.GetArtist(context.Background(), "a74b1b7f-71a5-4011-9441-d0b5e4122711")
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("want a busy error, got %v", err)
	}
	if f.count() != 1+defaultRetries {
		t.Fatalf("want %d requests, got %d", 1+defaultRetries, f.count())
	}
}

func TestUnknownIDIsErrNotFound(t *testing.T) {
	c, _ := newTestClient(t)
	_, err := c.GetArtist(context.Background(), "no-such-artist")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestAnswersAreCached(t *testing.T) {
	c, f := newTestClient(t, WithCacheTTL(time.Hour))
	for i := 0; i < 3; i++ {
		if _, err := c.SearchArtists(context.Background(), "Radiohead", 5); err != nil {
			t.Fatalf("search: %v", err)
		}
	}
	if f.count() != 1 {
		t.Fatalf("want 1 request thanks to the cache, got %d", f.count())
	}
	if _, err := c.SearchArtists(context.Background(), "Other", 5); err != nil || f.count() != 2 {
		t.Fatalf("another query must be asked: %d requests, %v", f.count(), err)
	}

	off, f2 := newTestClient(t, WithCacheTTL(0))
	off.SearchArtists(context.Background(), "Radiohead", 5)
	off.SearchArtists(context.Background(), "Radiohead", 5)
	if f2.count() != 2 {
		t.Fatalf("cache off: want 2 requests, got %d", f2.count())
	}
}

func TestRateLimitIsSharedByConcurrentCallers(t *testing.T) {
	const interval = 40 * time.Millisecond
	c, f := newTestClient(t, WithRateLimit(interval), WithCacheTTL(0))
	start := time.Now()
	var wg sync.WaitGroup
	var failed atomic.Int32
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.SearchArtists(context.Background(), "Radiohead", 5); err != nil {
				failed.Add(1)
			}
		}()
	}
	wg.Wait()
	if failed.Load() != 0 || f.count() != 4 {
		t.Fatalf("requests: %d, failures: %d", f.count(), failed.Load())
	}
	// Four requests one interval apart: the last starts three intervals in.
	if elapsed := time.Since(start); elapsed < 3*interval {
		t.Fatalf("4 requests took %v, want at least %v", elapsed, 3*interval)
	}
}

func TestRateLimitWaitStopsWithTheContext(t *testing.T) {
	c, _ := newTestClient(t, WithRateLimit(time.Hour), WithCacheTTL(0))
	if _, err := c.SearchArtists(context.Background(), "first", 5); err != nil {
		t.Fatalf("first request: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.SearchArtists(ctx, "second", 5); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want the context error while waiting for a token, got %v", err)
	}
}

func TestCoverArtURLAndEscaping(t *testing.T) {
	if got := CoverArtURL("b1392450-e666-3926-a536-22c65f834433"); got != "https://coverartarchive.org/release-group/b1392450-e666-3926-a536-22c65f834433/front-500" {
		t.Fatalf("cover url = %s", got)
	}
	if CoverArtURL("") != "" {
		t.Fatal("no id, no url")
	}
	for in, want := range map[string]string{
		"Radiohead":   "Radiohead",
		"AC/DC":       `AC\/DC`,
		"!!!":         `\!\!\!`,
		"Sigur Rós":   "Sigur Rós",
		`Guns N' (R)`: `Guns N' \(R\)`,
	} {
		if got := luceneEscape(in); got != want {
			t.Errorf("luceneEscape(%q) = %q, want %q", in, got, want)
		}
	}
	if UserAgent("") != "Mediarium/dev ( https://github.com/rdborg/Mediarium )" {
		t.Fatalf("empty version: %s", UserAgent(""))
	}
}
