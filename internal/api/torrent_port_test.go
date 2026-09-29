package api_test

import (
	"net/http"
	"testing"

	"github.com/ryanborg/mediarium/internal/settings"
)

func TestTorrentListenPortDefaultsTo58264(t *testing.T) {
	server, base, client := loginNewServer(t)

	if got := getJSON[map[string]any](t, client, base+"/api/settings")["torrentListenPort"]; got != "58264" {
		t.Fatalf("default torrentListenPort = %v, want 58264", got)
	}
	status := getJSON[map[string]any](t, client, base+"/api/downloads/status")["torrent"].(map[string]any)
	if status["listenPort"] != float64(58264) || status["incomingSeen"] != false || status["listening"] != false || status["activeTorrents"] != float64(0) {
		t.Fatalf("unexpected torrent status with nothing running: %+v", status)
	}

	tests := []struct {
		name   string
		send   string
		status int
		want   string
	}{
		{"a port of your own", "51413", http.StatusOK, "51413"},
		{"0 means the default", "0", http.StatusOK, "58264"},
		{"spaces are trimmed", " 6881 ", http.StatusOK, "6881"},
		{"too high", "70000", http.StatusBadRequest, "6881"},
		{"not a number", "abc", http.StatusBadRequest, "6881"},
		{"negative", "-1", http.StatusBadRequest, "6881"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			putJSONStatus(t, client, base+"/api/settings", map[string]any{"torrentListenPort": tc.send}, tc.status)
			if got := getJSON[map[string]any](t, client, base+"/api/settings")["torrentListenPort"]; got != tc.want {
				t.Fatalf("torrentListenPort = %v, want %s", got, tc.want)
			}
		})
	}

	// A port saved by an earlier version is kept as it is.
	if err := server.Settings.Set(settings.KeyTorrentListenPort, "6882", false); err != nil {
		t.Fatal(err)
	}
	status = getJSON[map[string]any](t, client, base+"/api/downloads/status")["torrent"].(map[string]any)
	if status["listenPort"] != float64(6882) {
		t.Fatalf("a saved port must be kept, got %+v", status)
	}
}
