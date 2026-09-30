package mediafiles_test

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/mediafiles"
)

const secretBody = "SECRET-OUTSIDE-THE-FOLDER"

// FuzzFolderOpen checks that no relative path, however it is written, reaches
// a file outside the title's folder, even with links planted inside it.
func FuzzFolderOpen(f *testing.F) {
	if runtime.GOOS == "windows" {
		f.Skip("symlinks need privileges on Windows")
	}
	base := f.TempDir()
	lib := filepath.Join(base, "lib")
	folder := filepath.Join(lib, "Movie (2020)")
	mk := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			f.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			f.Fatal(err)
		}
	}
	mk(filepath.Join(base, "secret.txt"), secretBody)
	mk(filepath.Join(lib, "other", "secret.txt"), secretBody)
	mk(filepath.Join(folder, "Movie (2020).mkv"), "video")
	mk(filepath.Join(folder, "sub", "Movie.en.srt"), "subs")
	mk(filepath.Join(folder, ".hidden", "x.txt"), "hidden")
	for name, target := range map[string]string{
		"abs-link":  filepath.Join(base, "secret.txt"),
		"rel-link":  "../../secret.txt",
		"sibling":   "../../other/secret.txt",
		"dir-link":  "../../other",
		"self-loop": "self-loop",
		"inside":    "Movie (2020).mkv",
	} {
		if err := os.Symlink(target, filepath.Join(folder, name)); err != nil {
			f.Skip("cannot make a symlink here")
		}
	}
	for _, s := range []string{
		"Movie (2020).mkv", "sub/Movie.en.srt", "abs-link", "rel-link", "sibling", "dir-link/secret.txt", "self-loop", "inside",
		"../secret.txt", "../../secret.txt", "sub/../../secret.txt", "/etc/passwd", "sub//Movie.en.srt", "./Movie (2020).mkv",
		".hidden/x.txt", "", ".", "..", "sub/", "a\x00b", `..\secret.txt`, strings.Repeat("../", 50) + "etc/passwd",
	} {
		f.Add(s)
	}

	dir, err := filepath.EvalSymlinks(folder)
	if err != nil {
		f.Fatal(err)
	}
	fo := mediafiles.Folder{Dir: dir, Name: "Movie (2020)"}
	f.Fuzz(func(t *testing.T, rel string) {
		file, _, err := fo.Open(rel)
		if err != nil {
			return
		}
		defer file.Close()
		body, _ := io.ReadAll(file)
		if strings.Contains(string(body), secretBody) {
			t.Fatalf("Open(%q) reached a file outside the folder", rel)
		}
		if strings.HasPrefix(filepath.Base(rel), ".") || strings.Contains(rel, "/.") {
			t.Fatalf("Open(%q) opened a hidden file", rel)
		}
	})
}

func TestListNeverShowsLinksOutOfTheFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	base := t.TempDir()
	folder := filepath.Join(base, "lib", "Movie (2020)")
	write(t, filepath.Join(base, "secret.txt"), secretBody)
	write(t, filepath.Join(folder, "Movie (2020).mkv"), "video")
	symlink(t, filepath.Join(base, "secret.txt"), filepath.Join(folder, "abs-link.txt"))
	symlink(t, "../../secret.txt", filepath.Join(folder, "rel-link.txt"))
	symlink(t, "self", filepath.Join(folder, "self"))
	dir, _ := filepath.EvalSymlinks(folder)
	files, err := mediafiles.Folder{Dir: dir}.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "Movie (2020).mkv" {
		t.Fatalf("List = %+v, want only the video", files)
	}
}
