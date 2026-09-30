package mediafiles_test

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/mediafiles"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks not available here: %v", err)
	}
}

func TestKindAndContentType(t *testing.T) {
	cases := []struct {
		name string
		kind mediafiles.Kind
		ct   string
	}{
		{"Movie.mkv", mediafiles.KindVideo, "video/x-matroska"},
		{"Movie.MP4", mediafiles.KindVideo, "video/mp4"},
		{"Movie.m4v", mediafiles.KindVideo, "video/mp4"},
		{"clip.webm", mediafiles.KindVideo, "video/webm"},
		{"Movie.en.srt", mediafiles.KindSubtitle, ""},
		{"poster.jpg", mediafiles.KindImage, ""},
		{"movie.nfo", mediafiles.KindNFO, ""},
		{"readme.txt", mediafiles.KindOther, ""},
		{"page.html", mediafiles.KindOther, ""},
		{"noext", mediafiles.KindOther, ""},
	}
	for _, tc := range cases {
		if got := mediafiles.KindOf(tc.name); got != tc.kind {
			t.Errorf("KindOf(%q) = %s, want %s", tc.name, got, tc.kind)
		}
		if got := mediafiles.VideoContentType(tc.name); got != tc.ct {
			t.Errorf("VideoContentType(%q) = %q, want %q", tc.name, got, tc.ct)
		}
	}
}

func TestPreviewContentType(t *testing.T) {
	cases := []struct {
		name   string
		ct     string
		isText bool
	}{
		{"Movie.mkv", "video/x-matroska", false},
		{"Movie.MP4", "video/mp4", false},
		{"poster.jpg", "image/jpeg", false},
		{"fanart.JPEG", "image/jpeg", false},
		{"folder.png", "image/png", false},
		{"thumb.webp", "image/webp", false},
		{"anim.gif", "image/gif", false},
		{"movie.nfo", mediafiles.TextContentType, true},
		{"Movie.en.srt", mediafiles.TextContentType, true},
		{"Movie.en.ASS", mediafiles.TextContentType, true},
		{"Movie.ssa", mediafiles.TextContentType, true},
		{"Movie.vtt", mediafiles.TextContentType, true},
		{"readme.txt", mediafiles.TextContentType, true},
		// Never served: pages, scripts, vector images, binary subtitles, odd pictures.
		{"page.html", "", false},
		{"page.htm", "", false},
		{"logo.svg", "", false},
		{"app.js", "", false},
		{"feed.xml", "", false},
		{"Movie.sub", "", false},
		{"Movie.idx", "", false},
		{"poster.bmp", "", false},
		{"poster.tbn", "", false},
		{"noext", "", false},
		{"sample.mp4.html", "", false},
	}
	for _, tc := range cases {
		ct, isText := mediafiles.PreviewContentType(tc.name)
		if ct != tc.ct || isText != tc.isText {
			t.Errorf("PreviewContentType(%q) = %q, %v; want %q, %v", tc.name, ct, isText, tc.ct, tc.isText)
		}
	}
}

// library builds <tmp>/movies and <tmp>/tv plus a secret file outside both.
func library(t *testing.T) (movies, tv, secret string) {
	t.Helper()
	base := t.TempDir()
	movies, tv = filepath.Join(base, "movies"), filepath.Join(base, "tv")
	secret = filepath.Join(base, "secret", "passwords.txt")
	write(t, secret, "hunter2")
	write(t, filepath.Join(movies, "The Matrix (1999)", "The Matrix (1999).mkv"), "video-bytes")
	write(t, filepath.Join(movies, "The Matrix (1999)", "The Matrix (1999).en.srt"), "subs")
	write(t, filepath.Join(movies, "The Matrix (1999)", "poster.jpg"), "img")
	write(t, filepath.Join(movies, "The Matrix (1999)", "movie.nfo"), "<movie/>")
	write(t, filepath.Join(movies, "The Matrix (1999)", ".hidden"), "x")
	write(t, filepath.Join(movies, "The Matrix (1999)", "Extras", "Making Of.mp4"), "extra")
	write(t, filepath.Join(movies, "Loose Movie (2001).mkv"), "loose")
	write(t, filepath.Join(movies, "Loose Movie (2001).en.srt"), "loose subs")
	write(t, filepath.Join(movies, "Other Movie (2002).mkv"), "other")
	write(t, filepath.Join(tv, "Show (2011)", "Season 01", "Show - S01E01.mkv"), "ep1")
	write(t, filepath.Join(tv, "Show (2011)", "Season 02", "Show - S02E01.mp4"), "ep2")
	write(t, filepath.Join(tv, "Show (2011)", "tvshow.nfo"), "<tvshow/>")
	return movies, tv, secret
}

func paths(files []mediafiles.File) string {
	var p []string
	for _, f := range files {
		p = append(p, f.Path+":"+string(f.Kind))
	}
	return strings.Join(p, ",")
}

func TestForFileAndList(t *testing.T) {
	movies, tv, _ := library(t)
	roots := []string{movies, tv, ""}

	cases := []struct {
		name     string
		file     string
		topLevel bool
		wantName string
		want     string
	}{
		{"movie folder", filepath.Join(movies, "The Matrix (1999)", "The Matrix (1999).mkv"), false, "The Matrix (1999)",
			"Extras/Making Of.mp4:video,The Matrix (1999).en.srt:subtitle,The Matrix (1999).mkv:video,movie.nfo:nfo,poster.jpg:image"},
		{"movie loose in the root shows only its own files", filepath.Join(movies, "Loose Movie (2001).mkv"), false, "",
			"Loose Movie (2001).en.srt:subtitle,Loose Movie (2001).mkv:video"},
		{"show folder above the seasons", filepath.Join(tv, "Show (2011)", "Season 02", "Show - S02E01.mp4"), true, "Show (2011)",
			"Season 01/Show - S01E01.mkv:video,Season 02/Show - S02E01.mp4:video,tvshow.nfo:nfo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			folder, err := mediafiles.ForFile(tc.file, roots, tc.topLevel)
			if err != nil {
				t.Fatal(err)
			}
			if folder.Name != tc.wantName {
				t.Fatalf("name %q, want %q", folder.Name, tc.wantName)
			}
			files, err := folder.List()
			if err != nil {
				t.Fatal(err)
			}
			if got := paths(files); got != tc.want {
				t.Fatalf("files\n got %s\nwant %s", got, tc.want)
			}
			for _, f := range files {
				if f.Size == 0 || f.Modified.IsZero() {
					t.Fatalf("size and time should be set: %+v", f)
				}
			}
		})
	}
}

func TestForFileRejectsOutsideLibrary(t *testing.T) {
	movies, tv, secret := library(t)
	cases := []struct {
		name string
		file string
		top  bool
	}{
		{"file outside every root", secret, false},
		{"traversal in the stored path", filepath.Join(movies, "..", "secret", "passwords.txt"), false},
		{"show episode directly in the root", filepath.Join(tv, "loose.mkv"), true},
	}
	write(t, filepath.Join(tv, "loose.mkv"), "x")
	for _, tc := range cases {
		if _, err := mediafiles.ForFile(tc.file, []string{movies, tv}, tc.top); !errors.Is(err, mediafiles.ErrOutside) {
			t.Errorf("%s: want ErrOutside, got %v", tc.name, err)
		}
	}
	if _, err := mediafiles.ForFile(filepath.Join(movies, "Gone (2000)", "Gone.mkv"), []string{movies}, false); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a missing folder should be ErrNotExist, got %v", err)
	}
}

func TestForFileRejectsSymlinkedFolderEscape(t *testing.T) {
	movies, _, secret := library(t)
	// A title folder inside the library that is really a symlink to elsewhere.
	symlink(t, filepath.Dir(secret), filepath.Join(movies, "Sneaky (2020)"))
	if _, err := mediafiles.ForFile(filepath.Join(movies, "Sneaky (2020)", "passwords.txt"), []string{movies}, false); !errors.Is(err, mediafiles.ErrOutside) {
		t.Fatalf("a folder symlinked out of the library must be refused, got %v", err)
	}
}

func TestOpen(t *testing.T) {
	movies, _, secret := library(t)
	folder, err := mediafiles.ForFile(filepath.Join(movies, "The Matrix (1999)", "The Matrix (1999).mkv"), []string{movies}, false)
	if err != nil {
		t.Fatal(err)
	}
	// Symlinks: one escaping the folder, one staying inside it.
	symlink(t, secret, filepath.Join(folder.Dir, "escape.mkv"))
	symlink(t, filepath.Dir(secret), filepath.Join(folder.Dir, "escapedir"))
	symlink(t, "The Matrix (1999).mkv", filepath.Join(folder.Dir, "alias.mkv"))

	f, info, err := folder.Open("The Matrix (1999).mkv")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(f)
	f.Close()
	if string(body) != "video-bytes" || info.Size() != int64(len("video-bytes")) {
		t.Fatalf("read %q size %d", body, info.Size())
	}
	if f, _, err := folder.Open("Extras/Making Of.mp4"); err != nil {
		t.Fatalf("subfolder file: %v", err)
	} else {
		f.Close()
	}
	if f, _, err := folder.Open("alias.mkv"); err != nil {
		t.Fatalf("a symlink inside the folder should open: %v", err)
	} else {
		f.Close()
	}

	bad := []string{
		"", ".", "../../secret/passwords.txt", "../The Matrix (1999)/movie.nfo", "Extras/../../x",
		"/etc/passwd", secret, `..\..\secret\passwords.txt`, ".hidden", "escape.mkv", "escapedir/passwords.txt",
	}
	for _, rel := range bad {
		f, _, err := folder.Open(rel)
		if err == nil {
			f.Close()
			t.Errorf("Open(%q) should be refused", rel)
		}
	}
	if _, _, err := folder.Open("Extras"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a directory is not a file: %v", err)
	}

	files, err := folder.List()
	if err != nil {
		t.Fatal(err)
	}
	got := paths(files)
	if strings.Contains(got, "escape") || !strings.Contains(got, "alias.mkv") {
		t.Fatalf("listing should hide escaping symlinks and keep inside ones: %s", got)
	}
}

func TestOpenPrefixFolder(t *testing.T) {
	movies, _, _ := library(t)
	folder, err := mediafiles.ForFile(filepath.Join(movies, "Loose Movie (2001).mkv"), []string{movies}, false)
	if err != nil {
		t.Fatal(err)
	}
	if f, _, err := folder.Open("Loose Movie (2001).mkv"); err != nil {
		t.Fatal(err)
	} else {
		f.Close()
	}
	for _, rel := range []string{"Other Movie (2002).mkv", "The Matrix (1999)/The Matrix (1999).mkv"} {
		if f, _, err := folder.Open(rel); err == nil {
			f.Close()
			t.Errorf("Open(%q) should not reach other titles in the root", rel)
		}
	}
}
