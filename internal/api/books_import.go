package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
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
	mu      sync.Mutex
	phase   string
	kinds   []books.Format // what this run reads or looks up again
	done    int
	total   int
	results []bookImportResult
	// matching holds the files (kind and path) being matched by hand right now,
	// so a second request for the same one is refused until the first is done.
	matching map[string]bool
	err      string
	started  time.Time
	finished time.Time
}

type bookImportPayload struct {
	Phase string `json:"phase"` // "" (never run), running, done or failed
	// Kinds is what the run in Phase is about (ebook, audiobook). Results of
	// the other kind are those of its own last run, kept as they were.
	Kinds   []books.Format     `json:"kinds"`
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
	p := bookImportPayload{Phase: j.phase, Kinds: append([]books.Format{}, j.kinds...), Done: j.done, Total: j.total, Error: j.err, Summary: map[string]int{}, Results: append([]bookImportResult{}, j.results...)}
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
		writeJSON(w, http.StatusOK, bookImportPayload{Kinds: []books.Format{}, Summary: map[string]int{}, Results: []bookImportResult{}})
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
	var kept []bookImportResult
	if prev := s.bookImport.job; prev != nil {
		snap := prev.snapshot()
		if snap.Phase == bookImportRunning {
			s.bookImport.mu.Unlock()
			writeError(w, http.StatusConflict, "An import is already running.")
			return
		}
		// Importing audiobooks keeps what the last ebook import found (and the
		// other way round), so its "Find it myself" rows are still there.
		for _, res := range snap.Results {
			if !slices.Contains(formats, res.Kind) {
				kept = append(kept, res)
			}
		}
	}
	job := &bookImportJob{phase: bookImportRunning, kinds: formats, started: time.Now()}
	s.bookImport.job = job
	s.bookImport.mu.Unlock()
	s.background(func() { s.runBookImport(job, formats, kept) })
	writeJSON(w, http.StatusAccepted, job.snapshot())
}

func (s *Server) runBookImport(job *bookImportJob, formats []books.Format, kept []bookImportResult) {
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
			job.results = append(job.results, kept...)
			job.mu.Unlock()
			return
		}
		for _, lb := range list {
			found = append(found, bookImportResult{LocalBook: lb, Kind: f})
		}
	}
	s.processBookImport(job, found, kept)
}

// handleRetryBookImport looks again only at the books the last import did not
// find (or could not save), without reading the folders again: for the ones
// Open Library or Audnexus did not answer, or that were added there since.
func (s *Server) handleRetryBookImport(w http.ResponseWriter, r *http.Request) {
	var req bookImportRequest
	_ = decodeJSON(r, &req)
	s.bookImport.mu.Lock()
	prev := s.bookImport.job
	if prev == nil {
		s.bookImport.mu.Unlock()
		writeError(w, http.StatusConflict, "There is no import to run again. Start one first.")
		return
	}
	snap := prev.snapshot()
	if snap.Phase == bookImportRunning {
		s.bookImport.mu.Unlock()
		writeError(w, http.StatusConflict, "An import is already running.")
		return
	}
	var again, kept []bookImportResult
	var kinds []books.Format
	for _, res := range snap.Results {
		if (res.Status == bookUnmatched || res.Status == bookFailed) && (req.Format == "" || req.Format == string(res.Kind)) {
			res.Status, res.Message, res.Matched = "", "", ""
			again = append(again, res)
			if !slices.Contains(kinds, res.Kind) {
				kinds = append(kinds, res.Kind)
			}
		} else {
			kept = append(kept, res)
		}
	}
	if len(again) == 0 {
		s.bookImport.mu.Unlock()
		writeError(w, http.StatusConflict, "Nothing to run again: every book was found.")
		return
	}
	job := &bookImportJob{phase: bookImportRunning, kinds: kinds, started: time.Now()}
	s.bookImport.job = job
	s.bookImport.mu.Unlock()
	s.background(func() { s.processBookImport(job, again, kept) })
	writeJSON(w, http.StatusAccepted, job.snapshot())
}

// processBookImport looks up every book in found and adds it to the library.
// kept are results from an earlier run that stay as they are.
func (s *Server) processBookImport(job *bookImportJob, found, kept []bookImportResult) {
	job.mu.Lock()
	// The progress counts what this run looks up, not what it keeps.
	job.total = len(found)
	job.done = 0
	job.results = append(job.results, kept...)
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
	if res.Kind == books.Ebook && books.ComicFormat(res.Format) {
		return s.importOneComic(res) // a comic isn't on Open Library: it goes in as it is
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	match, err := s.matchLocalBook(ctx, res.LocalBook)
	if err != nil {
		res.Status, res.Message = bookUnmatched, "Open Library didn't answer. Run the import again later."
		return res
	}
	if match == nil {
		// An audiobook Open Library doesn't carry is often on Audible: ask
		// Audnexus before giving up, and add the book from what it says.
		if res.Kind == books.Audiobook {
			if d, derr := s.Audnexus.Lookup(ctx, res.Title, res.Author); derr == nil && d.ASIN != "" {
				return s.importOneBookViaAudnexus(ctx, res, d)
			} else if derr != nil && !errors.Is(derr, books.ErrNotFound) {
				res.Status, res.Message = bookUnmatched, "Audnexus didn't answer. Run the import again later."
				return res
			}
		}
		res.Status, res.Message = bookUnmatched, "Not found on Open Library. Check that the name gives the author and the title, or use Find it myself."
		return res
	}
	return s.attachOpenLibraryBook(ctx, res, *match, false)
}

// attachOpenLibraryBook adds the Open Library book match to the library (when
// it is not there yet) and records the file in res as its file. With rename
// set, the file (or the audiobook's folder) is first moved to where a download
// of that book would go. It is the end of an automatic match and the whole of
// one picked by hand.
func (s *Server) attachOpenLibraryBook(ctx context.Context, res bookImportResult, match books.Found, rename bool) bookImportResult {
	res.Matched = match.Title
	b, ok, err := s.BookRepo.GetByKey(match.Key)
	if err != nil {
		res.Status, res.Message = bookFailed, err.Error()
		return res
	}
	if !ok {
		nb := books.Book{OLKey: match.Key, Title: match.Title, Author: match.Author, AuthorKey: match.AuthorKey, Year: match.Year, CoverID: match.CoverID,
			WantEbook: res.Kind == books.Ebook, WantAudiobook: res.Kind == books.Audiobook}
		hcID := s.prepareNewBook(ctx, &nb, match.HardcoverID)
		fresh := false
		b, err = s.BookRepo.Add(nb)
		if err == nil {
			fresh = true
		} else if errors.Is(err, books.ErrExists) {
			b, _, err = s.BookRepo.GetByKey(nb.OLKey)
		}
		if err == nil && fresh && hcID > 0 && b.CoverID == 0 && b.OwnCover == 0 {
			s.keepHardcoverCover(ctx, b.ID, hcID)
			if got, gerr := s.BookRepo.Get(b.ID); gerr == nil {
				b = got
			}
		}
		if err != nil {
			res.Status, res.Message = bookFailed, err.Error()
			return res
		}
		if b.Path(res.Kind) != "" {
			// Added by a request that ran at the same moment, and it has its file already.
			res.Status, res.BookID, res.Message = bookAlready, b.ID, "This book already has a file: "+b.Path(res.Kind)
			return res
		}
	} else if b.Path(res.Kind) != "" {
		res.Status, res.BookID, res.Message = bookAlready, b.ID, "This book already has a file: "+b.Path(res.Kind)
		return res
	}
	if !b.Wants(res.Kind) {
		_ = s.BookRepo.SetWant(b.ID, res.Kind, true)
	}
	finish := func(bool) {}
	if rename {
		var note string
		res.Path, finish, note = s.renameLocalBook(b, res)
		res.Message = note
	}
	if err := s.BookRepo.SetState(b.ID, res.Kind, books.StatusDownloaded, res.Format, res.Path); err != nil {
		finish(false)
		res.Status, res.Message = bookFailed, err.Error()
		return res
	}
	finish(true)
	res.Status, res.BookID = bookImported, b.ID
	if updated, err := s.BookRepo.Get(b.ID); err == nil {
		s.lookUpAudioDetailsLater(updated) // narrator and length, for an audiobook
		s.convertLater(updated)            // an EPUB copy of a Kindle book, for the reader
	}
	return res
}

// importOneBookViaAudnexus adds a book Audible has and Open Library doesn't:
// it gets a library entry keyed by its Audible id, straight away as
// downloaded, with the narrator and length Audnexus reported.
func (s *Server) importOneBookViaAudnexus(ctx context.Context, res bookImportResult, d books.AudioDetails) bookImportResult {
	key := "audnexus:" + d.ASIN
	b, ok, err := s.BookRepo.GetByKey(key)
	if err != nil {
		res.Status, res.Message = bookFailed, err.Error()
		return res
	}
	if !ok {
		nb := books.Book{OLKey: key, Title: d.Title, Author: res.Author, WantAudiobook: true,
			SeriesName: d.SeriesName, SeriesPosition: d.SeriesPosition, ReleaseDate: d.ReleaseDate}
		if len(d.ReleaseDate) >= 4 {
			nb.Year, _ = strconv.Atoi(d.ReleaseDate[:4])
		}
		b, err = s.BookRepo.Add(nb)
		if errors.Is(err, books.ErrExists) {
			b, _, err = s.BookRepo.GetByKey(key)
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
	if err := s.BookRepo.SetAudioDetails(b.ID, d); err != nil {
		slog.Warn("books: save audiobook details", "book", b.ID, "err", err)
	}
	res.Status, res.BookID, res.Matched = bookImported, b.ID, d.Title+" (Audible)"
	return res
}

// matchLocalBook looks a book found on disk up on Open Library: the first
// result with the same title (and author, when the folder names one), or
// failing that the first whose title words are all in the name on disk. A
// name written the other way round ("1984 - George Orwell.epub") is tried
// that way too.
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
	if i := books.BestLocalMatch(lb.Title, lb.Author, found); i >= 0 {
		return &found[i], nil
	}
	return nil, nil
}

// importOneComic adds a comic file as it is: no lookup, a library entry keyed
// by its series, number and name, straight away as downloaded. Its series and
// number come from the ComicInfo.xml inside it when there is one.
func (s *Server) importOneComic(res bookImportResult) bookImportResult {
	id := res.Author + "|" + res.Title + "|" + res.Series + "|" + res.Position
	if res.Year > 0 {
		// Two runs of a series ("Batman (2011)", "Batman (2016)") both have an issue 1.
		id += "|" + strconv.Itoa(res.Year)
	}
	sum := sha256.Sum256([]byte(strings.ToLower(id)))
	key := "comic:" + hex.EncodeToString(sum[:10])
	b, ok, err := s.BookRepo.GetByKey(key)
	if err != nil {
		res.Status, res.Message = bookFailed, err.Error()
		return res
	}
	if !ok {
		b, err = s.BookRepo.Add(books.Book{OLKey: key, Title: res.Title, Author: res.Author, Year: res.Year, WantEbook: true,
			SeriesName: res.Series, SeriesPosition: res.Position})
		if errors.Is(err, books.ErrExists) {
			b, _, err = s.BookRepo.GetByKey(key)
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
	res.Status, res.BookID, res.Matched = bookImported, b.ID, res.Title+" (comic)"
	return res
}
