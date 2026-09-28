package api_test

import (
	"net/http"
	"testing"
)

func TestOnboardingNameIsOptionalAndEmailMustBeValid(t *testing.T) {
	_, httpSrv, client := newHuntTestServer(t)
	url := httpSrv.URL + "/api/onboarding/admin"

	for name, body := range map[string]map[string]string{
		"malformed mail": {"username": "ryan", "password": "correct-horse-battery-staple", "name": "Ryan B", "email": "nope"},
	} {
		got := postJSON[map[string]any](t, client, url, body, http.StatusBadRequest)
		if got["error"] == nil {
			t.Fatalf("%s: expected an error message, got %+v", name, got)
		}
	}

	created := postJSON[map[string]any](t, client, url, map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple",
		"name": " Ryan Borg ", "email": "ryan@example.com",
	}, http.StatusCreated)
	if created["name"] != "Ryan Borg" || created["email"] != "ryan@example.com" || created["isAdmin"] != true {
		t.Fatalf("unexpected onboarding response: %+v", created)
	}

	me := getJSON[map[string]any](t, client, httpSrv.URL+"/api/auth/me")
	if me["name"] != "Ryan Borg" || me["email"] != "ryan@example.com" || me["username"] != "ryan" {
		t.Fatalf("unexpected /me: %+v", me)
	}
}

func TestUpdateProfileAndChangePassword(t *testing.T) {
	_, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Borg",
	}, http.StatusCreated)
	profile := httpSrv.URL + "/api/auth/profile"

	for name, body := range map[string]map[string]string{
		"short username": {"username": "ab", "firstName": "R", "lastName": "B"},
		"bad email":      {"username": "ryan2", "firstName": "R", "lastName": "B", "email": "a@b"},
	} {
		putJSONStatus(t, client, profile, body, http.StatusBadRequest)
		_ = name
	}

	updated := putJSONStatus(t, client, profile, map[string]any{
		"username": "ryanb", "name": "Ryan Borg-Smith", "email": "rb@example.org",
	}, http.StatusOK)
	if updated["username"] != "ryanb" || updated["name"] != "Ryan Borg-Smith" || updated["email"] != "rb@example.org" || updated["isAdmin"] != true {
		t.Fatalf("unexpected update response: %+v", updated)
	}
	me := getJSON[map[string]any](t, client, httpSrv.URL+"/api/auth/me")
	if me["username"] != "ryanb" {
		t.Fatalf("profile change should be visible on /me: %+v", me)
	}

	// The password survives a profile edit and can be changed afterwards.
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/auth/change-password", map[string]string{
		"currentPassword": "wrong", "newPassword": "another-long-password",
	}, http.StatusUnauthorized)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/auth/change-password", map[string]string{
		"currentPassword": "correct-horse-battery-staple", "newPassword": "another-long-password",
	}, http.StatusOK)
	login := postJSON[map[string]any](t, client, httpSrv.URL+"/api/auth/login", map[string]string{
		"username": "ryanb", "password": "another-long-password",
	}, http.StatusOK)
	if login["name"] != "Ryan Borg-Smith" || login["email"] != "rb@example.org" {
		t.Fatalf("login should return the profile: %+v", login)
	}
}

func TestWithoutANameTheGreetingFallsBackToTheUsername(t *testing.T) {
	_, httpSrv, client := newHuntTestServer(t)
	created := postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple",
	}, http.StatusCreated)
	if created["name"] != "ryan" {
		t.Fatalf("name should fall back to the username, got %+v", created)
	}
}
