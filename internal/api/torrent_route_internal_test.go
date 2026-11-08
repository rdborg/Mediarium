package api

import (
	"database/sql"
	"os"
	"testing"

	"github.com/rdborg/mediarium/internal/debrid"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
)

// An indexer can send its torrents to the download client, the debrid service
// or the built-in client; while that one isn't set up, the usual order holds
// (#166, jasonmackie76).
func TestTorrentRouteFollowsTheIndexer(t *testing.T) {
	s := newBareServer(t)
	set := func(k, v string) {
		t.Helper()
		if err := s.Settings.Set(k, v, false); err != nil {
			t.Fatal(err)
		}
	}
	add := func(name, route string) sql.NullInt64 {
		t.Helper()
		inst, err := s.IndexerRepo.Create(indexers.Instance{Name: name, BaseURL: "https://" + name + ".test", Protocol: indexers.ProtocolTorrent, Enabled: true, TorrentRoute: route}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return sql.NullInt64{Int64: inst.ID, Valid: true}
	}
	private := add("private", indexers.RouteClient)
	public := add("public", indexers.RouteDebrid)
	plain := add("plain", "")

	// Nothing set up but the built-in client: everything goes there.
	for _, id := range []sql.NullInt64{private, public, plain, {}} {
		if got := s.torrentRouteFor(id); got != indexers.RouteBuiltin {
			t.Errorf("with nothing set up, indexer %v goes to %q", id, got)
		}
	}

	set(settings.KeyDownloadClientKind, "qbittorrent")
	set(settings.KeyDownloadClientURL, "http://127.0.0.1:1")
	set(settings.KeyDebridService, debrid.RealDebrid)
	set(settings.KeyDebridAPIKey, "k")
	cases := []struct {
		name string
		id   sql.NullInt64
		want string
	}{
		{"private tracker to the client", private, indexers.RouteClient},
		{"public indexer to debrid", public, indexers.RouteDebrid},
		{"no choice: the usual order (client first)", plain, indexers.RouteClient},
		{"no indexer known", sql.NullInt64{}, indexers.RouteClient},
	}
	for _, c := range cases {
		if got := s.torrentRouteFor(c.id); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}

	// A download that started keeps the route it took, whatever changes later.
	m, err := s.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "Fixture", Year: 1999})
	if err != nil {
		t.Fatal(err)
	}
	qid, err := s.QueueRepo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "X", NZBURL: "magnet:?xt=urn:btih:abc", Protocol: queue.ProtocolTorrent, IndexerID: public})
	if err != nil {
		t.Fatal(err)
	}
	q, err := s.QueueRepo.Get(qid)
	if err != nil {
		t.Fatal(err)
	}
	dir := s.defaultWorkDirFor(q.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTorrentRoute(dir, indexers.RouteDebrid)
	set(settings.KeyDebridAPIKey, "")
	if got := s.itemTorrentRoute(q); got != indexers.RouteDebrid {
		t.Errorf("a started download moved from debrid to %q", got)
	}
	if got := s.torrentRouteFor(public); got != indexers.RouteClient {
		t.Errorf("with debrid gone, the public indexer should fall back to the client, got %q", got)
	}
}

// A torrent the debrid service has cached only gets past the minimum seeders
// when it could go to debrid.
func TestCachedExemptionOnlyForDebridRoutes(t *testing.T) {
	s := newBareServer(t)
	_ = s.Settings.Set(settings.KeyDownloadClientKind, "qbittorrent", false)
	_ = s.Settings.Set(settings.KeyDownloadClientURL, "http://127.0.0.1:1", false)
	if s.mayGoToDebrid(indexers.Result{TorrentRoute: indexers.RouteClient}) {
		t.Error("an indexer sending torrents to the client should not count as debrid")
	}
	if !s.mayGoToDebrid(indexers.Result{}) || !s.mayGoToDebrid(indexers.Result{TorrentRoute: indexers.RouteDebrid}) {
		t.Error("the usual order and a debrid choice may go to debrid")
	}
}
