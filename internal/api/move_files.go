package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/cleanup"
	"github.com/rdborg/mediarium/internal/fsinfo"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metafiles"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/queue"
)

// Moving titles to another library folder (the Library page's bulk "Move to
// folder"). Two things can happen, and the person chooses:
//
//   - only the folder changes: the next files of each title go to the new
//     folder and the files already there stay where they are (POST
//     /api/library/bulk/folder);
//   - the files already there are moved too (move-files): shown first as a
//     preview, then done in the background. On the same drive that is a
//     rename; to another drive each file is copied, checked against the
//     original, and only then is the original removed. Nothing is overwritten,
//     a title that cannot be moved completely is left exactly as it was.

const (
	// moveSpaceMargin is kept free on the target drive over the size of what is copied.
	moveSpaceMargin = 512 << 20
	// moveProblemsShown is how many problems the preview and the status list.
	moveProblemsShown = 200
	// moveMaxTitles is the most titles one move takes.
	moveMaxTitles = 50
)

type bulkFolderRequest struct {
	Items    []bulkItem `json:"items"`
	RootPath string     `json:"rootPath"`
}

// sameKind checks that a selection is only movies or only shows (each has its
// own list of folders) and says which.
func sameKind(refs []library.Ref) (string, error) {
	kind := refs[0].Kind
	for _, r := range refs {
		if r.Kind != kind {
			return "", errors.New("Pick movies or shows, not both: they have different folders.")
		}
	}
	return kind, nil
}

// handleBulkFolder is POST /api/library/bulk/folder: the chosen titles keep
// their files where they are, and their next files go to rootPath.
func (s *Server) handleBulkFolder(w http.ResponseWriter, r *http.Request) {
	var req bulkFolderRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	refs, err := parseBulkItems(req.Items)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	kind, err := sameKind(refs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	stored, problem := s.folderForKind(kind, req.RootPath)
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	res := bulkResult{Failed: []bulkFailure{}}
	for _, ref := range refs {
		title, reason := s.setTitleFolder(ref, stored)
		if reason != "" {
			res.Failed = append(res.Failed, bulkFailure{Kind: ref.Kind, ID: ref.ID, Title: title, Reason: reason})
			continue
		}
		res.Updated++
	}
	res.Message = bulkMessage(res.Updated, res.Failed, "title")
	writeJSON(w, http.StatusOK, res)
}

// folderForKind checks a folder against the ones on offer for movies or shows
// and returns what to store for the title ("" is the main folder).
func (s *Server) folderForKind(kind, want string) (string, string) {
	if kind == library.KindMovie {
		return s.checkFolderChoice(want, s.movieFolders(), "movie")
	}
	return s.checkFolderChoice(want, s.tvFolders(), "show")
}

// setTitleFolder changes where a title's next files go. It returns the title's
// name and, when it could not be done, why.
func (s *Server) setTitleFolder(ref library.Ref, stored string) (string, string) {
	if ref.Kind == library.KindMovie {
		m, err := s.MovieRepo.Get(ref.ID)
		if err != nil {
			return "", reasonNotFound
		}
		if m.CopyOf != 0 {
			return m.Title, "A second copy stays in the folder for second copies."
		}
		if err := s.MovieRepo.SetRoot(ref.ID, stored); err != nil {
			return m.Title, plainerror.Message(err)
		}
		return m.Title, ""
	}
	sr, err := s.MovieRepo.GetSeries(ref.ID)
	if err != nil {
		return "", reasonNotFound
	}
	if sr.CopyOf != 0 {
		return sr.Title, "A second copy stays in the folder for second copies."
	}
	if err := s.MovieRepo.SetSeriesRoot(ref.ID, stored); err != nil {
		return sr.Title, plainerror.Message(err)
	}
	return sr.Title, ""
}

// ---- Planning --------------------------------------------------------------

type moveRequest struct {
	Items    []bulkItem `json:"items"`
	RootPath string     `json:"rootPath"`
}

// moveTitle is one title in the preview.
type moveTitle struct {
	Kind  string `json:"kind"`
	ID    int64  `json:"id"`
	Title string `json:"title"`
	// From and To are the folder (or file) that moves and where it goes.
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
	// Method is "rename" (same drive: instant), "copy" (another drive: each
	// file is copied, checked and then the original removed) or "" when only
	// the folder setting changes (nothing is on disk to move).
	Method string `json:"method"`
	// Problem says why this title will not be moved; it is left as it is.
	Problem string `json:"problem,omitempty"`
	// Note is something to know that does not stop the move.
	Note string `json:"note,omitempty"`
	// Linked is how many of the files are hard-linked to something else (a
	// torrent that is still seeding). A rename keeps the link; a copy to
	// another drive does not.
	Linked   int    `json:"linkedFiles,omitempty"`
	LinkNote string `json:"linkNote,omitempty"`

	units   []organizer.MoveUnit
	oldRoot string
	newFile []string // where the title's video files end up (for the media servers)
}

type movePreview struct {
	Titles    []moveTitle `json:"titles"`
	CanMove   int         `json:"canMove"`           // titles that will be moved (or just get the new folder)
	Blocked   int         `json:"blocked"`           // titles that will be left as they are
	Files     int         `json:"files"`             // files that will move
	Bytes     int64       `json:"bytes"`             // their size
	CopyBytes int64       `json:"copyBytes"`         // of which to another drive
	FreeBytes int64       `json:"freeBytes"`         // free on the new folder's drive
	Problem   string      `json:"problem,omitempty"` // a reason that stops the whole move
	Truncated int         `json:"truncated,omitempty"`
}

// treeStats counts the files under path (1 for a file) and their size.
func treeStats(path string) (int, int64) {
	files, size := 0, int64(0)
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			files++
			size += info.Size()
		}
		return nil
	})
	return files, size
}

// nearestFolder is path, or the closest folder above it that exists.
func nearestFolder(path string) string {
	p := filepath.Clean(path)
	for {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
		up := filepath.Dir(p)
		if up == p {
			return p
		}
		p = up
	}
}

// busyTitles are the ids of the movies and shows with a download going on. If
// the downloads cannot be read it says so: a title is not moved while it is
// unknown whether a download is about to land in its folder.
func (s *Server) busyTitles() (movies, shows map[int64]bool, err error) {
	movies, shows = map[int64]bool{}, map[int64]bool{}
	items, err := s.QueueRepo.List()
	if err != nil {
		return nil, nil, err
	}
	for _, it := range items {
		switch it.Status {
		case queue.StatusQueued, queue.StatusDownloading, queue.StatusImporting, queue.StatusPaused, queue.StatusConflict:
			// Still on its way into the folder (a paused download or a file waiting
			// for an answer is picked up again later).
		default:
			continue
		}
		if it.SeriesID > 0 {
			shows[it.SeriesID] = true
		} else if it.MovieID > 0 {
			movies[it.MovieID] = true
		}
	}
	return movies, shows, nil
}

// planMove works out what moving one title to newRoot (the folder as listed in
// Settings) means. It changes nothing.
func (s *Server) planMove(ref library.Ref, newRoot, stored string, busyMovies, busyShows map[int64]bool) moveTitle {
	t := moveTitle{Kind: ref.Kind, ID: ref.ID}
	var (
		files   []string
		oldRoot string
	)
	if ref.Kind == library.KindMovie {
		m, err := s.MovieRepo.Get(ref.ID)
		if err != nil {
			t.Problem = reasonNotFound
			return t
		}
		t.Title = movieLabel(m)
		switch {
		case m.CopyOf != 0:
			t.Problem = "A second copy stays in the folder for second copies."
			return t
		case busyMovies[m.ID]:
			t.Problem = "A download for it is running. Try again when it has finished."
			return t
		case m.FilePath == "":
			return t // nothing on disk: only the folder setting changes
		}
		files = []string{m.FilePath}
		oldRoot = rootContaining(s.movieFolders(), m.FilePath)
	} else {
		sr, err := s.MovieRepo.GetSeries(ref.ID)
		if err != nil {
			t.Problem = reasonNotFound
			return t
		}
		t.Title = sr.Title
		switch {
		case sr.CopyOf != 0:
			t.Problem = "A second copy stays in the folder for second copies."
			return t
		case busyShows[sr.ID]:
			t.Problem = "A download for it is running. Try again when it has finished."
			return t
		}
		eps, err := s.MovieRepo.ListEpisodes(sr.ID)
		if err != nil {
			t.Problem = plainerror.Message(err)
			return t
		}
		seen := map[string]bool{}
		for _, e := range eps {
			if e.FilePath != "" && !seen[e.FilePath] {
				seen[e.FilePath] = true
				files = append(files, e.FilePath)
			}
		}
		if len(files) == 0 {
			return t
		}
		sort.Strings(files)
		oldRoot = rootContaining(s.tvFolders(), files[0])
	}

	if oldRoot == "" {
		t.Problem = "Its files are not inside one of your library folders, so Mediarium leaves them where they are."
		return t
	}
	t.oldRoot = oldRoot
	if filepath.Clean(oldRoot) == filepath.Clean(newRoot) {
		return t // already in that folder: only the folder setting needs saving
	}
	for _, f := range files {
		if rootContaining([]string{oldRoot}, f) == "" {
			t.Problem = "Its files are in more than one library folder. Move it by hand, or use the folder choice without moving the files."
			return t
		}
		if _, err := os.Stat(f); err != nil {
			t.Problem = fmt.Sprintf("A file is not where Mediarium expects it (%s).", filepath.Base(f))
			return t
		}
	}
	if info, err := os.Stat(newRoot); err != nil || !info.IsDir() {
		t.Problem = "The new folder doesn't exist."
		return t
	}
	if err := s.checkLibraryRoot(newRoot); err != nil {
		t.Problem = err.Error()
		return t
	}

	// What moves: the title's own folder, or just its files.
	var units []organizer.MoveUnit
	var outside int
	if ref.Kind == library.KindMovie {
		video := files[0]
		dir := filepath.Dir(video)
		rel, _ := filepath.Rel(oldRoot, dir)
		if rel != "." && videosInDir(dir) <= 1 {
			units = []organizer.MoveUnit{{Src: dir, Dst: filepath.Join(newRoot, rel), Dir: true}}
		} else {
			sides, err := cleanup.FileAndSidecars(video)
			if err != nil {
				t.Problem = plainerror.Message(err)
				return t
			}
			for _, f := range sides {
				frel, _ := filepath.Rel(oldRoot, f)
				units = append(units, organizer.MoveUnit{Src: f, Dst: filepath.Join(newRoot, frel)})
			}
		}
	} else if dir, ok := metafiles.ShowDir(oldRoot, files); ok {
		rel, _ := filepath.Rel(oldRoot, dir)
		units = []organizer.MoveUnit{{Src: dir, Dst: filepath.Join(newRoot, rel), Dir: true}}
		for _, f := range files {
			if _, covered := organizer.MapPath(units, f); !covered {
				outside++
			}
		}
	} else {
		// A show with its episodes straight in the library folder: each file
		// goes with the files named after it.
		done := map[string]bool{}
		for _, f := range files {
			sides, err := cleanup.FileAndSidecars(f)
			if err != nil {
				t.Problem = plainerror.Message(err)
				return t
			}
			for _, sf := range sides {
				if done[sf] {
					continue
				}
				done[sf] = true
				frel, _ := filepath.Rel(oldRoot, sf)
				units = append(units, organizer.MoveUnit{Src: sf, Dst: filepath.Join(newRoot, frel)})
			}
		}
	}
	if len(units) == 0 {
		return t
	}

	for _, u := range units {
		if _, err := cleanup.InsideRoot(oldRoot, u.Src); err != nil {
			t.Problem = "It is reached through a link, so Mediarium leaves it where it is."
			return t
		}
		if _, err := os.Lstat(u.Dst); err == nil {
			t.Problem = fmt.Sprintf("There is already something at %s.", u.Dst)
			return t
		}
		n, size := treeStats(u.Src)
		t.Files += n
		t.Bytes += size
	}
	t.units = units
	t.From, t.To = units[0].Src, units[0].Dst
	if len(units) > 1 {
		t.From, t.To = oldRoot, newRoot
	}
	if same, ok, err := organizer.SameFilesystem(filepath.Dir(units[0].Src), nearestFolder(filepath.Dir(units[0].Dst))); err == nil && ok && same {
		t.Method = "rename"
	} else {
		t.Method = "copy"
	}
	if outside > 0 {
		t.Note = plural(outside, "file") + " outside the show's folder stay where they are."
	}
	if t.Method == "copy" {
		for _, u := range units {
			t.Linked += linkedFiles(u.Src)
		}
		switch {
		case t.Linked == 1:
			t.LinkNote = "One file is linked to a download. The copy in the new folder is not linked, so it takes up space twice until that download is removed. The download's own file is kept."
		case t.Linked > 1:
			t.LinkNote = fmt.Sprintf("There are %d files linked to a download. The copies in the new folder are not linked, so they take up space twice until those downloads are removed. The downloads' own files are kept.", t.Linked)
		}
	}
	for _, f := range files {
		if p, ok := organizer.MapPath(units, f); ok {
			t.newFile = append(t.newFile, p)
		}
	}
	return t
}

// linkedFiles counts the files under path that have more than one name on disk
// (hard links, for example to a torrent that is still seeding).
func linkedFiles(path string) int {
	n := 0
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil && organizer.LinkCount(info) > 1 {
			n++
		}
		return nil
	})
	return n
}

// videosInDir counts the video files directly in dir, samples left out.
func videosInDir(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !organizer.IsVideoFile(e.Name()) || organizer.IsSample(e.Name()) {
			continue
		}
		n++
	}
	return n
}

// moveContext checks a request and works out the plan for each title.
func (s *Server) moveContext(w http.ResponseWriter, r *http.Request) (plans []moveTitle, newRoot, stored string, ok bool) {
	var req moveRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	refs, err := parseBulkItems(req.Items)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	kind, err := sameKind(refs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var problem string
	stored, problem = s.folderForKind(kind, req.RootPath)
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	newRoot = stored
	if newRoot == "" { // the main folder
		if kind == library.KindMovie {
			newRoot = s.moviesRoot()
		} else {
			newRoot = s.tvRoot()
		}
	}
	if newRoot == "" {
		writeError(w, http.StatusBadRequest, "That folder isn't set up. Choose it in Settings > Library and files > Folders.")
		return
	}
	if len(refs) > moveMaxTitles {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Move at most %d titles at a time, so a move stays easy to follow. Pick fewer and do the rest afterwards.", moveMaxTitles))
		return
	}
	busyMovies, busyShows, err := s.busyTitles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Couldn't check what is downloading right now. %s", plainerror.Message(err)))
		return
	}
	taken := map[string]string{} // where each unit goes, so two titles can't land on one place
	for _, ref := range refs {
		t := s.planMove(ref, newRoot, stored, busyMovies, busyShows)
		if t.Problem == "" {
			for _, u := range t.units {
				if other, clash := taken[u.Dst]; clash {
					t.Problem = fmt.Sprintf("It would go to the same place as %s.", other)
					t.units, t.Method = nil, ""
					break
				}
			}
			if t.Problem == "" {
				for _, u := range t.units {
					taken[u.Dst] = t.Title
				}
			}
		}
		plans = append(plans, t)
	}
	return plans, newRoot, stored, true
}

// handleMoveFilesPreview is POST /api/library/move-files/preview: what moving
// the files of the chosen titles to a folder would do. It changes nothing.
func (s *Server) handleMoveFilesPreview(w http.ResponseWriter, r *http.Request) {
	plans, newRoot, _, ok := s.moveContext(w, r)
	if !ok {
		return
	}
	out := movePreview{Titles: []moveTitle{}}
	for _, t := range plans {
		if t.Problem != "" {
			out.Blocked++
		} else {
			out.CanMove++
			out.Files += t.Files
			out.Bytes += t.Bytes
			if t.Method == "copy" {
				out.CopyBytes += t.Bytes
			}
		}
	}
	if f := fsinfo.Inspect(newRoot); f.Exists {
		out.FreeBytes = int64(f.FreeBytes)
	}
	if out.CopyBytes > 0 && out.FreeBytes > 0 && out.CopyBytes+moveSpaceMargin > out.FreeBytes {
		out.Problem = fmt.Sprintf("There isn't enough room for it: %s to copy and %s free on the new folder's drive.", humanBytes(out.CopyBytes), humanBytes(out.FreeBytes))
	}
	// Problems first, then the biggest moves, capped.
	sort.SliceStable(plans, func(i, j int) bool {
		if (plans[i].Problem != "") != (plans[j].Problem != "") {
			return plans[i].Problem != ""
		}
		return plans[i].Bytes > plans[j].Bytes
	})
	if len(plans) > moveProblemsShown {
		out.Truncated = len(plans) - moveProblemsShown
		plans = plans[:moveProblemsShown]
	}
	out.Titles = append(out.Titles, plans...)
	writeJSON(w, http.StatusOK, out)
}

// ---- The job ---------------------------------------------------------------

type moveProblem struct {
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

type moveStatus struct {
	Running   bool          `json:"running"`
	Titles    int           `json:"titles"`
	Done      int           `json:"done"`  // titles dealt with
	Moved     int           `json:"moved"` // titles whose files are in the new folder
	Left      int           `json:"left"`  // titles left as they were
	BytesAll  int64         `json:"bytesAll"`
	BytesDone int64         `json:"bytesDone"`
	Current   string        `json:"current,omitempty"`
	Problems  []moveProblem `json:"problems"`
	Warnings  []string      `json:"warnings"`
	Folder    string        `json:"folder,omitempty"`
	Started   string        `json:"startedAt,omitempty"`
	Finished  string        `json:"finishedAt,omitempty"`
	Canceled  bool          `json:"canceled"`
	Message   string        `json:"message,omitempty"`
}

type moveJob struct {
	mu     sync.Mutex
	status moveStatus
	cancel context.CancelFunc
}

func (j *moveJob) snapshot() moveStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	st := j.status
	st.Problems = append([]moveProblem{}, st.Problems...)
	st.Warnings = append([]string{}, st.Warnings...)
	return st
}

// handleMoveFiles is POST /api/library/move-files: moves the files of the
// chosen titles to the folder, in the background. Titles that cannot be moved
// completely are left as they are and listed in the status.
func (s *Server) handleMoveFiles(w http.ResponseWriter, r *http.Request) {
	plans, newRoot, stored, ok := s.moveContext(w, r)
	if !ok {
		return
	}
	j := &s.moveJob
	j.mu.Lock()
	if j.status.Running {
		j.mu.Unlock()
		writeError(w, http.StatusConflict, "Files are being moved already. Wait for that to finish, or stop it first.")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	j.cancel = cancel
	st := moveStatus{Running: true, Titles: len(plans), Problems: []moveProblem{}, Warnings: []string{}, Folder: newRoot, Started: time.Now().UTC().Format(time.RFC3339)}
	for _, t := range plans {
		if t.Problem == "" {
			st.BytesAll += t.Bytes
		}
	}
	j.status = st
	j.mu.Unlock()
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		s.runMoveJob(ctx, plans, newRoot, stored)
	}()
	writeJSON(w, http.StatusAccepted, j.snapshot())
}

func (s *Server) runMoveJob(ctx context.Context, plans []moveTitle, newRoot, stored string) {
	j := &s.moveJob
	update := func(f func(st *moveStatus)) {
		j.mu.Lock()
		f(&j.status)
		j.mu.Unlock()
	}
	problem := func(t moveTitle, reason string) {
		update(func(st *moveStatus) {
			st.Left++
			if len(st.Problems) < moveProblemsShown {
				st.Problems = append(st.Problems, moveProblem{Title: t.Title, Reason: reason})
			}
		})
	}

	// The copy checks free space once more before each title, since other things use the drive.
	for _, t := range plans {
		if ctx.Err() != nil {
			break
		}
		update(func(st *moveStatus) { st.Current = t.Title })
		if t.Problem != "" {
			problem(t, t.Problem)
			update(func(st *moveStatus) { st.Done++ })
			continue
		}
		s.moveOneTitle(ctx, t, newRoot, stored, problem, update)
		update(func(st *moveStatus) { st.Done++ })
	}

	j.mu.Lock()
	j.status.Running = false
	j.status.Current = ""
	j.status.Canceled = ctx.Err() != nil
	j.status.Finished = time.Now().UTC().Format(time.RFC3339)
	j.status.Message = moveDoneMessage(j.status)
	j.cancel = nil
	msg, moved := j.status.Message, j.status.Moved
	j.mu.Unlock()
	// One title that moved already has its own line ("Moved X from A to B").
	if len(plans) != 1 || moved != 1 {
		_ = s.QueueRepo.LogActivity(0, "moved", msg)
	}
}

func moveDoneMessage(st moveStatus) string {
	var msg string
	switch {
	case st.Canceled:
		msg = fmt.Sprintf("Stopped. %s moved to %s so far.", plural(st.Moved, "title"), st.Folder)
	case st.Moved == 0 && st.Left == 0:
		msg = "Nothing to move."
	case st.Moved == 0:
		msg = "Nothing was moved."
	default:
		msg = fmt.Sprintf("%s moved to %s.", plural(st.Moved, "title"), st.Folder)
	}
	if st.Left > 0 && !st.Canceled {
		msg += " " + plural(st.Left, "title") + " left as they were."
	}
	return msg
}

// moveOneTitle moves one title: its files first (undone if anything fails),
// then the records, then the originals are removed.
func (s *Server) moveOneTitle(ctx context.Context, t moveTitle, newRoot, stored string, problem func(moveTitle, string), update func(func(*moveStatus))) {
	save := func() error {
		if t.Kind == library.KindMovie {
			return s.MovieRepo.SetRoot(t.ID, stored)
		}
		return s.MovieRepo.SetSeriesRoot(t.ID, stored)
	}
	if len(t.units) == 0 {
		// Nothing on disk to move (or already in that folder): the folder setting is all.
		if err := save(); err != nil {
			problem(t, plainerror.Message(err))
			return
		}
		update(func(st *moveStatus) { st.Moved++ })
		return
	}

	// Plan again now: the files may have changed since the preview.
	busyMovies, busyShows, err := s.busyTitles()
	if err != nil {
		problem(t, "Couldn't check what is downloading right now.")
		return
	}
	fresh := s.planMove(library.Ref{Kind: t.Kind, ID: t.ID}, newRoot, stored, busyMovies, busyShows)
	if fresh.Problem != "" {
		problem(t, fresh.Problem)
		return
	}
	t = fresh
	if t.Method == "copy" {
		if f := fsinfo.Inspect(newRoot); f.Exists && int64(f.FreeBytes) < t.Bytes+moveSpaceMargin {
			problem(t, fmt.Sprintf("There isn't enough room on the new drive (%s to copy, %s free).", humanBytes(t.Bytes), humanBytes(int64(f.FreeBytes))))
			return
		}
	}

	var counted int64
	mv, err := organizer.StartMove(ctx, t.units, organizer.MoveOptions{Progress: func(n int64) {
		counted += n
		update(func(st *moveStatus) { st.BytesDone += n })
	}})
	if err != nil {
		// The bytes of an undone copy do not count as done.
		update(func(st *moveStatus) { st.BytesDone -= counted })
		if ctx.Err() == nil {
			slog.Warn("moving a title's files failed", "title", t.Title, "err", err)
			problem(t, moveReason(err))
		}
		return
	}

	units := t.units
	if err := s.MovieRepo.ApplyMove(t.Kind, t.ID, stored, func(p string) (string, bool) { return organizer.MapPath(units, p) }); err != nil {
		if left := mv.Rollback(); len(left) > 0 {
			update(func(st *moveStatus) {
				st.Warnings = append(st.Warnings, fmt.Sprintf("%s: a copy could not be removed again: %s", t.Title, left[0]))
			})
		}
		update(func(st *moveStatus) { st.BytesDone -= counted })
		slog.Warn("recording a move failed", "title", t.Title, "err", err)
		problem(t, "The move was undone because it couldn't be saved: "+plainerror.Message(err))
		return
	}
	left := mv.Commit()
	for _, u := range t.units {
		cleanup.PruneEmptyFolders(t.oldRoot, filepath.Dir(u.Src))
	}
	if len(left) > 0 {
		update(func(st *moveStatus) {
			st.Warnings = append(st.Warnings, fmt.Sprintf("%s: %s could not be removed from the old folder (they are safe in the new one), for example %s", t.Title, plural(len(left), "file"), left[0]))
		})
	}
	update(func(st *moveStatus) { st.Moved++ })
	var movieID int64
	if t.Kind == library.KindMovie {
		movieID = t.ID
	}
	if t.Files == 1 {
		_ = s.QueueRepo.LogActivity(movieID, "moved", fmt.Sprintf("Moved %s from %s to %s (1 file, %s).", t.Title, t.oldRoot, newRoot, humanBytes(t.Bytes)))
	} else {
		_ = s.QueueRepo.LogActivity(movieID, "moved", fmt.Sprintf("Moved %s from %s to %s (%d files, %s).", t.Title, t.oldRoot, newRoot, t.Files, humanBytes(t.Bytes)))
	}

	// The media servers look at the new place.
	if t.Kind == library.KindMovie {
		for _, f := range t.newFile {
			s.movieImported(f)
		}
	} else if len(t.newFile) > 0 {
		s.episodesImported(t.newFile...)
	}
}

// moveReason puts a failed move into a sentence for the person.
func moveReason(err error) string {
	switch {
	case errors.Is(err, organizer.ErrMoveTargetExists):
		return "There is already something at the new place."
	case errors.Is(err, os.ErrPermission):
		return "Mediarium may not write to the new folder. Give the user Mediarium runs as (PUID and PGID) write access to it."
	}
	msg := plainerror.Message(err)
	if strings.TrimSpace(msg) == "" {
		msg = err.Error()
	}
	return msg
}

func (s *Server) handleMoveFilesStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.moveJob.snapshot())
}

func (s *Server) handleCancelMoveFiles(w http.ResponseWriter, r *http.Request) {
	j := &s.moveJob
	j.mu.Lock()
	if j.cancel != nil {
		j.cancel()
	}
	j.mu.Unlock()
	writeJSON(w, http.StatusOK, j.snapshot())
}
