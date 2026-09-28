package api_test

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIndexerConnectionTest(t *testing.T) {
	_, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)

	good := newTVIndexerWith(t, []string{"A.Release.2001.1080p-GRP", "B.Release.2002.720p-GRP"})
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<?xml version="1.0"?><error code="100" description="Incorrect user credentials"/>`)
	}))
	t.Cleanup(bad.Close)

	// Unsaved config.
	ok := postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers/test", map[string]any{"name": "Good", "baseUrl": good.URL, "apiKey": "k"}, http.StatusOK)
	if ok["ok"] != true {
		t.Fatalf("expected a working indexer to pass, got %+v", ok)
	}
	failed := postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers/test", map[string]any{"name": "Bad", "baseUrl": bad.URL, "apiKey": "wrong"}, http.StatusOK)
	if failed["ok"] != false || failed["message"] == "" {
		t.Fatalf("expected a failing indexer to report a message, got %+v", failed)
	}
	empty := postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers/test", map[string]any{"name": "x"}, http.StatusOK)
	if empty["ok"] != false {
		t.Fatalf("a test without a URL must not pass: %+v", empty)
	}

	// Saved indexer, then enable/disable.
	created := postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "Saved", "definitionId": "fixture", "baseUrl": good.URL, "apiKey": "k",
	}, http.StatusCreated)
	id := int64(created["id"].(float64))
	saved := postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/indexers/%d/test", httpSrv.URL, id), nil, http.StatusOK)
	if saved["ok"] != true {
		t.Fatalf("saved indexer test failed: %+v", saved)
	}
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers/9999/test", nil, http.StatusNotFound)

	putJSONStatus(t, client, fmt.Sprintf("%s/api/indexers/%d/enabled", httpSrv.URL, id), map[string]any{"enabled": false}, http.StatusOK)
	list := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/indexers")
	if len(list) != 1 || list[0]["enabled"] != false {
		t.Fatalf("indexer should be disabled: %+v", list)
	}
}

func TestDownloadClientConnectionTest(t *testing.T) {
	_, httpSrv, client := newHuntTestServer(t)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)

	host, port := newArticleNNTPServer(t, nil)
	ok := postJSON[map[string]any](t, client, httpSrv.URL+"/api/usenet-servers/test", map[string]any{
		"host": host, "port": port, "useSsl": false, "username": "u", "password": "p",
	}, http.StatusOK)
	if ok["ok"] != true {
		t.Fatalf("expected the fake NNTP server to pass, got %+v", ok)
	}

	// A port nothing listens on.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	closedPort := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	failed := postJSON[map[string]any](t, client, httpSrv.URL+"/api/usenet-servers/test", map[string]any{
		"host": "127.0.0.1", "port": closedPort, "useSsl": false,
	}, http.StatusOK)
	if failed["ok"] != false || failed["message"] == "" {
		t.Fatalf("expected a connection failure message, got %+v", failed)
	}

	// Saved client.
	created := postJSON[map[string]any](t, client, httpSrv.URL+"/api/usenet-servers", map[string]any{
		"name": "Usenet", "host": host, "port": port, "useSsl": false, "username": "u", "password": "p", "connections": 1,
	}, http.StatusCreated)
	saved := postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/usenet-servers/%d/test", httpSrv.URL, int64(created["id"].(float64))), nil, http.StatusOK)
	if saved["ok"] != true {
		t.Fatalf("saved client test failed: %+v", saved)
	}
}
