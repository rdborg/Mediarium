package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestNotificationTypesCatalog(t *testing.T) {
	_, base, client := loginNewServer(t)
	types := getJSON[[]map[string]any](t, client, base+"/api/notifications/types")
	seen := map[string]bool{}
	for _, typ := range types {
		seen[typ["type"].(string)] = true
		if typ["label"] == "" {
			t.Fatalf("type needs a label: %+v", typ)
		}
		for _, raw := range typ["fields"].([]any) {
			f := raw.(map[string]any)
			for _, key := range []string{"name", "label", "kind", "help"} {
				if f[key] == nil || f[key] == "" {
					t.Fatalf("%v field is missing %s: %+v", typ["type"], key, f)
				}
			}
			if _, ok := f["required"].(bool); !ok {
				t.Fatalf("required must be a boolean: %+v", f)
			}
		}
	}
	for _, want := range []string{"email", "ntfy", "gotify", "pushover", "slack", "webhook", "discord", "telegram"} {
		if !seen[want] {
			t.Fatalf("missing type %q in %v", want, seen)
		}
	}
	events := getJSON[[]map[string]any](t, client, base+"/api/notifications/events")
	ids := []string{}
	for _, e := range events {
		ids = append(ids, e["id"].(string))
	}
	if strings.Join(ids, ",") != "added,imported,failed,conflict,subtitle,health,update,request" {
		t.Fatalf("unexpected events %v", ids)
	}
}

// hookRecorder is a webhook endpoint that remembers what it was sent.
type hookRecorder struct {
	mu     sync.Mutex
	bodies []string
	status int
}

func newHookRecorder(t *testing.T, status int) (*httptest.Server, *hookRecorder) {
	t.Helper()
	rec := &hookRecorder{status: status}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.bodies = append(rec.bodies, string(b))
		rec.mu.Unlock()
		w.WriteHeader(rec.status)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func (h *hookRecorder) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.bodies)
}

func TestNotificationTargetLifecycleHidesSecrets(t *testing.T) {
	_, base, client := loginNewServer(t)

	// Validation problems are explained, not stored.
	for name, body := range map[string]map[string]any{
		"unknown type":  {"name": "x", "type": "pigeon"},
		"email no host": {"type": "email", "config": map[string]string{"port": "587", "security": "starttls", "from": "a@b.co", "to": "c@d.co"}},
		"bad event":     {"type": "ntfy", "config": map[string]string{"topic": "t"}, "events": []string{"lunch"}},
	} {
		got := postJSON[map[string]any](t, client, base+"/api/notifications", body, http.StatusBadRequest)
		if got["error"] == nil {
			t.Fatalf("%s: expected an error message, got %+v", name, got)
		}
	}

	created := postJSON[map[string]any](t, client, base+"/api/notifications", map[string]any{
		"name": "Mail me", "type": "email", "events": []string{"failed", "health"},
		"config": map[string]string{
			"host": "smtp.example.com", "port": "587", "security": "starttls",
			"username": "me@example.com", "password": "hunter2-secret", "from": "me@example.com", "to": "me@example.com",
		},
	}, http.StatusCreated)
	if created["hasPassword"] != true || created["enabled"] != true {
		t.Fatalf("unexpected create response: %+v", created)
	}
	id := int64(created["id"].(float64))

	raw, _ := json.Marshal(getJSON[[]map[string]any](t, client, base+"/api/notifications"))
	if strings.Contains(string(raw), "hunter2-secret") {
		t.Fatalf("a secret leaked into the list response: %s", raw)
	}
	list := getJSON[[]map[string]any](t, client, base+"/api/notifications")
	target := list[0]
	cfg := target["config"].(map[string]any)
	secrets := target["hasSecrets"].(map[string]any)
	events := target["events"].([]any)
	if cfg["host"] != "smtp.example.com" || cfg["password"] != nil || secrets["password"] != true || len(events) != 2 {
		t.Fatalf("unexpected listing: %+v", target)
	}

	// Edit: blank password keeps the stored one; events and enabled change.
	updated := putJSONStatus(t, client, base+"/api/notifications/"+itoa(id), map[string]any{
		"name": "Mail me", "enabled": false, "events": []string{"imported"},
		"config": map[string]string{"host": "smtp.example.com", "port": "465", "security": "ssl", "username": "me@example.com", "from": "me@example.com", "to": "me@example.com"},
	}, http.StatusOK)
	if updated["hasPassword"] != true || updated["enabled"] != false || updated["config"].(map[string]any)["port"] != "465" {
		t.Fatalf("unexpected update response: %+v", updated)
	}
	putJSONStatus(t, client, base+"/api/notifications/999", map[string]any{"name": "x"}, http.StatusNotFound)

	if code := deleteReq(t, client, base+"/api/notifications/"+itoa(id)); code != http.StatusOK {
		t.Fatalf("delete: %d", code)
	}
	if list := getJSON[[]map[string]any](t, client, base+"/api/notifications"); len(list) != 0 {
		t.Fatalf("expected no targets, got %+v", list)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestNotificationTestEndpointUnsavedAndSaved(t *testing.T) {
	_, base, client := loginNewServer(t)
	good, goodRec := newHookRecorder(t, 200)
	bad, _ := newHookRecorder(t, 500)

	// Unsaved: nothing is stored, the message is really sent.
	res := postJSON[map[string]any](t, client, base+"/api/notifications/test", map[string]any{
		"type": "webhook", "config": map[string]string{"url": good.URL},
	}, http.StatusOK)
	if res["sent"] != true || res["error"] != nil {
		t.Fatalf("expected sent, got %+v", res)
	}
	if goodRec.count() != 1 || !strings.Contains(goodRec.bodies[0], `"event":"test"`) {
		t.Fatalf("the test message did not arrive: %v", goodRec.bodies)
	}
	if list := getJSON[[]map[string]any](t, client, base+"/api/notifications"); len(list) != 0 {
		t.Fatalf("testing must not save anything: %+v", list)
	}

	// A delivery failure is reported, not thrown.
	res = postJSON[map[string]any](t, client, base+"/api/notifications/test", map[string]any{
		"type": "webhook", "url": bad.URL,
	}, http.StatusOK)
	if res["sent"] != false || !strings.Contains(res["error"].(string), "500") {
		t.Fatalf("expected a failure with a reason, got %+v", res)
	}

	// Malformed input is a 400 with the reason.
	res = postJSON[map[string]any](t, client, base+"/api/notifications/test", map[string]any{"type": "ntfy", "config": map[string]string{}}, http.StatusBadRequest)
	if res["error"] == nil {
		t.Fatalf("expected a validation error, got %+v", res)
	}
	postJSON[map[string]any](t, client, base+"/api/notifications/test", map[string]any{"type": "pigeon"}, http.StatusBadRequest)

	// Saved: test by id, using the stored config.
	created := postJSON[map[string]any](t, client, base+"/api/notifications", map[string]any{
		"name": "Hook", "type": "webhook", "config": map[string]string{"url": good.URL},
	}, http.StatusCreated)
	id := created["id"]
	res = postJSON[map[string]any](t, client, base+"/api/notifications/test", map[string]any{"id": id}, http.StatusOK)
	if res["sent"] != true || goodRec.count() != 2 {
		t.Fatalf("saved target test: %+v (deliveries: %d)", res, goodRec.count())
	}
	postJSON[map[string]any](t, client, base+"/api/notifications/test", map[string]any{"id": 12345}, http.StatusNotFound)
}
