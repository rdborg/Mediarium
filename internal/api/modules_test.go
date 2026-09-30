package api_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

func moduleEnabled(t *testing.T, mods map[string]any, name string) (enabled, available bool) {
	t.Helper()
	m, ok := mods[name].(map[string]any)
	if !ok {
		t.Fatalf("modules answer has no %q: %v", name, mods)
	}
	return m["enabled"] == true, m["available"] == true
}

func TestModulesDefaultsAndShape(t *testing.T) {
	_, base, client := loginNewServer(t)
	mods := getJSON[map[string]any](t, client, base+"/api/modules")
	want := map[string][2]bool{
		"movies": {true, true}, "tv": {true, true}, "music": {false, true},
		"audiobooks": {false, false}, "ebooks": {false, false},
	}
	for name, w := range want {
		if en, av := moduleEnabled(t, mods, name); en != w[0] || av != w[1] {
			t.Errorf("%s: enabled=%v available=%v, want %v", name, en, av, w)
		}
	}
}

func TestModulesRules(t *testing.T) {
	const lastOne = "At least one media type has to stay switched on."
	cases := []struct {
		name    string
		steps   []map[string]any // applied in order to one server; the last decides
		status  int
		message string
		want    map[string]bool // enabled flags after the last step (when it succeeded)
	}{
		{"music on", []map[string]any{{"music": true}}, 200, "", map[string]bool{"movies": true, "tv": true, "music": true}},
		{"movies off keeps the rest", []map[string]any{{"movies": false}}, 200, "", map[string]bool{"movies": false, "tv": true, "music": false}},
		{"movies and tv off is refused while music is off", []map[string]any{{"movies": false}, {"tv": false}}, 400, lastOne, nil},
		{"movies and tv off in one go is refused", []map[string]any{{"movies": false, "tv": false}}, 400, lastOne, nil},
		{"movies and tv off is fine with music on", []map[string]any{{"music": true}, {"movies": false, "tv": false}}, 200, "", map[string]bool{"movies": false, "tv": false, "music": true}},
		{"turning the last one off is refused", []map[string]any{{"music": true}, {"movies": false, "tv": false}, {"music": false}}, 400, lastOne, nil},
		{"swapping is allowed in one request", []map[string]any{{"movies": false}, {"tv": false, "music": true}}, 200, "", map[string]bool{"movies": false, "tv": false, "music": true}},
		{"audiobooks are coming soon", []map[string]any{{"audiobooks": true}}, 400, "Coming soon", nil},
		{"ebooks are coming soon", []map[string]any{{"ebooks": true}}, 400, "Coming soon", nil},
		{"switching a coming-soon module off is harmless", []map[string]any{{"audiobooks": false}}, 200, "", map[string]bool{"movies": true, "tv": true, "music": false}},
		{"an empty change keeps everything", []map[string]any{{}}, 200, "", map[string]bool{"movies": true, "tv": true, "music": false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, base, client := loginNewServer(t)
			var status int
			var body map[string]any
			for _, step := range tc.steps {
				status, body = doJSONStatus(t, client, http.MethodPut, base+"/api/modules", step)
			}
			if status != tc.status {
				t.Fatalf("status %d %v, want %d", status, body, tc.status)
			}
			if tc.message != "" {
				if body["error"] != tc.message {
					t.Fatalf("error %v, want %q", body["error"], tc.message)
				}
				return
			}
			// The answer is what GET reports afterwards.
			get := getJSON[map[string]any](t, client, base+"/api/modules")
			for name, en := range tc.want {
				if got, _ := moduleEnabled(t, body, name); got != en {
					t.Errorf("answer: %s enabled=%v, want %v", name, got, en)
				}
				if got, _ := moduleEnabled(t, get, name); got != en {
					t.Errorf("GET: %s enabled=%v, want %v", name, got, en)
				}
			}
		})
	}
}

func TestRefusedModuleChangeChangesNothing(t *testing.T) {
	_, base, client := loginNewServer(t)
	doJSONStatus(t, client, http.MethodPut, base+"/api/modules", map[string]any{"movies": false})
	if status, _ := doJSONStatus(t, client, http.MethodPut, base+"/api/modules", map[string]any{"tv": false, "audiobooks": true}); status != http.StatusBadRequest {
		t.Fatalf("status %d", status)
	}
	mods := getJSON[map[string]any](t, client, base+"/api/modules")
	if en, _ := moduleEnabled(t, mods, "tv"); !en {
		t.Fatal("a refused request must not switch tv off")
	}
}

func TestOnlyAdministratorsChangeModulesButEveryoneReadsThem(t *testing.T) {
	_, base, admin, member, _ := familyServer(t)
	if status, body := doJSONStatus(t, member, http.MethodPut, base+"/api/modules", map[string]any{"movies": false}); status != http.StatusForbidden {
		t.Fatalf("member PUT: %d %v", status, body)
	}
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/modules", map[string]any{"tv": false}, http.StatusOK)
	mods := getJSON[map[string]any](t, member, base+"/api/modules")
	if en, _ := moduleEnabled(t, mods, "tv"); en {
		t.Fatalf("a member should see tv switched off: %v", mods)
	}
}

func TestOlderMusicEnabledSettingStillWorks(t *testing.T) {
	server, base, client := loginNewServer(t)
	// A database that only has the older key (set by an earlier build).
	if err := server.Settings.Set("music.enabled", "1", false); err != nil {
		t.Fatal(err)
	}
	if en, _ := moduleEnabled(t, getJSON[map[string]any](t, client, base+"/api/modules"), "music"); !en {
		t.Fatal("music.enabled=1 should keep music on while modules.music is unset")
	}
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"music": false}, http.StatusOK)
	if en, _ := moduleEnabled(t, getJSON[map[string]any](t, client, base+"/api/modules"), "music"); en {
		t.Fatal("switching music off must win over the older key")
	}
	if v, _ := server.Settings.Get("music.enabled"); v != "0" {
		t.Fatalf("the older key is kept in step, got %q", v)
	}
}

func TestSwitchedOffMoviesAndTVAreNotAddedNorHunted(t *testing.T) {
	server, base, client := loginNewServer(t)
	tmdb, _ := newFamilyTMDB(t)
	server.TestSetTMDBBaseURL("k", tmdb.URL)

	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	idx := newMusicIndexer(t, []string{"The.Fixture.Movie.1999.1080p.WEB-DL.x264-GRP"}, "")
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{"name": "I", "definitionId": "x", "baseUrl": idx.URL, "apiKey": "k", "protocol": "usenet"}, http.StatusCreated)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"movies": false, "tv": false, "music": true}, http.StatusOK)

	server.TestHunt(context.Background())
	server.TestRSSSync(context.Background())
	waitBackground(t, server)
	if got := idx.categories(); len(got) != 0 {
		t.Fatalf("automation searched %v while movies and TV are off", got)
	}
	if m, _ := server.MovieRepo.Get(movie.ID); m.Status != library.StatusMissing {
		t.Fatalf("nothing should have been grabbed: %s", m.Status)
	}

	for _, tc := range []struct{ path, msg string }{
		{"/api/movies", "Movies are switched off. Switch them on in Settings > Media types."},
		{"/api/series", "TV is switched off. Switch it on in Settings > Media types."},
	} {
		status, body := doJSONStatus(t, client, http.MethodPost, base+tc.path, map[string]any{"tmdbId": 1399})
		if status != http.StatusConflict || body["error"] != tc.msg {
			t.Errorf("POST %s: %d %v", tc.path, status, body)
		}
	}
	status, body := doJSONStatus(t, client, http.MethodPost, base+"/api/search/grab", map[string]any{"releaseTitle": "The.Fixture.Movie.1999.1080p.WEB-DL.x264-GRP", "downloadUrl": "http://127.0.0.1:1/x.nzb"})
	if status != http.StatusConflict || !strings.Contains(fmt.Sprint(body["error"]), "switched off") {
		t.Errorf("grab from search: %d %v", status, body)
	}

	// Existing data is kept, and switching back on resumes automation.
	if _, err := server.MovieRepo.Get(movie.ID); err != nil {
		t.Fatal(err)
	}
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"movies": true}, http.StatusOK)
	server.TestHunt(context.Background())
	waitBackground(t, server)
	if got := idx.categories(); len(got) == 0 {
		t.Fatal("with movies back on the hunt should search again")
	}
}
