package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/subtitles"
)

// With "Keep the original file names" on, a file named at random takes the
// release's name when the release is a movie or one episode on its own, and
// the naming preset otherwise.
func TestKeepOriginalNamesRandomFileTakesReleaseName(t *testing.T) {
	s := newAuditServer(t)
	movies, tv := filepath.Join(t.TempDir(), "movies"), filepath.Join(t.TempDir(), "tv")
	for k, v := range map[string]string{settings.KeyMoviesPath: movies, settings.KeyTVPath: tv, settings.KeyNamingPreset: "plex", settings.KeyIllegalCharMode: "strip"} {
		if err := s.Settings.Set(k, v, false); err != nil {
			t.Fatal(err)
		}
	}
	work := filepath.Join(t.TempDir(), "queue-9")
	movieFolder := filepath.Join(movies, "Example Movie (2024)")
	seasonFolder := filepath.Join(tv, "Tracker (2024)", "Season 04")
	series := library.Series{Title: "Tracker", Year: 2024, TMDBID: 1}
	ep := library.Episode{Season: 4, Episode: 3, Title: "Original Parts"}
	const (
		epRelease    = "Tracker.2024.S04E03.Original.Parts.1080p.DSNP.WEB-DL.DDP5.1.H.264-NTb"
		packRelease  = "Tracker.2024.S04.1080p.DSNP.WEB-DL.DDP5.1.H.264-NTb"
		movieRelease = "Example.Movie.2024.1080p.BluRay-GRP"
	)

	cases := []struct {
		name      string
		keep      bool
		release   string
		file      string
		may       bool // the release's name may stand in
		tv        bool
		want      string
		wantFrom  nameSource
		wantNoted bool
	}{
		{"movie: a normal file name is kept", true, movieRelease, "Other.Name.2024.mkv", true, false, filepath.Join(movieFolder, "Other.Name.2024.mkv"), namedByFile, false},
		{"movie: a random file name takes the release name", true, movieRelease, "a8f7d6e5c4b3.mkv", true, false, filepath.Join(movieFolder, movieRelease+".mkv"), namedByRelease, true},
		{"movie: the real extension stays", true, movieRelease, "wFvDJHhDrbESDBLq.m4v", true, false, filepath.Join(movieFolder, movieRelease+".m4v"), namedByRelease, true},
		{"movie: a random file and a random release use the preset", true, "q9w8e7r6t5y4", "a8f7d6e5c4b3.mkv", true, false, filepath.Join(movieFolder, "Example Movie (2024).mkv"), namedByPresetInstead, true},
		{"movie: not allowed to use the release name", true, movieRelease, "a8f7d6e5c4b3.mkv", false, false, filepath.Join(movieFolder, "Example Movie (2024).mkv"), namedByPresetInstead, true},
		{"movie: off, the preset names it", false, movieRelease, "a8f7d6e5c4b3.mkv", true, false, filepath.Join(movieFolder, "Example Movie (2024).mkv"), namedByPreset, false},
		{"movie: illegal characters in the release are removed", true, "Example: Movie? 2024 1080p", "abc.mkv", true, false, filepath.Join(movieFolder, "Example Movie 2024 1080p.mkv"), namedByRelease, true},
		{"episode: a normal file name is kept", true, epRelease, "Tracker.S04E03.1080p-X.mkv", true, true, filepath.Join(seasonFolder, "Tracker.S04E03.1080p-X.mkv"), namedByFile, false},
		{"episode: a random file name takes the release name", true, epRelease, "a8f7d6e5c4b3.mkv", true, true, filepath.Join(seasonFolder, epRelease+".mkv"), namedByRelease, true},
		{"episode: a random file and a random release use the preset", true, "q9w8e7r6t5y4", "a8f7d6e5c4b3.mkv", true, true, filepath.Join(seasonFolder, "Tracker - S04E03 - Original Parts.mkv"), namedByPresetInstead, true},
		{"episode: a season pack uses the preset", true, packRelease, "a8f7d6e5c4b3.mkv", false, true, filepath.Join(seasonFolder, "Tracker - S04E03 - Original Parts.mkv"), namedByPresetInstead, true},
		{"episode: off, the preset names it", false, epRelease, "a8f7d6e5c4b3.mkv", true, true, filepath.Join(seasonFolder, "Tracker - S04E03 - Original Parts.mkv"), namedByPreset, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := setBool(s.Settings, settings.KeyKeepOriginalNames, tc.keep); err != nil {
				t.Fatal(err)
			}
			var got string
			var from nameSource
			var err error
			if tc.tv {
				got, from, err = s.buildTVDestPathFor(series, []library.Episode{ep}, parser.Parse(tc.release), tc.release, filepath.Join(work, tc.file), tc.may)
			} else {
				got, from, err = s.buildDestPathFor("", "Example Movie", 2024, 5, tc.release, filepath.Join(work, tc.file), tc.may)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want || from != tc.wantFrom {
				t.Errorf("got %s (%d), want %s (%d)", got, from, tc.want, tc.wantFrom)
			}
			if noted := renamedNote("X", from, 1) != ""; noted != tc.wantNoted {
				t.Errorf("a note was written = %v, want %v", noted, tc.wantNoted)
			}
			if len(filepath.Base(got)) > 255 {
				t.Errorf("name is %d bytes", len(filepath.Base(got)))
			}
		})
	}

	t.Run("a very long release name stays within the file system limit", func(t *testing.T) {
		if err := setBool(s.Settings, settings.KeyKeepOriginalNames, true); err != nil {
			t.Fatal(err)
		}
		long := strings.Repeat("Very.Long.Release.Name.", 30) + "2024.1080p-GRP"
		got, from, err := s.buildDestPathFor("", "Example Movie", 2024, 5, long, filepath.Join(work, "a8f7d6e5c4b3.mkv"), true)
		if err != nil || from != namedByRelease || len(filepath.Base(got)) > 255 || !strings.HasSuffix(got, ".mkv") {
			t.Errorf("got %q (%d bytes), %d, %v", got, len(filepath.Base(got)), from, err)
		}
	})
}

// Which downloads let the release's name stand in for a file's name.
func TestReleaseNamesTheFile(t *testing.T) {
	one := parser.Parse("Tracker.2024.S04E03.1080p.WEB-DL-NTb")
	double := parser.Parse("Tracker.2024.S04E03E04.1080p.WEB-DL-NTb")
	pack := parser.Parse("Tracker.2024.S04.1080p.WEB-DL-NTb")
	cases := []struct {
		name     string
		single   bool
		files    int
		episodes int
		release  parser.Release
		want     bool
	}{
		{"one episode on its own", true, 1, 1, one, true},
		{"a season pack grab", false, 1, 1, one, false},
		{"several files in the download", true, 3, 1, one, false},
		{"a file holding two episodes", true, 1, 2, one, false},
		{"a double-episode release name", true, 1, 1, double, false},
		{"a release name without an episode", true, 1, 1, pack, false},
	}
	for _, tc := range cases {
		if got := releaseNamesTheFile(tc.single, tc.files, tc.episodes, tc.release); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The activity note says why a name was replaced, and only when one was.
func TestRenamedNote(t *testing.T) {
	cases := []struct {
		src   nameSource
		files int
		want  string
	}{
		{namedByRelease, 1, "Show: the file inside was named at random, so it was saved under the release name."},
		{namedByPresetInstead, 1, "Show: the file inside was named at random, so it was saved under your naming preset."},
		{namedByPresetInstead, 4, "Show: some files inside were named at random, so they were saved under your naming preset."},
		{namedByRelease, 2, "Show: some files inside were named at random, so they were saved under the release name."},
		{namedByFile, 1, ""},
		{namedByPreset, 1, ""},
		{namedByRelease, 0, ""},
	}
	for _, tc := range cases {
		if got := renamedNote("Show", tc.src, tc.files); got != tc.want {
			t.Errorf("renamedNote(%d, %d) = %q, want %q", tc.src, tc.files, got, tc.want)
		}
	}
}

// Subtitles that came with an obfuscated release follow the video's final
// name, which is the release name.
func TestKeepOriginalNamesSidecarsFollowReleaseName(t *testing.T) {
	s := newAuditServer(t)
	movies := filepath.Join(t.TempDir(), "movies")
	if err := s.Settings.Set(settings.KeyMoviesPath, movies, false); err != nil {
		t.Fatal(err)
	}
	if err := setBool(s.Settings, settings.KeyKeepOriginalNames, true); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	sub := filepath.Join(work, "a8f7d6e5c4b3.en.srt")
	if err := os.WriteFile(sub, []byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const release = "Example.Movie.2024.1080p.BluRay-GRP"
	dest, _, err := s.buildDestPathFor("", "Example Movie", 2024, 5, release, filepath.Join(work, "a8f7d6e5c4b3.mkv"), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := subtitles.ImportSidecars(dest, []subtitles.Sidecar{{Lang: "en", Files: []string{sub}}}); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(movies, "Example Movie (2024)", release+".en.srt")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("the subtitle should follow the video's final name: %v", err)
	}
}

// The rename tool still leaves files alone while names are kept, so a name
// taken from the release is never renamed back and forth.
func TestKeepOriginalNamesReleaseNameNotRenamedBack(t *testing.T) {
	s := newAuditServer(t)
	movies := filepath.Join(t.TempDir(), "movies")
	if err := s.Settings.Set(settings.KeyMoviesPath, movies, false); err != nil {
		t.Fatal(err)
	}
	if err := setBool(s.Settings, settings.KeyKeepOriginalNames, true); err != nil {
		t.Fatal(err)
	}
	m, err := s.MovieRepo.Add(library.Movie{TMDBID: 5, Title: "Example Movie", Year: 2024})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(movies, "Example Movie (2024)", "Example.Movie.2024.1080p.BluRay-GRP.mkv")
	if err := s.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "Bluray-1080p", file); err != nil {
		t.Fatal(err)
	}
	if plan, err := s.renamePlan(""); err != nil || len(plan) != 0 {
		t.Fatalf("nothing is renamed while names are kept, got %v %v", plan, err)
	}
}
