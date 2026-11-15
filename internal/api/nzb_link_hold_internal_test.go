package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
)

func TestNZBLinkHold(t *testing.T) {
	s := newAuditServer(t)
	const title = "Safety.Last.1923.1080p.BluRay.x264-LOGiN"
	link := "https://indexer.example/getnzb/abc.nzb&r=secretkey"
	other := "https://other.example/getnzb/abc.nzb"

	if err := notAnNZB([]byte("<!DOCTYPE html><html>Sign in</html>"), link); !isNZBLinkError(err) {
		t.Fatalf("a web page for an NZB is a link error: %v", err)
	}
	if isNZBLinkError(errors.New("couldn't read the NZB file")) {
		t.Fatal("other errors are not link errors")
	}

	now := time.Now()
	if !s.holdNZBLink(link, title, now) {
		t.Fatal("the first failure is notified")
	}
	if s.holdNZBLink(link, title, now.Add(time.Minute)) {
		t.Fatal("a repeat a minute later is not notified again")
	}
	if !s.nzbLinkHeld(link, title) || !s.nzbLinkHeld("https://INDEXER.example/other?id=1", strings.ToLower(title)) {
		t.Fatal("the release is held back on that indexer")
	}
	if s.nzbLinkHeld(other, title) {
		t.Fatal("the same release on another indexer is not held back")
	}
	if raw, _ := s.Settings.Get(settings.KeyNZBLinkHolds); strings.Contains(raw, "secretkey") {
		t.Fatalf("the stored hold must not keep the link's key: %s", raw)
	}

	// Searches leave it out; the copy on the other indexer stays.
	results := []indexers.Result{{Title: title, DownloadURL: link}, {Title: title, DownloadURL: other}}
	if got := dropBlocked(results, s.searchSkipKeys(), blocklist.Scope{}); len(got) != 1 || got[0].DownloadURL != other {
		t.Fatalf("dropBlocked kept %+v", got)
	}
	if err := s.grabRefused(grabAutomatic, title, link, blocklist.Scope{}); !errors.Is(err, errNZBLinkHeld) || !errors.Is(err, errBlocklisted) {
		t.Fatalf("an automatic grab is refused: %v", err)
	}
	if err := s.grabRefused(grabPicked, title, link, blocklist.Scope{}); err != nil {
		t.Fatalf("a release a person picks is never held back: %v", err)
	}

	// Once the hold has run out the release is searched again, and a failure
	// a day after the last notification is notified again.
	later := now.Add(nzbLinkHoldFor + time.Minute)
	if s.activeNZBLinkHolds(later)[nzbLinkHoldKey(link, title)] {
		t.Fatal("the hold runs out")
	}
	if s.holdNZBLink(link, title, later) {
		t.Fatal("within a day of the last notification, still quiet")
	}
	if !s.holdNZBLink(link, title, now.Add(nzbLinkQuietFor+time.Hour)) {
		t.Fatal("a day later it is notified again")
	}
}

// The Activity rows say what their status alone does not: a waiting download
// held back by low disk space says why, and a failed one whose release is
// held back says so.
func TestQueueRowNotes(t *testing.T) {
	s := newAuditServer(t)
	if err := s.Settings.Set(settings.KeyMinFreeGB, "10000", false); err != nil {
		t.Fatal(err)
	}
	waiting, err := s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: "Metropolis.1927.1080p.BluRay.x264-HDEX", NZBURL: "http://indexer.example/getnzb/1.nzb"})
	if err != nil {
		t.Fatal(err)
	}
	const held = "Safety.Last.1923.1080p.BluRay.x264-LOGiN"
	failed, err := s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: held, NZBURL: "http://indexer.example/getnzb/2.nzb"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.QueueRepo.SetStatus(failed, queue.StatusFailed, "the indexer sent a web page instead of the NZB file"); err != nil {
		t.Fatal(err)
	}
	s.holdNZBLink("http://indexer.example/getnzb/2.nzb", held, time.Now())

	rec := httptest.NewRecorder()
	s.handleListQueue(rec, httptest.NewRequest(http.MethodGet, "/api/queue", nil))
	var rows []queueItemPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	notes := map[int64]string{}
	for _, r := range rows {
		notes[r.ID] = r.Note
	}
	if !strings.HasPrefix(notes[waiting], "Downloads are waiting: only ") || !strings.HasSuffix(notes[waiting], "less than the 10000 GB you asked to keep.") {
		t.Errorf("waiting row note = %q", notes[waiting])
	}
	if notes[failed] != nzbLinkHeldNote {
		t.Errorf("failed row note = %q", notes[failed])
	}
}
