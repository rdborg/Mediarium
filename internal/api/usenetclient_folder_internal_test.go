package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveFinishedFolder(t *testing.T) {
	base := t.TempDir()
	completed := filepath.Join(base, "downloads", "completed")
	library := filepath.Join(base, "media", "movies")
	mk := func(p string) string {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "file.mkv"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	exists := func(p string) bool { _, err := os.Lstat(p); return err == nil }

	job := mk(filepath.Join(completed, "films", "Some.Release-GRP"))
	removeFinishedFolder(job, completed, []string{library})
	if exists(job) {
		t.Error("the job's folder should be gone")
	}

	for name, tc := range map[string]struct{ folder, root string }{
		"the completed folder itself":  {completed, completed},
		"outside the completed folder": {mk(filepath.Join(base, "other", "Thing")), completed},
		"the library folder":           {library, completed},
		"holding the library folder":   {filepath.Join(base, "media"), completed},
		"inside the library folder":    {mk(filepath.Join(library, "Movie (2020)")), completed},
		"no path mapping":              {mk(filepath.Join(completed, "films", "Unmapped-GRP")), ""},
		"a path that is not absolute":  {"downloads/completed/x", ""},
		"nothing":                      {"", ""},
	} {
		os.MkdirAll(library, 0o755)
		removeFinishedFolder(tc.folder, tc.root, []string{library})
		if tc.folder != "" && filepath.IsAbs(tc.folder) && !exists(tc.folder) {
			t.Errorf("%s: the folder must stay", name)
		}
	}
}
