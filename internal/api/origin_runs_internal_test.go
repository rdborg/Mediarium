package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/music"
	"github.com/rdborg/mediarium/internal/origin"
	"github.com/rdborg/mediarium/internal/settings"
)

// newOriginIndexer is a Newznab indexer with one release for each kind of
// title, picked by the categories it is asked for.
func newOriginIndexer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "application/xml")
		if q.Get("t") == "caps" {
			fmt.Fprint(w, `<?xml version="1.0"?><caps><searching><search available="yes"/><tv-search available="yes"/><movie-search available="yes"/><audio-search available="yes"/><book-search available="yes"/></searching></caps>`)
			return
		}
		title, cat := "Fixture.Movie.1999.1080p.WEB-DL.x264-GRP", "2040"
		switch c := q.Get("cat"); {
		case strings.HasPrefix(c, "3"):
			title, cat = "Fixture Band - Fixture Album (2001) [MP3 320]", "3040"
		case strings.HasPrefix(c, "7"):
			title, cat = "Fixture Writer - A Fixture Book (2001) (epub)", "7020"
		case strings.HasPrefix(c, "5"):
			title, cat = "Fixture.Show.S01E01.1080p.WEB-DL.x264-GRP", "5040"
		}
		fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel><item><title>%s</title><guid>g-%s</guid><pubDate>%s</pubDate><enclosure url="http://%s/nzb/%s.nzb" length="2147483648" type="application/x-nzb" /><newznab:attr name="size" value="2147483648"/><newznab:attr name="category" value="%s"/></item></channel></rss>`,
			title, cat, time.Now().UTC().Add(-time.Hour).Format(time.RFC1123Z), r.Host, cat, cat)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Each search run credits what it grabs to whoever is behind that run, even
// while other runs for the same title are going on: an import list's search
// and a person's Search now that overlap each keep their own, and the
// scheduled search that grabs something meanwhile is credited to itself, not
// to the person or list whose run happens to be open. Movies, shows, albums
// and books alike.
func TestEachSearchRunCreditsItsOwnGrabs(t *testing.T) {
	s := newAuditServer(t)
	s.dispatch.Stop() // nothing is downloaded: only what the download line records
	for _, k := range []string{settings.KeyModulesMusic, settings.KeyModulesEbooks} {
		if err := s.Settings.Set(k, "1", false); err != nil {
			t.Fatal(err)
		}
	}
	idx := newOriginIndexer(t)
	if _, err := s.IndexerRepo.Create(indexers.Instance{Name: "Fixture", BaseURL: idx.URL, APIKey: "k", Protocol: indexers.ProtocolUsenet, Kind: indexers.KindNewznab, Enabled: true}, nil); err != nil {
		t.Fatal(err)
	}
	instances, err := s.searchableIndexers()
	if err != nil || len(instances) != 1 {
		t.Fatalf("indexers: %v %v", instances, err)
	}
	profiles, err := s.loadProfiles()
	if err != nil {
		t.Fatal(err)
	}

	movie, err := s.MovieRepo.Add(library.Movie{TMDBID: 1001, Title: "Fixture Movie", Year: 1999, Status: library.StatusMissing, Monitored: true, ReleaseDate: "1999-06-01"})
	if err != nil {
		t.Fatal(err)
	}
	show, err := s.MovieRepo.AddSeries(library.Series{TMDBID: 1002, Title: "Fixture Show", Year: 2020, Monitored: true},
		[]library.Episode{{Season: 1, Episode: 1, AirDate: "2020-01-01", Status: library.StatusMissing, Monitored: true}})
	if err != nil {
		t.Fatal(err)
	}
	artist, albums, err := s.MusicRepo.AddArtist(music.Artist{MBID: "mb-1", Name: "Fixture Band", Monitored: true}, []music.Album{{MBID: "rg-1", Title: "Fixture Album", Type: "album", ReleaseDate: "2001-01-01", Monitored: true}})
	if err != nil || len(albums) != 1 {
		t.Fatalf("add artist: %v", err)
	}
	album := albums[0]
	book, err := s.BookRepo.Add(books.Book{OLKey: "OL1W", Title: "A Fixture Book", Author: "Fixture Writer", Year: 2001, WantEbook: true})
	if err != nil {
		t.Fatal(err)
	}

	listA := origin.Origin{Kind: origin.List, Name: "Picks", ListID: 1}
	listB := origin.Origin{Kind: origin.List, Name: "Picks", ListID: 2} // the same name, another list
	anna := origin.Origin{Kind: origin.Search, UserID: 3, User: "Anna"}
	scheduled := origin.Origin{Kind: origin.Auto, Name: origin.JobSearch}
	// The runs are all open at once, as when they overlap.
	runA := withGrabBy(context.Background(), listA)
	runB := withGrabBy(context.Background(), listB)
	runAnna := withGrabBy(context.Background(), anna)
	schedule := context.Background()

	type run struct {
		name  string
		ctx   context.Context
		force bool // a person's Search now
		want  origin.Origin
	}
	runs := []run{
		{"list A's search", runA, false, listA},
		{"Anna's Search now while list A's runs", runAnna, true, anna},
		{"list B, with the same name, at the same time", runB, false, listB},
		{"the scheduled search while Anna's runs", schedule, false, scheduled},
	}
	media := []struct {
		name  string
		hunt  func(ctx context.Context, force bool) bool
		reset func()
	}{
		{"movie", func(ctx context.Context, force bool) bool {
			m, err := s.MovieRepo.Get(movie.ID)
			if err != nil {
				t.Fatal(err)
			}
			return s.huntMovie(ctx, m, instances, profiles, map[string]bool{}, force)
		}, func() { _ = s.MovieRepo.SetStatus(movie.ID, library.StatusMissing, "", "") }},
		{"show", func(ctx context.Context, force bool) bool {
			release := indexers.Result{Title: "Fixture.Show.S01E01.1080p.WEB-DL.x264-GRP", DownloadURL: "http://x.invalid/show.nzb", SizeBytes: 2 << 30, Protocol: indexers.ProtocolUsenet, PublishDate: time.Now().Add(-time.Hour)}
			searcher := func(library.Series, int, int) []indexers.Result { return []indexers.Result{release} }
			return s.autoGrabTV(searcher, "test", profiles, tvScope{seriesID: show.ID, force: force, by: grabByFrom(ctx)}) > 0
		}, func() {
			eps, _ := s.MovieRepo.ListEpisodes(show.ID)
			for _, e := range eps {
				_ = s.MovieRepo.SetEpisodeStatus(e.ID, library.StatusMissing, "", "")
			}
		}},
		{"album", func(ctx context.Context, force bool) bool {
			a, err := s.MusicRepo.GetAlbum(album.ID)
			if err != nil {
				t.Fatal(err)
			}
			profile, err := s.albumProfile(artist)
			if err != nil {
				t.Fatal(err)
			}
			return s.huntAlbum(ctx, artist, a, profile, instances, map[string]bool{}, force)
		}, func() { _ = s.MusicRepo.SetAlbumStatus(album.ID, music.StatusMissing) }},
		{"book", func(ctx context.Context, force bool) bool {
			return s.searchBook(book.ID, books.Ebook, false, grabByFrom(ctx))
		}, func() { _ = s.BookRepo.SetState(book.ID, books.Ebook, books.StatusMissing, "", "") }},
	}
	for _, m := range media {
		for _, r := range runs {
			t.Run(m.name+": "+r.name, func(t *testing.T) {
				if !m.hunt(r.ctx, r.force) {
					t.Fatal("nothing was grabbed")
				}
				items, err := s.QueueRepo.List()
				if err != nil || len(items) != 1 {
					t.Fatalf("download line: %d items, %v", len(items), err)
				}
				if got := items[0].Origin; got != r.want {
					t.Errorf("credited to %+v, want %+v", got, r.want)
				}
				// One download at a time per title: clear it for the next run.
				if err := s.QueueRepo.Delete(items[0].ID); err != nil {
					t.Fatal(err)
				}
				m.reset()
			})
		}
	}
}
