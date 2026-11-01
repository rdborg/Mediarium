package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/books"
)

// Book covers come from Open Library, which answers every cover with a redirect
// to an archive.org server and takes one to two seconds for each, again on every
// visit. A Library or Discover page of books therefore filled in one cover at a
// time. Now each cover is fetched once, kept in the config folder and served
// from there with a long browser cache; the pages ask the server for the next
// screenful of covers in the background so they are usually ready.

// bookCoverFile is the file part of a cover address: "8231856-M.jpg".
var bookCoverFile = regexp.MustCompile(`^(\d{1,9})-([SML])\.jpg$`)

// bookCoverMaxAge is how long a browser keeps a book cover.
const bookCoverMaxAge = 30 * 24 * time.Hour

var bookCoverLocks coverLocks // one download per cover at a time

func (s *Server) bookCoverDir() string { return filepath.Join(s.cfg.ConfigDir, "book-covers") }

// cachedBookCover returns the cached file for a cover, fetching it first when
// it is not there. books.ErrNotFound means Open Library has none (remembered for
// a day); any other error means it could not be reached (not remembered).
func (s *Server) cachedBookCover(ctx context.Context, id int, size string) (string, error) {
	dir := s.bookCoverDir()
	key := fmt.Sprintf("%d-%s", id, size)
	img, none := filepath.Join(dir, key+".img"), filepath.Join(dir, key+".none")
	if st, err := os.Stat(img); err == nil && st.Size() > 0 {
		touchCover(img, st)
		return img, nil
	}
	unlock := bookCoverLocks.lock(key)
	defer unlock()
	if st, err := os.Stat(img); err == nil && st.Size() > 0 { // fetched while we waited
		return img, nil
	}
	if st, err := os.Stat(none); err == nil && time.Since(st.ModTime()) < coverMissingFor {
		return "", books.ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, coverFetchTimeout)
	defer cancel()
	// Only a few downloads from Open Library at once, however many covers a
	// page (or a client asking for made-up ids) wants.
	if err := acquireCoverWork(ctx); err != nil {
		return "", err
	}
	defer releaseCoverWork()
	data, err := s.OpenLibrary.FetchCover(ctx, id, size)
	if errors.Is(err, books.ErrNotFound) {
		if os.MkdirAll(dir, 0o755) == nil {
			_ = os.WriteFile(none, nil, 0o644)
			bookCoverLocks.noteWrite(dir)
		}
		return "", err
	}
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create book cover cache: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "cover-*.tmp")
	if err != nil {
		return "", fmt.Errorf("cache book cover: %w", err)
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("cache book cover: %w", errors.Join(werr, cerr))
	}
	if err := os.Rename(tmp.Name(), img); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("cache book cover: %w", err)
	}
	_ = os.Remove(none)
	bookCoverLocks.noteWrite(dir)
	return img, nil
}

// handleBookCover serves a book cover from the local copy.
func (s *Server) handleBookCover(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	var path string
	var err error
	if id, ok := books.ParseHardcoverCover(file); ok {
		path, err = s.cachedHardcoverCover(r.Context(), id)
	} else if m := bookCoverFile.FindStringSubmatch(file); m != nil {
		id, _ := strconv.Atoi(m[1])
		path, err = s.cachedBookCover(r.Context(), id, m[2])
	} else {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		if !errors.Is(err, books.ErrNotFound) {
			slog.Info("books: cover unavailable", "cover", file, "err", err)
		}
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	head := make([]byte, 512)
	n, _ := f.Read(head)
	ct := http.DetectContentType(head[:n])
	if ct != "image/jpeg" && ct != "image/png" {
		http.NotFound(w, r) // never send anything but a picture
		return
	}
	if _, err := f.Seek(0, 0); err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Cache-Control", fmt.Sprintf("private, max-age=%d, immutable", int(bookCoverMaxAge.Seconds())))
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

var warmingBookCovers sync.Map // "id-size" -> struct{}, covers already being fetched

// warmBookCovers fetches covers in the background so the page that is about to
// ask for them finds them ready. It runs a few downloads at a time and skips
// what is cached or already on its way.
func (s *Server) warmBookCovers(size string, ids []int) {
	var todo []int
	seen := map[int]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		key := fmt.Sprintf("%d-%s", id, size)
		if st, err := os.Stat(filepath.Join(s.bookCoverDir(), key+".img")); err == nil && st.Size() > 0 {
			continue
		}
		if _, busy := warmingBookCovers.LoadOrStore(key, struct{}{}); busy {
			continue
		}
		todo = append(todo, id)
	}
	if len(todo) == 0 {
		return
	}
	go func() {
		sem := make(chan struct{}, 6)
		var wg sync.WaitGroup
		for _, id := range todo {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				defer warmingBookCovers.Delete(fmt.Sprintf("%d-%s", id, size))
				_, _ = s.cachedBookCover(context.Background(), id, size)
			}()
		}
		wg.Wait()
	}()
}

// A Hardcover cover id and its image address, remembered from a search or a
// book page. The cover route reads this and does not ask Hardcover again.
type hardcoverCoverNote struct {
	url   string
	until time.Time
}

var hardcoverCoverMem = struct {
	sync.Mutex
	seen map[int]hardcoverCoverNote
	fail map[int]time.Time
}{}

const (
	// hardcoverCoverRemember is how long an id from a search or a book page
	// may be fetched. After that, only a copy already on disk is served.
	hardcoverCoverRemember = 30 * time.Minute
	// hardcoverCoverFailFor is how long a failed download (a rate limit, a
	// network error) is remembered, so the next request does not try again
	// straight away. A missing picture is remembered for longer, on disk.
	hardcoverCoverFailFor = 10 * time.Minute
)

// rememberHardcoverCover notes an id this server just saw, with the image
// address Hardcover already returned ("" when that book has no picture).
func (s *Server) rememberHardcoverCover(id int, url string) {
	if id <= 0 {
		return
	}
	hardcoverCoverMem.Lock()
	defer hardcoverCoverMem.Unlock()
	if hardcoverCoverMem.seen == nil || len(hardcoverCoverMem.seen) > 2000 {
		hardcoverCoverMem.seen = map[int]hardcoverCoverNote{}
	}
	hardcoverCoverMem.seen[id] = hardcoverCoverNote{url: url, until: time.Now().Add(hardcoverCoverRemember)}
	delete(hardcoverCoverMem.fail, id)
}

func hardcoverCoverKnown(id int) (string, bool) {
	hardcoverCoverMem.Lock()
	defer hardcoverCoverMem.Unlock()
	n, ok := hardcoverCoverMem.seen[id]
	if !ok || time.Now().After(n.until) {
		return "", false
	}
	return n.url, true
}

func hardcoverCoverFailing(id int) bool {
	hardcoverCoverMem.Lock()
	defer hardcoverCoverMem.Unlock()
	until, ok := hardcoverCoverMem.fail[id]
	return ok && time.Now().Before(until)
}

func rememberHardcoverCoverFail(id int) {
	hardcoverCoverMem.Lock()
	defer hardcoverCoverMem.Unlock()
	if hardcoverCoverMem.fail == nil || len(hardcoverCoverMem.fail) > 2000 {
		hardcoverCoverMem.fail = map[int]time.Time{}
	}
	hardcoverCoverMem.fail[id] = time.Now().Add(hardcoverCoverFailFor)
}

// cachedHardcoverCover returns the cached file for a Hardcover cover. The
// image address comes from a recent search or book page; an id nobody saw is
// refused without calling Hardcover. A missing picture is remembered for a
// day. Any other failure is remembered for a short while.
func (s *Server) cachedHardcoverCover(ctx context.Context, id int) (string, error) {
	if id <= 0 {
		return "", books.ErrNotFound
	}
	dir := s.bookCoverDir()
	key := fmt.Sprintf("hc-%d", id)
	img, none := filepath.Join(dir, key+".img"), filepath.Join(dir, key+".none")
	if st, err := os.Stat(img); err == nil && st.Size() > 0 {
		touchCover(img, st)
		return img, nil
	}
	unlock := bookCoverLocks.lock(key)
	defer unlock()
	if st, err := os.Stat(img); err == nil && st.Size() > 0 {
		return img, nil
	}
	if hardcoverCoverFailing(id) {
		return "", errors.New("hardcover cover failed a moment ago")
	}
	if st, err := os.Stat(none); err == nil && time.Since(st.ModTime()) < coverMissingFor {
		return "", books.ErrNotFound
	}
	link, seen := hardcoverCoverKnown(id)
	if !seen || link == "" {
		if seen && os.MkdirAll(dir, 0o755) == nil {
			_ = os.WriteFile(none, nil, 0o644)
			bookCoverLocks.noteWrite(dir)
		}
		return "", books.ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, coverFetchTimeout)
	defer cancel()
	data, err := s.fetchCoverLink(ctx, link)
	if err != nil {
		rememberHardcoverCoverFail(id)
		return "", err
	}
	data, err = books.NormalizeCover(data)
	if err != nil {
		rememberHardcoverCoverFail(id)
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create book cover cache: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "cover-*.tmp")
	if err != nil {
		return "", fmt.Errorf("cache book cover: %w", err)
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("cache book cover: %w", errors.Join(werr, cerr))
	}
	if err := os.Rename(tmp.Name(), img); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("cache book cover: %w", err)
	}
	_ = os.Remove(none)
	bookCoverLocks.noteWrite(dir)
	return img, nil
}

// warmHardcoverCovers fetches Hardcover covers in the background from the
// image address a search or book page already stored. It does not ask
// Hardcover for that address again.
func (s *Server) warmHardcoverCovers(ids []int) {
	var todo []int
	seen := map[int]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		key := fmt.Sprintf("hc-%d", id)
		if st, err := os.Stat(filepath.Join(s.bookCoverDir(), key+".img")); err == nil && st.Size() > 0 {
			continue
		}
		if _, busy := warmingBookCovers.LoadOrStore(key, struct{}{}); busy {
			continue
		}
		todo = append(todo, id)
	}
	if len(todo) == 0 {
		return
	}
	go func() {
		sem := make(chan struct{}, 6)
		var wg sync.WaitGroup
		for _, id := range todo {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				defer warmingBookCovers.Delete(fmt.Sprintf("hc-%d", id))
				_, _ = s.cachedHardcoverCover(context.Background(), id)
			}()
		}
		wg.Wait()
	}()
}
