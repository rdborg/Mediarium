package mediaservers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMapPath(t *testing.T) {
	mappings := []PathMapping{
		{From: "/movies", To: "/data/movies"},
		{From: "/movies/4k", To: "/data/uhd"},
		{From: "/tv", To: `D:\Media\TV`},
		{From: `C:\Library\Films`, To: "/mnt/films"},
	}
	tests := []struct {
		name, in, want string
	}{
		{"prefix replaced", "/movies/Heat (1995)", "/data/movies/Heat (1995)"},
		{"exact folder", "/movies", "/data/movies"},
		{"longest mapping wins", "/movies/4k/Dune (2021)", "/data/uhd/Dune (2021)"},
		{"no partial folder-name match", "/movies-old/Heat (1995)", "/movies-old/Heat (1995)"},
		{"unmapped path unchanged", "/downloads/x", "/downloads/x"},
		{"windows target gets backslashes", "/tv/Show (2020)/Season 01", `D:\Media\TV\Show (2020)\Season 01`},
		{"windows source is case-insensitive", `c:\library\films\Heat (1995)`, "/mnt/films/Heat (1995)"},
		{"trailing slash on input", "/movies/Heat (1995)/", "/data/movies/Heat (1995)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MapPath(tc.in, mappings); got != tc.want {
				t.Fatalf("MapPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
	if got := MapPath("/movies/x", nil); got != "/movies/x" {
		t.Fatalf("no mappings should leave the path alone, got %q", got)
	}
	if got := MapPath("/movies/x", []PathMapping{{From: "/", To: "/share"}}); got != "/share/movies/x" {
		t.Fatalf("root mapping: got %q", got)
	}
}

func TestNormalizeURLAndPathMap(t *testing.T) {
	urls := []struct {
		in, want string
		ok       bool
	}{
		{"http://192.168.1.10:32400/", "http://192.168.1.10:32400", true},
		{"plex.local:32400", "http://plex.local:32400", true},
		{"https://jf.example.com/jellyfin/", "https://jf.example.com/jellyfin", true},
		{"ftp://x", "", false},
		{"", "", false},
		{"http://", "", false},
	}
	for _, tc := range urls {
		got, err := NormalizeURL(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("NormalizeURL(%q) = %q, %v; want %q ok=%v", tc.in, got, err, tc.want, tc.ok)
		}
	}

	got, err := NormalizePathMap([]PathMapping{{From: " /movies/ ", To: "/data/movies/"}, {}, {From: `D:\TV\`, To: "/tv"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []PathMapping{{From: "/movies", To: "/data/movies"}, {From: `D:\TV`, To: "/tv"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizePathMap = %+v, want %+v", got, want)
	}
	if _, err := NormalizePathMap([]PathMapping{{From: "/movies"}}); err == nil {
		t.Fatal("a mapping with only one side must be refused")
	}
}

func TestPlexTMDBID(t *testing.T) {
	tests := []struct {
		guid string
		id   int
		ok   bool
	}{
		{"tmdb://603", 603, true},
		{"com.plexapp.agents.themoviedb://1399?lang=en", 1399, true},
		{"imdb://tt0133093", 0, false},
		{"plex://movie/5d776825880197001ec967c6", 0, false},
		{"tmdb://abc", 0, false},
	}
	for _, tc := range tests {
		id, ok := plexTMDBID(tc.guid)
		if id != tc.id || ok != tc.ok {
			t.Errorf("plexTMDBID(%q) = %d, %v; want %d, %v", tc.guid, id, ok, tc.id, tc.ok)
		}
	}
}

func TestConnectionTest(t *testing.T) {
	plex := newFakePlex(t, "plex-token")
	plex.sections = []Library{{ID: "1", Title: "Movies", Type: "movie", Locations: []string{"/data/movies"}}}
	plexXML := newFakePlex(t, "plex-token")
	plexXML.xml = true
	plexXML.sections = plex.sections
	jelly := newFakeEmby(t, KindJellyfin, "jf-key")
	jellyStrict := newFakeEmby(t, KindJellyfin, "jf-key")
	jellyStrict.headerOnly = true
	emby := newFakeEmby(t, KindEmby, "emby-key")
	notAServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>router login</body>"))
	}))
	t.Cleanup(notAServer.Close)
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()

	with := func(s Server, f func(*Server)) Server { f(&s); return s }

	tests := []struct {
		name       string
		srv        Server
		wantErr    string // substring of the friendly message; "" = success
		wantID     string
		wantLibs   int
		wantServer string
	}{
		{name: "plex ok", srv: plex.server(), wantID: "abc123machine", wantLibs: 1, wantServer: "Living Room"},
		{name: "plex answering XML", srv: plexXML.server(), wantID: "abc123machine", wantLibs: 1},
		{name: "plex wrong token", srv: with(plex.server(), func(s *Server) { s.Token = "nope" }), wantErr: "Plex refused the token"},
		{name: "plex no token", srv: with(plex.server(), func(s *Server) { s.Token = "" }), wantErr: "needs a token"},
		{name: "not a plex server", srv: with(plex.server(), func(s *Server) { s.BaseURL = notAServer.URL }), wantErr: "does not look like a Plex server"},
		{name: "cannot reach", srv: with(plex.server(), func(s *Server) { s.BaseURL = closedURL }), wantErr: "Could not reach Plex"},
		{name: "jellyfin ok", srv: jelly.server(), wantID: "srv-jellyfin", wantLibs: 1, wantServer: "Den"},
		{name: "jellyfin with legacy header off", srv: jellyStrict.server(), wantID: "srv-jellyfin", wantLibs: 1},
		{name: "jellyfin wrong key", srv: with(jelly.server(), func(s *Server) { s.Token = "bad" }), wantErr: "Jellyfin refused the API key"},
		{name: "emby ok", srv: emby.server(), wantID: "srv-emby", wantLibs: 1},
		{name: "emby chosen for a jellyfin server", srv: with(jelly.server(), func(s *Server) { s.Kind = KindEmby }), wantErr: "This is a Jellyfin server"},
		{name: "jellyfin pointed at plex", srv: with(plex.server(), func(s *Server) { s.Kind = KindJellyfin }), wantErr: "refused the API key"},
		{name: "emby pointed at a web page", srv: with(emby.server(), func(s *Server) { s.BaseURL = notAServer.URL }), wantErr: "does not look like an Emby server"},
	}
	c := NewClient("test")
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := c.Test(context.Background(), tc.srv)
			if tc.wantErr != "" {
				var ue *UserError
				if err == nil || !errors.As(err, &ue) || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want a friendly error containing %q, got %v", tc.wantErr, err)
				}
				if tc.srv.Token != "" && strings.Contains(err.Error(), tc.srv.Token) {
					t.Fatalf("the error must never contain the token: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.ServerID != tc.wantID || len(res.Libraries) != tc.wantLibs {
				t.Fatalf("result %+v", res)
			}
			if tc.wantServer != "" && res.ServerName != tc.wantServer {
				t.Fatalf("server name %q, want %q", res.ServerName, tc.wantServer)
			}
		})
	}
}

func TestPlexRefreshFolders(t *testing.T) {
	plex := newFakePlex(t, "tok")
	plex.sections = []Library{
		{ID: "1", Title: "Movies", Type: "movie", Locations: []string{"/data/movies"}},
		{ID: "2", Title: "TV", Type: "show", Locations: []string{"/data/tv", "/more/tv"}},
		{ID: "3", Title: "Kids Movies", Type: "movie", Locations: []string{"/data/kids"}},
	}
	c := NewClient("test")

	tests := []struct {
		name    string
		kind    MediaKind
		pathMap []PathMapping
		folders []string
		want    []string
	}{
		{"mapped folder scans just that folder", MediaMovie, []PathMapping{{From: "/movies", To: "/data/movies"}},
			[]string{"/movies/Heat (1995)"}, []string{"1 /data/movies/Heat (1995)"}},
		{"same paths need no mapping", MediaTV, nil,
			[]string{"/data/tv/Show (2020)/Season 01"}, []string{"2 /data/tv/Show (2020)/Season 01"}},
		{"unmatched folder scans every library of that type", MediaMovie, nil,
			[]string{"/movies/Heat (1995)"}, []string{"1", "3"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plex.mu.Lock()
			plex.refreshes = nil
			plex.mu.Unlock()
			s := plex.server()
			s.PathMap = tc.pathMap
			done, err := c.RefreshFolders(context.Background(), s, tc.kind, tc.folders)
			if err != nil {
				t.Fatal(err)
			}
			if len(done) != len(tc.want) {
				t.Fatalf("done = %v", done)
			}
			plex.mu.Lock()
			got := append([]string(nil), plex.refreshes...)
			plex.mu.Unlock()
			sort.Strings(got)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("refreshes = %q, want %q", got, tc.want)
			}
		})
	}

	// Refresh now: every library, whole.
	plex.refreshes = nil
	if err := c.RefreshAll(context.Background(), plex.server()); err != nil {
		t.Fatal(err)
	}
	if len(plex.refreshes) != 3 {
		t.Fatalf("refresh all should scan all 3 libraries: %q", plex.refreshes)
	}
}

func TestEmbyRefreshFolders(t *testing.T) {
	c := NewClient("test")
	for _, kind := range []Kind{KindJellyfin, KindEmby} {
		t.Run(string(kind), func(t *testing.T) {
			f := newFakeEmby(t, kind, "key")
			f.locations = []string{"/media/movies", "/media/tv"}
			s := f.server()
			s.PathMap = []PathMapping{{From: "/tv", To: "/media/tv"}}
			if _, err := c.RefreshFolders(context.Background(), s, MediaTV, []string{"/tv/Show/Season 01", "/tv/Show/Season 01"}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(f.updated, []string{"/media/tv/Show/Season 01"}) || f.refreshes != 0 {
				t.Fatalf("updated=%q refreshes=%d", f.updated, f.refreshes)
			}

			// An old server without the folder scan gets a full scan instead.
			f.noMediaScan = true
			done, err := c.RefreshFolders(context.Background(), s, MediaTV, []string{"/tv/Other"})
			if err != nil || f.refreshes != 1 || len(done) != 1 {
				t.Fatalf("fallback: done=%v err=%v refreshes=%d", done, err, f.refreshes)
			}

			// Refresh now.
			if err := c.RefreshAll(context.Background(), s); err != nil || f.refreshes != 2 {
				t.Fatalf("refresh all: err=%v refreshes=%d", err, f.refreshes)
			}
		})
	}
}

// A folder the server's libraries do not hold (it sees the files under another
// name and there is no folder mapping) would be accepted and then ignored, so
// the whole library is scanned instead.
func TestEmbyRefreshFoldersOutsideEveryLibrary(t *testing.T) {
	c := NewClient("test")
	tests := []struct {
		name        string
		locations   []string
		noFolders   bool
		pathMap     []PathMapping
		folders     []string
		wantUpdated []string
		wantFull    int
	}{
		{"unmapped folder gets a full scan", []string{"/volume1/Media/Movies"}, false, nil,
			[]string{"/data/Movies/Heat (1995)"}, nil, 1},
		{"mapped folder is scanned on its own", []string{"/volume1/Media/Movies"}, false,
			[]PathMapping{{From: "/data/Movies", To: "/volume1/Media/Movies"}},
			[]string{"/data/Movies/Heat (1995)"}, []string{"/volume1/Media/Movies/Heat (1995)"}, 0},
		{"one inside and one outside: both are handled", []string{"/media/movies"}, false, nil,
			[]string{"/media/movies/Heat (1995)", "/elsewhere/Show"}, []string{"/media/movies/Heat (1995)"}, 1},
		{"a library folder is not confused with a longer name", []string{"/media/movies"}, false, nil,
			[]string{"/media/movies2/Heat (1995)"}, nil, 1},
		{"a server that will not list its libraries is trusted", nil, true, nil,
			[]string{"/data/Movies/Heat (1995)"}, []string{"/data/Movies/Heat (1995)"}, 0},
	}
	for _, kind := range []Kind{KindJellyfin, KindEmby} {
		for _, tc := range tests {
			t.Run(string(kind)+"/"+tc.name, func(t *testing.T) {
				f := newFakeEmby(t, kind, "key")
				f.locations, f.noFolders = tc.locations, tc.noFolders
				s := f.server()
				s.PathMap = tc.pathMap
				done, err := c.RefreshFolders(context.Background(), s, MediaMovie, tc.folders)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(f.updated, tc.wantUpdated) || f.refreshes != tc.wantFull {
					t.Fatalf("updated=%q refreshes=%d, want %q and %d (done=%q)", f.updated, f.refreshes, tc.wantUpdated, tc.wantFull, done)
				}
				if len(done) == 0 {
					t.Fatal("nothing reported as started")
				}
			})
		}
	}
}

func TestFindLinks(t *testing.T) {
	plex := newFakePlex(t, "tok")
	plex.sections = []Library{
		{ID: "1", Title: "Movies", Type: "movie", Locations: []string{"/data/movies"}},
		{ID: "2", Title: "TV", Type: "show", Locations: []string{"/data/tv"}},
	}
	plex.items["1"] = []map[string]any{
		{"ratingKey": "101", "type": "movie", "guid": "plex://movie/abc", "Guid": []map[string]any{{"id": "imdb://tt0133093"}, {"id": "tmdb://603"}}},
		{"ratingKey": "102", "type": "movie", "guid": "com.plexapp.agents.themoviedb://949?lang=en"},
	}
	plex.items["2"] = []map[string]any{
		{"ratingKey": "201", "type": "show", "guid": "plex://show/xyz", "Guid": []map[string]any{{"id": "tmdb://1399"}}},
	}
	plexXML := newFakePlex(t, "tok")
	plexXML.xml = true
	plexXML.sections, plexXML.items = plex.sections, plex.items

	jelly := newFakeEmby(t, KindJellyfin, "k")
	jelly.items = []embyItem{{ID: "jf-603", ServerID: "srv-jellyfin", Type: "Movie", ProviderIDs: map[string]string{"Tmdb": "603", "Imdb": "tt0133093"}}}
	emby := newFakeEmby(t, KindEmby, "k")
	emby.items = []embyItem{{ID: "em-1399", ServerID: "srv-emby", Type: "Series", ProviderIDs: map[string]string{"tmdb": "1399"}}}

	withPublic := func(s Server, u string) Server { s.PublicURL = u; return s }
	c := NewClient("test")
	appLink := "https://app.plex.tv/desktop/#!/server/abc123machine/details?key=%2Flibrary%2Fmetadata%2F"
	tests := []struct {
		name    string
		srv     Server
		kind    MediaKind
		tmdb    int
		wantURL string // "" = not found
		wantApp string
	}{
		{"plex movie by new-agent guid", plex.server(), MediaMovie, 603, appLink + "101", appLink + "101"},
		{"plex movie by legacy guid", plex.server(), MediaMovie, 949, appLink + "102", appLink + "102"},
		{"plex show", plex.server(), MediaTV, 1399, appLink + "201", appLink + "201"},
		{"plex show id is not a movie", plex.server(), MediaMovie, 1399, "", ""},
		{"plex xml", plexXML.server(), MediaTV, 1399, appLink + "201", appLink + "201"},
		{"plex with public address", withPublic(plex.server(), "https://plex.example.com/"), MediaMovie, 603,
			"https://plex.example.com/web/index.html#!/server/abc123machine/details?key=%2Flibrary%2Fmetadata%2F101", appLink + "101"},
		{"jellyfin movie", withPublic(jelly.server(), "https://jf.example.com"), MediaMovie, 603, "https://jf.example.com/web/#/details?id=jf-603&serverId=srv-jellyfin", ""},
		{"jellyfin missing", jelly.server(), MediaMovie, 1, "", ""},
		{"emby show", emby.server(), MediaTV, 1399, emby.srv.URL + "/web/index.html#!/item?id=em-1399&serverId=srv-emby", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			item, found, err := c.Find(context.Background(), tc.srv, tc.kind, tc.tmdb)
			if err != nil {
				t.Fatal(err)
			}
			if found != (tc.wantURL != "") || item.URL != tc.wantURL || item.AppURL != tc.wantApp {
				t.Fatalf("found=%v item=%+v, want url %q app %q", found, item, tc.wantURL, tc.wantApp)
			}
		})
	}
}

func TestFinderCachesAndSkipsBrokenServers(t *testing.T) {
	plex := newFakePlex(t, "tok")
	plex.sections = []Library{{ID: "1", Title: "Movies", Type: "movie"}}
	plex.items["1"] = []map[string]any{{"ratingKey": "7", "type": "movie", "Guid": []map[string]any{{"id": "tmdb://603"}}}}
	jelly := newFakeEmby(t, KindJellyfin, "k")
	jelly.items = []embyItem{{ID: "j1", Type: "Movie", ProviderIDs: map[string]string{"Tmdb": "603"}}}
	broken := Server{ID: 9, Name: "Down", Kind: KindEmby, BaseURL: "http://127.0.0.1:1", Enabled: true}
	disabled := jelly.server()
	disabled.ID, disabled.Enabled = 10, false

	plexSrv := plex.server()
	plexSrv.ServerID = "abc123machine"
	servers := []Server{plexSrv, broken, jelly.server(), disabled}
	f := NewFinder(NewClient("test"))

	links := f.Links(context.Background(), servers, MediaMovie, 603)
	if len(links) != 2 || links[0].Kind != KindPlex || links[1].Kind != KindJellyfin {
		t.Fatalf("links = %+v", links)
	}
	// Another title on the same Plex server reuses the library index.
	_ = f.Links(context.Background(), servers, MediaMovie, 604)
	_ = f.Links(context.Background(), servers, MediaMovie, 603)
	if plex.listCalls != 1 || jelly.itemCalls != 2 {
		t.Fatalf("cache not used: plex listings=%d jellyfin lookups=%d", plex.listCalls, jelly.itemCalls)
	}
	f.Invalidate()
	_ = f.Links(context.Background(), servers, MediaMovie, 603)
	if plex.listCalls != 2 || jelly.itemCalls != 3 {
		t.Fatalf("invalidate should ask again: plex listings=%d jellyfin lookups=%d", plex.listCalls, jelly.itemCalls)
	}

	home := HomeLinks(servers)
	if len(home) != 3 || home[0].URL != "https://app.plex.tv/desktop/#!/media/abc123machine/com.plexapp.plugins.library" || home[2].URL != jelly.srv.URL+"/web/" {
		t.Fatalf("home links = %+v", home)
	}
}

// memStore is an in-memory ServerStore.
type memStore struct {
	mu      sync.Mutex
	servers []Server
	checks  map[int64]string // id -> last error ("" = ok)
}

func (m *memStore) List() ([]Server, error) { return m.servers, nil }
func (m *memStore) RecordCheck(id int64, _ string, err error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.checks == nil {
		m.checks = map[int64]string{}
	}
	m.checks[id] = ""
	if err != nil {
		m.checks[id] = err.Error()
	}
	return nil
}

func TestRefresherDebouncesPerFolder(t *testing.T) {
	plex := newFakePlex(t, "tok")
	plex.sections = []Library{
		{ID: "1", Title: "Movies", Type: "movie", Locations: []string{"/data/movies"}},
		{ID: "2", Title: "TV", Type: "show", Locations: []string{"/data/tv"}},
	}
	jelly := newFakeEmby(t, KindJellyfin, "k")
	jelly.locations = []string{"/movies", "/tv"}
	noAuto := newFakeEmby(t, KindEmby, "k")

	p := plex.server()
	p.PathMap = []PathMapping{{From: "/tv", To: "/data/tv"}, {From: "/movies", To: "/data/movies"}}
	j := jelly.server()
	n := noAuto.server()
	n.ID, n.RefreshAfterImport = 3, false
	down := Server{ID: 4, Name: "Down", Kind: KindJellyfin, BaseURL: "http://127.0.0.1:1", Token: "x", Enabled: true, RefreshAfterImport: true}
	store := &memStore{servers: []Server{p, j, n, down}}

	r := NewRefresher(store, NewClient("test"))
	r.Delay = time.Hour // only Flush runs it, so the test is not timing-dependent
	done := 0
	r.OnDone = func() { done++ }

	season := filepath.Join("/tv", "Show (2020)", "Season 01")
	for _, ep := range []string{"Show - S01E01.mkv", "Show - S01E02.mkv", "Show - S01E03.mkv"} {
		r.Imported(MediaTV, filepath.Join(season, ep))
	}
	r.Imported(MediaMovie, filepath.Join("/movies", "Heat (1995)", "Heat (1995).mkv"))
	r.Flush()

	sort.Strings(plex.refreshes)
	wantPlex := []string{"1 /data/movies/Heat (1995)", "2 /data/tv/Show (2020)/Season 01"}
	if !reflect.DeepEqual(plex.refreshes, wantPlex) {
		t.Fatalf("plex refreshes = %q, want %q", plex.refreshes, wantPlex)
	}
	sort.Strings(jelly.updated)
	wantJelly := []string{filepath.Join("/movies", "Heat (1995)"), season}
	sort.Strings(wantJelly)
	if !reflect.DeepEqual(jelly.updated, wantJelly) {
		t.Fatalf("jellyfin updates = %q, want %q", jelly.updated, wantJelly)
	}
	if len(noAuto.updated) != 0 {
		t.Fatalf("a server with refresh-after-import off must not be asked: %q", noAuto.updated)
	}
	if store.checks[p.ID] != "" || store.checks[j.ID] != "" || !strings.Contains(store.checks[down.ID], "Could not reach") {
		t.Fatalf("recorded checks = %+v", store.checks)
	}
	if _, asked := store.checks[n.ID]; asked || done != 1 {
		t.Fatalf("checks=%+v onDone=%d", store.checks, done)
	}

	// Nothing waiting: Flush is a no-op.
	r.Flush()
	if done != 1 {
		t.Fatalf("an empty flush should not refresh")
	}
}

func TestRefresherRunsOnItsOwn(t *testing.T) {
	jelly := newFakeEmby(t, KindJellyfin, "k")
	jelly.locations = []string{"/movies"}
	store := &memStore{servers: []Server{jelly.server()}}
	r := NewRefresher(store, NewClient("test"))
	r.Delay = 20 * time.Millisecond
	r.Imported(MediaMovie, "/movies/A (2000)/A.mkv")
	r.Imported(MediaMovie, "/movies/A (2000)/A.srt")
	deadline := time.Now().Add(5 * time.Second)
	for {
		jelly.mu.Lock()
		n := len(jelly.updated)
		jelly.mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.Flush()
	if len(jelly.updated) != 1 {
		t.Fatalf("want one folder scan, got %q", jelly.updated)
	}
}
