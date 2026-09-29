package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ryanborg/mediarium/internal/api"
	"github.com/ryanborg/mediarium/internal/library"
)

// newFamilyTMDB serves the TMDB endpoints the family-account tests touch:
// movie and show details with genres, genre lists and the discover browse.
// It records the last discover query it was asked.
func newFamilyTMDB(t *testing.T) (*httptest.Server, func() string) {
	t.Helper()
	var (
		mu        sync.Mutex
		lastQuery string
	)
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/movie/603", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"id": 603, "title": "The Matrix", "release_date": "1999-03-31",
			"genres": []map[string]any{{"id": 28, "name": "Action"}, {"id": 878, "name": "Science Fiction"}}})
	})
	mux.HandleFunc("/movie/604", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"id": 604, "title": "The Matrix Reloaded", "release_date": "2003-05-15",
			"genres": []map[string]any{{"id": 28, "name": "Action"}}})
	})
	mux.HandleFunc("/tv/1399", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"id": 1399, "name": "Fixture Show", "first_air_date": "2011-04-17",
			"genres":  []map[string]any{{"id": 18, "name": "Drama"}},
			"seasons": []map[string]any{{"season_number": 1, "episode_count": 1}}})
	})
	mux.HandleFunc("/tv/1399/season/1", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"episodes": []map[string]any{{"season_number": 1, "episode_number": 1, "name": "Pilot", "air_date": "2011-04-17"}}})
	})
	mux.HandleFunc("/genre/movie/list", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"genres": []map[string]any{{"id": 878, "name": "Science Fiction"}, {"id": 28, "name": "Action"}}})
	})
	mux.HandleFunc("/genre/tv/list", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"genres": []map[string]any{{"id": 18, "name": "Drama"}}})
	})
	mux.HandleFunc("/discover/movie", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		lastQuery = r.URL.RawQuery
		mu.Unlock()
		write(w, map[string]any{"page": 1, "total_pages": 3, "results": []map[string]any{
			{"id": 603, "title": "The Matrix", "release_date": "1999-03-31", "genre_ids": []int{28}, "vote_average": 8.21},
			{"id": 999, "title": "Not In Library", "release_date": "1999-01-01", "genre_ids": []int{878}},
		}})
	})
	mux.HandleFunc("/discover/tv", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		lastQuery = r.URL.RawQuery
		mu.Unlock()
		write(w, map[string]any{"page": 2, "total_pages": 2, "results": []map[string]any{
			{"id": 1399, "name": "Fixture Show", "first_air_date": "2011-04-17", "genre_ids": []int{18}},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func() string {
		mu.Lock()
		defer mu.Unlock()
		return lastQuery
	}
}

// familyServer is a server with an administrator (signed in on admin) and a
// member account "sam" (signed in on member).
func familyServer(t *testing.T) (server *api.Server, base string, admin, member *http.Client, memberID float64) {
	t.Helper()
	server, base, admin = loginNewServer(t)
	created := postJSON[map[string]any](t, admin, base+"/api/users", map[string]any{
		"username": "sam", "password": "kids-password", "name": "Sam", "role": "member",
	}, http.StatusCreated)
	jar, _ := cookiejar.New(nil)
	member = &http.Client{Jar: jar, Timeout: 20 * time.Second}
	login := postJSON[map[string]any](t, member, base+"/api/auth/login", map[string]string{
		"username": "sam", "password": "kids-password",
	}, http.StatusOK)
	if login["role"] != "member" || login["isAdmin"] != false {
		t.Fatalf("member login payload: %+v", login)
	}
	return server, base, admin, member, created["id"].(float64)
}

func doStatus(t *testing.T, client *http.Client, method, url string) (int, map[string]any) {
	t.Helper()
	var body *strings.Reader
	if method == http.MethodGet || method == http.MethodDelete {
		body = strings.NewReader("")
	} else {
		body = strings.NewReader("{}")
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func fillPattern(pattern string) (method, path string) {
	method, path, _ = strings.Cut(pattern, " ")
	r := strings.NewReplacer("{id}", "999999", "{season}", "1", "{tmdbId}", "999999")
	return method, r.Replace(path)
}

func TestMemberIsRefusedEveryAdminRouteAndAllowedEveryMemberRoute(t *testing.T) {
	server, base, _, member, _ := familyServer(t)
	tmdb, _ := newFamilyTMDB(t)
	server.TestSetTMDBBaseURL("k", tmdb.URL)

	routes := server.TestRouteAccess()
	var admins, members int
	for pattern, level := range routes {
		method, path := fillPattern(pattern)
		status, body := doStatus(t, member, method, base+path)
		switch level {
		case "admin":
			admins++
			if status != http.StatusForbidden || body["error"] != "Only an administrator can do this." {
				t.Errorf("member %s: want 403 with the admin message, got %d %v", pattern, status, body)
			}
		case "member":
			members++
			if status == http.StatusForbidden || status == http.StatusUnauthorized {
				t.Errorf("member %s: must be allowed, got %d %v", pattern, status, body)
			}
		default:
			t.Errorf("%s: unexpected access level %q", pattern, level)
		}
	}
	if admins < 50 || members < 40 {
		t.Fatalf("suspiciously few routes walked: %d admin, %d member", admins, members)
	}

	// The everyday pages load for a member.
	for _, path := range []string{
		"/api/auth/me", "/api/movies", "/api/series", "/api/queue", "/api/activity", "/api/calendar",
		"/api/wanted", "/api/dashboard", "/api/health", "/api/quality-profiles", "/api/auth/api-keys",
		"/api/subtitles/wanted", "/api/discover/genres?kind=movie",
	} {
		if status, body := doStatus(t, member, http.MethodGet, base+path); status != http.StatusOK {
			t.Errorf("member GET %s: want 200, got %d %v", path, status, body)
		}
	}

	// Members see no setup problems and no server paths.
	health := getJSON[map[string]any](t, member, base+"/api/health")
	if items := health["items"].([]any); len(items) != 0 {
		t.Fatalf("members should get no health items, got %v", items)
	}
	dash := getJSON[map[string]any](t, member, base+"/api/dashboard")
	for _, f := range dash["folders"].([]any) {
		if p := f.(map[string]any)["path"]; p != "" {
			t.Fatalf("members should not see folder paths, got %v", p)
		}
	}
	me := getJSON[map[string]any](t, member, base+"/api/auth/me")
	if me["role"] != "member" || me["isAdmin"] != false || me["name"] != "Sam" {
		t.Fatalf("member /me: %+v", me)
	}
}

func TestAdminManagesAccounts(t *testing.T) {
	_, base, admin, member, memberID := familyServer(t)
	users := base + "/api/users"

	list := getJSON[[]map[string]any](t, admin, users)
	if len(list) != 2 || list[0]["role"] != "admin" || list[1]["role"] != "member" || list[1]["name"] != "Sam" {
		t.Fatalf("unexpected account list: %+v", list)
	}
	if list[1]["lastLoginAt"] == nil || list[1]["createdAt"] == nil {
		t.Fatalf("member signed in, so lastLoginAt should be set: %+v", list[1])
	}
	adminID := list[0]["id"].(float64)

	// Validation and the plain-message conflicts.
	for name, tc := range map[string]struct {
		body map[string]any
		want int
	}{
		"taken username": {map[string]any{"username": "sam", "password": "another-password", "role": "member"}, http.StatusConflict},
		"short password": {map[string]any{"username": "alex", "password": "short", "role": "member"}, http.StatusBadRequest},
		"unknown role":   {map[string]any{"username": "alex", "password": "long-enough-pw", "role": "owner"}, http.StatusBadRequest},
		"bad email":      {map[string]any{"username": "alex", "password": "long-enough-pw", "role": "member", "email": "nope"}, http.StatusBadRequest},
	} {
		got := postJSON[map[string]any](t, admin, users, tc.body, tc.want)
		if got["error"] == nil {
			t.Fatalf("%s: expected an error message, got %+v", name, got)
		}
	}
	dup := postJSON[map[string]any](t, admin, users, map[string]any{"username": "sam", "password": "another-password", "role": "member"}, http.StatusConflict)
	if dup["error"] != "That username is already taken." {
		t.Fatalf("duplicate username message: %+v", dup)
	}

	// An administrator cannot delete or demote themselves.
	self := users + "/" + ftoa(adminID)
	putJSONStatus(t, admin, self, map[string]any{"role": "member"}, http.StatusBadRequest)
	if status, _ := doStatus(t, admin, http.MethodDelete, self); status != http.StatusBadRequest {
		t.Fatalf("self delete: want 400, got %d", status)
	}

	// Edit the member: name, email, then promote and demote again.
	them := users + "/" + ftoa(memberID)
	updated := putJSONStatus(t, admin, them, map[string]any{"name": "Sam Borg", "email": "sam@example.com"}, http.StatusOK)
	if updated["name"] != "Sam Borg" || updated["email"] != "sam@example.com" || updated["role"] != "member" {
		t.Fatalf("update: %+v", updated)
	}
	if got := putJSONStatus(t, admin, them, map[string]any{"role": "admin"}, http.StatusOK); got["role"] != "admin" {
		t.Fatalf("promote: %+v", got)
	}
	if status, _ := doStatus(t, member, http.MethodGet, base+"/api/settings"); status != http.StatusOK {
		t.Fatalf("a promoted account gets admin routes straight away, got %d", status)
	}
	putJSONStatus(t, admin, them, map[string]any{"role": "member"}, http.StatusOK)
	if status, _ := doStatus(t, member, http.MethodGet, base+"/api/settings"); status != http.StatusForbidden {
		t.Fatalf("a demoted account loses admin routes straight away, got %d", status)
	}

	// A password reset signs the member out and the new password works.
	putJSONStatus(t, admin, them, map[string]any{"password": "short"}, http.StatusBadRequest)
	putJSONStatus(t, admin, them, map[string]any{"password": "a-new-password"}, http.StatusOK)
	if status, _ := doStatus(t, member, http.MethodGet, base+"/api/auth/me"); status != http.StatusUnauthorized {
		t.Fatalf("reset should sign the member out, got %d", status)
	}
	postJSON[map[string]any](t, member, base+"/api/auth/login", map[string]string{"username": "sam", "password": "a-new-password"}, http.StatusOK)

	// Deleting ends the member's session; a second delete is a 404.
	if status, _ := doStatus(t, admin, http.MethodDelete, them); status != http.StatusOK {
		t.Fatalf("delete member: got %d", status)
	}
	if status, _ := doStatus(t, member, http.MethodGet, base+"/api/auth/me"); status != http.StatusUnauthorized {
		t.Fatalf("deleted account's session should be gone, got %d", status)
	}
	if status, _ := doStatus(t, admin, http.MethodDelete, them); status != http.StatusNotFound {
		t.Fatalf("second delete: want 404, got %d", status)
	}
	putJSONStatus(t, admin, users+"/424242", map[string]any{"name": "x"}, http.StatusNotFound)
}

// ftoa formats a JSON number id for a URL.
func ftoa(f float64) string { return strconv.FormatInt(int64(f), 10) }

func TestMemberAddsAreCreditedAndCarryGenres(t *testing.T) {
	server, base, admin, member, memberID := familyServer(t)
	tmdb, _ := newFamilyTMDB(t)
	server.TestSetTMDBBaseURL("k", tmdb.URL)

	movie := postJSON[map[string]any](t, member, base+"/api/movies", map[string]any{"tmdbId": 603}, http.StatusCreated)
	by, _ := movie["addedBy"].(map[string]any)
	if by == nil || by["id"] != memberID || by["name"] != "Sam" {
		t.Fatalf("addedBy on the new movie: %+v", movie["addedBy"])
	}
	if strings.Join(toStrings(movie["genres"]), ",") != "Action,Science Fiction" {
		t.Fatalf("genres on the new movie: %v", movie["genres"])
	}
	show := postJSON[map[string]any](t, member, base+"/api/series", map[string]any{"tmdbId": 1399}, http.StatusCreated)
	if by, _ := show["addedBy"].(map[string]any); by == nil || by["name"] != "Sam" || strings.Join(toStrings(show["genres"]), ",") != "Drama" {
		t.Fatalf("new show: addedBy %v genres %v", show["addedBy"], show["genres"])
	}

	// The admin sees the same in the lists, and the activity feed says who.
	movies := getJSON[[]map[string]any](t, admin, base+"/api/movies")
	if len(movies) != 1 || movies[0]["addedBy"].(map[string]any)["name"] != "Sam" {
		t.Fatalf("movie list: %+v", movies)
	}
	activity := getJSON[[]map[string]any](t, admin, base+"/api/activity")
	found := false
	for _, a := range activity {
		if a["message"] == "The Matrix added to library by Sam" {
			found = true
		}
	}
	if !found {
		t.Fatalf("activity should credit Sam: %+v", activity)
	}

	// Titles added some other way have no adder and get their genres from the
	// background job; a title TMDB does not know gets an empty list.
	old, err := server.MovieRepo.Add(library.Movie{TMDBID: 604, Title: "The Matrix Reloaded", Monitored: true})
	if err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	gone, err := server.MovieRepo.Add(library.Movie{TMDBID: 605, Title: "Gone From TMDB", Monitored: true})
	if err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	server.TestBackfillGenres(context.Background())
	got := getJSON[map[string]any](t, admin, base+"/api/movies/"+ftoa(float64(old.ID)))
	if got["addedBy"] != nil || strings.Join(toStrings(got["genres"]), ",") != "Action" {
		t.Fatalf("backfilled movie: addedBy %v genres %v", got["addedBy"], got["genres"])
	}
	if m, _ := server.MovieRepo.Get(gone.ID); m.Genres == nil || len(m.Genres) != 0 {
		t.Fatalf("a title TMDB does not know should get an empty list, got %#v", m.Genres)
	}

	// Deleting the member leaves the title in place, credited to nobody.
	if status, _ := doStatus(t, admin, http.MethodDelete, base+"/api/users/"+ftoa(memberID)); status != http.StatusOK {
		t.Fatalf("delete member: %d", status)
	}
	movies = getJSON[[]map[string]any](t, admin, base+"/api/movies")
	for _, m := range movies {
		if m["addedBy"] != nil {
			t.Fatalf("a deleted account should resolve to null: %+v", m)
		}
	}
}

func TestDiscoverBrowseAndGenres(t *testing.T) {
	server, base, _, member, _ := familyServer(t)
	tmdb, lastQuery := newFamilyTMDB(t)
	server.TestSetTMDBBaseURL("k", tmdb.URL)
	postJSON[map[string]any](t, member, base+"/api/movies", map[string]any{"tmdbId": 603}, http.StatusCreated)

	genres := getJSON[[]map[string]any](t, member, base+"/api/discover/genres?kind=movie")
	if len(genres) != 2 || genres[0]["name"] != "Action" || genres[0]["id"] != float64(28) {
		t.Fatalf("movie genres should be sorted by name: %+v", genres)
	}

	page := getJSON[map[string]any](t, member, base+"/api/discover/browse?kind=movie&genre=28&yearFrom=1990&yearTo=1999&sort=rating&page=1")
	if page["page"] != float64(1) || page["totalPages"] != float64(3) {
		t.Fatalf("paging: %+v", page)
	}
	q := lastQuery()
	for _, want := range []string{"with_genres=28", "primary_release_date.gte=1990-01-01", "primary_release_date.lte=1999-12-31", "sort_by=vote_average.desc", "vote_count.gte=200"} {
		if !strings.Contains(q, want) {
			t.Fatalf("TMDB query %q should contain %q", q, want)
		}
	}
	results := page["results"].([]any)
	first, second := results[0].(map[string]any), results[1].(map[string]any)
	if first["inLibrary"] != true || first["status"] != "missing" || first["mediaType"] != "movie" || first["rating"] != 8.2 ||
		strings.Join(toStrings(first["genres"]), ",") != "Action" {
		t.Fatalf("first result: %+v", first)
	}
	if second["inLibrary"] != false || second["libraryId"] != nil {
		t.Fatalf("second result should not be in the library: %+v", second)
	}

	tv := getJSON[map[string]any](t, member, base+"/api/discover/browse?kind=tv&year=2011&sort=newest&page=2")
	if tv["page"] != float64(2) || !strings.Contains(lastQuery(), "first_air_date_year=2011") || !strings.Contains(lastQuery(), "sort_by=first_air_date.desc") {
		t.Fatalf("tv browse: %+v query %s", tv, lastQuery())
	}
	if r := tv["results"].([]any)[0].(map[string]any); r["mediaType"] != "tv" || strings.Join(toStrings(r["genres"]), ",") != "Drama" {
		t.Fatalf("tv result: %+v", r)
	}

	for _, bad := range []string{"kind=books", "sort=best", "year=abc", "yearFrom=2000&yearTo=1990"} {
		if status, _ := doStatus(t, member, http.MethodGet, base+"/api/discover/browse?"+bad); status != http.StatusBadRequest {
			t.Fatalf("browse?%s: want 400, got %d", bad, status)
		}
	}
}
