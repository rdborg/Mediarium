package migrate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

const (
	seerrKey = "overseerr-fixture-key-0009"
	ombiKey  = "ombi-fixture-key-0010"
)

// newFakeOverseerr serves /api/v1/status (open) and the paged
// /api/v1/request behind X-Api-Key, cutting the fixture list by take and
// skip like the real API.
func newFakeOverseerr(t *testing.T) *fakeApp {
	t.Helper()
	f := &fakeApp{t: t, key: seerrKey}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		switch r.URL.Path {
		case "/api/v1/status":
			serveFixture(t, w, "overseerr/status.json")
		case "/api/v1/request":
			if r.Header.Get("X-Api-Key") != seerrKey {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			q := r.URL.Query()
			take, _ := strconv.Atoi(q.Get("take"))
			skip, _ := strconv.Atoi(q.Get("skip"))
			if take <= 0 || q.Get("filter") != "all" {
				t.Errorf("unexpected request query: %s", r.URL.RawQuery)
			}
			data, err := os.ReadFile(filepath.Join("testdata", "overseerr", "request.json"))
			if err != nil {
				t.Fatal(err)
			}
			var all struct {
				PageInfo map[string]any    `json:"pageInfo"`
				Results  []json.RawMessage `json:"results"`
			}
			if err := json.Unmarshal(data, &all); err != nil {
				t.Fatal(err)
			}
			end := min(skip+take, len(all.Results))
			var page []json.RawMessage
			if skip < len(all.Results) {
				page = all.Results[skip:end]
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"pageInfo": all.PageInfo, "results": page})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func newFakeOmbi(t *testing.T) *fakeApp {
	t.Helper()
	f := &fakeApp{t: t, key: ombiKey}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if r.Header.Get("ApiKey") != ombiKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/Request/movie":
			serveFixture(t, w, "ombi/movie.json")
		case "/api/v1/Request/tvlite":
			serveFixture(t, w, "ombi/tvlite.json")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// recordActivity captures what the importer writes to the activity feed.
func recordActivity(h *harness) *[]string {
	var (
		mu   sync.Mutex
		msgs []string
	)
	h.im.deps.Activity = func(movieID, seriesID int64, message string) {
		mu.Lock()
		msgs = append(msgs, message)
		mu.Unlock()
	}
	return &msgs
}

func requestByTMDB(t *testing.T, items []RequestItem, media string, tmdbID int) RequestItem {
	t.Helper()
	for _, it := range items {
		if it.MediaType == media && it.TMDBID == tmdbID {
			return it
		}
	}
	t.Fatalf("no %s request with TMDB id %d in %+v", media, tmdbID, items)
	return RequestItem{}
}

func TestOverseerrPreviewAndImport(t *testing.T) {
	h := newHarness(t)
	seerr := newFakeOverseerr(t)
	msgs := recordActivity(h)
	src := Sources{Overseerr: &Conn{URL: seerr.URL, APIKey: seerrKey}}
	ctx := context.Background()

	pv, err := h.im.Preview(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	o := pv.Overseerr
	if o == nil || !o.OK || o.Version != "1.33.2" || o.Summary != (Summary{Total: 7, Add: 3, Exists: 1, Skip: 3}) {
		t.Fatalf("overseerr preview: %+v", o)
	}
	dune := requestByTMDB(t, o.Items, "movie", 693134)
	if dune.Action != ActionAdd || dune.Title != "Dune: Part Two" || dune.Year != 2024 || dune.Status != RequestApproved ||
		!reflect.DeepEqual(dune.RequestedBy, []string{"Ann", "Bob"}) {
		t.Fatalf("two requests for one movie are one item: %+v", dune)
	}
	if heat := requestByTMDB(t, o.Items, "movie", 949); heat.Action != ActionAdd || heat.Status != RequestProcessing || !strings.Contains(heat.Reason, "already being downloaded") {
		t.Fatalf("heat: %+v", heat)
	}
	if fc := requestByTMDB(t, o.Items, "movie", 550); fc.Action != ActionSkip || fc.Status != RequestAvailable || !strings.Contains(fc.Reason, "already downloaded there") {
		t.Fatalf("available request: %+v", fc)
	}
	if m := requestByTMDB(t, o.Items, "movie", 603); m.Action != ActionExists || m.Title != "The Matrix" || !reflect.DeepEqual(m.RequestedBy, []string{"dana"}) {
		t.Fatalf("title already in the library: %+v", m)
	}
	show := requestByTMDB(t, o.Items, "tv", 5555)
	if show.Action != ActionAdd || show.Title != "Fixture Old Show" || !reflect.DeepEqual(show.Seasons, []int{1}) || !strings.Contains(show.Reason, "requested seasons (1)") {
		t.Fatalf("requested show: %+v", show)
	}
	if bb := requestByTMDB(t, o.Items, "tv", 1396); bb.Action != ActionSkip || bb.Status != RequestPartiallyAvailable {
		t.Fatalf("partly available show: %+v", bb)
	}
	if inc := requestByTMDB(t, o.Items, "movie", 27205); inc.Action != ActionSkip || inc.Status != RequestDeclined || inc.Reason != "declined in Overseerr" {
		t.Fatalf("declined: %+v", inc)
	}
	if body := mustMarshal(t, pv); strings.Contains(body, seerrKey) || strings.Contains(body, "example.invalid") {
		t.Fatal("the preview must not echo the API key or email addresses")
	}
	if movies, _ := h.lib.List(); len(movies) != 1 {
		t.Fatalf("preview added movies: %d", len(movies))
	}

	st, err := h.im.Run(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	if st.Step != StepDone || st.Results.Requests != (Counts{Added: 3, Existing: 1, Skipped: 3}) || st.Total != 7 || len(st.Errors) != 0 {
		t.Fatalf("overseerr import: %+v total=%d errors=%v items=%+v", st.Results.Requests, st.Total, st.Errors, st.Items)
	}
	if len(st.Notes) == 0 || !strings.Contains(st.Notes[0], "No search was started") {
		t.Fatalf("expected the no-search note: %v", st.Notes)
	}
	for _, id := range []int{693134, 949} {
		m, ok, _ := h.lib.GetByTMDBID(id)
		if !ok || !m.Monitored || m.Status != library.StatusMissing {
			t.Fatalf("requested movie %d should be wanted: %+v", id, m)
		}
	}
	if _, ok, _ := h.lib.GetByTMDBID(550); ok {
		t.Fatal("an available request must not be added")
	}
	old, ok, _ := h.lib.GetSeriesByTMDBID(5555)
	if !ok || !old.Monitored {
		t.Fatalf("requested show: %+v", old)
	}
	eps, _ := h.lib.ListEpisodes(old.ID)
	for _, e := range eps {
		if e.Monitored != (e.Season == 1) {
			t.Fatalf("only the requested season is monitored: %+v", e)
		}
	}
	if !reflect.DeepEqual(*msgs, []string{"Requested by Ann and Bob in Overseerr", "Requested by Carl in Overseerr", "Requested by dana in Overseerr"}) {
		t.Fatalf("events: %v", *msgs)
	}

	// A second run adds nothing and writes no more events.
	st2, err := h.im.Run(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	if st2.Results.Requests != (Counts{Existing: 4, Skipped: 3}) || len(*msgs) != 3 {
		t.Fatalf("second run: %+v events=%v", st2.Results.Requests, *msgs)
	}
	seerr.assertOnlyGET(t)
	if strings.Contains(h.logs.String(), seerrKey) {
		t.Fatal("the API key was logged")
	}
}

func TestOverseerrPagesThroughAllRequests(t *testing.T) {
	h := newHarness(t)
	seerr := newFakeOverseerr(t)
	prev := overseerrPageSize
	overseerrPageSize = 2
	t.Cleanup(func() { overseerrPageSize = prev })
	pv, err := h.im.Preview(context.Background(), Options{Sources: Sources{Overseerr: &Conn{URL: seerr.URL, APIKey: seerrKey}}})
	if err != nil {
		t.Fatal(err)
	}
	if o := pv.Overseerr; !o.OK || o.Summary.Total != 7 {
		t.Fatalf("all pages should be read: %+v", o)
	}
	seerr.mu.Lock()
	defer seerr.mu.Unlock()
	if len(seerr.requests) < 5 {
		t.Fatalf("expected several pages, got %v", seerr.requests)
	}
}

func TestOmbiPreviewAndImport(t *testing.T) {
	h := newHarness(t)
	ombi := newFakeOmbi(t)
	msgs := recordActivity(h)
	src := Sources{Ombi: &Conn{URL: ombi.URL, APIKey: ombiKey}}
	ctx := context.Background()

	pv, err := h.im.Preview(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	o := pv.Ombi
	if o == nil || !o.OK || o.Summary != (Summary{Total: 6, Add: 3, Skip: 3}) {
		t.Fatalf("ombi preview: %+v", o)
	}
	dune := requestByTMDB(t, o.Items, "movie", 693134)
	if dune.Action != ActionAdd || dune.Title != "Dune: Part Two" || dune.Year != 2024 || dune.Status != RequestApproved || !reflect.DeepEqual(dune.RequestedBy, []string{"Eve"}) {
		t.Fatalf("dune: %+v", dune)
	}
	if heat := requestByTMDB(t, o.Items, "movie", 949); heat.Status != RequestPending || !strings.Contains(heat.Reason, "waiting for approval in Ombi") || !reflect.DeepEqual(heat.RequestedBy, []string{"Fay"}) {
		t.Fatalf("heat: %+v", heat)
	}
	if m := requestByTMDB(t, o.Items, "movie", 603); m.Action != ActionSkip || m.Status != RequestAvailable {
		t.Fatalf("matrix: %+v", m)
	}
	if inc := requestByTMDB(t, o.Items, "movie", 27205); inc.Action != ActionSkip || inc.Status != RequestDeclined {
		t.Fatalf("inception: %+v", inc)
	}
	show := requestByTMDB(t, o.Items, "tv", 5555)
	if show.Action != ActionAdd || show.TVDBID != 12345 || show.Title != "Fixture Old Show" || show.Status != RequestApproved ||
		!reflect.DeepEqual(show.Seasons, []int{1}) || !reflect.DeepEqual(show.RequestedBy, []string{"Hal", "ida"}) {
		t.Fatalf("show found by TVDB id: %+v", show)
	}
	var unknown RequestItem
	for _, it := range o.Items {
		if it.Title == "Unknown To TMDB" {
			unknown = it
		}
	}
	if unknown.Action != ActionSkip || !strings.Contains(unknown.Reason, "no TMDB id") {
		t.Fatalf("unknown show: %+v", unknown)
	}

	st, err := h.im.Run(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	if st.Results.Requests != (Counts{Added: 3, Skipped: 3}) || len(st.Errors) != 0 {
		t.Fatalf("ombi import: %+v errors=%v", st.Results.Requests, st.Errors)
	}
	if !reflect.DeepEqual(*msgs, []string{"Requested by Eve in Ombi", "Requested by Fay in Ombi", "Requested by Hal and ida in Ombi"}) {
		t.Fatalf("events: %v", *msgs)
	}
	if _, ok, _ := h.lib.GetSeriesByTMDBID(5555); !ok {
		t.Fatal("the requested show was not added")
	}
	st2, err := h.im.Run(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	if st2.Results.Requests != (Counts{Existing: 3, Skipped: 3}) {
		t.Fatalf("second run: %+v", st2.Results.Requests)
	}
	ombi.assertOnlyGET(t)
	if strings.Contains(h.logs.String(), ombiKey) {
		t.Fatal("the API key was logged")
	}
}

func TestOverseerrAndOmbiTogetherAddATitleOnce(t *testing.T) {
	h := newHarness(t)
	src := Sources{
		Overseerr: &Conn{URL: newFakeOverseerr(t).URL, APIKey: seerrKey},
		Ombi:      &Conn{URL: newFakeOmbi(t).URL, APIKey: ombiKey},
	}
	pv, err := h.im.Preview(context.Background(), Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	if o := pv.Ombi; o.Summary != (Summary{Total: 6, Exists: 3, Skip: 3}) {
		t.Fatalf("what Overseerr already brings is not added again: %+v", o.Summary)
	}
	if dune := requestByTMDB(t, pv.Ombi.Items, "movie", 693134); dune.Reason != "already added from another app in this import" {
		t.Fatalf("dune: %+v", dune)
	}
	st, err := h.im.Run(context.Background(), Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	if st.Results.Requests.Added != 3 || st.Results.Requests.Existing != 4 {
		t.Fatalf("results: %+v", st.Results.Requests)
	}
	if movies, _ := h.lib.List(); len(movies) != 3 {
		t.Fatalf("movies: %d", len(movies))
	}
}

func TestRequestsCanBeLeftOut(t *testing.T) {
	h := newHarness(t)
	seerr := newFakeOverseerr(t)
	no := false
	st, err := h.im.Run(context.Background(), Options{
		Sources: Sources{Overseerr: &Conn{URL: seerr.URL, APIKey: seerrKey}},
		Include: Include{Requests: &no},
	})
	if err != nil {
		t.Fatal(err)
	}
	seerr.mu.Lock()
	n := len(seerr.requests)
	seerr.mu.Unlock()
	if n != 0 || st.Results.Requests != (Counts{}) {
		t.Fatalf("requests were read although left out: %d requests, %+v", n, st.Results.Requests)
	}
}

func TestRequestApps_ConnectionErrors(t *testing.T) {
	h := newHarness(t)
	seerr, ombi := newFakeOverseerr(t), newFakeOmbi(t)
	tests := []struct {
		name    string
		src     Sources
		app     func(Preview) AppStatus
		wantErr string
	}{
		{"wrong Overseerr key", Sources{Overseerr: &Conn{URL: seerr.URL, APIKey: "nope-secret"}}, func(p Preview) AppStatus { return p.Overseerr.AppStatus }, "the API key was not accepted"},
		{"wrong Ombi key", Sources{Ombi: &Conn{URL: ombi.URL, APIKey: "nope-secret"}}, func(p Preview) AppStatus { return p.Ombi.AppStatus }, "the API key was not accepted"},
		{"Sonarr address in the Overseerr box", Sources{Overseerr: &Conn{URL: h.sonarr.URL, APIKey: sonarrKey}}, func(p Preview) AppStatus { return p.Overseerr.AppStatus }, "answered 404"},
		{"no key", Sources{Ombi: &Conn{URL: ombi.URL}}, func(p Preview) AppStatus { return p.Ombi.AppStatus }, "enter the API key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pv, err := h.im.Preview(context.Background(), Options{Sources: tt.src})
			if err != nil {
				t.Fatal(err)
			}
			st := tt.app(pv)
			if st.OK || !strings.Contains(st.Error, tt.wantErr) || strings.Contains(st.Error, "nope-secret") {
				t.Fatalf("got %+v, want error containing %q", st, tt.wantErr)
			}
		})
	}
}

func TestMergeRequests(t *testing.T) {
	tests := []struct {
		name string
		in   []requestEntry
		want []requestEntry
	}{
		{
			name: "same movie, most advanced status and both people",
			in: []requestEntry{
				{MediaType: "movie", TMDBID: 1, Status: RequestPending, By: []string{"A"}},
				{MediaType: "movie", TMDBID: 1, Status: RequestApproved, By: []string{"B", "A"}},
			},
			want: []requestEntry{{MediaType: "movie", TMDBID: 1, Status: RequestApproved, By: []string{"A", "B"}}},
		},
		{
			name: "a movie and a show with the same id are different titles",
			in: []requestEntry{
				{MediaType: "movie", TMDBID: 5, Status: RequestPending},
				{MediaType: "tv", TMDBID: 5, Status: RequestPending},
			},
			want: []requestEntry{{MediaType: "movie", TMDBID: 5, Status: RequestPending}, {MediaType: "tv", TMDBID: 5, Status: RequestPending}},
		},
		{
			name: "seasons are joined and sorted; a live request beats a declined one",
			in: []requestEntry{
				{MediaType: "tv", TVDBID: 7, Status: RequestDeclined, Seasons: []int{3}},
				{MediaType: "tv", TVDBID: 7, Status: RequestPending, Seasons: []int{2, 3}},
			},
			want: []requestEntry{{MediaType: "tv", TVDBID: 7, Status: RequestPending, Seasons: []int{2, 3}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mergeRequests(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestOverseerrStatus(t *testing.T) {
	mk := func(req, media int) overseerrRequest {
		var r overseerrRequest
		r.Status, r.Media.Status = req, media
		return r
	}
	tests := []struct {
		req, media int
		want       string
	}{
		{1, 2, RequestPending},
		{2, 2, RequestApproved},
		{2, 3, RequestProcessing},
		{2, 4, RequestPartiallyAvailable},
		{2, 5, RequestAvailable},
		{5, 1, RequestAvailable},
		{3, 1, RequestDeclined},
		{4, 1, RequestFailed},
		{0, 0, RequestPending},
	}
	for _, tt := range tests {
		if got := mk(tt.req, tt.media).status(); got != tt.want {
			t.Errorf("request %d / media %d: got %s, want %s", tt.req, tt.media, got, tt.want)
		}
	}
}

func TestNameList(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{{nil, ""}, {[]string{"A"}, "A"}, {[]string{"A", "B"}, "A and B"}, {[]string{"A", "B", "C"}, "A, B and C"}}
	for _, tt := range tests {
		if got := nameList(tt.in); got != tt.want {
			t.Errorf("nameList(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
