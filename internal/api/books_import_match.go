package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/plainerror"
)

// Matching a book by hand: for a file the import could not find, the person
// searches Open Library themselves (the page uses GET /api/books/search),
// picks the book, and it is attached to that file the same way an automatic
// match is. Optionally the file is then moved to where the library would put
// a download of that book.

type matchBookRequest struct {
	Path        string       `json:"path"` // the file or folder as the import list shows it
	Kind        books.Format `json:"kind"`
	OLKey       string       `json:"olKey"`
	Title       string       `json:"title"`
	Author      string       `json:"author"`
	AuthorKey   string       `json:"authorKey"`
	Year        int          `json:"year"`
	CoverID     int          `json:"coverId"`
	HardcoverID int          `json:"hardcoverId"`
	Rename      bool         `json:"rename"` // move the file to the library's naming afterwards
}

// handleMatchBookImport attaches a book picked by hand to one of the files
// the last import did not find (administrators, like the import itself).
func (s *Server) handleMatchBookImport(w http.ResponseWriter, r *http.Request) {
	var req matchBookRequest
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Path) == "" || strings.TrimSpace(req.OLKey) == "" || strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "Pick a book from the search results to match it.")
		return
	}
	if req.Kind != books.Ebook && req.Kind != books.Audiobook {
		writeError(w, http.StatusBadRequest, "Say whether it is an ebook or an audiobook.")
		return
	}
	match := books.Found{Key: strings.TrimPrefix(strings.TrimSpace(req.OLKey), "/works/"), Title: strings.TrimSpace(req.Title),
		Author: strings.TrimSpace(req.Author), AuthorKey: req.AuthorKey, Year: req.Year, CoverID: req.CoverID, HardcoverID: req.HardcoverID}

	s.bookImport.mu.Lock()
	job := s.bookImport.job
	s.bookImport.mu.Unlock()
	if job == nil {
		writeError(w, http.StatusConflict, "There is no import to match from. Start one first.")
		return
	}
	job.mu.Lock()
	if job.phase == bookImportRunning {
		job.mu.Unlock()
		writeError(w, http.StatusConflict, "An import is running. Match it by hand when it has finished.")
		return
	}
	at := -1
	for i, res := range job.results {
		if res.Path == req.Path && res.Kind == req.Kind {
			at = i
			break
		}
	}
	var res bookImportResult
	if at >= 0 {
		res = job.results[at]
	}
	// One request at a time for a file: two at once (a double click, a retry
	// after a slow answer) would both move it, and the second would record the
	// place it was moved from.
	lock := string(req.Kind) + "\x00" + req.Path
	busy := at >= 0 && job.matching[lock]
	if at >= 0 && !busy {
		if job.matching == nil {
			job.matching = map[string]bool{}
		}
		job.matching[lock] = true
	}
	job.mu.Unlock()
	if busy {
		writeError(w, http.StatusConflict, "That file is being matched right now. Give it a moment and look at the list again.")
		return
	}
	if at >= 0 {
		defer func() {
			job.mu.Lock()
			delete(job.matching, lock)
			job.mu.Unlock()
		}()
	}
	if at < 0 {
		writeError(w, http.StatusNotFound, "That file isn't in the last import. Run the import again.")
		return
	}
	if res.Status != bookUnmatched && res.Status != bookFailed {
		writeError(w, http.StatusConflict, "That book was already found.")
		return
	}

	res.Status, res.Message, res.Matched = "", "", ""
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	res = s.attachOpenLibraryBook(ctx, res, match, req.Rename)

	job.mu.Lock()
	// The list can have been replaced by a new import while this ran.
	if at < len(job.results) && job.results[at].Path == req.Path && job.results[at].Kind == req.Kind {
		job.results[at] = res
	}
	job.mu.Unlock()
	if res.Status == bookImported {
		_ = s.QueueRepo.LogActivity(0, "imported", fmt.Sprintf("%s matched by hand to %s", filepath.Base(req.Path), match.Title))
	}
	writeJSON(w, http.StatusOK, job.snapshot())
}

// renameLocalBook moves a file the import found (an ebook, or an audiobook's
// folder) to where the library puts a download of the book: the same folder
// and file names as a download gets. It returns the path to record, and a
// finish to call once that is saved (true) or could not be (false, which puts
// the file back). When the file stays where it is, note says why.
func (s *Server) renameLocalBook(b books.Book, res bookImportResult) (path string, finish func(bool), note string) {
	stay := func(why string) (string, func(bool), string) { return res.Path, func(bool) {}, why }
	root := s.bookRoot(res.Kind)
	if root == "" || !isWithin(root, res.Path) {
		return stay("Added, but the file is outside your books folder, so it was not renamed.")
	}
	folder, err := s.bookFolder(b, res.Kind)
	if err != nil {
		return stay(fmt.Sprintf("Added, but it was not renamed: %s", plainerror.Message(err)))
	}
	dest := folder
	dir := true
	info, err := os.Stat(res.Path)
	if err != nil {
		return stay("Added, but it was not renamed: the file is not there any more.")
	}
	if !info.IsDir() {
		dir = false
		name := filepath.Base(res.Path) // an audiobook keeps its file's own name
		if res.Kind == books.Ebook {
			mode, repl := s.illegalCharSettings()
			name = organizer.Sanitize(b.Title, mode, repl) + filepath.Ext(res.Path)
		}
		dest = filepath.Join(folder, name)
	}
	if filepath.Clean(dest) == filepath.Clean(res.Path) {
		return res.Path, func(bool) {}, "" // already named that way
	}
	if dir && isWithin(res.Path, dest) {
		return stay("Added, but it was not renamed: the new folder would be inside the old one.")
	}
	mv, err := organizer.StartMove(context.Background(), []organizer.MoveUnit{{Src: res.Path, Dst: dest, Dir: dir}}, organizer.MoveOptions{})
	if err != nil {
		slog.Warn("books: rename after a match by hand", "book", b.ID, "err", err)
		return stay(fmt.Sprintf("Added, but it was not renamed: %s", plainerror.Message(err)))
	}
	return dest, func(saved bool) {
		if !saved {
			mv.Rollback()
			return
		}
		if left := mv.Commit(); len(left) > 0 {
			slog.Warn("books: old files left behind after a rename", "book", b.ID, "left", len(left))
		}
		s.booksImported(dest)
	}, ""
}
