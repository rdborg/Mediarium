package api

import (
	"errors"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

// Two uploads of one name: the failed one is left out of searches and grabs,
// the other is still offered (GitHub #195).
func TestFailedUploadDoesNotBlockTheSameNameElsewhere(t *testing.T) {
	s := newAuditServer(t)
	m, err := s.MovieRepo.Add(library.Movie{TMDBID: 1, Title: "Fixture Movie", Year: 2001, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	const name = "Fixture.Movie.2001.1080p.BluRay.x264-GRP"
	bad := indexers.Result{Title: name, IndexerID: 7, SizeBytes: 8000, DownloadURL: "http://x/bad"}
	good := indexers.Result{Title: name, IndexerID: 7, SizeBytes: 9000, DownloadURL: "http://x/good"}
	if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: name, MovieID: m.ID, Reason: "corrupt", ReleaseID: resultReleaseID(bad)}); err != nil {
		t.Fatal(err)
	}
	sc := blocklist.Scope{MovieID: m.ID}
	got := dropBlocked([]indexers.Result{bad, good}, s.searchSkipKeys(), sc)
	if len(got) != 1 || got[0].SizeBytes != 9000 {
		t.Fatalf("kept %+v, want only the other upload", got)
	}
	if !releaseBlocked(s.searchSkipKeys(), bad, sc) || releaseBlocked(s.searchSkipKeys(), good, sc) {
		t.Error("releaseBlocked disagrees with dropBlocked")
	}
	// The check at grab time follows the same rule.
	if !s.releaseBlocklisted(name, resultReleaseID(bad), sc) || s.releaseBlocklisted(name, resultReleaseID(good), sc) {
		t.Error("the grab-time check blocks the wrong upload")
	}

	// An entry from before, with no upload, still blocks both.
	const old = "Fixture.Movie.2001.2160p.WEB-DL.x265-OLD"
	if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: old, MovieID: m.ID, Reason: "corrupt"}); err != nil {
		t.Fatal(err)
	}
	a := indexers.Result{Title: old, IndexerID: 7, SizeBytes: 1}
	b := indexers.Result{Title: old, IndexerID: 7, SizeBytes: 2}
	if got := dropBlocked([]indexers.Result{a, b}, s.searchSkipKeys(), sc); len(got) != 0 {
		t.Fatalf("an entry with no upload let %+v through", got)
	}
}

func TestNewestOfTheSameNameIsPreferred(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 11, n, 12, 0, 0, 0, time.UTC) }
	const name = "Fixture.Movie.2001.1080p.BluRay.x264-GRP"
	old := indexers.Result{Title: name, IndexerName: "A", PublishDate: day(1), SizeBytes: 9000}
	fixed := indexers.Result{Title: name, IndexerName: "A", PublishDate: day(5), SizeBytes: 8000}
	tests := []struct {
		name string
		a, b indexers.Result
		want string // "a", "b" or "" for no preference
	}{
		{"later post wins on one indexer", old, fixed, "b"},
		{"order does not matter", fixed, old, "a"},
		{"title case does not matter", old, indexers.Result{Title: "fixture.movie.2001.1080p.bluray.x264-grp", IndexerName: "A", PublishDate: day(5)}, "b"},
		{"another name is not a repost", old, indexers.Result{Title: name + "2", IndexerName: "A", PublishDate: day(5)}, ""},
		{"no date, no preference", old, indexers.Result{Title: name, IndexerName: "A"}, ""},
		{"minutes apart on two indexers is the same upload", old, indexers.Result{Title: name, IndexerName: "B", PublishDate: day(1).Add(20 * time.Minute)}, ""},
		{"days apart on two indexers is a repost", old, indexers.Result{Title: name, IndexerName: "B", PublishDate: day(3)}, "b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := tt.a, tt.b
			got := ""
			switch newerSameName(&a, &b) {
			case &a:
				got = "a"
			case &b:
				got = "b"
			}
			if got != tt.want {
				t.Errorf("newer = %q, want %q", got, tt.want)
			}
		})
	}

	// The automatic pick takes the newest upload, even when the older one is
	// from a better indexer.
	if !preferOver(&old, &fixed) || preferOver(&fixed, &old) {
		t.Error("preferOver does not take the newest upload")
	}
	hi := fixed
	hi.IndexerName = "B"
	hi.Priority = 50
	lo := old
	lo.Priority = 1
	if !preferOver(&lo, &hi) {
		t.Error("the repost on a worse indexer was not preferred")
	}
}

// Two uploads with the same name, from the same indexer and of the same size,
// only told apart by their guid: the one that failed is blocked after the
// download is queued and fails, the copy is still grabbed (GitHub #195).
func TestFailedDownloadBlocksOnlyItsGUID(t *testing.T) {
	s := newAuditServer(t)
	m, err := s.MovieRepo.Add(library.Movie{TMDBID: 1, Title: "Fixture Movie", Year: 2001, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	inst, err := s.IndexerRepo.Create(indexers.Instance{Name: "Fixture", BaseURL: "https://idx.example/api", Protocol: indexers.ProtocolUsenet, Kind: indexers.KindNewznab, Enabled: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	const name = "Fixture.Movie.2001.1080p.BluRay.x264-GRP"
	const badURL, goodURL, noGUIDURL = "https://idx.example/getnzb/bad", "https://idx.example/getnzb/good", "https://idx.example/getnzb/none"
	s.IndexerRepo.Tracker().RecordSearch(inst.ID, []indexers.Result{
		{Title: name, IndexerID: inst.ID, SizeBytes: 8000, GUID: "guid-bad", DownloadURL: badURL},
		{Title: name, IndexerID: inst.ID, SizeBytes: 8000, GUID: "guid-good", DownloadURL: goodURL},
		{Title: name, IndexerID: inst.ID, SizeBytes: 8000, DownloadURL: noGUIDURL},
	}, nil)

	id, err := s.QueueRepo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: name, NZBURL: badURL, SizeBytes: 8000, Protocol: queue.ProtocolUsenet})
	if err != nil {
		t.Fatal(err)
	}
	item, err := s.QueueRepo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.ReleaseGUID != "guid-bad" {
		t.Fatalf("queue row kept guid %q, want guid-bad", item.ReleaseGUID)
	}
	if !s.blocklistBadRelease(id, name, indexers.ProtocolUsenet, m.ID, 0, errors.New("corrupt")) {
		t.Fatal("the failed release was not blocklisted")
	}

	sc := blocklist.Scope{MovieID: m.ID}
	tests := []struct {
		name string
		url  string
		want bool // refused
	}{
		{"the upload that failed", badURL, true},
		{"same name and size, other guid", goodURL, false},
		{"same name and size, no guid known", noGUIDURL, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s.grabMu.Lock()
			err := s.grabRefused(grabAutomatic, name, tt.url, 8000, sc)
			s.grabMu.Unlock()
			if got := errors.Is(err, errBlocklisted); got != tt.want {
				t.Errorf("grabRefused = %v, want refused %v", err, tt.want)
			}
		})
	}
	// Search results are filtered the same way.
	results := []indexers.Result{
		{Title: name, IndexerID: inst.ID, SizeBytes: 8000, GUID: "guid-bad", DownloadURL: badURL},
		{Title: name, IndexerID: inst.ID, SizeBytes: 8000, GUID: "guid-good", DownloadURL: goodURL},
	}
	if got := dropBlocked(results, s.searchSkipKeys(), sc); len(got) != 1 || got[0].GUID != "guid-good" {
		t.Fatalf("kept %+v, want only the other guid", got)
	}
}
