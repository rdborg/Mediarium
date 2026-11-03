package api

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/rdborg/mediarium/internal/cleanup"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
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

// keepsEarlierFile reports whether an import must be turned away because the
// title already has a file under another name that the new one may not
// replace. Without this a worse release (or any release, for a title set to
// keep its file) landed next to the file the title had and became the file the
// library points at, leaving two copies. A recorded file that is gone from
// the disk does not count: the new release then imports as usual.
func keepsEarlierFile(earlier, dest string, replaceEarlier bool) bool {
	if replaceEarlier || earlier == "" || filepath.Clean(earlier) == filepath.Clean(dest) {
		return false
	}
	info, err := os.Stat(earlier)
	return err == nil && info.Mode().IsRegular()
}

// existingTier is the quality an existing file counts as when a new release is
// compared with it. The quality stored with the title is used; a title
// without one (a file added by hand or by an older version) is read from the
// name of its file, which includes names in the TRaSH style such as
// "[Bluray-1080p]", and then from the release it came from. It reports false
// when the quality cannot be told: a release is then never taken for an
// upgrade over a file of unknown quality.
func existingTier(stored, release, file string) (quality.Tier, bool) {
	if t := quality.Tier(stored); t != "" && t != quality.TierUnknown {
		return t, true
	}
	names := []string{release}
	if file != "" {
		base := filepath.Base(file)
		names = append([]string{base[:len(base)-len(filepath.Ext(base))]}, names...)
	}
	for _, n := range names {
		if n == "" {
			continue
		}
		if t := quality.Classify(parser.Parse(n)); t != quality.TierUnknown {
			return t, true
		}
	}
	return "", false
}

// removeReplacedFile removes the file a title had before an import put a new
// one under another name (a different quality tag or container, say), so the
// library does not keep two copies. The file and the files named after it go
// to the recycle bin when that is on, otherwise they are deleted. It only removes a plain file strictly
// inside root, reached through no link that leads out of it; never the new
// file; and never a file another movie or episode still points at. A problem
// is logged and never fails the import. It reports whether it removed the
// file.
func (s *Server) removeReplacedFile(root, old, kept string, movieID, seriesID int64, title string) bool {
	if root == "" || old == "" || filepath.Clean(old) == filepath.Clean(kept) {
		return false
	}
	// Both have to be inside the library folder before anything on the disk is
	// looked at.
	if _, err := cleanup.InsideRoot(root, old); err != nil {
		if !errors.Is(err, fs.ErrNotExist) { // a file that is gone already needs nothing
			slog.Warn("import: the earlier file is outside the library folder, left in place", "path", old, "err", err)
		}
		return false
	}
	keptPath, err := cleanup.InsideRoot(root, kept)
	if err != nil {
		return false // the new file is missing, or not in this library folder
	}
	info, err := os.Lstat(old)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if now, err := os.Stat(keptPath); err != nil || os.SameFile(info, now) {
		return false // the new file is missing, or is this very file under another name
	}
	for _, f := range s.trackedFiles() {
		if filepath.Clean(f) == filepath.Clean(old) {
			return false // still another title's (or episode's) file
		}
	}
	if _, _, err := s.discardFileWithSidecars(root, old, title); err != nil {
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
