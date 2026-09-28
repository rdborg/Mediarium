package api_test

import (
	"net/http"
	"testing"
	"time"
)

func TestLegalAcknowledgement(t *testing.T) {
	_, base, client := loginNewServer(t)

	st := getJSON[map[string]any](t, client, base+"/api/settings")
	if st["legalAcknowledgedAt"] != "" {
		t.Fatalf("nothing acknowledged yet: %+v", st)
	}

	// legalAcknowledged:true is stamped with the server's own clock.
	before := time.Now().UTC().Add(-2 * time.Second)
	st = postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"legalAcknowledged": true}, http.StatusOK)
	stamp, _ := st["legalAcknowledgedAt"].(string)
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil || at.Before(before) || at.After(time.Now().Add(2*time.Second)) {
		t.Fatalf("expected a fresh RFC 3339 server timestamp, got %q (%v)", stamp, err)
	}

	// Unrelated updates keep it; an explicit value is stored as given.
	st = postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"namingPreset": "plex"}, http.StatusOK)
	if st["legalAcknowledgedAt"] != stamp {
		t.Fatalf("a partial update lost the acknowledgement: %+v", st)
	}
	st = postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"legalAcknowledgedAt": "2026-01-02T03:04:05Z"}, http.StatusOK)
	if st["legalAcknowledgedAt"] != "2026-01-02T03:04:05Z" {
		t.Fatalf("explicit value should be stored: %+v", st)
	}
}
