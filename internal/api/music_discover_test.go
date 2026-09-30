package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const (
	popAlbum     = "bbbbbbbb-0000-4000-8000-000000000002" // Pop Album
	popSingle    = "bbbbbbbb-0000-4000-8000-000000000003" // Hit Single
	popBroadcast = "bbbbbbbb-0000-4000-8000-000000000004" // a radio show: never listed
	freshAlbum   = "cccccccc-0000-4000-8000-000000000001"
	freshSingle  = "cccccccc-0000-4000-8000-000000000002"
	freshLive    = "cccccccc-0000-4000-8000-000000000003"
	freshFuture  = "cccccccc-0000-4000-8000-000000000004" // dated after today in the "new" answer
	soonAlbum    = "dddddddd-0000-4000-8000-000000000001"
	soonEP       = "dddddddd-0000-4000-8000-000000000002"
	soonPast     = "dddddddd-0000-4000-8000-000000000003" // dated before today in the "upcoming" answer
	fixtureBand  = "11111111-1111-4111-8111-111111111111"
)

// fakeListenBrainz serves the shapes of the real service and counts requests.
type fakeListenBrainz struct {
	*httptest.Server
	mu   sync.Mutex
	hits map[string]int
	last map[string]string // the query of the last request, by path
}

func (f *fakeListenBrainz) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

func (f *fakeListenBrainz) lastQuery(path string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last[path]
}

func newFakeListenBrainz(t *testing.T) *fakeListenBrainz {
	t.Helper()
	f := &fakeListenBrainz{hits: map[string]int{}, last: map[string]string{}}
	mux := http.NewServeMux()
	handle := func(path string, h func(w http.ResponseWriter, r *http.Request)) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "Mediarium/") {
				t.Errorf("request without the identifying User-Agent: %q", ua)
			}
			f.mu.Lock()
			f.hits[path]++
			f.last[path] = r.URL.RawQuery
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			h(w, r)
		})
	}
	handle("/1/stats/sitewide/release-groups", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") != "0" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// The first release group is listed once per release it was heard on.
		fmt.Fprintf(w, `{"payload":{"release_groups":[
			{"artist_mbids":[%q],"artist_name":"Fixture Band","listen_count":300,"release_group_mbid":%q,"release_group_name":"First Album"},
			{"artist_mbids":["eeeeeeee-0000-4000-8000-000000000001"],"artist_name":"Pop Star","listen_count":400,"release_group_mbid":%q,"release_group_name":"Pop Album"},
			{"artist_mbids":[%q],"artist_name":"Fixture Band","listen_count":200,"release_group_mbid":%q,"release_group_name":"First Album"},
			{"artist_mbids":["eeeeeeee-0000-4000-8000-000000000001"],"artist_name":"Pop Star","listen_count":100,"release_group_mbid":%q,"release_group_name":"Hit Single"},
			{"artist_mbids":[],"artist_name":"Radio","listen_count":50,"release_group_mbid":%q,"release_group_name":"Radio Show"}
		],"count":5,"offset":0,"range":%q}}`, fixtureBand, albumWithCover, popAlbum, fixtureBand, albumWithCover, popSingle, popBroadcast, r.URL.Query().Get("range"))
	})
	handle("/1/metadata/release_group/", func(w http.ResponseWriter, r *http.Request) {
		known := map[string]string{
			albumWithCover: `{"release_group":{"date":"1999-03-01","name":"First Album","type":"Album"},"tag":{"artist":[],"release_group":[{"count":2,"genre_mbid":"g1","tag":"rock"}]}}`,
			popAlbum:       `{"release_group":{"date":"2026-03-20","name":"Pop Album","type":"Album"},"tag":{"artist":[{"artist_mbid":"x","count":9,"genre_mbid":"g2","tag":"pop"},{"artist_mbid":"x","count":8,"tag":"2020s"}],"release_group":[{"count":1,"genre_mbid":"g3","tag":"dance"},{"count":5,"genre_mbid":"g2","tag":"Pop"}]}}`,
			popSingle:      `{"release_group":{"date":"2026-09-15","name":"Hit Single","type":"Single"},"tag":{"artist":[{"artist_mbid":"x","count":9,"genre_mbid":"g2","tag":"pop"}],"release_group":[]}}`,
			popBroadcast:   `{"release_group":{"date":"2026-09-15","name":"Radio Show","type":"Broadcast"},"tag":{"artist":[],"release_group":[]}}`,
			freshAlbum:     `{"release_group":{"date":"2020-01-05","name":"Fresh Album","type":"Album"},"tag":{"artist":[{"artist_mbid":"x","count":3,"genre_mbid":"g4","tag":"folk"}],"release_group":[]}}`,
			soonEP:         `{"release_group":{"date":"2099-01-15","name":"Soon EP","type":"EP"},"tag":{"artist":[],"release_group":[]}}`,
		}
		var parts []string
		for _, id := range strings.Split(r.URL.Query().Get("release_group_mbids"), ",") {
			if body, ok := known[id]; ok {
				parts = append(parts, fmt.Sprintf("%q:%s", id, body))
			}
		}
		fmt.Fprintf(w, "{%s}", strings.Join(parts, ","))
	})
	handle("/1/stats/sitewide/artists", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"payload":{"artists":[
			{"artist_mbid":%q,"artist_name":"Fixture Band","listen_count":900},
			{"artist_mbid":"eeeeeeee-0000-4000-8000-000000000001","artist_name":"Pop Star","listen_count":800},
			{"artist_mbid":"eeeeeeee-0000-4000-8000-000000000002","artist_name":"Third","listen_count":700}
		],"range":%q}}`, fixtureBand, r.URL.Query().Get("range"))
	})
	handle("/1/explore/fresh-releases/", func(w http.ResponseWriter, r *http.Request) {
		rel := func(id, title, artist, date, primary, secondary string, tags string) string {
			sec := ""
			if secondary != "" {
				sec = fmt.Sprintf(`,"release_group_secondary_type":%q`, secondary)
			}
			return fmt.Sprintf(`{"artist_credit_name":%q,"artist_mbids":["ffffffff-0000-4000-8000-000000000001"],"listen_count":0,"release_date":%q,"release_group_mbid":%q,"release_group_primary_type":%q%s,"release_name":%q,"release_tags":%s}`,
				artist, date, id, primary, sec, title, tags)
		}
		var list []string
		if r.URL.Query().Get("future") == "true" {
			list = []string{
				rel(soonEP, "Soon EP", "Later Band", "2099-01-15", "EP", "", `[]`),
				rel(soonPast, "Already Out", "Later Band", "2020-01-01", "Album", "", `[]`),
				rel(soonAlbum, "Soon Album", "Later Band", "2099-02-01", "Album", "", `["folk"]`),
			}
		} else {
			list = []string{
				rel(freshAlbum, "Fresh Album", "New Band", "2020-01-05", "Album", "", `[]`),
				rel(freshSingle, "Fresh Single", "New Band", "2020-01-07", "Single", "", `["rock","Indie"]`),
				rel(freshLive, "Fresh Live", "New Band", "2020-01-06", "Album", "Live", `["rock"]`),
				rel(freshFuture, "Not Yet", "New Band", "2099-03-01", "Album", "", `[]`),
			}
		}
		fmt.Fprintf(w, `{"payload":{"releases":[%s],"total_count":%d}}`, strings.Join(list, ","), len(list))
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

type discoverAnswer struct {
	Page       int    `json:"page"`
	TotalPages int    `json:"totalPages"`
	Note       string `json:"note"`
	Items      []struct {
		MBID        string   `json:"mbid"`
		Title       string   `json:"title"`
		Type        string   `json:"type"`
		ArtistName  string   `json:"artistName"`
		ArtistMBID  string   `json:"artistMbid"`
		ReleaseDate string   `json:"releaseDate"`
		Genres      []string `json:"genres"`
		CoverURL    string   `json:"coverUrl"`
		InLibrary   bool     `json:"inLibrary"`
		ArtistID    int64    `json:"artistId"`
		AlbumID     int64    `json:"albumId"`
	} `json:"items"`
}

func (d discoverAnswer) titles() string {
	var out []string
	for _, it := range d.Items {
		out = append(out, it.Title)
	}
	return strings.Join(out, ",")
}

func TestMusicDiscoverRoutesAnswer404WhileTheModuleIsOff(t *testing.T) {
	_, base, client := loginNewServer(t)
	for _, path := range []string{"/api/music/discover", "/api/music/discover/artists", "/api/music/covers/release-group/" + albumWithCover} {
		status, body := doStatus(t, client, http.MethodGet, base+path)
		if status != http.StatusNotFound || !strings.Contains(fmt.Sprint(body["error"]), "music module is switched off") {
			t.Errorf("GET %s with music off: %d %v", path, status, body)
		}
	}
}

func TestMusicDiscoverPopular(t *testing.T) {
	e := newMusicEnv(t)
	lb := newFakeListenBrainz(t)
	e.server.TestSetListenBrainz(lb.URL)
	artist := e.addArtist(t, "none")
	album := albumByTitle(t, artist, "First Album")

	got := getJSON[discoverAnswer](t, e.client, e.base+"/api/music/discover?list=popular&range=month")
	// First Album (300+200 merged) comes before Pop Album (400); the
	// broadcast is left out.
	if got.titles() != "First Album,Pop Album,Hit Single" || got.Page != 1 || got.TotalPages != 1 || got.Note != "" {
		t.Fatalf("popular: %+v", got)
	}
	if q := lb.lastQuery("/1/stats/sitewide/release-groups"); !strings.Contains(q, "range=month") || !strings.Contains(q, "count=100") {
		t.Fatalf("source query: %s", q)
	}
	first, pop, single := got.Items[0], got.Items[1], got.Items[2]
	if !first.InLibrary || first.AlbumID != int64(album["id"].(float64)) || first.ArtistID != int64(artist["id"].(float64)) ||
		first.CoverURL != fmt.Sprintf("/api/music/albums/%d/cover", first.AlbumID) || first.Type != "album" || first.ReleaseDate != "1999-03-01" ||
		strings.Join(first.Genres, ",") != "rock" || first.ArtistName != "Fixture Band" || first.ArtistMBID != fixtureBand {
		t.Fatalf("an album in the library: %+v", first)
	}
	if pop.InLibrary || pop.AlbumID != 0 || pop.ArtistID != 0 || pop.CoverURL != "/api/music/covers/release-group/"+popAlbum ||
		pop.Type != "album" || pop.ReleaseDate != "2026-03-20" || strings.Join(pop.Genres, ",") != "Pop,dance" {
		t.Fatalf("an album not in the library (own genres first, no duplicates): %+v", pop)
	}
	if single.Type != "single" || strings.Join(single.Genres, ",") != "pop" {
		t.Fatalf("a single with the genres of its artist: %+v", single)
	}

	// Type and genre filters, and paging.
	if got := getJSON[discoverAnswer](t, e.client, e.base+"/api/music/discover?list=popular&type=single"); got.titles() != "Hit Single" {
		t.Fatalf("type=single: %s", got.titles())
	}
	if got := getJSON[discoverAnswer](t, e.client, e.base+"/api/music/discover?list=popular&genre=POP"); got.titles() != "Pop Album,Hit Single" {
		t.Fatalf("genre=POP: %s", got.titles())
	}
	got = getJSON[discoverAnswer](t, e.client, e.base+"/api/music/discover?list=popular&pageSize=2&page=2")
	if got.titles() != "Hit Single" || got.Page != 2 || got.TotalPages != 2 {
		t.Fatalf("page 2 of 2: %+v", got)
	}
	got = getJSON[discoverAnswer](t, e.client, e.base+"/api/music/discover?list=popular&pageSize=2&page=9")
	if len(got.Items) != 0 || got.TotalPages != 2 || got.Note != "" {
		t.Fatalf("past the end: %+v", got)
	}

	// Two ranges were asked for (month, then the default week five times):
	// every repeat came from the cache.
	if n := lb.count("/1/stats/sitewide/release-groups"); n != 2 {
		t.Fatalf("the source was asked %d times for 2 ranges, want 2 (cache)", n)
	}
}

func TestMusicDiscoverNewAndUpcoming(t *testing.T) {
	e := newMusicEnv(t)
	lb := newFakeListenBrainz(t)
	e.server.TestSetListenBrainz(lb.URL)

	// New: newest first; the live album and the release dated after today
	// are left out.
	got := getJSON[discoverAnswer](t, e.client, e.base+"/api/music/discover?list=new")
	if got.titles() != "Fresh Single,Fresh Album" {
		t.Fatalf("new: %s", got.titles())
	}
	if q := lb.lastQuery("/1/explore/fresh-releases/"); !strings.Contains(q, "days=14") || !strings.Contains(q, "future=false") || !strings.Contains(q, "sort=release_date") {
		t.Fatalf("new query: %s", q)
	}
	single, album := got.Items[0], got.Items[1]
	if single.Type != "single" || single.ReleaseDate != "2020-01-07" || strings.Join(single.Genres, ",") != "rock,Indie" {
		t.Fatalf("single: %+v", single)
	}
	// No tags in the list: the genres come from the details service.
	if strings.Join(album.Genres, ",") != "folk" || album.ArtistMBID != "ffffffff-0000-4000-8000-000000000001" || album.InLibrary {
		t.Fatalf("album: %+v", album)
	}

	// The genre filter uses the tags of the list; untagged entries drop out.
	if got := getJSON[discoverAnswer](t, e.client, e.base+"/api/music/discover?list=new&genre=indie"); got.titles() != "Fresh Single" {
		t.Fatalf("genre=indie: %s", got.titles())
	}
	if got := getJSON[discoverAnswer](t, e.client, e.base+"/api/music/discover?list=new&genre=jazz"); len(got.Items) != 0 || got.Note != "" {
		t.Fatalf("genre=jazz: %+v", got)
	}
	if got := getJSON[discoverAnswer](t, e.client, e.base+"/api/music/discover?list=new&type=album"); got.titles() != "Fresh Album" {
		t.Fatalf("type=album: %s", got.titles())
	}

	// Upcoming: soonest first; the one already out is left out.
	got = getJSON[discoverAnswer](t, e.client, e.base+"/api/music/discover?list=upcoming&type=all")
	if got.titles() != "Soon EP,Soon Album" || got.Items[0].Type != "ep" {
		t.Fatalf("upcoming: %s (%+v)", got.titles(), got.Items)
	}
	if q := lb.lastQuery("/1/explore/fresh-releases/"); !strings.Contains(q, "future=true") || !strings.Contains(q, "past=false") || !strings.Contains(q, "days=90") {
		t.Fatalf("upcoming query: %s", q)
	}
}

// A type whose entries carry no tags at all cannot be filtered by genre, so
// the genre is ignored instead of emptying the page.
func TestMusicDiscoverGenreIgnoredWhenNothingIsTagged(t *testing.T) {
	e := newMusicEnv(t)
	lb := newFakeListenBrainz(t)
	e.server.TestSetListenBrainz(lb.URL)
	got := getJSON[discoverAnswer](t, e.client, e.base+"/api/music/discover?list=upcoming&genre=jazz&type=ep")
	if got.titles() != "Soon EP" {
		t.Fatalf("upcoming EPs are untagged, so the genre is ignored: %s", got.titles())
	}
}

// The years and the order narrow and sort the list like they do for movies.
func TestMusicDiscoverYearsAndOrder(t *testing.T) {
	e := newMusicEnv(t)
	e.server.TestSetListenBrainz(newFakeListenBrainz(t).URL)
	for _, c := range []struct{ query, want string }{
		{"yearTo=2000", "First Album"},
		{"yearFrom=2026", "Pop Album,Hit Single"},
		{"yearFrom=2000&yearTo=2026", "Pop Album,Hit Single"},
		{"yearFrom=2027", ""},
		{"sort=newest", "Hit Single,Pop Album,First Album"},
		{"sort=oldest", "First Album,Pop Album,Hit Single"},
		{"sort=popular", "First Album,Pop Album,Hit Single"},
		{"sort=newest&type=album", "Pop Album,First Album"},
		{"genre=rock&yearTo=2000", "First Album"},
	} {
		got := getJSON[discoverAnswer](t, e.client, e.base+"/api/music/discover?list=popular&"+c.query)
		if got.titles() != c.want || got.Note != "" {
			t.Errorf("%s: got %q (note %q), want %q", c.query, got.titles(), got.Note, c.want)
		}
	}
}

// A genre also finds the kinds of it: "rock" finds "punk rock".
func TestMusicDiscoverGenreFindsKindsOfIt(t *testing.T) {
	e := newMusicEnv(t)
	e.server.TestSetListenBrainz(newFakeListenBrainz(t).URL)
	if got := getJSON[discoverAnswer](t, e.client, e.base+"/api/music/discover?list=new&genre=roc"); got.titles() != "Fresh Single" {
		t.Fatalf("a part of a tag: %s", got.titles())
	}
}

func TestMusicDiscoverBadParameters(t *testing.T) {
	e := newMusicEnv(t)
	e.server.TestSetListenBrainz(newFakeListenBrainz(t).URL)
	for _, q := range []string{"list=weird", "type=live", "range=decade", "yearFrom=abc", "yearTo=99", "sort=random"} {
		if status, body := doStatus(t, e.client, http.MethodGet, e.base+"/api/music/discover?"+q); status != http.StatusBadRequest {
			t.Errorf("%s: %d %v", q, status, body)
		}
	}
	if status, _ := doStatus(t, e.client, http.MethodGet, e.base+"/api/music/discover/artists?range=decade"); status != http.StatusBadRequest {
		t.Errorf("artists range: %d", status)
	}
}

func TestMusicDiscoverArtists(t *testing.T) {
	e := newMusicEnv(t)
	lb := newFakeListenBrainz(t)
	e.server.TestSetListenBrainz(lb.URL)
	artist := e.addArtist(t, "none")

	var got struct {
		Page       int `json:"page"`
		TotalPages int `json:"totalPages"`
		Items      []struct {
			MBID        string `json:"mbid"`
			Name        string `json:"name"`
			ListenCount int    `json:"listenCount"`
			InLibrary   bool   `json:"inLibrary"`
			ArtistID    int64  `json:"artistId"`
			CoverURL    string `json:"coverUrl"`
		} `json:"items"`
	}
	raw := getJSON[json.RawMessage](t, e.client, e.base+"/api/music/discover/artists?range=year&pageSize=2")
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Page != 1 || got.TotalPages != 2 || len(got.Items) != 2 {
		t.Fatalf("artists: %s", raw)
	}
	a, b := got.Items[0], got.Items[1]
	if a.Name != "Fixture Band" || a.ListenCount != 900 || !a.InLibrary || a.ArtistID != int64(artist["id"].(float64)) || !strings.HasSuffix(a.CoverURL, "/cover") {
		t.Fatalf("artist in the library: %+v", a)
	}
	if b.InLibrary || b.ArtistID != 0 || b.CoverURL != "" || b.MBID != "eeeeeeee-0000-4000-8000-000000000001" {
		t.Fatalf("artist not in the library: %+v", b)
	}
	if q := lb.lastQuery("/1/stats/sitewide/artists"); !strings.Contains(q, "range=year") {
		t.Fatalf("source query: %s", q)
	}
}

// When the source cannot be reached the answer is still a 200 with an empty
// list and a plain note that names no service.
func TestMusicDiscoverSourceDownIsNotAnError(t *testing.T) {
	e := newMusicEnv(t)
	dead := newFakeListenBrainz(t)
	dead.Close()
	e.server.TestSetListenBrainz(dead.URL)

	for _, path := range []string{"/api/music/discover?list=popular", "/api/music/discover?list=new", "/api/music/discover?list=upcoming", "/api/music/discover/artists"} {
		raw := getJSON[map[string]any](t, e.client, e.base+path)
		items, _ := raw["items"].([]any)
		if raw["note"] != "The list isn't available right now. Try again later." || items == nil || len(items) != 0 {
			t.Errorf("%s: %v", path, raw)
		}
		for _, name := range []string{"ListenBrainz", "MusicBrainz", "127.0.0.1"} {
			if b, _ := json.Marshal(raw); strings.Contains(string(b), name) {
				t.Errorf("%s: the answer names %q: %s", path, name, b)
			}
		}
	}
}

func TestMusicReleaseGroupCoverIsServedFromOurOwnRoute(t *testing.T) {
	e := newMusicEnv(t)
	status, hdr, body := getRaw(t, e.client, e.base+"/api/music/covers/release-group/"+albumWithCover)
	if status != 200 || hdr.Get("Content-Type") != "image/jpeg" || string(body) != string(fakeJPEG) {
		t.Fatalf("cover: %d %q %d bytes", status, hdr.Get("Content-Type"), len(body))
	}
	if n := e.covers.count(albumWithCover); n != 1 {
		t.Fatalf("archive requests: %d", n)
	}
	getRaw(t, e.client, e.base+"/api/music/covers/release-group/"+albumWithCover)
	if n := e.covers.count(albumWithCover); n != 1 {
		t.Fatalf("a kept cover must not be fetched again: %d", n)
	}
	if status, _, _ := getRaw(t, e.client, e.base+"/api/music/covers/release-group/"+albumWithoutCover); status != http.StatusNotFound {
		t.Fatalf("a release group without a cover: %d", status)
	}
	if status, _, _ := getRaw(t, e.client, e.base+"/api/music/covers/release-group/..%2f..%2fetc"); status != http.StatusNotFound {
		t.Fatalf("a bad id: %d", status)
	}
}

func TestMusicDiscoverIsOpenToMembers(t *testing.T) {
	server, base, admin, member, _ := familyServer(t)
	server.TestSetListenBrainz(newFakeListenBrainz(t).URL)
	mb, _ := newFakeMusicBrainzHits(t)
	server.TestSetMusicBrainz(mb.URL)
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/settings", map[string]any{"musicEnabled": true, "musicPath": t.TempDir()}, http.StatusOK)
	if got := getJSON[discoverAnswer](t, member, base+"/api/music/discover?list=new"); got.titles() != "Fresh Single,Fresh Album" {
		t.Fatalf("member: %s", got.titles())
	}
	if got := getJSON[map[string]any](t, member, base+"/api/music/discover/artists"); len(got["items"].([]any)) != 3 {
		t.Fatalf("member artists: %v", got)
	}
}
