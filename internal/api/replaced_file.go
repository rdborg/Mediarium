package api

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/rdborg/mediarium/internal/cleanup"
	"github.com/rdborg/mediarium/internal/organizer"
)

// replacesOldFiles reports whether the import conflict policy lets a new file
// take the place of a title's earlier one. Only "overwrite" and "overwrite if
// better" (when the new file is better) do; by default an existing file is
// never replaced. Call it before the title's quality is updated, as the
// "better" check compares against the file that is there now.
func replacesOldFiles(policy organizer.ConflictPolicy, isBetter func() bool) bool {
	switch policy {
	case organizer.ConflictOverwrite:
		return true
	case organizer.ConflictOverwriteIfBetter:
		return isBetter != nil && isBetter()
	}
	return false
}

// removeReplacedFile deletes the file a title had before an import put a new
// one under another name (a different quality tag or container, say), so the
// library does not keep two copies. It only removes a plain file strictly
// inside root, reached through no link that leads out of it; never the new
// file; and never a file another movie or episode still points at. A problem
// is logged and never fails the import. It reports whether it removed the
// file.
func (s *Server) removeReplacedFile(root, old, kept string, movieID, seriesID int64, title string) bool {
	if root == "" || old == "" || filepath.Clean(old) == filepath.Clean(kept) {
		return false
	}
	info, err := os.Lstat(old)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if now, err := os.Stat(kept); err != nil || os.SameFile(info, now) {
		return false // the new file is missing, or is this very file under another name
	}
	if _, err := cleanup.InsideRoot(root, old); err != nil {
		slog.Warn("import: the earlier file is outside the library folder, left in place", "path", old, "err", err)
		return false
	}
	for _, f := range s.trackedFiles() {
		if filepath.Clean(f) == filepath.Clean(old) {
			return false // still another title's (or episode's) file
		}
	}
	if err := os.Remove(old); err != nil {
		slog.Warn("import: remove the file this one replaces", "path", old, "err", err)
		return false
	}
	cleanup.PruneEmptyFolders(root, filepath.Dir(old))
	message := fmt.Sprintf("%s: removed %s, which the new file replaced", title, filepath.Base(old))
	if seriesID > 0 {
		_ = s.QueueRepo.LogSeriesActivity(seriesID, "removed", message)
	} else {
		_ = s.QueueRepo.LogActivity(movieID, "removed", message)
	}
	return true
}
