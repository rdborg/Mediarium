package blocklist_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/library"
)

func TestReleaseID(t *testing.T) {
	tests := []struct {
		name    string
		indexer int64
		size    int64
		url     string
		want    string
	}{
		{"indexer and size", 4, 1000, "https://i/getnzb/abc", "i4:1000"},
		{"a magnet link is told by its hash", 4, 1000, "magnet:?dn=x&xt=urn:btih:ABCDEF0123456789ABCDEF0123456789ABCDEF01", "h:abcdef0123456789abcdef0123456789abcdef01"},
		{"no size", 4, 0, "https://i/getnzb/abc", ""},
		{"no indexer", 0, 1000, "https://i/getnzb/abc", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := blocklist.ReleaseID(tt.indexer, tt.size, tt.url); got != tt.want {
				t.Errorf("ReleaseID = %q, want %q", got, tt.want)
			}
		})
	}
}

// A name uploaded twice: the one that failed is blocked, the other is not. An
// entry from before (no upload known) still blocks both.
func TestBlockedByUpload(t *testing.T) {
	repo, movies := newRepos(t)
	one, _ := movies.Add(library.Movie{TMDBID: 1, Title: "One", Monitored: true})
	two, _ := movies.Add(library.Movie{TMDBID: 2, Title: "Two", Monitored: true})
	const name = "Same.Name.2020.1080p.WEB-DL-GRP"
	failed := blocklist.ReleaseID(3, 5000, "")
	other := blocklist.ReleaseID(3, 6000, "")
	if failed == "" || failed == other {
		t.Fatalf("ids %q %q", failed, other)
	}
	if err := repo.Add(blocklist.Entry{ReleaseTitle: name, ReleaseID: failed, MovieID: one.ID, Reason: "corrupt"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Add(blocklist.Entry{ReleaseTitle: "Old.Entry.2020", MovieID: one.ID, Reason: "old"}); err != nil {
		t.Fatal(err)
	}
	keys, err := repo.Keys()
	if err != nil {
		t.Fatal(err)
	}
	sc := blocklist.Scope{MovieID: two.ID}
	if !blocklist.BlockedRelease(keys, name, failed, sc) {
		t.Error("the upload that failed is not blocked")
	}
	if blocklist.BlockedRelease(keys, name, other, sc) {
		t.Error("another upload with the same name is blocked")
	}
	if blocklist.BlockedRelease(keys, name, "", sc) || blocklist.Blocked(keys, name, sc) {
		t.Error("a release whose upload is not known is blocked by an entry for one upload")
	}
	for _, id := range []string{failed, other, ""} {
		if !blocklist.BlockedRelease(keys, "old.entry.2020", id, sc) {
			t.Errorf("an entry without an upload must block every upload (id %q)", id)
		}
	}

	// Blocked for one title only, and for one upload.
	if err := repo.Add(blocklist.Entry{ReleaseTitle: "Scoped.Name", ReleaseID: failed, MovieID: one.ID, ItemOnly: true}); err != nil {
		t.Fatal(err)
	}
	keys, _ = repo.Keys()
	if !blocklist.BlockedRelease(keys, "Scoped.Name", failed, blocklist.Scope{MovieID: one.ID}) {
		t.Error("scoped upload entry does not block its own title")
	}
	if blocklist.BlockedRelease(keys, "Scoped.Name", failed, sc) || blocklist.BlockedRelease(keys, "Scoped.Name", other, blocklist.Scope{MovieID: one.ID}) {
		t.Error("scoped upload entry blocks too much")
	}
}

func TestUploadEntryKeepsItsUploadThroughScopeSwitchAndList(t *testing.T) {
	repo, movies := newRepos(t)
	one, _ := movies.Add(library.Movie{TMDBID: 1, Title: "One", Monitored: true})
	const name = "Same.Name.2020.1080p.WEB-DL-GRP"
	id := blocklist.ReleaseID(3, 5000, "")
	if err := repo.Add(blocklist.Entry{ReleaseTitle: name, ReleaseID: id, MovieID: one.ID, Reason: "corrupt"}); err != nil {
		t.Fatal(err)
	}
	list, err := repo.List()
	if err != nil || len(list) != 1 || list[0].ReleaseID != id {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if reason, ok, _ := repo.ReasonOf(name, id); !ok || reason != "corrupt" {
		t.Errorf("ReasonOf = %q, %v", reason, ok)
	}
	if _, ok, _ := repo.ReasonOf(name, blocklist.ReleaseID(3, 6000, "")); ok {
		t.Error("another upload has a reason")
	}
	if err := repo.SetScope(list[0].ID, true); err != nil {
		t.Fatal(err)
	}
	keys, _ := repo.Keys()
	if !blocklist.BlockedRelease(keys, name, id, blocklist.Scope{MovieID: one.ID}) || blocklist.BlockedRelease(keys, name, id, blocklist.Scope{}) {
		t.Error("the entry is not blocked for its title only after the switch")
	}
	if err := repo.SetScope(list[0].ID, false); err != nil {
		t.Fatal(err)
	}
	keys, _ = repo.Keys()
	if !blocklist.BlockedRelease(keys, name, id, blocklist.Scope{}) || blocklist.BlockedRelease(keys, name, blocklist.ReleaseID(3, 6000, ""), blocklist.Scope{}) {
		t.Error("the entry lost its upload when switched back")
	}
	// Blocking the same upload again by hand narrows it, not duplicates it.
	if err := repo.Add(blocklist.Entry{ReleaseTitle: name, ReleaseID: id, MovieID: one.ID, ItemOnly: true}); err != nil {
		t.Fatal(err)
	}
	if list, _ = repo.List(); len(list) != 1 || !list[0].ItemOnly {
		t.Errorf("list = %+v", list)
	}
}

// Uploads of one name, one size and one indexer, told apart by guid: only the
// guid that failed is blocked. Entries from before (by size, or by name alone)
// keep matching the way they did.
func TestBlockedByGUID(t *testing.T) {
	repo, movies := newRepos(t)
	mv, _ := movies.Add(library.Movie{TMDBID: 1, Title: "One", Monitored: true})
	const name = "Same.Name.2020.1080p.WEB-DL-GRP"
	const size = 5000
	sizeID := blocklist.ReleaseID(3, size, "")
	badIDs := blocklist.ReleaseIDs(3, "guid-bad", size, "")
	goodIDs := blocklist.ReleaseIDs(3, "guid-good", size, "")
	if len(badIDs) != 2 || badIDs[0] == goodIDs[0] || badIDs[1] != sizeID {
		t.Fatalf("ids %v %v", badIDs, goodIDs)
	}
	if got := blocklist.ReleaseIDs(3, "  ", size, ""); len(got) != 1 || got[0] != sizeID {
		t.Fatalf("a blank guid changed the ids: %v", got)
	}
	if got := blocklist.ReleaseIDs(3, "", 0, ""); len(got) != 0 {
		t.Fatalf("ids without anything known: %v", got)
	}

	add := func(title string, id string) {
		t.Helper()
		if err := repo.Add(blocklist.Entry{ReleaseTitle: title, ReleaseID: id, MovieID: mv.ID, Reason: "corrupt"}); err != nil {
			t.Fatal(err)
		}
	}
	add(name, badIDs[0])          // new entry: by guid
	add("Old.Sized.2020", sizeID) // entry from before: by size
	add("Old.Named.2020", "")     // entry from before: by name alone
	keys, err := repo.Keys()
	if err != nil {
		t.Fatal(err)
	}
	sc := blocklist.Scope{MovieID: mv.ID}
	tests := []struct {
		name  string
		title string
		ids   []string
		want  bool
	}{
		{"the guid that failed", name, badIDs, true},
		{"same name and size, other guid", name, goodIDs, false},
		{"same name and size, no guid known", name, []string{sizeID}, false},
		{"title case does not matter", "SAME.name.2020.1080p.web-dl-grp", badIDs, true},
		{"old size entry, same size and another guid", "Old.Sized.2020", goodIDs, true},
		{"old size entry, another size", "Old.Sized.2020", blocklist.ReleaseIDs(3, "guid-x", 6000, ""), false},
		{"old name entry blocks every guid", "Old.Named.2020", goodIDs, true},
		{"old name entry blocks with no ids", "Old.Named.2020", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := blocklist.BlockedAny(keys, tt.title, tt.ids, sc); got != tt.want {
				t.Errorf("BlockedAny = %v, want %v", got, tt.want)
			}
		})
	}

	// The reason is found the same way.
	if r, ok, err := repo.ReasonOfAny(name, badIDs); err != nil || !ok || r != "corrupt" {
		t.Errorf("reason of the failed guid = %q %v %v", r, ok, err)
	}
	if _, ok, _ := repo.ReasonOfAny(name, goodIDs); ok {
		t.Error("the other guid has a reason")
	}
	if _, ok, _ := repo.ReasonOfAny("Old.Named.2020", goodIDs); !ok {
		t.Error("an old name entry has no reason for a guid")
	}
}

// An upload that failed while its guid was known is still blocked when it
// turns up later without a guid (same indexer, same size), and still not when
// it turns up with another guid. The entry made through PackIDs keeps both.
func TestBlockedByGUIDFallsBackToSizeWhenTheGUIDIsUnknown(t *testing.T) {
	repo, movies := newRepos(t)
	mv, _ := movies.Add(library.Movie{TMDBID: 1, Title: "One", Monitored: true})
	other, _ := movies.Add(library.Movie{TMDBID: 2, Title: "Two", Monitored: true})
	const name = "Same.Name.2020.1080p.WEB-DL-GRP"
	const size = 5000
	failed := blocklist.ReleaseIDs(3, "guid-bad", size, "")
	noGUID := blocklist.ReleaseIDs(3, "", size, "")
	otherGUID := blocklist.ReleaseIDs(3, "guid-good", size, "")
	otherSize := blocklist.ReleaseIDs(3, "", size+1, "")
	otherIndexer := blocklist.ReleaseIDs(4, "", size, "")

	packed := blocklist.PackIDs(failed)
	if packed == failed[0] || blocklist.PackIDs(noGUID) != noGUID[0] || blocklist.PackIDs(nil) != "" {
		t.Fatalf("PackIDs: %q for a guid, %q without", packed, blocklist.PackIDs(noGUID))
	}
	if err := repo.Add(blocklist.Entry{ReleaseTitle: name, ReleaseID: packed, MovieID: mv.ID, Reason: "corrupt"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Add(blocklist.Entry{ReleaseTitle: "Scoped.Name.2020", ReleaseID: packed, MovieID: mv.ID, ItemOnly: true, Reason: "corrupt"}); err != nil {
		t.Fatal(err)
	}
	list, err := repo.List()
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	for _, e := range list {
		if e.ReleaseID != failed[0] {
			t.Errorf("entry %q is listed under %q, want the guid %q", e.ReleaseTitle, e.ReleaseID, failed[0])
		}
	}
	keys, err := repo.Keys()
	if err != nil {
		t.Fatal(err)
	}
	sc := blocklist.Scope{MovieID: mv.ID}
	tests := []struct {
		name  string
		title string
		ids   []string
		sc    blocklist.Scope
		want  bool
	}{
		{"the guid that failed", name, failed, sc, true},
		{"the same upload without a guid", name, noGUID, sc, true},
		{"the same upload without a guid, another title", name, noGUID, blocklist.Scope{MovieID: other.ID}, true},
		{"another guid of the same size", name, otherGUID, sc, false},
		{"another size without a guid", name, otherSize, sc, false},
		{"another indexer without a guid", name, otherIndexer, sc, false},
		{"another name without a guid", "Different.Name.2020", noGUID, sc, false},
		{"nothing known about the upload", name, nil, sc, false},
		{"scoped: the same upload without a guid, for its title", "Scoped.Name.2020", noGUID, sc, true},
		{"scoped: not for another title", "Scoped.Name.2020", noGUID, blocklist.Scope{MovieID: other.ID}, false},
		{"scoped: not for no title", "Scoped.Name.2020", noGUID, blocklist.Scope{}, false},
		{"scoped: another guid", "Scoped.Name.2020", otherGUID, sc, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := blocklist.BlockedAny(keys, tt.title, tt.ids, tt.sc); got != tt.want {
				t.Errorf("BlockedAny = %v, want %v", got, tt.want)
			}
		})
	}
	// The reason is found the same way.
	if r, ok, err := repo.ReasonOfAny(name, noGUID); err != nil || !ok || r != "corrupt" {
		t.Errorf("reason of the same upload without a guid = %q %v %v", r, ok, err)
	}
	if _, ok, _ := repo.ReasonOfAny(name, otherGUID); ok {
		t.Error("another guid has a reason")
	}
	if _, ok, _ := repo.ReasonOfAny(name, otherSize); ok {
		t.Error("another size has a reason")
	}

	// Switching the scope keeps the fallback, and removing the entry drops it.
	if err := repo.SetScope(list[0].ID, !list[0].ItemOnly); err != nil {
		t.Fatal(err)
	}
	keys, _ = repo.Keys()
	if !blocklist.BlockedAny(keys, name, noGUID, sc) && !blocklist.BlockedAny(keys, "Scoped.Name.2020", noGUID, sc) {
		t.Error("the fallback was lost when the scope was switched")
	}
	for _, e := range list {
		if err := repo.Remove(e.ID); err != nil {
			t.Fatal(err)
		}
	}
	keys, _ = repo.Keys()
	if blocklist.BlockedAny(keys, name, noGUID, sc) {
		t.Error("a removed entry still blocks")
	}
}
