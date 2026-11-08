package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/rdborg/mediarium/internal/librarysearch"
)

// suggestTimeout is the longest a type-ahead search may take. A search that
// is slower than this is not worth waiting for: the next key press asks again.
const suggestTimeout = 3 * time.Second

// handleLibrarySuggest searches the library as a person types (GET
// /api/library/suggest?q=star+w&limit=8): the movies, shows, books, artists and
// albums whose name has a word that starts with each word typed.
func (s *Server) handleLibrarySuggest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if len(q) > 200 {
		q = q[:200]
	}
	limit := 8
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = min(n, librarysearch.MaxLimit)
	}
	ctx, cancel := context.WithTimeout(r.Context(), suggestTimeout)
	defer cancel()
	hits, err := librarysearch.Search(ctx, s.db, q, limit)
	if err != nil {
		slog.Warn("library search failed", "err", err)
		writeError(w, http.StatusInternalServerError, "The library search did not work. Try again in a moment.")
		return
	}
	if hits == nil {
		hits = []librarysearch.Hit{}
	}
	writeJSON(w, http.StatusOK, hits)
}
