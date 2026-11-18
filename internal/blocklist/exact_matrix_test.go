package blocklist_test

import (
	"fmt"
	"testing"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/library"
)

// candidate is a search result as the blocklist sees it.
type candidate struct {
	label   string
	title   string
	indexer int64
	guid    string
	size    int64
	url     string
	scope   blocklist.Scope
}

func (c candidate) ids() []string { return blocklist.ReleaseIDs(c.indexer, c.guid, c.size, c.url) }

const matrixName = "Fixture.Movie.2001.1080p.BluRay.x264-GRP"
const hashA = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=x"
const hashB = "magnet:?xt=urn:btih:ffffffffffffffffffffffffffffffffffffffff&dn=x"

// TestExactBlocklistMatrix puts one failed upload on the blocklist in each way
// an entry can be made and asks, for a set of other uploads, which are blocked.
// An entry for one upload must never block another upload of the same name,
// and an entry made without knowing the upload must block them all.
func TestExactBlocklistMatrix(t *testing.T) {
	repo, movies := newRepos(t)
	a, _ := movies.Add(library.Movie{TMDBID: 1, Title: "A", Monitored: true})
	b, _ := movies.Add(library.Movie{TMDBID: 2, Title: "B", Monitored: true})
	scA, scB := blocklist.Scope{MovieID: a.ID}, blocklist.Scope{MovieID: b.ID}

	type entryCase struct {
		name    string
		entry   func() blocklist.Entry
		blocked map[string]bool // candidate label -> blocked
	}
	cands := []candidate{
		{label: "same guid", title: matrixName, indexer: 4, guid: "abc", size: 1000, scope: scA},
		{label: "same guid, lower case title", title: "fixture.movie.2001.1080p.bluray.x264-grp", indexer: 4, guid: "abc", size: 1000, scope: scA},
		{label: "same guid, padded title", title: "  " + matrixName + " ", indexer: 4, guid: "abc", size: 1000, scope: scA},
		{label: "other guid, same size, same indexer", title: matrixName, indexer: 4, guid: "xyz", size: 1000, scope: scA},
		{label: "no guid, same size, same indexer", title: matrixName, indexer: 4, size: 1000, scope: scA},
		{label: "no guid, other size, same indexer", title: matrixName, indexer: 4, size: 2000, scope: scA},
		{label: "other guid, other size", title: matrixName, indexer: 4, guid: "xyz", size: 2000, scope: scA},
		{label: "same guid on another indexer", title: matrixName, indexer: 5, guid: "abc", size: 1000, scope: scA},
		{label: "same size on another indexer", title: matrixName, indexer: 5, size: 1000, scope: scA},
		{label: "magnet hash A", title: matrixName, indexer: 6, size: 1000, url: hashA, scope: scA},
		{label: "magnet hash B", title: matrixName, indexer: 6, size: 1000, url: hashB, scope: scA},
		{label: "name that only starts the same", title: matrixName + ".REPACK", indexer: 4, guid: "abc", size: 1000, scope: scA},
		{label: "another name", title: "Other.Movie.2001.1080p.BluRay.x264-GRP", indexer: 4, guid: "abc", size: 1000, scope: scA},
		{label: "same guid, search for B", title: matrixName, indexer: 4, guid: "abc", size: 1000, scope: scB},
		{label: "same guid, search for nothing in particular", title: matrixName, indexer: 4, guid: "abc", size: 1000},
	}

	entries := []entryCase{
		{
			name:  "by name only",
			entry: func() blocklist.Entry { return blocklist.Entry{ReleaseTitle: matrixName, Reason: "bad"} },
			blocked: map[string]bool{
				"same guid": true, "same guid, lower case title": true, "same guid, padded title": true,
				"other guid, same size, same indexer": true, "no guid, same size, same indexer": true,
				"no guid, other size, same indexer": true, "other guid, other size": true,
				"same guid on another indexer": true, "same size on another indexer": true,
				"magnet hash A": true, "magnet hash B": true,
				"same guid, search for B": true, "same guid, search for nothing in particular": true,
			},
		},
		{
			name: "by guid",
			entry: func() blocklist.Entry {
				return blocklist.Entry{ReleaseTitle: matrixName, Reason: "bad", ReleaseID: blocklist.PackIDs(blocklist.ReleaseIDs(4, "abc", 1000, ""))}
			},
			blocked: map[string]bool{
				"same guid": true, "same guid, lower case title": true, "same guid, padded title": true,
				// A release without a guid is looked up under the size-based id of the same upload.
				"no guid, same size, same indexer": true,
				"same guid, search for B":          true, "same guid, search for nothing in particular": true,
			},
		},
		{
			name: "by size and indexer",
			entry: func() blocklist.Entry {
				return blocklist.Entry{ReleaseTitle: matrixName, Reason: "bad", ReleaseID: blocklist.ReleaseID(4, 1000, "")}
			},
			blocked: map[string]bool{
				"same guid": true, "same guid, lower case title": true, "same guid, padded title": true,
				"other guid, same size, same indexer": true, "no guid, same size, same indexer": true,
				"same guid, search for B": true, "same guid, search for nothing in particular": true,
			},
		},
		{
			name: "by magnet hash",
			entry: func() blocklist.Entry {
				return blocklist.Entry{ReleaseTitle: matrixName, Reason: "bad", ReleaseID: blocklist.ReleaseID(6, 1000, hashA)}
			},
			blocked: map[string]bool{"magnet hash A": true},
		},
		{
			name: "by guid, for one movie only",
			entry: func() blocklist.Entry {
				return blocklist.Entry{ReleaseTitle: matrixName, Reason: "bad", MovieID: a.ID, ItemOnly: true,
					ReleaseID: blocklist.PackIDs(blocklist.ReleaseIDs(4, "abc", 1000, ""))}
			},
			blocked: map[string]bool{
				"same guid": true, "same guid, lower case title": true, "same guid, padded title": true,
				"no guid, same size, same indexer": true,
			},
		},
		{
			name: "by name, for one movie only",
			entry: func() blocklist.Entry {
				return blocklist.Entry{ReleaseTitle: matrixName, Reason: "bad", MovieID: a.ID, ItemOnly: true}
			},
			blocked: map[string]bool{
				"same guid": true, "same guid, lower case title": true, "same guid, padded title": true,
				"other guid, same size, same indexer": true, "no guid, same size, same indexer": true,
				"no guid, other size, same indexer": true, "other guid, other size": true,
				"same guid on another indexer": true, "same size on another indexer": true,
				"magnet hash A": true, "magnet hash B": true,
			},
		},
	}

	checked := 0
	for _, ec := range entries {
		t.Run(ec.name, func(t *testing.T) {
			repo, movies := newRepos(t)
			ma, _ := movies.Add(library.Movie{TMDBID: 1, Title: "A", Monitored: true})
			mb, _ := movies.Add(library.Movie{TMDBID: 2, Title: "B", Monitored: true})
			_ = mb
			e := ec.entry()
			if e.MovieID != 0 {
				e.MovieID = ma.ID
			}
			if err := repo.Add(e); err != nil {
				t.Fatal(err)
			}
			keys, err := repo.Keys()
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range cands {
				sc := c.scope
				if sc.MovieID == a.ID {
					sc.MovieID = ma.ID
				} else if sc.MovieID == b.ID {
					sc.MovieID = mb.ID
				}
				want := ec.blocked[c.label]
				// A one-movie entry only blocks searches for that movie.
				if e.ItemOnly && sc.MovieID != ma.ID {
					want = false
				}
				if got := blocklist.BlockedAny(keys, c.title, c.ids(), sc); got != want {
					t.Errorf("%s: candidate %q blocked = %v, want %v", ec.name, c.label, got, want)
				}
				checked++
			}
		})
	}
	_ = repo
	_ = scA
	_ = scB
	t.Log(fmt.Sprintf("%d entry and candidate pairs checked", checked))
}

// ReleaseIDs, PackIDs and FallbackID are the pieces the matrix relies on.
func TestReleaseIDShapes(t *testing.T) {
	for _, tc := range []struct {
		name         string
		indexer      int64
		guid         string
		size         int64
		url          string
		wantIDs      []string
		wantPacked   string
		wantFallback string
	}{
		{"guid and size", 4, "abc", 1000, "", []string{"g4:abc", "i4:1000"}, "g4:abc\x00fb\x00i4:1000", "~g4:abc"},
		{"guid only", 4, "abc", 0, "", []string{"g4:abc"}, "g4:abc", "~g4:abc"},
		{"guid with spaces around", 4, "  abc  ", 0, "", []string{"g4:abc"}, "g4:abc", "~g4:abc"},
		{"size only", 4, "", 1000, "", []string{"i4:1000"}, "i4:1000", "~i4:1000"},
		{"magnet beats size", 4, "", 1000, hashA, []string{"h:0123456789abcdef0123456789abcdef01234567"}, "h:0123456789abcdef0123456789abcdef01234567", "~h:0123456789abcdef0123456789abcdef01234567"},
		{"upper case magnet hash", 4, "", 0, "magnet:?xt=urn:btih:0123456789ABCDEF0123456789ABCDEF01234567", []string{"h:0123456789abcdef0123456789abcdef01234567"}, "h:0123456789abcdef0123456789abcdef01234567", "~h:0123456789abcdef0123456789abcdef01234567"},
		{"nothing known", 0, "", 0, "https://x/y", nil, "", ""},
		{"size but no indexer", 0, "", 1000, "", nil, "", ""},
		{"indexer but no size", 4, "", 0, "https://x/y", nil, "", ""},
		{"32 character base32 hash", 4, "", 0, "magnet:?xt=urn:btih:ABCDEFGHIJKLMNOPQRSTUVWXYZ234567", []string{"h:abcdefghijklmnopqrstuvwxyz234567"}, "h:abcdefghijklmnopqrstuvwxyz234567", "~h:abcdefghijklmnopqrstuvwxyz234567"},
		{"short hash is not a hash", 4, "", 100, "magnet:?xt=urn:btih:abc123", []string{"i4:100"}, "i4:100", "~i4:100"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids := blocklist.ReleaseIDs(tc.indexer, tc.guid, tc.size, tc.url)
			if fmt.Sprint(ids) != fmt.Sprint(tc.wantIDs) {
				t.Errorf("ids %q, want %q", ids, tc.wantIDs)
			}
			if got := blocklist.PackIDs(ids); got != tc.wantPacked {
				t.Errorf("packed %q, want %q", got, tc.wantPacked)
			}
			if len(ids) > 0 {
				if got := blocklist.FallbackID(ids[0]); got != tc.wantFallback {
					t.Errorf("fallback %q, want %q", got, tc.wantFallback)
				}
			}
		})
	}
	if blocklist.FallbackID("") != "" {
		t.Error("an empty id has a fallback")
	}
}
