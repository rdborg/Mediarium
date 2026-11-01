package api

import (
	"context"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/indexers"
)

// connTestResult is the outcome of a "Test" button. A failed test is a
// normal 200 with ok=false and a human-readable message, not an HTTP error:
// the request itself worked, the thing being tested didn't.
type connTestResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	// Detail is the technical text behind Message when Message is an
	// explanation written for the person (FlareSolverr not answering, a site
	// that says its page is gone).
	Detail string `json:"detail,omitempty"`
}

const connTestTimeout = 15 * time.Second

func testIndexer(ctx context.Context, name, baseURL, apiKey string) connTestResult {
	if baseURL == "" {
		return connTestResult{Message: "Enter the indexer's address first."}
	}
	ctx, cancel := context.WithTimeout(ctx, max(connTestTimeout, indexers.FeedWait(baseURL)))
	defer cancel()
	client := indexers.NewNewznabClient(name, baseURL, apiKey)
	results, err := client.Search(ctx, "", []int{2000, 5000})
	if err != nil {
		return connTestResult{Message: err.Error()}
	}
	return connTestResult{OK: true, Message: fmt.Sprintf("Connected. The indexer returned %s.", plural(len(results), "recent release"))}
}

func testNNTP(host string, port int, useSSL bool, username, password string) connTestResult {
	if host == "" || port == 0 {
		return connTestResult{Message: "Enter the server host and port first."}
	}
	login := download.ClientConfig{Host: host, Port: port, UseSSL: useSSL, Username: username}
	if working, _ := download.LoginUse(login); working {
		// Downloads hold signed-in connections right now, which proves the
		// server and the login work. A test would only add one more connection
		// to a login that may be full.
		return connTestResult{OK: true, Message: "Connected and logged in. Downloads are using this login right now."}
	}
	conn, err := download.DialNNTP(host, port, useSSL, 10*time.Second)
	if err != nil {
		if download.IsTooManyConnections(err) {
			return tooManyTestResult(login)
		}
		if msg, ok := certificateProblem(err); ok {
			return connTestResult{Message: msg}
		}
		return connTestResult{Message: err.Error()}
	}
	defer conn.Quit()
	if err := conn.Authenticate(username, password); err != nil {
		if download.IsTooManyConnections(err) {
			return tooManyTestResult(login)
		}
		if msg, ok := download.FriendlyError(err); ok {
			return connTestResult{Message: "Connected, but the login failed. " + msg}
		}
		return connTestResult{Message: "Connected, but signing in failed: " + err.Error()}
	}
	return connTestResult{OK: true, Message: "Connected and logged in."}
}

// tooManyTestResult is the answer to a test the provider refused for having too
// many connections open. When Mediarium's own downloads have the login busy (or
// just closed connections the provider has not counted out yet), that says
// nothing about the server or the login, so it is not a failure.
func tooManyTestResult(login download.ClientConfig) connTestResult {
	if _, busy := download.LoginUse(login); busy {
		return connTestResult{OK: true, Message: "The server is up. Downloads are using every connection this login allows right now, so there was no room for the test."}
	}
	return connTestResult{Message: download.TooManyConnectionsMessage}
}

// certificateProblem puts a refused SSL certificate in plain words. Mediarium
// only connects to news servers whose certificate a public authority vouches
// for, so a self-signed one can't be used over SSL.
func certificateProblem(err error) (string, bool) {
	var (
		unknown  x509.UnknownAuthorityError
		hostname x509.HostnameError
		invalid  x509.CertificateInvalidError
	)
	switch {
	case errors.As(err, &unknown):
		return "The server's certificate isn't trusted (it is self-signed, or signed by an authority Mediarium doesn't know), so Mediarium won't connect to it over SSL. Use the SSL address and port your provider gives, or turn SSL off for this server.", true
	case errors.As(err, &hostname):
		return "The server's certificate is for a different name. Use the exact server name your provider gives.", true
	case errors.As(err, &invalid) && invalid.Reason == x509.Expired:
		return "The server's certificate has expired or isn't valid yet. Check the clock of the computer Mediarium runs on, or ask your provider.", true
	}
	return "", false
}

// handleTestIndexerConfig tests an indexer that has not been saved yet.
func (s *Server) handleTestIndexerConfig(w http.ResponseWriter, r *http.Request) {
	var req indexerPayload
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if !req.isCardigann() {
		req.BaseURL, req.APIKey = strings.TrimSpace(req.BaseURL), strings.TrimSpace(req.APIKey)
		if req.BaseURL != "" && rejectBad(w, checkIndexerAddress(req.BaseURL), checkAPIKey(req.APIKey)) {
			return
		}
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
		writeError(w, http.StatusBadRequest, "The address must be one of the site's own addresses: "+strings.Join(sum.Links, ", "))
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
		writeError(w, http.StatusBadRequest, "That isn't a valid indexer ID.")
		return
	}
	inst, err := s.IndexerRepo.Get(id)
	if errors.Is(err, indexers.ErrNotFound) {
		writeError(w, http.StatusNotFound, "That indexer no longer exists.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	inst.Tracker.CountRequest(inst.ID, 1) // the test is a real search
	res := s.testIndexerInstance(r.Context(), inst)
	if !res.OK {
		inst.Tracker.LearnLimit(inst.ID, res.Message) // "Request limit reached" is worth knowing
	}
	msg := ""
	if !res.OK {
		msg = res.Message
	}
	if err := s.IndexerRepo.SetTestResult(inst.ID, msg, time.Now()); err != nil {
		log.Printf("api: record indexer test result: %v", err)
	}
	if res.OK {
		s.IndexerRepo.Tracker().Resume(inst.ID) // it answered: searches may use it again
	}
	writeJSON(w, http.StatusOK, res)
}

// handleIndexerCategories lists the categories a saved Newznab or Torznab
// indexer says it has (its capabilities), as suggestions for the extra
// categories. It is a help only: when the indexer can't be asked, or it is a
// site from the definition list, the answer is an empty list.
func (s *Server) handleIndexerCategories(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid indexer ID.")
		return
	}
	inst, err := s.IndexerRepo.Get(id)
	if errors.Is(err, indexers.ErrNotFound) {
		writeError(w, http.StatusNotFound, "That indexer no longer exists.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := struct {
		Categories []indexers.CapCategory `json:"categories"`
	}{Categories: []indexers.CapCategory{}}
	if inst.IsCardigann() || inst.SecretError != "" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	inst.Tracker.CountRequest(inst.ID, 1)
	cctx, cancel := context.WithTimeout(r.Context(), max(connTestTimeout, indexers.FeedWait(inst.BaseURL)))
	defer cancel()
	cats, err := indexers.NewNewznabClient(inst.Name, inst.BaseURL, inst.APIKey).Categories(cctx)
	if err != nil {
		log.Printf("api: could not read the categories of indexer %q: %v", inst.Name, err)
	} else {
		out.Categories = cats
	}
	writeJSON(w, http.StatusOK, out)
}

type enabledRequest struct {
	Enabled bool `json:"enabled"`
}

// handleSetIndexerPriority sets how much an indexer is preferred: a number
// from 1 to 50, lower first. The old words (preferred, normal, last resort)
// are still accepted.
func (s *Server) handleSetIndexerPriority(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid indexer ID.")
		return
	}
	var req struct {
		Priority any `json:"priority"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	priority, ok := indexers.ParsePriority(req.Priority)
	if !ok {
		writeError(w, http.StatusBadRequest, "The priority must be a whole number from 1 to 50.")
		return
	}
	if err := s.IndexerRepo.SetPriority(id, priority); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That indexer no longer exists.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

func (s *Server) handleSetIndexerEnabled(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid indexer ID.")
		return
	}
	var req enabledRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if err := s.IndexerRepo.SetEnabled(id, req.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}
