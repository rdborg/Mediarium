package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/plainerror"
)

// mediaServersImported tells the media servers about newly imported files;
// the refresh runs later in the background and never affects the import.
func (s *Server) mediaServersImported(kind mediaservers.MediaKind, files ...string) {
	if s.mediaRefresher == nil {
		return
	}
	s.mediaRefresher.Imported(kind, files...)
}

// movieImported and episodesImported are the pipeline's hooks into
// mediaServersImported.
func (s *Server) movieImported(file string) { s.mediaServersImported(mediaservers.MediaMovie, file) }

// musicImported tells the media servers an album was imported: their music
// libraries scan the album folder (Plex sections of type artist, Jellyfin and
// Emby through the same folder update as movies and shows).
func (s *Server) musicImported(files ...string) {
	s.mediaServersImported(mediaservers.MediaMusic, files...)
}

// booksImported tells the media servers a book was imported (Audiobookshelf
// and Kavita scan the library that holds it).
func (s *Server) booksImported(files ...string) {
	s.mediaServersImported(mediaservers.MediaBook, files...)
}

func (s *Server) episodesImported(files ...string) {
	s.mediaServersImported(mediaservers.MediaTV, files...)
}

// mediaServerRequest is the body of create, update and test. Blank fields
// on update (and on a test that names a saved server) keep the saved value;
// that is how the token stays without being sent back.
type mediaServerRequest struct {
	ID                 int64                      `json:"id,omitempty"` // test only: a saved server to take blank fields from
	Name               string                     `json:"name"`
	Kind               string                     `json:"kind"`
	BaseURL            string                     `json:"baseUrl"`
	PublicURL          *string                    `json:"publicUrl,omitempty"`
	Token              string                     `json:"token,omitempty"`
	APIKey             string                     `json:"apiKey,omitempty"` // same as token (Jellyfin/Emby call it an API key)
	Enabled            *bool                      `json:"enabled,omitempty"`
	RefreshAfterImport *bool                      `json:"refreshAfterImport,omitempty"`
	PathMap            []mediaservers.PathMapping `json:"pathMap,omitempty"`
}

func (r mediaServerRequest) token() string {
	if t := strings.TrimSpace(r.Token); t != "" {
		return t
	}
	return strings.TrimSpace(r.APIKey)
}

// mediaServerPayload is a server on the way out. The token is never
// included, only whether one is saved.
type mediaServerPayload struct {
	ID                 int64                      `json:"id"`
	Name               string                     `json:"name"`
	Kind               mediaservers.Kind          `json:"kind"`
	BaseURL            string                     `json:"baseUrl"`
	PublicURL          string                     `json:"publicUrl"` // as entered; "" means baseUrl is used
	WebURL             string                     `json:"webUrl"`    // the address links use
	HasToken           bool                       `json:"hasToken"`
	Enabled            bool                       `json:"enabled"`
	RefreshAfterImport bool                       `json:"refreshAfterImport"`
	PathMap            []mediaservers.PathMapping `json:"pathMap"`
	MachineIdentifier  string                     `json:"machineIdentifier"` // Plex machineIdentifier / Jellyfin or Emby server id, from the last good test
	LastError          string                     `json:"lastError"`
	LastCheckedAt      string                     `json:"lastCheckedAt,omitempty"` // RFC 3339
}

func toMediaServerPayload(m mediaservers.Server) mediaServerPayload {
	p := mediaServerPayload{
		ID: m.ID, Name: m.Name, Kind: m.Kind, BaseURL: m.BaseURL, PublicURL: m.PublicURL, WebURL: m.WebURL(),
		HasToken: m.Token != "", Enabled: m.Enabled, RefreshAfterImport: m.RefreshAfterImport,
		PathMap: m.PathMap, MachineIdentifier: m.ServerID, LastError: m.LastError,
	}
	if p.PathMap == nil {
		p.PathMap = []mediaservers.PathMapping{}
	}
	if !m.LastCheckedAt.IsZero() {
		p.LastCheckedAt = m.LastCheckedAt.UTC().Format(time.RFC3339)
	}
	return p
}

// buildMediaServer applies req on top of base (a saved server, or the
// defaults for a new one) and checks the result.
func buildMediaServer(req mediaServerRequest, base mediaservers.Server) (mediaservers.Server, error) {
	out := base
	if strings.TrimSpace(req.Kind) != "" {
		kind, ok := mediaservers.ParseKind(req.Kind)
		if !ok {
			return out, fmt.Errorf("Unknown media server type %q. Use Plex, Jellyfin, Emby, Audiobookshelf or Kavita.", req.Kind)
		}
		out.Kind = kind
	}
	if out.Kind == "" {
		return out, errors.New("Choose the server type: Plex, Jellyfin or Emby.")
	}
	if strings.TrimSpace(req.BaseURL) != "" || out.BaseURL == "" {
		const example = "http://192.168.1.10:32400"
		if msg := firstProblem(
			checkRequired(req.BaseURL, "Add the address of your server, for example "+example+"."),
			checkHTTPURL(req.BaseURL, example, false),
		); msg != "" {
			return out, errors.New(msg)
		}
		u, err := mediaservers.NormalizeURL(req.BaseURL)
		if err != nil {
			return out, errors.New("That doesn't look like the address of a media server. It should look like " + example + ".")
		}
		out.BaseURL = u
	}
	if req.PublicURL != nil {
		out.PublicURL = ""
		if strings.TrimSpace(*req.PublicURL) != "" {
			if msg := checkHTTPURL(*req.PublicURL, "https://plex.example.com", false); msg != "" {
				return out, errors.New(msg)
			}
			u, err := mediaservers.NormalizeURL(*req.PublicURL)
			if err != nil {
				return out, errors.New("That doesn't look like a web address. The public address should look like https://plex.example.com.")
			}
			out.PublicURL = u
		}
	}
	if msg := firstProblem(checkAPIKey(req.token()), checkMaxLen(req.Name, "Name", nameMax), checkNoControl(req.Name, "Name")); msg != "" {
		return out, errors.New(msg)
	}
	if t := req.token(); t != "" {
		out.Token = t
	} else if base.Token != "" && base.BaseURL != "" && addrMoved(base.BaseURL, out.BaseURL) {
		return out, errors.New(retypeSecretMessage("token"))
	}
	if req.Enabled != nil {
		out.Enabled = *req.Enabled
	}
	if req.RefreshAfterImport != nil {
		out.RefreshAfterImport = *req.RefreshAfterImport
	}
	if req.PathMap != nil {
		pm, err := mediaservers.NormalizePathMap(req.PathMap)
		if err != nil {
			return out, errors.New("Each folder mapping needs a folder in Mediarium and the same folder as your media server sees it.")
		}
		// Folders an older version saved can stay; anything new must be a full path.
		had := map[mediaservers.PathMapping]bool{}
		for _, m := range base.PathMap {
			had[m] = true
		}
		for _, m := range pm {
			if had[m] {
				continue
			}
			if msg := firstProblem(checkAbsPath(m.From, "/media/movies"), checkAbsPath(m.To, "/data/movies")); msg != "" {
				return out, errors.New(msg)
			}
		}
		out.PathMap = pm
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		out.Name = name
	}
	if out.Name == "" {
		out.Name = out.Kind.Label()
	}
	return out, nil
}

func mediaServerID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid media server ID.")
		return 0, false
	}
	return id, true
}

// loadMediaServer fetches a saved server, answering 404/500 itself.
func (s *Server) loadMediaServer(w http.ResponseWriter, id int64) (mediaservers.Server, bool) {
	m, err := s.MediaServers.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That media server no longer exists.")
		return m, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return m, false
	}
	return m, true
}

// handleListMediaServers lists the Plex, Jellyfin and Emby servers (tokens are never included).
func (s *Server) handleListMediaServers(w http.ResponseWriter, r *http.Request) {
	list, err := s.MediaServers.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]mediaServerPayload, len(list))
	for i, m := range list {
		out[i] = toMediaServerPayload(m)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateMediaServer adds a media server; refreshAfterImport and enabled default to on.
func (s *Server) handleCreateMediaServer(w http.ResponseWriter, r *http.Request) {
	var req mediaServerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	m, err := buildMediaServer(req, mediaservers.Server{Enabled: true, RefreshAfterImport: true})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.MediaServers.Create(m)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.mediaFinder.Invalidate()
	writeJSON(w, http.StatusCreated, toMediaServerPayload(created))
}

// handleUpdateMediaServer edits a saved server; a blank token keeps the saved one.
func (s *Server) handleUpdateMediaServer(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaServerID(w, r)
	if !ok {
		return
	}
	var req mediaServerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	old, ok := s.loadMediaServer(w, id)
	if !ok {
		return
	}
	m, err := buildMediaServer(req, old)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.MediaServers.Update(m)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.mediaFinder.Invalidate()
	writeJSON(w, http.StatusOK, toMediaServerPayload(updated))
}

// handleDeleteMediaServer removes a saved media server.
func (s *Server) handleDeleteMediaServer(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaServerID(w, r)
	if !ok {
		return
	}
	if err := s.MediaServers.Delete(id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That media server no longer exists.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.mediaFinder.Invalidate()
	writeJSON(w, http.StatusOK, nil)
}

// mediaServerTestResponse is the answer to a test or refresh. A server that
// can't be reached or refuses the token is a 200 with ok:false and a
// message to show as it is.
type mediaServerTestResponse struct {
	OK                bool                   `json:"ok"`
	Error             string                 `json:"error,omitempty"`
	ServerName        string                 `json:"serverName,omitempty"`
	Version           string                 `json:"version,omitempty"`
	MachineIdentifier string                 `json:"machineIdentifier,omitempty"`
	Libraries         []mediaservers.Library `json:"libraries,omitempty"`
}

func mediaServerFailure(err error) mediaServerTestResponse {
	return mediaServerTestResponse{OK: false, Error: plainerror.Message(err)}
}

func (s *Server) runMediaServerTest(ctx context.Context, m mediaservers.Server) mediaServerTestResponse {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := s.mediaClient.Test(ctx, m)
	if err != nil {
		return mediaServerFailure(err)
	}
	return mediaServerTestResponse{
		OK: true, ServerName: res.ServerName, Version: res.Version,
		MachineIdentifier: res.ServerID, Libraries: res.Libraries,
	}
}

// handleTestMediaServerConfig tests the server as typed into the form, without saving it. With an id, blank fields (the token) come from that saved server.
func (s *Server) handleTestMediaServerConfig(w http.ResponseWriter, r *http.Request) {
	var req mediaServerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	base := mediaservers.Server{Enabled: true, RefreshAfterImport: true}
	if req.ID != 0 {
		saved, ok := s.loadMediaServer(w, req.ID)
		if !ok {
			return
		}
		base = saved
		if k, ok := mediaservers.ParseKind(req.Kind); ok && k != saved.Kind {
			base.Token = "" // a token for another kind of server is no use
		}
	}
	m, err := buildMediaServer(req, base)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.runMediaServerTest(r.Context(), m))
}

// handleTestMediaServer tests a saved server and remembers the outcome (shown on the dashboard when it fails).
func (s *Server) handleTestMediaServer(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaServerID(w, r)
	if !ok {
		return
	}
	m, ok := s.loadMediaServer(w, id)
	if !ok {
		return
	}
	res := s.runMediaServerTest(r.Context(), m)
	var checkErr error
	if !res.OK {
		checkErr = errors.New(res.Error)
	}
	if err := s.MediaServers.RecordCheck(id, res.MachineIdentifier, checkErr); err != nil {
		slog.Error("media servers: record test result", "server", m.Name, "err", err)
	}
	if res.OK {
		s.mediaFinder.Invalidate()
	}
	writeJSON(w, http.StatusOK, res)
}

// handleRefreshMediaServer asks a saved server to scan all of its libraries now.
func (s *Server) handleRefreshMediaServer(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaServerID(w, r)
	if !ok {
		return
	}
	m, ok := s.loadMediaServer(w, id)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	err := s.mediaClient.RefreshAll(ctx, m)
	if rerr := s.MediaServers.RecordCheck(id, "", err); rerr != nil {
		slog.Error("media servers: record refresh result", "server", m.Name, "err", rerr)
	}
	if err != nil {
		slog.Warn("media server refresh failed", "server", m.Name, "type", string(m.Kind), "err", err)
		writeJSON(w, http.StatusOK, mediaServerFailure(err))
		return
	}
	slog.Info("media server refresh started", "server", m.Name, "type", string(m.Kind))
	s.mediaFinder.Invalidate()
	writeJSON(w, http.StatusOK, mediaServerTestResponse{OK: true})
}

// handleMediaServerLinks lists where a title can be watched: with tmdbId and kind (movie or tv), the enabled servers that have it; without tmdbId, each enabled server's own home page ("Open my media server").
func (s *Server) handleMediaServerLinks(w http.ResponseWriter, r *http.Request) {
	servers, err := s.MediaServers.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("tmdbId"))
	if raw == "" {
		writeJSON(w, http.StatusOK, mediaservers.HomeLinks(servers))
		return
	}
	tmdbID, err := strconv.Atoi(raw)
	if err != nil || tmdbID <= 0 {
		writeError(w, http.StatusBadRequest, "tmdbId must be a positive number")
		return
	}
	kindParam := r.URL.Query().Get("kind")
	if kindParam == "" {
		kindParam = "movie"
	}
	kind, ok := mediaservers.ParseMediaKind(kindParam)
	if !ok {
		writeError(w, http.StatusBadRequest, "kind must be movie or tv")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, s.mediaFinder.Links(ctx, servers, kind, tmdbID))
}

// mediaServerHealth reports each enabled server whose last test or refresh failed.
func (s *Server) mediaServerHealth() []healthItem {
	if s.MediaServers == nil {
		return nil
	}
	servers, err := s.MediaServers.List()
	if err != nil {
		return nil
	}
	var items []healthItem
	for _, m := range servers {
		if !m.Enabled || m.LastError == "" {
			continue
		}
		impact := "New downloads may not show up there until it rescans on its own, and \"Watch in " + m.Kind.Label() + "\" links can be missing."
		if m.Kind.BookServer() {
			impact = "New books may not show up there until it rescans on its own, and the links to it on book pages can be missing."
		}
		items = append(items, healthItem{
			ID:     fmt.Sprintf("media-server-failed-%d", m.ID),
			Level:  "warn",
			Title:  fmt.Sprintf("%s (%s) isn't working", m.Name, m.Kind.Label()),
			Impact: impact + " The last check said: " + strings.TrimRight(m.LastError, ". ") + ". Test it again once it is fixed.",
			Action: &healthAction{Label: "Check media servers", Path: "/settings/media-servers"},
		})
	}
	return items
}
