package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/queue"
)

// The ebooks and audiobooks modules: add a book from Open Library, choose
// the formats you want, and Mediarium searches your indexers (book and
// audiobook categories), downloads the best release and files it.

// Newznab categories: 7000 books, 7020 ebooks; 3030 audiobooks.
var (
	ebookCategories     = []int{7000, 7020}
	audiobookCategories = []int{3030}
)

func categoriesFor(f books.Format) []int {
	if f == books.Audiobook {
		return audiobookCategories
	}
	return ebookCategories
}

func (s *Server) formatEnabled(f books.Format) bool {
	if f == books.Audiobook {
		return s.audiobooksEnabled()
	}
	return s.ebooksEnabled()
}

type bookFormatPayload struct {
	Wanted bool   `json:"wanted"`
	Status string `json:"status"`
	Format string `json:"format,omitempty"` // file format: epub, m4b, ...
	Path   string `json:"path,omitempty"`
}

type bookPayload struct {
	ID          int64             `json:"id"`
	OLKey       string            `json:"olKey"`
	Title       string            `json:"title"`
	Author      string            `json:"author"`
	AuthorKey   string            `json:"authorKey,omitempty"`
	Year        int               `json:"year,omitempty"`
	CoverURL    string            `json:"coverUrl,omitempty"`
	Description string            `json:"description,omitempty"`
	AddedAt     string            `json:"addedAt"`
	Ebook       bookFormatPayload `json:"ebook"`
	Audiobook   bookFormatPayload `json:"audiobook"`

	SeriesName     string `json:"seriesName,omitempty"`
	SeriesPosition string `json:"seriesPosition,omitempty"`
	ReleaseDate    string `json:"releaseDate,omitempty"` // "2026-11-04" when known

	// The audiobook's details (Audnexus).
	Narrators  string `json:"narrators,omitempty"`
	RuntimeMin int    `json:"runtimeMin,omitempty"`
	ASIN       string `json:"asin,omitempty"`
}

func toBookPayload(b books.Book, admin bool) bookPayload {
	p := bookPayload{ID: b.ID, OLKey: b.OLKey, Title: b.Title, Author: b.Author, AuthorKey: b.AuthorKey, Year: b.Year, CoverURL: books.CoverURL(b.CoverID, "M"),
		Description: b.Description, AddedAt: b.AddedAt,
		Ebook:      bookFormatPayload{Wanted: b.WantEbook, Status: b.EbookStatus, Format: b.EbookFormat},
		Audiobook:  bookFormatPayload{Wanted: b.WantAudiobook, Status: b.AudioStatus, Format: b.AudioFormat},
		SeriesName: b.SeriesName, SeriesPosition: b.SeriesPosition, ReleaseDate: b.ReleaseDate,
		Narrators: b.Narrators, RuntimeMin: b.RuntimeMin, ASIN: b.ASIN}
	if admin {
		p.Ebook.Path, p.Audiobook.Path = b.EbookPath, b.AudioPath
	}
	return p
}

func (s *Server) handleListBooks(w http.ResponseWriter, r *http.Request) {
	list, err := s.BookRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	admin := isAdminRequest(r)
	out := make([]bookPayload, 0, len(list))
	for _, b := range list {
		out = append(out, toBookPayload(b, admin))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) bookFromPath(w http.ResponseWriter, r *http.Request) (books.Book, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid book.")
		return books.Book{}, false
	}
	b, err := s.BookRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That book isn't in your library.")
		return books.Book{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return books.Book{}, false
	}
	return b, true
}

func (s *Server) handleGetBook(w http.ResponseWriter, r *http.Request) {
	if b, ok := s.bookFromPath(w, r); ok {
		s.lookUpAudioDetailsLater(b)
		writeJSON(w, http.StatusOK, toBookPayload(b, isAdminRequest(r)))
	}
}

type bookSearchResult struct {
	books.Found
	CoverURL  string `json:"coverUrl,omitempty"`
	LibraryID int64  `json:"libraryId,omitempty"`
}

// handleBookSearch looks books up on Open Library by title or author.
func (s *Server) handleBookSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeJSON(w, http.StatusOK, []bookSearchResult{})
		return
	}
	found, err := s.OpenLibrary.Search(r.Context(), q, 24)
	if err != nil {
		writeUpstreamError(w, "look up books", err)
		return
	}
	writeJSON(w, http.StatusOK, s.bookResults(found))
}

type addBookRequest struct {
	OLKey     string `json:"olKey"`
	Title     string `json:"title"`
	Author    string `json:"author"`
	AuthorKey string `json:"authorKey"`
	Year      int    `json:"year"`
	CoverID   int    `json:"coverId"`
	Ebook     bool   `json:"ebook"`
	Audiobook bool   `json:"audiobook"`
	SearchNow bool   `json:"searchNow"`
}

// handleAddBook adds a book with the formats asked for. Details (the
// description, a cover) are fetched from Open Library when it answers.
func (s *Server) handleAddBook(w http.ResponseWriter, r *http.Request) {
	if s.mustRequest(r) {
		s.fileRequest(w, r, "book")
		return
	}
	var req addBookRequest
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.OLKey) == "" || strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "Pick a book from the search results to add.")
		return
	}
	if !req.Ebook && !req.Audiobook {
		writeError(w, http.StatusBadRequest, "Choose ebook, audiobook or both.")
		return
	}
	if (req.Ebook && !s.ebooksEnabled()) || (req.Audiobook && !s.audiobooksEnabled()) {
		writeError(w, http.StatusConflict, "That format is switched off. Switch it on in Settings > Media types.")
		return
	}
	b := books.Book{OLKey: strings.TrimPrefix(req.OLKey, "/works/"), Title: strings.TrimSpace(req.Title), Author: strings.TrimSpace(req.Author),
		AuthorKey: req.AuthorKey, Year: req.Year, CoverID: req.CoverID, WantEbook: req.Ebook, WantAudiobook: req.Audiobook}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if work, err := s.OpenLibrary.GetWork(ctx, b.OLKey); err == nil {
		b.Description = work.Description
		if b.CoverID == 0 {
			b.CoverID = work.CoverID
		}
	}
	userID, byline := requester(r)
	b.AddedBy = userID
	created, err := s.BookRepo.Add(b)
	if errors.Is(err, books.ErrExists) {
		writeError(w, http.StatusConflict, b.Title+" is already in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("%s by %s added to the library%s", created.Name(), created.Author, byline))
	if req.SearchNow {
		for _, f := range []books.Format{books.Ebook, books.Audiobook} {
			if created.Wants(f) {
				f := f
				s.background(func() { s.searchBook(created.ID, f, false) })
			}
		}
	}
	s.lookUpAudioDetailsLater(created)
	writeJSON(w, http.StatusCreated, toBookPayload(created, isAdminRequest(r)))
}

type bookWantRequest struct {
	Format string `json:"format"`
	Wanted bool   `json:"wanted"`
}

func (s *Server) handleSetBookWant(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	var req bookWantRequest
	if err := decodeJSON(r, &req); err != nil || !books.Format(req.Format).Valid() {
		writeError(w, http.StatusBadRequest, `Send "ebook" or "audiobook".`)
		return
	}
	if err := s.BookRepo.SetWant(b.ID, books.Format(req.Format), req.Wanted); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	b, _ = s.BookRepo.Get(b.ID)
	writeJSON(w, http.StatusOK, toBookPayload(b, isAdminRequest(r)))
}

// handleDeleteBook removes a book; with ?deleteFiles=true its files go to the
// recycle bin of their folder (or are deleted when the bin is off).
func (s *Server) handleDeleteBook(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	var err error
	if r.URL.Query().Get("deleteFiles") == "true" {
		for _, f := range []books.Format{books.Ebook, books.Audiobook} {
			p := b.Path(f)
			if p == "" {
				continue
			}
			root := s.bookRoot(f)
			if err := checkRemovable(root, []string{p}); err != nil {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			label := b.Name() + " (" + string(f) + ")"
			if folder, own := bookOwnFolder(root, p); own {
				_, err = s.discardFolder(root, folder, label)
			} else {
				_, _, err = s.discardFileWithSidecars(root, p, label)
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	if err = s.BookRepo.Delete(b.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.QueueRepo.LogActivity(0, "removed", b.Name()+" removed from the library")
	writeJSON(w, http.StatusOK, nil)
}

// bookReleases searches the indexers for one format of a book and returns
// the matching releases, best first.
func (s *Server) bookReleases(ctx context.Context, b books.Book, f books.Format) ([]indexers.Result, error) {
	list, err := s.searchableIndexers()
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	matching := func(query string) []indexers.Result {
		var out []indexers.Result
		for _, res := range indexers.MergeResults(indexers.SearchAll(ctx, list, query, categoriesFor(f))) {
			if books.Matches(res.Title, b) && books.ReleaseRank(res.Title, f) > 0 {
				out = append(out, res)
			}
		}
		return out
	}
	out := matching(strings.TrimSpace(b.Author + " " + b.Title))
	if len(out) == 0 && b.Author != "" && ctx.Err() == nil {
		// Some indexers don't index the author: try the title alone (the
		// release still has to name the author to match).
		out = matching(b.Title)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := books.ReleaseRank(out[i].Title, f), books.ReleaseRank(out[j].Title, f)
		if ri != rj {
			return ri > rj
		}
		if out[i].Priority != out[j].Priority {
			return indexerRank(out[i].Priority) < indexerRank(out[j].Priority)
		}
		return out[i].Protocol == indexers.ProtocolUsenet && out[j].Protocol != indexers.ProtocolUsenet
	})
	return out, nil
}

func (s *Server) handleBookReleases(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	f := books.Format(r.URL.Query().Get("format"))
	if !f.Valid() {
		writeError(w, http.StatusBadRequest, `Choose the format: "ebook" or "audiobook".`)
		return
	}
	results, err := s.bookReleases(r.Context(), b, f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	blocked := s.blockedKeys()
	out := make([]searchResultPayload, 0, len(results))
	for _, res := range results {
		p := s.toSearchResultPayload(res)
		p.Blocklisted = blocked[blocklist.Key(res.Title)]
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
}

type bookGrabRequest struct {
	Format       string `json:"format"`
	ReleaseTitle string `json:"releaseTitle"`
	DownloadURL  string `json:"downloadUrl"`
	SizeBytes    int64  `json:"sizeBytes"`
	Protocol     string `json:"protocol"`
}

func (s *Server) handleGrabBook(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	var req bookGrabRequest
	if err := decodeJSON(r, &req); err != nil || !books.Format(req.Format).Valid() || req.ReleaseTitle == "" || req.DownloadURL == "" {
		writeError(w, http.StatusBadRequest, "Choose a release and a format.")
		return
	}
	if !s.checkGrabURL(w, r, &req.DownloadURL) {
		return
	}
	protocol := indexers.Protocol(req.Protocol)
	if protocol == "" {
		protocol = indexers.ProtocolUsenet
	}
	id, err := s.grabBook(b, books.Format(req.Format), req.ReleaseTitle, req.DownloadURL, req.SizeBytes, protocol, queue.PriorityManual)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queueId": id})
}

// grabBook puts a release for one format of a book in the download line.
func (s *Server) grabBook(b books.Book, f books.Format, title, url string, size int64, protocol indexers.Protocol, prio queue.Priority) (int64, error) {
	if protocol == indexers.ProtocolTorrent && !s.torrentsEnabled() {
		return 0, errTorrentsDisabled
	}
	s.grabMu.Lock()
	busy, err := s.QueueRepo.HasActiveForBook(b.ID, string(f))
	if err == nil && busy {
		err = errors.New("A download for this book is already running or waiting.")
	}
	var id int64
	if err == nil {
		id, err = s.QueueRepo.Enqueue(queue.Item{BookID: b.ID, BookFormat: string(f), ReleaseTitle: title, NZBURL: url, SizeBytes: size, Protocol: queue.Protocol(protocol), Priority: prio})
	}
	if err == nil {
		_ = s.BookRepo.SetState(b.ID, f, books.StatusDownloading, "", "")
	}
	s.grabMu.Unlock()
	if err != nil {
		return 0, err
	}
	_ = s.QueueRepo.LogActivity(0, "grabbed", fmt.Sprintf("Downloading %s (%s): %s", b.Name(), f, title))
	s.dispatch.Kick()
	return id, nil
}

// searchBook looks for one format of a book and grabs the best release that
// is not blocklisted. It reports whether it grabbed one.
func (s *Server) searchBook(bookID int64, f books.Format, retry bool) bool {
	if !s.formatEnabled(f) {
		return false
	}
	b, err := s.BookRepo.Get(bookID)
	if err != nil || !b.Wants(f) || b.Status(f) == books.StatusDownloaded && !retry {
		return false
	}
	if !b.Released(time.Now()) {
		return false // not out yet: nothing to find
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	results, err := s.bookReleases(ctx, b, f)
	if err != nil {
		slog.Info("books: search", "book", bookID, "err", err)
		return false
	}
	blocked := s.blockedKeys()
	for _, res := range filterSources(results, s.sourcesFor("")) {
		if blocked[blocklist.Key(res.Title)] {
			continue
		}
		if _, err := s.grabBook(b, f, res.Title, res.DownloadURL, res.SizeBytes, res.Protocol, queue.PriorityAutomatic); err == nil {
			return true
		}
		return false
	}
	if retry {
		_ = s.QueueRepo.LogActivity(0, "searched", fmt.Sprintf("No other release found for %s (%s)", b.Name(), f))
	}
	return false
}

func (s *Server) handleSearchBookNow(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	f := books.Format(r.URL.Query().Get("format"))
	if !f.Valid() {
		writeError(w, http.StatusBadRequest, `Choose the format: "ebook" or "audiobook".`)
		return
	}
	if s.searchBook(b.ID, f, false) {
		writeJSON(w, http.StatusOK, map[string]any{"grabbed": 1, "message": "Found a release. It is downloading now."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grabbed": 0, "message": "Nothing suitable was found right now."})
}

// huntBooks is the automatic search: every wanted, missing book, a few per run.
func (s *Server) huntBooks(ctx context.Context) {
	if !s.booksEnabled() {
		return
	}
	s.huntAuthors(ctx)
	s.huntSeries(ctx)
	wanted, err := s.BookRepo.Wanted()
	if err != nil {
		return
	}
	n := 0
	now := time.Now()
	for _, b := range wanted {
		if !b.Released(now) {
			continue // not out yet: it doesn't take one of the searches
		}
		for _, f := range []books.Format{books.Ebook, books.Audiobook} {
			if n >= 10 {
				return
			}
			if b.Wants(f) && b.Status(f) == books.StatusMissing && s.formatEnabled(f) {
				n++
				s.searchBook(b.ID, f, false)
			}
		}
	}
}

// bookOwnFolder is the folder that belongs to a book alone, for removing it
// whole: an audiobook folder, or an ebook's "<Author>/<Title>" folder. A file
// right in the library folder or in an author's folder has none (own is
// false), so only the file and the files named after it go.
func bookOwnFolder(root, p string) (folder string, own bool) {
	st, err := os.Stat(p)
	if err == nil && st.IsDir() {
		folder = p
	} else {
		folder = filepath.Dir(p)
	}
	rel, err := filepath.Rel(root, folder)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	if folder != p && len(strings.Split(filepath.ToSlash(rel), "/")) < 2 {
		return "", false // <root>/<Author>/<file>: the author folder holds other books
	}
	return folder, true
}
