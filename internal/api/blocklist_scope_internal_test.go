package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/quality"
)

// A release blocked for one title is left out of that title's searches and
// grabs, and stays available to every other title.
func TestScopedBlocklistEntryOnlyAffectsItsTitle(t *testing.T) {
	s := newAuditServer(t)
	one, err := s.MovieRepo.Add(library.Movie{TMDBID: 1, Title: "Fixture Movie", Year: 2001, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.MovieRepo.Add(library.Movie{TMDBID: 2, Title: "Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	const wrong = "Fixture.Movie.1999.1080p.BluRay.x264-GRP"
	const dud = "Fixture.Movie.2001.1080p.BluRay.x264-DUD"
	if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: wrong, MovieID: one.ID, ItemOnly: true, Reason: "wrong year"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: dud, MovieID: one.ID, Reason: "corrupt"}); err != nil {
		t.Fatal(err)
	}

	results := []indexers.Result{{Title: wrong}, {Title: dud}, {Title: "Fixture.Movie.2001.2160p.WEB-DL.x265-OK"}}
	blocked := s.searchSkipKeys()
	titles := func(rs []indexers.Result) []string {
		var out []string
		for _, r := range rs {
			out = append(out, r.Title)
		}
		return out
	}
	tests := []struct {
		name  string
		scope blocklist.Scope
		want  int
	}{
		{"the movie it was blocked for loses both", blocklist.Scope{MovieID: one.ID}, 1},
		{"another movie loses only the global one", blocklist.Scope{MovieID: two.ID}, 2},
		{"a search for no title loses only the global one", blocklist.Scope{}, 2},
		{"a series with the same id number is not affected", blocklist.Scope{SeriesID: one.ID}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dropBlocked(results, blocked, tt.scope); len(got) != tt.want {
				t.Fatalf("kept %v, want %d of them", titles(got), tt.want)
			}
		})
	}

	// The check at grab time follows the same rules.
	if err := s.grabRefused(grabAutomatic, wrong, "http://x/1", blocklist.Scope{MovieID: one.ID}); err == nil {
		t.Fatal("an automatic grab of the blocked release for its own movie is refused")
	}
	if err := s.grabRefused(grabAutomatic, wrong, "http://x/1", blocklist.Scope{MovieID: two.ID}); err != nil {
		t.Fatalf("the other movie may take it: %v", err)
	}
	if err := s.grabRefused(grabAutomatic, dud, "http://x/2", blocklist.Scope{MovieID: two.ID}); err == nil {
		t.Fatal("a release blocked everywhere is refused for every movie")
	}
	if err := s.grabRefused(grabPicked, wrong, "http://x/1", blocklist.Scope{MovieID: one.ID}); err != nil {
		t.Fatalf("a person can still pick it by hand: %v", err)
	}

	// The search note counts it as blocklisted for the right movie only.
	keep := func(indexers.Result) (bool, string) { return true, "" }
	plain := quality.Profile{}
	forOne := judgeSearch(results[:1], blocked, blocklist.Scope{MovieID: one.ID}, sourcesBoth, plain, keep, "", "")
	forTwo := judgeSearch(results[:1], blocked, blocklist.Scope{MovieID: two.ID}, sourcesBoth, plain, keep, "", "")
	if forOne.reasons["blocklisted"] != 1 || forTwo.reasons["blocklisted"] != 0 {
		t.Fatalf("blocklisted for movie one only: one %+v, two %+v", forOne, forTwo)
	}
}

func TestScopedBlocklistEntryForAnAlbum(t *testing.T) {
	s := newAuditServer(t)
	_, albums, err := s.MusicRepo.AddArtist(music.Artist{MBID: "mb-1", Name: "Artist", Monitored: true}, []music.Album{
		{MBID: "rg-1", Title: "Album", Monitored: true}, {MBID: "rg-2", Title: "Other", Monitored: true},
	})
	if err != nil || len(albums) != 2 {
		t.Fatalf("add artist: %v", err)
	}
	a, b := albums[0].ID, albums[1].ID
	if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: "Artist - Album (2010) [FLAC]", AlbumID: a, ItemOnly: true}); err != nil {
		t.Fatal(err)
	}
	blocked := s.searchSkipKeys()
	res := []indexers.Result{{Title: "Artist - Album (2010) [FLAC]"}}
	if got := dropBlocked(res, blocked, blocklist.Scope{AlbumID: a}); len(got) != 0 {
		t.Fatal("blocked for that album")
	}
	if got := dropBlocked(res, blocked, blocklist.Scope{AlbumID: b}); len(got) != 1 {
		t.Fatal("another album may still take it")
	}
}
