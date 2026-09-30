package libimport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzIdentity feeds folder and file names to the code that turns them into a
// title, year and episode list.
func FuzzIdentity(f *testing.F) {
	for _, s := range []string{
		"Inception (2010)/Inception (2010) 1080p BluRay.mkv",
		"Show/Season 01/Show.S01E01.mkv",
		"Season 01/Show.S01E01-E03.mkv",
		"Show/Specials/Show.S00E01.mkv",
		"1917 (2019)/1917.2019.mkv",
		"Blade Runner 2049/Blade.Runner.2049.mkv",
		"a.mkv",
		"Show/S01E01.mkv",
		"Show/Season 1/",
		"../../etc/x.mkv",
		"日本語/日本語.S01E01.mkv",
		"/",
		"",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, path string) {
		rel := strings.Split(path, "/")
		title, year, tier := movieIdentity(rel)
		checkIdentity(t, path, title, year)
		_ = tier

		title, year, season, episodes, _, ok := tvIdentity(rel)
		if !ok {
			return
		}
		checkIdentity(t, path, title, year)
		if title == "" {
			t.Fatalf("tvIdentity(%q) accepted an empty title", path)
		}
		if len(episodes) == 0 {
			t.Fatalf("tvIdentity(%q) accepted a file without episodes", path)
		}
		if season < 0 || season > 99 {
			t.Fatalf("tvIdentity(%q) season %d", path, season)
		}
		_ = groupKey(title, year)
	})
}

func checkIdentity(t *testing.T, path, title string, year int) {
	t.Helper()
	if !utf8.ValidString(title) {
		t.Fatalf("%q: title %q is not valid UTF-8", path, title)
	}
	if title != strings.TrimSpace(title) {
		t.Fatalf("%q: title %q has stray spaces", path, title)
	}
	if year < 0 || year > 2099 {
		t.Fatalf("%q: year %d", path, year)
	}
}

// FuzzScanTree builds a small tree from fuzzed names and scans it. Whatever
// the names are, the scan must finish and only report files inside the root.
func FuzzScanTree(f *testing.F) {
	f.Add("Inception (2010)/Inception.mkv", "Show/Season 01/Show.S01E01.mkv", "Heat (1995)/sample.mkv")
	f.Add("a/../../b.mkv", "...mkv", ".mkv")
	f.Add("x/y/z/w/v/u.mkv", "Show.S01E01.mkv", "Season 1/S01E01.mkv")
	f.Add(strings.Repeat("a", 300)+".mkv", "\x00.mkv", "日本語/x.mkv")
	f.Fuzz(func(t *testing.T, a, b, c string) {
		root := t.TempDir()
		made := 0
		for _, name := range []string{a, b, c} {
			if !plainRelPath(name) {
				continue
			}
			p := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				continue
			}
			if err := os.WriteFile(p, []byte("x"), 0o644); err == nil {
				made++
			}
		}
		for _, scan := range []func(string) (Result, error){ScanMovies, ScanTV} {
			res, err := scan(root)
			if err != nil {
				t.Fatalf("scan of %q %q %q: %v", a, b, c, err)
			}
			count := 0
			for _, g := range res.Groups {
				checkIdentity(t, g.Key, g.Title, g.Year)
				for _, f := range g.Files {
					count++
					if rel, err := filepath.Rel(root, f.Path); err != nil || strings.HasPrefix(rel, "..") {
						t.Fatalf("file %s is outside %s", f.Path, root)
					}
				}
			}
			if count > made {
				t.Fatalf("%d files reported but only %d were made", count, made)
			}
		}
		_, _ = ScanMovieFolder(root)
		_, _, _ = ScanSeriesFolder(root)
	})
}

// plainRelPath reports whether name can be created below a folder: no empty,
// dot or dot-dot parts and no NUL.
func plainRelPath(name string) bool {
	if name == "" || strings.ContainsRune(name, 0) || len(name) > 400 {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." || len(part) > 255 {
			return false
		}
	}
	return true
}
