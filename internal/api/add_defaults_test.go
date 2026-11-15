package api_test

import (
	"net/http"
	"testing"
)

// The add dialog's two starting choices are on until switched off, read back
// from both the settings and the quality-profile list (which members can read),
// and each can be changed on its own.
func TestAddDialogDefaults(t *testing.T) {
	_, base, client := loginNewServer(t)

	read := func() (map[string]any, map[string]any) {
		return getJSON[map[string]any](t, client, base+"/api/settings"), getJSON[map[string]any](t, client, base+"/api/quality-profiles")
	}
	check := func(step string, monitored, search bool) {
		t.Helper()
		st, pr := read()
		if st["addMonitored"] != monitored || st["addSearchNow"] != search {
			t.Fatalf("%s: settings say %v/%v, want %v/%v", step, st["addMonitored"], st["addSearchNow"], monitored, search)
		}
		if pr["addMonitored"] != monitored || pr["addSearchNow"] != search {
			t.Fatalf("%s: profile list says %v/%v, want %v/%v", step, pr["addMonitored"], pr["addSearchNow"], monitored, search)
		}
	}

	check("fresh install", true, true)

	putJSON(t, client, base+"/api/settings", map[string]any{"addMonitored": false}, http.StatusOK)
	check("monitoring off only", false, true)

	putJSON(t, client, base+"/api/settings", map[string]any{"addSearchNow": false}, http.StatusOK)
	check("both off", false, false)

	// A save that leaves them out keeps them.
	putJSON(t, client, base+"/api/settings", map[string]any{"seasonPacks": true}, http.StatusOK)
	check("unrelated save", false, false)

	putJSON(t, client, base+"/api/settings", map[string]any{"addMonitored": true, "addSearchNow": true}, http.StatusOK)
	check("back on", true, true)
}
