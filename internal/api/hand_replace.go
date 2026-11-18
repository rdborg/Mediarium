package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/rdborg/mediarium/internal/cleanup"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
)

// A release a person picked by hand (Choose a release) and agreed to replace
// the title's file is an explicit decision: the title's "Better versions"
// setting and "If a file already exists" decide for automatic grabs only. The
// swap is as careful as any other replacement:
//
//   - the new file is written next to the old one under a temporary name and
//     is only used when it is complete (the old file is not touched until then);
//   - the old file and the files named after it go to the recycle bin (when it
//     is on) and the new file takes the old name, or the old file is replaced
//     in one rename (when the bin is off, as "overwrite" does);
//   - if the last step fails, the old file is put back;
//   - a file that another movie or episode has down is never replaced, and
//     nothing outside the library folder is touched.
//
// A new file with another name than the old one is imported the usual way and
// the old file goes afterwards (removeReplacedFile), once the new one is in the
// library.

// stageName is the temporary name a file gets beside dest while it is copied or
// linked, before it takes dest's place. It does not start with dest's name, so
// it is never taken for one of dest's own files (subtitles and the like).
func stageName(dest string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	suffix := ".mediarium-tmp"
	prefix := "." + hex.EncodeToString(b[:]) + "-"
	base := filepath.Base(dest)
	room := 255 - len(prefix) - len(suffix)
	for len(base) > room {
		_, size := lastRune(base)
		base = base[:len(base)-size]
	}
	return filepath.Join(filepath.Dir(dest), prefix+base+suffix)
}

// removeStaleStages deletes the temporary files an earlier swap of dest left
// beside it when Mediarium stopped part-way (see stageName). Only ones nothing
// has written to for an hour go, so a swap that is still running is not hurt.
func removeStaleStages(dest string, now time.Time) {
	dir := filepath.Dir(dest)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	// What stageName makes for this dest, without the random part.
	want := filepath.Base(stageName(dest))
	const randLen = 8
	for _, e := range entries {
		name := e.Name()
		if len(name) != len(want) || !e.Type().IsRegular() || name[0] != '.' || name[1+randLen] != '-' || name[2+randLen:] != want[2+randLen:] {
			continue
		}
		if _, err := hex.DecodeString(name[1 : 1+randLen]); err != nil {
			continue
		}
		if info, err := e.Info(); err != nil || now.Sub(info.ModTime()) < time.Hour {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}

func lastRune(s string) (rune, int) {
	r := []rune(s)
	if len(r) == 0 {
		return 0, 0
	}
	last := r[len(r)-1]
	return last, len(string(last))
}

// isPlainFile reports whether path is a regular file (not a link, not a folder).
func isPlainFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

// handPickedSwap says whether an import of dest must swap the file there: a
// release picked by hand replaces a file the title has at exactly that path.
func handPickedSwap(byHand bool, dest string, earlier []string) bool {
	if !byHand || dest == "" {
		return false
	}
	for _, e := range earlier {
		if e != "" && filepath.Clean(e) == filepath.Clean(dest) && isPlainFile(dest) {
			return true
		}
	}
	return false
}

// handPickedReplaces says whether a release picked by hand takes the place of
// a file the title has now (one of earlier, which still exists as a plain
// file), under whatever name the new file gets.
func handPickedReplaces(byHand bool, earlier []string) bool {
	if !byHand {
		return false
	}
	for _, e := range earlier {
		if e != "" && isPlainFile(e) {
			return true
		}
	}
	return false
}

// importSwapping imports src over the title's own file at dest (which exists
// as a plain file) the careful way described above. movieID/episodeIDs say
// whose file it is: a file that anyone else has down is refused. root is the
// library folder dest is in, label names the title for the recycle bin. It
// reports whether the old file went to the recycle bin.
func (s *Server) importSwapping(root, src, dest, label string, movieID int64, episodeIDs []int64) (organizer.ImportResult, bool, error) {
	if owner := otherFileOwner(s.trackedFileOwners(), dest, movieID, episodeIDs); owner != "" {
		return organizer.ImportResult{}, false, errFileOfAnother(owner, dest)
	}
	removeStaleStages(dest, time.Now())
	stage := stageName(dest)
	res, err := s.importDownloaded(src, stage, organizer.ConflictSkip, nil)
	if err == nil && res.Skipped {
		err = fmt.Errorf("%s is already there", filepath.Base(stage))
	}
	if err != nil {
		_ = os.Remove(stage)
		return organizer.ImportResult{}, false, err
	}
	// The new file is used only when it is whole.
	if si, e1 := os.Stat(src); e1 == nil {
		if di, e2 := os.Stat(stage); e2 != nil || di.Size() != si.Size() {
			_ = os.Remove(stage)
			return organizer.ImportResult{}, false, errors.New("the new file isn't complete, so the file you have was left as it is")
		}
	}
	binned := false
	var entry cleanup.TrashEntry
	if s.trashDays() > 0 {
		entry, err = cleanup.TrashFileWithSidecars(root, dest, label, time.Now())
		if err != nil {
			_ = os.Remove(stage)
			return organizer.ImportResult{}, false, fmt.Errorf("couldn't move the file you have to the recycle bin, so it was left as it is: %w", err)
		}
		binned = entry.ID != ""
	}
	if err := renameFile(stage, dest); err != nil {
		_ = os.Remove(stage)
		if binned {
			if _, rerr := cleanup.RestoreTrash(root, entry.ID); rerr != nil {
				slog.Error("import: couldn't put the earlier file back after a failed swap", "path", dest, "binEntry", entry.ID, "err", rerr)
				return organizer.ImportResult{}, false, fmt.Errorf("couldn't put the new file in place (%v) and the file you had is in the recycle bin: restore it from Activity > Recycle bin", err)
			}
		}
		return organizer.ImportResult{}, false, fmt.Errorf("couldn't put the new file in place, so the file you have was left as it is: %w", err)
	}
	return organizer.ImportResult{DestPath: dest, UsedHardlink: res.UsedHardlink}, binned, nil
}

// recordKept records, for a download whose file was not imported because the
// file the title has was kept, which file that is and what became of the
// downloaded data (see discardWorkDirReport).
func (s *Server) recordKept(queueID int64, keptFile, data string) {
	if err := s.QueueRepo.SetKept(queueID, keptFile, data); err != nil {
		slog.Warn("queue: couldn't record the kept file", "queueId", queueID, "err", err)
	}
}

// keptDataSentence is what the title's log says about the downloaded data of a
// release that was not imported.
func keptDataSentence(data, release string) string {
	switch data {
	case queue.KeptSeeding:
		return fmt.Sprintf("The release %s is still seeding, so its downloaded files stay until seeding is done.", release)
	case queue.KeptClient:
		return fmt.Sprintf("The release %s stays in your download client, which keeps its files.", release)
	}
	return fmt.Sprintf("The downloaded files of %s were deleted.", release)
}

// logKeptData adds the downloaded data's fate to the title's own log (not the
// global feed: the "kept" line there already names the file and the release).
func (s *Server) logKeptData(movieID, seriesID int64, data, release string) {
	_ = s.QueueRepo.LogItemEvent(queue.ItemEvent{MovieID: movieID, SeriesID: seriesID, Kind: "kept", Message: keptDataSentence(data, release), ItemOnly: true})
}

// importKeeps says why the import would turn away a new file for a title that
// has one, in an automatic grab. Release lists use it so they do not recommend
// (or mark as the best match) a release that would be downloaded and then
// thrown away. A release picked by hand can still replace the file.
type importKeeps int

const (
	keepsNothing importKeeps = iota
	keepsAlone               // the title is set to leave what it has alone
	keepsUnknown             // the quality of the file it has can't be told, so nothing counts as better
)

// importKeepsFor follows conflictPolicy: noUpgrade is the title's "Leave what I
// have alone"; currentKnown says the quality of the file there can be told.
// "Always overwrite" and "ask" take (or park) any new file, "overwrite if better"
// ignores the title's switch but needs a known quality, and the default skips
// a file unless the title may be upgraded.
func (s *Server) importKeepsFor(noUpgrade, currentKnown bool) importKeeps {
	v, _ := s.Settings.Get(settings.KeyImportConflictPolicy)
	switch v {
	case "overwrite", "ask":
		return keepsNothing
	case "overwrite_if_better":
	default:
		if noUpgrade {
			return keepsAlone
		}
	}
	if !currentKnown {
		return keepsUnknown
	}
	return keepsNothing
}

// notPickedReason is the note a release list shows for a release the import
// would turn away.
func notPickedReason(k importKeeps) (releaseReason, bool) {
	switch k {
	case keepsAlone:
		return releaseReason{"upgrade", "Not picked automatically: this title is set to leave what you have alone. You can still pick it by hand and replace the file."}, true
	case keepsUnknown:
		return releaseReason{"upgrade", "Not picked automatically: Mediarium can't tell the quality of the file you have, so it would keep it. You can still pick it by hand and replace the file."}, true
	}
	return releaseReason{}, false
}

// tvImportKeeps is importKeepsFor for a search of one episode or a season: it
// only applies when every episode asked for already has a file (an automatic
// grab fills the missing ones whatever the title's switch says).
func (s *Server) tvImportKeeps(series library.Series, season, episode int) importKeeps {
	eps, err := s.MovieRepo.ListEpisodes(series.ID)
	if err != nil {
		return keepsNothing
	}
	asked, known := 0, true
	for _, ep := range eps {
		if ep.Season != season || (episode > 0 && ep.Episode != episode) {
			continue
		}
		asked++
		if ep.Status != library.StatusDownloaded || ep.FilePath == "" {
			return keepsNothing
		}
		if _, ok := existingTier(ep.Quality, ep.ReleaseTitle, ep.FilePath); !ok {
			known = false
		}
	}
	if asked == 0 {
		return keepsNothing
	}
	return s.importKeepsFor(series.NoUpgrade, known)
}
