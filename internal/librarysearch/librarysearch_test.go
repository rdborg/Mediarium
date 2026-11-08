package librarysearch_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/librarysearch"
	"github.com/rdborg/mediarium/internal/store"
)

func openDB(t testing.TB) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func exec(t testing.TB, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func movie(t testing.TB, db *sql.DB, tmdb int, title string, year int) {
	t.Helper()
	exec(t, db, `INSERT INTO movies (tmdb_id, title, year) VALUES (?, ?, ?)`, tmdb, title, year)
}

func titles(hits []librarysearch.Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Title
	}
	return out
}

func TestTokens(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"Star Wars", []string{"star", "wars"}},
		{"  spaced   out ", []string{"spaced", "out"}},
		{"Don't Look Up!", []string{"don", "t", "look", "up"}},
		{"AC/DC: Back in Black", []string{"ac", "dc", "back", "in", "black"}},
		{`"quoted" * (parens) ^caret`, []string{"quoted", "parens", "caret"}},
		{"Amélie", []string{"amélie"}},
		{"千と千尋の神隠し", []string{"千と千尋の神隠し"}},
		{"Ленинград 2", []string{"ленинград", "2"}},
		{"!!! ---", nil},
		{"", nil},
	} {
		got := librarysearch.Tokens(c.in)
		if !slices.Equal(got, c.want) {
			t.Errorf("Tokens(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestQuery(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"star w", `"star"* "w"*`},
		{"it's", `"it"* "s"*`},
		{`a" OR "b`, `"a"* "or"* "b"*`},
		{"NEAR(a b)", `"near"* "a"* "b"*`},
		{"", ""},
		{"***", ""},
	} {
		if got := librarysearch.Query(c.in); got != c.want {
			t.Errorf("Query(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

// The index is filled by the triggers as the library changes, and the search
// finds titles by the start of any word, ignoring accents and punctuation.
func TestSearchFindsTitles(t *testing.T) {
	db := openDB(t)
	for i, m := range []struct {
		title string
		year  int
	}{
		{"Star Wars", 1977},
		{"The Empire Strikes Back", 1980},
		{"Amélie", 2001},
		{"Léon: The Professional", 1994},
		{"Don't Look Up", 2021},
		{"Spirited Away 千と千尋の神隠し", 2001},
		{"Ленинград", 2007},
		{"Se7en", 1995},
		{"AC/DC: Let There Be Rock", 1980},
		{"Wall-E", 2008},
		{"100% Wolf", 2020},
	} {
		movie(t, db, i+1, m.title, m.year)
	}
	for _, c := range []struct {
		name, q string
		want    []string
	}{
		{"one word prefix", "emp", []string{"The Empire Strikes Back"}},
		{"two letters", "st", []string{"Star Wars", "The Empire Strikes Back"}},
		{"multi word, any order", "wars star", []string{"Star Wars"}},
		{"second word prefix", "star w", []string{"Star Wars"}},
		{"accent typed plain", "amelie", []string{"Amélie"}},
		{"accent typed", "amélie", []string{"Amélie"}},
		{"accent in title, plain query", "leon", []string{"Léon: The Professional"}},
		{"capitals", "STAR", []string{"Star Wars"}},
		{"apostrophe", "don't lo", []string{"Don't Look Up"}},
		{"apostrophe split", "don t", []string{"Don't Look Up"}},
		{"cjk", "千と千", []string{"Spirited Away 千と千尋の神隠し"}},
		{"cyrillic", "ленин", []string{"Ленинград"}},
		{"cyrillic capital", "ЛЕНИН", []string{"Ленинград"}},
		{"digit in title", "se7", []string{"Se7en"}},
		{"punctuation in query", "ac/dc let", []string{"AC/DC: Let There Be Rock"}},
		{"hyphen", "wall e", []string{"Wall-E"}},
		{"percent sign", "100", []string{"100% Wolf"}},
		{"nothing", "zzzz", nil},
		{"only punctuation", "!!!", nil},
		{"quotes and operators are plain text", `star" OR "x`, nil},
		{"a single letter", "w", []string{"Star Wars", "Wall-E", "100% Wolf"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			hits, err := librarysearch.Search(context.Background(), db, c.q, 20)
			if err != nil {
				t.Fatal(err)
			}
			got := titles(hits)
			slices.Sort(got)
			want := slices.Clone(c.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("Search(%q) = %q, want %q", c.q, got, want)
			}
		})
	}
}

func TestSearchRanksTheTitleFirst(t *testing.T) {
	db := openDB(t)
	// A name found only through the other names ranks below a title that
	// holds the typed word.
	movie(t, db, 1, "Der Herr der Ringe", 2001)
	movie(t, db, 2, "Lord Jim", 1965)
	exec(t, db, `INSERT INTO title_names (kind, tmdb_id, names) VALUES ('movie', 1, '["The Lord of the Rings"]')`)
	hits, err := librarysearch.Search(context.Background(), db, "lord", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(hits); !slices.Equal(got, []string{"Lord Jim", "Der Herr der Ringe"}) {
		t.Errorf("got %q", got)
	}
}

func TestSearchOtherKinds(t *testing.T) {
	db := openDB(t)
	exec(t, db, `INSERT INTO series (tmdb_id, title, year) VALUES (10, 'Breaking Bad', 2008)`)
	exec(t, db, `INSERT INTO books (ol_key, title, author, year) VALUES ('OL1W', 'Dune', 'Frank Herbert', 1965)`)
	exec(t, db, `INSERT INTO artists (mbid, name, sort_name) VALUES ('a1', 'Björk', 'Bjork')`)
	exec(t, db, `INSERT INTO albums (artist_id, mbid, title) VALUES (1, 'r1', 'Homogenic')`)
	exec(t, db, `INSERT INTO display_faces (kind, tmdb_id, language, title) VALUES ('tv', 10, 'de-DE', 'Frei Fall')`)

	for _, c := range []struct {
		q    string
		kind string
		sub  string
	}{
		{"break", "tv", ""},
		{"frei", "tv", ""}, // a translated title
		{"dun", "book", "Frank Herbert"},
		{"herbert", "book", "Frank Herbert"}, // the author
		{"bjork", "artist", ""},
		{"homo", "album", "Björk"},
	} {
		hits, err := librarysearch.Search(context.Background(), db, c.q, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) == 0 || hits[0].Kind != c.kind || hits[0].Subtitle != c.sub {
			t.Errorf("Search(%q) = %+v, want a %s hit with subtitle %q", c.q, hits, c.kind, c.sub)
		}
	}
	hits, _ := librarysearch.Search(context.Background(), db, "breaking", 10)
	if len(hits) != 1 || hits[0].TMDBID != 10 || hits[0].Year != 2008 {
		t.Errorf("a show hit lost its ids: %+v", hits)
	}
}

func TestIndexFollowsTheLibrary(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	movie(t, db, 1, "Old Name", 2000)
	exec(t, db, `UPDATE movies SET title = 'Fresh Title' WHERE tmdb_id = 1`)
	if hits, _ := librarysearch.Search(ctx, db, "old", 10); len(hits) != 0 {
		t.Errorf("the old title is still found: %+v", hits)
	}
	if hits, _ := librarysearch.Search(ctx, db, "fresh", 10); len(hits) != 1 {
		t.Errorf("the new title is not found: %+v", hits)
	}
	// A second copy of a movie is not a second hit.
	exec(t, db, `INSERT INTO movies (tmdb_id, title, copy_of) VALUES (1, 'Fresh Title', 1)`)
	if hits, _ := librarysearch.Search(ctx, db, "fresh", 10); len(hits) != 1 {
		t.Errorf("a copy shows up as its own hit: %+v", hits)
	}
	exec(t, db, `DELETE FROM movies WHERE copy_of IS NULL`)
	if hits, _ := librarysearch.Search(ctx, db, "fresh", 10); len(hits) != 0 {
		t.Errorf("a removed movie is still found: %+v", hits)
	}
}

func TestEnsureFillsAnEmptyIndex(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	if did, err := librarysearch.Ensure(ctx, db); err != nil || did {
		t.Fatalf("an empty library needs no rebuild: %v %v", did, err)
	}
	movie(t, db, 1, "Alien", 1979)
	movie(t, db, 2, "Aliens", 1986)
	exec(t, db, `DELETE FROM library_search`)
	if hits, _ := librarysearch.Search(ctx, db, "alien", 10); len(hits) != 0 {
		t.Fatal("the index should be empty")
	}
	did, err := librarysearch.Ensure(ctx, db)
	if err != nil || !did {
		t.Fatalf("Ensure = %v, %v; want a rebuild", did, err)
	}
	if hits, _ := librarysearch.Search(ctx, db, "alien", 10); len(hits) != 2 {
		t.Errorf("after the rebuild: %+v", hits)
	}
	if did, _ := librarysearch.Ensure(ctx, db); did {
		t.Error("a filled index was rebuilt")
	}
}

// When the index cannot be read, the LIKE search answers instead.
func TestSearchFallsBackToLike(t *testing.T) {
	db := openDB(t)
	movie(t, db, 1, "Star Wars", 1977)
	movie(t, db, 2, "Wars of the Worlds", 2005)
	exec(t, db, `DROP TRIGGER library_search_movies_insert`)
	exec(t, db, `DROP TRIGGER library_search_movies_update`)
	exec(t, db, `DROP TRIGGER library_search_movies_delete`)
	exec(t, db, `DROP TRIGGER library_search_title_names_insert_movie`)
	exec(t, db, `DROP TRIGGER library_search_title_names_update_movie`)
	exec(t, db, `DROP TRIGGER library_search_display_faces_insert_movie`)
	exec(t, db, `DROP TRIGGER library_search_display_faces_update_movie`)
	exec(t, db, `DROP TABLE library_search`)
	hits, err := librarysearch.Search(context.Background(), db, "wars", 10)
	if err != nil {
		t.Fatal(err)
	}
	// Titles that start with the text come first.
	if got := titles(hits); !slices.Equal(got, []string{"Wars of the Worlds", "Star Wars"}) {
		t.Errorf("got %q", got)
	}
	// Characters that mean something in a LIKE pattern are plain text.
	if hits, _ := librarysearch.Search(context.Background(), db, "100%", 10); len(hits) != 0 {
		t.Errorf("a percent sign matched everything: %+v", hits)
	}
}

// Several thousand titles are searched in well under a frame.
func TestSearchIsFastOnABigLibrary(t *testing.T) {
	if testing.Short() {
		t.Skip("fills a few thousand rows")
	}
	db := openDB(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	words := []string{"Night", "Dark", "Silent", "River", "Golden", "Last", "Lost", "City", "Dream", "Storm", "Shadow", "King", "Road", "Summer", "Winter", "Garden"}
	const n = 20000
	for i := 0; i < n; i++ {
		title := fmt.Sprintf("%s %s %s %d", words[i%16], words[(i/16)%16], words[(i/256)%16], i)
		if _, err := tx.Exec(`INSERT INTO movies (tmdb_id, title, year) VALUES (?, ?, ?)`, i+1, title, 1950+i%70); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"da", "dark n", "sil riv", "gold", "stor shad"} {
		start := time.Now()
		hits, err := librarysearch.Search(context.Background(), db, q, 10)
		took := time.Since(start)
		if err != nil || len(hits) == 0 {
			t.Fatalf("%q: %v %d hits", q, err, len(hits))
		}
		if took > 150*time.Millisecond {
			t.Errorf("%q took %v over %d titles", q, took, n)
		}
	}
}

func BenchmarkSearch(b *testing.B) {
	db := openDB(b)
	tx, _ := db.Begin()
	for i := 0; i < 10000; i++ {
		if _, err := tx.Exec(`INSERT INTO movies (tmdb_id, title, year) VALUES (?, ?, 2000)`, i+1, fmt.Sprintf("Title number %d of the long library", i)); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := librarysearch.Search(context.Background(), db, "title num", 10); err != nil {
			b.Fatal(err)
		}
	}
}
