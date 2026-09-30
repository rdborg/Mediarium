package library_test

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/store"
)

func TestBulkSetChangesEveryTitleAndReportsUnknownOnes(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	repo := library.NewRepo(db)
	res, err := db.Exec(`INSERT INTO quality_profiles (name, allowed_tiers, cutoff, upgrade_allowed) VALUES ('Mine', 'WEBDL-1080p', 'WEBDL-1080p', 0)`)
	if err != nil {
		t.Fatal(err)
	}
	profileID, _ := res.LastInsertId()
	m1, _ := repo.Add(library.Movie{TMDBID: 1, Title: "One", Monitored: true})
	m2, _ := repo.Add(library.Movie{TMDBID: 2, Title: "Two", Monitored: true})
	show, err := repo.AddSeries(library.Series{TMDBID: 3, Title: "Show", Monitored: true}, nil)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		field   library.BulkField
		value   any
		refs    []library.Ref
		missing int
		check   func(t *testing.T)
	}{
		{"stop monitoring", library.BulkMonitored, false,
			[]library.Ref{{Kind: "movie", ID: m1.ID}, {Kind: "tv", ID: show.ID}, {Kind: "movie", ID: 9999}}, 1,
			func(t *testing.T) {
				a, _ := repo.Get(m1.ID)
				b, _ := repo.Get(m2.ID)
				s, _ := repo.GetSeries(show.ID)
				if a.Monitored || !b.Monitored || s.Monitored {
					t.Fatalf("monitored: %v %v %v", a.Monitored, b.Monitored, s.Monitored)
				}
			}},
		{"no better versions", library.BulkNoUpgrade, true,
			[]library.Ref{{Kind: "movie", ID: m2.ID}, {Kind: "tv", ID: show.ID}}, 0,
			func(t *testing.T) {
				a, _ := repo.Get(m1.ID)
				b, _ := repo.Get(m2.ID)
				s, _ := repo.GetSeries(show.ID)
				if a.NoUpgrade || !b.NoUpgrade || !s.NoUpgrade {
					t.Fatalf("no-upgrade: %v %v %v", a.NoUpgrade, b.NoUpgrade, s.NoUpgrade)
				}
			}},
		{"quality profile", library.BulkProfile, profileID,
			[]library.Ref{{Kind: "movie", ID: m1.ID}, {Kind: "movie", ID: m2.ID}}, 0,
			func(t *testing.T) {
				a, _ := repo.Get(m1.ID)
				if a.ProfileID != profileID {
					t.Fatalf("profile = %d", a.ProfileID)
				}
			}},
		{"back to the default profile", library.BulkProfile, int64(0),
			[]library.Ref{{Kind: "movie", ID: m1.ID}}, 0,
			func(t *testing.T) {
				if a, _ := repo.Get(m1.ID); a.ProfileID != 0 {
					t.Fatalf("profile = %d", a.ProfileID)
				}
			}},
		{"download from torrents", library.BulkSourcePref, "torrent",
			[]library.Ref{{Kind: "movie", ID: m1.ID}, {Kind: "tv", ID: show.ID}, {Kind: "tv", ID: 4242}}, 1,
			func(t *testing.T) {
				a, _ := repo.Get(m1.ID)
				s, _ := repo.GetSeries(show.ID)
				if a.SourcePref != "torrent" || s.SourcePref != "torrent" {
					t.Fatalf("sources: %q %q", a.SourcePref, s.SourcePref)
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			missing, err := repo.BulkSet(tc.field, tc.value, tc.refs)
			if err != nil {
				t.Fatalf("bulk set: %v", err)
			}
			if len(missing) != tc.missing {
				t.Fatalf("missing = %v, want %d", missing, tc.missing)
			}
			tc.check(t)
		})
	}
}

// One bad reference undoes the whole change: nothing is half-applied.
func TestBulkSetIsAllOrNothing(t *testing.T) {
	repo := newRepo(t)
	m, _ := repo.Add(library.Movie{TMDBID: 1, Title: "One", Monitored: true})
	_, err := repo.BulkSet(library.BulkMonitored, false, []library.Ref{{Kind: "movie", ID: m.ID}, {Kind: "album", ID: 1}})
	if err == nil {
		t.Fatal("an unknown kind should fail")
	}
	if got, _ := repo.Get(m.ID); !got.Monitored {
		t.Fatal("the change must be rolled back when it fails part way")
	}
	if _, err := repo.BulkSet(library.BulkField(99), true, []library.Ref{{Kind: "movie", ID: m.ID}}); err == nil {
		t.Fatal("an unknown setting should fail")
	}
}
