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
	// LocalSite is set for a site from the list that runs on the person's own
	// network: its address can be changed to their own server.
	LocalSite bool `json:"localSite,omitempty"`
	Priority  int  `json:"priority"` // 1 to 50, lower is tried first
	// MinSeeders is the fewest seeders a torrent from this indexer needs to be
	// grabbed by itself (0 = off).
	MinSeeders int `json:"minSeeders"`
	// ExtraCategories are category numbers the indexer is also searched in, per
	// kind of media ("movies", "tv", "music", "books", "audiobooks").
	ExtraCategories map[string][]int `json:"extraCategories"`
	// TorrentRoute is where torrents from this indexer are downloaded:
	// "client", "debrid", "builtin", or empty for the usual order.
	TorrentRoute string `json:"torrentRoute"`
	// FromProwlarr is set for an indexer the linked Prowlarr keeps in step.
	FromProwlarr bool `json:"fromProwlarr,omitempty"`
	// Back-off: set while searches skip the indexer after repeated failures.
	Failures    int    `json:"failures,omitempty"`    // failures in a row
	PausedUntil string `json:"pausedUntil,omitempty"` // RFC 3339; searches skip the indexer until then
	PausedNote  string `json:"pausedNote,omitempty"`  // the same in plain words
	PausedWhy   string `json:"pausedWhy,omitempty"`   // why the last failure happened, for example "it could not be reached"
	// Daily limit: RequestsPerDay is what a person set (0 = none); DailyLimit is
	// the limit in use, set or learned from the indexer (0 = none known).
	RequestsPerDay int    `json:"requestsPerDay"`
	DailyLimit     int    `json:"dailyLimit,omitempty"`
	LimitLearned   bool   `json:"limitLearned,omitempty"`  // DailyLimit came from the indexer's own answers
	RequestsToday  int    `json:"requestsToday,omitempty"` // requests Mediarium made to it today (UTC)
	LimitNote      string `json:"limitNote,omitempty"`     // plain words, while automatic searches leave the indexer alone
	// RSSEveryMinutes is how often the check for new releases reads this indexer,
	// when its daily limit makes that less often than the general setting.
	RSSEveryMinutes int    `json:"rssEveryMinutes,omitempty"`
	RSSNote         string `json:"rssNote,omitempty"` // the same in plain words
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
		Protocol: string(inst.Protocol), Enabled: inst.Enabled, LastTestError: inst.LastTestError, RequestsPerDay: inst.RequestsPerDay, MinSeeders: inst.MinSeeders, ExtraCategories: inst.ExtraCategories, TorrentRoute: inst.TorrentRoute,
		HasAPIKey: inst.APIKey != "", Priority: indexerRank(inst.Priority),
	}
	if !inst.LastTestAt.IsZero() {
		p.LastTestAt = inst.LastTestAt.UTC().Format(time.RFC3339)
	}
	addPauseInfo(&p, inst)
	addQuotaInfo(&p, inst)
	p.RSSEveryMinutes, p.RSSNote = s.rssNote(inst)
	if !inst.IsCardigann() {
		return p
	}
	p.Settings = map[string]any{}
	// the local cache only: listing indexers must never trigger a download
	sum, known := s.Definitions.CachedSummary(inst.DefinitionID)
	p.LocalSite = known && sum.Local && !sum.Custom
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
	linked := map[int64]bool{}
	for _, id := range s.prowlarrLinks() {
		linked[id] = true
	}
	out := make([]indexerPayload, len(list))
	for i, inst := range list {
		out[i] = s.toIndexerPayload(inst)
		out[i].FromProwlarr = linked[inst.ID]
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
		checkRequestsPerDay(req.RequestsPerDay),
		checkMinSeeders(req.MinSeeders),
		indexers.ExtraCategories(req.ExtraCategories).Validate(),
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
		RequestsPerDay: req.RequestsPerDay, MinSeeders: req.MinSeeders, TorrentRoute: indexers.CleanTorrentRoute(req.TorrentRoute),
		ExtraCategories: indexers.ExtraCategories(req.ExtraCategories).Normalize(),
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
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Could not download the list of indexers (check that this server can reach github.com): %s", err.Error()))
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
	if rejectBad(w, checkRequestsPerDay(req.RequestsPerDay), checkMinSeeders(req.MinSeeders), indexers.ExtraCategories(req.ExtraCategories).Validate()) {
		return
	}
	base := strings.TrimSpace(req.BaseURL)
	if base == "" && len(sum.Links) > 0 {
		base = sum.Links[0]
	}
	if !siteAddressAllowed(w, sum, base) {
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
		Enabled: true, Settings: settingsMap, RequestsPerDay: req.RequestsPerDay, MinSeeders: req.MinSeeders, TorrentRoute: indexers.CleanTorrentRoute(req.TorrentRoute),
		ExtraCategories: indexers.ExtraCategories(req.ExtraCategories).Normalize(),
	}, sum.IsSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, s.toIndexerPayload(created))
}

// localSiteExample is the example address shown in messages about a site on
// the person's own network.
const localSiteExample = "http://192.168.1.10:3333"

// siteAddressAllowed judges the address for a site from the list. A site that
// runs on the person's own network (a BitMagnet, say) takes any web address,
// because the one in its definition is only an example; every other site keeps
// to the addresses its definition lists. It writes the refusal and returns
// false when the address is not allowed.
func siteAddressAllowed(w http.ResponseWriter, sum indexers.DefinitionSummary, addr string) bool {
	addr = strings.TrimSpace(addr)
	if sum.Local && !sum.Custom {
		switch indexers.CheckLocalSiteAddress(addr) {
		case indexers.AddrOK:
			return true
		case indexers.AddrEmpty:
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Add the address of your server, for example %s.", localSiteExample))
		case indexers.AddrTooLong:
			writeError(w, http.StatusBadRequest, "The address is too long. It can have at most 200 characters.")
		case indexers.AddrUserInfo:
			writeError(w, http.StatusBadRequest, "Leave the user name and password out of the address. Sign-in details go in the site's settings.")
		case indexers.AddrQuery:
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Leave everything after a ? or # out of the address. Use only the server and the port, for example %s.", localSiteExample))
		default:
			writeError(w, http.StatusBadRequest, fmt.Sprintf("The address has to start with http or https, then the server and, if needed, the port, for example %s.", localSiteExample))
		}
		return false
	}
	if !sum.HasLink(addr) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("The address must be one of the site's own addresses: %s", strings.Join(sum.Links, ", ")))
		return false
	}
	return true
}

type indexerUpdate struct {
	Name     *string        `json:"name"`
	BaseURL  *string        `json:"baseUrl"`
	APIKey   *string        `json:"apiKey"` // blank keeps the saved key
	Enabled  *bool          `json:"enabled"`
	Protocol *string        `json:"protocol"` // Newznab/Torznab only: "usenet" or "torrent"
	Settings map[string]any `json:"settings"` // secret settings left blank keep their saved value
	// RequestsPerDay is the most requests a day Mediarium makes to the indexer
	// (0 = no limit set). Left out keeps the saved value.
	RequestsPerDay *int `json:"requestsPerDay"`
	// MinSeeders is the fewest seeders a torrent needs to be grabbed by itself
	// (0 = off). Left out keeps the saved value.
	MinSeeders *int `json:"minSeeders"`
	// ExtraCategories replaces the extra category numbers (an empty object
	// clears them). Left out keeps the saved ones.
	ExtraCategories map[string][]int `json:"extraCategories"`
	// TorrentRoute is where torrents from this indexer go: "client", "debrid",
	// "builtin", or "" for the usual order. Left out keeps the saved value.
	TorrentRoute *string `json:"torrentRoute"`
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
	if req.RequestsPerDay != nil {
		if rejectBad(w, checkRequestsPerDay(*req.RequestsPerDay)) {
			return
		}
		inst.RequestsPerDay = *req.RequestsPerDay
	}
	if req.MinSeeders != nil {
		if rejectBad(w, checkMinSeeders(*req.MinSeeders)) {
			return
		}
		inst.MinSeeders = *req.MinSeeders
	}
	if req.ExtraCategories != nil {
		extra := indexers.ExtraCategories(req.ExtraCategories)
		if rejectBad(w, extra.Validate()) {
			return
		}
		inst.ExtraCategories = extra.Normalize()
	}
	if req.TorrentRoute != nil {
		route := strings.TrimSpace(*req.TorrentRoute)
		if route != indexers.CleanTorrentRoute(route) {
			writeError(w, http.StatusBadRequest, "Choose where torrents from this indexer go: the usual order, the download client, the debrid service or the built-in client.")
			return
		}
		inst.TorrentRoute = route
	}
	var secret indexers.Secrets
	if inst.IsCardigann() {
		sum, ok := s.definitionFor(r.Context(), w, inst.DefinitionID)
		if !ok {
			return
		}
		secret = sum.IsSecret
		if req.BaseURL != nil && strings.TrimSpace(*req.BaseURL) != "" {
			// An address that is already saved is not checked again, so a site
			// whose definition changed can still be renamed or switched off.
			if v := strings.TrimSpace(*req.BaseURL); v != inst.BaseURL && !siteAddressAllowed(w, sum, v) {
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
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Could not download the list of indexers (check that this server can reach github.com): %s", err.Error()))
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

// indexerTestBudget is how long the check of one indexer may take. A site that
// goes through FlareSolverr needs a minute or more for the Cloudflare check.
func (s *Server) indexerTestBudget(inst indexers.Instance) time.Duration {
	if !inst.IsCardigann() {
		return 2 * connTestTimeout
	}
	if s.flareSolverrURL() != "" {
		return 100 * time.Second
	}
	return 2 * connTestTimeout
}

// testIndexerInstance runs the right connection test for any kind of indexer.
func (s *Server) testIndexerInstance(ctx context.Context, inst indexers.Instance) connTestResult {
	if !inst.IsCardigann() {
		return testIndexer(ctx, inst.Name, inst.BaseURL, inst.APIKey)
	}
	// Passing a Cloudflare check through FlareSolverr can take up to a minute
	// the first time; after that its cookies are reused and requests are quick.
	timeout := s.indexerTestBudget(inst)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := s.Cardigann.Test(ctx, inst)
	if err != nil {
		if addr, ok := indexers.FlareSolverrDown(err); ok {
			return connTestResult{Message: indexers.FlareSolverrDownMessage(addr), Detail: err.Error()}
		}
		if status, ok := indexers.GoneStatus(err); ok {
			return connTestResult{Message: indexers.GoneMessage(status), Detail: err.Error()}
		}
		if errors.Is(err, context.DeadlineExceeded) && s.flareSolverrURL() != "" {
			return connTestResult{Message: fmt.Sprintf("The check waited %d seconds for FlareSolverr and it didn't finish. Check that FlareSolverr is running and has internet access, then try again.", int(timeout/time.Second))}
		}
		if indexers.LoopbackSite(inst.BaseURL) && strings.Contains(strings.ToLower(err.Error()), "connection refused") {
			return connTestResult{Message: fmt.Sprintf("The server refused the connection. Check it's running and the address and port are right. Inside Docker, %s is the Mediarium container itself, so use the address of the machine that runs it, for example %s.", "127.0.0.1", localSiteExample)}
		}
		return connTestResult{Message: plainerror.Message(err)}
	}
	if res.Releases == 0 {
		return connTestResult{Message: "Signed in and searched, but the site returned no releases. Try updating the site list."}
	}
	msg := fmt.Sprintf("Connected. The site returned %s.", plural(res.Releases, "recent release"))
	if res.ViaFlareSolverr && s.vpnForIndexers() {
		msg += " FlareSolverr fetched it past Cloudflare, so the site saw FlareSolverr's address, not your VPN's."
	}
	return connTestResult{OK: true, Message: msg}
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

// maxRequestsPerDay is the largest daily limit that can be set.
const maxRequestsPerDay = 100000

// checkRequestsPerDay wants 0 (no limit) or a number of requests a day.
func checkRequestsPerDay(n int) string {
	if n < 0 || n > maxRequestsPerDay {
		return fmt.Sprintf("Requests per day must be a number from 0 to %d. Use 0 for no limit.", maxRequestsPerDay)
	}
	return ""
}

func checkIndexerProtocol(p string) string {
	return checkChoice(p, indexerProtocolMessage, string(indexers.ProtocolUsenet), string(indexers.ProtocolTorrent))
}
