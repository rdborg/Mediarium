package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/ebookconv"
)

// Kindle books (MOBI, AZW, AZW3) open in the reader as EPUB: Mediarium makes
// an EPUB copy the first time, one book at a time, and keeps it in its own
// cache folder (/config/cache/epub). The library itself is never changed, so
// a media server or e-reader that reads the folder sees no extra files.

// convertMu lets one conversion run at a time, so a shelf of Kindle books
// can't keep the processor busy.
var convertMu sync.Mutex

// convertedMaxAge is how long a copy nobody opens is kept.
const convertedMaxAge = 90 * 24 * time.Hour

// kindleFormat reports whether a file format is one the converter reads.
func kindleFormat(ext string) bool {
	switch strings.TrimPrefix(strings.ToLower(ext), ".") {
	case "mobi", "azw", "azw3", "prc":
		return true
	}
	return false
}

func (s *Server) convertedDir() string { return filepath.Join(s.cfg.ConfigDir, "cache", "epub") }

// convertedEPUB returns the EPUB copy of a Kindle book file, making it when
// there isn't one for this version of the file yet.
func (s *Server) convertedEPUB(full string, info os.FileInfo) (string, error) {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d", full, info.Size(), info.ModTime().UnixNano())))
	out := filepath.Join(s.convertedDir(), hex.EncodeToString(sum[:12])+".epub")
	if _, err := os.Stat(out); err == nil {
		now := time.Now()
		_ = os.Chtimes(out, now, now) // still in use
		return out, nil
	}
	convertMu.Lock()
	defer convertMu.Unlock()
	if _, err := os.Stat(out); err == nil {
		return out, nil // made while this call waited
	}
	if info.Size() > ebookconv.MaxInput {
		return "", ebookconv.ErrUnsupported
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("read the book: %w", err)
	}
	if err := os.MkdirAll(s.convertedDir(), 0o755); err != nil {
		return "", fmt.Errorf("make the cache folder: %w", err)
	}
	tmp, err := os.CreateTemp(s.convertedDir(), "converting-*.epub")
	if err != nil {
		return "", fmt.Errorf("make the EPUB: %w", err)
	}
	started := time.Now()
	_, cerr := ebookconv.Convert(data, tmp)
	if err := tmp.Close(); err != nil && cerr == nil {
		cerr = err
	}
	if cerr != nil {
		_ = os.Remove(tmp.Name())
		return "", cerr
	}
	if err := os.Rename(tmp.Name(), out); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("save the EPUB: %w", err)
	}
	slog.Info("books: made an EPUB copy for the reader", "file", filepath.Base(full), "took", time.Since(started).Round(time.Millisecond))
	s.pruneConverted()
	return out, nil
}

// pruneConverted removes copies nobody has opened for a long time.
func (s *Server) pruneConverted() {
	entries, err := os.ReadDir(s.convertedDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || e.IsDir() {
			continue
		}
		if time.Since(info.ModTime()) > convertedMaxAge || (strings.HasPrefix(e.Name(), "converting-") && time.Since(info.ModTime()) > time.Hour) {
			_ = os.Remove(filepath.Join(s.convertedDir(), e.Name()))
		}
	}
}

// convertLater makes the EPUB copy of a freshly imported Kindle book in the
// background, so it opens at once the first time.
func (s *Server) convertLater(b books.Book) {
	if b.EbookPath == "" || !kindleFormat(filepath.Ext(b.EbookPath)) {
		return
	}
	s.background(func() {
		info, err := os.Stat(b.EbookPath)
		if err != nil {
			return
		}
		if _, err := s.convertedEPUB(b.EbookPath, info); err != nil {
			slog.Info("books: no EPUB copy", "book", b.ID, "err", err)
		}
	})
}

// convertError is the message for a book that couldn't be converted.
func convertError(err error) string {
	switch {
	case errors.Is(err, ebookconv.ErrDRM):
		return "This book is locked with DRM, so it can't be opened here. Download it for the Kindle it was bought for."
	case errors.Is(err, ebookconv.ErrUnsupported):
		return "This Kindle book is stored in a way Mediarium can't read yet. Download it to read it on a Kindle or in Calibre."
	default:
		return "This Kindle book couldn't be turned into a book the reader can open. Download it to read it elsewhere."
	}
}

// serveConverted answers the reader's request for a Kindle book as EPUB.
func (s *Server) serveConverted(w http.ResponseWriter, r *http.Request, full string, info os.FileInfo, name string) {
	path, err := s.convertedEPUB(full, info)
	if err != nil {
		slog.Info("books: convert for the reader", "file", filepath.Base(full), "err", err)
		writeError(w, http.StatusUnprocessableEntity, convertError(err))
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeFileReadError(w, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		writeFileReadError(w, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/epub+zip")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": strings.TrimSuffix(name, filepath.Ext(name)) + ".epub"}))
	http.ServeContent(w, r, name, st.ModTime(), io.ReadSeeker(f))
}
