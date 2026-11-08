package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/settings"
)

// With the anime module on and an anime films folder set, an animated film
// made in Japanese goes there when it is added without a folder of its own.
func TestAnimeFilmsFolder(t *testing.T) {
	s := newBareServer(t)
	root := t.TempDir()
	movies, films := filepath.Join(root, "movies"), filepath.Join(root, "anime-films")
	other := filepath.Join(root, "documentaries")
	for _, d := range []string{movies, films, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Settings.Set(settings.KeyMoviesPath, movies, false); err != nil {
		t.Fatal(err)
	}
	put := func(body string) (int, string) {
		w := httptest.NewRecorder()
		s.handlePutAnimeSettings(w, httptest.NewRequest(http.MethodPut, "/api/settings/anime", strings.NewReader(body)))
		return w.Code, w.Body.String()
	}
	if code, body := put(`{"moviesPath": "` + filepath.ToSlash(filepath.Join(movies, "anime")) + `"}`); code != http.StatusBadRequest {
		t.Errorf("a folder inside the movie folder must be refused: %d %s", code, body)
	}
	if code, body := put(`{"moviesPath": "` + filepath.ToSlash(films) + `"}`); code != http.StatusOK || !strings.Contains(body, `"moviesPath"`) {
		t.Fatalf("saving: %d %s", code, body)
	}
	if !slices.ContainsFunc(s.movieFolders(), func(f string) bool { return filepath.Clean(f) == filepath.Clean(films) }) {
		t.Errorf("the anime films folder isn't one of the movie folders: %v", s.movieFolders())
	}

	spirited := metadata.Movie{Title: "Spirited Away", OriginalLanguage: "ja", Genres: []metadata.Genre{{ID: 16, Name: "Animation"}}}
	fromList := metadata.Movie{Title: "Your Name.", OriginalLanguage: "ja", GenreIDs: []int{10749, 16}}
	liveAction := metadata.Movie{Title: "Shoplifters", OriginalLanguage: "ja", GenreIDs: []int{18}}
	pixar := metadata.Movie{Title: "Up", OriginalLanguage: "en", GenreIDs: []int{16}}
	for _, m := range []metadata.Movie{spirited, fromList} {
		if !m.IsAnime() {
			t.Errorf("%s isn't anime", m.Title)
		}
	}
	for _, m := range []metadata.Movie{liveAction, pixar} {
		if m.IsAnime() {
			t.Errorf("%s is anime", m.Title)
		}
	}

	if got := s.animeMovieFolderFor(spirited, movies); got != movies {
		t.Errorf("module off: went to %q", got)
	}
	if err := s.Settings.Set(settings.KeyModulesAnime, "1", false); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		m        metadata.Movie
		in, want string
	}{
		{spirited, "", films},
		{spirited, movies, films},
		{fromList, movies, films},
		{spirited, other, other}, // a folder chosen on purpose stays
		{liveAction, movies, movies},
		{pixar, movies, movies},
	} {
		if got := s.animeMovieFolderFor(c.m, c.in); filepath.Clean(got) != filepath.Clean(c.want) {
			t.Errorf("%s into %q went to %q, want %q", c.m.Title, c.in, got, c.want)
		}
	}
}
