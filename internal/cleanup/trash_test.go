package cleanup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

var trashNow = time.Date(2026, 10, 3, 9, 30, 0, 0, time.UTC)

func TestTrashFolderAndRestore(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "Heat (1995)")
	writeFile(t, filepath.Join(folder, "Heat (1995).mkv"), "video")
	writeFile(t, filepath.Join(folder, "Heat (1995).en.srt"), "subs")

	e, err := TrashFolder(root, folder, "Heat (1995)", trashNow)
	if err != nil {
		t.Fatal(err)
	}
	if exists(folder) {
		t.Fatal("the folder should have left the library")
	}
	if e.Files != 2 || e.Size != int64(len("video")+len("subs")) || e.Label != "Heat (1995)" {
		t.Errorf("entry: %+v", e)
	}
	list, err := ListTrash(root)
	if err != nil || len(list) != 1 || list[0].ID != e.ID {
		t.Fatalf("list: %+v (err %v)", list, err)
	}

	if _, err := RestoreTrash(root, e.ID); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(folder, "Heat (1995).mkv")) || !exists(filepath.Join(folder, "Heat (1995).en.srt")) {
		t.Fatal("restore should put every file back")
	}
	if list, _ := ListTrash(root); len(list) != 0 {
		t.Errorf("the entry should be gone after a restore: %+v", list)
	}
}

func TestTrashFileTakesItsSidecarsAndNothingElse(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Shared")
	writeFile(t, filepath.Join(dir, "Heat.mkv"), "v")
	writeFile(t, filepath.Join(dir, "Heat.nfo"), "n")
	writeFile(t, filepath.Join(dir, "Other.mkv"), "o")

	e, err := TrashFileWithSidecars(root, filepath.Join(dir, "Heat.mkv"), "Heat", trashNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Paths) != 2 || exists(filepath.Join(dir, "Heat.mkv")) || exists(filepath.Join(dir, "Heat.nfo")) {
		t.Fatalf("video and .nfo should be in the bin: %+v", e)
	}
	if !exists(filepath.Join(dir, "Other.mkv")) {
		t.Fatal("a file of another title must stay")
	}
}

func TestRestoreRefusesWhenTheSpotIsTaken(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "Heat (1995)")
	writeFile(t, filepath.Join(folder, "a.mkv"), "old")
	e, err := TrashFolder(root, folder, "Heat", trashNow)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(folder, "b.mkv"), "new")
	if _, err := RestoreTrash(root, e.ID); !errors.Is(err, ErrRestoreBlocked) {
		t.Fatalf("want ErrRestoreBlocked, got %v", err)
	}
	if list, _ := ListTrash(root); len(list) != 1 {
		t.Fatal("a refused restore must keep the entry")
	}
}

func TestTrashRefusesWhatItShouldNotTouch(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "x.mkv"), "x")
	cases := []struct {
		name string
		run  func() error
	}{
		{"a folder outside the library", func() error { _, err := TrashFolder(root, outside, "x", trashNow); return err }},
		{"the library folder itself", func() error { _, err := TrashFolder(root, root, "x", trashNow); return err }},
		{"a file outside the library", func() error {
			_, err := TrashFileWithSidecars(root, filepath.Join(outside, "x.mkv"), "x", trashNow)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); !errors.Is(err, ErrOutside) {
				t.Fatalf("want ErrOutside, got %v", err)
			}
		})
	}
	for _, id := range []string{"../x", "20261003-093000-zzzzzzzz", "", "20261003-093000-0011223"} {
		if err := DeleteTrash(root, id); !errors.Is(err, ErrTrashEntry) {
			t.Errorf("id %q: want ErrTrashEntry, got %v", id, err)
		}
		if _, err := RestoreTrash(root, id); !errors.Is(err, ErrTrashEntry) {
			t.Errorf("restore id %q: want ErrTrashEntry, got %v", id, err)
		}
	}
}

func TestPurgeTrashDeletesOnlyOldEntries(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Old", "a.mkv"), "a")
	writeFile(t, filepath.Join(root, "New", "b.mkv"), "bb")
	if _, err := TrashFolder(root, filepath.Join(root, "Old"), "Old", trashNow.AddDate(0, 0, -10)); err != nil {
		t.Fatal(err)
	}
	if _, err := TrashFolder(root, filepath.Join(root, "New"), "New", trashNow); err != nil {
		t.Fatal(err)
	}
	n, freed, err := PurgeTrash(root, trashNow.AddDate(0, 0, -7))
	if err != nil || n != 1 || freed != 1 {
		t.Fatalf("purged %d freed %d err %v", n, freed, err)
	}
	list, _ := ListTrash(root)
	if len(list) != 1 || list[0].Label != "New" {
		t.Fatalf("left: %+v", list)
	}
}
