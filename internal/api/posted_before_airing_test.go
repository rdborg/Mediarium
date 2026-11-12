package api

import (
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
)

func TestDropPostedBeforeAiring(t *testing.T) {
	day := func(s string) time.Time { v, _ := time.Parse("2006-01-02", s); return v }
	old := indexers.Result{Title: "Scrubs.S02E08.1080p.WEB-DL", PublishDate: day("2019-05-01")}
	fresh := indexers.Result{Title: "Scrubs.S02E08.1080p.WEB-DL.new", PublishDate: day("2026-11-12")}
	early := indexers.Result{Title: "Scrubs.S02E08.early", PublishDate: day("2026-11-10")}
	undated := indexers.Result{Title: "Scrubs.S02E08.undated"}
	in := []indexers.Result{old, fresh, early, undated}

	got := dropPostedBeforeAiring(in, "2026-11-11")
	if len(got) != 3 || got[0].Title != fresh.Title || got[1].Title != early.Title || got[2].Title != undated.Title {
		t.Fatalf("kept %+v, want the fresh, the 1-day-early and the undated release", got)
	}
	if got := dropPostedBeforeAiring(in, ""); len(got) != 4 {
		t.Fatalf("no air date must keep everything, kept %d", len(got))
	}
	if got := dropPostedBeforeAiring(in, "bad"); len(got) != 4 {
		t.Fatalf("bad air date must keep everything, kept %d", len(got))
	}
}
