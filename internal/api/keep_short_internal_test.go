package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/mediainfo"
)

func TestIsLengthFailure(t *testing.T) {
	var bad *badReleaseError
	if !errors.As(incompleteFile(mediainfo.KindEpisode, 12*60, 25), &bad) {
		t.Fatal("a short file should be a bad release")
	}
	tests := []struct {
		msg  string
		want bool
	}{
		{bad.Error(), true},
		{incompleteFile(mediainfo.KindMovie, 600, 0).Error(), true},
		{incompleteFile(mediainfo.KindMovie, 600, 90).Error(), true},
		{"The file movie.rar is a RAR archive, not a video: the release was never unpacked.", false},
		{"The download could not be finished.", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isLengthFailure(tt.msg); got != tt.want {
			t.Errorf("isLengthFailure(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

// "Keep anyway" on the blocklist remembers the release and takes the entry
// off, so the length check lets it through and it is not blocked again.
func TestKeepBlocklistEntry(t *testing.T) {
	s := newAuditServer(t)
	const rel = "Show.S01E01.GERMAN.720p.WEB-GRP"
	if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: rel, Reason: incompleteReason}); err != nil {
		t.Fatal(err)
	}
	if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: "Other.Release-GRP", Reason: "couldn't unpack the download"}); err != nil {
		t.Fatal(err)
	}
	entries, _ := s.Blocklist.List()
	idOf := map[string]int64{}
	for _, e := range entries {
		idOf[e.ReleaseTitle] = e.ID
	}
	call := func(id int64) int {
		r := httptest.NewRequest(http.MethodPost, "/api/blocklist/x/keep", nil)
		r.SetPathValue("id", strconv.FormatInt(id, 10))
		w := httptest.NewRecorder()
		s.handleKeepBlocklistEntry(w, r)
		return w.Code
	}
	if s.lengthKept(rel) {
		t.Fatal("nothing is kept yet")
	}
	if got := call(idOf["Other.Release-GRP"]); got != http.StatusConflict {
		t.Fatalf("a release blocked for another reason: status %d, want 409", got)
	}
	if got := call(9999); got != http.StatusNotFound {
		t.Fatalf("a missing entry: status %d, want 404", got)
	}
	if got := call(idOf[rel]); got != http.StatusOK {
		t.Fatalf("keep: status %d", got)
	}
	if !s.lengthKept("  " + rel + " ") {
		t.Fatal("the release should be remembered as kept")
	}
	if keys, _ := s.Blocklist.Keys(); keys[blocklist.Key(rel)] {
		t.Fatal("the release should be off the blocklist")
	}
	if left, _ := s.Blocklist.List(); len(left) != 1 {
		t.Fatalf("the other entry should stay, got %d entries", len(left))
	}
}
