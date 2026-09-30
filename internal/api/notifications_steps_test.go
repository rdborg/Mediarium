package api_test

import (
	"net/http"
	"strings"
	"testing"
)

type stepJSON struct {
	Time string
	Text string
	OK   bool
}

func stepsOf(t *testing.T, res map[string]any) []stepJSON {
	t.Helper()
	raw, ok := res["steps"].([]any)
	if !ok {
		t.Fatalf("no steps in %+v", res)
	}
	var out []stepJSON
	for _, r := range raw {
		m := r.(map[string]any)
		out = append(out, stepJSON{Time: m["time"].(string), Text: m["text"].(string), OK: m["ok"].(bool)})
	}
	return out
}

// Every method that can be tested without the internet leaves a log: what it
// did, in order, ending in success or in the reason it failed. The log never
// holds the secret part of the address or a token.
func TestNotificationTestReturnsALog(t *testing.T) {
	_, base, client := loginNewServer(t)
	good, _ := newHookRecorder(t, 200)
	bad, _ := newHookRecorder(t, 401)

	cases := []struct {
		name   string
		config func(url string) map[string]any
	}{
		{"webhook", func(u string) map[string]any {
			return map[string]any{"type": "webhook", "config": map[string]string{"url": u + "/hook/SECRETPATH123"}}
		}},
		{"discord", func(u string) map[string]any {
			return map[string]any{"type": "discord", "config": map[string]string{"url": u + "/api/webhooks/SECRETPATH123"}}
		}},
		{"slack", func(u string) map[string]any {
			return map[string]any{"type": "slack", "config": map[string]string{"url": u + "/services/SECRETPATH123"}}
		}},
		{"ntfy", func(u string) map[string]any {
			return map[string]any{"type": "ntfy", "config": map[string]string{"server": u, "topic": "SECRETTOPIC123"}}
		}},
		{"gotify", func(u string) map[string]any {
			return map[string]any{"type": "gotify", "config": map[string]string{"url": u, "token": "SECRETTOKEN123"}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name+" works", func(t *testing.T) {
			res := postJSON[map[string]any](t, client, base+"/api/notifications/test", tc.config(good.URL), http.StatusOK)
			if res["sent"] != true {
				t.Fatalf("not sent: %+v", res)
			}
			steps := stepsOf(t, res)
			if len(steps) < 3 {
				t.Fatalf("a short log: %+v", steps)
			}
			for _, s := range steps {
				if s.Time == "" || strings.Contains(s.Text, "SECRET") {
					t.Errorf("bad step %+v", s)
				}
			}
			if last := steps[len(steps)-1]; !last.OK || !strings.HasPrefix(last.Text, "Done: accepted by the server") {
				t.Errorf("the log should end with the result, got %+v", last)
			}
		})
		t.Run(tc.name+" refused", func(t *testing.T) {
			res := postJSON[map[string]any](t, client, base+"/api/notifications/test", tc.config(bad.URL), http.StatusOK)
			if res["sent"] != false || res["error"] == nil {
				t.Fatalf("expected a failure: %+v", res)
			}
			steps := stepsOf(t, res)
			last := steps[len(steps)-1]
			if last.OK || !strings.Contains(last.Text, "Refused: wrong token, key or password (401)") {
				t.Errorf("the log should end with the reason, got %+v", last)
			}
			for _, s := range steps {
				if strings.Contains(s.Text, "SECRET") {
					t.Errorf("a secret is in %+v", s)
				}
			}
		})
	}
}

func TestNotificationTestLogWhenNothingIsListening(t *testing.T) {
	_, base, client := loginNewServer(t)
	res := postJSON[map[string]any](t, client, base+"/api/notifications/test", map[string]any{
		"type": "email", "config": map[string]string{"host": "127.0.0.1", "port": "1", "security": "none", "from": "a@example.com", "to": "b@example.com"},
	}, http.StatusOK)
	steps := stepsOf(t, res)
	if res["sent"] != false || len(steps) < 2 {
		t.Fatalf("expected a failure with a log: %+v", res)
	}
	if steps[0].Text != "Connecting to 127.0.0.1:1" || steps[len(steps)-1].OK {
		t.Errorf("steps %+v", steps)
	}
}

func TestPublicURLSetting(t *testing.T) {
	_, base, client := loginNewServer(t)
	put := func(v string, want int) map[string]any {
		return postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"publicUrl": v}, want)
	}
	get := func() any { return getJSON[map[string]any](t, client, base+"/api/settings")["publicUrl"] }

	if get() != "" {
		t.Fatalf("no address is set by default, got %v", get())
	}
	if put("https://media.example.com/", http.StatusOK)["publicUrl"] != "https://media.example.com" {
		t.Fatal("a trailing slash should be dropped")
	}
	if get() != "https://media.example.com" {
		t.Fatalf("got %v", get())
	}
	for _, bad := range []string{"media.example.com", "ftp://media.example.com", "https://", "https://exa mple.com"} {
		put(bad, http.StatusBadRequest)
	}
	if get() != "https://media.example.com" {
		t.Fatal("a rejected address must not change the setting")
	}
	if put("", http.StatusOK)["publicUrl"] != "" || get() != "" {
		t.Fatal("an empty address removes it")
	}
}
