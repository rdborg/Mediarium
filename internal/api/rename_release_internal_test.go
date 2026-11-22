package api

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/settings"
)

func TestRenameReleaseChoosesTheNameToReadFrom(t *testing.T) {
	tests := []struct {
		name, stored, file, want string
	}{
		{"no release stored: the file name", "", "/m/Heat (1995) [1080p].mkv", "Heat (1995) [1080p]"},
		{"only spaces stored: the file name", "  ", "/m/Heat (1995) [1080p].mkv", "Heat (1995) [1080p]"},
		{"the stored release wins", "Heat.1995.1080p.WEBRip.x265-GRP", "/m/Heat (1995) [1080p].mkv", "Heat.1995.1080p.WEBRip.x265-GRP"},
		{"a release without resolution borrows the file's", "Heat.1995.WEBRip.x265-GRP", "/m/Heat (1995) [1080p].mkv", "Heat.1995.WEBRip.x265-GRP 1080p"},
		{"neither has a resolution", "Heat.1995.WEBRip-GRP", "/m/Heat (1995).mkv", "Heat.1995.WEBRip-GRP"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renameRelease(tt.stored, tt.file); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// Renaming reads the stored release, so what the old name had lost comes back,
// and the title is the movie's, not the old folder's.
func TestRenamePlanUsesTheStoredRelease(t *testing.T) {
	s := newAuditServer(t)
	movies, tv := filepath.Join(t.TempDir(), "movies"), filepath.Join(t.TempDir(), "tv")
	for k, v := range map[string]string{
		settings.KeyMoviesPath: movies, settings.KeyTVPath: tv, settings.KeyIllegalCharMode: "strip",
		settings.KeyNamingPreset:       "custom",
		settings.KeyMovieNameFormat:    "{Movie CleanTitle} {(Release Year)} - {{Edition Tags}} {[Quality Full]}{[MediaInfo AudioCodec]}{[MediaInfo VideoCodec]}{-Release Group}",
		settings.KeyEpisodeNameFormat:  "{Series CleanTitle} - S{season:00}E{episode:00} - {[Quality Full]}{[MediaInfo VideoCodec]}{-Release Group}",
	} {
		if err := s.Settings.Set(k, v, false); err != nil {
			t.Fatal(err)
		}
	}

	old := filepath.Join(movies, "Ölümcül Deney (2026)", "Ölümcül Deney (2026) [1080p].mkv")
	m, err := s.MovieRepo.Add(library.Movie{TMDBID: 1, Title: "Resident Evil", Year: 2026, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "WEBRip-1080p", old); err != nil {
		t.Fatal(err)
	}
	plain, err := s.MovieRepo.Add(library.Movie{TMDBID: 2, Title: "Heat", Year: 1995, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	heat := filepath.Join(movies, "Heat (1995)", "Heat.1995.1080p.BluRay.x264-GRP.mkv")
	if err := s.MovieRepo.SetStatus(plain.ID, library.StatusDownloaded, "Bluray-1080p", heat); err != nil {
		t.Fatal(err)
	}
	if err := s.MovieRepo.SetReleaseTitle(m.ID, "Resident.Evil.2026.1080p.WEBRip.10Bit.DDP5.1.x265-NeoNoir"); err != nil {
		t.Fatal(err)
	}

	sr, err := s.MovieRepo.AddSeries(library.Series{TMDBID: 9, Title: "Pluribus", Year: 2025, Monitored: true},
		[]library.Episode{{Season: 1, Episode: 1, Title: "Pilot"}})
	if err != nil {
		t.Fatal(err)
	}
	eps, err := s.MovieRepo.ListEpisodes(sr.ID)
	if err != nil || len(eps) != 1 {
		t.Fatalf("episodes: %v %d", err, len(eps))
	}
	epFile := filepath.Join(tv, "Pluribus (2025)", "Season 01", "Pluribus - S01E01 - [1080p].mkv")
	if err := s.MovieRepo.SetEpisodeStatus(eps[0].ID, library.StatusDownloaded, "WEBRip-1080p", epFile); err != nil {
		t.Fatal(err)
	}
	if err := s.MovieRepo.SetEpisodeReleaseTitle(eps[0].ID, "Pluribus.S01E01.Pilot.1080p.ATVP.WEBRip.x265-Rapta"); err != nil {
		t.Fatal(err)
	}

	plan, err := s.renamePlan("")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, it := range plan {
		got[it.Kind+it.Title] = it.To
	}
	t.Logf("plan: %v", got)
	wantMovie := filepath.Join(movies, "Resident Evil (2026)", "Resident Evil (2026) - [WEBRip-1080p][DD5.1][x265]-NeoNoir.mkv")
	if got["movieResident Evil (2026)"] != wantMovie {
		t.Errorf("movie:\n got  %s\n want %s", got["movieResident Evil (2026)"], wantMovie)
	}
	wantEp := filepath.Join(tv, "Pluribus (2025)", "Season 01", "Pluribus - S01E01 - [WEBRip-1080p][x265]-Rapta.mkv")
	if got["episodePluribus S01E01"] != wantEp {
		t.Errorf("episode:\n got  %s\n want %s", got["episodePluribus S01E01"], wantEp)
	}
	// No release stored: the name still comes from the file, as before.
	if to := got["movieHeat (1995)"]; to != "" && filepath.Base(to) == "Heat (1995) - .mkv" {
		t.Errorf("movie without a stored release lost its tokens: %q", to)
	}
}
