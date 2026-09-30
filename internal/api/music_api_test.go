package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
)

// newFakeMusicBrainz serves the testdata/musicbrainz fixtures: one artist
// ("Fixture Band") with five release groups, and the tracklist of "First
// Album".
func newFakeMusicBrainz(t *testing.T) *httptest.Server {
	t.Helper()
	srv, _ := newFakeMusicBrainzHits(t)
	return srv
}

// coverHits counts requests to the fake Cover Art Archive, by release group.
type coverHits struct {
	mu   sync.Mutex
	hits map[string]int
}

func (c *coverHits) count(mbid string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits[mbid]
}

// fakeJPEG is a few bytes that are recognisably a JPEG.
var fakeJPEG = append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}, []byte("fake cover image bytes")...)

// newFakeMusicBrainzHits is newFakeMusicBrainz plus a fake Cover Art
// Archive on the same server: the front cover of release group ...0001 is
// there (behind a redirect, like the real archive), ...0002 has none.
func newFakeMusicBrainzHits(t *testing.T) (*httptest.Server, *coverHits) {
	t.Helper()
	hits := &coverHits{hits: map[string]int{}}
	fixture := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "Mediarium/") {
				t.Errorf("MusicBrainz request without the identifying User-Agent: %q", ua)
			}
			body, err := os.ReadFile(filepath.Join("testdata", "musicbrainz", name))
			if err != nil {
				t.Errorf("read fixture %s: %v", name, err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(body)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/2/artist", fixture("artist_search.json"))
	mux.HandleFunc("/ws/2/artist/11111111-1111-4111-8111-111111111111", fixture("artist.json"))
	mux.HandleFunc("/ws/2/release-group", fixture("release_groups.json"))
	mux.HandleFunc("/ws/2/release", fixture("releases.json"))
	mux.HandleFunc("/ws/2/release/rel-first", fixture("release.json"))
	mux.HandleFunc("/release-group/{mbid}/front-500", func(w http.ResponseWriter, r *http.Request) {
		mbid := r.PathValue("mbid")
		hits.mu.Lock()
		hits.hits[mbid]++
		hits.mu.Unlock()
		if mbid == "aaaaaaaa-0000-4000-8000-000000000001" {
			http.Redirect(w, r, "/images/front.jpg", http.StatusTemporaryRedirect)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/images/front.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(fakeJPEG)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, hits
}

// musicIndexer is a fake Newznab indexer for music: it lists titles and
// serves one NZB, and records the categories it was asked for.
type musicIndexer struct {
	*httptest.Server
	mu   sync.Mutex
	cats []string
}

func newMusicIndexer(t *testing.T, titles []string, nzb string) *musicIndexer {
	t.Helper()
	idx := &musicIndexer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		idx.mu.Lock()
		idx.cats = append(idx.cats, r.URL.Query().Get("cat"))
		idx.mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		var items strings.Builder
		for i, title := range titles {
			fmt.Fprintf(&items, `<item><title>%s</title><guid>g%d</guid><enclosure url="http://%s/nzb/%d.nzb" length="1000" type="application/x-nzb" /><newznab:attr name="size" value="1000"/><newznab:attr name="category" value="3040"/></item>`,
				title, i, r.Host, i)
		}
		fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>%s</channel></rss>`, items.String())
	})
	mux.HandleFunc("/nzb/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, nzb)
	})
	idx.Server = httptest.NewServer(mux)
	t.Cleanup(idx.Close)
	return idx
}

func (m *musicIndexer) categories() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.cats...)
}

type musicEnv struct {
	server    *api.Server
	base      string
	client    *http.Client
	musicRoot string
	covers    *coverHits // requests the fake Cover Art Archive has seen
}

// newMusicEnv boots a server with an administrator, the music module on
// (music folder in a temp dir) and MusicBrainz faked.
func newMusicEnv(t *testing.T) *musicEnv {
	t.Helper()
	server, base, client := loginNewServer(t)
	mb, covers := newFakeMusicBrainzHits(t)
	server.TestSetMusicBrainz(mb.URL)
	root := filepath.Join(t.TempDir(), "music")
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{
		"musicEnabled": true, "musicPath": root,
	}, http.StatusOK)
	return &musicEnv{server: server, base: base, client: client, musicRoot: root, covers: covers}
}

func (e *musicEnv) addArtist(t *testing.T, monitor string) map[string]any {
	t.Helper()
	return postJSON[map[string]any](t, e.client, e.base+"/api/music/artists", map[string]any{
		"mbid": "11111111-1111-4111-8111-111111111111", "monitor": monitor,
	}, http.StatusCreated)
}

// useLosslessByDefault makes the Lossless (FLAC) profile the default, for the
// cases that are about FLAC releases (a fresh install defaults to Lossy).
func (e *musicEnv) useLosslessByDefault(t *testing.T) {
	t.Helper()
	lossless := getJSON[[]map[string]any](t, e.client, e.base+"/api/music/profiles")[1]
	postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/settings", map[string]any{"musicDefaultProfileId": lossless["id"]}, http.StatusOK)
}

func albumByTitle(t *testing.T, artist map[string]any, title string) map[string]any {
	t.Helper()
	for _, a := range artist["albums"].([]any) {
		if m := a.(map[string]any); m["title"] == title {
			return m
		}
	}
	t.Fatalf("album %q not in %v", title, artist["albums"])
	return nil
}

func TestMusicRoutesAnswer404WhileTheModuleIsOff(t *testing.T) {
	_, base, client := loginNewServer(t)
	for _, path := range []string{"/api/music/artists", "/api/music/wanted", "/api/music/search?q=x", "/api/music/profiles"} {
		status, body := doStatus(t, client, http.MethodGet, base+path)
		if status != http.StatusNotFound || !strings.Contains(fmt.Sprint(body["error"]), "music module is switched off") {
			t.Errorf("GET %s with music off: %d %v", path, status, body)
		}
	}
	if status, _ := doStatus(t, client, http.MethodPost, base+"/api/music/artists"); status != http.StatusNotFound {
		t.Errorf("POST /api/music/artists with music off: %d", status)
	}
}

func TestMusicAddArtistMonitorChoices(t *testing.T) {
	e := newMusicEnv(t)

	found := getJSON[[]map[string]any](t, e.client, e.base+"/api/music/search?q=Fixture")
	if len(found) != 2 || found[0]["name"] != "Fixture Band" || found[0]["artistId"] != float64(0) {
		t.Fatalf("search: %v", found)
	}

	artist := e.addArtist(t, "future")
	if artist["name"] != "Fixture Band" || artist["monitored"] != true || artist["profileName"] != "Lossy (MP3 320)" {
		t.Fatalf("artist: %v", artist)
	}
	albums := artist["albums"].([]any)
	if len(albums) != 4 {
		t.Fatalf("want 4 releases (the live album left out), got %v", albums)
	}
	for _, a := range albums {
		m := a.(map[string]any)
		wantMonitored := m["title"] == "Upcoming Album"
		if m["monitored"] != wantMonitored {
			t.Errorf("future: %s monitored=%v", m["title"], m["monitored"])
		}
		if m["coverUrl"] != fmt.Sprintf("/api/music/albums/%v/cover", m["id"]) || !strings.HasPrefix(m["coverArchiveUrl"].(string), "https://coverartarchive.org/release-group/") {
			t.Errorf("cover urls: %v / %v", m["coverUrl"], m["coverArchiveUrl"])
		}
	}
	if single := albumByTitle(t, artist, "A Single"); single["type"] != "single" || single["year"] != float64(2006) {
		t.Fatalf("single: %v", single)
	}

	// Already there: 409. The search marks it as in the library.
	status, body := doJSONStatus(t, e.client, http.MethodPost, e.base+"/api/music/artists", map[string]any{"mbid": "11111111-1111-4111-8111-111111111111"})
	if status != http.StatusConflict {
		t.Fatalf("adding twice: %d %v", status, body)
	}
	found = getJSON[[]map[string]any](t, e.client, e.base+"/api/music/search?q=Fixture")
	if found[0]["artistId"] != artist["id"] {
		t.Fatalf("search should link the added artist: %v", found[0])
	}
	if status, _ := doJSONStatus(t, e.client, http.MethodPost, e.base+"/api/music/artists", map[string]any{"mbid": "x", "monitor": "sometimes"}); status != http.StatusBadRequest {
		t.Fatalf("bad monitor value: %d", status)
	}
	if status, _ := doJSONStatus(t, e.client, http.MethodPost, e.base+"/api/music/artists", map[string]any{"mbid": "22222222-2222-4222-8222-222222222222"}); status != http.StatusNotFound {
		t.Fatalf("unknown artist: %d", status)
	}

	// Monitoring one album, the list and the wanted list.
	second := albumByTitle(t, artist, "Second Album")
	postJSONMethod[any](t, e.client, http.MethodPut, fmt.Sprintf("%s/api/music/albums/%v/monitored", e.base, second["id"]), map[string]any{"monitored": true}, http.StatusOK)
	wanted := getJSON[[]map[string]any](t, e.client, e.base+"/api/music/wanted")
	if len(wanted) != 1 || wanted[0]["title"] != "Second Album" || wanted[0]["artistName"] != "Fixture Band" {
		t.Fatalf("wanted (unreleased albums are not wanted yet): %v", wanted)
	}
	list := getJSON[[]map[string]any](t, e.client, e.base+"/api/music/artists")
	if len(list) != 1 || list[0]["albumCount"] != float64(4) || list[0]["monitoredCount"] != float64(2) {
		t.Fatalf("artist list: %v", list)
	}
	if status, _ := doJSONStatus(t, e.client, http.MethodPut, e.base+"/api/music/albums/99999/monitored", map[string]any{"monitored": true}); status != http.StatusNotFound {
		t.Fatalf("unknown album: %d", status)
	}
	profiles := getJSON[[]map[string]any](t, e.client, e.base+"/api/music/profiles")
	if len(profiles) != 2 || profiles[0]["default"] != true || profiles[0]["cutoff"] != "MP3-320/V0" || profiles[1]["name"] != "Lossless (FLAC)" {
		t.Fatalf("profiles: %v", profiles)
	}
}

func TestMusicSearchGrabImportEndToEnd(t *testing.T) {
	e := newMusicEnv(t)
	e.useLosslessByDefault(t) // this case is about FLAC releases
	artist := e.addArtist(t, "all")
	first := albumByTitle(t, artist, "First Album")
	albumURL := fmt.Sprintf("%s/api/music/albums/%v", e.base, first["id"])

	articles := map[string]nntpArticle{
		"t1@x":  {fileName: "01 - Opening.flac", content: []byte("flac one " + strings.Repeat("a", 300))},
		"t2@x":  {fileName: "02 - Middle Song.flac", content: []byte("flac two " + strings.Repeat("b", 300))},
		"t3@x":  {fileName: "03 - Closing Time.flac", content: []byte("flac three " + strings.Repeat("c", 300))},
		"art@x": {fileName: "cover.jpg", content: []byte("jpeg bytes")},
		"nfo@x": {fileName: "release.nfo", content: []byte("nfo text")},
	}
	host, port := newArticleNNTPServer(t, articles)
	idx := newMusicIndexer(t, []string{
		"Fixture Band - First Album (1999) [MP3 320]",
		"Fixture Band - First Album (1999) [FLAC]",
		"Fixture Band - First Album (1999) [MP3 192]",
		"Fixture Band - Other Album (2001) [FLAC]",
		"Fixture Band - Discography (1999-2010) [FLAC]",
	}, nzbFor(articles))
	postJSON[map[string]any](t, e.client, e.base+"/api/indexers", map[string]any{"name": "Music Indexer", "definitionId": "x", "baseUrl": idx.URL, "apiKey": "k", "protocol": "usenet"}, http.StatusCreated)
	postJSON[map[string]any](t, e.client, e.base+"/api/usenet-servers", map[string]any{"name": "Fixture Usenet", "host": host, "port": port, "useSsl": false, "connections": 2}, http.StatusCreated)

	// Interactive search: every result with its reasons.
	results := postJSON[[]map[string]any](t, e.client, albumURL+"/search", nil, http.StatusOK)
	if len(results) != 5 {
		t.Fatalf("results: %v", results)
	}
	byTitle := map[string]map[string]any{}
	for _, r := range results {
		byTitle[r["title"].(string)] = r
	}
	if cats := idx.categories(); len(cats) == 0 || cats[0] != "3000,3010,3040,3050" {
		t.Fatalf("searched categories: %v", cats)
	}
	flac := byTitle["Fixture Band - First Album (1999) [FLAC]"]
	if flac["rejections"] != nil || flac["quality"] != "FLAC" || flac["acceptedBy"].(map[string]any)["profileName"] != "Lossless (FLAC)" || flac["format"] != "FLAC" {
		t.Fatalf("the FLAC release is what the profile wants: %v", flac)
	}
	mp3 := byTitle["Fixture Band - First Album (1999) [MP3 320]"]
	if !strings.Contains(fmt.Sprint(mp3["rejections"]), "allowed only as a fallback") || mp3["acceptedBy"].(map[string]any)["fallback"] != true {
		t.Fatalf("MP3 320 is only a fallback: %v", mp3)
	}
	if r := fmt.Sprint(byTitle["Fixture Band - Other Album (2001) [FLAC]"]["rejections"]); !strings.Contains(r, "another album") {
		t.Fatalf("other album: %s", r)
	}
	if r := fmt.Sprint(byTitle["Fixture Band - Discography (1999-2010) [FLAC]"]["rejections"]); !strings.Contains(r, "discography") {
		t.Fatalf("discography: %s", r)
	}

	// Search now picks the FLAC release and the pipeline imports it.
	now := postJSON[map[string]any](t, e.client, albumURL+"/search-now", nil, http.StatusOK)
	if now["grabbed"] != float64(1) {
		t.Fatalf("search now: %v", now)
	}
	waitBackground(t, e.server)

	queueList := getJSON[[]map[string]any](t, e.client, e.base+"/api/queue")
	if len(queueList) != 1 || queueList[0]["status"] != "completed" || queueList[0]["albumId"] != first["id"] ||
		queueList[0]["title"] != "Fixture Band – First Album" || queueList[0]["releaseTitle"] != "Fixture Band - First Album (1999) [FLAC]" {
		t.Fatalf("queue: %v", queueList)
	}
	folder := filepath.Join(e.musicRoot, "Fixture Band", "First Album (1999)")
	for _, name := range []string{"01 - Opening.flac", "02 - Middle Song.flac", "03 - Closing Time.flac", "cover.jpg"} {
		if _, err := os.Stat(filepath.Join(folder, name)); err != nil {
			t.Errorf("expected %s in the album folder: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(folder, "release.nfo")); err == nil {
		t.Error("only audio and cover art are imported")
	}
	got, _ := os.ReadFile(filepath.Join(folder, "02 - Middle Song.flac"))
	if string(got) != string(articles["t2@x"].content) {
		t.Fatalf("track 2 holds the wrong file: %q", got)
	}

	album := getJSON[map[string]any](t, e.client, albumURL)
	if album["status"] != "downloaded" || album["quality"] != "FLAC" || album["path"] != folder || album["artistName"] != "Fixture Band" {
		t.Fatalf("album after import: %v", album)
	}
	tracks := album["tracks"].([]any)
	if len(tracks) != 3 || tracks[2].(map[string]any)["title"] != "Closing Time" || tracks[2].(map[string]any)["hasFile"] != true {
		t.Fatalf("tracks: %v", tracks)
	}
	// The release brought its own cover.jpg: it is kept as it came, and the
	// Cover Art Archive was not asked.
	if got, _ := os.ReadFile(filepath.Join(folder, "cover.jpg")); string(got) != "jpeg bytes" || e.covers.count("aaaaaaaa-0000-4000-8000-000000000001") != 0 {
		t.Fatalf("the release's own cover must be kept: %q (archive hits %d)", got, e.covers.count("aaaaaaaa-0000-4000-8000-000000000001"))
	}

	events := getJSON[[]map[string]any](t, e.client, albumURL+"/events")
	kinds := map[string]bool{}
	for _, ev := range events {
		kinds[ev["kind"].(string)] = true
	}
	for _, k := range []string{"searched", "grabbed", "download", "imported"} {
		if !kinds[k] {
			t.Errorf("album events miss %q: %v", k, events)
		}
	}
	for _, ev := range events {
		if ev["kind"] == "searched" && !strings.Contains(ev["message"].(string), `picked "Fixture Band - First Album (1999) [FLAC]"`) {
			t.Errorf("search event should name the pick: %v", ev["message"])
		}
	}

	// Downloaded FLAC is at the cutoff: nothing is wanted for it any more,
	// and a second search-now finds nothing to take.
	for _, w := range getJSON[[]map[string]any](t, e.client, e.base+"/api/music/wanted") {
		if w["id"] == first["id"] {
			t.Fatalf("a downloaded album is not missing: %v", w)
		}
	}
	if cut := getJSON[[]map[string]any](t, e.client, e.base+"/api/music/wanted?kind=cutoff"); len(cut) != 0 {
		t.Fatalf("FLAC is the cutoff: %v", cut)
	}
	again := postJSON[map[string]any](t, e.client, albumURL+"/search-now", nil, http.StatusOK)
	if again["grabbed"] != float64(0) {
		t.Fatalf("nothing better to take: %v", again)
	}

	// The scheduled hunt searches only while the module is on.
	e.server.TestHunt(context.Background())
	waitBackground(t, e.server)

	// Removing the artist with its files takes the album folder and the
	// now-empty artist folder, nothing else.
	if status := deleteReq(t, e.client, fmt.Sprintf("%s/api/music/artists/%v?deleteFiles=true", e.base, artist["id"])); status != http.StatusOK {
		t.Fatalf("delete artist: %d", status)
	}
	if _, err := os.Stat(filepath.Join(e.musicRoot, "Fixture Band")); !os.IsNotExist(err) {
		t.Fatalf("artist folder should be gone: %v", err)
	}
	if _, err := os.Stat(e.musicRoot); err != nil {
		t.Fatalf("the music folder itself stays: %v", err)
	}
	if status, _ := doStatus(t, e.client, http.MethodGet, albumURL); status != http.StatusNotFound {
		t.Fatalf("album after delete: %d", status)
	}
}

func TestMusicGrabOfUnmatchableReleaseIsBlocklisted(t *testing.T) {
	e := newMusicEnv(t)
	artist := e.addArtist(t, "none")
	first := albumByTitle(t, artist, "First Album")
	albumURL := fmt.Sprintf("%s/api/music/albums/%v", e.base, first["id"])

	articles := map[string]nntpArticle{"x@x": {fileName: "some other song.mp3", content: []byte("mp3 bytes " + strings.Repeat("z", 200))}}
	host, port := newArticleNNTPServer(t, articles)
	idx := newMusicIndexer(t, []string{"Fixture Band - First Album (1999) [MP3 320]"}, nzbFor(articles))
	postJSON[map[string]any](t, e.client, e.base+"/api/indexers", map[string]any{"name": "Music Indexer", "definitionId": "x", "baseUrl": idx.URL, "apiKey": "k", "protocol": "usenet"}, http.StatusCreated)
	postJSON[map[string]any](t, e.client, e.base+"/api/usenet-servers", map[string]any{"name": "Fixture Usenet", "host": host, "port": port, "useSsl": false, "connections": 1}, http.StatusCreated)

	results := postJSON[[]map[string]any](t, e.client, albumURL+"/search", nil, http.StatusOK)
	if len(results) != 1 {
		t.Fatalf("results: %v", results)
	}
	// A manual grab of a release automation would only take as a fallback.
	grab := postJSON[map[string]any](t, e.client, albumURL+"/grab", map[string]any{
		"releaseTitle": results[0]["title"], "downloadUrl": results[0]["downloadUrl"], "sizeBytes": 100,
	}, http.StatusAccepted)
	if grab["queueId"] == nil {
		t.Fatalf("grab: %v", grab)
	}
	waitBackground(t, e.server)

	q := getJSON[[]map[string]any](t, e.client, e.base+"/api/queue")
	if len(q) != 1 || q[0]["status"] != "failed" || !strings.Contains(fmt.Sprint(q[0]["error"]), "could be matched") {
		t.Fatalf("queue: %v", q)
	}
	album := getJSON[map[string]any](t, e.client, albumURL)
	if album["status"] != "missing" {
		t.Fatalf("a failed grab puts the album back to missing: %v", album)
	}
	bl := getJSON[[]map[string]any](t, e.client, e.base+"/api/blocklist")
	if len(bl) != 1 || bl[0]["releaseTitle"] != "Fixture Band - First Album (1999) [MP3 320]" {
		t.Fatalf("blocklist: %v", bl)
	}
	var sawBlocklisted bool
	for _, ev := range getJSON[[]map[string]any](t, e.client, albumURL+"/events") {
		sawBlocklisted = sawBlocklisted || ev["kind"] == "blocklisted"
	}
	if !sawBlocklisted {
		t.Fatal("the album's log should show the blocklisting")
	}
}
