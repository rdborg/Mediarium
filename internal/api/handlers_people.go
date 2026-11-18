package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/rdborg/mediarium/internal/metadata"
)

// personCredit is one title a person worked on, shaped like a Discover card
// plus what they did on it.
type personCredit struct {
	discoverPayload
	Roles     []string `json:"roles"`               // "acting", "directing", "writing" or "other"
	Character string   `json:"character,omitempty"` // the part they played, when they acted in it
}

type personPayload struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Biography    string `json:"biography,omitempty"`
	Birthday     string `json:"birthday,omitempty"`
	Deathday     string `json:"deathday,omitempty"`
	PlaceOfBirth string `json:"placeOfBirth,omitempty"`
	PhotoURL     string `json:"photoUrl,omitempty"`
	IMDBID       string `json:"imdbId,omitempty"`
	// KnownFor is TMDB's main department for them ("Acting", "Directing"...).
	KnownFor string `json:"knownFor,omitempty"`
	// WorkKinds is which of acting, directing and writing they have credits in;
	// the page offers a filter when there are two or more.
	WorkKinds []string       `json:"workKinds"`
	Movies    []personCredit `json:"movies"`
	Shows     []personCredit `json:"shows"`
}

func (s *Server) personCredits(r *http.Request, items []metadata.FilmographyItem, tv bool) []personCredit {
	out := make([]personCredit, 0, len(items))
	for _, it := range items {
		var dp discoverPayload
		if tv {
			dp = s.showDiscover(r.Context(), it.Show())
		} else {
			dp = s.movieDiscover(r.Context(), it.Movie())
		}
		out = append(out, personCredit{discoverPayload: dp, Roles: it.Roles, Character: it.Character})
	}
	return out
}

// handleTMDBPerson gives a person's page in one answer: who they are and every
// movie and show they are credited on, most popular first. The biography comes
// in the chosen info language, in English where TMDB has no translation.
func (s *Server) handleTMDBPerson(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("personId"))
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "That isn't a valid TMDB ID.")
		return
	}
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "Add your TMDB API key first (Settings > Library and files > Movie and show info).")
		return
	}
	p, err := s.displayTMDB().GetPerson(r.Context(), id)
	if err != nil {
		var se *metadata.StatusError
		if errors.As(err, &se) && se.Code == http.StatusNotFound {
			writeError(w, http.StatusNotFound, "TMDB has no person with that ID.")
			return
		}
		writeUpstreamError(w, "look up that person", err)
		return
	}
	movies, shows := p.Filmography()
	writeJSON(w, http.StatusOK, personPayload{
		ID: p.ID, Name: p.Name, Biography: p.Biography, Birthday: isoDate(p.Birthday), Deathday: isoDate(p.Deathday),
		PlaceOfBirth: p.PlaceOfBirth, PhotoURL: metadata.ProfileLargeURL(p.ProfilePath), IMDBID: p.IMDBID,
		KnownFor: p.KnownForDepartment, WorkKinds: metadata.WorkKinds(movies, shows),
		Movies: s.personCredits(r, movies, false), Shows: s.personCredits(r, shows, true),
	})
}
