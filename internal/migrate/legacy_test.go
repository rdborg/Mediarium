package migrate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

const (
	medusaKey    = "medusa-fixture-key-0012"
	sickchillKey = "sickchill-fixture-key-0013"
)

// newFakeMedusa serves the paged /api/v2/series behind x-api-key.
func newFakeMedusa(t *testing.T) *fakeApp {
	t.Helper()
	f := &fakeApp{t: t, key: medusaKey}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if r.Header.Get("x-api-key") != medusaKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/api/v2/series" {
			http.NotFound(w, r)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if page < 1 || limit < 1 {
			t.Errorf("unexpected query %s", r.URL.RawQuery)
		}
		data, err := os.ReadFile(filepath.Join("testdata", "medusa", "series.json"))
		if err != nil {
			t.Fatal(err)
		}
		var all []json.RawMessage
		if err := json.Unmarshal(data, &all); err != nil {
			t.Fatal(err)
		}
		start := min((page-1)*limit, len(all))
		end := min(start+limit, len(all))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(all[start:end])
	}))
	t.Cleanup(f.Close)
	return f
}

// newFakeSickChill serves cmd=shows and cmd=show under /api/<key>/; a wrong
// key is answered with HTTP 200 and {"result": "denied"} as SickChill does.
func newFakeSickChill(t *testing.T) *fakeApp {
	t.Helper()
	f := &fakeApp{t: t, key: sickchillKey}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/api/"+sickchillKey+"/" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": "denied", "message": "You are not authorized to make this request", "data": {}}`))
			return
		}
		switch q := r.URL.Query(); q.Get("cmd") {
		case "shows":
			serveFixture(t, w, "sickchill/shows.json")
		case "show":
			serveFixture(t, w, "sickchill/show_"+q.Get("tvdbid")+".json")
		default:
			t.Errorf("unexpected SickChill command %q", q.Get("cmd"))
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func showByTitle(t *testing.T, items []TitleItem, title string) TitleItem {
	t.Helper()
	return titleByName(t, items, title)
}

func checkLegacyImport(t *testing.T, h *harness, st Status, app string) {
	t.Helper()
	if st.Step != StepDone || st.Results.Series != (Counts{Added: 2, Skipped: 2, FilesLinked: 3}) || len(st.Errors) != 0 {
		t.Fatalf("%s import: %+v errors=%v items=%+v", app, st.Results.Series, st.Errors, st.Items)
	}
	bb, ok, _ := h.lib.GetSeriesByTMDBID(1396)
	if !ok || !bb.Monitored {
		t.Fatalf("breaking bad: %+v", bb)
	}
	eps, _ := h.lib.ListEpisodes(bb.ID)
	downloaded := 0
	for _, e := range eps {
		if e.Status == library.StatusDownloaded {
			downloaded++
			if !strings.Contains(e.FilePath, "Breaking Bad") {
				t.Fatalf("episode file must stay in the show folder: %+v", e)
			}
		}
	}
	if downloaded != 3 {
		t.Fatalf("episodes registered in place: %d", downloaded)
	}
	old, ok, _ := h.lib.GetSeriesByTMDBID(5555)
	if !ok || old.Monitored {
		t.Fatalf("a paused show is added unmonitored: %+v", old)
	}
	oldEps, _ := h.lib.ListEpisodes(old.ID)
	for _, e := range oldEps {
		if e.Monitored {
			t.Fatalf("episodes of a paused show must not be monitored: %+v", e)
		}
	}
	if _, found, _ := h.lib.GetSeriesByTMDBID(7777); found {
		t.Fatal("a show whose folder was not found must not be added (it would be downloaded again)")
	}
}

func TestMedusaPreviewAndImport(t *testing.T) {
	h := newHarness(t)
	med := newFakeMedusa(t)
	msgs := recordActivity(h)
	src := Sources{Medusa: &Conn{URL: med.URL, APIKey: medusaKey}}
	ctx := context.Background()

	pv, err := h.im.Preview(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	m := pv.Medusa
	if m == nil || !m.OK || m.Summary != (Summary{Total: 4, Add: 2, Skip: 2}) {
		t.Fatalf("medusa preview: %+v", m)
	}
	if len(pv.SuggestedPathMap) != 1 || pv.SuggestedPathMap[0].From != "/data/media/tv" || pv.SuggestedPathMap[0].To != h.tvRoot {
		t.Fatalf("suggested path map: %+v", pv.SuggestedPathMap)
	}
	if len(m.RootFolders) != 1 || m.RootFolders[0].Path != "/data/media/tv" || !m.RootFolders[0].Exists || m.RootFolders[0].Titles != 4 || m.RootFolders[0].FoldersFound != 2 {
		t.Fatalf("root folders: %+v", m.RootFolders)
	}
	bb := showByTitle(t, m.Items, "Breaking Bad")
	if bb.Action != ActionAdd || bb.TVDBID != 81189 || bb.TMDBID != 1396 || bb.Year != 2008 || !bb.Monitored || !bb.FolderFound || !bb.InLibraryFolder || bb.Path != filepath.Join(h.tvRoot, "Breaking Bad") {
		t.Fatalf("breaking bad: %+v", bb)
	}
	old := showByTitle(t, m.Items, "Fixture Old Show")
	if old.Action != ActionAdd || old.TMDBID != 5555 || old.Monitored || !strings.Contains(old.Reason, "paused in Medusa") {
		t.Fatalf("paused show with a TMDB indexer: %+v", old)
	}
	if u := showByTitle(t, m.Items, "Unknown To TMDB"); u.Action != ActionSkip || !strings.Contains(u.Reason, "no TMDB id") {
		t.Fatalf("unknown show: %+v", u)
	}
	if g := showByTitle(t, m.Items, "Gone Show"); g.Action != ActionSkip || g.Reason != "folder not found at "+filepath.Join(h.tvRoot, "Gone Show") {
		t.Fatalf("show without a folder: %+v", g)
	}
	if strings.Contains(mustMarshal(t, pv), medusaKey) {
		t.Fatal("the preview must not echo the API key")
	}
	if shows, _ := h.lib.ListSeries(); len(shows) != 0 {
		t.Fatalf("preview added shows: %d", len(shows))
	}

	st, err := h.im.Run(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	checkLegacyImport(t, h, st, "medusa")
	if len(*msgs) < 2 || !strings.Contains((*msgs)[0], "added to library from Medusa") {
		t.Fatalf("activity: %v", *msgs)
	}

	st2, err := h.im.Run(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	if st2.Results.Series != (Counts{Existing: 2, Skipped: 2}) {
		t.Fatalf("second run: %+v", st2.Results.Series)
	}
	if shows, _ := h.lib.ListSeries(); len(shows) != 2 {
		t.Fatalf("shows after two runs: %d", len(shows))
	}
	med.assertOnlyGET(t)
	if strings.Contains(h.logs.String(), medusaKey) {
		t.Fatal("the API key was logged")
	}
}

func TestMedusaPagesThroughTheList(t *testing.T) {
	h := newHarness(t)
	med := newFakeMedusa(t)
	prev := medusaPageSize
	medusaPageSize = 2
	t.Cleanup(func() { medusaPageSize = prev })
	pv, err := h.im.Preview(context.Background(), Options{Sources: Sources{Medusa: &Conn{URL: med.URL, APIKey: medusaKey}}})
	if err != nil {
		t.Fatal(err)
	}
	if !pv.Medusa.OK || pv.Medusa.Summary.Total != 4 {
		t.Fatalf("all pages should be read: %+v", pv.Medusa)
	}
}

func TestMedusaLeftOutWithoutSeries(t *testing.T) {
	h := newHarness(t)
	med := newFakeMedusa(t)
	no := false
	if _, err := h.im.Run(context.Background(), Options{Sources: Sources{Medusa: &Conn{URL: med.URL, APIKey: medusaKey}}, Include: Include{Series: &no}}); err != nil {
		t.Fatal(err)
	}
	med.mu.Lock()
	defer med.mu.Unlock()
	if len(med.requests) != 0 {
		t.Fatalf("Medusa was read although shows were left out: %v", med.requests)
	}
}

func TestSickChillPreviewAndImport(t *testing.T) {
	h := newHarness(t)
	sc := newFakeSickChill(t)
	src := Sources{SickChill: &Conn{URL: sc.URL + "/", APIKey: sickchillKey}}
	ctx := context.Background()

	pv, err := h.im.Preview(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	s := pv.SickChill
	if s == nil || !s.OK || s.Summary != (Summary{Total: 3, Add: 2, Skip: 1}) {
		t.Fatalf("sickchill preview: %+v", s)
	}
	old := showByTitle(t, s.Items, "Fixture Old Show")
	if old.Year != 2015 || old.TVDBID != 12345 || old.TMDBID != 5555 || old.Monitored || old.Action != ActionAdd {
		t.Fatalf("show named with a year: %+v", old)
	}
	if bb := showByTitle(t, s.Items, "Breaking Bad"); bb.Action != ActionAdd || bb.TMDBID != 1396 || bb.Path != filepath.Join(h.tvRoot, "Breaking Bad") || !bb.FolderFound {
		t.Fatalf("breaking bad: %+v", bb)
	}
	if strings.Contains(mustMarshal(t, pv), sickchillKey) {
		t.Fatal("the preview must not echo the API key")
	}

	st, err := h.im.Run(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	// Three shows: Breaking Bad and Fixture Old Show are added, the unknown
	// one is skipped (the fourth skip of the Medusa list is not in this one).
	if st.Step != StepDone || st.Results.Series != (Counts{Added: 2, Skipped: 1, FilesLinked: 3}) || len(st.Errors) != 0 {
		t.Fatalf("sickchill import: %+v errors=%v items=%+v", st.Results.Series, st.Errors, st.Items)
	}
	if old, ok, _ := h.lib.GetSeriesByTMDBID(5555); !ok || old.Monitored {
		t.Fatalf("paused show: %+v", old)
	}
	st2, err := h.im.Run(ctx, Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	if st2.Results.Series != (Counts{Existing: 2, Skipped: 1}) {
		t.Fatalf("second run: %+v", st2.Results.Series)
	}
	sc.assertOnlyGET(t)
	if strings.Contains(h.logs.String(), sickchillKey) {
		t.Fatal("the API key was logged")
	}
}

func TestMedusaAndSickChillAddAShowOnce(t *testing.T) {
	h := newHarness(t)
	src := Sources{
		Medusa:    &Conn{URL: newFakeMedusa(t).URL, APIKey: medusaKey},
		SickChill: &Conn{URL: newFakeSickChill(t).URL, APIKey: sickchillKey},
	}
	pv, err := h.im.Preview(context.Background(), Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	if len(pv.SuggestedPathMap) != 1 {
		t.Fatalf("both apps share one folder mapping: %+v", pv.SuggestedPathMap)
	}
	st, err := h.im.Run(context.Background(), Options{Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	if shows, _ := h.lib.ListSeries(); len(shows) != 2 {
		t.Fatalf("shows: %d (%+v)", len(shows), st.Items)
	}
	if st.Results.Series.Added != 2 || st.Results.Series.Existing != 2 {
		t.Fatalf("results: %+v", st.Results.Series)
	}
}

func TestLegacyAppErrors(t *testing.T) {
	h := newHarness(t)
	med, sc := newFakeMedusa(t), newFakeSickChill(t)
	nothing := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(nothing.Close)
	tests := []struct {
		name    string
		src     Sources
		app     func(Preview) AppStatus
		wantErr string
	}{
		{"wrong Medusa key", Sources{Medusa: &Conn{URL: med.URL, APIKey: "nope-secret"}}, func(p Preview) AppStatus { return p.Medusa.AppStatus }, "the API key was not accepted"},
		{"wrong SickChill key", Sources{SickChill: &Conn{URL: sc.URL, APIKey: "nope-secret"}}, func(p Preview) AppStatus { return p.SickChill.AppStatus }, "the API key was not accepted"},
		{"no key", Sources{SickChill: &Conn{URL: sc.URL}}, func(p Preview) AppStatus { return p.SickChill.AppStatus }, "enter the API key"},
		{"wrong SickChill address", Sources{SickChill: &Conn{URL: nothing.URL, APIKey: sickchillKey}}, func(p Preview) AppStatus { return p.SickChill.AppStatus }, "answered 404"},
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

func TestYearFromTitle(t *testing.T) {
	tests := []struct {
		in    string
		title string
		year  int
	}{
		{"Fixture Old Show (2015)", "Fixture Old Show", 2015},
		{"Breaking Bad", "Breaking Bad", 0},
		{"Show (US)", "Show (US)", 0},
		{"1883", "1883", 0},
		{" Spaced (1999) ", "Spaced", 1999},
	}
	for _, tt := range tests {
		if title, year := yearFromTitle(tt.in); title != tt.title || year != tt.year {
			t.Errorf("yearFromTitle(%q) = %q, %d; want %q, %d", tt.in, title, year, tt.title, tt.year)
		}
	}
}

func TestMedusaShowIDs(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want legacyShow
	}{
		{"tvdb indexer", `{"id":{"tvdb":81189},"title":"BB","indexer":"tvdb","year":{"start":2008},"config":{"location":"/tv/BB","paused":true}}`,
			legacyShow{Title: "BB", Year: 2008, TVDBID: 81189, Location: "/tv/BB", Paused: true}},
		{"tmdb indexer", `{"id":{"tmdb":5555},"title":"T","indexer":"tmdb","config":{}}`, legacyShow{Title: "T", TMDBID: 5555}},
		{"tvdb with a tmdb external", `{"id":{"tvdb":7},"externals":{"tmdb":"70"},"title":"X","indexer":"tvdb","config":{}}`, legacyShow{Title: "X", TVDBID: 7, TMDBID: 70}},
		{"another indexer", `{"id":{"tvdb":0},"title":"Y","indexer":"tvmaze","config":{}}`, legacyShow{Title: "Y"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m medusaSeries
			if err := json.Unmarshal([]byte(tt.raw), &m); err != nil {
				t.Fatal(err)
			}
			if got := m.show(); got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
