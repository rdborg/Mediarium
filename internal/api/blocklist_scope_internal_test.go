package api

import (
	"errors"
	"fmt"
	"testing"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/origin"
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
	if err := s.grabRefused(grabAutomatic, wrong, "http://x/1", 0, blocklist.Scope{MovieID: one.ID}); err == nil {
		t.Fatal("an automatic grab of the blocked release for its own movie is refused")
	}
	if err := s.grabRefused(grabAutomatic, wrong, "http://x/1", 0, blocklist.Scope{MovieID: two.ID}); err != nil {
		t.Fatalf("the other movie may take it: %v", err)
	}
	if err := s.grabRefused(grabAutomatic, dud, "http://x/2", 0, blocklist.Scope{MovieID: two.ID}); err == nil {
		t.Fatal("a release blocked everywhere is refused for every movie")
	}
	if err := s.grabRefused(grabPicked, wrong, "http://x/1", 0, blocklist.Scope{MovieID: one.ID}); err != nil {
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

// An automatic grab for an album checks the blocklist when it is made, like
// one for a movie or a show: a release blocked for that album, or everywhere,
// is refused; another album may still take a title-only one, and a person
// picking the release by hand is never refused.
func TestAlbumGrabChecksTheBlocklist(t *testing.T) {
	s := newAuditServer(t)
	s.dispatch.Stop() // nothing is downloaded: only the check at grab time is under test
	artist, albums, err := s.MusicRepo.AddArtist(music.Artist{MBID: "mb-1", Name: "Artist", Monitored: true}, []music.Album{
		{MBID: "rg-1", Title: "Album", Monitored: true}, {MBID: "rg-2", Title: "Other", Monitored: true},
	})
	if err != nil || len(albums) != 2 {
		t.Fatalf("add artist: %v", err)
	}
	const onlyHere = "Artist - Album (2010) [FLAC]"
	const everywhere = "Artist - Album (2010) [MP3 320]"
	if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: onlyHere, AlbumID: albums[0].ID, ItemOnly: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: everywhere, AlbumID: albums[0].ID}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		album   music.Album
		release string
		kind    grabKind
		refused bool
	}{
		{"blocked for this album, automatic", albums[0], onlyHere, grabAutomatic, true},
		{"blocked everywhere, automatic", albums[1], everywhere, grabAutomatic, true},
		{"blocked for another album, automatic", albums[1], onlyHere, grabAutomatic, false},
		{"blocked for this album, picked by hand", albums[0], onlyHere, grabPicked, false},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := s.grabAlbum(artist, tt.album, tt.release, fmt.Sprintf("http://indexer.invalid/%d", i), 1, indexers.ProtocolUsenet, tt.kind, origin.Origin{})
			if refused := errors.Is(err, errBlocklisted); refused != tt.refused {
				t.Fatalf("refused = %v (err %v), want %v", refused, err, tt.refused)
			}
			if !tt.refused {
				if err != nil {
					t.Fatalf("grab: %v", err)
				}
				// One download per album: clear it for the next case.
				if err := s.QueueRepo.Delete(id); err != nil {
					t.Fatal(err)
				}
				if err := s.MusicRepo.SetAlbumStatus(tt.album.ID, music.StatusMissing); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// A release that failed for an album is tied to that album, so it can later
// be narrowed to "only for this title" without a 409.
func TestAutoBlocklistedAlbumReleaseCanBeNarrowed(t *testing.T) {
	s := newAuditServer(t)
	_, albums, err := s.MusicRepo.AddArtist(music.Artist{MBID: "mb-2", Name: "Artist", Monitored: true}, []music.Album{
		{MBID: "rg-3", Title: "Album", Monitored: true},
	})
	if err != nil || len(albums) != 1 {
		t.Fatalf("add artist: %v", err)
	}
	const rel = "Artist - Album (2010) [FLAC]"
	if !s.blocklistAlbumRelease(0, albums[0].ID, rel, indexers.ProtocolUsenet, errors.New("bad archive")) {
		t.Fatal("not blocklisted")
	}
	list, err := s.Blocklist.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v, %v", list, err)
	}
	if list[0].AlbumID != albums[0].ID {
		t.Fatalf("entry not tied to the album: %+v", list[0])
	}
	if err := s.Blocklist.SetScope(list[0].ID, true); err != nil {
		t.Fatalf("narrow: %v", err)
	}
}
