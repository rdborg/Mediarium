package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/settings"
)

func TestAnimeLibrary(t *testing.T) {
	s := newBareServer(t)
	root := t.TempDir()
	tv, anime := filepath.Join(root, "tv"), filepath.Join(root, "anime")
	for _, d := range []string{tv, anime} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	set := func(k, v string) {
		t.Helper()
		if err := s.Settings.Set(k, v, false); err != nil {
			t.Fatal(err)
		}
	}
	set(settings.KeyTVPath, tv)

	put := func(body string) (int, string) {
		w := httptest.NewRecorder()
		s.handlePutAnimeSettings(w, httptest.NewRequest(http.MethodPut, "/api/settings/anime", strings.NewReader(body)))
		return w.Code, w.Body.String()
	}
	if code, body := put(`{"path": "` + filepath.ToSlash(filepath.Join(tv, "anime")) + `"}`); code != http.StatusBadRequest {
		t.Errorf("an anime folder inside the TV folder must be refused: %d %s", code, body)
	}
	if code, body := put(`{"episodeFormat": "{Series Title}"}`); code != http.StatusBadRequest {
		t.Errorf("a format without the episode must be refused: %d %s", code, body)
	}
	if code, body := put(`{"path": "` + filepath.ToSlash(anime) + `", "episodeFormat": "` + organizer.AnimeFormat + `"}`); code != http.StatusOK {
		t.Fatalf("saving: %d %s", code, body)
	}

	// With the module off, nothing changes for new anime shows.
	if got := s.animeFolderFor(library.SeriesAnime, ""); got != "" {
		t.Errorf("module off: anime went to %q", got)
	}
	set(settings.KeyModulesAnime, "1")
	if got := s.animeFolderFor(library.SeriesAnime, ""); filepath.Clean(got) != filepath.Clean(anime) {
		t.Errorf("an anime show added without a folder went to %q, want the anime folder", got)
	}
	if got := s.animeFolderFor(library.SeriesAnime, tv); filepath.Clean(got) != filepath.Clean(anime) {
		t.Errorf("an anime show added to the main TV folder went to %q, want the anime folder", got)
	}
	if got := s.animeFolderFor(library.SeriesStandard, ""); got != "" {
		t.Errorf("a standard show went to %q", got)
	}
	found := false
	for _, f := range s.tvFolders() {
		if filepath.Clean(f) == filepath.Clean(anime) {
			found = true
		}
	}
	if !found {
		t.Errorf("the anime folder must be one of the TV folders: %v", s.tvFolders())
	}

	// The anime format names episodes with the number from the first episode.
	sr, err := s.MovieRepo.AddSeries(library.Series{TMDBID: 37854, Title: "One Piece", Year: 1999, SeriesType: library.SeriesAnime, RootPath: anime}, []library.Episode{
		{Season: 1, Episode: 1}, {Season: 1, Episode: 2}, {Season: 2, Episode: 1, Title: "The Reverse Mountain"},
	})
	if err != nil {
		t.Fatal(err)
	}
	dest, err := s.buildTVDestPath(sr, library.Episode{Season: 2, Episode: 1, Title: "The Reverse Mountain"}, parser.Parse("One.Piece.S02E01.1080p.WEB.x264-GRP"), "One.Piece.S02E01.1080p.WEB.x264-GRP", "x.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if want := "One Piece - S02E01 - 003 - The Reverse Mountain [1080p].mkv"; filepath.Base(dest) != want {
		t.Errorf("anime episode named %q, want %q", filepath.Base(dest), want)
	}
	if !strings.HasPrefix(filepath.Clean(dest), filepath.Clean(anime)) {
		t.Errorf("anime episode went to %q, not the anime folder", dest)
	}

	// Switching TV off switches the anime library off with it.
	set(settings.KeyModulesTV, "0")
	if s.animeEnabled() {
		t.Error("the anime library must be off while TV is")
	}
	if err := s.setModules(moduleChanges{Anime: ptr(true)}); err == nil {
		t.Error("switching anime on while TV is off must be refused")
	}
}
