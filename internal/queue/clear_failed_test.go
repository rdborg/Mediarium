package queue_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/queue"
)

func TestFailedTriesAreClearedOnceTheTitleIsDownloaded(t *testing.T) {
	db := openDB(t)
	for _, q := range []string{
		`INSERT INTO movies (id, tmdb_id, title, year, status) VALUES (1, 101, 'A', 2000, 'missing'), (2, 102, 'B', 2001, 'missing')`,
		`INSERT INTO series (id, tmdb_id, title) VALUES (7, 107, 'Show')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	repo := queue.NewRepo(db)
	add := func(it queue.Item, st queue.Status) int64 {
		t.Helper()
		id, err := repo.Enqueue(it)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.SetStatus(id, st, ""); err != nil {
			t.Fatal(err)
		}
		return id
	}
	failedOld := add(queue.Item{MovieID: 1, ReleaseTitle: "Old.Bad.Release"}, queue.StatusFailed)
	otherMovie := add(queue.Item{MovieID: 2, ReleaseTitle: "Other.Movie"}, queue.StatusFailed)
	good := add(queue.Item{MovieID: 1, ReleaseTitle: "Good.Release"}, queue.StatusCompleted)
	epFailed := add(queue.Item{SeriesID: 7, Season: 1, Episode: 2, ReleaseTitle: "Show.S01E02.Bad"}, queue.StatusFailed)
	epOther := add(queue.Item{SeriesID: 7, Season: 1, Episode: 3, ReleaseTitle: "Show.S01E03.Bad"}, queue.StatusFailed)

	if n, err := repo.ClearFailedForMovie(1, good); err != nil || n != 1 {
		t.Fatalf("movie: cleared %d (err %v)", n, err)
	}
	if n, err := repo.ClearFailedForEpisodes(7, 1, []int{2}, 0); err != nil || n != 1 {
		t.Fatalf("episodes: cleared %d (err %v)", n, err)
	}
	for id, want := range map[int64]bool{failedOld: false, otherMovie: true, good: true, epFailed: false, epOther: true} {
		_, err := repo.Get(id)
		if (err == nil) != want {
			t.Errorf("item %d: present = %v, want %v", id, err == nil, want)
		}
	}
}

func TestOldFailuresSettledByALaterDownloadAreCleared(t *testing.T) {
	db := openDB(t)
	if _, err := db.Exec(`INSERT INTO movies (id, tmdb_id, title, year, status) VALUES (1, 101, 'A', 2000, 'downloaded'), (2, 102, 'B', 2001, 'missing')`); err != nil {
		t.Fatal(err)
	}
	ins := func(movie int, status, at string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO download_queue (movie_id, release_title, status, completed_at) VALUES (?, 'r', ?, ?)`, movie, status, at); err != nil {
			t.Fatal(err)
		}
	}
	ins(1, "failed", "2026-10-01T10:00:00Z")    // settled by the later download below
	ins(1, "completed", "2026-10-01T12:00:00Z") //
	ins(1, "failed", "2026-10-02T10:00:00Z")    // a newer failure (an upgrade that failed): kept
	ins(2, "failed", "2026-10-01T10:00:00Z")    // nothing settled it: kept
	n, err := queue.NewRepo(db).ClearSettledFailures()
	if err != nil || n != 1 {
		t.Fatalf("cleared %d (err %v), want 1", n, err)
	}
}
