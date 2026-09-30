package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

type bulkTitle struct {
	Kind string `json:"kind"`
	ID   int64  `json:"id"`
}

func movieRef(id int64) bulkTitle { return bulkTitle{"movie", id} }
func showRef(id int64) bulkTitle  { return bulkTitle{"tv", id} }

func seedShow(t *testing.T, server *api.Server, tmdbID int, title string, eps int, downloaded int) int64 {
	t.Helper()
	var list []library.Episode
	for i := 1; i <= eps; i++ {
		list = append(list, library.Episode{Season: 1, Episode: i, Title: fmt.Sprintf("Episode %d", i), AirDate: "2001-01-01"})
	}
	s, err := server.MovieRepo.AddSeries(library.Series{TMDBID: tmdbID, Title: title, Year: 2001, Monitored: true}, list)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := server.MovieRepo.ListEpisodes(s.ID)
	for i := 0; i < downloaded; i++ {
		if err := server.MovieRepo.SetEpisodeStatus(stored[i].ID, library.StatusDownloaded, "WEBDL-1080p", ""); err != nil {
			t.Fatal(err)
		}
	}
	return s.ID
}

type bulkAnswer struct {
	Updated int `json:"updated"`
	Failed  []struct {
		Kind   string `json:"kind"`
		ID     int64  `json:"id"`
		Title  string `json:"title"`
		Reason string `json:"reason"`
	} `json:"failed"`
	Message string `json:"message"`
}

func bulkCall(t *testing.T, client *http.Client, method, url string, body any, want int) bulkAnswer {
	t.Helper()
	return postJSONMethod[bulkAnswer](t, client, method, url, body, want)
}

func TestBulkChangesUpdateManyTitlesInOneRequest(t *testing.T) {
	server, base, client := loginNewServer(t)
	m1 := seedMovie(t, server.MovieRepo, 1, "One", 2001, "")
	m2 := seedMovie(t, server.MovieRepo, 2, "Two", 2002, "")
	m3 := seedMovie(t, server.MovieRepo, 3, "Three", 2003, "")
	sh := seedShow(t, server, 10, "Show", 2, 0)
	profiles := getJSON[map[string]any](t, client, base+"/api/quality-profiles")
	profileID := int64(profiles["profiles"].([]any)[0].(map[string]any)["id"].(float64))

	all := []bulkTitle{movieRef(m1), movieRef(m2), movieRef(m3), showRef(sh)}
	movie := func(id int64) library.Movie {
		m, err := server.MovieRepo.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	show := func() library.Series {
		s, err := server.MovieRepo.GetSeries(sh)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	cases := []struct {
		name    string
		method  string
		path    string
		body    map[string]any
		status  int
		updated int
		failed  int
		check   func(t *testing.T)
	}{
		{"stop monitoring", "PUT", "/monitored", map[string]any{"items": all, "monitored": false}, 200, 4, 0, func(t *testing.T) {
			if movie(m1).Monitored || movie(m3).Monitored || show().Monitored {
				t.Fatal("everything should be unmonitored")
			}
		}},
		{"monitor two of them again", "PUT", "/monitored", map[string]any{"items": []bulkTitle{movieRef(m1), showRef(sh)}, "monitored": true}, 200, 2, 0, func(t *testing.T) {
			if !movie(m1).Monitored || movie(m2).Monitored || !show().Monitored {
				t.Fatal("only the two chosen titles should be monitored again")
			}
		}},
		{"a title that is gone is reported, the rest are changed", "PUT", "/monitored", map[string]any{"items": []bulkTitle{movieRef(m2), movieRef(9999), showRef(8888)}, "monitored": true}, 200, 1, 2, func(t *testing.T) {
			if !movie(m2).Monitored {
				t.Fatal("the title that exists should still be changed")
			}
		}},
		{"the same title twice counts once", "PUT", "/monitored", map[string]any{"items": []bulkTitle{movieRef(m1), movieRef(m1)}, "monitored": false}, 200, 1, 0, nil},
		{"no better versions", "PUT", "/no-upgrade", map[string]any{"items": all, "noUpgrade": true}, 200, 4, 0, func(t *testing.T) {
			if !movie(m1).NoUpgrade || !movie(m3).NoUpgrade || !show().NoUpgrade {
				t.Fatal("no-upgrade should be on for all")
			}
		}},
		{"better versions back on", "PUT", "/no-upgrade", map[string]any{"items": []bulkTitle{movieRef(m1)}, "noUpgrade": false}, 200, 1, 0, func(t *testing.T) {
			if movie(m1).NoUpgrade || !movie(m2).NoUpgrade {
				t.Fatal("only the chosen title should have it switched back")
			}
		}},
		{"quality profile", "PUT", "/profile", map[string]any{"items": all, "profileId": profileID}, 200, 4, 0, func(t *testing.T) {
			if movie(m2).ProfileID != profileID || show().ProfileID != profileID {
				t.Fatal("profile not applied")
			}
		}},
		{"a profile that does not exist changes nothing", "PUT", "/profile", map[string]any{"items": all, "profileId": 99999}, 400, 0, 0, func(t *testing.T) {
			if movie(m2).ProfileID != profileID {
				t.Fatal("nothing should change when the profile is refused")
			}
		}},
		{"back to the default profile", "PUT", "/profile", map[string]any{"items": all, "profileId": 0}, 200, 4, 0, func(t *testing.T) {
			if movie(m2).ProfileID != 0 || show().ProfileID != 0 {
				t.Fatal("profile not cleared")
			}
		}},
		{"download from torrents", "PUT", "/sources", map[string]any{"items": all, "sources": "torrent"}, 200, 4, 0, func(t *testing.T) {
			if movie(m2).SourcePref != "torrent" || show().SourcePref != "torrent" {
				t.Fatal("sources not applied")
			}
		}},
		{"use the default downloaders again", "PUT", "/sources", map[string]any{"items": all, "sources": ""}, 200, 4, 0, func(t *testing.T) {
			if movie(m2).SourcePref != "" {
				t.Fatal("sources not cleared")
			}
		}},
		{"an unknown downloader is refused", "PUT", "/sources", map[string]any{"items": all, "sources": "carrier-pigeon"}, 400, 0, 0, func(t *testing.T) {
			if movie(m2).SourcePref != "" {
				t.Fatal("nothing should change")
			}
		}},
		{"no titles chosen", "PUT", "/monitored", map[string]any{"items": []bulkTitle{}, "monitored": true}, 400, 0, 0, nil},
		{"a value must be given", "PUT", "/monitored", map[string]any{"items": all}, 400, 0, 0, nil},
		{"an unknown kind of title", "PUT", "/monitored", map[string]any{"items": []bulkTitle{{"album", 1}}, "monitored": true}, 400, 0, 0, nil},
		{"a title id of zero", "PUT", "/no-upgrade", map[string]any{"items": []bulkTitle{movieRef(0)}, "noUpgrade": true}, 400, 0, 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bulkCall(t, client, tc.method, base+"/api/library/bulk"+tc.path, tc.body, tc.status)
			if tc.status == http.StatusOK {
				if got.Updated != tc.updated || len(got.Failed) != tc.failed {
					t.Fatalf("updated %d, failed %v; want %d updated and %d failed", got.Updated, got.Failed, tc.updated, tc.failed)
				}
				if tc.failed == 0 && got.Message != fmt.Sprintf("Done: %d updated.", tc.updated) {
					t.Fatalf("message = %q", got.Message)
				}
				if tc.failed > 0 && (!strings.HasPrefix(got.Message, "Done: 1 updated. 2 titles could not be changed.") || got.Failed[0].Reason == "") {
					t.Fatalf("message = %q, failed = %+v", got.Message, got.Failed)
				}
			}
			if tc.check != nil {
				tc.check(t)
			}
		})
	}
}

func TestBulkChangesAreAdminOnly(t *testing.T) {
	server, base, admin, member, _ := familyServer(t)
	id := seedMovie(t, server.MovieRepo, 1, "One", 2001, "")
	items := []bulkTitle{movieRef(id)}
	calls := []struct {
		method, path string
		body         map[string]any
	}{
		{"PUT", "/monitored", map[string]any{"items": items, "monitored": false}},
		{"PUT", "/no-upgrade", map[string]any{"items": items, "noUpgrade": true}},
		{"PUT", "/profile", map[string]any{"items": items, "profileId": 0}},
		{"PUT", "/sources", map[string]any{"items": items, "sources": "usenet"}},
		{"POST", "/search-now", map[string]any{"items": items}},
		{"POST", "/remove", map[string]any{"items": items, "deleteFiles": true}},
	}
	for _, c := range calls {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			status, body := doJSONStatus(t, member, c.method, base+"/api/library/bulk"+c.path, c.body)
			if status != http.StatusForbidden || body["error"] != "Only an administrator can do this." {
				t.Fatalf("a member should be refused, got %d %v", status, body)
			}
		})
	}
	m, err := server.MovieRepo.Get(id)
	if err != nil || !m.Monitored || m.NoUpgrade || m.SourcePref != "" {
		t.Fatalf("a refused request must change nothing: %+v %v", m, err)
	}
	// The administrator can.
	bulkCall(t, admin, "PUT", base+"/api/library/bulk/monitored", map[string]any{"items": items, "monitored": false}, http.StatusOK)
}

func TestBulkRemoveNeverDeletesFilesUnlessAsked(t *testing.T) {
	server, base, client := loginNewServer(t)
	movies := server.TestMoviesRoot()
	makeMovie := func(tmdb int, name string) (int64, string) {
		file := filepath.Join(movies, name+" (2001)", name+" (2001).mkv")
		putFile(t, file, "x")
		return seedMovie(t, server.MovieRepo, tmdb, name, 2001, file), file
	}
	remove := func(body map[string]any) bulkAnswer {
		return bulkCall(t, client, "POST", base+"/api/library/bulk/remove", body, http.StatusOK)
	}

	// Nothing said about files: they stay.
	a, fileA := makeMovie(1, "Alpha")
	b, fileB := makeMovie(2, "Beta")
	sh := seedShow(t, server, 20, "Gamma", 2, 0)
	got := remove(map[string]any{"items": []bulkTitle{movieRef(a), movieRef(b), showRef(sh)}})
	if got.Updated != 3 || len(got.Failed) != 0 || got.Message != "Done: 3 removed." {
		t.Fatalf("answer: %+v", got)
	}
	if !exists(fileA) || !exists(fileB) {
		t.Fatal("files were deleted although the request did not ask for it")
	}
	if list, _ := server.MovieRepo.List(); len(list) != 0 {
		t.Fatalf("the titles should be gone from the library, %d left", len(list))
	}

	// An explicit false is the same.
	c, fileC := makeMovie(3, "Delta")
	remove(map[string]any{"items": []bulkTitle{movieRef(c)}, "deleteFiles": false})
	if !exists(fileC) {
		t.Fatal("deleteFiles false must keep the files")
	}

	// Asked for, and only then, the files go: with a title that is gone
	// reported and the rest still removed.
	d, fileD := makeMovie(4, "Epsilon")
	e, fileE := makeMovie(5, "Zeta")
	got = remove(map[string]any{"items": []bulkTitle{movieRef(d), movieRef(9999), movieRef(e)}, "deleteFiles": true})
	if got.Updated != 2 || len(got.Failed) != 1 || got.Failed[0].ID != 9999 || got.Failed[0].Reason == "" {
		t.Fatalf("answer: %+v", got)
	}
	if !strings.Contains(got.Message, "Done: 2 removed, with their files.") || !strings.Contains(got.Message, "1 title could not be removed") {
		t.Fatalf("message = %q", got.Message)
	}
	if exists(fileD) || exists(fileE) {
		t.Fatal("the files should be deleted when the request asks for it")
	}
	if !exists(fileA) || !exists(fileC) {
		t.Fatal("other titles' files must stay")
	}

	// A file outside the library folder is never deleted; the others go on.
	outside := filepath.Join(t.TempDir(), "Outside (2001).mkv")
	putFile(t, outside, "not yours")
	out := seedMovie(t, server.MovieRepo, 6, "Outside", 2001, outside)
	f, fileF := makeMovie(7, "Eta")
	got = remove(map[string]any{"items": []bulkTitle{movieRef(out), movieRef(f)}, "deleteFiles": true})
	if got.Updated != 1 || len(got.Failed) != 1 || got.Failed[0].ID != out || got.Failed[0].Title != "Outside" {
		t.Fatalf("answer: %+v", got)
	}
	if !exists(outside) || exists(fileF) {
		t.Fatal("the file outside the library must stay and the other title's files must go")
	}
	if _, err := server.MovieRepo.Get(out); err != nil {
		t.Fatalf("a title whose files could not be deleted stays in the library: %v", err)
	}

	// Nothing chosen.
	bulkCall(t, client, "POST", base+"/api/library/bulk/remove", map[string]any{"items": []bulkTitle{}}, http.StatusBadRequest)
}

// countingIndexer answers every search with no results and remembers what was
// searched for.
type countingIndexer struct {
	*httptest.Server
	mu      sync.Mutex
	queries []string
}

func newCountingIndexer(t *testing.T) *countingIndexer {
	t.Helper()
	ci := &countingIndexer{}
	ci.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("q"); q != "" {
			ci.mu.Lock()
			ci.queries = append(ci.queries, q)
			ci.mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<?xml version="1.0"?><rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel></channel></rss>`)
	}))
	t.Cleanup(ci.Close)
	return ci
}

func (ci *countingIndexer) searched(title string) bool {
	ci.mu.Lock()
	defer ci.mu.Unlock()
	for _, q := range ci.queries {
		if strings.Contains(q, title) {
			return true
		}
	}
	return false
}

func TestBulkSearchNowOnlySearchesMonitoredTitlesThatAreMissing(t *testing.T) {
	server, base, client := loginNewServer(t)
	type searchAnswer struct {
		Searching int    `json:"searching"`
		LeftOut   int    `json:"leftOut"`
		Limit     int    `json:"limit"`
		Skipped   int    `json:"skipped"`
		Message   string `json:"message"`
		Failed    []struct {
			ID     int64  `json:"id"`
			Reason string `json:"reason"`
		} `json:"failed"`
	}
	search := func(items []bulkTitle, want int) searchAnswer {
		return postJSON[searchAnswer](t, client, base+"/api/library/bulk/search-now", map[string]any{"items": items}, want)
	}

	// Without an indexer there is nothing to search with, and it says so.
	id := seedMovie(t, server.MovieRepo, 1, "Wanted Movie", 2001, "")
	if got := postJSON[map[string]any](t, client, base+"/api/library/bulk/search-now", map[string]any{"items": []bulkTitle{movieRef(id)}}, http.StatusPreconditionFailed); got["error"] == "" {
		t.Fatalf("expected a plain reason, got %v", got)
	}

	idx := newCountingIndexer(t)
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"name": "Indexer", "definitionId": "fixture", "baseUrl": idx.URL, "apiKey": "k",
	}, http.StatusCreated)

	unmonitored := seedMovie(t, server.MovieRepo, 2, "Unmonitored Movie", 2001, "")
	if err := server.MovieRepo.SetMonitored(unmonitored, false); err != nil {
		t.Fatal(err)
	}
	have := seedMovie(t, server.MovieRepo, 3, "Downloaded Movie", 2001, filepath.Join(t.TempDir(), "x.mkv"))
	wantedShow := seedShow(t, server, 30, "Wanted Show", 3, 1)
	completeShow := seedShow(t, server, 31, "Complete Show", 2, 2)

	got := search([]bulkTitle{movieRef(id), movieRef(unmonitored), movieRef(have), showRef(wantedShow), showRef(completeShow), movieRef(9999)}, http.StatusAccepted)
	if got.Searching != 2 || got.Skipped != 3 || len(got.Failed) != 1 || got.Failed[0].ID != 9999 || got.LeftOut != 0 {
		t.Fatalf("answer: %+v", got)
	}
	if !strings.HasPrefix(got.Message, "Searching for 2 titles now.") {
		t.Fatalf("message = %q", got.Message)
	}
	server.TestWaitBackground()
	if !idx.searched("Wanted Movie") || !idx.searched("Wanted Show") {
		t.Fatalf("the wanted titles should have been searched, searches were %v", idx.queries)
	}
	for _, not := range []string{"Unmonitored Movie", "Downloaded Movie", "Complete Show"} {
		if idx.searched(not) {
			t.Errorf("%q needed no search but was searched", not)
		}
	}
	activity := getJSON[[]map[string]any](t, client, base+"/api/activity")
	found := false
	for _, a := range activity {
		if a["eventType"] == "searched" && strings.Contains(a["message"].(string), "Searched for 2 titles") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the outcome should be noted in Activity: %v", activity)
	}

	// Nothing left that needs a search.
	got = search([]bulkTitle{movieRef(unmonitored), movieRef(have)}, http.StatusOK)
	if got.Searching != 0 || got.Skipped != 2 || !strings.HasPrefix(got.Message, "Nothing to search for.") {
		t.Fatalf("answer: %+v", got)
	}
	search([]bulkTitle{}, http.StatusBadRequest)
}

func TestBulkSearchNowIsCapped(t *testing.T) {
	server, base, client := loginNewServer(t)
	idx := newCountingIndexer(t)
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"name": "Indexer", "definitionId": "fixture", "baseUrl": idx.URL, "apiKey": "k",
	}, http.StatusCreated)
	var items []bulkTitle
	for i := 1; i <= 30; i++ {
		items = append(items, movieRef(seedMovie(t, server.MovieRepo, i, "Capped "+strconv.Itoa(i)+" Movie", 2001, "")))
	}
	got := postJSON[struct {
		Searching int    `json:"searching"`
		LeftOut   int    `json:"leftOut"`
		Limit     int    `json:"limit"`
		Message   string `json:"message"`
	}](t, client, base+"/api/library/bulk/search-now", map[string]any{"items": items}, http.StatusAccepted)
	if got.Searching != 25 || got.LeftOut != 5 || got.Limit != 25 {
		t.Fatalf("answer: %+v", got)
	}
	if !strings.Contains(got.Message, "The other 5 are left to the automatic search.") {
		t.Fatalf("message = %q", got.Message)
	}
	server.TestWaitBackground()
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if len(idx.queries) != 25 {
		t.Fatalf("expected 25 searches, got %d", len(idx.queries))
	}
}

func TestBulkArtistChangesAndRemoval(t *testing.T) {
	e := newMusicEnv(t)
	artist := e.addArtist(t, "all")
	id := int64(artist["id"].(float64))
	profiles := getJSON[[]map[string]any](t, e.client, e.base+"/api/music/profiles")
	lossy := int64(profiles[1]["id"].(float64))
	call := func(method, path string, body map[string]any, want int) bulkAnswer {
		return bulkCall(t, e.client, method, e.base+"/api/music/bulk"+path, body, want)
	}
	stored := func() (bool, int64) {
		a, err := e.server.MusicRepo.GetArtist(id)
		if err != nil {
			t.Fatal(err)
		}
		return a.Monitored, a.ProfileID
	}

	got := call("PUT", "/follow", map[string]any{"ids": []int64{id, 999}, "monitored": false}, http.StatusOK)
	if got.Updated != 1 || len(got.Failed) != 1 || got.Failed[0].ID != 999 || got.Failed[0].Reason == "" || !strings.HasPrefix(got.Message, "Done: 1 updated. 1 artist could not be changed.") {
		t.Fatalf("answer: %+v", got)
	}
	if followed, _ := stored(); followed {
		t.Fatal("the artist should not be followed any more")
	}
	got = call("PUT", "/profile", map[string]any{"ids": []int64{id, id}, "profileId": lossy}, http.StatusOK)
	if got.Updated != 1 || got.Message != "Done: 1 updated." {
		t.Fatalf("answer: %+v", got)
	}
	if _, profile := stored(); profile != lossy {
		t.Fatalf("profile = %d, want %d", profile, lossy)
	}
	call("PUT", "/profile", map[string]any{"ids": []int64{id}, "profileId": 99999}, http.StatusBadRequest)
	call("PUT", "/profile", map[string]any{"ids": []int64{}, "profileId": 0}, http.StatusBadRequest)
	call("PUT", "/follow", map[string]any{"ids": []int64{id}}, http.StatusBadRequest)

	// Removing keeps the album folders unless the request asks for them to go.
	albums, err := e.server.MusicRepo.ListAlbums(id)
	if err != nil || len(albums) == 0 {
		t.Fatalf("albums: %v %v", albums, err)
	}
	folder := filepath.Join(e.musicRoot, "Fixture Band", "First Album")
	putFile(t, filepath.Join(folder, "01 Track.flac"), "x")
	if err := e.server.MusicRepo.SetAlbumImported(albums[0].ID, "FLAC", folder); err != nil {
		t.Fatal(err)
	}
	got = call("POST", "/remove", map[string]any{"ids": []int64{id, 999}}, http.StatusOK)
	if got.Updated != 1 || len(got.Failed) != 1 || !strings.Contains(got.Message, "1 artist could not be removed") {
		t.Fatalf("answer: %+v", got)
	}
	if !exists(folder) {
		t.Fatal("album folders must stay unless the request asks for them to be deleted")
	}
	if _, err := e.server.MusicRepo.GetArtist(id); err == nil {
		t.Fatal("the artist should be gone")
	}

	// With deleteFiles the album folders go too.
	artist = e.addArtist(t, "all")
	id = int64(artist["id"].(float64))
	albums, _ = e.server.MusicRepo.ListAlbums(id)
	folder2 := filepath.Join(e.musicRoot, "Fixture Band", "Second Album")
	putFile(t, filepath.Join(folder2, "01 Track.flac"), "x")
	if err := e.server.MusicRepo.SetAlbumImported(albums[0].ID, "FLAC", folder2); err != nil {
		t.Fatal(err)
	}
	got = call("POST", "/remove", map[string]any{"ids": []int64{id}, "deleteFiles": true}, http.StatusOK)
	if got.Updated != 1 || got.Message != "Done: 1 removed, with their files." {
		t.Fatalf("answer: %+v", got)
	}
	if exists(folder2) || !exists(folder) {
		t.Fatal("only the removed artist's album folder should be deleted")
	}
}

// Stopping monitoring, or leaving what a title has alone, also clears the
// downloads that are only waiting, as it does one title at a time.
func TestBulkStopsClearWaitingDownloads(t *testing.T) {
	server, base, client := loginNewServer(t)
	file := filepath.Join(server.TestMoviesRoot(), "Have (2001)", "Have (2001).mkv")
	putFile(t, file, "x")
	have := seedMovie(t, server.MovieRepo, 1, "Have", 2001, file)
	want := seedMovie(t, server.MovieRepo, 2, "Want", 2002, "")
	other := seedMovie(t, server.MovieRepo, 3, "Other", 2003, "")
	enqueue := func(movieID int64, title string) int64 {
		id, err := server.QueueRepo.Enqueue(queue.Item{MovieID: movieID, ReleaseTitle: title})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	upgrade, missing, untouched := enqueue(have, "Have.2001.2160p"), enqueue(want, "Want.2002.1080p"), enqueue(other, "Other.2003.1080p")
	queued := func() map[int64]bool {
		list, err := server.QueueRepo.List()
		if err != nil {
			t.Fatal(err)
		}
		out := map[int64]bool{}
		for _, it := range list {
			out[it.ID] = true
		}
		return out
	}

	// Better versions off: only the download that would replace a file goes.
	bulkCall(t, client, "PUT", base+"/api/library/bulk/no-upgrade", map[string]any{"items": []bulkTitle{movieRef(have), movieRef(want)}, "noUpgrade": true}, http.StatusOK)
	if q := queued(); q[upgrade] || !q[missing] || !q[untouched] {
		t.Fatalf("only the waiting upgrade should be gone: %v", q)
	}
	// Better versions on again removes nothing.
	bulkCall(t, client, "PUT", base+"/api/library/bulk/no-upgrade", map[string]any{"items": []bulkTitle{movieRef(want)}, "noUpgrade": false}, http.StatusOK)
	if q := queued(); !q[missing] || !q[untouched] {
		t.Fatalf("nothing else should change: %v", q)
	}
	// Monitoring off clears whatever waits for that title, and only those.
	bulkCall(t, client, "PUT", base+"/api/library/bulk/monitored", map[string]any{"items": []bulkTitle{movieRef(want)}, "monitored": false}, http.StatusOK)
	if q := queued(); q[missing] || !q[untouched] {
		t.Fatalf("the unmonitored title's waiting download should be gone: %v", q)
	}
}
