package mediafiles

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestAudioContentTypeAndKind(t *testing.T) {
	for name, want := range map[string]string{
		"a.flac": "audio/flac", "b.MP3": "audio/mpeg", "c.m4a": "audio/mp4", "d.alac": "audio/mp4",
		"e.aac": "audio/aac", "f.ogg": "audio/ogg", "g.opus": "audio/opus", "h.oga": "audio/ogg",
		"cover.jpg": "", "notes.txt": "", "movie.mkv": "", "noext": "",
	} {
		if got := AudioContentType(name); got != want {
			t.Errorf("AudioContentType(%q) = %q, want %q", name, got, want)
		}
	}
	if AlbumKindOf("01.flac") != KindAudio || AlbumKindOf("cover.jpg") != KindImage || AlbumKindOf("x.log") != KindOther {
		t.Fatal("album kinds")
	}
	// The listings of movies and shows are not changed by music.
	if KindOf("01.flac") != KindOther {
		t.Fatal("KindOf must keep calling audio files other")
	}
}

func TestForDirStaysInsideTheRoot(t *testing.T) {
	root := t.TempDir()
	album := filepath.Join(root, "Artist", "Album (2001)")
	if err := os.MkdirAll(album, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "Link")); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}

	f, err := ForDir(album, []string{root})
	if err != nil || f.Name != "Artist/Album (2001)" {
		t.Fatalf("album folder: %+v %v", f, err)
	}
	if _, err := ForDir(filepath.Join(root, "Missing"), []string{root}); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing folder: %v", err)
	}
	if _, err := ForDir(root, []string{root}); !errors.Is(err, ErrOutside) {
		t.Fatalf("the root itself is not an album folder: %v", err)
	}
	if _, err := ForDir(outside, []string{root}); !errors.Is(err, ErrOutside) {
		t.Fatalf("outside the root: %v", err)
	}
	if _, err := ForDir(filepath.Join(root, "Link"), []string{root}); !errors.Is(err, ErrOutside) {
		t.Fatalf("a symlink leading out of the root: %v", err)
	}
	if _, err := ForDir("", []string{root}); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("empty: %v", err)
	}
	if _, err := ForDir(album, nil); !errors.Is(err, ErrOutside) {
		t.Fatalf("no roots: %v", err)
	}
}
