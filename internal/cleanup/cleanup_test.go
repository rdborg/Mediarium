package cleanup_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/cleanup"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// layout is a downloads folder with a working folder, and a library folder
// beside it holding a precious file.
type layout struct {
	base, work, library, precious string
	area                          cleanup.Area
}

func newLayout(t *testing.T) layout {
	t.Helper()
	root := t.TempDir()
	l := layout{
		base:    filepath.Join(root, "downloads"),
		work:    filepath.Join(root, "downloads", "incomplete"),
		library: filepath.Join(root, "movies"),
	}
	l.precious = filepath.Join(l.library, "Keep (2020)", "Keep (2020).mkv")
	write(t, l.precious, "precious")
	if err := os.MkdirAll(l.work, 0o755); err != nil {
		t.Fatal(err)
	}
	l.area = cleanup.Area{Base: l.base, Work: l.work, Protected: []string{l.library}}
	return l
}

func (l layout) requirePrecious(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(l.precious)
	if err != nil || string(data) != "precious" {
		t.Fatalf("the library file was touched: %v %q", err, data)
	}
}

func TestScanClassifiesAndRemoves(t *testing.T) {
	l := newLayout(t)
	write(t, filepath.Join(l.work, "queue-1", "movie.mkv"), "12345")                     // imported leftover
	write(t, filepath.Join(l.work, "queue-2", "part.mkv"), "downloading")                // running download
	write(t, filepath.Join(l.work, "stray", "x.nfo"), "abc")                             // orphaned
	write(t, filepath.Join(l.work, "fresh", "x.nfo"), "abc")                             // too new to judge
	if err := os.MkdirAll(filepath.Join(l.work, "empty", "nested"), 0o755); err != nil { // empty
		t.Fatal(err)
	}

	decide := map[string]cleanup.Decision{
		"queue-1": {Remove: true, Reason: cleanup.ReasonImportedLeftover},
		"queue-2": {Active: true},
		"stray":   {Remove: true, Reason: cleanup.ReasonOrphaned},
		"fresh":   {},
		"empty":   {},
	}
	items, err := cleanup.Scan(l.area, func(name string, _ fs.FileInfo) cleanup.Decision { return decide[name] })
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]cleanup.Item{}
	for _, it := range items {
		got[it.Path] = it
	}
	want := map[string]cleanup.Reason{
		"incomplete/queue-1": cleanup.ReasonImportedLeftover,
		"incomplete/stray":   cleanup.ReasonOrphaned,
		"incomplete/empty":   cleanup.ReasonEmptyFolder,
	}
	if len(got) != len(want) {
		t.Fatalf("scan found %+v, want %v", items, want)
	}
	for p, reason := range want {
		if got[p].Reason != reason {
			t.Fatalf("%s: reason %q, want %q", p, got[p].Reason, reason)
		}
	}
	if got["incomplete/queue-1"].SizeBytes != 5 {
		t.Fatalf("size of queue-1 = %d, want 5", got["incomplete/queue-1"].SizeBytes)
	}

	removed, err := cleanup.Remove(l.area, items)
	if err != nil || len(removed) != 3 {
		t.Fatalf("removed %d, err %v", len(removed), err)
	}
	for _, name := range []string{"queue-1", "stray", "empty"} {
		if exists(filepath.Join(l.work, name)) {
			t.Fatalf("%s should be gone", name)
		}
	}
	for _, name := range []string{"queue-2", "fresh"} {
		if !exists(filepath.Join(l.work, name)) {
			t.Fatalf("%s must be kept", name)
		}
	}
	if !exists(l.work) {
		t.Fatal("the working folder itself must never be removed")
	}
	l.requirePrecious(t)
}

// TestNothingOutsideTheWorkingFolderIsTouched covers the ways a path could
// lead out of the downloads area.
func TestNothingOutsideTheWorkingFolderIsTouched(t *testing.T) {
	removeAll := func(string, fs.FileInfo) cleanup.Decision {
		return cleanup.Decision{Remove: true, Reason: cleanup.ReasonOrphaned}
	}

	t.Run("a symlink pointing into the library is removed as a link", func(t *testing.T) {
		l := newLayout(t)
		link := filepath.Join(l.work, "queue-7")
		if err := os.Symlink(filepath.Dir(l.precious), link); err != nil {
			t.Skipf("symlinks not available: %v", err)
		}
		items, err := cleanup.Scan(l.area, removeAll)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].SizeBytes != 0 {
			t.Fatalf("a link frees nothing: %+v", items)
		}
		if _, err := cleanup.Remove(l.area, items); err != nil {
			t.Fatal(err)
		}
		if exists(link) {
			t.Fatal("the link itself should be gone")
		}
		l.requirePrecious(t)
	})

	t.Run("a file inside a symlinked folder is refused", func(t *testing.T) {
		l := newLayout(t)
		link := filepath.Join(l.work, "queue-8")
		if err := os.Symlink(filepath.Dir(l.precious), link); err != nil {
			t.Skipf("symlinks not available: %v", err)
		}
		_, err := cleanup.RemoveInside(l.area, filepath.Join(link, filepath.Base(l.precious)))
		if !errors.Is(err, cleanup.ErrOutside) {
			t.Fatalf("expected ErrOutside, got %v", err)
		}
		l.requirePrecious(t)
	})

	t.Run("a path with ../ is refused", func(t *testing.T) {
		l := newLayout(t)
		escape := l.work + string(filepath.Separator) + ".." + string(filepath.Separator) + ".." + string(filepath.Separator) + "movies"
		_, err := cleanup.RemoveInside(l.area, escape)
		if !errors.Is(err, cleanup.ErrOutside) {
			t.Fatalf("expected ErrOutside, got %v", err)
		}
		l.requirePrecious(t)
	})

	t.Run("the working folder itself is refused", func(t *testing.T) {
		l := newLayout(t)
		write(t, filepath.Join(l.work, "queue-1", "a.mkv"), "a")
		if _, err := cleanup.RemoveInside(l.area, l.work); !errors.Is(err, cleanup.ErrOutside) {
			t.Fatalf("expected ErrOutside, got %v", err)
		}
		if !exists(filepath.Join(l.work, "queue-1", "a.mkv")) {
			t.Fatal("nothing may be removed")
		}
	})

	t.Run("a library folder inside the working folder is never touched", func(t *testing.T) {
		l := newLayout(t)
		inside := filepath.Join(l.work, "movies")
		write(t, filepath.Join(inside, "a.mkv"), "library")
		l.area.Protected = append(l.area.Protected, inside)
		items, err := cleanup.Scan(l.area, removeAll)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 0 {
			t.Fatalf("a protected folder was offered for removal: %+v", items)
		}
		if _, err := cleanup.RemoveInside(l.area, inside); !errors.Is(err, cleanup.ErrOutside) {
			t.Fatalf("expected ErrOutside, got %v", err)
		}
		if !exists(filepath.Join(inside, "a.mkv")) {
			t.Fatal("the library file inside the working folder was removed")
		}
	})

	t.Run("a working folder reached through a symlink still works", func(t *testing.T) {
		l := newLayout(t)
		realWork := filepath.Join(t.TempDir(), "real-incomplete")
		write(t, filepath.Join(realWork, "queue-3", "a.mkv"), "a")
		linkedWork := filepath.Join(l.base, "linked")
		if err := os.Symlink(realWork, linkedWork); err != nil {
			t.Skipf("symlinks not available: %v", err)
		}
		area := cleanup.Area{Base: l.base, Work: linkedWork, Protected: []string{l.library}}
		if _, err := cleanup.RemoveInside(area, filepath.Join(linkedWork, "queue-3")); err != nil {
			t.Fatal(err)
		}
		if exists(filepath.Join(realWork, "queue-3")) {
			t.Fatal("the download folder should be gone")
		}
		l.requirePrecious(t)
	})
}

func TestSizeCountsOnlyUnsharedFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a", "one.bin"), "1234")
	write(t, filepath.Join(dir, "a", "two.bin"), "123456")
	elsewhere := filepath.Join(t.TempDir(), "hardlinked-into-library.bin")
	if err := os.Link(filepath.Join(dir, "a", "two.bin"), elsewhere); err != nil {
		t.Skipf("hardlinks not available: %v", err)
	}
	got := cleanup.Size(filepath.Join(dir, "a"))
	if got != 4 && got != 10 { // 10 where the OS cannot report link counts
		t.Fatalf("Size = %d, want 4 (the hardlinked file frees nothing)", got)
	}
}

func TestOlderThan(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	write(t, p, "x")
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Lstat(p)
	if !cleanup.OlderThan(info, 24*time.Hour, time.Now()) || cleanup.OlderThan(info, 72*time.Hour, time.Now()) {
		t.Fatal("OlderThan is wrong")
	}
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && p != dir {
			r, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(r))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func TestRemoveFolderStaysInsideTheLibrary(t *testing.T) {
	root := t.TempDir()
	movies := filepath.Join(root, "movies")
	write(t, filepath.Join(movies, "Gone (2020)", "Gone (2020).mkv"), "x")
	write(t, filepath.Join(movies, "Gone (2020)", "Gone (2020).en.srt"), "x")
	write(t, filepath.Join(movies, "Gone (2020)", "poster.jpg"), "x")
	write(t, filepath.Join(movies, "Keep (2020)", "Keep (2020).mkv"), "keep")
	outside := filepath.Join(root, "outside")
	write(t, filepath.Join(outside, "important.mkv"), "outside")

	if _, err := cleanup.RemoveFolder(movies, filepath.Join(movies, "Gone (2020)")); err != nil {
		t.Fatal(err)
	}
	if got := listDir(t, movies); strings.Join(got, ",") != "Keep (2020),Keep (2020)/Keep (2020).mkv" {
		t.Fatalf("library after removing one title: %v", got)
	}

	refused := []struct {
		name   string
		folder string
		setup  func() error
	}{
		{"the root itself", movies, nil},
		{"a path with ../", filepath.Join(movies, "..", "outside"), nil},
		{"a symlink leading out", filepath.Join(movies, "Escape (2020)"), func() error { return os.Symlink(outside, filepath.Join(movies, "Escape (2020)")) }},
		{"a folder under a symlink leading out", filepath.Join(movies, "Via", "sub"), func() error {
			if err := os.MkdirAll(filepath.Join(outside, "sub"), 0o755); err != nil {
				return err
			}
			return os.Symlink(outside, filepath.Join(movies, "Via"))
		}},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				if err := tc.setup(); err != nil {
					t.Skipf("setup: %v", err)
				}
			}
			if _, err := cleanup.RemoveFolder(movies, tc.folder); !errors.Is(err, cleanup.ErrOutside) {
				t.Fatalf("expected ErrOutside, got %v", err)
			}
			if !exists(filepath.Join(outside, "important.mkv")) || !exists(filepath.Join(movies, "Keep (2020)", "Keep (2020).mkv")) {
				t.Fatal("something outside the title's folder was removed")
			}
		})
	}
	if !exists(movies) {
		t.Fatal("the library folder itself was removed")
	}
}

func TestRemoveFileWithSidecars(t *testing.T) {
	root := t.TempDir()
	loose := []string{
		"Alien (1979).mkv", "Alien (1979).en.srt", "Alien (1979).en.forced.srt", "Alien (1979).nfo", "Alien (1979).pt-BR.sdh.ass",
		"Alien (1979) Directors Cut.mkv", "Alien (1979).Resurrection.1997.nfo", "Aliens (1986).mkv", "Aliens (1986).nfo",
	}
	for _, n := range loose {
		write(t, filepath.Join(root, n), n)
	}
	write(t, filepath.Join(root, "Show", "Season 01", "Show - S01E01.mkv"), "e1")
	write(t, filepath.Join(root, "Show", "Season 01", "Show - S01E01.en.srt"), "e1")

	removed, err := cleanup.RemoveFileWithSidecars(root, filepath.Join(root, "Alien (1979).mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 5 {
		t.Fatalf("removed %v", removed)
	}
	want := "Alien (1979) Directors Cut.mkv,Alien (1979).Resurrection.1997.nfo,Aliens (1986).mkv,Aliens (1986).nfo,Show,Show/Season 01,Show/Season 01/Show - S01E01.en.srt,Show/Season 01/Show - S01E01.mkv"
	if got := strings.Join(listDir(t, root), ","); got != want {
		t.Fatalf("left behind:\n%s\nwant:\n%s", got, want)
	}

	// The last episode of a show goes with its subtitle, and the folders it
	// leaves empty go too, but never the root.
	if _, err := cleanup.RemoveFileWithSidecars(root, filepath.Join(root, "Show", "Season 01", "Show - S01E01.mkv")); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(root, "Show")) || !exists(root) {
		t.Fatal("empty show folders should be pruned, the root kept")
	}

	if _, err := cleanup.RemoveFileWithSidecars(root, filepath.Join(root, "..", "elsewhere.mkv")); !errors.Is(err, cleanup.ErrOutside) {
		t.Fatalf("expected ErrOutside, got %v", err)
	}
}
