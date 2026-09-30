package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestEditIndexer(t *testing.T) {
	server, base, client := loginNewServer(t)
	idx := newTVIndexerWith(t, []string{"A.Release.2001.1080p-GRP"})
	other := newTVIndexerWith(t, nil)
	created := postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{
		"name": "Geek", "baseUrl": idx.URL, "apiKey": "first-key", "protocol": "usenet",
	}, http.StatusCreated)
	id := int64(created["id"].(float64))
	url := fmt.Sprintf("%s/api/indexers/%d", base, id)
	if created["hasApiKey"] != true || created["apiKey"] != nil {
		t.Fatalf("the key is reported as saved, never returned: %+v", created)
	}

	stored := func() (name, baseURL, key, protocol, kind string, enabled bool) {
		t.Helper()
		inst, err := server.IndexerRepo.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		return inst.Name, inst.BaseURL, inst.APIKey, string(inst.Protocol), string(inst.Kind), inst.Enabled
	}

	cases := []struct {
		name string
		body map[string]any
		want string // name|baseURL|key|protocol|kind|enabled after the edit
	}{
		{"empty edit keeps everything", map[string]any{},
			"Geek|" + idx.URL + "|first-key|usenet|newznab|true"},
		{"rename, blank key keeps the saved one", map[string]any{"name": " NZBGeek ", "apiKey": ""},
			"NZBGeek|" + idx.URL + "|first-key|usenet|newznab|true"},
		{"new address and key", map[string]any{"baseUrl": other.URL, "apiKey": " second-key "},
			"NZBGeek|" + other.URL + "|second-key|usenet|newznab|true"},
		{"switch to torrents", map[string]any{"protocol": "torrent"},
			"NZBGeek|" + other.URL + "|second-key|torrent|torznab|true"},
		{"disable", map[string]any{"enabled": false},
			"NZBGeek|" + other.URL + "|second-key|torrent|torznab|false"},
		{"back to Usenet and on again", map[string]any{"protocol": "usenet", "enabled": true, "baseUrl": idx.URL},
			"NZBGeek|" + idx.URL + "|second-key|usenet|newznab|true"},
	}
	for _, tc := range cases {
		out := putJSONStatus(t, client, url, tc.body, http.StatusOK)
		if out["apiKey"] != nil || out["hasApiKey"] != true {
			t.Fatalf("%s: the key must never be returned: %+v", tc.name, out)
		}
		n, b, k, p, kind, e := stored()
		if got := strings.Join([]string{n, b, k, p, kind, fmt.Sprint(e)}, "|"); got != tc.want {
			t.Fatalf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}

	putJSONStatus(t, client, url, map[string]any{"protocol": "ftp"}, http.StatusBadRequest)
	putJSONStatus(t, client, base+"/api/indexers/99999", map[string]any{"name": "x"}, http.StatusNotFound)

	// The listing never carries the key either.
	for _, ix := range getJSON[[]map[string]any](t, client, base+"/api/indexers") {
		if ix["apiKey"] != nil || strings.Contains(fmt.Sprint(ix), "second-key") {
			t.Fatalf("listing leaks the key: %+v", ix)
		}
	}
}

func TestEditUsenetServerPartially(t *testing.T) {
	server, base, client := loginNewServer(t)
	created := postJSON[map[string]any](t, client, base+"/api/usenet-servers", map[string]any{
		"name": "Primary", "host": "news.example.com", "port": 563, "useSsl": true, "username": "u", "password": "secret",
		"connections": 20, "priority": 0, "enabled": true,
	}, http.StatusCreated)
	id := int64(created["id"].(float64))
	url := fmt.Sprintf("%s/api/usenet-servers/%d", base, id)

	// Sending only the name changes only the name: the server stays enabled,
	// keeps its settings and its password.
	out := putJSONStatus(t, client, url, map[string]any{"name": "Eweka"}, http.StatusOK)
	if out["name"] != "Eweka" || out["host"] != "news.example.com" || out["port"] != float64(563) || out["useSsl"] != true ||
		out["connections"] != float64(20) || out["enabled"] != true || out["hasPassword"] != true || out["password"] != nil {
		t.Fatalf("partial edit: %+v", out)
	}
	sc, err := server.ClientRepo.Get(id)
	if err != nil || sc.Config.Password != "secret" || sc.Config.Username != "u" {
		t.Fatalf("stored login should be kept: %+v %v", sc.Config.Username, err)
	}

	// A new password replaces the stored one; disabling works on its own.
	putJSONStatus(t, client, url, map[string]any{"password": "new-secret", "enabled": false}, http.StatusOK)
	sc, _ = server.ClientRepo.Get(id)
	if sc.Config.Password != "new-secret" || sc.Enabled || sc.Config.Host != "news.example.com" {
		t.Fatalf("after edit: enabled %v host %q", sc.Enabled, sc.Config.Host)
	}

	putJSONStatus(t, client, url, map[string]any{"port": 0}, http.StatusBadRequest)
	putJSONStatus(t, client, base+"/api/usenet-servers/99999", map[string]any{"name": "x"}, http.StatusNotFound)
}
