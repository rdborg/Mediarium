package api

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/origin"
	"github.com/rdborg/mediarium/internal/other"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/queue"
)

// The Other media type (#200): things that are not movies, shows, music or
// books. An item is made by name and is never looked up on TMDB, TVDB or any
// other service. Its files go into "<Other folder>/<name>/" under the names
// they came with. An NZB, a torrent or files already on the disk can be put
// into it, and the download line, history, blocklist and media server
// refresh treat it like every other kind.

func otherLink(id int64) string { return fmt.Sprintf("/other/%d", id) }

// otherFolder is where an item's files go: "<root>/<name>". A name another
// item's folder already has (two names that only differ in characters a file
// system can't hold) gets the item's number added.
func (s *Server) otherFolder(it other.Item) (string, error) {
	root := strings.TrimSpace(s.otherRoot())
	if root == "" {
		return "", errors.New("the Other folder isn't set. Choose one in Settings > Library and files > Folders")
	}
	if it.Path != "" && rootContaining([]string{root}, filepath.Clean(it.Path)) != "" && filepath.Clean(it.Path) != filepath.Clean(root) {
		// The item already has a folder (a rename leaves it where it is):
		// more files go there, not into a second folder.
		return filepath.Clean(it.Path), nil
	}
	mode, repl := s.illegalCharSettings()
	name := strings.TrimRight(organizer.Sanitize(it.Name, mode, repl), " .")
	if name == "" {
		name = fmt.Sprintf("Item %d", it.ID)
	}
	folder := filepath.Join(root, name)
	if all, err := s.OtherRepo.List(); err == nil {
		for _, o := range all {
			if o.ID != it.ID && o.Path != "" && strings.EqualFold(filepath.Clean(o.Path), folder) {
				return folder + fmt.Sprintf(" [%d]", it.ID), nil
			}
		}
	}
	return folder, nil
}

// otherJunk is a name that is part of a download's packaging, not of what was
// downloaded.
func otherJunk(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".par2", ".sfv", ".nzb", ".url", ".torrent", ".!qb", ".part":
		return true
	}
	return false
}

// otherFiles lists the files to keep from a finished download (a file, or a
// folder walked from top to bottom), in a steady order. Links count as the
// file they lead to only in a view of an external client's download.
func otherFiles(dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("look at %s: %w", dir, err)
	}
	if !info.IsDir() {
		if otherJunk(filepath.Base(dir)) {
			return nil, nil
		}
		return []string{dir}, nil
	}
	follow := isClientView(dir)
	var out []string
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			if p != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if otherJunk(d.Name()) || strings.EqualFold(d.Name(), clientContentFile) {
			return nil
		}
		if d.Type().IsRegular() {
			out = append(out, p)
		} else if follow && d.Type()&fs.ModeSymlink != 0 {
			if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
				out = append(out, p)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list the files in %s: %w", dir, err)
	}
	return out, nil
}

// placeOtherFiles puts files into an item's folder under their own names and
// records what the folder holds. It returns the placed files.
func (s *Server) placeOtherFiles(it other.Item, srcRoot string, files []string) (folder string, placed []string, err error) {
	if len(files) == 0 {
		return "", nil, errors.New("there are no files to add")
	}
	folder, err = s.otherFolder(it)
	if err != nil {
		return "", nil, err
	}
	names := audiobookFileNames(srcRoot, files)
	for i, src := range files {
		res, err := s.importDownloaded(src, filepath.Join(folder, names[i]), organizer.ConflictOverwrite, nil)
		if err != nil {
			return "", placed, fmt.Errorf("couldn't move %s into your library: %w", filepath.Base(src), err)
		}
		placed = append(placed, res.DestPath)
	}
	return folder, placed, nil
}

// settleOther records the folder an item now holds (counting what is in it,
// old files included) and tells the media servers.
func (s *Server) settleOther(it other.Item, folder string, placed []string) error {
	count, size := 0, int64(0)
	entries, _ := os.ReadDir(folder)
	for _, e := range entries {
		if e.IsDir() || otherJunk(e.Name()) {
			continue
		}
		if fi, err := e.Info(); err == nil {
			count++
			size += fi.Size()
		}
	}
	if err := s.OtherRepo.SetState(it.ID, other.StatusDownloaded, folder, count, size); err != nil {
		return err
	}
	s.otherImported(placed...)
	return nil
}

// otherImported tells the media servers files were added to the Other library
// (a server scans only a library that holds the folder).
func (s *Server) otherImported(files ...string) {
	s.mediaServersImported(mediaservers.MediaOther, files...)
}

// grabOther puts a release (an upload, an address or a magnet link) into the
// download line for an Other item.
func (s *Server) grabOther(it other.Item, title, url string, size int64, protocol indexers.Protocol, by origin.Origin) (int64, error) {
	if protocol == indexers.ProtocolTorrent && !s.torrentReleasesOn() {
		return 0, errTorrentsDisabled
	}
	s.grabMu.Lock()
	busy, err := s.QueueRepo.HasActiveForOther(it.ID)
	if err == nil && busy {
		err = errors.New("A download for this item is already running or waiting.")
	}
	var id int64
	if err == nil {
		id, err = s.QueueRepo.Enqueue(queue.Item{OtherID: it.ID, ReleaseTitle: title, NZBURL: url, SizeBytes: size, Protocol: queue.Protocol(protocol), Priority: queue.PriorityManual,
			Origin: s.grabOrigin(grabPicked, by, "")})
	}
	if err == nil {
		_ = s.OtherRepo.SetState(it.ID, other.StatusDownloading, "", 0, 0)
	}
	s.grabMu.Unlock()
	if err != nil {
		return 0, err
	}
	_ = s.QueueRepo.LogActivity(0, "grabbed", fmt.Sprintf("Downloading %s: %s", it.Name, title))
	s.dispatch.Kick()
	return id, nil
}

// restoredOtherStatus is what an item goes back to when a download for it
// ends without importing: downloaded when it still holds files, else missing.
func restoredOtherStatus(it other.Item) string {
	if it.Path != "" && it.FileCount > 0 {
		return other.StatusDownloaded
	}
	return other.StatusMissing
}

func (s *Server) runOtherPipeline(queueID int64, it other.Item, releaseTitle, downloadURL string, protocol indexers.Protocol) error {
	ctx, run := s.pipelines.begin(queueID)
	defer s.pipelines.end(queueID, run)
	if s.pausedBeforeStart(queueID) {
		return errPaused
	}
	restore := restoredOtherStatus(it)
	fail := func(stepErr error) error {
		if run.cancelled.Load() {
			_ = s.QueueRepo.SetStatus(queueID, queue.StatusFailed, errRemovedFromLibrary.Error())
			return errRemovedFromLibrary
		}
		if handled, err := s.settleInterrupted(queueID, run); handled {
			return err
		}
		if dup := asDuplicate(stepErr); dup != nil {
			s.settleDuplicate(queueID, dup)
			if !dup.holdsTitle() {
				_ = s.OtherRepo.SetState(it.ID, restore, "", 0, 0)
			}
			return stepErr
		}
		plain := plainerror.Message(stepErr)
		_ = s.QueueRepo.SetStatus(queueID, queue.StatusFailed, plain)
		noteDownloadFailure(queueID, it.Name, otherLink(it.ID), stepErr)
		s.doctorNoteFailure(queueID, stepErr)
		_ = s.OtherRepo.SetState(it.ID, restore, "", 0, 0)
		_ = s.QueueRepo.LogActivity(0, "failed", fmt.Sprintf("%s: %s", it.Name, plain))
		if s.failureNotifyWanted(stepErr, downloadURL, releaseTitle) {
			s.notifyItem("failed", notify.Item{Media: "other", Title: it.Name, Reason: plain, Release: releaseTitle, LinkPath: otherLink(it.ID)})
		}
		if isBadRelease(stepErr) {
			// Nothing searches for an Other item, so the entry only keeps this
			// release from being picked again; there is no automatic retry.
			_ = s.Blocklist.Add(blocklist.Entry{ReleaseTitle: releaseTitle, Protocol: string(protocol), Reason: stepErr.Error(), ReleaseID: s.queueReleaseID(queueID)})
		}
		return stepErr
	}

	_ = s.OtherRepo.SetState(it.ID, other.StatusDownloading, "", 0, 0)
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusDownloading, ""); err != nil {
		return fail(err)
	}
	dir, err := s.prepareDownload(ctx, queueID, downloadURL, protocol)
	if err != nil {
		return fail(err)
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	files, err := otherFiles(dir)
	if err != nil {
		return fail(err)
	}
	if len(files) == 0 {
		return fail(badRelease(errors.New("the download has no files in it")))
	}
	folder, placed, err := s.placeOtherFiles(it, dir, files)
	if err != nil {
		return fail(err)
	}
	if err := s.settleOther(it, folder, placed); err != nil {
		return fail(err)
	}
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, ""); err != nil {
		return fail(err)
	}
	s.cleanupWorkDir(dir, protocol)
	_ = s.QueueRepo.LogActivity(0, "imported", fmt.Sprintf("%s imported to %s", it.Name, folder))
	s.notifyItem("imported", notify.Item{Media: "other", Title: it.Name, SizeBytes: fileSize(placed...), Path: folder, Release: releaseTitle, LinkPath: otherLink(it.ID)})
	slog.Info("other: imported", "item", it.ID, "files", len(placed))
	return nil
}

// --- the API ---

type otherPayload struct {
	other.Item
	// Files are the names in the item's folder (only on the single-item read).
	Files []otherFilePayload `json:"files,omitempty"`
	// Active is true while a download for the item is running or waiting.
	Active bool `json:"active"`
}

type otherFilePayload struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
}

func (s *Server) otherPayloadOf(it other.Item, withFiles bool) otherPayload {
	p := otherPayload{Item: it}
	p.Active, _ = s.QueueRepo.HasActiveForOther(it.ID)
	if withFiles && it.Path != "" {
		if entries, err := os.ReadDir(it.Path); err == nil {
			for _, e := range entries {
				if e.IsDir() || otherJunk(e.Name()) {
					continue
				}
				fi, err := e.Info()
				if err != nil {
					continue
				}
				p.Files = append(p.Files, otherFilePayload{Name: e.Name(), SizeBytes: fi.Size()})
			}
		}
	}
	return p
}

func (s *Server) otherFromPath(w http.ResponseWriter, r *http.Request) (other.Item, bool) {
	if !s.requireModule(w, moduleOther) {
		return other.Item{}, false
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid item.")
		return other.Item{}, false
	}
	it, err := s.OtherRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That item isn't in your library.")
		return other.Item{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return other.Item{}, false
	}
	return it, true
}

func writeOtherNameError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, other.ErrName):
		writeError(w, http.StatusBadRequest, "Give the item a name of up to 200 characters.")
	case errors.Is(err, other.ErrExists):
		writeError(w, http.StatusConflict, "An item with that name is already in your library.")
	default:
		return false
	}
	return true
}

// handleListOther lists the Other library.
func (s *Server) handleListOther(w http.ResponseWriter, r *http.Request) {
	if !s.requireModule(w, moduleOther) {
		return
	}
	items, err := s.OtherRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]otherPayload, 0, len(items))
	for _, it := range items {
		p := s.otherPayloadOf(it, false)
		p.Path = fileLabel(r, p.Path) // members see the folder's name, not where the server keeps it
		out = append(out, p)
	}
	root := s.otherRoot()
	if !isAdminRequest(r) {
		root = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "root": root})
}

func (s *Server) handleGetOther(w http.ResponseWriter, r *http.Request) {
	it, ok := s.otherFromPath(w, r)
	if !ok {
		return
	}
	p := s.otherPayloadOf(it, true)
	p.Path = fileLabel(r, p.Path)
	writeJSON(w, http.StatusOK, p)
}

// handleAddOther makes an item from a name.
func (s *Server) handleAddOther(w http.ResponseWriter, r *http.Request) {
	if !s.requireModule(w, moduleOther) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Give the item a name.")
		return
	}
	userID, _ := requester(r)
	o := addOrigin(r)
	it, err := s.OtherRepo.Add(req.Name, userID, o.Kind, o.Name)
	if err != nil {
		if !writeOtherNameError(w, err) {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("%s added to the library%s", it.Name, byline(r)))
	writeJSON(w, http.StatusCreated, s.otherPayloadOf(it, false))
}

func byline(r *http.Request) string {
	_, b := requester(r)
	return b
}

func (s *Server) handleRenameOther(w http.ResponseWriter, r *http.Request) {
	it, ok := s.otherFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Give the item a name.")
		return
	}
	if err := s.OtherRepo.Rename(it.ID, req.Name); err != nil {
		if !writeOtherNameError(w, err) {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	got, _ := s.OtherRepo.Get(it.ID)
	writeJSON(w, http.StatusOK, s.otherPayloadOf(got, false))
}

func (s *Server) handleDeleteOther(w http.ResponseWriter, r *http.Request) {
	it, ok := s.otherFromPath(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("deleteFiles") == "true" && it.Path != "" {
		root := s.otherRoot()
		if err := checkRemovable(root, []string{it.Path}); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if _, err := s.discardFolder(root, it.Path, it.Name); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	// A running download would put its files into the folder of an item that
	// is gone, so it is stopped first (and its working folder cleared).
	if _, err := s.removeDownloadsWhere(func(q queue.Item) bool { return q.OtherID == it.ID }, it.Name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.OtherRepo.Delete(it.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.QueueRepo.LogActivity(0, "removed", fmt.Sprintf("%s removed from the library", it.Name))
	writeJSON(w, http.StatusOK, nil)
}

// handleImportOther moves files that are already on the server (a file, or a
// folder of them) into an item's folder. The path has to be inside a library
// folder or the downloads folder.
func (s *Server) handleImportOther(w http.ResponseWriter, r *http.Request) {
	it, ok := s.otherFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Path) == "" {
		writeError(w, http.StatusBadRequest, "Choose the file or folder to import.")
		return
	}
	src, ok := pathInside(append(s.browseRoots(), s.otherRoot()), strings.TrimSpace(req.Path))
	if !ok {
		writeError(w, http.StatusBadRequest, "That file or folder can't be found, or it isn't inside your downloads or library folders.")
		return
	}
	if isAnyRoot(append(s.browseRoots(), s.otherRoot()), src) {
		writeError(w, http.StatusBadRequest, "Choose a file or folder inside your library or downloads folder, not the folder itself.")
		return
	}
	if folder, ferr := s.otherFolder(it); ferr == nil && rootContaining([]string{folder}, src) != "" {
		writeError(w, http.StatusBadRequest, "Those files are already in this item's folder.")
		return
	}
	if busy, _ := s.QueueRepo.HasActiveForOther(it.ID); busy {
		writeError(w, http.StatusConflict, "A download for this item is still running. Wait for it to finish, then import.")
		return
	}
	files, err := otherFiles(src)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, "There are no files to import there.")
		return
	}
	root := src
	if fi, err := os.Stat(src); err == nil && !fi.IsDir() {
		root = filepath.Dir(src)
	}
	folder, placed, err := s.placeOtherFiles(it, root, files)
	if err == nil {
		err = s.settleOther(it, folder, placed)
	}
	if err != nil {
		status := http.StatusInternalServerError
		if isLibraryRootError(err) {
			status = http.StatusConflict
		}
		writeError(w, status, plainerror.Message(err))
		return
	}
	_ = s.QueueRepo.LogActivity(0, "imported", fmt.Sprintf("%s: %d files imported to %s", it.Name, len(placed), folder))
	got, _ := s.OtherRepo.Get(it.ID)
	writeJSON(w, http.StatusOK, s.otherPayloadOf(got, true))
}

// otherNameFromRelease makes an item's name out of a release name: dots and
// underscores between words become spaces, so "Some.Lecture.Series" reads as a
// name. A name that already has spaces is left alone.
func otherNameFromRelease(release string) string {
	name := cleanReleaseName(release)
	if !strings.Contains(name, " ") {
		name = strings.Map(func(r rune) rune {
			if r == '.' || r == '_' {
				return ' '
			}
			return r
		}, name)
	}
	name = strings.Join(strings.Fields(name), " ")
	name = strings.TrimFunc(name, func(r rune) bool { return unicode.IsSpace(r) || r == '-' })
	if len([]rune(name)) > other.MaxNameLen {
		name = string([]rune(name)[:other.MaxNameLen])
	}
	return name
}

// isAnyRoot says whether p is one of the folders itself (not something in it).
func isAnyRoot(roots []string, p string) bool {
	for _, r := range roots {
		if r != "" && filepath.Clean(r) == filepath.Clean(p) {
			return true
		}
	}
	return false
}
