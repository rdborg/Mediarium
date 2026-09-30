package api_test

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newAuthNNTPServer is a fake news server whose login can be switched between
// accepted and rejected while the test runs.
func newAuthNNTPServer(t *testing.T) (host string, port int, accept *atomic.Bool) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	accept = &atomic.Bool{}
	accept.Store(true)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				fmt.Fprint(conn, "200 fake nntp ready\r\n")
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					switch upper := strings.ToUpper(strings.TrimSpace(line)); {
					case strings.HasPrefix(upper, "AUTHINFO USER"):
						fmt.Fprint(conn, "381 password required\r\n")
					case strings.HasPrefix(upper, "AUTHINFO PASS"):
						if accept.Load() {
							fmt.Fprint(conn, "281 ok\r\n")
						} else {
							fmt.Fprint(conn, "481 Authentication failed\r\n")
						}
					case strings.HasPrefix(upper, "QUIT"):
						fmt.Fprint(conn, "205 bye\r\n")
						return
					default:
						fmt.Fprint(conn, "500 what\r\n")
					}
				}
			}()
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port, accept
}

func waitForDeliveries(t *testing.T, rec *hookRecorder, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if rec.count() >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected %d webhook deliveries, got %d: %v", want, rec.count(), rec.bodies)
}

func TestMonitorReportsStatusAndNotifiesOnlyOnChanges(t *testing.T) {
	_, base, client := loginNewServer(t)
	host, port, accept := newAuthNNTPServer(t)

	created := postJSON[map[string]any](t, client, base+"/api/usenet-servers", map[string]any{
		"name": "Fake Provider", "host": host, "port": port, "username": "u", "password": "p", "connections": 1,
	}, http.StatusCreated)
	serverID := created["id"]

	goodIdx := newTVIndexerWith(t, []string{"A.Release.2001.1080p-GRP"})
	badIdx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<?xml version="1.0"?><error code="100" description="Incorrect user credentials"/>`)
	}))
	t.Cleanup(badIdx.Close)
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{"name": "Good Idx", "definitionId": "fixture", "baseUrl": goodIdx.URL, "apiKey": "k"}, http.StatusCreated)
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{"name": "Bad Idx", "definitionId": "fixture", "baseUrl": badIdx.URL, "apiKey": "k"}, http.StatusCreated)

	healthHook, healthRec := newHookRecorder(t, 200)
	failedOnlyHook, failedOnlyRec := newHookRecorder(t, 200)
	postJSON[map[string]any](t, client, base+"/api/notifications", map[string]any{
		"name": "Health hook", "type": "webhook", "config": map[string]string{"url": healthHook.URL}, "events": []string{"health"},
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, base+"/api/notifications", map[string]any{
		"name": "Failed only", "type": "webhook", "config": map[string]string{"url": failedOnlyHook.URL}, "events": []string{"failed"},
	}, http.StatusCreated)

	// Nothing has been checked yet.
	if st := getJSON[[]map[string]any](t, client, base+"/api/monitor/status"); len(st) != 0 {
		t.Fatalf("expected no results before the first check, got %+v", st)
	}

	statuses := postJSON[[]map[string]any](t, client, base+"/api/monitor/run", nil, http.StatusOK)
	byName := map[string]map[string]any{}
	for _, s := range statuses {
		byName[s["name"].(string)] = s
	}
	if len(byName) != 3 {
		t.Fatalf("expected the server and both indexers, got %+v", statuses)
	}
	srv, good, bad := byName["Fake Provider"], byName["Good Idx"], byName["Bad Idx"]
	if srv["kind"] != "usenet" || srv["ok"] != true || srv["checkedAt"] == "" || srv["id"] != serverID {
		t.Fatalf("unexpected usenet status: %+v", srv)
	}
	if good["kind"] != "indexer" || good["ok"] != true {
		t.Fatalf("unexpected good indexer status: %+v", good)
	}
	if bad["ok"] != false || !strings.Contains(bad["hint"].(string), "The login was rejected") || bad["error"] == "" {
		t.Fatalf("a rejected indexer login should carry an actionable hint: %+v", bad)
	}

	// The bad indexer was failing on the very first look: reported once.
	waitForDeliveries(t, healthRec, 1)
	if !strings.Contains(healthRec.bodies[0], `"event":"health"`) || !strings.Contains(healthRec.bodies[0], "Bad Idx") {
		t.Fatalf("unexpected first notification: %s", healthRec.bodies[0])
	}

	// The provider login starts failing: one more notification, then silence.
	accept.Store(false)
	statuses = postJSON[[]map[string]any](t, client, base+"/api/monitor/run", nil, http.StatusOK)
	for _, s := range statuses {
		if s["name"] == "Fake Provider" && (s["ok"] != false || !strings.Contains(s["hint"].(string), "The login was rejected. Your subscription may have expired or the password may have changed.")) {
			t.Fatalf("expected the rejected login to be explained: %+v", s)
		}
	}
	waitForDeliveries(t, healthRec, 2)
	postJSON[[]map[string]any](t, client, base+"/api/monitor/run", nil, http.StatusOK)
	postJSON[[]map[string]any](t, client, base+"/api/monitor/run", nil, http.StatusOK)

	// Recovery: one notification.
	accept.Store(true)
	postJSON[[]map[string]any](t, client, base+"/api/monitor/run", nil, http.StatusOK)
	waitForDeliveries(t, healthRec, 3)
	time.Sleep(200 * time.Millisecond) // let any wrongly repeated sends land
	if healthRec.count() != 3 {
		t.Fatalf("expected exactly 3 health notifications (bad indexer, provider down, provider up), got %d: %v", healthRec.count(), healthRec.bodies)
	}
	if !strings.Contains(healthRec.bodies[2], "working again") {
		t.Fatalf("the third notification should be the recovery: %s", healthRec.bodies[2])
	}
	if failedOnlyRec.count() != 0 {
		t.Fatalf("a target not subscribed to health must hear nothing, got %v", failedOnlyRec.bodies)
	}

	// A disabled indexer is no longer checked.
	list := getJSON[[]map[string]any](t, client, base+"/api/indexers")
	for _, ix := range list {
		if ix["name"] == "Bad Idx" {
			putJSONStatus(t, client, fmt.Sprintf("%s/api/indexers/%d/enabled", base, int64(ix["id"].(float64))), map[string]any{"enabled": false}, http.StatusOK)
		}
	}
	statuses = postJSON[[]map[string]any](t, client, base+"/api/monitor/run", nil, http.StatusOK)
	if len(statuses) != 2 {
		t.Fatalf("a disabled indexer should drop out of the results: %+v", statuses)
	}
}

func TestMonitorIntervalSetting(t *testing.T) {
	_, base, client := loginNewServer(t)
	get := func() any { return getJSON[map[string]any](t, client, base+"/api/settings")["monitorIntervalMinutes"] }
	put := func(v any, want int) map[string]any {
		return postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"monitorIntervalMinutes": v}, want)
	}

	if get() != float64(30) {
		t.Fatalf("default should be 30 minutes, got %v", get())
	}
	if put(0, http.StatusOK)["monitorIntervalMinutes"] != float64(0) {
		t.Fatal("0 should turn the monitor off and read back as 0")
	}
	if put(2, http.StatusOK)["monitorIntervalMinutes"] != float64(5) {
		t.Fatal("anything under 5 should be raised to 5")
	}
	if put(60, http.StatusOK)["monitorIntervalMinutes"] != float64(60) {
		t.Fatal("60 should be kept")
	}
	put(-1, http.StatusBadRequest)
	if get() != float64(60) {
		t.Fatalf("a rejected value must not change the setting, got %v", get())
	}
}
