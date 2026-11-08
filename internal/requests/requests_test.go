package requests_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/requests"
	"github.com/rdborg/mediarium/internal/store"
)

func newRepo(t *testing.T) (*requests.Repo, *sql.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (7, 'sam', 'x'), (8, 'kim', 'x')`); err != nil {
		t.Fatal(err)
	}
	return requests.NewRepo(db), db
}

func TestCountSinceCountsWaitingAndApprovedRequests(t *testing.T) {
	repo, db := newRepo(t)
	now := time.Now().UTC()
	ask := func(by int64, title string, ago time.Duration) requests.Request {
		r, err := repo.Create(requests.Request{Kind: "movie", Title: title, Payload: `{}`, RequestedBy: by})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE requests SET created_at = ? WHERE id = ?`, now.Add(-ago).Format(time.RFC3339), r.ID); err != nil {
			t.Fatal(err)
		}
		return r
	}
	ask(7, "A", time.Hour)
	approved := ask(7, "B", 3*24*time.Hour)
	declined := ask(7, "C", 2*time.Hour)
	ask(7, "Old", 10*24*time.Hour)
	ask(8, "Someone else's", time.Hour)
	if err := repo.Decide(declined.ID, requests.Declined, "no", 0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Decide(approved.ID, requests.Approved, "", 0); err != nil {
		t.Fatal(err)
	}

	n, oldest, err := repo.CountSince(7, now.Add(-7*24*time.Hour).Format(time.RFC3339))
	if err != nil || n != 2 {
		t.Fatalf("week: %d, %v (waiting and approved count; declined and old ones don't)", n, err)
	}
	if want := now.Add(-3 * 24 * time.Hour).Format(time.RFC3339); oldest != want {
		t.Fatalf("oldest %q, want %q", oldest, want)
	}
	if n, _, _ = repo.CountSince(7, now.Add(-24*time.Hour).Format(time.RFC3339)); n != 1 {
		t.Fatalf("day: %d", n)
	}
	if n, oldest, _ = repo.CountSince(9, now.Add(-24*time.Hour).Format(time.RFC3339)); n != 0 || oldest != "" {
		t.Fatalf("nobody: %d %q", n, oldest)
	}
}

func TestUnannouncedAndFourK(t *testing.T) {
	repo, _ := newRepo(t)
	r, err := repo.Create(requests.Request{Kind: "movie", Title: "Dune", Payload: `{"tmdbId":438631,"fourK":true}`, RequestedBy: 7, FourK: true})
	if err != nil || !r.FourK || r.AvailableAt != "" {
		t.Fatalf("created: %+v, %v", r, err)
	}
	if got, _ := repo.Unannounced(); len(got) != 0 {
		t.Fatalf("a waiting request is not approved yet: %v", got)
	}
	if err := repo.Decide(r.ID, requests.Approved, "", 0); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Unannounced()
	if err != nil || len(got) != 1 || got[0].ID != r.ID || got[0].Requester != "sam" {
		t.Fatalf("unannounced: %+v, %v", got, err)
	}
	if err := repo.SetAvailable(r.ID, "2026-10-09T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if got, _ = repo.Unannounced(); len(got) != 0 {
		t.Fatalf("announced already: %v", got)
	}
	back, _ := repo.Get(r.ID)
	if back.AvailableAt != "2026-10-09T10:00:00Z" {
		t.Fatalf("available at %q", back.AvailableAt)
	}
}

// A removal request keeps its reason, reads back as a removal, and never
// counts against the quota or gets a "ready to watch" message.
func TestRemovalRequestsAreNotAdds(t *testing.T) {
	repo, _ := newRepo(t)
	old, err := repo.Create(requests.Request{Kind: "movie", Title: "Old", Payload: `{}`, RequestedBy: 7})
	if err != nil || old.Action != requests.ActionAdd {
		t.Fatalf("a request without an action is an add: %+v, %v", old, err)
	}
	rm, err := repo.Create(requests.Request{Kind: "movie", Title: "Dune", Payload: `{"libraryId":4,"tmdbId":438631,"reason":"Nobody watches it"}`, RequestedBy: 7, Action: requests.ActionRemove})
	if err != nil || rm.Action != requests.ActionRemove || rm.Reason != "Nobody watches it" {
		t.Fatalf("removal: %+v, %v", rm, err)
	}
	if n, _, _ := repo.CountSince(7, "2000-01-01T00:00:00Z"); n != 1 {
		t.Fatalf("only the add counts for the quota, got %d", n)
	}
	if err := repo.Decide(rm.ID, requests.Approved, "", 0); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.Unannounced(); len(got) != 0 {
		t.Fatalf("an approved removal is not announced as ready: %+v", got)
	}
	if list, _ := repo.List(0); len(list) != 2 {
		t.Fatalf("list: %+v", list)
	}
}
