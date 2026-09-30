package api_test

import (
	"net/http"
	"strings"
	"testing"
)

// A blank password keeps the saved one only for the same username. Changing
// the name without a password is refused rather than pairing the old password
// with the new name, and nothing else in the same request is saved.
func TestOpenSubtitlesNewUsernameNeedsAPassword(t *testing.T) {
	_, base, client := loginNewServer(t)
	put := func(body map[string]any, want int) map[string]any {
		return putJSONStatus(t, client, base+"/api/settings", body, want)
	}
	put(map[string]any{"openSubtitlesUsername": "ryan", "openSubtitlesPassword": "first-pw"}, http.StatusOK)

	got := put(map[string]any{"openSubtitlesUsername": "someone-else", "namingPreset": "kodi"}, http.StatusBadRequest)
	if msg, _ := got["error"].(string); !strings.Contains(msg, "password") {
		t.Fatalf("want a message asking for the password, got %+v", got)
	}
	s := getJSON[map[string]any](t, client, base+"/api/settings")
	if s["openSubtitlesAccountName"] != "ryan" {
		t.Errorf("the account name changed to %v although the request was refused", s["openSubtitlesAccountName"])
	}
	if s["namingPreset"] == "kodi" {
		t.Error("a refused request must not save its other fields")
	}

	// With a password the name can change, and the same name keeps its password.
	put(map[string]any{"openSubtitlesUsername": "someone-else", "openSubtitlesPassword": "second-pw"}, http.StatusOK)
	got = put(map[string]any{"openSubtitlesUsername": "someone-else"}, http.StatusOK)
	if got["openSubtitlesAccountName"] != "someone-else" || got["hasOpenSubtitlesAccount"] != true {
		t.Errorf("same name with a blank password should keep the account: %+v", got)
	}
}
