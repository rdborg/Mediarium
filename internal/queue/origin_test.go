package queue_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/origin"
	"github.com/rdborg/mediarium/internal/queue"
)

// A download keeps who or what started it, and Get and List give it back. One
// without an origin (an old row) comes back empty.
func TestEnqueueKeepsOrigin(t *testing.T) {
	db := openDB(t)
	repo := queue.NewRepo(db)
	lib := library.NewRepo(db)
	m, err := lib.Add(library.Movie{TMDBID: 7, Title: "Film"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		in   origin.Origin
		want origin.Origin
	}{
		{"a person", origin.Origin{Kind: origin.Manual, UserID: 3, User: "Anna"}, origin.Origin{Kind: origin.Manual, UserID: 3, User: "Anna"}},
		{"a list", origin.Origin{Kind: origin.List, Name: "Trending"}, origin.Origin{Kind: origin.List, Name: "Trending"}},
		{"a scheduled job", origin.Origin{Kind: origin.Auto, Name: origin.JobRSS}, origin.Origin{Kind: origin.Auto, Name: origin.JobRSS}},
		{"nothing recorded", origin.Origin{}, origin.Origin{}},
		{"a kind nobody knows is not stored", origin.Origin{Kind: "teleport"}, origin.Origin{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, err := repo.Enqueue(queue.Item{MovieID: m.ID, ReleaseTitle: "Film." + c.name, Origin: c.in})
			if err != nil {
				t.Fatal(err)
			}
			got, err := repo.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if got.Origin != c.want {
				t.Fatalf("Get origin = %+v, want %+v", got.Origin, c.want)
			}
			all, err := repo.List()
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, it := range all {
				if it.ID == id {
					found = true
					if it.Origin != c.want {
						t.Fatalf("List origin = %+v, want %+v", it.Origin, c.want)
					}
				}
			}
			if !found {
				t.Fatal("the download is missing from the list")
			}
		})
	}
}
