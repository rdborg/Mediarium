package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/books"
)

// Importing books already on disk: the ebooks and audiobooks folders are read
// (see books.ScanEbooks and books.ScanAudiobooks), each book is looked up on
// Open Library by title and author, and what matches is added to the library
// as downloaded, where it is. Nothing is moved, renamed or changed. It runs in
// the background; the page polls GET /api/books/import.

const (
	bookImportRunning = "running"
	bookImportDone    = "done"
	bookImportFailed  = "failed"

	bookImported  = "imported"  // added to the library (or a format added to a book already there)
	bookAlready   = "already"   // that book already had a file in this format
	bookUnmatched = "unmatched" // not found on Open Library
	bookFailed    = "failed"    // found, but saving it failed
)

type bookImportResult struct {
	books.LocalBook
	Kind    books.Format `json:"kind"` // ebook or audiobook
	Status  string       `json:"status"`
	BookID  int64        `json:"bookId,omitempty"`
	Matched string       `json:"matched,omitempty"` // the Open Library title it matched
	Message string       `json:"message,omitempty"`
}

type bookImportJob struct {
	mu       sync.Mutex
	phase    string
	done     int
	total    int
	results  []bookImportResult
	err      string
	started  time.Time
	finished time.Time
}

type bookImportPayload struct {
	Phase   string             `json:"phase"` // "" (never run), running, done or failed
	Done    int                `json:"done"`
	Total   int                `json:"total"`
	Summary map[string]int     `json:"summary"`
	Results []bookImportResult `json:"results"`
	Error   string             `json:"error,omitempty"`
	Started string             `json:"started,omitempty"`
}

func (j *bookImportJob) snapshot() bookImportPayload {
	j.mu.Lock()
	defer j.mu.Unlock()
	p := bookImportPayload{Phase: j.phase, Done: j.done, Total: j.total, Error: j.err, Summary: map[string]int{}, Results: append([]bookImportResult{}, j.results...)}
	if !j.started.IsZero() {
		p.Started = j.started.UTC().Format(time.RFC3339)
	}
	for _, r := range j.results {
		p.Summary[r.Status]++
	}
	return p
}

// bookImports holds the one import job a server runs at a time.
type bookImports struct {
	mu  sync.Mutex
	job *bookImportJob
}

func (s *Server) handleBookImportStatus(w http.ResponseWriter, r *http.Request) {
	s.bookImport.mu.Lock()
	job := s.bookImport.job
	s.bookImport.mu.Unlock()
	if job == nil {
		writeJSON(w, http.StatusOK, bookImportPayload{Summary: map[string]int{}, Results: []bookImportResult{}})
		return
	}
	writeJSON(w, http.StatusOK, job.snapshot())
}

type bookImportRequest struct {
	Format string `json:"format"` // ebook, audiobook, or "" for both (whichever are on)
}

// handleStartBookImport starts reading the books folders (administrators).
func (s *Server) handleStartBookImport(w http.ResponseWriter, r *http.Request) {
	var req bookImportRequest
	_ = decodeJSON(r, &req)
	var formats []books.Format
	for _, f := range []books.Format{books.Ebook, books.Audiobook} {
		if (req.Format == "" || req.Format == string(f)) && s.formatEnabled(f) && s.bookRoot(f) != "" {
			formats = append(formats, f)
		}
	}
	if len(formats) == 0 {
		writeError(w, http.StatusConflict, "Switch on ebooks or audiobooks and set their folder first.")
		return
	}
	s.bookImport.mu.Lock()
	if s.bookImport.job != nil && s.bookImport.job.snapshot().Phase == bookImportRunning {
		s.bookImport.mu.Unlock()
		writeError(w, http.StatusConflict, "An import is already running.")
		return
	}
	job := &bookImportJob{phase: bookImportRunning, started: time.Now()}
	s.bookImport.job = job
	s.bookImport.mu.Unlock()
	s.background(func() { s.runBookImport(job, formats) })
	writeJSON(w, http.StatusAccepted, job.snapshot())
}

func (s *Server) runBookImport(job *bookImportJob, formats []books.Format) {
	var found []bookImportResult
	for _, f := range formats {
		root := s.bookRoot(f)
		var list []books.LocalBook
		var err error
		if f == books.Ebook {
			list, err = books.ScanEbooks(root)
		} else {
			list, err = books.ScanAudiobooks(root)
		}
		if err != nil {
			job.mu.Lock()
			job.phase, job.err, job.finished = bookImportFailed, fmt.Sprintf("Couldn't read %s: %v", root, err), time.Now()
			job.mu.Unlock()
			return
		}
		for _, lb := range list {
			found = append(found, bookImportResult{LocalBook: lb, Kind: f})
		}
	}
	job.mu.Lock()
	job.total = len(found)
	job.mu.Unlock()

	known := map[string]bool{} // paths already in the library
	if list, err := s.BookRepo.List(); err == nil {
		for _, b := range list {
			known[b.EbookPath], known[b.AudioPath] = true, true
		}
	}
	imported := 0
	for i, res := range found {
		if known[res.Path] {
			res.Status = bookAlready
		} else {
			res = s.importOneBook(res)
			if res.Status == bookImported {
				imported++
			}
			if i < len(found)-1 {
				time.Sleep(300 * time.Millisecond) // gentle on Open Library
			}
		}
		job.mu.Lock()
		job.results = append(job.results, res)
		job.done++
		job.mu.Unlock()
	}
	job.mu.Lock()
	job.phase, job.finished = bookImportDone, time.Now()
	job.mu.Unlock()
	if imported > 0 {
		_ = s.QueueRepo.LogActivity(0, "imported", plural(imported, "book")+" added from your books folders")
	}
	slog.Info("books: import finished", "found", len(found), "imported", imported)
}

// importOneBook finds a book on Open Library and records its file.
func (s *Server) importOneBook(res bookImportResult) bookImportResult {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	match, err := s.matchLocalBook(ctx, res.LocalBook)
	if err != nil {
		res.Status, res.Message = bookUnmatched, "Open Library didn't answer. Run the import again later."
		return res
	}
	if match == nil {
		res.Status, res.Message = bookUnmatched, "Not found on Open Library. Check the folder is named Author/Title."
		return res
	}
	res.Matched = match.Title
	b, ok, err := s.BookRepo.GetByKey(match.Key)
	if err != nil {
		res.Status, res.Message = bookFailed, err.Error()
		return res
	}
	if !ok {
		nb := books.Book{OLKey: match.Key, Title: match.Title, Author: match.Author, AuthorKey: match.AuthorKey, Year: match.Year, CoverID: match.CoverID,
			WantEbook: res.Kind == books.Ebook, WantAudiobook: res.Kind == books.Audiobook}
		if work, err := s.OpenLibrary.GetWork(ctx, match.Key); err == nil {
			nb.Description = work.Description
			if nb.CoverID == 0 {
				nb.CoverID = work.CoverID
			}
		}
		b, err = s.BookRepo.Add(nb)
		if errors.Is(err, books.ErrExists) {
			b, _, err = s.BookRepo.GetByKey(match.Key)
		}
		if err != nil {
			res.Status, res.Message = bookFailed, err.Error()
			return res
		}
	} else if b.Path(res.Kind) != "" {
		res.Status, res.BookID, res.Message = bookAlready, b.ID, "This book already has a file: "+b.Path(res.Kind)
		return res
	}
	if !b.Wants(res.Kind) {
		_ = s.BookRepo.SetWant(b.ID, res.Kind, true)
	}
	if err := s.BookRepo.SetState(b.ID, res.Kind, books.StatusDownloaded, res.Format, res.Path); err != nil {
		res.Status, res.Message = bookFailed, err.Error()
		return res
	}
	res.Status, res.BookID = bookImported, b.ID
	if updated, err := s.BookRepo.Get(b.ID); err == nil {
		s.lookUpAudioDetailsLater(updated) // narrator and length, for an audiobook
		s.convertLater(updated)            // an EPUB copy of a Kindle book, for the reader
	}
	return res
}

// matchLocalBook looks a book found on disk up on Open Library: the first
// result with the same title (and author, when the folder names one), or
// failing that the first whose title words are all in the name on disk.
func (s *Server) matchLocalBook(ctx context.Context, lb books.LocalBook) (*books.Found, error) {
	query := lb.Title
	if lb.Author != "" {
		query += " " + lb.Author
	}
	found, err := s.OpenLibrary.Search(ctx, query, 10)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 && lb.Author != "" {
		// The folder name may not be the author after all.
		if found, err = s.OpenLibrary.Search(ctx, lb.Title, 10); err != nil {
			return nil, err
		}
	}
	for _, exact := range []bool{true, false} {
		for i := range found {
			if books.SameTitle(lb.Title, found[i].Title, exact) && (lb.Author == "" || books.SameAuthor(lb.Author, found[i].Author)) {
				return &found[i], nil
			}
		}
	}
	return nil, nil
}
