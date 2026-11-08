package api

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/queue"
)

// A book download: the same download, repair and unpack as everything else,
// then the book's file (an ebook) or files (an audiobook) are put in
// "<root>/<Author>/<Title (Year)>/". An audiobook keeps its files' own names
// so the chapters stay in order.

func bookLink(id int64) string { return fmt.Sprintf("/book/%d", id) }

// bookRetryBudget counts the automatic retries after a bad release, per book
// and format, over the last retryWindow. Book blocklist entries carry no book,
// so the count is kept here (a restart starts it again).
type bookRetryBudget struct {
	mu sync.Mutex
	at map[string][]time.Time
}

// bookRetries holds each server's budget (tests run several servers in one
// process).
var bookRetries sync.Map // *Server -> *bookRetryBudget

func bookRetriesOf(s *Server) *bookRetryBudget {
	b, _ := bookRetries.LoadOrStore(s, &bookRetryBudget{at: map[string][]time.Time{}})
	return b.(*bookRetryBudget)
}

// allow records one more retry for a book's format and reports whether it may
// go ahead; n is how many bad releases it had in the window.
func (b *bookRetryBudget) allow(bookID int64, f books.Format, now time.Time) (n int, ok bool) {
	key := fmt.Sprintf("%d|%s", bookID, f)
	b.mu.Lock()
	defer b.mu.Unlock()
	recent := b.at[key][:0]
	for _, t := range b.at[key] {
		if now.Sub(t) < retryWindow {
			recent = append(recent, t)
		}
	}
	recent = append(recent, now)
	b.at[key] = recent
	return len(recent), len(recent) <= maxAutoRetries
}

// bookRoot is the library folder for a format.
func (s *Server) bookRoot(f books.Format) string {
	if f == books.Audiobook {
		return s.audiobooksRoot()
	}
	return s.ebooksRoot()
}

// bookFolder is where a book's files go: "<root>/<Author>/<Title (Year)>".
func (s *Server) bookFolder(b books.Book, f books.Format) (string, error) {
	root := s.bookRoot(f)
	if root == "" {
		return "", fmt.Errorf("the %s folder isn't set. Choose one in Settings > Library > Folders", formatWord(f))
	}
	mode, repl := s.illegalCharSettings()
	author := strings.TrimSpace(b.Author)
	if author == "" {
		author = "Unknown author"
	}
	return filepath.Join(root, organizer.Sanitize(author, mode, repl), organizer.Sanitize(books.FolderName(b), mode, repl)), nil
}

func formatWord(f books.Format) string {
	if f == books.Audiobook {
		return "audiobooks"
	}
	return "ebooks"
}

// audiobookFileNames is the name each audiobook file gets in the book's one
// folder. A file keeps its own name, unless another file has the same name in
// a different folder of the download (CD1/Track 01.mp3 and CD2/Track 01.mp3):
// then its folder is put in front ("CD1 - Track 01.mp3"), so no part of the
// book replaces another. Any name that is still taken gets a number.
func audiobookFileNames(dir string, files []string) []string {
	rels := make([][]string, len(files))
	for i, f := range files {
		rel, err := filepath.Rel(dir, filepath.Dir(f))
		if err != nil || rel == "." {
			continue
		}
		rels[i] = strings.Split(filepath.ToSlash(rel), "/")
	}
	// Folders every file shares (the release's own folder) say nothing.
	for len(rels) > 0 {
		if len(rels[0]) == 0 {
			break
		}
		first, shared := rels[0][0], true
		for _, r := range rels {
			if len(r) == 0 || r[0] != first {
				shared = false
				break
			}
		}
		if !shared {
			break
		}
		for i := range rels {
			rels[i] = rels[i][1:]
		}
	}
	count := map[string]int{}
	for _, f := range files {
		count[strings.ToLower(filepath.Base(f))]++
	}
	names := make([]string, len(files))
	used := map[string]bool{}
	for i, f := range files {
		base := filepath.Base(f)
		name := base
		if count[strings.ToLower(base)] > 1 && len(rels[i]) > 0 {
			name = strings.Join(rels[i], " - ") + " - " + base
		}
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		for n := 2; used[strings.ToLower(name)]; n++ {
			name = fmt.Sprintf("%s (%d)%s", stem, n, ext)
		}
		used[strings.ToLower(name)] = true
		names[i] = name
	}
	return names
}

func (s *Server) runBookPipeline(queueID int64, b books.Book, f books.Format, releaseTitle, downloadURL string, protocol indexers.Protocol) error {
	ctx, run := s.pipelines.begin(queueID)
	defer s.pipelines.end(queueID, run)
	if s.pausedBeforeStart(queueID) {
		return errPaused
	}
	restore := books.StatusMissing
	if b.Path(f) != "" {
		restore = books.StatusDownloaded
	}
	label := b.Name()
	fail := func(stepErr error) error {
		if run.cancelled.Load() {
			_ = s.QueueRepo.SetStatus(queueID, queue.StatusFailed, errRemovedFromLibrary.Error())
			return errRemovedFromLibrary
		}
		if handled, err := s.settleInterrupted(queueID, run); handled {
			return err
		}
		if dup := asDuplicate(stepErr); dup != nil {
			s.settleDuplicate(queueID, dup)
			if !dup.holdsTitle() {
				_ = s.BookRepo.SetState(b.ID, f, restore, "", "")
			}
			return stepErr
		}
		plain := plainerror.Message(stepErr)
		_ = s.QueueRepo.SetStatus(queueID, queue.StatusFailed, plain)
		noteDownloadFailure(queueID, label, bookLink(b.ID), stepErr)
		s.doctorNoteFailure(queueID, stepErr)
		_ = s.BookRepo.SetState(b.ID, f, restore, "", "")
		_ = s.QueueRepo.LogActivity(0, "failed", fmt.Sprintf("%s (%s): %s", label, f, plain))
		if s.failureNotifyWanted(stepErr, downloadURL, releaseTitle) {
			s.notifyItem("failed", notify.Item{Media: string(f), Title: b.Title, Year: b.Year, Reason: plain, Release: releaseTitle, LinkPath: bookLink(b.ID)})
		}
		if isBadRelease(stepErr) {
			if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: releaseTitle, Protocol: string(protocol), Reason: stepErr.Error()}); err == nil {
				// The same daily budget as movies and shows: an indexer that
				// keeps handing out bad releases can't empty a book's list.
				if n, ok := bookRetriesOf(s).allow(b.ID, f, time.Now()); ok {
					s.background(func() { s.searchBook(b.ID, f, true) })
				} else {
					_ = s.QueueRepo.LogActivity(0, "failed", fmt.Sprintf("%s (%s): %s", label, f, retryPausedMessage(n)))
				}
			}
		}
		return stepErr
	}

	_ = s.BookRepo.SetState(b.ID, f, books.StatusDownloading, "", "")
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusDownloading, ""); err != nil {
		return fail(err)
	}
	dir, err := s.prepareDownload(ctx, queueID, downloadURL, protocol)
	if err != nil {
		return fail(err)
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	folder, err := s.bookFolder(b, f)
	if err != nil {
		return fail(err)
	}
	var placed []string
	fileFormat := ""
	if f == books.Ebook {
		src, format := books.EbookFile(dir, bookFindOptions(dir)...)
		if src == "" {
			return fail(badRelease(errors.New("the download has no ebook file (epub, azw3, mobi or pdf)")))
		}
		mode, repl := s.illegalCharSettings()
		dest := filepath.Join(folder, organizer.Sanitize(b.Title, mode, repl)+"."+format)
		res, err := s.importDownloaded(src, dest, organizer.ConflictOverwriteIfBetter, func() bool {
			return books.EbookRank(format) >= books.EbookRank(b.EbookFormat)
		})
		if err != nil {
			return fail(fmt.Errorf("couldn't move the ebook into your library: %w", err))
		}
		if res.Skipped {
			return fail(fmt.Errorf("a file is already at %s", dest))
		}
		placed, fileFormat = []string{res.DestPath}, format
	} else {
		files, format := books.AudioFiles(dir, bookFindOptions(dir)...)
		if len(files) == 0 {
			return fail(badRelease(errors.New("the download has no audiobook files (m4b, mp3 and the like)")))
		}
		names := audiobookFileNames(dir, files)
		for i, src := range files {
			res, err := s.importDownloaded(src, filepath.Join(folder, names[i]), organizer.ConflictOverwrite, nil)
			if err != nil {
				return fail(fmt.Errorf("couldn't move %s into your library: %w", filepath.Base(src), err))
			}
			placed = append(placed, res.DestPath)
		}
		fileFormat = format
	}

	path := placed[0]
	if f == books.Audiobook {
		path = folder
	}
	if err := s.BookRepo.SetState(b.ID, f, books.StatusDownloaded, fileFormat, path); err != nil {
		return fail(err)
	}
	if f == books.Ebook {
		withFile := b
		withFile.EbookPath = path
		s.convertLater(withFile)
	}
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, ""); err != nil {
		return fail(err)
	}
	s.cleanupWorkDir(dir, protocol)
	// Earlier failed tries for this book in this format are settled now.
	_, _ = s.QueueRepo.ClearFailedForBook(b.ID, string(f), queueID)
	s.booksImported(placed...)
	_ = s.QueueRepo.LogActivity(0, "imported", fmt.Sprintf("%s (%s, %s) imported to %s", label, f, fileFormat, path))
	s.notifyItem("imported", notify.Item{Media: string(f), Title: b.Title, Year: b.Year, Quality: strings.ToUpper(fileFormat), SizeBytes: fileSize(placed...), Path: path, Release: releaseTitle, LinkPath: bookLink(b.ID)})
	slog.Info("books: imported", "book", b.ID, "format", string(f), "files", len(placed))
	return nil
}
