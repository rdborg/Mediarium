package api

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

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
		return "", fmt.Errorf("the %s folder isn't set. Choose one in Settings > Library > Folders and file names", formatWord(f))
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
		plain := plainerror.Message(stepErr)
		_ = s.QueueRepo.SetStatus(queueID, queue.StatusFailed, plain)
		noteDownloadFailure(queueID, label, bookLink(b.ID), stepErr)
		_ = s.BookRepo.SetState(b.ID, f, restore, "", "")
		_ = s.QueueRepo.LogActivity(0, "failed", fmt.Sprintf("%s (%s): %s", label, f, plain))
		s.notifyItem("failed", notify.Item{Media: string(f), Title: b.Title, Year: b.Year, Reason: plain, Release: releaseTitle, LinkPath: bookLink(b.ID)})
		if isBadRelease(stepErr) {
			if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: releaseTitle, Protocol: string(protocol), Reason: stepErr.Error()}); err == nil {
				s.background(func() { s.searchBook(b.ID, f, true) })
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
		src, format := books.EbookFile(dir)
		if src == "" {
			return fail(badRelease(errors.New("the download has no ebook file (epub, azw3, mobi or pdf)")))
		}
		mode, repl := s.illegalCharSettings()
		dest := filepath.Join(folder, organizer.Sanitize(b.Title, mode, repl)+"."+format)
		res, err := organizer.Import(src, dest, organizer.ConflictOverwriteIfBetter, func() bool {
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
		files, format := books.AudioFiles(dir)
		if len(files) == 0 {
			return fail(badRelease(errors.New("the download has no audiobook files (m4b, mp3 and the like)")))
		}
		for _, src := range files {
			res, err := organizer.Import(src, filepath.Join(folder, filepath.Base(src)), organizer.ConflictOverwrite, nil)
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
