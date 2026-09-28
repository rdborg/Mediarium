package api_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestUsenetServerCRUDAndValidation(t *testing.T) {
	_, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	base := httpSrv.URL + "/api/usenet-servers"

	// Bad input is explained, not stored.
	for name, body := range map[string]map[string]any{
		"no host":        {"port": 563},
		"url as host":    {"host": "https://news.example.com", "port": 563},
		"bad port":       {"host": "news.example.com", "port": 70000},
		"too many conns": {"host": "news.example.com", "port": 563, "connections": 500},
		"bad priority":   {"host": "news.example.com", "port": 563, "priority": -1},
	} {
		got := postJSON[map[string]any](t, client, base, body, http.StatusBadRequest)
		if got["error"] == nil {
			t.Fatalf("%s: expected an error message, got %+v", name, got)
		}
	}

	// Defaults: name falls back to the host, connections to a sane number.
	primary := postJSON[map[string]any](t, client, base, map[string]any{
		"host": "news.example.com", "port": 563, "useSsl": true, "username": "u", "password": "secret",
	}, http.StatusCreated)
	if primary["name"] != "news.example.com" || primary["connections"] != float64(8) || primary["enabled"] != true {
		t.Fatalf("unexpected defaults: %+v", primary)
	}
	if primary["hasPassword"] != true || primary["password"] != nil {
		t.Fatalf("the password must be reported as set but never returned: %+v", primary)
	}
	backup := postJSON[map[string]any](t, client, base, map[string]any{
		"name": "Backup", "host": "backup.example.com", "port": 563, "priority": 1, "connections": 4,
	}, http.StatusCreated)

	// Editing with a blank password keeps the stored one.
	id := int64(primary["id"].(float64))
	updated := putJSONStatus(t, client, fmt.Sprintf("%s/%d", base, id), map[string]any{
		"name": "Primary", "host": "news.example.com", "port": 119, "useSsl": false, "username": "u", "connections": 12, "priority": 0, "enabled": true,
	}, http.StatusOK)
	if updated["name"] != "Primary" || updated["port"] != float64(119) || updated["hasPassword"] != true {
		t.Fatalf("edit should keep the password: %+v", updated)
	}
	putJSONStatus(t, client, fmt.Sprintf("%s/99999", base), map[string]any{"host": "x.example.com", "port": 563, "enabled": true}, http.StatusNotFound)

	// Listed in priority order, disabled ones included.
	putJSONStatus(t, client, fmt.Sprintf("%s/%d", base, int64(backup["id"].(float64))), map[string]any{
		"name": "Backup", "host": "backup.example.com", "port": 563, "priority": 1, "connections": 4, "enabled": false,
	}, http.StatusOK)
	list := getJSON[[]map[string]any](t, client, base)
	if len(list) != 2 || list[0]["name"] != "Primary" || list[1]["enabled"] != false {
		t.Fatalf("unexpected list: %+v", list)
	}

	// The status page reflects enabled servers only.
	status := getJSON[map[string]any](t, client, httpSrv.URL+"/api/downloads/status")
	usenet := status["usenet"].(map[string]any)
	if usenet["servers"] != float64(2) || usenet["enabledServers"] != float64(1) || usenet["ready"] != true {
		t.Fatalf("unexpected usenet status: %+v", usenet)
	}
	if status["torrent"].(map[string]any)["ready"] != true {
		t.Fatalf("the built-in torrent client needs no setup and should be ready: %+v", status["torrent"])
	}

	if code := deleteReq(t, client, fmt.Sprintf("%s/%d", base, id)); code != http.StatusOK {
		t.Fatalf("delete status %d", code)
	}
	if code := deleteReq(t, client, fmt.Sprintf("%s/%d", base, id)); code != http.StatusNotFound {
		t.Fatalf("deleting twice should be 404, got %d", code)
	}
}

// Testing an edited server with a blank password uses the stored one.
func TestUsenetServerTestReusesStoredPassword(t *testing.T) {
	_, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	host, port := newArticleNNTPServer(t, nil)
	created := postJSON[map[string]any](t, client, httpSrv.URL+"/api/usenet-servers", map[string]any{
		"host": host, "port": port, "username": "u", "password": "p",
	}, http.StatusCreated)

	res := postJSON[map[string]any](t, client, httpSrv.URL+"/api/usenet-servers/test", map[string]any{
		"id": created["id"], "host": host, "port": port, "username": "u",
	}, http.StatusOK)
	if res["ok"] != true {
		t.Fatalf("expected the stored login to be reused: %+v", res)
	}
}
