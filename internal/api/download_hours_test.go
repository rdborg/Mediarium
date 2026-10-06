package api_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestDownloadsWaitForTheirHours(t *testing.T) {
	e := newControlEnv(t, -1)
	h := time.Now().Hour()
	closed := fmt.Sprintf("%d-%d", (h+2)%24, (h+3)%24) // a window that isn't now
	postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/settings", map[string]any{"downloadHours": "25-3"}, http.StatusBadRequest)
	postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/settings", map[string]any{"downloadHours": closed}, http.StatusOK)
	if got := getJSON[map[string]any](t, e.client, e.base+"/api/settings")["downloadHours"]; got != closed {
		t.Fatalf("saved hours: %v", got)
	}
	id := e.grab(t)
	time.Sleep(300 * time.Millisecond)
	if st := e.status(t, id); st != "queued" {
		t.Fatalf("outside the hours a download waits, it is %s", st)
	}
	// Any time again: it starts.
	postJSONMethod[map[string]any](t, e.client, http.MethodPut, e.base+"/api/settings", map[string]any{"downloadHours": ""}, http.StatusOK)
	waitFor(t, "the download to start", func() bool { return e.status(t, id) != "queued" })
	e.waitAllDone(t, []int64{id})
}
