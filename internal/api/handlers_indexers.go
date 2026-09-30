package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/settings"
)

// indexerPayload is an indexer as the UI sees it. For definition-based
// sites, Settings carries the definition's settings: in requests all of
// them, in responses only the non-secret ones (StoredSecrets lists the
// secret ones that have a saved value).
type indexerPayload struct {
	ID            int64          `json:"id,omitempty"`
	Name          string         `json:"name"`
	Kind          string         `json:"kind"` // "newznab", "torznab" or "cardigann"
	DefinitionID  string         `json:"definitionId"`
	BaseURL       string         `json:"baseUrl"`
	APIKey        string         `json:"apiKey,omitempty"` // request only; never returned
	HasAPIKey     bool           `json:"hasApiKey"`        // an API key is saved (Newznab/Torznab)
	Protocol      string         `json:"protocol"`         // "usenet" or "torrent"
	Enabled       bool           `json:"enabled"`
	Settings      map[string]any `json:"settings,omitempty"`
	StoredSecrets []string       `json:"storedSecrets,omitempty"`
	LastTestError string         `json:"lastTestError,omitempty"`
	LastTestAt    string         `json:"lastTestAt,omitempty"`
}

// isCardigann reports whether a create/test request is for a
// definition-based site.
func (p indexerPayload) isCardigann() bool {
	return p.Kind == string(indexers.KindCardigann) || (p.Settings != nil && p.Kind == "")
}

// toIndexerPayload never includes the API key or secret settings.
func (s *Server) toIndexerPayload(inst indexers.Instance) indexerPayload {
	p := indexerPayload{
		ID: inst.ID, Name: inst.Name, Kind: string(inst.Kind), DefinitionID: inst.DefinitionID, BaseURL: inst.BaseURL,
		Protocol: string(inst.Protocol), Enabled: inst.Enabled, LastTestError: inst.LastTestError,
		HasAPIKey: inst.APIKey != "",
	}
	if !inst.LastTestAt.IsZero() {
		p.LastTestAt = inst.LastTestAt.UTC().Format(time.RFC3339)
	}
	if !inst.IsCardigann() {
		return p
	}
	p.Settings = map[string]any{}
	// the local cache only: listing indexers must never trigger a download
	sum, known := s.Definitions.CachedSummary(inst.DefinitionID)
	for k, v := range inst.Settings {
		secret := indexers.SecretSetting("", k)
		if known {
			secret = sum.IsSecret(k)
		}
		if secret {
			if v != "" {
				p.StoredSecrets = append(p.StoredSecrets, k)
			}
			continue
		}
		if known {
			if st, ok := sum.Setting(k); ok && st.Type == "checkbox" {
				p.Settings[k] = v == "true"
				continue
			}
		}
		p.Settings[k] = v
	}
	sort.Strings(p.StoredSecrets)
	return p
}

// handleListIndexers lists the configured indexers, never with their API keys or passwords.
func (s *Server) handleListIndexers(w http.ResponseWriter, r *http.Request) {
	list, err := s.IndexerRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]indexerPayload, len(list))
	for i, inst := range list {
		out[i] = s.toIndexerPayload(inst)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateIndexer adds an indexer: a Newznab/Torznab API, or a site from the definition list (definitionId + settings).
func (s *Server) handleCreateIndexer(w http.ResponseWriter, r *http.Request) {
	var req indexerPayload
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if req.isCardigann() {
		s.createCardigannIndexer(w, r, req)
		return
	}
	req.Name, req.BaseURL, req.APIKey = strings.TrimSpace(req.Name), strings.TrimSpace(req.BaseURL), strings.TrimSpace(req.APIKey)
	if rejectBad(w,
		checkRequired(req.Name, "Give this indexer a name, for example the name of the website."),
		checkIndexerName(req.Name),
		checkRequired(req.BaseURL, "Add the address of the indexer, for example https://api.example.com."),
		checkIndexerAddress(req.BaseURL),
		checkAPIKey(req.APIKey),
		checkIndexerProtocol(req.Protocol),
		checkMaxLen(req.DefinitionID, "The site ID", indexerNameMax), checkNoControl(req.DefinitionID, "The site ID"),
	) {
		return
	}
	protocol := indexers.Protocol(req.Protocol)
	if protocol != indexers.ProtocolTorrent {
		protocol = indexers.ProtocolUsenet
	}
	created, err := s.IndexerRepo.Create(indexers.Instance{
		Name: req.Name, DefinitionID: req.DefinitionID, BaseURL: req.BaseURL, APIKey: req.APIKey, Protocol: protocol, Enabled: true,
	}, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, s.toIndexerPayload(created))
}

// definitionFor finds a definition's catalogue entry, downloading the
// catalogue if needed, and writes the error response if it can't be used.
func (s *Server) definitionFor(ctx context.Context, w http.ResponseWriter, id string) (indexers.DefinitionSummary, bool) {
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "definitionId is required")
		return indexers.DefinitionSummary{}, false
	}
	sum, err := s.Definitions.Summary(ctx, id)
	switch {
	case errors.Is(err, indexers.ErrDefinitionNotFound):
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown indexer %q", id))
		return sum, false
	case err != nil:
		writeError(w, http.StatusBadGateway, "Could not download the list of indexers (check that this server can reach github.com): "+err.Error())
		return sum, false
	case !sum.Supported:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%s uses something Mediarium can't run yet (%s).", sum.Name, sum.Problem))
		return sum, false
	}
	return sum, true
}

// cardigannSettings validates and normalises a settings object against the
// definition: unknown and info-only keys are dropped, checkboxes become
// "true"/"false", select values must be one of the options.
func cardigannSettings(sum indexers.DefinitionSummary, in map[string]any) (map[string]string, error) {
	out := map[string]string{}
	for k, raw := range in {
		st, ok := sum.Setting(k)
		if !ok || strings.HasPrefix(st.Type, "info") {
			continue
		}
		var v string
		switch x := raw.(type) {
		case nil:
		case string:
			v = x
		case bool:
			v = strconv.FormatBool(x)
		case float64:
			v = strconv.FormatFloat(x, 'f', -1, 64)
		default:
			return nil, fmt.Errorf("The %q setting has to be text, a number, or true or false.", k)
		}
		switch st.Type {
		case "checkbox":
			b := v == "true" || v == "1" || v == "on"
			v = strconv.FormatBool(b)
		case "select":
			valid := false
			for _, o := range st.Options {
				if o.Value == v {
					valid = true
				}
			}
			if !valid && v != "" {
				return nil, fmt.Errorf("%q isn't one of the choices for the %q setting. Pick one from the list.", v, k)
			}
		default:
			v = strings.TrimSpace(v)
		}
		if strings.ContainsRune(v, 0) || len(v) > indexerSettingMax {
			return nil, fmt.Errorf("The %q setting is too long or has characters that can't be saved. Check what you pasted.", k)
		}
		out[k] = v
	}
	return out, nil
}

func (s *Server) createCardigannIndexer(w http.ResponseWriter, r *http.Request, req indexerPayload) {
	sum, ok := s.definitionFor(r.Context(), w, req.DefinitionID)
	if !ok {
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
	settingsMap, err := cardigannSettings(sum, req.Settings)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if rejectBad(w, checkIndexerName(name)) {
		return
	}
	if name == "" {
		name = sum.Name
	}
	protocol := indexers.Protocol(sum.Protocol)
	if protocol != indexers.ProtocolUsenet {
		protocol = indexers.ProtocolTorrent
	}
	created, err := s.IndexerRepo.Create(indexers.Instance{
		Name: name, Kind: indexers.KindCardigann, DefinitionID: sum.ID, BaseURL: base, Protocol: protocol,
		Enabled: true, Settings: settingsMap,
	}, sum.IsSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, s.toIndexerPayload(created))
}

type indexerUpdate struct {
	Name     *string        `json:"name"`
	BaseURL  *string        `json:"baseUrl"`
	APIKey   *string        `json:"apiKey"` // blank keeps the saved key
	Enabled  *bool          `json:"enabled"`
	Protocol *string        `json:"protocol"` // Newznab/Torznab only: "usenet" or "torrent"
	Settings map[string]any `json:"settings"` // secret settings left blank keep their saved value
}

// handleUpdateIndexer edits a saved indexer. Every field is optional: what
// is left out keeps its saved value, and secret values (API key, passwords)
// left blank keep what is stored.
func (s *Server) handleUpdateIndexer(w http.ResponseWriter, r *http.Request) {
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
	var req indexerUpdate
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
		if rejectBad(w, checkIndexerName(strings.TrimSpace(*req.Name))) {
			return
		}
		inst.Name = strings.TrimSpace(*req.Name)
	}
	if req.Enabled != nil {
		inst.Enabled = *req.Enabled
	}
	var secret indexers.Secrets
	if inst.IsCardigann() {
		sum, ok := s.definitionFor(r.Context(), w, inst.DefinitionID)
		if !ok {
			return
		}
		secret = sum.IsSecret
		if req.BaseURL != nil && strings.TrimSpace(*req.BaseURL) != "" {
			if !sum.HasLink(*req.BaseURL) {
				writeError(w, http.StatusBadRequest, "The address must be one of the site's own addresses: "+strings.Join(sum.Links, ", "))
				return
			}
			inst.BaseURL = strings.TrimSpace(*req.BaseURL)
		}
		if req.Settings != nil {
			updates, err := cardigannSettings(sum, req.Settings)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if inst.Settings == nil {
				inst.Settings = map[string]string{}
			}
			for k, v := range updates {
				if v == "" && sum.IsSecret(k) {
					continue // blank keeps the saved secret
				}
				inst.Settings[k] = v
			}
		}
	} else {
		if req.BaseURL != nil && strings.TrimSpace(*req.BaseURL) != "" {
			// An address that is already saved is not checked again, so an old
			// source can still be renamed or switched off.
			if v := strings.TrimSpace(*req.BaseURL); v != inst.BaseURL && rejectBad(w, checkIndexerAddress(v)) {
				return
			}
			if inst.APIKey != "" && (req.APIKey == nil || strings.TrimSpace(*req.APIKey) == "") && addrMoved(inst.BaseURL, *req.BaseURL) {
				writeError(w, http.StatusBadRequest, retypeSecretMessage("API key"))
				return
			}
			inst.BaseURL = strings.TrimSpace(*req.BaseURL)
		}
		if req.APIKey != nil && strings.TrimSpace(*req.APIKey) != "" {
			if rejectBad(w, checkAPIKey(*req.APIKey)) {
				return
			}
			inst.APIKey = strings.TrimSpace(*req.APIKey)
		}
		if req.Protocol != nil && *req.Protocol != "" {
			switch p := indexers.Protocol(*req.Protocol); p {
			case indexers.ProtocolUsenet, indexers.ProtocolTorrent:
				if p != inst.Protocol {
					inst.Protocol = p
					inst.Kind = "" // Newznab for Usenet, Torznab for torrents
				}
			default:
				writeError(w, http.StatusBadRequest, indexerProtocolMessage)
				return
			}
		}
	}
	if err := s.IndexerRepo.Update(inst, secret); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Cardigann.Forget(inst.ID) // new settings: sign in again
	updated, err := s.IndexerRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.toIndexerPayload(updated))
}

func (s *Server) handleDeleteIndexer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid indexer ID.")
		return
	}
	if err := s.IndexerRepo.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

type indexerDefinitionsPayload struct {
	UpdatedAt    string                       `json:"updatedAt"`
	Source       string                       `json:"source"`
	Licence      string                       `json:"licence"`
	RefreshError string                       `json:"refreshError,omitempty"`
	Definitions  []indexers.DefinitionSummary `json:"definitions"`
}

// handleListIndexerDefinitions lists the sites that can be added, downloading the community definitions on first use (refresh=1 re-downloads them).
func (s *Server) handleListIndexerDefinitions(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true"
	idx, err := s.Definitions.Catalogue(r.Context(), force)
	if idx == nil {
		writeError(w, http.StatusBadGateway, "Could not download the list of indexers (check that this server can reach github.com): "+err.Error())
		return
	}
	out := indexerDefinitionsPayload{
		UpdatedAt:   idx.UpdatedAt.UTC().Format(time.RFC3339),
		Source:      idx.Source,
		Licence:     indexers.DefinitionsLicence,
		Definitions: idx.Definitions,
	}
	if out.Definitions == nil {
		out.Definitions = []indexers.DefinitionSummary{}
	}
	if err != nil {
		out.RefreshError = plainerror.Message(err)
	}
	writeJSON(w, http.StatusOK, out)
}

// flareSolverrURL is the FlareSolverr address in use: the one saved in
// Settings, or the built-in helper of the "-full" image when none is saved
// ("" = none).
func (s *Server) flareSolverrURL() string {
	if v := s.flareSolverrSetting(); v != "" {
		return v
	}
	if s.cfg.BundledFlareSolverr {
		return bundledFlareSolverrURL
	}
	return ""
}

// flareSolverrSetting is only what the person saved in Settings ("" = none).
func (s *Server) flareSolverrSetting() string {
	v, _ := s.Settings.Get(settings.KeyFlareSolverrURL)
	return strings.TrimSpace(v)
}

// testIndexerInstance runs the right connection test for any kind of indexer.
func (s *Server) testIndexerInstance(ctx context.Context, inst indexers.Instance) connTestResult {
	if !inst.IsCardigann() {
		return testIndexer(ctx, inst.Name, inst.BaseURL, inst.APIKey)
	}
	// Passing a Cloudflare check through FlareSolverr can take up to a minute
	// the first time; after that its cookies are reused and requests are quick.
	timeout := 2 * connTestTimeout
	if s.flareSolverrURL() != "" {
		timeout = 100 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	n, err := s.Cardigann.Test(ctx, inst)
	if err != nil {
		return connTestResult{Message: plainerror.Message(err)}
	}
	if n == 0 {
		return connTestResult{Message: "Signed in and searched, but the site returned no releases. Try updating the site list."}
	}
	return connTestResult{OK: true, Message: fmt.Sprintf("Connected. The site returned %s.", plural(n, "recent release"))}
}

const (
	indexerNameMax         = 80
	indexerProtocolMessage = `Choose "usenet" or "torrent" for the kind of indexer.`
	indexerSettingMax      = 8000
)

func checkIndexerName(name string) string {
	return firstProblem(checkMaxLen(name, "The name", indexerNameMax), checkNoControl(name, "The name"))
}

// checkIndexerAddress wants a full web address for a Newznab or Torznab source.
func checkIndexerAddress(v string) string {
	return firstProblem(
		checkHTTPURL(v, "https://api.example.com", true),
		checkMaxLen(v, "The address", 2000),
		checkNoControl(v, "The address"),
	)
}

func checkIndexerProtocol(p string) string {
	return checkChoice(p, indexerProtocolMessage, string(indexers.ProtocolUsenet), string(indexers.ProtocolTorrent))
}
