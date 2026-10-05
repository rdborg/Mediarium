package api

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/mediaservers"
)

// Tags on movies and shows ("Kids", "4K"): set on a title or on a selection,
// listed for the Library's filter, and passed on to Plex, Jellyfin and Emby as
// collections named after the tag.

// tagSyncDelay is how long after an import the tags are passed on: the media
// server needs to have scanned the new file first.
const tagSyncDelay = 3 * time.Minute

func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	list, err := s.MovieRepo.Tags()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type setTagsRequest struct {
	Tags []string `json:"tags"`
}

// PUT /api/movies/{id}/tags and /api/series/{id}/tags: replace a title's tags.
func (s *Server) handleSetMovieTags(w http.ResponseWriter, r *http.Request) {
	s.setTitleTags(w, r, library.TagMovie)
}

func (s *Server) handleSetSeriesTags(w http.ResponseWriter, r *http.Request) {
	s.setTitleTags(w, r, library.TagSeries)
}

func (s *Server) setTitleTags(w http.ResponseWriter, r *http.Request, k library.TitleKind) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid title.")
		return
	}
	var req setTagsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Send the tags as a list.")
		return
	}
	tmdbID, err := s.titleTMDBID(k, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That title isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	added, removed, err := s.MovieRepo.SetTitleTags(k, id, req.Tags)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.pushTags(k, tmdbID, added, removed)
	tags, _ := s.MovieRepo.TitleTags(k, id)
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

func (s *Server) titleTMDBID(k library.TitleKind, id int64) (int, error) {
	if k == library.TagSeries {
		sr, err := s.MovieRepo.GetSeries(id)
		return sr.TMDBID, err
	}
	m, err := s.MovieRepo.Get(id)
	return m.TMDBID, err
}

type bulkTagsRequest struct {
	Items  []bulkItem `json:"items"`
	Add    []string   `json:"add"`
	Remove []string   `json:"remove"`
}

// handleBulkTags adds and removes tags on the chosen titles (administrators).
func (s *Server) handleBulkTags(w http.ResponseWriter, r *http.Request) {
	var req bulkTagsRequest
	if err := decodeJSON(r, &req); err != nil || len(req.Add)+len(req.Remove) == 0 {
		writeError(w, http.StatusBadRequest, "Choose a tag to add or remove.")
		return
	}
	refs, err := parseBulkItems(req.Items)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res := bulkResult{Failed: []bulkFailure{}}
	for _, ref := range refs {
		k := library.TagMovie
		if ref.Kind == library.KindShow {
			k = library.TagSeries
		}
		tmdbID, err := s.titleTMDBID(k, ref.ID)
		if err != nil {
			res.Failed = append(res.Failed, bulkFailure{Kind: ref.Kind, ID: ref.ID, Reason: reasonNotFound})
			continue
		}
		added, removed, err := s.MovieRepo.ChangeTitleTags(k, ref.ID, req.Add, req.Remove)
		if err != nil {
			res.Failed = append(res.Failed, bulkFailure{Kind: ref.Kind, ID: ref.ID, Reason: err.Error()})
			continue
		}
		res.Updated++
		s.pushTags(k, tmdbID, added, removed)
	}
	res.Message = bulkMessage(res.Updated, res.Failed, "title")
	writeJSON(w, http.StatusOK, res)
}

// pushTags passes tag changes on to the enabled Plex, Jellyfin and Emby
// servers, in the background. A server that doesn't have the title yet gets
// its tags after the next import.
func (s *Server) pushTags(k library.TitleKind, tmdbID int, added, removed []string) {
	if (len(added) == 0 && len(removed) == 0) || tmdbID <= 0 || s.MediaServers == nil || s.mediaClient == nil {
		return
	}
	kind := mediaservers.MediaMovie
	if k == library.TagSeries {
		kind = mediaservers.MediaTV
	}
	servers, err := s.MediaServers.List()
	if err != nil {
		return
	}
	for _, m := range servers {
		if !m.Enabled || m.Kind.BookServer() {
			continue
		}
		m := m
		s.background(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			if _, err := s.mediaClient.SyncTags(ctx, m, kind, tmdbID, added, removed); err != nil {
				slog.Warn("media server: pass on tags", "server", m.Name, "tmdbId", tmdbID, "err", err)
			}
		})
	}
}

// pushTagsAfterImport passes all of a title's tags on once the media server
// has had time to scan the newly imported file.
func (s *Server) pushTagsAfterImport(k library.TitleKind, id int64) {
	tags, err := s.MovieRepo.TitleTags(k, id)
	if err != nil || len(tags) == 0 {
		return
	}
	tmdbID, err := s.titleTMDBID(k, id)
	if err != nil {
		return
	}
	time.AfterFunc(tagSyncDelay, func() { s.pushTags(k, tmdbID, tags, nil) })
}
