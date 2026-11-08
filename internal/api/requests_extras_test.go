package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
)

// newExtrasTMDB serves what the request extras read: movies with a rating (R,
// PG, none), two shows with a content rating and their seasons.
func newExtrasTMDB(t *testing.T) *httptest.Server {
	t.Helper()
	movie := func(id int, title, cert string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			out := map[string]any{"id": id, "title": title, "release_date": "2001-05-01", "poster_path": "/m.jpg"}
			if cert != "" {
				out["release_dates"] = map[string]any{"results": []map[string]any{
					{"iso_3166_1": "US", "release_dates": []map[string]any{{"certification": cert, "type": 3}}},
				}}
			}
			json.NewEncoder(w).Encode(out)
		}
	}
	show := func(id int, name, rating string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"id": id, "name": name, "first_air_date": "2011-04-17", "poster_path": "/p.jpg",
				"seasons": []map[string]any{{"season_number": 0}, {"season_number": 1}, {"season_number": 2}},
				"status":  "Ended",
				"content_ratings": map[string]any{"results": []map[string]any{
					{"iso_3166_1": "US", "rating": rating},
				}},
			})
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/movie/603", movie(603, "Hard Movie", "R"))
	mux.HandleFunc("/movie/604", movie(604, "Family Movie", "PG"))
	mux.HandleFunc("/movie/605", movie(605, "Mystery Movie", ""))
	mux.HandleFunc("/tv/1399", show(1399, "Fixture Show", "TV-14"))
	mux.HandleFunc("/tv/1400", show(1400, "Grown Up Show", "TV-MA"))
	for _, id := range []int{1399, 1400} {
		mux.HandleFunc(fmt.Sprintf("/tv/%d/season/1", id), func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"episodes": []map[string]any{
				{"season_number": 1, "episode_number": 1, "name": "Pilot", "air_date": "2011-04-17"},
				{"season_number": 1, "episode_number": 2, "name": "Second", "air_date": "2011-04-24"},
			}})
		})
		mux.HandleFunc(fmt.Sprintf("/tv/%d/season/2", id), func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"episodes": []map[string]any{
				{"season_number": 2, "episode_number": 1, "name": "Return", "air_date": "2012-04-01"},
			}})
		})
		mux.HandleFunc(fmt.Sprintf("/tv/%d/season/0", id), func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"episodes": []map[string]any{}})
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// extrasMember creates a member account and signs it in. mustAsk = true is an
// account whose adds become requests.
func extrasMember(t *testing.T, admin *http.Client, base, name string, mustAsk bool) (*http.Client, int64) {
	t.Helper()
	perms := map[string]any{"addDirect": !mustAsk, "movies": true, "tv": true, "music": true, "books": true, "releases": true, "manage": true, "retry": true, "subtitles": true, "play": true}
	created := postJSON[map[string]any](t, admin, base+"/api/users", map[string]any{"username": name, "password": name + "-password-1", "role": "member", "permissions": perms}, http.StatusCreated)
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	postJSON[map[string]any](t, c, base+"/api/auth/login", map[string]string{"username": name, "password": name + "-password-1"}, http.StatusOK)
	return c, int64(created["id"].(float64))
}

func requestRows(t *testing.T, c *http.Client, base string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, x := range getJSON[map[string]any](t, c, base+"/api/requests")["requests"].([]any) {
		out = append(out, x.(map[string]any))
	}
	return out
}

// A member who has to ask can ask for one episode. Approving it adds the show
// with only that episode wanted; asking for another episode of a show that is
// in the library only makes that one wanted.
func TestSingleEpisodeRequest(t *testing.T) {
	server, base, admin := loginNewServer(t)
	server.TestSetTMDBBaseURL("fixture-tmdb-key", newExtrasTMDB(t).URL)
	sam, _ := extrasMember(t, admin, base, "sam", true)

	asked := postJSON[map[string]any](t, sam, base+"/api/series", map[string]any{"tmdbId": 1399, "episode": map[string]any{"season": 2, "number": 1}}, http.StatusAccepted)
	if asked["requested"] != true {
		t.Fatalf("not a request: %v", asked)
	}
	rows := requestRows(t, sam, base)
	if len(rows) != 1 || rows[0]["title"] != "Fixture Show S02E01" || rows[0]["action"] != "add" {
		t.Fatalf("the list should say which episode: %v", rows)
	}
	if _, has := rows[0]["year"]; has {
		t.Fatalf("an episode request shows no year after the episode: %v", rows[0])
	}
	// Asked twice, an episode the show doesn't have, and a bad number.
	postJSON[map[string]any](t, sam, base+"/api/series", map[string]any{"tmdbId": 1399, "episode": map[string]any{"season": 2, "number": 1}}, http.StatusConflict)
	postJSON[map[string]any](t, sam, base+"/api/series", map[string]any{"tmdbId": 1399, "episode": map[string]any{"season": 2, "number": 9}}, http.StatusBadRequest)
	postJSON[map[string]any](t, sam, base+"/api/series", map[string]any{"tmdbId": 1399, "episode": map[string]any{"season": 1, "number": 0}}, http.StatusBadRequest)
	// The whole show is a different request from one of its episodes.
	postJSON[map[string]any](t, sam, base+"/api/series", map[string]any{"tmdbId": 1399}, http.StatusAccepted)
	if list := getJSON[[]map[string]any](t, admin, base+"/api/series"); len(list) != 0 {
		t.Fatalf("a request added a show: %v", list)
	}

	// Sam can't approve. The admin can; the show comes in with one episode wanted.
	id := rows[0]["id"]
	if status, _ := doStatus(t, sam, http.MethodPost, fmt.Sprintf("%s/api/requests/%v/approve", base, id)); status != http.StatusForbidden {
		t.Fatalf("a member approved: %d", status)
	}
	approved := postJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/requests/%v/approve", base, id), nil, http.StatusOK)
	if approved["status"] != "approved" {
		t.Fatalf("approve: %v", approved)
	}
	waitBackground(t, server)
	list := getJSON[[]map[string]any](t, admin, base+"/api/series")
	if len(list) != 1 {
		t.Fatalf("the show wasn't added: %v", list)
	}
	showID := int64(list[0]["id"].(float64))
	wanted := func() map[string]bool {
		out := map[string]bool{}
		detail := getJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/series/%d", base, showID))
		for _, e := range detail["episodes"].([]any) {
			ep := e.(map[string]any)
			if ep["monitored"] == true {
				out[fmt.Sprintf("S%02.0fE%02.0f", ep["season"], ep["episode"])] = true
			}
		}
		return out
	}
	if got := wanted(); len(got) != 1 || !got["S02E01"] {
		t.Fatalf("only the asked-for episode should be wanted: %v", got)
	}

	// Now the show is in the library: asking for another episode is fine, and
	// asking for the one that is wanted already is not.
	postJSON[map[string]any](t, sam, base+"/api/series", map[string]any{"tmdbId": 1399, "episode": map[string]any{"season": 2, "number": 1}}, http.StatusConflict)
	postJSON[map[string]any](t, sam, base+"/api/series", map[string]any{"tmdbId": 1399, "episode": map[string]any{"season": 1, "number": 2}}, http.StatusAccepted)
	var second any
	for _, r := range requestRows(t, admin, base) {
		if r["title"] == "Fixture Show S01E02" {
			second = r["id"]
		}
	}
	postJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/requests/%v/approve", base, second), nil, http.StatusOK)
	waitBackground(t, server)
	if got := wanted(); len(got) != 2 || !got["S01E02"] || !got["S02E01"] {
		t.Fatalf("the second episode should be wanted too: %v", got)
	}
}

// An account with an age limit can't ask for, or add, a title above it. A
// title with no rating is allowed unless the account says otherwise.
func TestAgeLimits(t *testing.T) {
	server, base, admin := loginNewServer(t)
	server.TestSetTMDBBaseURL("fixture-tmdb-key", newExtrasTMDB(t).URL)
	sam, samID := extrasMember(t, admin, base, "sam", true)  // asks
	kim, kimID := extrasMember(t, admin, base, "kim", false) // adds directly
	settings := base + "/api/requests/settings"
	limit := func(id int64, max string, block bool) map[string]any {
		return map[string]any{"ageLimits": map[string]any{fmt.Sprint(id): map[string]any{"maxRating": max, "blockUnrated": block}}}
	}

	// Only an administrator sets limits, and only values the scale knows.
	if status, _ := doStatus(t, sam, http.MethodPut, settings); status != http.StatusForbidden {
		t.Fatalf("a member changed the settings: %d", status)
	}
	postJSONMethod[map[string]any](t, admin, http.MethodPut, settings, limit(samID, "Banana", false), http.StatusBadRequest)
	postJSONMethod[map[string]any](t, admin, http.MethodPut, settings, limit(samID, "PG-13", false), http.StatusOK)
	postJSONMethod[map[string]any](t, admin, http.MethodPut, settings, limit(kimID, "12", false), http.StatusOK)
	for _, a := range getJSON[map[string]any](t, admin, settings)["accounts"].([]any) {
		acc := a.(map[string]any)
		switch acc["name"] {
		case "sam":
			if acc["maxRating"] != "PG-13" {
				t.Fatalf("sam's limit: %v", acc)
			}
		case "kim":
			if acc["maxRating"] != "12" {
				t.Fatalf("kim's limit: %v", acc)
			}
		}
	}

	// Above the limit: a request is refused with a plain message, nothing is saved.
	res := postJSON[map[string]any](t, sam, base+"/api/movies", map[string]any{"tmdbId": 603}, http.StatusForbidden)
	if msg, _ := res["error"].(string); msg == "" {
		t.Fatalf("no message: %v", res)
	}
	if rows := requestRows(t, sam, base); len(rows) != 0 {
		t.Fatalf("a refused request was saved: %v", rows)
	}
	// At or below the limit, or with no rating, is fine; so is a show at the same level (TV-14).
	postJSON[map[string]any](t, sam, base+"/api/movies", map[string]any{"tmdbId": 604}, http.StatusAccepted)
	postJSON[map[string]any](t, sam, base+"/api/movies", map[string]any{"tmdbId": 605}, http.StatusAccepted)
	postJSON[map[string]any](t, sam, base+"/api/series", map[string]any{"tmdbId": 1399}, http.StatusAccepted)
	postJSON[map[string]any](t, sam, base+"/api/series", map[string]any{"tmdbId": 1400}, http.StatusForbidden)
	postJSON[map[string]any](t, sam, base+"/api/series", map[string]any{"tmdbId": 1400, "episode": map[string]any{"season": 1, "number": 1}}, http.StatusForbidden)

	// An account that adds directly has the same limit.
	postJSON[map[string]any](t, kim, base+"/api/movies", map[string]any{"tmdbId": 603}, http.StatusForbidden)
	postJSON[map[string]any](t, kim, base+"/api/movies", map[string]any{"tmdbId": 604}, http.StatusCreated)

	// Unrated titles are refused once the account says so.
	postJSONMethod[map[string]any](t, admin, http.MethodPut, settings, limit(kimID, "12", true), http.StatusOK)
	postJSON[map[string]any](t, kim, base+"/api/movies", map[string]any{"tmdbId": 605}, http.StatusForbidden)

	// No limit again, and administrators are never held back.
	postJSONMethod[map[string]any](t, admin, http.MethodPut, settings, limit(kimID, "", false), http.StatusOK)
	postJSON[map[string]any](t, kim, base+"/api/movies", map[string]any{"tmdbId": 603}, http.StatusCreated)
	postJSONMethod[map[string]any](t, admin, http.MethodPut, settings, limit(samID, "G", false), http.StatusOK)
	postJSON[map[string]any](t, admin, base+"/api/movies", map[string]any{"tmdbId": 605}, http.StatusCreated)

	// An administrator approving a request that was fine when it was made
	// isn't stopped by a limit set since.
	var pending any
	for _, r := range requestRows(t, admin, base) {
		if r["title"] == "Family Movie" {
			pending = r["id"]
		}
	}
	postJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/requests/%v/approve", base, pending), nil, http.StatusOK)
}

// A member can ask for a title in the library to be removed. An administrator
// approves it (the title goes, its files stay unless asked) or declines it.
func TestRemovalRequests(t *testing.T) {
	server, base, admin := loginNewServer(t)
	server.TestSetTMDBBaseURL("fixture-tmdb-key", newExtrasTMDB(t).URL)
	sam, _ := extrasMember(t, admin, base, "sam", true)

	show := postJSON[map[string]any](t, admin, base+"/api/series", map[string]any{"tmdbId": 1399}, http.StatusCreated)
	movie := postJSON[map[string]any](t, admin, base+"/api/movies", map[string]any{"tmdbId": 604}, http.StatusCreated)
	showID, movieID := int64(show["id"].(float64)), int64(movie["id"].(float64))
	removal := base + "/api/requests/removal"

	// Bad asks: an administrator removes things itself, unknown titles, other kinds.
	postJSON[map[string]any](t, admin, removal, map[string]any{"kind": "tv", "itemId": showID}, http.StatusBadRequest)
	postJSON[map[string]any](t, sam, removal, map[string]any{"kind": "tv", "itemId": 999}, http.StatusNotFound)
	postJSON[map[string]any](t, sam, removal, map[string]any{"kind": "book", "itemId": 1}, http.StatusForbidden)
	postJSON[map[string]any](t, sam, removal, map[string]any{"kind": "music", "itemId": 1}, http.StatusForbidden)

	res := postJSON[map[string]any](t, sam, removal, map[string]any{"kind": "tv", "itemId": showID, "reason": "We finished it"}, http.StatusAccepted)
	if res["requested"] != true {
		t.Fatalf("removal: %v", res)
	}
	postJSON[map[string]any](t, sam, removal, map[string]any{"kind": "tv", "itemId": showID}, http.StatusConflict) // asked already
	postJSON[map[string]any](t, sam, removal, map[string]any{"kind": "movie", "itemId": movieID}, http.StatusAccepted)

	var showReq, movieReq map[string]any
	for _, r := range requestRows(t, admin, base) {
		if r["action"] != "remove" {
			t.Fatalf("expected removal requests only: %v", r)
		}
		if r["kind"] == "tv" {
			showReq = r
		} else {
			movieReq = r
		}
	}
	if showReq["reason"] != "We finished it" || showReq["status"] != "pending" || showReq["requester"] == "" {
		t.Fatalf("the admin should see who asked and why: %v", showReq)
	}
	// Asking doesn't remove anything, and sam can't answer.
	if list := getJSON[[]map[string]any](t, admin, base+"/api/series"); len(list) != 1 {
		t.Fatalf("the show went early: %v", list)
	}
	if status, _ := doStatus(t, sam, http.MethodPost, fmt.Sprintf("%s/api/requests/%v/approve", base, showReq["id"])); status != http.StatusForbidden {
		t.Fatalf("a member approved a removal: %d", status)
	}
	if status, _ := doStatus(t, sam, http.MethodPost, fmt.Sprintf("%s/api/requests/%v/decline", base, showReq["id"])); status != http.StatusForbidden {
		t.Fatalf("a member declined a removal: %d", status)
	}

	// Approved: the show goes, with its files kept unless the admin says otherwise.
	done := postJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/requests/%v/approve", base, showReq["id"]), map[string]any{}, http.StatusOK)
	if done["status"] != "approved" || done["note"] != "The files were kept." {
		t.Fatalf("approved removal: %v", done)
	}
	if list := getJSON[[]map[string]any](t, admin, base+"/api/series"); len(list) != 0 {
		t.Fatalf("the show is still there: %v", list)
	}
	postJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/requests/%v/approve", base, showReq["id"]), nil, http.StatusConflict)

	// Declined: the movie stays, and the note is kept.
	no := postJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/requests/%v/decline", base, movieReq["id"]), map[string]string{"note": "Others still watch it"}, http.StatusOK)
	if no["status"] != "declined" || no["note"] != "Others still watch it" {
		t.Fatalf("declined removal: %v", no)
	}
	if list := getJSON[[]map[string]any](t, admin, base+"/api/movies"); len(list) != 1 {
		t.Fatalf("a declined removal removed the movie: %v", list)
	}
	// A removal doesn't count against the request quota.
	if q := getJSON[map[string]any](t, sam, base+"/api/requests/status")["quota"].(map[string]any); q["used"] != float64(0) {
		t.Fatalf("quota used: %v", q)
	}

	// A request for a title that went away some other way is still answered.
	postJSON[map[string]any](t, sam, removal, map[string]any{"kind": "movie", "itemId": movieID}, http.StatusAccepted)
	postJSONMethod[map[string]any](t, admin, http.MethodDelete, fmt.Sprintf("%s/api/movies/%d", base, movieID), nil, http.StatusOK)
	var again any
	for _, r := range requestRows(t, admin, base) {
		if r["status"] == "pending" {
			again = r["id"]
		}
	}
	gone := postJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/requests/%v/approve", base, again), nil, http.StatusOK)
	if gone["status"] != "approved" || gone["note"] != "It was already gone from the library." {
		t.Fatalf("already gone: %v", gone)
	}
}
