package books_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/origin"
)

// A book keeps how it came into the library; one added without an origin
// (everything from before it was kept) has none.
func TestBookAddKeepsOrigin(t *testing.T) {
	repo := newRepo(t)
	cases := []struct {
		name string
		in   origin.Origin
		want origin.Origin
	}{
		{"by hand", origin.Origin{Kind: origin.Manual}, origin.Origin{Kind: origin.Manual}},
		{"a followed author", origin.Origin{Kind: origin.Auto, Name: origin.JobFollow}, origin.Origin{Kind: origin.Auto, Name: origin.JobFollow}},
		{"a folder scan", origin.Origin{Kind: origin.Import}, origin.Origin{Kind: origin.Import}},
		{"nothing", origin.Origin{}, origin.Origin{}},
		{"an unknown kind", origin.Origin{Kind: "teleport", Name: "x"}, origin.Origin{}},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := repo.Add(books.Book{OLKey: "OL" + string(rune('A'+i)) + "W", Title: "Book " + c.name, WantEbook: true, Origin: c.in})
			if err != nil {
				t.Fatal(err)
			}
			got, err := repo.Get(b.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Origin != c.want {
				t.Fatalf("origin %+v, want %+v", got.Origin, c.want)
			}
		})
	}
}
