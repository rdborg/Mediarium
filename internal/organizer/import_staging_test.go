package organizer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Two imports to the same place each stage under a name of their own, so one
// can't remove or overwrite the other's half-written file.
func TestStagingNamesAreUnique(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "Movie (2020).mkv")
	a, b := stagingPath(dest), stagingPath(dest)
	if a == b {
		t.Fatalf("two imports got the same staging name %q", a)
	}
	for _, p := range []string{a, b} {
		if filepath.Dir(p) != dir || !strings.HasSuffix(p, stagingSuffix) || !strings.HasPrefix(filepath.Base(p), "Movie (2020).mkv.") {
			t.Errorf("staging name %q", p)
		}
	}
	long := filepath.Join(dir, strings.Repeat("日", MaxNameBytes/3)+".mkv")
	if n := len(filepath.Base(stagingPath(long))); n > 255 {
		t.Errorf("staging name is %d bytes", n)
	}
}

// A link at the destination counts as something already there, even when it
// leads nowhere.
func TestImportSeesADanglingLinkAtTheDestination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "a.mkv")
	if err := os.WriteFile(src, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "Movie.mkv")
	if err := os.Symlink(filepath.Join(dir, "gone.mkv"), dest); err != nil {
		t.Skip("cannot make a symlink here")
	}
	res, err := Import(src, dest, ConflictSkip, nil)
	if err != nil || !res.Skipped {
		t.Fatalf("skip policy: %+v, %v", res, err)
	}
	res, err = Import(src, dest, ConflictOverwrite, nil)
	if err != nil || res.Skipped {
		t.Fatalf("overwrite policy: %+v, %v", res, err)
	}
	if fi, err := os.Lstat(dest); err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("the link should be replaced by the file: %v, %v", fi, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "gone.mkv")); !os.IsNotExist(err) {
		t.Fatalf("nothing is written through the link: %v", err)
	}
}

// A copy cut short by a restart or a power cut leaves its staging file in the
// library. The next import of that title clears the old ones away, and leaves
// alone a recent one (a copy that may still be running), other titles' files
// and anything that is not a staging file.
func TestImportClearsStaleStagingFiles(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "Movie (2020).mkv")
	old := time.Now().Add(-3 * time.Hour)
	put := func(name string, mod time.Time) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("half"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatal(err)
		}
		return p
	}
	stale := put("Movie (2020).mkv.0badc0de"+stagingSuffix, old)
	recent := put("Movie (2020).mkv.1badc0de"+stagingSuffix, time.Now())
	other := put("Other (2021).mkv.2badc0de"+stagingSuffix, old)
	notStaging := put("Movie (2020).mkv.nfo", old)
	badName := put("Movie (2020).mkv.zz"+stagingSuffix, old)

	src := filepath.Join(dir, "download.mkv")
	if err := os.WriteFile(src, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportCopy(src, dest, ConflictSkip, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("the old staging file should be gone: %v", err)
	}
	for name, p := range map[string]string{"recent staging file": recent, "other title's": other, "sidecar": notStaging, "odd name": badName} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("the %s should stay: %v", name, err)
		}
	}
}
