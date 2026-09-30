package organizer_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rdborg/mediarium/internal/organizer"
)

// A download that holds a link (a hostile archive can carry one) must never
// have the link's target copied into the library.
func TestImportRefusesLinksAndIgnoresThemWhenLookingForVideos(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root := t.TempDir()
	secret := filepath.Join(root, "config", "secret.key")
	if err := os.MkdirAll(filepath.Dir(secret), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte("do not copy me"), 0o600); err != nil {
		t.Fatal(err)
	}
	dl := filepath.Join(root, "downloads", "release")
	if err := os.MkdirAll(dl, 0o755); err != nil {
		t.Fatal(err)
	}
	// A link named like a big movie, next to the real one.
	link := filepath.Join(dl, "Movie.mkv")
	if err := os.Symlink(secret, link); err != nil {
		t.Skip("cannot make a symlink here")
	}
	real := filepath.Join(dl, "real.mp4")
	if err := os.WriteFile(real, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := organizer.FindLargestVideoFile(dl)
	if err != nil || got != real {
		t.Fatalf("FindLargestVideoFile = %q, %v; want the real file %q", got, err, real)
	}
	files, err := organizer.FindVideoFiles(dl)
	if err != nil || len(files) != 1 || files[0] != real {
		t.Fatalf("FindVideoFiles = %v, %v; want only the real file", files, err)
	}

	dest := filepath.Join(root, "library", "Movie (2020)", "Movie (2020).mkv")
	if _, err := organizer.Import(link, dest, organizer.ConflictSkip, nil); !errors.Is(err, organizer.ErrNotRegularFile) {
		t.Fatalf("Import of a link: %v, want ErrNotRegularFile", err)
	}
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		t.Fatalf("nothing may be created for a link, Lstat: %v", err)
	}
}
