package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/library"
)

// Blocklisting from Activity can be for the title only; the list says so, and
// the scope can be switched afterwards.
func TestBlocklistFromTheQueueForOneTitleOnly(t *testing.T) {
	base, client, movieID, item, _ := failedGrab(t)
	qid := int64(item["id"].(float64))

	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/queue/%d/blocklist", base, qid), map[string]any{"scope": "item"}, http.StatusOK)

	list := getJSON[[]map[string]any](t, client, base+"/api/blocklist")
	if len(list) != 1 || list[0]["scope"] != "item" || list[0]["itemKind"] != "movie" || int64(list[0]["itemId"].(float64)) != movieID || list[0]["itemTitle"] != "Fixture Movie" {
		t.Fatalf("the entry should be for the movie only: %+v", list)
	}
	id := int64(list[0]["id"].(float64))

	// Switch to everywhere and back.
	if code, _ := doJSONStatus(t, client, http.MethodPut, fmt.Sprintf("%s/api/blocklist/%d/scope", base, id), map[string]any{"scope": "all"}); code != http.StatusOK {
		t.Fatalf("switch to everywhere: %d", code)
	}
	if got := getJSON[[]map[string]any](t, client, base+"/api/blocklist"); got[0]["scope"] != "all" {
		t.Fatalf("scope after switching: %+v", got)
	}
	if code, _ := doJSONStatus(t, client, http.MethodPut, fmt.Sprintf("%s/api/blocklist/%d/scope", base, id), map[string]any{"scope": "item"}); code != http.StatusOK {
		t.Fatalf("switch to the title only: %d", code)
	}
	if code, _ := doJSONStatus(t, client, http.MethodPut, fmt.Sprintf("%s/api/blocklist/%d/scope", base, id), map[string]any{"scope": "nonsense"}); code != http.StatusBadRequest {
		t.Fatalf("an unknown scope is refused: %d", code)
	}
	if code, _ := doJSONStatus(t, client, http.MethodPut, base+"/api/blocklist/99999/scope", map[string]any{"scope": "all"}); code != http.StatusNotFound {
		t.Fatalf("a missing entry is 404: %d", code)
	}
}

// Without a scope the call works as it always did: the release is blocked
// everywhere. The bulk call takes the same choice.
func TestBlocklistFromTheQueueDefaultsToEverywhere(t *testing.T) {
	base, client, _, item, _ := failedGrab(t)
	qid := int64(item["id"].(float64))
	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/queue/%d/blocklist", base, qid), nil, http.StatusOK)
	list := getJSON[[]map[string]any](t, client, base+"/api/blocklist")
	if len(list) != 1 || list[0]["scope"] != "all" {
		t.Fatalf("no scope means everywhere: %+v", list)
	}
}

func TestBulkBlocklistForOneTitleOnly(t *testing.T) {
	base, client, _, item, _ := failedGrab(t)
	qid := int64(item["id"].(float64))
	postJSON[map[string]any](t, client, base+"/api/queue/bulk", map[string]any{"ids": []int64{qid}, "action": "blocklist", "scope": "item"}, http.StatusOK)
	list := getJSON[[]map[string]any](t, client, base+"/api/blocklist")
	if len(list) != 1 || list[0]["scope"] != "item" {
		t.Fatalf("the bulk call should blocklist for the title only: %+v", list)
	}
	if code, _ := doJSONStatus(t, client, http.MethodPost, base+"/api/queue/bulk", map[string]any{"ids": []int64{qid}, "action": "blocklist", "scope": "x"}); code != http.StatusBadRequest {
		t.Fatalf("an unknown scope is refused: %d", code)
	}
}

// An entry that is not tied to a title cannot be limited to one.
func TestBlocklistScopeNeedsATitle(t *testing.T) {
	server, base, client := loginNewServer(t)
	if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 5, Title: "Fixture Movie", Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if err := server.Blocklist.Add(blocklist.Entry{ReleaseTitle: "Loose.Release"}); err != nil {
		t.Fatal(err)
	}
	list := getJSON[[]map[string]any](t, client, base+"/api/blocklist")
	id := int64(list[0]["id"].(float64))
	if code, _ := doJSONStatus(t, client, http.MethodPut, fmt.Sprintf("%s/api/blocklist/%d/scope", base, id), map[string]any{"scope": "item"}); code != http.StatusConflict {
		t.Fatalf("limiting a title-less entry should be refused: %d", code)
	}
}
