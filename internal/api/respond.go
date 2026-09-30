package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rdborg/mediarium/internal/logbuf"
	"github.com/rdborg/mediarium/internal/plainerror"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil && !errors.Is(err, http.ErrHandlerTimeout) { // a late reply to a page that was already told the app is busy
		log.Printf("api: encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	if status >= http.StatusInternalServerError {
		// Most server errors arrive here as a raw err.Error(). The page shows
		// this text, so raw network and database wording is swapped for a
		// sentence; the original goes to the log (scrubbed of keys).
		if plain, changed := plainerror.ForResponse(message); changed {
			log.Printf("api: server error shown as %q: %s", plain, logbuf.Scrub(message))
			message = plain
		}
	}
	writeJSON(w, status, map[string]string{"error": message})
}

// maxJSONBody is the largest JSON request body any endpoint accepts. Real
// requests are a few KB; the cap stops one huge body from eating memory.
const maxJSONBody = 8 << 20

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxJSONBody)).Decode(v)
}

// problemText turns a wrapped error such as "invalid quality profile: name
// must be 1-64 characters" into the sentence a person should read: the part
// after the sentinel's own text, starting with a capital and ending with a
// full stop.
func problemText(err, sentinel error) string {
	text := strings.TrimPrefix(err.Error(), sentinel.Error()+": ")
	if text == "" {
		return err.Error()
	}
	r, size := utf8.DecodeRuneInString(text)
	text = string(unicode.ToUpper(r)) + text[size:]
	if !strings.HasSuffix(text, ".") {
		text += "."
	}
	return text
}
