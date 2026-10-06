package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/settings"
)

// Book series: the series a book is in, with the other books in it, and
// following a series so its missing books are added now and new ones as
// they appear (checked twice a day). Series come from Hardcover when an
// administrator saved a Hardcover token, otherwise from Open Library.

// hardcoverState keeps one Hardcover client per saved token, so its answers
// stay cached between calls.
type hardcoverState struct {
	mu     sync.Mutex
	token  string
	client *books.Hardcover
	base   string // tests point Hardcover elsewhere; "" = the real one
}

// newHardcover is a Hardcover client for token.
func (s *Server) newHardcover(token string) *books.Hardcover {
	h := books.NewHardcover(token, s.OpenLibrary.UserAgent)
	s.hc.mu.Lock()
	if s.hc.base != "" {
		h.Base = s.hc.base
	}
	s.hc.mu.Unlock()
	return h
}

// hardcover is the Hardcover client for the saved token, or nil without one.
func (s *Server) hardcover() *books.Hardcover {
	token, err := s.Settings.Get(settings.KeyHardcoverToken)
	if err != nil {
		return nil
	}
	token = books.CleanHardcoverToken(token)
	if token == "" {
		return nil
	}
	s.hc.mu.Lock()
	if s.hc.client != nil && s.hc.token == token {
		defer s.hc.mu.Unlock()
		return s.hc.client
	}
	s.hc.mu.Unlock()
	h := s.newHardcover(token)
	s.hc.mu.Lock()
	defer s.hc.mu.Unlock()
	s.hc.token, s.hc.client = token, h
	return h
}

// seriesOf finds the series of a book: Hardcover first when there is a
// token, then Open Library. nil when neither knows one.
func (s *Server) seriesOf(ctx context.Context, workKey, title, author string) (*books.Series, error) {
	if hc := s.hardcover(); hc != nil && title != "" {
		series, err := hc.SeriesFor(ctx, title, author)
		if err == nil && series != nil {
			linked := *series
			linked.Entries = append([]books.SeriesEntry(nil), series.Entries...)
			s.OpenLibrary.LinkToOpenLibrary(ctx, &linked)
			return &linked, nil
		}
		if err != nil {
			slog.Info("books: Hardcover series lookup failed, using Open Library", "err", err)
		}
	}
	return s.OpenLibrary.SeriesOf(ctx, workKey)
}

// loadSeries fetches a followed series by its source and key.
func (s *Server) loadSeries(ctx context.Context, source, key, name string) (*books.Series, error) {
	if source == books.SourceHardcover {
		hc := s.hardcover()
		if hc == nil {
			return nil, errors.New("this series comes from Hardcover, and no Hardcover token is saved")
		}
		id, _ := strconv.Atoi(key)
		series, err := hc.SeriesBooks(ctx, id)
		if err != nil {
			return nil, err
		}
		linked := *series
		linked.Entries = append([]books.SeriesEntry(nil), series.Entries...)
		s.OpenLibrary.LinkToOpenLibrary(ctx, &linked)
		return &linked, nil
	}
	return s.OpenLibrary.SeriesBooks(ctx, key, name)
}

type bookSeriesEntryPayload struct {
	books.SeriesEntry
	CoverURL  string `json:"coverUrl,omitempty"`
	LibraryID int64  `json:"libraryId,omitempty"`
}

type bookSeriesPayload struct {
	Source    string                   `json:"source"`
	Key       string                   `json:"key"`
	Name      string                   `json:"name"`
	Position  string                   `json:"position,omitempty"` // the book asked about
	Followed  bool                     `json:"followed"`
	Ebook     bool                     `json:"ebook"`     // formats wanted, when followed
	Audiobook bool                     `json:"audiobook"` //
	Entries   []bookSeriesEntryPayload `json:"entries"`
}

func (s *Server) toBookSeriesPayload(series *books.Series, workKey, title string) bookSeriesPayload {
	p := bookSeriesPayload{Source: series.Source, Key: series.Key, Name: series.Name, Entries: []bookSeriesEntryPayload{}}
	library, _ := s.BookRepo.List()
	for _, e := range series.Entries {
		ep := bookSeriesEntryPayload{SeriesEntry: e, CoverURL: books.CoverURL(e.CoverID, "M")}
		for _, b := range library {
			if (e.Key != "" && b.OLKey == e.Key) || (books.SameTitle(b.Title, e.Title, true) && books.SameAuthor(b.Author, e.Author)) {
				ep.LibraryID = b.ID
				break
			}
		}
		if (e.Key != "" && e.Key == workKey) || (p.Position == "" && title != "" && books.SameTitle(e.Title, title, true)) {
			p.Position = e.Position
		}
		p.Entries = append(p.Entries, ep)
	}
	if followed, err := s.BookRepo.ListFollowedSeries(); err == nil {
		for _, f := range followed {
			if f.Source == series.Source && f.Key == series.Key {
				p.Followed, p.Ebook, p.Audiobook = true, f.WantEbook, f.WantAudiobook
			}
		}
	}
	return p
}

// GET /api/book-works/{key}/series?title=&author=: the series this book is in
// ({"series": null} when none is known).
func (s *Server) handleBookSeries(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.PathValue("key"))
	if key == "" || len(key) > 32 || strings.ContainsAny(key, "/?#: ") {
		writeError(w, http.StatusBadRequest, "That isn't an Open Library book.")
		return
	}
	title, author := strings.TrimSpace(r.URL.Query().Get("title")), strings.TrimSpace(r.URL.Query().Get("author"))
	local, inLibrary, _ := s.BookRepo.GetByKey(key)
	if inLibrary {
		title, author = local.Title, local.Author
	}
	series, err := s.seriesOf(r.Context(), key, title, author)
	if errors.Is(err, books.ErrNotFound) || (err == nil && series == nil) {
		writeJSON(w, http.StatusOK, map[string]any{"series": nil})
		return
	}
	if err != nil {
		writeUpstreamError(w, "load the book's series", err)
		return
	}
	p := s.toBookSeriesPayload(series, key, title)
	if inLibrary {
		s.rememberSeries(local, series, p.Position)
	}
	writeJSON(w, http.StatusOK, map[string]any{"series": p})
}

// rememberSeries stores a library book's series, place and release date when
// they changed.
func (s *Server) rememberSeries(b books.Book, series *books.Series, position string) {
	if position != "" && (b.SeriesName != series.Name || b.SeriesPosition != position) {
		_ = s.BookRepo.SetSeries(b.ID, series.Name, position)
	}
	for _, e := range series.Entries {
		if e.Key == b.OLKey && e.ReleaseDate != "" && e.ReleaseDate != b.ReleaseDate {
			_ = s.BookRepo.SetReleaseDate(b.ID, e.ReleaseDate)
		}
	}
}

// GET /api/book-series: the series followed.
func (s *Server) handleFollowedSeries(w http.ResponseWriter, r *http.Request) {
	list, err := s.BookRepo.ListFollowedSeries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func seriesFromPath(w http.ResponseWriter, r *http.Request) (source, key string, ok bool) {
	source, key = r.PathValue("source"), r.PathValue("key")
	if !books.ValidSeriesKey(source, key) {
		writeError(w, http.StatusBadRequest, "That isn't a series Mediarium knows.")
		return "", "", false
	}
	return source, key, true
}

type followSeriesRequest struct {
	Name      string `json:"name"`
	Ebook     bool   `json:"ebook"`
	Audiobook bool   `json:"audiobook"`
}

// PUT /api/book-series/{source}/{key}/follow: follow a series (or change the
// formats) and add the books of it that are missing.
func (s *Server) handleFollowSeries(w http.ResponseWriter, r *http.Request) {
	source, key, ok := seriesFromPath(w, r)
	if !ok {
		return
	}
	var req followSeriesRequest
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" || len(req.Name) > 200 {
		writeError(w, http.StatusBadRequest, "Send the series' name.")
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
	f := books.FollowedSeries{Source: source, Key: key, Name: strings.TrimSpace(req.Name), WantEbook: req.Ebook, WantAudiobook: req.Audiobook}
	if err := s.BookRepo.FollowSeries(f); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	by, byline := requester(r)
	_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("Following the %s series: its missing and new books are added by themselves%s", f.Name, byline))
	added, err := s.addSeriesBooks(r.Context(), f, by)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"added": 0, "message": "Following it. The books couldn't be loaded just now; Mediarium tries again later."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": added})
}

// DELETE /api/book-series/{source}/{key}/follow: stop following. Its books stay.
func (s *Server) handleUnfollowSeries(w http.ResponseWriter, r *http.Request) {
	source, key, ok := seriesFromPath(w, r)
	if !ok {
		return
	}
	if err := s.BookRepo.UnfollowSeries(source, key); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

// addSeriesBooks adds the books of a followed series that the library lacks:
// whole places only (no novellas in between, no box sets), ones Open Library
// has. Books already out are searched for straight away; the rest wait for
// their release date. It answers how many were added.
func (s *Server) addSeriesBooks(ctx context.Context, f books.FollowedSeries, by int64) (int, error) {
	series, err := s.loadSeries(ctx, f.Source, f.Key, f.Name)
	if err != nil {
		return 0, err
	}
	library, err := s.BookRepo.List()
	if err != nil {
		return 0, err
	}
	now := time.Now()
	added := 0
	for _, e := range series.Entries {
		if !books.MainPosition(e.Position) || e.Key == "" {
			continue
		}
		found := books.Found{Key: e.Key, Title: e.Title, Author: e.Author}
		if have := findBook(library, found); have != nil {
			if have.SeriesName == "" {
				_ = s.BookRepo.SetSeries(have.ID, series.Name, e.Position)
			}
			// A release date that moved (a book postponed) is kept up to date.
			if e.ReleaseDate != "" && e.ReleaseDate != have.ReleaseDate {
				_ = s.BookRepo.SetReleaseDate(have.ID, e.ReleaseDate)
			}
			continue
		}
		b, err := s.BookRepo.Add(books.Book{OLKey: e.Key, Title: e.Title, Author: e.Author, AuthorKey: e.AuthorKey, Year: e.Year, CoverID: e.CoverID,
			WantEbook: f.WantEbook && s.ebooksEnabled(), WantAudiobook: f.WantAudiobook && s.audiobooksEnabled(), AddedBy: by,
			SeriesName: series.Name, SeriesPosition: e.Position, ReleaseDate: e.ReleaseDate})
		if errors.Is(err, books.ErrExists) || (err == nil && !b.WantEbook && !b.WantAudiobook) {
			continue
		}
		if err != nil {
			slog.Warn("books: add a book of a followed series", "series", series.Name, "err", err)
			continue
		}
		library = append(library, b)
		added++
		_ = s.QueueRepo.LogActivity(0, "added", fmt.Sprintf("%s, book %s of %s, added", b.Name(), e.Position, series.Name))
		if !b.Released(now) {
			continue
		}
		for _, bf := range []books.Format{books.Ebook, books.Audiobook} {
			if b.Wants(bf) {
				bf := bf
				s.background(func() { s.searchBook(b.ID, bf, false) })
			}
		}
	}
	return added, nil
}

// findBook is the library book for f (by key, or same title and author).
func findBook(library []books.Book, f books.Found) *books.Book {
	for i, b := range library {
		if b.OLKey == f.Key || (books.SameTitle(b.Title, f.Title, true) && books.SameAuthor(b.Author, f.Author)) {
			return &library[i]
		}
	}
	return nil
}

// huntSeries looks at every followed series at most every 12 hours and adds
// the books that appeared since.
func (s *Server) huntSeries(ctx context.Context) {
	s.seriesCheck.mu.Lock()
	if time.Since(s.seriesCheck.last) < authorCheckEvery {
		s.seriesCheck.mu.Unlock()
		return
	}
	s.seriesCheck.last = time.Now()
	s.seriesCheck.mu.Unlock()
	followed, err := s.BookRepo.ListFollowedSeries()
	if err != nil {
		return
	}
	for _, f := range followed {
		if ctx.Err() != nil {
			return
		}
		if _, err := s.addSeriesBooks(ctx, f, 0); err != nil {
			slog.Info("books: check followed series", "series", f.Name, "err", err)
		}
	}
}

// ---- The Hardcover token (Settings > Info, lists and subtitles)

type hardcoverTokenState struct {
	Set      bool   `json:"set"`
	Username string `json:"username,omitempty"` // after a successful save
}

// GET /api/settings/hardcover
func (s *Server) handleGetHardcover(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, hardcoverTokenState{Set: s.hardcover() != nil})
}

// PUT /api/settings/hardcover {"token": "..."}: checks the token with
// Hardcover and saves it (encrypted). An empty token removes it.
func (s *Server) handlePutHardcover(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	token := books.CleanHardcoverToken(req.Token)
	if len(token) > 4096 || strings.ContainsAny(token, " \r\n\t") {
		writeError(w, http.StatusBadRequest, "That doesn't look like a Hardcover token. Copy it again from hardcover.app, Settings, Hardcover API.")
		return
	}
	state := hardcoverTokenState{}
	if token != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		name, err := s.newHardcover(token).Me(ctx)
		if errors.Is(err, books.ErrHardcoverToken) {
			writeError(w, http.StatusBadRequest, "Hardcover didn't accept that token. Copy it again from hardcover.app, Settings, Hardcover API, and check it hasn't expired.")
			return
		}
		if err != nil {
			writeUpstreamError(w, "check the token with Hardcover", err)
			return
		}
		state = hardcoverTokenState{Set: true, Username: name}
	}
	if err := s.Settings.Set(settings.KeyHardcoverToken, token, true); err != nil {
		writeError(w, http.StatusInternalServerError, "The token couldn't be saved.")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// ---- Audiobook details (Audnexus)

// audioLookups keeps track of the books whose audiobook details are being
// looked up, so opening a page twice doesn't ask twice.
var audioLookups sync.Map

// lookUpAudioDetailsLater fetches the narrator, running time (and, when
// missing, the series) of a book's audiobook in the background, once per book.
func (s *Server) lookUpAudioDetailsLater(b books.Book) {
	if b.AudioCheckedAt != "" || !s.audiobooksEnabled() || (!b.WantAudiobook && b.AudioStatus != books.StatusDownloaded) {
		return
	}
	if _, busy := audioLookups.LoadOrStore(b.ID, true); busy {
		return
	}
	s.background(func() {
		defer audioLookups.Delete(b.ID)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		d, err := s.Audnexus.Lookup(ctx, b.Title, b.Author)
		if err != nil && !errors.Is(err, books.ErrNotFound) {
			slog.Info("books: audiobook details", "book", b.ID, "err", err)
			return // a network problem: try again another time
		}
		if err := s.BookRepo.SetAudioDetails(b.ID, d); err != nil {
			slog.Warn("books: save audiobook details", "book", b.ID, "err", err)
			return
		}
		if b.SeriesName == "" && d.SeriesName != "" {
			_ = s.BookRepo.SetSeries(b.ID, d.SeriesName, d.SeriesPosition)
		}
		// An audiobook-only book that isn't out yet waits for its day.
		if b.ReleaseDate == "" && !b.WantEbook && d.ReleaseDate > time.Now().Format("2006-01-02") {
			_ = s.BookRepo.SetReleaseDate(b.ID, d.ReleaseDate)
		}
	})
}
