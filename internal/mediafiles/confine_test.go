package mediafiles_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/mediafiles"
)

// A folder whose name merely starts like a library folder (movies2 next to
// movies) is not inside it.
func TestForFileAndForDirRefuseASiblingWithTheSamePrefix(t *testing.T) {
	movies, _, _ := library(t)
	sibling := movies + "2"
	write(t, filepath.Join(sibling, "Film (2020)", "Film (2020).mkv"), "x")

	file := filepath.Join(sibling, "Film (2020)", "Film (2020).mkv")
	if _, err := mediafiles.ForFile(file, []string{movies}, false); !errors.Is(err, mediafiles.ErrOutside) {
		t.Errorf("ForFile: want ErrOutside, got %v", err)
	}
	if _, err := mediafiles.ForDir(filepath.Dir(file), []string{movies}); !errors.Is(err, mediafiles.ErrOutside) {
		t.Errorf("ForDir: want ErrOutside, got %v", err)
	}
	// The library folder itself is not a title's folder either.
	if _, err := mediafiles.ForDir(movies, []string{movies}); !errors.Is(err, mediafiles.ErrOutside) {
		t.Errorf("ForDir of the root: want ErrOutside, got %v", err)
	}
}

// Awkward names never open anything: NUL bytes, Windows separators, very long
// names and paths that only look relative.
func TestOpenRefusesAwkwardNames(t *testing.T) {
	movies, _, _ := library(t)
	folder, err := mediafiles.ForFile(filepath.Join(movies, "The Matrix (1999)", "The Matrix (1999).mkv"), []string{movies}, false)
	if err != nil {
		t.Fatal(err)
	}
	long := make([]byte, 5000)
	for i := range long {
		long[i] = 'a'
	}
	for _, rel := range []string{
		"The Matrix (1999).mkv\x00.jpg", "poster.jpg\x00", "\x00", `Extras\Making Of.mp4`, string(long), "Extras//Making Of.mp4",
		"./poster.jpg", "Extras/./Making Of.mp4", "Extras/", "C:/Windows/win.ini", "//etc/passwd",
	} {
		f, _, err := folder.Open(rel)
		if err == nil {
			f.Close()
			t.Errorf("Open(%q) should be refused", rel)
		}
	}
}
