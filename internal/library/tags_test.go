package library_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/store"
)

func TestNormalizeTags(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{" Kids ", "kids", "4K"}, "[Kids 4K]"},
		{[]string{"Kids, 4K ,", ""}, "[Kids 4K]"},
		{[]string{"Family   films"}, "[Family films]"},
		{[]string{"a very long tag that goes on and on and on"}, "[a very long tag that goes on a]"},
		{nil, "[]"},
	}
	for _, tc := range cases {
		if got := fmt.Sprint(library.NormalizeTags(tc.in)); got != tc.want {
			t.Errorf("NormalizeTags(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestTitleTags(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	repo := library.NewRepo(db)
	m, _ := repo.Add(library.Movie{TMDBID: 1, Title: "One", Monitored: true})
	m2, _ := repo.Add(library.Movie{TMDBID: 2, Title: "Two", Monitored: true})

	added, removed, err := repo.SetTitleTags(library.TagMovie, m.ID, []string{"Kids", "4K"})
	if err != nil || fmt.Sprint(added) != "[Kids 4K]" || len(removed) != 0 {
		t.Fatalf("set: %v %v %v", added, removed, err)
	}
	// Another title spelling it differently joins the same tag.
	if added, _, _ := repo.SetTitleTags(library.TagMovie, m2.ID, []string{"kids"}); fmt.Sprint(added) != "[Kids]" {
		t.Fatalf("canonical spelling: %v", added)
	}
	added, removed, _ = repo.ChangeTitleTags(library.TagMovie, m.ID, []string{"Favourites"}, []string{"4k"})
	if fmt.Sprint(added) != "[Favourites]" || fmt.Sprint(removed) != "[4K]" {
		t.Fatalf("change: %v %v", added, removed)
	}
	if got, _ := repo.TitleTags(library.TagMovie, m.ID); fmt.Sprint(got) != "[Favourites Kids]" {
		t.Fatalf("tags: %v", got)
	}
	all, _ := repo.Tags()
	if fmt.Sprint(all) != "[{Favourites 1 0} {Kids 2 0}]" {
		t.Fatalf("all: %v", all)
	}
	byTitle, _ := repo.AllTitleTags(library.TagMovie)
	if len(byTitle[m2.ID]) != 1 {
		t.Fatalf("by title: %v", byTitle)
	}
	if err := repo.Delete(m2.ID); err != nil {
		t.Fatal(err)
	}
	if all, _ := repo.Tags(); fmt.Sprint(all) != "[{Favourites 1 0} {Kids 1 0}]" {
		t.Fatalf("a removed title takes its tags along: %v", all)
	}
}
