package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// A saved password, key or token may be kept by leaving its field blank, but
// not when the address changes in the same request: otherwise pointing the
// address at another machine and pressing Test would send the secret there.
func TestSavedSecretsAreNotSentToAChangedAddress(t *testing.T) {
	_, base, admin := loginNewServer(t)

	mentions := func(body map[string]any, part string) bool {
		msg, _ := body["error"].(string)
		if msg == "" {
			msg, _ = body["message"].(string)
		}
		if msg == "" {
			msg, _ = body["error"].(string)
		}
		return strings.Contains(msg, part)
	}

	t.Run("a Usenet server", func(t *testing.T) {
		created := postJSON[map[string]any](t, admin, base+"/api/usenet-servers", map[string]any{
			"name": "Provider", "host": "news.example.com", "port": 563, "useSsl": true, "username": "me", "password": "usenet-secret", "connections": 5, "enabled": true,
		}, http.StatusCreated)
		id := int(created["id"].(float64))
		url := fmt.Sprintf("%s/api/usenet-servers/%d", base, id)

		// Testing a different host with the saved password is refused.
		status, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/usenet-servers/test", map[string]any{
			"id": id, "host": "evil.example.org", "port": 119, "username": "me",
		})
		if status != http.StatusOK || body["ok"] == true || !mentions(body, "type the password again") {
			t.Errorf("test with another host: %d %v; want a refusal that asks for the password", status, body)
		}
		// Saving it that way is refused too, and nothing changed.
		status, body = doJSONStatus(t, admin, http.MethodPut, url, map[string]any{"host": "evil.example.org"})
		if status != http.StatusBadRequest || !mentions(body, "type the password again") {
			t.Errorf("save with another host: %d %v; want 400 asking for the password", status, body)
		}
		if list := getJSON[[]map[string]any](t, admin, base+"/api/usenet-servers"); list[0]["host"] != "news.example.com" {
			t.Errorf("the host changed to %v", list[0]["host"])
		}
		// Other edits (a rename, another port on the same host), and a new host
		// with a new password, are fine.
		if status, _ = doJSONStatus(t, admin, http.MethodPut, url, map[string]any{"port": 119, "useSsl": false}); status != http.StatusOK {
			t.Errorf("another port on the same host: %d", status)
		}
		if status, _ = doJSONStatus(t, admin, http.MethodPut, url, map[string]any{"name": "Renamed", "connections": 8}); status != http.StatusOK {
			t.Errorf("rename: %d", status)
		}
		if status, _ = doJSONStatus(t, admin, http.MethodPut, url, map[string]any{"host": "news2.example.com", "password": "new-secret"}); status != http.StatusOK {
			t.Errorf("new host with a new password: %d", status)
		}
	})

	t.Run("an indexer", func(t *testing.T) {
		indexer := newTVIndexerWith(t, nil)
		created := postJSON[map[string]any](t, admin, base+"/api/indexers", map[string]any{
			"name": "Indexer", "definitionId": "fixture", "baseUrl": indexer.URL, "apiKey": "indexer-secret",
		}, http.StatusCreated)
		url := fmt.Sprintf("%s/api/indexers/%d", base, int(created["id"].(float64)))

		status, body := doJSONStatus(t, admin, http.MethodPut, url, map[string]any{"baseUrl": "http://evil.example.org:9999"})
		if status != http.StatusBadRequest || !mentions(body, "type the API key again") {
			t.Errorf("save with another address: %d %v; want 400 asking for the key", status, body)
		}
		// The same address (written differently), a rename, or a new address with a new key are fine.
		if status, _ = doJSONStatus(t, admin, http.MethodPut, url, map[string]any{"baseUrl": strings.ToUpper(indexer.URL[:4]) + indexer.URL[4:] + "/"}); status != http.StatusOK {
			t.Errorf("same address written differently: %d", status)
		}
		if status, _ = doJSONStatus(t, admin, http.MethodPut, url, map[string]any{"name": "Renamed"}); status != http.StatusOK {
			t.Errorf("rename: %d", status)
		}
		if status, _ = doJSONStatus(t, admin, http.MethodPut, url, map[string]any{"baseUrl": "http://other.example.org:9999", "apiKey": "another-key"}); status != http.StatusOK {
			t.Errorf("new address with a new key: %d", status)
		}
	})

	t.Run("a media server", func(t *testing.T) {
		created := postJSON[map[string]any](t, admin, base+"/api/media-servers", map[string]any{
			"name": "Jellyfin", "kind": "jellyfin", "baseUrl": "http://192.168.1.20:8096", "token": "media-secret",
		}, http.StatusCreated)
		id := int(created["id"].(float64))
		url := fmt.Sprintf("%s/api/media-servers/%d", base, id)

		status, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/media-servers/test", map[string]any{
			"id": id, "kind": "jellyfin", "baseUrl": "http://evil.example.org:8096",
		})
		if status != http.StatusBadRequest || !mentions(body, "type the token again") {
			t.Errorf("test with another address: %d %v; want 400 asking for the token", status, body)
		}
		status, body = doJSONStatus(t, admin, http.MethodPut, url, map[string]any{"kind": "jellyfin", "baseUrl": "http://evil.example.org:8096"})
		if status != http.StatusBadRequest || !mentions(body, "type the token again") {
			t.Errorf("save with another address: %d %v; want 400 asking for the token", status, body)
		}
		if status, _ = doJSONStatus(t, admin, http.MethodPut, url, map[string]any{"name": "Renamed"}); status != http.StatusOK {
			t.Errorf("rename: %d", status)
		}
		if status, _ = doJSONStatus(t, admin, http.MethodPut, url, map[string]any{"kind": "jellyfin", "baseUrl": "http://192.168.1.21:8096", "token": "new-token"}); status != http.StatusOK {
			t.Errorf("new address with a new token: %d", status)
		}
	})

	t.Run("a notification", func(t *testing.T) {
		created := postJSON[map[string]any](t, admin, base+"/api/notifications", map[string]any{
			"name": "Gotify", "type": "gotify", "config": map[string]string{"url": "https://gotify.example.com", "token": "gotify-secret"},
		}, http.StatusCreated)
		id := int(created["id"].(float64))
		url := fmt.Sprintf("%s/api/notifications/%d", base, id)

		status, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/notifications/test", map[string]any{
			"id": id, "type": "gotify", "config": map[string]string{"url": "https://evil.example.org"},
		})
		if status != http.StatusBadRequest || !mentions(body, "type the app token again") {
			t.Errorf("test with another address: %d %v; want 400 asking for the token", status, body)
		}
		status, body = doJSONStatus(t, admin, http.MethodPut, url, map[string]any{"type": "gotify", "config": map[string]string{"url": "https://evil.example.org"}})
		if status != http.StatusBadRequest || !mentions(body, "type the app token again") {
			t.Errorf("save with another address: %d %v; want 400 asking for the token", status, body)
		}
		if status, _ = doJSONStatus(t, admin, http.MethodPut, url, map[string]any{"type": "gotify", "name": "Renamed", "config": map[string]string{"url": "https://gotify.example.com"}}); status != http.StatusOK {
			t.Errorf("same address: %d", status)
		}
		if status, _ = doJSONStatus(t, admin, http.MethodPut, url, map[string]any{"type": "gotify", "config": map[string]string{"url": "https://gotify2.example.com", "token": "new-secret"}}); status != http.StatusOK {
			t.Errorf("new address with a new token: %d", status)
		}
	})
}
