//go:build !windows

package organizer

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// The copy fallback is used when a hardlink cannot be made (another drive, a
// share without links). These tests reach it directly, and through a real
// second filesystem when the machine has one.

func TestCopyFileKeepsTimeAndRemovesPartialOnFailure(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.mkv")
	if err := os.WriteFile(src, []byte("bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2019, 3, 4, 5, 6, 7, 0, time.UTC)
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Lstat(src)

	dest := filepath.Join(dir, "dest.mkv")
	if err := copyFile(src, dest, info); err != nil {
		t.Fatal(err)
	}
	got, _ := os.Stat(dest)
	if !got.ModTime().Equal(old) {
		t.Errorf("modification time %v, want %v", got.ModTime(), old)
	}

	// The destination is never overwritten, and a failure must not delete what
	// was already there.
	if err := copyFile(src, dest, info); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("copy onto an existing file: %v, want ErrExist", err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "bytes" {
		t.Fatalf("existing file damaged: %q", b)
	}

	// Reading a folder fails half way through the copy: nothing is left.
	folder := filepath.Join(dir, "folder")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(dir, "partial.mkv")
	if err := copyFile(folder, partial, info); err == nil {
		t.Fatal("expected an error copying a folder")
	}
	if _, err := os.Lstat(partial); !os.IsNotExist(err) {
		t.Fatalf("a partial copy was left behind: %v", err)
	}
}

func TestPublishNewNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	staged := filepath.Join(dir, "movie.mkv"+stagingSuffix)
	dest := filepath.Join(dir, "movie.mkv")
	if err := os.WriteFile(staged, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("already here"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := publishNew(staged, dest); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("publishNew over an existing file: %v, want ErrExist", err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "already here" {
		t.Fatalf("existing file replaced: %q", b)
	}
	os.Remove(dest)
	if err := publishNew(staged, dest); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "new" {
		t.Fatalf("got %q", b)
	}
	if _, err := os.Lstat(staged); !os.IsNotExist(err) {
		t.Fatalf("staged file still there: %v", err)
	}
}

// With the library on another filesystem no hardlink is possible (EXDEV) and
// the file must be copied, complete, with no staging file left behind.
func TestImportAcrossFilesystems(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs a second filesystem at /dev/shm")
	}
	shm, err := os.MkdirTemp("/dev/shm", "mediarium-test-")
	if err != nil {
		t.Skip("no /dev/shm here")
	}
	t.Cleanup(func() { os.RemoveAll(shm) })
	dir := t.TempDir()
	same, supported, err := SameFilesystem(dir, shm)
	if err != nil || !supported || same {
		t.Skip("/dev/shm is on the same filesystem as the temp folder")
	}
	src := filepath.Join(dir, "movie.mkv")
	if err := os.WriteFile(src, []byte("movie bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(shm, "Movie (2020)", "Movie (2020).mkv")

	res, err := Import(src, dest, ConflictSkip, nil)
	if err != nil || res.UsedHardlink || res.Skipped {
		t.Fatalf("Import = %+v, %v; want a copy", res, err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "movie bytes" {
		t.Fatalf("copied %q", b)
	}
	if got, _ := os.ReadDir(filepath.Dir(dest)); len(got) != 1 {
		t.Fatalf("staging file left behind: %v", got)
	}

	// Replacing across filesystems keeps the old file until the new one is whole.
	if err := os.WriteFile(src, []byte("better movie"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res, err = Import(src, dest, ConflictOverwrite, nil); err != nil || res.UsedHardlink {
		t.Fatalf("replace = %+v, %v", res, err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "better movie" {
		t.Fatalf("replaced with %q", b)
	}
	if got, _ := os.ReadDir(filepath.Dir(dest)); len(got) != 1 {
		t.Fatalf("staging file left behind: %v", got)
	}
	if b, _ := os.ReadFile(src); string(b) != "better movie" {
		t.Fatalf("source changed: %q", b)
	}
}

// Opening a pipe waits for a writer forever, so a pipe is refused up front.
func TestImportRefusesAPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pipes on disk")
	}
	dir := t.TempDir()
	fifo := filepath.Join(dir, "movie.mkv")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip("cannot make a pipe here")
	}
	done := make(chan error, 1)
	go func() {
		_, err := Import(fifo, filepath.Join(dir, "lib", "movie.mkv"), ConflictSkip, nil)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNotRegularFile) {
			t.Fatalf("Import of a pipe: %v, want ErrNotRegularFile", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Import of a pipe hangs")
	}
}
