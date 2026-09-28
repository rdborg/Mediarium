package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ryanborg/mediarium/internal/download"
	"github.com/ryanborg/mediarium/internal/indexers"
)

// connTestResult is the outcome of a "Test" button. A failed test is a
// normal 200 with ok=false and a human-readable message, not an HTTP error:
// the request itself worked, the thing being tested didn't.
type connTestResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

const connTestTimeout = 15 * time.Second

func testIndexer(ctx context.Context, name, baseURL, apiKey string) connTestResult {
	if baseURL == "" {
		return connTestResult{Message: "Enter the indexer's base URL first."}
	}
	ctx, cancel := context.WithTimeout(ctx, connTestTimeout)
	defer cancel()
	client := indexers.NewNewznabClient(name, baseURL, apiKey)
	results, err := client.Search(ctx, "", []int{2000, 5000})
	if err != nil {
		return connTestResult{Message: err.Error()}
	}
	return connTestResult{OK: true, Message: fmt.Sprintf("Connected — the indexer returned %d recent release(s).", len(results))}
}

func testNNTP(host string, port int, useSSL bool, username, password string) connTestResult {
	if host == "" || port == 0 {
		return connTestResult{Message: "Enter the server host and port first."}
	}
	conn, err := download.DialNNTP(host, port, useSSL, 10*time.Second)
	if err != nil {
		return connTestResult{Message: err.Error()}
	}
	defer conn.Quit()
	if err := conn.Authenticate(username, password); err != nil {
		return connTestResult{Message: "Connected, but login failed: " + err.Error()}
	}
	return connTestResult{OK: true, Message: "Connected and logged in."}
}

// handleTestIndexerConfig tests an indexer that has not been saved yet.
func (s *Server) handleTestIndexerConfig(w http.ResponseWriter, r *http.Request) {
	var req indexerPayload
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	writeJSON(w, http.StatusOK, testIndexer(r.Context(), req.Name, req.BaseURL, req.APIKey))
}

// handleTestIndexer tests a saved indexer with its stored credentials.
func (s *Server) handleTestIndexer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid indexer id")
		return
	}
	list, err := s.IndexerRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, inst := range list {
		if inst.ID == id {
			writeJSON(w, http.StatusOK, testIndexer(r.Context(), inst.Name, inst.BaseURL, inst.APIKey))
			return
		}
	}
	writeError(w, http.StatusNotFound, "indexer not found")
}

type enabledRequest struct {
	Enabled bool `json:"enabled"`
}

func (s *Server) handleSetIndexerEnabled(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid indexer id")
		return
	}
	var req enabledRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.IndexerRepo.SetEnabled(id, req.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}
