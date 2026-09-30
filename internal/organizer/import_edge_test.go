package organizer_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/organizer"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// A retry after a crash finds the file it linked the first time. That is a
// success, not "a file already exists".
func TestImportOfAnAlreadyLinkedFileIsDone(t *testing.T) {
	for _, policy := range []organizer.ConflictPolicy{organizer.ConflictSkip, organizer.ConflictAsk, organizer.ConflictOverwrite, organizer.ConflictOverwriteIfBetter} {
		dir := t.TempDir()
		src := filepath.Join(dir, "dl", "movie.mkv")
		dest := filepath.Join(dir, "lib", "Movie (2020)", "Movie (2020).mkv")
		writeFile(t, src, "movie bytes")
		if _, err := organizer.Import(src, dest, organizer.ConflictSkip, nil); err != nil {
			t.Fatal(err)
		}
		res, err := organizer.Import(src, dest, policy, func() bool { return false })
		if err != nil || res.Skipped {
			t.Fatalf("policy %d: second import = %+v, %v; want done", policy, res, err)
		}
		if got := names(t, filepath.Dir(dest)); len(got) != 1 {
			t.Fatalf("policy %d: stray files next to the movie: %v", policy, got)
		}
	}
}

// Importing a file onto itself must not damage it.
func TestImportOntoItselfKeepsTheFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "movie.mkv")
	writeFile(t, src, "movie bytes")
	if _, err := organizer.Import(src, src, organizer.ConflictOverwrite, nil); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, src); got != "movie bytes" {
		t.Fatalf("file changed: %q", got)
	}
	if got := names(t, dir); len(got) != 1 {
		t.Fatalf("stray files: %v", got)
	}
}

func TestImportIntoAFolderPathBlockedByAFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "movie.mkv")
	writeFile(t, src, "x")
	writeFile(t, filepath.Join(dir, "lib"), "i am a file")
	if _, err := organizer.Import(src, filepath.Join(dir, "lib", "Movie", "Movie.mkv"), organizer.ConflictSkip, nil); err == nil {
		t.Fatal("expected an error when a parent of the destination is a file")
	}
	if got := readFile(t, src); got != "x" {
		t.Fatalf("source changed: %q", got)
	}
}

func TestImportOverAFolderFailsCleanly(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "movie.mkv")
	writeFile(t, src, "x")
	dest := filepath.Join(dir, "lib", "movie.mkv")
	if err := os.MkdirAll(filepath.Join(dest, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := organizer.Import(src, dest, organizer.ConflictOverwrite, nil); err == nil {
		t.Fatal("expected an error when the destination is a folder")
	}
	if got := names(t, filepath.Dir(dest)); len(got) != 1 || got[0] != "movie.mkv" {
		t.Fatalf("temporary file left behind: %v", got)
	}
	if got := readFile(t, src); got != "x" {
		t.Fatalf("source changed: %q", got)
	}
}

func TestImportMissingSource(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "lib", "movie.mkv")
	if _, err := organizer.Import(filepath.Join(dir, "gone.mkv"), dest, organizer.ConflictSkip, nil); err == nil {
		t.Fatal("expected an error for a missing source")
	}
	if _, err := os.Stat(filepath.Dir(dest)); err == nil {
		t.Fatal("no folder should be made for a source that is not there")
	}
}

func TestImportSourceIsAFolder(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "folder.mkv")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := organizer.Import(src, filepath.Join(dir, "lib", "movie.mkv"), organizer.ConflictSkip, nil); err == nil {
		t.Fatal("expected an error for a folder")
	}
}

func TestImportEmptyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "empty.mkv")
	writeFile(t, src, "")
	dest := filepath.Join(dir, "lib", "empty.mkv")
	if _, err := organizer.Import(src, dest, organizer.ConflictSkip, nil); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, dest); got != "" {
		t.Fatalf("got %q", got)
	}
}

// Paths with characters that trouble other tools.
func TestImportOddNames(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in put.mkv")
	writeFile(t, src, "x")
	for _, name := range []string{"Movie (2020) [1080p].mkv", "Amélie ☺.mkv", "a b  c.mkv", "日本語.mkv", "-dash.mkv", "$(id).mkv", "it's.mkv", strings.Repeat("a", 200) + ".mkv"} {
		dest := filepath.Join(dir, "lib", name)
		if _, err := organizer.Import(src, dest, organizer.ConflictSkip, nil); err != nil {
			t.Errorf("%q: %v", name, err)
			continue
		}
		if got := readFile(t, dest); got != "x" {
			t.Errorf("%q: got %q", name, got)
		}
	}
}

func TestIsSample(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"sample.mkv", true},
		{"Sample.mkv", true},
		{"movie-sample.mkv", true},
		{"movie.sample.mkv", true},
		{"movie_SAMPLE_1080p.mkv", true},
		{"Sample/movie.mkv", true},
		{"release/Samples/movie.mkv", true},
		{"movie.mkv", false},
		{"Sampler.mkv", false},
		{"Resample.2019.mkv", false},
		{"Example.2019.mkv", false},
		{"Sampleton/movie.mkv", false},
		{"release/movie.mkv", false},
	}
	for _, tc := range cases {
		if got := organizer.IsSample(tc.path); got != tc.want {
			t.Errorf("IsSample(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// A folder called "sample" above the download says nothing about its files.
func TestFindVideoFilesIgnoresSamplePartsOfTheScanRoot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sample", "downloads")
	writeFile(t, filepath.Join(dir, "Show.S01E01.mkv"), "x")
	writeFile(t, filepath.Join(dir, "Sample", "clip.mkv"), "x")
	got, err := organizer.FindVideoFiles(dir)
	if err != nil || len(got) != 1 || filepath.Base(got[0]) != "Show.S01E01.mkv" {
		t.Fatalf("FindVideoFiles = %v, %v", got, err)
	}
}
