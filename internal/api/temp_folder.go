package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/rdborg/mediarium/internal/cleanup"
	"github.com/rdborg/mediarium/internal/fsinfo"
	"github.com/rdborg/mediarium/internal/settings"
)

// The temporary download folder (Settings > Downloading) holds the unfinished
// part of downloads: the parts being fetched, PAR2 repair and the unpacked
// files. It is meant for a fast drive. Like the downloads folder it gets an
// "incomplete" folder of its own inside, so clean-up never has to judge
// anything else that lives in the chosen folder. Left empty, everything stays
// in downloads/incomplete as before.

// maxPreviousTempFolders is how many earlier temporary folders are remembered.
const maxPreviousTempFolders = 10

// tempBase is the temporary download folder from Settings, "" when none is set.
func (s *Server) tempBase() string {
	v, _ := s.Settings.Get(settings.KeyDownloadsTempDir)
	v = strings.TrimSpace(v)
	if v == "" || !filepath.IsAbs(v) {
		return ""
	}
	return filepath.Clean(v)
}

// tempWorkRoot is where downloads are worked on inside a temporary folder.
func tempWorkRoot(base string) string { return filepath.Join(base, "incomplete") }

// previousTempBases are the temporary folders used before the current one.
func (s *Server) previousTempBases() []string {
	raw, _ := s.Settings.Get(settings.KeyDownloadsTempPrev)
	var list []string
	if raw == "" || json.Unmarshal([]byte(raw), &list) != nil {
		return nil
	}
	out := list[:0]
	for _, p := range list {
		if p != "" && filepath.IsAbs(p) {
			out = append(out, filepath.Clean(p))
		}
	}
	return out
}

// rememberTempBase keeps a folder that stops being the temporary folder, so
// downloads that began there are still found, finished and cleaned up.
func (s *Server) rememberTempBase(old string) {
	s.rememberTempBaseIn(s.Settings, old)
}

// rememberTempBaseIn is rememberTempBase into an update saved with other settings.
func (s *Server) rememberTempBaseIn(st settingSetter, old string) {
	old = strings.TrimSpace(old)
	if old == "" {
		return
	}
	list := []string{filepath.Clean(old)}
	for _, p := range s.previousTempBases() {
		if p != list[0] {
			list = append(list, p)
		}
	}
	if len(list) > maxPreviousTempFolders {
		list = list[:maxPreviousTempFolders]
	}
	raw, _ := json.Marshal(list)
	if err := st.Set(settings.KeyDownloadsTempPrev, string(raw), false); err != nil {
		slog.Warn("settings: couldn't remember the earlier temporary folder", "err", err)
	}
}

// workRoots lists every folder downloads may be worked on in: the current
// temporary folder, the downloads folder's own, and earlier temporary folders.
func (s *Server) workRoots() []string {
	var roots []string
	seen := map[string]bool{}
	add := func(r string) {
		r = filepath.Clean(r)
		if r != "" && r != "." && !seen[r] {
			seen[r] = true
			roots = append(roots, r)
		}
	}
	if b := s.tempBase(); b != "" {
		add(tempWorkRoot(b))
	}
	add(s.downloadsIncompleteDir())
	for _, b := range s.previousTempBases() {
		add(tempWorkRoot(b))
	}
	return roots
}

// newWorkRoot is the folder a download that has not started yet is worked on in.
func (s *Server) newWorkRoot() string {
	if b := s.tempBase(); b != "" {
		return tempWorkRoot(b)
	}
	return s.downloadsIncompleteDir()
}

// workDirIn is queue item id's folder inside a work root (see workDirFor).
func (s *Server) workDirIn(root string, id int64) string {
	if s.TorrentOpts != nil {
		if o, ok, _ := s.TorrentOpts.Get(id); ok && o.Subfolder != "" {
			return filepath.Join(root, o.Subfolder, fmt.Sprintf("queue-%d", id))
		}
	}
	return filepath.Join(root, fmt.Sprintf("queue-%d", id))
}

// existingWorkDir finds the folder queue item id already has, in whichever work
// root it was started in. A change of the temporary folder therefore never
// loses a download that is under way.
func (s *Server) existingWorkDir(id int64) (string, bool) {
	for _, root := range s.workRoots() {
		for _, d := range []string{s.workDirIn(root, id), filepath.Join(root, fmt.Sprintf("queue-%d", id))} {
			if _, err := os.Lstat(d); err == nil {
				return d, true
			}
		}
	}
	return "", false
}

// downloadsAreas are the parts of the disk clean-up may touch: the work root of
// the downloads folder and of every temporary folder in use now or before.
// Work roots whose folder is gone (a drive that isn't mounted) are skipped.
func (s *Server) downloadsAreas() []cleanup.Area {
	var areas []cleanup.Area
	for _, root := range s.workRoots() {
		a := s.downloadsArea()
		a.Work = root
		areas = append(areas, a)
	}
	return areas
}

// areaFor is the clean-up area whose work root holds path.
func (s *Server) areaFor(path string) (cleanup.Area, bool) {
	path = filepath.Clean(path)
	for _, a := range s.downloadsAreas() {
		if isWithin(filepath.Clean(a.Work), path) {
			return a, true
		}
	}
	return cleanup.Area{}, false
}

// checkTempReady refuses to start a download in a temporary folder that isn't
// there: a drive that isn't mounted would otherwise be replaced by folders made
// inside the container, which fill the wrong disk and vanish with it.
func (s *Server) checkTempReady(dir string) error {
	base := s.tempBase()
	if base == "" || !isWithin(tempWorkRoot(base), filepath.Clean(dir)) {
		return nil
	}
	if _, err := os.Lstat(dir); err == nil {
		return nil // already started there
	}
	info, err := os.Stat(base)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("the temporary download folder %s isn't there. Check that its drive is mounted, or clear the folder in Settings > Downloading", base)
	}
	if err := fsinfo.CheckWritable(base); err != nil {
		return fmt.Errorf("Mediarium can't write to the temporary download folder %s. Check its permissions", base)
	}
	return nil
}

// checkTempFolder returns a plain message when v can't be the temporary
// download folder, "" when it can.
func (s *Server) checkTempFolder(v string) string {
	if m := firstProblem(checkAbsPath(v, "/mnt/cache/downloads"), checkMaxLen(v, "The temporary download folder", maxPathLen)); m != "" {
		return m
	}
	clean := filepath.Clean(v)
	if m := s.checkLibraryFolder(filepath.ToSlash(clean), "The temporary download folder"); m != "" {
		return m
	}
	info, err := os.Lstat(clean)
	switch {
	case err != nil:
		return "That folder doesn't exist. Create it first, and when Mediarium runs in Docker, mount it into the container."
	case info.Mode()&os.ModeSymlink != 0:
		return "The temporary download folder can't be a shortcut (symbolic link). Pick the real folder."
	case !info.IsDir():
		return "That path is a file, not a folder."
	}
	if err := fsinfo.CheckWritable(clean); err != nil {
		return "Mediarium can't write to that folder. Check its permissions, or pick another folder."
	}
	real, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "Mediarium can't read that folder. Pick another one."
	}
	return s.tempFolderClash(real)
}

// tempFolderClash compares the real location of a candidate temporary folder
// with the folders it must stay apart from.
func (s *Server) tempFolderClash(real string) string {
	realOf := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	related := func(a, b string) bool { return a == b || isWithin(a, b) || isWithin(b, a) }
	library := append(s.videoRoots(), s.musicRoot(), s.ebooksRoot(), s.audiobooksRoot(), s.otherRoot())
	for _, f := range library {
		if f != "" && related(real, realOf(f)) {
			return fmt.Sprintf("The temporary download folder can't be, hold or sit inside a library folder (%s). Pick a folder of its own.", f)
		}
	}
	if c := s.cfg.ConfigDir; c != "" && related(real, realOf(c)) {
		return fmt.Sprintf("The temporary download folder can't be, hold or sit inside Mediarium's own settings folder (%s). Pick a different folder.", c)
	}
	if d := s.downloadsRoot(); d != "" {
		rd := realOf(d)
		if real == rd {
			return fmt.Sprintf("The temporary download folder has to be a different folder from the downloads folder (%s).", d)
		}
		if real == realOf(filepath.Join(d, "incomplete")) || isWithin(realOf(filepath.Join(d, "incomplete")), real) {
			return "The temporary download folder can't be inside the downloads folder's own incomplete folder."
		}
	}
	return ""
}
