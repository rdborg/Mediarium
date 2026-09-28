package api

import (
	"net/http"
	"strconv"

	"github.com/ryanborg/mediarium/internal/indexers"
)

type indexerPayload struct {
	ID           int64  `json:"id,omitempty"`
	Name         string `json:"name"`
	DefinitionID string `json:"definitionId"`
	BaseURL      string `json:"baseUrl"`
	APIKey       string `json:"apiKey,omitempty"`
	Protocol     string `json:"protocol"` // "usenet" or "torrent" (PRD §7 Phase 2)
	Enabled      bool   `json:"enabled"`
}

func (s *Server) handleListIndexers(w http.ResponseWriter, r *http.Request) {
	list, err := s.IndexerRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]indexerPayload, len(list))
	for i, inst := range list {
		// API key intentionally omitted from the response (PRD §11).
		out[i] = indexerPayload{ID: inst.ID, Name: inst.Name, DefinitionID: inst.DefinitionID, BaseURL: inst.BaseURL, Protocol: string(inst.Protocol), Enabled: inst.Enabled}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateIndexer(w http.ResponseWriter, r *http.Request) {
	var req indexerPayload
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "name and baseUrl are required")
		return
	}
	protocol := indexers.Protocol(req.Protocol)
	if protocol != indexers.ProtocolTorrent {
		protocol = indexers.ProtocolUsenet
	}
	created, err := s.IndexerRepo.Create(indexers.Instance{
		Name: req.Name, DefinitionID: req.DefinitionID, BaseURL: req.BaseURL, APIKey: req.APIKey, Protocol: protocol, Enabled: true,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, indexerPayload{ID: created.ID, Name: created.Name, DefinitionID: created.DefinitionID, BaseURL: created.BaseURL, Protocol: string(created.Protocol), Enabled: created.Enabled})
}

func (s *Server) handleDeleteIndexer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid indexer id")
		return
	}
	if err := s.IndexerRepo.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}
