package api

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/cleanup"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/organizer"
)

// Removing a title from the library leaves nothing behind: its downloads
// are cancelled and their working folders deleted, and (when asked) its
// folder in the library goes with every subtitle, .nfo and artwork in it.

// cancelWait is how long removing a title waits for each of its running
// downloads to stop before it deletes their folders.
const cancelWait = 30 * time.Second

// removeDownloadsFor cancels and forgets every download of the movie
// (movieID) or show (seriesID) being removed: a running download is
// stopped (a torrent is taken out of the engine), its working folder in the
// downloads area is deleted (failed downloads' leftovers too) and its
// queue entry is removed. It returns how many downloads it cancelled.
func (s *Server) removeDownloadsFor(movieID, seriesID int64, title string) (int, error) {
	items, err := s.QueueRepo.List()
	if err != nil {
		return 0, err
	}
	cancelled := 0
	for _, it := range items {
		if (movieID == 0 || it.MovieID != movieID) && (seriesID == 0 || it.SeriesID != seriesID) {
			continue
		}
		running := s.pipelines.cancel(it.ID, cancelWait)
		if s.torrents.stopQueue(it.ID) {
			running = true
		}
		if running || isActiveStatus(it.Status) {
			cancelled++
		}
		s.removeWorkDir(s.workDirFor(it.ID))
		_ = s.QueueRepo.Delete(it.ID)
	}
	if cancelled > 0 {
		_ = s.QueueRepo.LogActivity(0, "removed", fmt.Sprintf("Cancelled %s of %s and removed their files from the downloads folder", plural(cancelled, "download"), title))
	}
	return cancelled, nil
}

// errFilesOutsideLibrary is returned when a title's file is not inside the
// library folder, so it is not deleted.
var errFilesOutsideLibrary = errors.New("outside the library folder")

// checkRemovable refuses before anything is changed when one of a title's
// files lies outside root (or is reached through a symbolic link leading
// out): such a file is never deleted. Missing files are fine.
func checkRemovable(root string, files []string) error {
	if root == "" && len(files) > 0 {
		return fmt.Errorf("the library folder isn't set, so %s can't be deleted safely: %w", files[0], errFilesOutsideLibrary)
	}
	for _, f := range files {
		if _, err := os.Lstat(f); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if _, err := cleanup.InsideRoot(root, f); err != nil {
			return fmt.Errorf("%s is %w %s, so it was left in place", f, errFilesOutsideLibrary, root)
		}
	}
	return nil
}

// removeMovieFiles deletes a removed movie's files: its whole folder when
// that folder is the movie's own, otherwise just the video and the
// subtitles, .nfo and artwork named after it.
func (s *Server) removeMovieFiles(m library.Movie) (removedFiles, error) {
	if m.FilePath == "" {
		return removedFiles{}, nil
	}
	root := s.moviesRoot()
	own := []string{m.FilePath}
	folder := filepath.Dir(m.FilePath)
	if s.isTitleFolder(root, folder, own, func(name string) bool { return matchesMovie(name, m) }) {
		return removeTitleFolder(root, folder)
	}
	gone, err := cleanup.RemoveFileWithSidecars(root, m.FilePath)
	return removedFiles{Files: len(gone)}, err
}

// removedFiles counts what removing a title's files deleted.
type removedFiles struct {
	Files   int
	Folders int
}

// removeTitleFolder deletes a title's own folder and counts what was in it.
func removeTitleFolder(root, folder string) (removedFiles, error) {
	var n removedFiles
	_ = filepath.WalkDir(folder, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			n.Folders++
		} else {
			n.Files++
		}
		return nil
	})
	if _, err := cleanup.RemoveFolder(root, folder); err != nil {
		return removedFiles{}, err
	}
	return n, nil
}

// removedLogLine is the activity line written when a title leaves the
// library. It always says whether the files were deleted or kept, and how
// many when that is known: deleted counts what was removed, kept counts the
// files the library had on record.
func removedLogLine(name string, filesDeleted bool, deleted removedFiles, tracked int) string {
	line := name + " removed from library."
	switch {
	case filesDeleted:
		return line + " Its files were deleted" + removedCounts(deleted.Files, deleted.Folders) + "."
	case tracked > 0:
		return line + " Its files were kept" + removedCounts(tracked, 0) + "."
	default:
		return line + " It had no files on disk."
	}
}

// removedCounts is " (3 files, 1 folder)", or nothing when both are zero.
func removedCounts(files, folders int) string {
	var parts []string
	if files > 0 {
		parts = append(parts, plural(files, "file"))
	}
	if folders > 0 {
		parts = append(parts, plural(folders, "folder"))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// seasonFolderName matches a season folder ("Season 01", "Season 1", "S01",
// "Specials").
var seasonFolderName = regexp.MustCompile(`(?i)^(season[ ._-]*\d+|s\d+|specials)$`)

// removeSeriesFiles deletes a removed show's files: the show's folder with
// every season, subtitle, .nfo and artwork when that folder is the show's
// own, otherwise each episode file with the files named after it.
func (s *Server) removeSeriesFiles(series library.Series, files []string) (removedFiles, error) {
	if len(files) == 0 {
		return removedFiles{}, nil
	}
	root := s.tvRoot()
	folder := commonDir(files)
	if seasonFolderName.MatchString(filepath.Base(folder)) {
		folder = filepath.Dir(folder)
	}
	if s.isTitleFolder(root, folder, files, func(name string) bool { return matchesShow(name, series) }) {
		return removeTitleFolder(root, folder)
	}
	var n removedFiles
	for _, f := range files {
		gone, err := cleanup.RemoveFileWithSidecars(root, f)
		n.Files += len(gone)
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// extrasFolder matches the folders media servers use for a title's extras.
var extrasFolder = regexp.MustCompile(`(?i)^(extras|featurettes|behind the scenes|deleted scenes|interviews|scenes|shorts|trailers|other|sample|samples)$`)

// isTitleFolder reports whether folder belongs to one title only, so it
// can be deleted as a whole: it lies strictly inside root, no other title
// in the library has a file in it, and every video in it (apart from own,
// samples and extras) is named after the title (ours).
func (s *Server) isTitleFolder(root, folder string, own []string, ours func(name string) bool) bool {
	if root == "" {
		return false
	}
	realFolder, err := cleanup.InsideRoot(root, folder)
	if err != nil {
		return false
	}
	mine := map[string]bool{}
	for _, f := range own {
		mine[filepath.Clean(f)] = true
	}
	for _, f := range s.trackedFiles() {
		if mine[filepath.Clean(f)] {
			continue
		}
		if real, err := filepath.EvalSymlinks(f); err == nil && isWithin(realFolder, real) {
			return false
		}
		if isWithin(filepath.Clean(folder), filepath.Clean(f)) {
			return false
		}
	}
	shared := false
	_ = filepath.WalkDir(folder, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			shared = true
			return fs.SkipAll
		}
		if d.IsDir() {
			if p != folder && extrasFolder.MatchString(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if mine[filepath.Clean(p)] || !organizer.IsVideoFile(p) || organizer.IsSample(p) {
			return nil
		}
		if !ours(filepath.Base(p)) {
			shared = true
			return fs.SkipAll
		}
		return nil
	})
	return !shared
}

// trackedFiles lists every file the library knows about (movies and
// episodes), for isTitleFolder.
func (s *Server) trackedFiles() []string {
	var out []string
	if movies, err := s.MovieRepo.List(); err == nil {
		for _, m := range movies {
			if m.FilePath != "" {
				out = append(out, m.FilePath)
			}
		}
	}
	if shows, err := s.MovieRepo.ListSeries(); err == nil {
		for _, sr := range shows {
			eps, err := s.MovieRepo.ListEpisodes(sr.ID)
			if err != nil {
				continue
			}
			for _, ep := range eps {
				if ep.FilePath != "" {
					out = append(out, ep.FilePath)
				}
			}
		}
	}
	return out
}

// commonDir is the deepest folder holding every one of files.
func commonDir(files []string) string {
	dir := filepath.Dir(filepath.Clean(files[0]))
	for _, f := range files[1:] {
		for !isWithin(dir, filepath.Clean(f)) {
			parent := filepath.Dir(dir)
			if parent == dir {
				return dir
			}
			dir = parent
		}
	}
	return dir
}

// isWithin reports whether path lies strictly inside dir.
func isWithin(dir, path string) bool {
	r, err := filepath.Rel(dir, path)
	return err == nil && r != "." && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) && !filepath.IsAbs(r)
}

// countDistinct is how many different paths are in files (a file holding
// several episodes is listed once per episode).
func countDistinct(files []string) int {
	seen := map[string]bool{}
	for _, f := range files {
		seen[f] = true
	}
	return len(seen)
}
