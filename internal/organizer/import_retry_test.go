package organizer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/organizer"
)

// Importing the same download twice (a retry after the app stopped between the
// import and the database update) finds the library file already hardlinked
// to the source. Replacing it must not leave the staged name behind.
func TestImportOverwriteOfAnAlreadyLinkedFileLeavesNothingBehind(t *testing.T) {
	policies := map[string]organizer.ConflictPolicy{
		"overwrite":           organizer.ConflictOverwrite,
		"overwrite if better": organizer.ConflictOverwriteIfBetter,
	}
	for name, policy := range policies {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "source.mkv")
			if err := os.WriteFile(src, []byte("movie bytes"), 0o644); err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(dir, "lib", "Movie (2024)", "Movie (2024).mkv")
			always := func() bool { return true }

			first, err := organizer.Import(src, dest, policy, always)
			if err != nil {
				t.Fatalf("first import: %v", err)
			}
			if first.Skipped {
				t.Fatal("first import was skipped")
			}
			second, err := organizer.Import(src, dest, policy, always)
			if err != nil {
				t.Fatalf("second import: %v", err)
			}
			if second.Skipped {
				t.Fatal("second import was skipped")
			}

			entries, err := os.ReadDir(filepath.Dir(dest))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "Movie (2024).mkv" {
				var names []string
				for _, e := range entries {
					names = append(names, e.Name())
				}
				t.Fatalf("the library folder holds %v, want just the movie", names)
			}
			if got, _ := os.ReadFile(dest); string(got) != "movie bytes" {
				t.Fatalf("the movie holds %q", got)
			}
		})
	}
}
