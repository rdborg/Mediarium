package api_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

// failedGrab leaves one failed queue item behind: a hunt grabs a release but
// there is no Usenet provider, so the pipeline fails (a setup failure, which
// deliberately does not blocklist anything).
func failedGrab(t *testing.T) (base string, client *http.Client, movieID int64, item map[string]any, srvServer interface{ TestHunt(context.Context) }) {
	t.Helper()
	server, base, client := loginNewServer(t)
	idx := newTVIndexerWith(t, []string{"Fixture.Movie.2001.1080p.WEB-DL.x264-GRP"})
	postJSON[map[string]any](t, client, base+"/api/indexers", map[string]any{"name": "U", "definitionId": "x", "baseUrl": idx.URL, "apiKey": "k", "protocol": "usenet"}, http.StatusCreated)
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 5, Title: "Fixture Movie", Year: 2001, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	server.TestHunt(context.Background())
	items := waitForQueue(t, client, base, 1)
	if len(items) != 1 || items[0]["status"] != "failed" {
		t.Fatalf("expected one failed grab, got %+v", items)
	}
	return base, client, m.ID, items[0], server
}

func TestQueueEntriesCarryTheTitleAndKind(t *testing.T) {
	_, _, _, item, _ := failedGrab(t)
	if item["title"] != "Fixture Movie" || item["protocol"] != "usenet" || item["releaseTitle"] == "" || item["addedAt"] == "" {
		t.Fatalf("queue entry should say which movie it is for: %+v", item)
	}
}

func TestRetryReplacesTheFailedEntry(t *testing.T) {
	base, client, _, item, _ := failedGrab(t)
	oldID := item["id"].(float64)

	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/queue/%d/retry", base, int64(oldID)), nil, http.StatusAccepted)
	items := waitForQueue(t, client, base, 1)
	if len(items) != 1 || items[0]["id"] == item["id"] {
		t.Fatalf("the failed entry should be replaced by a new attempt, got %+v", items)
	}
	// Retrying something that did not fail is refused.
	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/queue/9999/retry", base), nil, http.StatusNotFound)
}

func TestRemoveAndClearOnlyTouchFinishedEntries(t *testing.T) {
	server, base, client := loginNewServer(t)
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 5, Title: "Fixture Movie", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	enqueue := func(status queue.Status) int64 {
		id, err := server.QueueRepo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "r-" + string(status)})
		if err != nil {
			t.Fatal(err)
		}
		if status != queue.StatusQueued {
			_ = server.QueueRepo.SetStatus(id, status, "")
		}
		return id
	}
	active := enqueue(queue.StatusDownloading)
	failed := enqueue(queue.StatusFailed)
	done := enqueue(queue.StatusCompleted)

	if code := deleteReq(t, client, fmt.Sprintf("%s/api/queue/%d", base, active)); code != http.StatusConflict {
		t.Fatalf("a running download must not be removable, got %d", code)
	}
	if code := deleteReq(t, client, fmt.Sprintf("%s/api/queue/%d", base, failed)); code != http.StatusOK {
		t.Fatalf("removing a failed entry: %d", code)
	}
	if code := deleteReq(t, client, fmt.Sprintf("%s/api/queue/%d", base, failed)); code != http.StatusNotFound {
		t.Fatalf("removing twice should be 404, got %d", code)
	}

	if code := deleteReq(t, client, base+"/api/queue"); code != http.StatusOK {
		t.Fatalf("clear finished: %d", code)
	}
	left := getJSON[[]map[string]any](t, client, base+"/api/queue")
	if len(left) != 1 || int64(left[0]["id"].(float64)) != active {
		t.Fatalf("only the running download should remain (completed %d cleared): %+v", done, left)
	}
}

func TestBlocklistAndSearchAnotherFromTheQueue(t *testing.T) {
	base, client, _, item, _ := failedGrab(t)

	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/queue/%d/blocklist", base, int64(item["id"].(float64))), nil, http.StatusOK)

	list := getJSON[[]map[string]any](t, client, base+"/api/blocklist")
	if len(list) != 1 || list[0]["releaseTitle"] != item["releaseTitle"] || !strings.Contains(fmt.Sprint(list[0]["reason"]), "you") {
		t.Fatalf("the release should be on the blocklist: %+v", list)
	}
	for _, q := range getJSON[[]map[string]any](t, client, base+"/api/queue") {
		if q["id"] == item["id"] {
			t.Fatalf("the blocklisted entry should be gone from the queue: %+v", q)
		}
	}
}

// A failure stored with raw network text (as older versions wrote it) reaches
// the page as a plain sentence; the addresses stay out of it.
func TestQueueShowsFailuresInPlainWords(t *testing.T) {
	server, base, client := loginNewServer(t)
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 5, Title: "Fixture Movie", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	id, err := server.QueueRepo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "Fixture.Movie.2001"})
	if err != nil {
		t.Fatal(err)
	}
	raw := `couldn't get the NZB file: Get "https://api.example.com/getnzb/abc123.nzb": dial tcp: lookup api.example.com on 192.168.65.7:53: no such host`
	if err := server.QueueRepo.SetStatus(id, queue.StatusFailed, raw); err != nil {
		t.Fatal(err)
	}
	items := getJSON[[]map[string]any](t, client, base+"/api/queue")
	if len(items) != 1 {
		t.Fatalf("queue = %+v", items)
	}
	got, _ := items[0]["error"].(string)
	if !strings.HasPrefix(got, "Couldn't get the NZB file. The address could not be found") {
		t.Fatalf("error = %q", got)
	}
	for _, bad := range []string{"dial tcp", "192.168", "api.example.com"} {
		if strings.Contains(got, bad) {
			t.Fatalf("%q leaked %q", got, bad)
		}
	}
}
