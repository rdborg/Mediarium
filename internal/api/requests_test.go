package api_test

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"testing"
)

func TestRequestsAndPermissions(t *testing.T) {
	server, base, admin := loginNewServer(t)
	server.TestSetOpenLibrary(newFakeOpenLibrary(t).URL)
	postJSONMethod[map[string]any](t, admin, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)

	// A family account that has to ask, and may not pick releases.
	perms := map[string]any{"addDirect": false, "movies": true, "tv": true, "music": true, "books": true, "releases": false, "manage": true, "retry": true, "subtitles": true, "play": true}
	created := postJSON[map[string]any](t, admin, base+"/api/users", map[string]any{"username": "sam", "password": "sam-password-1", "role": "member", "permissions": perms}, http.StatusCreated)
	if p := created["permissions"].(map[string]any); p["addDirect"] != false || p["releases"] != false || p["play"] != true {
		t.Fatalf("permissions not saved: %v", p)
	}
	jar, _ := cookiejar.New(nil)
	sam := &http.Client{Jar: jar}
	postJSON[map[string]any](t, sam, base+"/api/auth/login", map[string]string{"username": "sam", "password": "sam-password-1"}, http.StatusOK)
	if me := getJSON[map[string]any](t, sam, base+"/api/auth/me"); me["permissions"].(map[string]any)["addDirect"] != false {
		t.Fatalf("me: %v", me)
	}

	// Picking releases is refused; adding a book becomes a request.
	if status, _ := doStatus(t, sam, http.MethodGet, base+"/api/search?q=x"); status != http.StatusForbidden {
		t.Fatalf("search without the permission: %d", status)
	}
	book := map[string]any{"olKey": "OL1W", "title": "The Hobbit", "author": "J.R.R. Tolkien", "year": 1937, "coverId": 123, "ebook": true}
	res := postJSON[map[string]any](t, sam, base+"/api/books", book, http.StatusAccepted)
	if res["requested"] != true {
		t.Fatalf("add as a request: %v", res)
	}
	postJSON[map[string]any](t, sam, base+"/api/books", book, http.StatusConflict) // asked already
	if list := getJSON[[]map[string]any](t, admin, base+"/api/books"); len(list) != 0 {
		t.Fatalf("a request added the book: %v", list)
	}
	mine := getJSON[map[string]any](t, sam, base+"/api/requests")
	reqs := mine["requests"].([]any)
	if len(reqs) != 1 || mine["pending"] != float64(1) {
		t.Fatalf("sam's requests: %v", mine)
	}
	id := reqs[0].(map[string]any)["id"]

	// Sam can't approve; the admin can, and the book is added for Sam.
	if status, _ := doStatus(t, sam, http.MethodPost, fmt.Sprintf("%s/api/requests/%v/approve", base, id)); status != http.StatusForbidden {
		t.Fatalf("a member approved: %d", status)
	}
	approved := postJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/requests/%v/approve", base, id), nil, http.StatusOK)
	if approved["status"] != "approved" {
		t.Fatalf("approve: %v", approved)
	}
	waitBackground(t, server)
	if list := getJSON[[]map[string]any](t, admin, base+"/api/books"); len(list) != 1 || list[0]["title"] != "The Hobbit" {
		t.Fatalf("approved book not added: %v", list)
	}
	postJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/requests/%v/approve", base, id), nil, http.StatusConflict)

	// A second request, declined with a note, and one taken back.
	other := map[string]any{"olKey": "OL2W", "title": "Farmer Giles", "author": "J.R.R. Tolkien", "ebook": true}
	postJSON[map[string]any](t, sam, base+"/api/books", other, http.StatusAccepted)
	all := getJSON[map[string]any](t, admin, base+"/api/requests")["requests"].([]any)
	second := all[0].(map[string]any)["id"]
	declined := postJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/requests/%v/decline", base, second), map[string]string{"note": "Not now"}, http.StatusOK)
	if declined["status"] != "declined" || declined["note"] != "Not now" {
		t.Fatalf("decline: %v", declined)
	}
	if status, _ := doStatus(t, sam, http.MethodDelete, fmt.Sprintf("%s/api/requests/%v", base, second)); status != http.StatusForbidden {
		t.Fatalf("an answered request was taken back: %d", status)
	}

	// Allowed to add directly again: adds go straight in.
	perms["addDirect"] = true
	perms["books"] = false
	postJSONMethod[map[string]any](t, admin, http.MethodPut, fmt.Sprintf("%s/api/users/%v", base, created["id"]), map[string]any{"permissions": perms}, http.StatusOK)
	if status, _ := doStatus(t, sam, http.MethodPost, base+"/api/books"); status != http.StatusForbidden {
		t.Fatalf("books switched off for sam, still allowed: %d", status)
	}
}
