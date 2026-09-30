package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/problems"
	"github.com/rdborg/mediarium/internal/trakt"
)

// writeUpstreamError answers 502 when a call to an outside data service
// failed (the movie, TV or music database, a list site). The reason goes to
// the log; the person gets a plain sentence that names no provider, because
// there is nothing they can do about it but wait. The one exception is a
// rejected key, which they can fix: that message names the service whose key
// field they have to open.
func writeUpstreamError(w http.ResponseWriter, doing string, err error) {
	switch {
	case errors.Is(err, metadata.ErrInvalidKey):
		problems.Record(problems.Problem{Code: problems.CodeTMDBKeyRejected, Message: "TMDB did not accept the API key."})
		writeError(w, http.StatusBadGateway, "TMDB rejected the API key. Check it in Settings.")
		return
	case errors.Is(err, trakt.ErrInvalidClientID):
		writeError(w, http.StatusBadGateway, "Trakt rejected the client ID. Check it in Settings.")
		return
	}
	slog.Warn("outside data service request failed", "while", doing, "err", err)
	writeError(w, http.StatusBadGateway, "Couldn't "+doing+" right now. Try again in a moment.")
}
