package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/rdborg/mediarium/internal/books"
)

// prepareNewBook fills in a book that is about to be added. Open Library still
// supplies the description and the cover id when it knows the book. A Hardcover
// id (from the search result, or from a key "hc-<id>") supplies the description
// when Open Library has none, and is returned so the caller can keep Hardcover's
// picture after the row exists. A Hardcover-only book is matched to Open Library
// first when the title and author are found there, so the library keeps one entry.
func (s *Server) prepareNewBook(ctx context.Context, b *books.Book, hcID int) int {
	if id, ok := books.ParseHardcoverKey(b.OLKey); ok {
		hcID = id
	}
	if hcID > 0 {
		if hc := s.hardcover(); hc != nil {
			if info, err := hc.Book(ctx, hcID); err == nil {
				if b.Description == "" {
					b.Description = info.Description
				}
				if b.Title == "" {
					b.Title = info.Title
				}
				if b.Author == "" {
					b.Author = info.Author
				}
				if b.Year == 0 {
					b.Year = info.Year
				}
			} else {
				slog.Info("books: Hardcover details unavailable", "id", hcID, "err", err)
			}
		}
	}
	if _, ok := books.ParseHardcoverKey(b.OLKey); ok && b.Title != "" {
		if ol, err := s.OpenLibrary.FindOnOpenLibrary(ctx, b.Title, b.Author); err == nil {
			b.OLKey = ol.Key
			if b.AuthorKey == "" {
				b.AuthorKey = ol.AuthorKey
			}
			if b.Year == 0 {
				b.Year = ol.Year
			}
			if b.CoverID == 0 {
				b.CoverID = ol.CoverID
			}
		}
	}
	if _, ok := books.ParseHardcoverKey(b.OLKey); !ok {
		if work, err := s.OpenLibrary.GetWork(ctx, b.OLKey); err == nil {
			if work.Description != "" {
				b.Description = work.Description
			}
			if b.CoverID == 0 {
				b.CoverID = work.CoverID
			}
		}
	}
	return hcID
}

// keepHardcoverCover downloads Hardcover's picture and stores it as the book's
// own cover. A failure leaves the book without a picture; adding it already succeeded.
func (s *Server) keepHardcoverCover(ctx context.Context, bookID int64, hcID int) {
	hc := s.hardcover()
	if hc == nil || hcID <= 0 {
		return
	}
	link, err := hc.ImageURL(ctx, hcID)
	if err != nil {
		slog.Info("books: Hardcover cover not saved", "book", bookID, "err", err)
		return
	}
	raw, err := s.fetchCoverLink(ctx, link)
	if err != nil {
		slog.Info("books: Hardcover cover not saved", "book", bookID, "err", err)
		return
	}
	jpeg, err := books.PrepareOwnCover(raw)
	if err != nil {
		slog.Info("books: Hardcover cover not saved", "book", bookID, "err", err)
		return
	}
	if err := s.BookRepo.SetOwnCover(bookID, jpeg, time.Now()); err != nil {
		slog.Info("books: Hardcover cover not saved", "book", bookID, "err", err)
	}
}
