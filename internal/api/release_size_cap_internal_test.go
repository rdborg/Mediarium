package api

import (
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/settings"
)

// The largest-release setting reaches the profiles every search uses, and an
// interactive search says why a big release is skipped.
func TestMaxReleaseGBReachesProfilesAndReasons(t *testing.T) {
	s := newAuditServer(t)
	if got := s.maxReleaseGB(); got != 0 {
		t.Fatalf("by default there is no limit, got %d", got)
	}
	if err := s.Settings.Set(settings.KeyMaxReleaseGB, "20", false); err != nil {
		t.Fatal(err)
	}
	if got := s.maxReleaseGB(); got != 20 {
		t.Fatalf("maxReleaseGB = %d, want 20", got)
	}
	ps, err := s.loadProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if ps.def.CapGB != 20 {
		t.Fatalf("the default profile should carry the cap, got %v", ps.def.CapGB)
	}
	for id, p := range ps.byID {
		if p.CapGB != 20 {
			t.Fatalf("profile %d lost the cap: %v", id, p.CapGB)
		}
	}
	const gb = int64(1) << 30
	const title = "Some.Movie.2020.2160p.WEB-DL.DDP5.1.x265-GRP"
	big := s.sizeRejections(quality.SizeLimits{}, 0, ps.def, title, 25*gb, 0)
	if len(big) != 1 || !strings.Contains(big[0], "20 GB you allow for a release") {
		t.Fatalf("a 25 GB release should be refused for the cap, got %v", big)
	}
	for _, r := range s.sizeRejections(quality.SizeLimits{}, 0, ps.def, title, 15*gb, 0) {
		if strings.Contains(r, "you allow for a release") {
			t.Fatalf("a 15 GB release is within the cap: %v", r)
		}
	}
}

// A disk below the reserve takes no new release, and the reason names the
// number the person set.
func TestReserveStopsGrabsWhenTheDiskWouldFill(t *testing.T) {
	s := newAuditServer(t)
	// No real disk has this much free, so every release is "too big for the room".
	if err := s.Settings.Set(settings.KeyMinFreeGB, "10000", false); err != nil {
		t.Fatal(err)
	}
	if room := s.downloadRoomBytes(); room != 1 {
		t.Fatalf("a disk below the reserve takes nothing, room = %d", room)
	}
	ps, err := s.loadProfiles()
	if err != nil {
		t.Fatal(err)
	}
	const title = "Some.Movie.2020.1080p.WEB-DL.DDP5.1.x264-GRP"
	got := s.sizeRejections(quality.SizeLimits{}, 0, ps.def, title, 4<<30, 0)
	found := false
	for _, r := range got {
		found = found || strings.Contains(r, "less than the 10000 GB you keep free")
	}
	if !found {
		t.Fatalf("the reason should name the reserve, got %v", got)
	}
	// And a download that is started anyway is refused before it fetches anything.
	err = s.checkRoomForRelease(t.TempDir(), 1<<20)
	if err == nil || !strings.Contains(err.Error(), "you asked to keep") {
		t.Fatalf("the start of the download should refuse too, got %v", err)
	}
	// With no minimum only the small fixed reserve applies: a small release fits.
	if err := s.Settings.Set(settings.KeyMinFreeGB, "0", false); err != nil {
		t.Fatal(err)
	}
	if err := s.checkRoomForRelease(t.TempDir(), 1<<20); err != nil {
		t.Fatalf("with no minimum a small release fits: %v", err)
	}
}
