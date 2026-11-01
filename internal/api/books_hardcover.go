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
// when Open Library has none. The image address is returned with the id so the
// caller can cache that picture without asking Hardcover again. A Hardcover-only
// book is matched to Open Library first when the title and author are found
// there, so the library keeps one entry.
func (s *Server) prepareNewBook(ctx context.Context, b *books.Book, hcID int) (int, string) {
	var cover string
	if id, ok := books.ParseHardcoverKey(b.OLKey); ok {
		hcID = id
	}
	if hcID > 0 {
		if hc := s.hardcover(); hc != nil {
			if info, err := hc.Book(ctx, hcID); err == nil {
				cover = info.CoverImage
				s.rememberHardcoverCover(hcID, info.CoverImage)
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
	return hcID, cover
}

// keepHardcoverCover keeps Hardcover's picture for a book that has no cover yet.
// link is the image address already returned with the book, so this does not
// ask Hardcover again. A book filed under an Open Library key gets that picture
// as its own cover, because the row does not keep the Hardcover id. A book
// filed under hc-<id> keeps it in the cover cache, which its page already
// points at. A book that already has a cover is left as it is. A failure leaves
// the book without a picture; adding it already succeeded.
func (s *Server) keepHardcoverCover(ctx context.Context, b books.Book, hcID int, link string) {
	if hcID <= 0 || link == "" || b.CoverID != 0 || b.OwnCover != 0 {
		return
	}
	s.rememberHardcoverCover(hcID, link)
	if _, ok := books.ParseHardcoverKey(b.OLKey); ok {
		if _, err := s.cachedHardcoverCover(ctx, hcID); err != nil {
			slog.Info("books: Hardcover cover not saved", "hardcover", hcID, "err", err)
		}
		return
	}
	raw, err := s.fetchCoverLink(ctx, link)
	if err != nil {
		slog.Info("books: Hardcover cover not saved", "book", b.ID, "err", err)
		return
	}
	jpeg, err := books.PrepareOwnCover(raw)
	if err != nil {
		slog.Info("books: Hardcover cover not saved", "book", b.ID, "err", err)
		return
	}
	if err := s.BookRepo.SetOwnCover(b.ID, jpeg, time.Now()); err != nil {
		slog.Info("books: Hardcover cover not saved", "book", b.ID, "err", err)
	}
}
