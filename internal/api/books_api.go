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
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/quality"
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
	CoverChosen bool              `json:"coverChosen,omitempty"` // a person gave this book its cover ("Remove my cover" is on offer)
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
	p := bookPayload{ID: b.ID, OLKey: b.OLKey, Title: b.Title, Author: b.Author, AuthorKey: b.AuthorKey, Year: b.Year, CoverURL: b.CoverPath("M"), CoverChosen: b.OwnCover > 0 || b.PickedCoverID > 0,
		Description: b.Description, AddedAt: b.AddedAt,
		Ebook:      bookFormatPayload{Wanted: b.WantEbook, Status: b.EbookStatus, Format: b.EbookFormat},
		Audiobook:  bookFormatPayload{Wanted: b.WantAudiobook, Status: b.AudioStatus, Format: b.AudioFormat},
		SeriesName: b.SeriesName, SeriesPosition: b.SeriesPosition, ReleaseDate: b.ReleaseDate,
		Narrators: b.Narrators, RuntimeMin: b.RuntimeMin, ASIN: b.ASIN}
	if admin {
		p.Ebook.Path, p.Audiobook.Path = b.EbookPath, b.AudioPath
	}
	if p.CoverURL == "" && hasEbook(b) && books.ComicFormat(ebookExt(b)) {
		// A comic has no Open Library cover: its first page is the cover.
		p.CoverURL = fmt.Sprintf("/api/books/%d/file-cover", b.ID)
	}
	return p
}

// handleListBooks lists the library's books. With ?brief=1 each book's
// description is left out, as an overview is for movies.
func (s *Server) handleListBooks(w http.ResponseWriter, r *http.Request) {
	brief := r.URL.Query().Get("brief") == "1"
	list, err := s.BookRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	admin := isAdminRequest(r)
	out := make([]bookPayload, 0, len(list))
	var covers []int
	for _, b := range list {
		p := toBookPayload(b, admin)
		if brief {
			p.Description = ""
		}
		out = append(out, p)
		covers = append(covers, b.OLCoverID())
	}
	s.warmBookCovers("M", covers)
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

// hardcoverSearchWait is how long a book search waits for Hardcover. After
// that the answer is Open Library's alone, so a slow Hardcover cannot hold
// the search for as long as two full API calls.
const hardcoverSearchWait = 4 * time.Second

// handleBookSearch looks books up on Open Library, and on Hardcover when a token is saved.
// Hardcover fills in covers Open Library doesn't have, and books only Hardcover
// knows are listed too. Google Books is asked afterwards, for a few seconds, with
// or without an API key: it fills a missing cover or description, and a book
// neither of the others lists is added from Google. If Google does not answer,
// the books already found are shown. Without a Hardcover token, that part is skipped.
func (s *Server) handleBookSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeJSON(w, http.StatusOK, []bookSearchResult{})
		return
	}
	// With an author (Find it myself on the import list sends the title as q
	// and the author apart), both go to the catalogues, and the books by that
	// author come first.
	author := strings.TrimSpace(r.URL.Query().Get("author"))
	query := q
	if author != "" {
		query += " " + author
	}
	var (
		found   []books.Found
		olErr   error
		hcFound []books.Found
		hcErr   error
	)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		found, olErr = s.OpenLibrary.Search(r.Context(), query, 24)
	}()
	var cancel context.CancelFunc
	if hc := s.hardcover(); hc != nil {
		var hcCtx context.Context
		hcCtx, cancel = context.WithTimeout(r.Context(), hardcoverSearchWait)
		wg.Add(1)
		go func() {
			defer wg.Done()
			hcFound, hcErr = hc.Search(hcCtx, query, 24)
		}()
	}
	wg.Wait()
	if cancel != nil {
		cancel()
	}
	if hcErr != nil {
		slog.Info("books: Hardcover search failed, using Open Library", "err", hcErr)
		hcFound = nil
	}
	if olErr != nil && len(hcFound) == 0 {
		if gb := s.googleHits(r.Context(), query, s.infoLanguage()); len(gb) > 0 {
			writeJSON(w, http.StatusOK, s.bookResults(gb))
			return
		}
		if errors.Is(olErr, books.ErrBadQuery) {
			slog.Info("open library refused a book search", "err", olErr)
			writeError(w, http.StatusUnprocessableEntity, "Open Library couldn't read that search. Try fewer or simpler words.")
			return
		}
		writeUpstreamError(w, "look up books", olErr)
		return
	}
	if olErr != nil {
		slog.Info("books: Open Library search failed, using Hardcover", "err", olErr)
		found = nil
	}
	found = books.MergeHardcover(found, hcFound)
	if author != "" {
		books.RankByTitleAndAuthor(found, q, author)
	} else {
		newestFirst(found, q, func(f books.Found) string { return f.Title }, func(f books.Found) int { return f.Year })
	}
	// Google is residual: one short query after the others, remembered so the
	// next keystroke does not ask again.
	found = books.MergeGoogle(found, s.googleHits(r.Context(), query, s.infoLanguage()))
	writeJSON(w, http.StatusOK, s.bookResults(found))
}

type addBookRequest struct {
	OLKey       string `json:"olKey"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	AuthorKey   string `json:"authorKey"`
	Year        int    `json:"year"`
	CoverID     int    `json:"coverId"`
	HardcoverID int    `json:"hardcoverId"`
	GoogleID    string `json:"googleId"`
	Ebook       bool   `json:"ebook"`
	Audiobook   bool   `json:"audiobook"`
	SearchNow   bool   `json:"searchNow"`
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
	s.finishAddBook(w, r, req)
}

// finishAddBook stores a book that was already chosen from Open Library or
// Hardcover. The caller has checked the formats.
func (s *Server) finishAddBook(w http.ResponseWriter, r *http.Request, req addBookRequest) {
	b := books.Book{OLKey: strings.TrimPrefix(req.OLKey, "/works/"), Title: strings.TrimSpace(req.Title), Author: strings.TrimSpace(req.Author),
		AuthorKey: req.AuthorKey, Year: req.Year, CoverID: req.CoverID, WantEbook: req.Ebook, WantAudiobook: req.Audiobook}
	gbID := ""
	if id, ok := books.ParseGoogleKey(b.OLKey); ok {
		gbID = id
	}
	if books.ValidGoogleID(strings.TrimSpace(req.GoogleID)) {
		gbID = strings.TrimSpace(req.GoogleID)
	}
	timeout := 10 * time.Second
	if _, ok := books.ParseHardcoverKey(b.OLKey); ok || req.HardcoverID > 0 || gbID != "" {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	hcID, cover := s.prepareNewBook(ctx, &b, req.HardcoverID)
	if b.Description == "" && gbID != "" {
		if info, err := s.googleWork(ctx, gbID); err == nil && info.Description != "" {
			b.Description = info.Description
		}
	}
	userID, byline := requester(r)
	b.AddedBy = userID
	created, err := s.BookRepo.Add(b)
	if errors.Is(err, books.ErrExists) {
		writeError(w, http.StatusConflict, fmt.Sprintf("%s is already in your library.", b.Title))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if hcID > 0 && created.CoverID == 0 && created.OwnCover == 0 {
		s.keepHardcoverCover(ctx, created, hcID, cover)
		if fresh, err := s.BookRepo.Get(created.ID); err == nil {
			created = fresh
		}
	}
	if gbID != "" && created.CoverID == 0 && created.OwnCover == 0 {
		s.keepGoogleCover(ctx, created.ID, gbID)
		if fresh, err := s.BookRepo.Get(created.ID); err == nil {
			created = fresh
		}
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
	_ = s.QueueRepo.LogActivity(0, "removed", fmt.Sprintf("%s removed from the library", b.Name()))
	writeJSON(w, http.StatusOK, nil)
}

// bookSearchWait is how long a search for a book's releases may take while a
// person waits for the page: both passes share it, and what the quick
// indexers found by then is shown. (The page gives up after 30 seconds.)
const bookSearchWait = 25 * time.Second

// bookWaitFor is the longest a book release search a person waits for may take.
func (s *Server) bookWaitFor() time.Duration {
	if s.bookWait > 0 {
		return s.bookWait
	}
	return bookSearchWait
}

// bookSearch is what a search for a book's releases found, and which
// indexers it did not ask.
type bookSearch struct {
	Results  []indexers.Result
	Fits     []quality.LanguageFit // how each of Results suits the wanted languages (book_language.go)
	Asked    int                   // how many indexers were asked
	Skipped  []string              // enabled indexers left out because they don't list this kind of release
	Outcomes []indexers.Outcome    // what each indexer said, for both passes
	All      []indexers.Instance   // every indexer that could have been asked
}

// autoBookReleases searches the indexers for one format of a book and returns
// the matching releases, best first.
// Releases clearly in a language that isn't wanted are left out: automation
// never takes those.
func (s *Server) autoBookReleases(ctx context.Context, b books.Book, f books.Format) ([]indexers.Result, error) {
	found, err := s.searchBookReleases(ctx, b, f, 0)
	var out []indexers.Result
	for i, res := range found.Results {
		if found.Fits[i].Accepted {
			out = append(out, res)
		}
	}
	return out, err
}

// searchBookReleases is bookReleases with the details. Only the indexers that
// can do this kind of release are asked: a definition-based site that lists no
// books (or audiobooks) is left out, so a search is not held up by movie
// sites. wait, when above zero, is the longest the whole search may take;
// both passes share it and the releases found by then are returned.
func (s *Server) searchBookReleases(ctx context.Context, b books.Book, f books.Format, wait time.Duration) (bookSearch, error) {
	var found bookSearch
	list, err := s.searchableIndexers()
	if err != nil {
		return found, err
	}
	cats := categoriesFor(f)
	found.All = list
	list, left := indexers.SplitByCategories(ctx, list, cats)
	for _, inst := range left {
		if inst.Enabled {
			found.Skipped = append(found.Skipped, inst.Name)
		}
	}
	for _, inst := range list {
		if inst.Enabled {
			found.Asked++
		}
	}
	if len(list) == 0 {
		return found, nil
	}
	if wait > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, wait)
		defer cancel()
	}
	matching := func(asked []indexers.Instance, query string) []indexers.Result {
		var out []indexers.Result
		outcomes := indexers.SearchAll(ctx, asked, query, cats)
		found.Outcomes = append(found.Outcomes, outcomes...)
		for _, res := range indexers.MergeResults(outcomes) {
			if books.Matches(res.Title, b) && books.ReleaseRank(res.Title, f) > 0 {
				out = append(out, res)
			}
		}
		return out
	}
	out := matching(list, strings.TrimSpace(b.Author+" "+b.Title))
	if len(out) == 0 && b.Author != "" && ctx.Err() == nil {
		// Some indexers don't index the author: try the title alone (the
		// release still has to name the author to match). An indexer that
		// failed or timed out the first time is not asked again: it would only
		// take as long again.
		failed := map[string]bool{}
		for _, o := range found.Outcomes {
			if o.Err != nil || o.Skipped {
				failed[o.IndexerName] = true
			}
		}
		var again []indexers.Instance
		for _, inst := range list {
			if !failed[inst.Name] {
				again = append(again, inst)
			}
		}
		if len(again) > 0 {
			out = matching(again, b.Title)
		}
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
	// A release in a wanted language goes first; the name says the language,
	// or the indexer's category for it does.
	found.Results, found.Fits = sortBooksByLanguage(out, s.bookLanguageFits(ctx, out, list))
	return found, nil
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
	found, err := s.searchBookReleases(r.Context(), b, f, s.bookWaitFor())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	blocked := s.searchSkipKeys()
	out := make([]searchResultPayload, 0, len(found.Results))
	for i, res := range found.Results {
		p := s.toSearchResultPayload(res)
		p.Blocklisted = blocked[blocklist.Key(res.Title)]
		p.Categories = res.Categories
		p.setLanguage(found.Fits[i])
		out = append(out, p)
	}
	s.markCachedPayloads(r.Context(), out)
	writeSearchAll(w, r, out, found.Asked, found.Outcomes, found.Skipped, found.All)
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
	if protocol == indexers.ProtocolTorrent && !s.torrentReleasesOn() {
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
	results, err := s.autoBookReleases(ctx, b, f)
	if err != nil {
		slog.Info("books: search", "book", bookID, "err", err)
		return false
	}
	blocked := s.searchSkipKeys()
	for _, res := range filterSources(s.autoCandidates(ctx, results), s.sourcesFor("")) {
		if searchSkips(res, blocked) {
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
	type bookSearch struct {
		id     int64
		format books.Format
	}
	var todo []bookSearch
	now := time.Now()
	for _, b := range wanted {
		if !b.Released(now) {
			continue // not out yet: it doesn't take one of the searches
		}
		for _, f := range []books.Format{books.Ebook, books.Audiobook} {
			if b.Wants(f) && b.Status(f) == books.StatusMissing && s.formatEnabled(f) {
				todo = append(todo, bookSearch{b.ID, f})
			}
		}
	}
	if len(todo) == 0 {
		return
	}
	// A few per run, and each run goes on where the last one stopped, so a long
	// list of books that cannot be found does not hold back the ones behind it.
	s.huntAt.mu.Lock()
	start, n := huntWindow(len(todo), s.huntAt.books, maxBookSearchesPerHunt)
	s.huntAt.books = start + n
	s.huntAt.mu.Unlock()
	for i := 0; i < n; i++ {
		if ctx.Err() != nil {
			return
		}
		t := todo[(start+i)%len(todo)]
		s.searchBook(t.id, t.format, false)
	}
}

// huntWindow says where a scheduled run over total items starts (offset is
// where the last one ended, which may be past the end) and how many items it
// takes, at most max. The items wrap around.
func huntWindow(total, offset, max int) (start, n int) {
	if total <= 0 {
		return 0, 0
	}
	return offset % total, min(total, max)
}

// maxBookSearchesPerHunt is how many books one scheduled run searches for.
const maxBookSearchesPerHunt = 10

// huntPosition is where the scheduled movie and book searches go on from.
type huntPosition struct {
	mu            sync.Mutex
	movies, books int
	// episodes is when the scheduled TV search last looked for each episode
	// (by id), so a show with more missing episodes than one run can search
	// goes on with the ones not tried yet instead of starting over.
	episodes map[int64]time.Time
}

// noteEpisode records that the scheduled search just looked for an episode.
func (h *huntPosition) noteEpisode(id int64, at time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.episodes == nil || len(h.episodes) > 100000 {
		h.episodes = map[int64]time.Time{}
	}
	h.episodes[id] = at
}

// orderEpisodes puts the episodes the scheduled search tried longest ago (or
// never) first, inside each season and among the seasons of one show. The
// order of the shows themselves is kept.
func (h *huntPosition) orderEpisodes(wants []tvWant) {
	h.mu.Lock()
	last := func(id int64) time.Time { return h.episodes[id] }
	oldest := make([]time.Time, len(wants))
	for i := range wants {
		eps := wants[i].episodes
		sort.SliceStable(eps, func(a, b int) bool { return last(eps[a].ID).Before(last(eps[b].ID)) })
		if len(eps) > 0 {
			oldest[i] = last(eps[0].ID)
		}
	}
	h.mu.Unlock()
	rank := map[int64]int{}
	for _, w := range wants {
		if _, ok := rank[w.series.ID]; !ok {
			rank[w.series.ID] = len(rank)
		}
	}
	idx := make([]int, len(wants))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ra, rb := rank[wants[idx[a]].series.ID], rank[wants[idx[b]].series.ID]
		if ra != rb {
			return ra < rb
		}
		return oldest[idx[a]].Before(oldest[idx[b]])
	})
	sorted := make([]tvWant, len(wants))
	for i, j := range idx {
		sorted[i] = wants[j]
	}
	copy(wants, sorted)
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
