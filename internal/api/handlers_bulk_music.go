package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/rdborg/mediarium/internal/plainerror"
)

// Bulk changes to artists, for the Music tab of the Library page. Like the
// ones for movies and shows they are for administrators, take a list of ids
// and answer with what was done and, for anything that was not, why.

type bulkArtistFollowRequest struct {
	IDs       []int64 `json:"ids"`
	Monitored *bool   `json:"monitored"`
}

// handleBulkArtistFollow follows or unfollows the chosen artists.
func (s *Server) handleBulkArtistFollow(w http.ResponseWriter, r *http.Request) {
	var req bulkArtistFollowRequest
	if err := decodeJSON(r, &req); err != nil || req.Monitored == nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	s.bulkArtists(w, req.IDs, req.Monitored, nil)
}

type bulkArtistProfileRequest struct {
	IDs       []int64 `json:"ids"`
	ProfileID *int64  `json:"profileId"` // 0 = use the default profile
}

// handleBulkArtistProfile gives the chosen artists a music quality profile.
func (s *Server) handleBulkArtistProfile(w http.ResponseWriter, r *http.Request) {
	var req bulkArtistProfileRequest
	if err := decodeJSON(r, &req); err != nil || req.ProfileID == nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if err := s.checkMusicProfileChoice(*req.ProfileID); err != nil {
		writeMusicProfileError(w, err)
		return
	}
	s.bulkArtists(w, req.IDs, nil, req.ProfileID)
}

// parseBulkIDs checks the ids of a bulk request and drops repeats.
func parseBulkIDs(ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, errors.New("Pick at least one artist first.")
	}
	if len(ids) > maxBulkItems {
		return nil, fmt.Errorf("That is too many artists at once. Change up to %d at a time.", maxBulkItems)
	}
	seen := map[int64]bool{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, errors.New("One of the chosen artists isn't valid. Reload the page and try again.")
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *Server) bulkArtists(w http.ResponseWriter, ids []int64, follow *bool, profileID *int64) {
	ids, err := parseBulkIDs(ids)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	missing, err := s.MusicRepo.BulkSetArtists(ids, follow, profileID)
	if err != nil {
		log.Printf("bulk change of artists: %v", err)
		writeError(w, http.StatusInternalServerError, "Nothing was changed because the database could not save it. Try again in a moment.")
		return
	}
	res := bulkResult{Updated: len(ids) - len(missing), Failed: []bulkFailure{}}
	for _, id := range missing {
		res.Failed = append(res.Failed, bulkFailure{Kind: "music", ID: id, Reason: reasonNotFound})
	}
	res.Message = bulkMessage(res.Updated, res.Failed, "artist")
	writeJSON(w, http.StatusOK, res)
}

type bulkArtistRemoveRequest struct {
	IDs []int64 `json:"ids"`
	// DeleteFiles also deletes the artists' album folders from the music
	// library. Left out, it is false: nothing on disk is touched.
	DeleteFiles bool `json:"deleteFiles"`
}

// handleBulkArtistRemove takes the chosen artists out of the library, one
// after the other, with the same rules as removing titles: downloads are
// cancelled either way and album folders are deleted only when the request
// says deleteFiles: true.
func (s *Server) handleBulkArtistRemove(w http.ResponseWriter, r *http.Request) {
	var req bulkArtistRemoveRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	ids, err := parseBulkIDs(req.IDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res := bulkResult{Failed: []bulkFailure{}}
	for _, id := range ids {
		name, err := s.removeArtist(id, req.DeleteFiles)
		if err == nil {
			res.Updated++
			continue
		}
		f := bulkFailure{Kind: "music", ID: id, Title: name, Reason: plainerror.Message(err)}
		var problem *removeProblem
		if errors.As(err, &problem) && problem.status == http.StatusNotFound {
			f.Reason = reasonNotFound
		}
		res.Failed = append(res.Failed, f)
	}
	res.Message = bulkRemoveMessage(res.Updated, res.Failed, req.DeleteFiles, "artist")
	writeJSON(w, http.StatusOK, res)
}
