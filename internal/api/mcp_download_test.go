package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newMovieReleaseIndexer is a fake Newznab indexer that answers every search
// with the same releases of "Lighthouse Keepers (2023)" and a few of other films.
func newMovieReleaseIndexer(t *testing.T) *httptest.Server {
	t.Helper()
	titles := []string{
		"Lighthouse.Keepers.2023.1080p.WEB-DL.x264-GRP",
		"Lighthouse.Keepers.2023.2160p.BluRay.x265-FuN",
		"Other.Film.2023.1080p.WEB-DL.x264-GRP",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		var b strings.Builder
		b.WriteString(`<?xml version="1.0"?><rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>`)
		for i, title := range titles {
			fmt.Fprintf(&b, `<item><title>%s</title><guid>m%d</guid><enclosure url="http://%s/nzb/%d.nzb" length="8589934592" type="application/x-nzb"/><newznab:attr name="size" value="8589934592"/><newznab:attr name="category" value="2040"/></item>`, title, i, r.Host, i)
		}
		b.WriteString(`</channel></rss>`)
		fmt.Fprint(w, b.String())
	}))
	t.Cleanup(srv.Close)
	return srv
}

// downloadsEnv is the MCP test server with MCP on, a library, an indexer, and
// the id of a movie that is wanted.
type downloadsEnv struct {
	*mcpEnv
	movieID int64
}

func newDownloadsEnv(t *testing.T) *downloadsEnv {
	t.Helper()
	e := newMCPEnv(t)
	e.enable(t)
	e.library(t)
	postJSON[map[string]any](t, e.admin, e.base+"/api/indexers", map[string]any{
		"name": "Film Indexer", "definitionId": "fixture", "baseUrl": newMovieReleaseIndexer(t).URL, "apiKey": "k",
	}, http.StatusCreated)
	m, in, err := e.server.MovieRepo.GetByTMDBID(703)
	if err != nil || !in {
		t.Fatalf("the wanted movie is missing: %v", err)
	}
	return &downloadsEnv{mcpEnv: e, movieID: m.ID}
}

func (e *downloadsEnv) allowDownloads(t *testing.T, on bool) {
	t.Helper()
	postJSONMethod[map[string]any](t, e.admin, http.MethodPut, e.base+"/api/system/mcp", map[string]any{"downloads": on}, http.StatusOK)
}

func (e *downloadsEnv) queueLen(t *testing.T) int {
	t.Helper()
	return len(getJSON[[]map[string]any](t, e.admin, e.base+"/api/queue"))
}

// firstRelease finds releases for the wanted movie and returns the id of the
// first one.
func (e *downloadsEnv) firstRelease(t *testing.T, key string) string {
	t.Helper()
	isErr, text, got := e.call(t, key, "find_releases", map[string]any{"movie_id": e.movieID})
	if isErr {
		t.Fatalf("find_releases: %s", text)
	}
	rel, _ := got["releases"].([]any)
	if len(rel) == 0 {
		t.Fatalf("find_releases found nothing: %s", text)
	}
	return rel[0].(map[string]any)["release_id"].(string)
}

func TestMCPDownloadsAreOffByDefault(t *testing.T) {
	e := newDownloadsEnv(t)

	// The setting says off, and the two tools are not even listed.
	if got := getJSON[map[string]any](t, e.admin, e.base+"/api/system/mcp"); got["downloads"] != false {
		t.Fatalf("downloads should start off: %v", got)
	}
	_, out, _ := e.rpc(t, e.adminKey, rpcBody(1, "tools/list", nil))
	listing := fmt.Sprint(out["result"])
	for _, name := range []string{"find_releases", "grab_release"} {
		if strings.Contains(listing, name) {
			t.Errorf("%s is listed while downloads are off", name)
		}
	}
	// Calling them anyway is refused with a plain explanation, and nothing happens.
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"find_releases", map[string]any{"movie_id": e.movieID}},
		{"grab_release", map[string]any{"release_id": "r123", "confirm_token": "mcpgrab_x"}},
	} {
		isErr, text, _ := e.call(t, e.adminKey, tc.tool, tc.args)
		if !isErr || !strings.Contains(text, "not allowed") {
			t.Errorf("%s with downloads off: isError=%v %q", tc.tool, isErr, text)
		}
	}
	if n := e.queueLen(t); n != 1 { // the failed download the library fixture starts with
		t.Fatalf("queue has %d items, want 1", n)
	}

	// Only a signed-in session can switch it on, not an API key.
	req, _ := http.NewRequest(http.MethodPut, e.base+"/api/system/mcp", strings.NewReader(`{"downloads":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", e.adminKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("an API key switched downloads on")
	}
	if got := getJSON[map[string]any](t, e.admin, e.base+"/api/system/mcp"); got["downloads"] != false {
		t.Fatalf("downloads are on after a refused request: %v", got)
	}
}

func TestMCPDownloadNeedsAWritableKey(t *testing.T) {
	e := newDownloadsEnv(t)
	e.allowDownloads(t, true)
	_, out, _ := e.rpc(t, e.roKey, rpcBody(1, "tools/list", nil))
	if listing := fmt.Sprint(out["result"]); strings.Contains(listing, "find_releases") || strings.Contains(listing, "grab_release") {
		t.Error("a read-only key sees the download tools")
	}
	for _, tool := range []string{"find_releases", "grab_release"} {
		if isErr, text, _ := e.call(t, e.roKey, tool, map[string]any{"movie_id": e.movieID, "release_id": "r1"}); !isErr || !strings.Contains(text, "read-only") {
			t.Errorf("%s with a read-only key: isError=%v %q", tool, isErr, text)
		}
	}
}

func TestMCPGrabNeedsTheConfirmationStep(t *testing.T) {
	e := newDownloadsEnv(t)
	e.allowDownloads(t, true)
	before := e.queueLen(t)

	id := e.firstRelease(t, e.adminKey)

	// The first call only previews: a token comes back, nothing is queued.
	isErr, text, prev := e.call(t, e.adminKey, "grab_release", map[string]any{"release_id": id})
	if isErr || prev["preview"] != true || prev["changed"] != false {
		t.Fatalf("first call: isError=%v %s", isErr, text)
	}
	token, _ := prev["confirm_token"].(string)
	if token == "" || !strings.Contains(prev["would_do"].(string), "Lighthouse Keepers") {
		t.Fatalf("preview has no token or does not say what happens: %s", text)
	}
	if strings.Contains(text, "/nzb/") || strings.Contains(text, "rel_") {
		t.Errorf("the preview leaks a download address: %s", text)
	}
	if n := e.queueLen(t); n != before {
		t.Fatalf("a preview queued something: %d items, was %d", n, before)
	}

	// A token for another release does nothing, and is used up by trying it.
	other := e.firstReleaseAt(t, 1)
	if isErr, _, _ := e.call(t, e.adminKey, "grab_release", map[string]any{"release_id": other, "confirm_token": token}); !isErr {
		t.Fatal("a token worked for a release it was not made for")
	}
	if n := e.queueLen(t); n != before {
		t.Fatalf("a wrong token queued something")
	}
	if isErr, _, _ := e.call(t, e.adminKey, "grab_release", map[string]any{"release_id": id, "confirm_token": token}); !isErr {
		t.Fatal("a token that was tried on another release still worked")
	}

	// A made-up token does nothing.
	if isErr, text, _ := e.call(t, e.adminKey, "grab_release", map[string]any{"release_id": id, "confirm_token": "mcpgrab_0000"}); !isErr || !strings.Contains(text, "Nothing was downloaded") {
		t.Fatalf("a made-up token: isError=%v %q", isErr, text)
	}

	// A fresh preview, confirmed, starts the download once.
	_, _, prev = e.call(t, e.adminKey, "grab_release", map[string]any{"release_id": id})
	token = prev["confirm_token"].(string)
	isErr, text, done := e.call(t, e.adminKey, "grab_release", map[string]any{"release_id": id, "confirm_token": token})
	if isErr || done["changed"] != true {
		t.Fatalf("confirmed grab: isError=%v %s", isErr, text)
	}
	if n := e.queueLen(t); n != before+1 {
		t.Fatalf("after the confirmed grab the queue has %d items, want %d", n, before+1)
	}

	// The token is used up: replaying it queues nothing more.
	if isErr, _, _ := e.call(t, e.adminKey, "grab_release", map[string]any{"release_id": id, "confirm_token": token}); !isErr {
		t.Fatal("a used token worked again")
	}
	if n := e.queueLen(t); n != before+1 {
		t.Fatalf("a replayed token queued something: %d items", n)
	}
}

// firstReleaseAt is the release_id of the n-th release of a fresh search.
func (e *downloadsEnv) firstReleaseAt(t *testing.T, n int) string {
	t.Helper()
	_, text, got := e.call(t, e.adminKey, "find_releases", map[string]any{"movie_id": e.movieID})
	rel, _ := got["releases"].([]any)
	if len(rel) <= n {
		t.Fatalf("only %d releases found: %s", len(rel), text)
	}
	return rel[n].(map[string]any)["release_id"].(string)
}

func TestMCPGrabTokenRunsOut(t *testing.T) {
	e := newDownloadsEnv(t)
	e.allowDownloads(t, true)
	before := e.queueLen(t)
	id := e.firstRelease(t, e.adminKey)
	_, _, prev := e.call(t, e.adminKey, "grab_release", map[string]any{"release_id": id})
	token := prev["confirm_token"].(string)

	e.server.TestMCPAdvanceClock(6 * time.Minute) // past the token's few minutes, inside the release's half hour
	isErr, text, _ := e.call(t, e.adminKey, "grab_release", map[string]any{"release_id": id, "confirm_token": token})
	if !isErr || !strings.Contains(text, "not valid") {
		t.Fatalf("an expired token: isError=%v %q", isErr, text)
	}
	if n := e.queueLen(t); n != before {
		t.Fatalf("an expired token queued something")
	}

	// A release id runs out too, later.
	e.server.TestMCPAdvanceClock(40 * time.Minute)
	if isErr, text, _ := e.call(t, e.adminKey, "grab_release", map[string]any{"release_id": id}); !isErr || !strings.Contains(text, "run out") {
		t.Fatalf("an expired release id: isError=%v %q", isErr, text)
	}
}

func TestMCPGrabStopsWhenDownloadsAreSwitchedOffBetweenTheSteps(t *testing.T) {
	e := newDownloadsEnv(t)
	e.allowDownloads(t, true)
	before := e.queueLen(t)
	id := e.firstRelease(t, e.adminKey)
	_, _, prev := e.call(t, e.adminKey, "grab_release", map[string]any{"release_id": id})
	token := prev["confirm_token"].(string)

	e.allowDownloads(t, false)
	if isErr, _, _ := e.call(t, e.adminKey, "grab_release", map[string]any{"release_id": id, "confirm_token": token}); !isErr {
		t.Fatal("the grab went through after downloads were switched off")
	}
	if n := e.queueLen(t); n != before {
		t.Fatalf("something was queued after downloads were switched off")
	}
}

func TestMCPFindReleasesChecksItsArguments(t *testing.T) {
	e := newDownloadsEnv(t)
	e.allowDownloads(t, true)
	for name, args := range map[string]map[string]any{
		"neither":        {},
		"both":           {"movie_id": 1, "series_id": 1},
		"unknown movie":  {"movie_id": 99999},
		"unknown show":   {"series_id": 99999, "season": 1},
		"show no season": {"series_id": 1},
	} {
		if isErr, text, _ := e.call(t, e.adminKey, "find_releases", args); !isErr {
			t.Errorf("%s: expected an error, got %q", name, text)
		}
	}
}

func TestMCPGrabAShowSeasonPack(t *testing.T) {
	e := newDownloadsEnv(t)
	e.allowDownloads(t, true)
	postJSON[map[string]any](t, e.admin, e.base+"/api/indexers", map[string]any{
		"name": "TV Indexer", "definitionId": "fixture", "baseUrl": newTVIndexerWith(t, []string{
			"Salt.and.Static.S01.1080p.WEB-DL.x264-GRP",
			"Other.Show.S01.1080p.WEB-DL.x264-GRP",
		}).URL, "apiKey": "k",
	}, http.StatusCreated)
	sr, in, err := e.server.MovieRepo.GetSeriesByTMDBID(9001)
	if err != nil || !in {
		t.Fatalf("the show is missing: %v", err)
	}
	before := e.queueLen(t)

	// A show needs a season (or complete).
	if isErr, _, _ := e.call(t, e.adminKey, "find_releases", map[string]any{"series_id": sr.ID}); !isErr {
		t.Fatal("find_releases without a season was accepted for a show")
	}
	isErr, text, got := e.call(t, e.adminKey, "find_releases", map[string]any{"series_id": sr.ID, "season": 1})
	rel, _ := got["releases"].([]any)
	if isErr || len(rel) != 1 {
		t.Fatalf("season search: isError=%v %s", isErr, text)
	}
	id := rel[0].(map[string]any)["release_id"].(string)

	_, _, prev := e.call(t, e.adminKey, "grab_release", map[string]any{"release_id": id})
	if e.queueLen(t) != before || prev["confirm_token"] == nil {
		t.Fatalf("the preview queued something or has no token: %v", prev)
	}
	isErr, text, done := e.call(t, e.adminKey, "grab_release", map[string]any{"release_id": id, "confirm_token": prev["confirm_token"]})
	if isErr || done["changed"] != true || e.queueLen(t) != before+1 {
		t.Fatalf("confirmed show grab: isError=%v %s (queue %d, was %d)", isErr, text, e.queueLen(t), before)
	}
}
