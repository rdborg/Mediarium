package api

import (
	"testing"
	"time"
)

func TestWatchdogDecision(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }

	type step struct {
		sec     int
		ok      bool
		busy    bool
		restart bool
	}
	cases := []struct {
		name  string
		steps []step
	}{
		{"healthy for ever", []step{{0, true, false, false}, {30, true, false, false}, {600, true, false, false}}},
		{"one failure is nothing", []step{{0, false, false, false}, {30, true, false, false}}},
		{"fails for exactly three minutes", []step{{0, false, false, false}, {30, false, false, false}, {60, false, false, false}, {90, false, false, false}, {120, false, false, false}, {150, false, false, false}, {179, false, false, false}, {180, false, false, true}}},
		{"a good look in between starts the count again", []step{{0, false, false, false}, {90, false, false, false}, {120, true, false, false}, {150, false, false, false}, {270, false, false, false}, {330, false, false, true}}},
		{"busy (restore, update, move) never counts", []step{{0, false, true, false}, {60, false, true, false}, {600, false, true, false}}},
		{"busy resets the count", []step{{0, false, false, false}, {120, false, false, false}, {150, false, true, false}, {200, false, false, false}, {350, false, false, false}, {380, false, false, true}}},
		{"long failure keeps saying restart", []step{{0, false, false, false}, {200, false, false, true}, {230, false, false, true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var w watchdogState
			for _, s := range tc.steps {
				if got := w.observe(at(s.sec), s.ok, s.busy); got != s.restart {
					t.Fatalf("at %ds (ok %v, busy %v): restart = %v, want %v", s.sec, s.ok, s.busy, got, s.restart)
				}
			}
		})
	}
}

func TestDetectSupervisor(t *testing.T) {
	env := func(kv ...string) func(string) string {
		m := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return func(k string) string { return m[k] }
	}
	none := func(string) bool { return false }
	dockerenv := func(p string) bool { return p == "/.dockerenv" }

	cases := []struct {
		name         string
		byHand       bool
		imageVersion string
		getenv       func(string) string
		exists       func(string) bool
		want         string
	}{
		{"started by the image entrypoint", false, "1.1.0", env(), none, "docker"},
		{"a container without our entrypoint", false, "", env(), dockerenv, "docker"},
		{"podman", false, "", env("container", "podman"), none, "docker"},
		{"systemd service", false, "", env("INVOCATION_ID", "abc123"), none, "systemd"},
		{"launchd job", false, "", env("XPC_SERVICE_NAME", "io.github.rdborg.mediarium"), none, "launchd"},
		{"a terminal on a Mac (launchd sets 0)", false, "", env("XPC_SERVICE_NAME", "0"), none, "none"},
		{"told by hand", true, "", env(), none, "custom"},
		{"a plain terminal", false, "", env(), none, "none"},
		{"docker wins over a hand-set flag", true, "1.1.0", env(), none, "docker"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectSupervisor(tc.byHand, tc.imageVersion, tc.getenv, tc.exists); got != tc.want {
				t.Fatalf("detectSupervisor = %q, want %q", got, tc.want)
			}
		})
	}
}
