package api

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/rdborg/mediarium/internal/settings"
)

// "Switch this on?" offers (V4 A7). After an update, the What's new window
// offers an administrator each new thing that is off: switch it on, not now
// (asked again after the next update) or never. The list of offers lives in
// the web app (featureOffers.ts); the server only remembers the answers, so
// every browser an administrator uses agrees.

var offerID = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

// maxOfferChoices bounds the stored answers.
const maxOfferChoices = 200

func (s *Server) featureOfferChoices() map[string]string {
	v, _ := s.Settings.Get(settings.KeyFeatureOffers)
	out := map[string]string{}
	if v != "" {
		_ = json.Unmarshal([]byte(v), &out)
	}
	return out
}

// handleGetFeatureOffers answers what was chosen for each offer: "never", or
// "later:<version>" for not now.
func (s *Server) handleGetFeatureOffers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"choices": s.featureOfferChoices()})
}

// handlePutFeatureOffer remembers the answer to one offer. Body: {"id":
// "anime", "choice": "never" | "later" | "clear", "version": "4.0.0"}.
func (s *Server) handlePutFeatureOffer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		Choice  string `json:"choice"`
		Version string `json:"version"`
	}
	if err := decodeJSON(r, &req); err != nil || !offerID.MatchString(req.ID) || len(req.Version) > 40 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	choices := s.featureOfferChoices()
	switch req.Choice {
	case "never":
		choices[req.ID] = "never"
	case "later":
		choices[req.ID] = "later:" + req.Version
	case "clear":
		delete(choices, req.ID)
	default:
		writeError(w, http.StatusBadRequest, `choice must be "never", "later" or "clear"`)
		return
	}
	if len(choices) > maxOfferChoices {
		writeError(w, http.StatusBadRequest, "Too many answers are stored.")
		return
	}
	b, _ := json.Marshal(choices)
	if err := s.Settings.Set(settings.KeyFeatureOffers, string(b), false); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"choices": choices})
}
