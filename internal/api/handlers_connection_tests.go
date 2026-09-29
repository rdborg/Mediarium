package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
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
	if !req.isCardigann() {
		writeJSON(w, http.StatusOK, testIndexer(r.Context(), req.Name, req.BaseURL, req.APIKey))
		return
	}
	sum, ok := s.definitionFor(r.Context(), w, req.DefinitionID)
	if !ok {
		return
	}
	settingsMap, err := cardigannSettings(sum, req.Settings)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	base := strings.TrimSpace(req.BaseURL)
	if base == "" && len(sum.Links) > 0 {
		base = sum.Links[0]
	}
	if !sum.HasLink(base) {
		writeError(w, http.StatusBadRequest, "baseUrl must be one of the site's addresses: "+strings.Join(sum.Links, ", "))
		return
	}
	name := req.Name
	if name == "" {
		name = sum.Name
	}
	// ID 0: a throw-away session, nothing is kept
	writeJSON(w, http.StatusOK, s.testIndexerInstance(r.Context(), indexers.Instance{
		Name: name, Kind: indexers.KindCardigann, DefinitionID: sum.ID, BaseURL: base, Settings: settingsMap, Cardigann: s.Cardigann,
	}))
}

// handleTestIndexer tests a saved indexer with its stored credentials.
func (s *Server) handleTestIndexer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid indexer id")
		return
	}
	inst, err := s.IndexerRepo.Get(id)
	if errors.Is(err, indexers.ErrNotFound) {
		writeError(w, http.StatusNotFound, "indexer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	res := s.testIndexerInstance(r.Context(), inst)
	msg := ""
	if !res.OK {
		msg = res.Message
	}
	if err := s.IndexerRepo.SetTestResult(inst.ID, msg, time.Now()); err != nil {
		log.Printf("api: record indexer test result: %v", err)
	}
	writeJSON(w, http.StatusOK, res)
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
