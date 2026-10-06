package api

import (
	"errors"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/mediafiles"
)

// Reading and listening in Mediarium itself (the Books app at /bookshelf):
// the ebook file for the reader, an audiobook's tracks for the player, and
// where each person is in each book.

var ebookTypes = map[string]string{
	"epub": "application/epub+zip",
	"pdf":  "application/pdf",
	"mobi": "application/x-mobipocket-ebook",
	"azw3": "application/vnd.amazon.ebook",
}

// GET /api/books/{id}/read: the ebook file. ?download=1 saves it instead.
func (s *Server) handleReadBook(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	if b.EbookPath == "" {
		writeError(w, http.StatusNotFound, "There is no ebook for this book yet.")
		return
	}
	folder, err := mediafiles.ForFile(b.EbookPath, []string{s.ebooksRoot()}, false)
	if err != nil {
		writeStreamFolderError(w, err)
		return
	}
	rel, ok := folder.Rel(b.EbookPath)
	if !ok {
		writeError(w, http.StatusForbidden, "This book's file isn't inside your ebooks folder.")
		return
	}
	file, info, err := folder.Open(rel)
	if !s.openedOK(w, err) {
		return
	}
	defer file.Close()
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(rel)), ".")
	// The reader asks for Kindle books as EPUB (see books_convert.go).
	if r.URL.Query().Get("as") == "epub" && kindleFormat(ext) && r.URL.Query().Get("download") == "" {
		s.serveConverted(w, r, filepath.Join(folder.Dir, filepath.FromSlash(rel)), info, path.Base(rel))
		return
	}
	ct := ebookTypes[ext]
	if ct == "" {
		ct = "application/octet-stream"
	}
	name := path.Base(rel)
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("X-Content-Type-Options", "nosniff")
	disposition := "attachment"
	if ext == "pdf" && r.URL.Query().Get("download") == "" {
		// Shown by the browser's own PDF viewer, which a sandbox would block.
		disposition = "inline"
		h.Set("Content-Security-Policy", "default-src 'none'; object-src 'self'; style-src 'unsafe-inline'; img-src data: blob:; frame-ancestors 'self'")
		h.Set("X-Frame-Options", "SAMEORIGIN") // the reader shows it in a frame
	} else {
		h.Set("Content-Security-Policy", "sandbox")
	}
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	http.ServeContent(w, r, name, info.ModTime(), file)
}

func (s *Server) openedOK(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, mediafiles.ErrInvalidPath):
		writeError(w, http.StatusBadRequest, "That file path isn't valid.")
	case errors.Is(err, fs.ErrNotExist):
		writeError(w, http.StatusNotFound, "Couldn't find the file. It may have been moved or deleted.")
	default:
		writeFileReadError(w, err)
	}
	return false
}

type bookTrack struct {
	Index    int             `json:"index"`
	Name     string          `json:"name"`
	Size     int64           `json:"size"`
	Chapters []books.Chapter `json:"chapters,omitempty"` // marks inside the file (an M4B), when it has two or more
}

// chapterCache keeps the chapters read from each file, by path, size and
// modification time, so a long book's index is read once.
var chapterCache sync.Map

type chapterKey struct {
	path  string
	size  int64
	mtime int64
}

// fileChapters reads the chapter marks of an M4B/M4A/MP4 file (nil for other
// files, or when it has none).
func fileChapters(full string, st os.FileInfo) []books.Chapter {
	switch strings.ToLower(filepath.Ext(full)) {
	case ".m4b", ".m4a", ".mp4", ".aax":
	default:
		return nil
	}
	key := chapterKey{full, st.Size(), st.ModTime().UnixNano()}
	if v, ok := chapterCache.Load(key); ok {
		return v.([]books.Chapter)
	}
	f, err := os.Open(full)
	if err != nil {
		return nil
	}
	defer f.Close()
	ch, err := books.ReadChapters(f, st.Size())
	if err != nil {
		slog.Info("books: read chapters", "file", filepath.Base(full), "err", err)
		return nil
	}
	chapterCache.Store(key, ch)
	return ch
}

// audiobookTracks lists an audiobook's audio files in playing order, with the
// folder they are served from.
func (s *Server) audiobookTracks(b books.Book) (mediafiles.Folder, []string, error) {
	if b.AudioPath == "" {
		return mediafiles.Folder{}, nil, fs.ErrNotExist
	}
	roots := []string{s.audiobooksRoot()}
	st, err := os.Stat(b.AudioPath)
	if err != nil {
		return mediafiles.Folder{}, nil, err
	}
	if !st.IsDir() {
		// A single file (an audiobook kept loose in the folder).
		folder, err := mediafiles.ForFile(b.AudioPath, roots, false)
		if err != nil {
			return folder, nil, err
		}
		rel, ok := folder.Rel(b.AudioPath)
		if !ok {
			return folder, nil, mediafiles.ErrOutside
		}
		return folder, []string{rel}, nil
	}
	folder, err := mediafiles.ForDir(b.AudioPath, roots)
	if err != nil {
		return folder, nil, err
	}
	files, _ := books.AudioFiles(b.AudioPath)
	var rels []string
	for _, f := range files {
		if rel, ok := folder.Rel(f); ok {
			rels = append(rels, rel)
		}
	}
	sort.SliceStable(rels, func(i, j int) bool { return naturalLess(rels[i], rels[j]) })
	return folder, rels, nil
}

// naturalLess orders names the way people count: "2 - x" before "10 - x".
func naturalLess(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	for a != "" && b != "" {
		if unicode.IsDigit(rune(a[0])) && unicode.IsDigit(rune(b[0])) {
			na, ra := leadingNumber(a)
			nb, rb := leadingNumber(b)
			if na != nb {
				return na < nb
			}
			a, b = ra, rb
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func leadingNumber(s string) (int, string) {
	i := 0
	for i < len(s) && unicode.IsDigit(rune(s[i])) {
		i++
	}
	n, _ := strconv.Atoi(s[:min(i, 9)])
	return n, s[i:]
}

// GET /api/books/{id}/tracks: the audiobook's tracks in order.
func (s *Server) handleBookTracks(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	folder, rels, err := s.audiobookTracks(b)
	if err != nil {
		writeStreamFolderError(w, err)
		return
	}
	out := make([]bookTrack, 0, len(rels))
	for i, rel := range rels {
		t := bookTrack{Index: i, Name: strings.TrimSuffix(path.Base(rel), path.Ext(rel))}
		full := filepath.Join(folder.Dir, filepath.FromSlash(rel))
		if st, err := os.Stat(full); err == nil {
			t.Size = st.Size()
			t.Chapters = fileChapters(full, st)
		}
		out = append(out, t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tracks": out, "format": b.AudioFormat})
}

// GET /api/books/{id}/listen/{n}: one track, with ranges for seeking.
func (s *Server) handleListenBook(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 {
		writeError(w, http.StatusBadRequest, "That isn't a valid track.")
		return
	}
	folder, rels, err := s.audiobookTracks(b)
	if err != nil {
		writeStreamFolderError(w, err)
		return
	}
	if n >= len(rels) {
		writeError(w, http.StatusNotFound, "This audiobook has no such track.")
		return
	}
	rel := rels[n]
	ct := mediafiles.AudioContentType(rel)
	if ct == "" {
		writeError(w, http.StatusUnsupportedMediaType, "This file can't be played in the browser.")
		return
	}
	file, info, err := folder.Open(rel)
	if !s.openedOK(w, err) {
		return
	}
	defer file.Close()
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": path.Base(rel)}))
	http.ServeContent(w, r, path.Base(rel), info.ModTime(), file)
}

// GET /api/books/progress: where the person is in every book.
func (s *Server) handleListBookProgress(w http.ResponseWriter, r *http.Request) {
	userID, _ := requester(r)
	if userID == 0 {
		writeJSON(w, http.StatusOK, []books.Progress{})
		return
	}
	list, err := s.BookRepo.ListProgress(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// GET /api/books/{id}/progress?format=: where the person is in one format.
func (s *Server) handleGetBookProgress(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	f := books.Format(r.URL.Query().Get("format"))
	if !f.Valid() {
		writeError(w, http.StatusBadRequest, `Choose the format: "ebook" or "audiobook".`)
		return
	}
	userID, _ := requester(r)
	p, _, err := s.BookRepo.GetProgress(userID, b.ID, f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

type bookProgressRequest struct {
	Format   string  `json:"format"`
	Position string  `json:"position"`
	Percent  float64 `json:"percent"`
	Finished bool    `json:"finished"`
}

// PUT /api/books/{id}/progress: saves where the person is.
func (s *Server) handleSetBookProgress(w http.ResponseWriter, r *http.Request) {
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	var req bookProgressRequest
	if err := decodeJSON(r, &req); err != nil || !books.Format(req.Format).Valid() || len(req.Position) > 2000 {
		writeError(w, http.StatusBadRequest, "Send the format and the position.")
		return
	}
	userID, _ := requester(r)
	if userID == 0 {
		writeJSON(w, http.StatusOK, nil) // an API key has no reading place to keep
		return
	}
	p := books.Progress{BookID: b.ID, Format: books.Format(req.Format), Position: req.Position, Percent: req.Percent, Finished: req.Finished}
	if err := s.BookRepo.SetProgress(userID, p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}
