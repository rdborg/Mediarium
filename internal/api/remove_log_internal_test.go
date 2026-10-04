package api

import "testing"

func TestRemovedLogLine(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		deleted bool
		gone    removedFiles
		tracked int
		want    string
	}{
		{"files deleted, folder and files counted", "Inception", true, removedFiles{Files: 4, Folders: 2}, 1, "Inception removed from library. Its files were deleted (4 files, 2 folders)."},
		{"one file and one folder", "Heat", true, removedFiles{Files: 1, Folders: 1}, 1, "Heat removed from library. Its files were deleted (1 file, 1 folder)."},
		{"loose files, no folder", "Loose", true, removedFiles{Files: 3}, 1, "Loose removed from library. Its files were deleted (3 files)."},
		{"deleted but nothing was counted", "Odd", true, removedFiles{}, 1, "Odd removed from library. Its files were deleted."},
		{"moved to the recycle bin", "Heat", true, removedFiles{Files: 2, Folders: 1, Trashed: true}, 1, "Heat removed from library. Its files were moved to the recycle bin (2 files, 1 folder)."},
		{"files kept", "Alien", false, removedFiles{}, 1, "Alien removed from library. Its files were kept (1 file)."},
		{"show files kept", "Lost (series)", false, removedFiles{}, 24, "Lost (series) removed from library. Its files were kept (24 files)."},
		{"nothing on disk", "Wanted", false, removedFiles{}, 0, "Wanted removed from library. It had no files on disk."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := removedLogLine(tc.title, tc.deleted, tc.gone, tc.tracked); got != tc.want {
				t.Fatalf("line = %q, want %q", got, tc.want)
			}
		})
	}
}
