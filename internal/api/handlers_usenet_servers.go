package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ryanborg/mediarium/internal/download"
)

// A "Usenet server" is the news-server account from a Usenet provider
// (Newshosting, Eweka, ...). Mediarium's built-in downloader connects to it
// directly — there is no separate download client program to set up, the
// way Sonarr/Radarr need SABnzbd. Torrents need nothing at all: the
// built-in torrent client has no server to configure.
type usenetServerPayload struct {
	ID          int64  `json:"id,omitempty"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	UseSSL      bool   `json:"useSsl"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"` // request only; never returned
	HasPassword bool   `json:"hasPassword"`
	Connections int    `json:"connections"`
	Priority    int    `json:"priority"`
	Enabled     bool   `json:"enabled"`
}

const (
	maxServerConnections = 100
	maxServerPriority    = 99
)

func toUsenetServerPayload(sc download.StoredClient) usenetServerPayload {
	return usenetServerPayload{
		ID: sc.ID, Name: sc.Name, Host: sc.Config.Host, Port: sc.Config.Port, UseSSL: sc.Config.UseSSL,
		Username: sc.Config.Username, HasPassword: sc.Config.Password != "", Connections: sc.Config.Connections,
		Priority: sc.Priority, Enabled: sc.Enabled,
	}
}

// validate normalises a request into a StoredClient, or returns a message
// for the user.
func (req usenetServerPayload) validate() (download.StoredClient, string) {
	req.Host = strings.TrimSpace(req.Host)
	req.Name = strings.TrimSpace(req.Name)
	if req.Host == "" || req.Port <= 0 || req.Port > 65535 {
		return download.StoredClient{}, "a host and a valid port are required"
	}
	if strings.Contains(req.Host, "://") || strings.ContainsAny(req.Host, "/ ") {
		return download.StoredClient{}, "enter just the server's host name (for example news.example.com), without http:// or a path"
	}
	if req.Name == "" {
		req.Name = req.Host
	}
	if req.Connections <= 0 {
		req.Connections = 8
	}
	if req.Connections > maxServerConnections {
		return download.StoredClient{}, "connections must be between 1 and " + strconv.Itoa(maxServerConnections)
	}
	if req.Priority < 0 || req.Priority > maxServerPriority {
		return download.StoredClient{}, "priority must be between 0 (primary) and " + strconv.Itoa(maxServerPriority)
	}
	return download.StoredClient{
		ID: req.ID, Name: req.Name, Priority: req.Priority, Enabled: req.Enabled,
		Config: download.ClientConfig{
			Host: req.Host, Port: req.Port, UseSSL: req.UseSSL, Username: req.Username, Password: req.Password, Connections: req.Connections,
		},
	}, ""
}

func (s *Server) handleListUsenetServers(w http.ResponseWriter, r *http.Request) {
	list, err := s.ClientRepo.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]usenetServerPayload, len(list))
	for i, sc := range list {
		out[i] = toUsenetServerPayload(sc)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateUsenetServer(w http.ResponseWriter, r *http.Request) {
	var req usenetServerPayload
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	sc, msg := req.validate()
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	created, err := s.ClientRepo.Create(sc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toUsenetServerPayload(created))
}

// handleUpdateUsenetServer edits a server. Fields left out of the request
// keep their saved values, and a blank or missing password keeps the stored
// one, so an edit form can send only what changed.
func (s *Server) handleUpdateUsenetServer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	stored, err := s.ClientRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Decoding over the saved values overwrites only the fields sent.
	req := toUsenetServerPayload(stored)
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ID = id
	sc, msg := req.validate()
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.ClientRepo.Update(sc); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "server not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, err := s.ClientRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toUsenetServerPayload(updated))
}

func (s *Server) handleDeleteUsenetServer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	if err := s.ClientRepo.Delete(id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "server not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

// handleTestUsenetServerConfig tests the server as typed into the form. If
// the request names an existing server (id) and leaves the password blank,
// the stored password is used, so a saved server can be re-tested without
// retyping it.
func (s *Server) handleTestUsenetServerConfig(w http.ResponseWriter, r *http.Request) {
	var req usenetServerPayload
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	password := req.Password
	if password == "" && req.ID != 0 {
		if stored, err := s.ClientRepo.Get(req.ID); err == nil {
			password = stored.Config.Password
		}
	}
	writeJSON(w, http.StatusOK, testNNTP(strings.TrimSpace(req.Host), req.Port, req.UseSSL, req.Username, password))
}

// handleTestUsenetServer tests a saved server with its stored login.
func (s *Server) handleTestUsenetServer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	sc, err := s.ClientRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, testNNTP(sc.Config.Host, sc.Config.Port, sc.Config.UseSSL, sc.Config.Username, sc.Config.Password))
}

type downloadsStatusPayload struct {
	Usenet struct {
		Servers        int  `json:"servers"`
		EnabledServers int  `json:"enabledServers"`
		Ready          bool `json:"ready"`
	} `json:"usenet"`
	Torrent struct {
		// State is "disabled" (switched off in Settings), "blocked" (on, but the
		// VPN kill switch is holding it back) or "ready".
		State        string `json:"state"`
		Enabled      bool   `json:"enabled"`
		Ready        bool   `json:"ready"`
		ListenPort   int    `json:"listenPort"`
		VPNRequired  bool   `json:"vpnRequired"`
		VPNConnected bool   `json:"vpnConnected"`
		BlockedByVPN bool   `json:"blockedByVpn"`
		// Incoming connections. Listening is true while the engine runs
		// (only while a torrent is downloading or seeding) and has a host port
		// open; ActivePort is that port (it differs from ListenPort only when
		// the chosen port was taken by another program). IncomingSeen is true
		// once another peer has connected to us since the app started, which
		// proves the port is reachable from the internet; LastIncomingAt says
		// when (RFC 3339). With the VPN kill switch on nothing listens, so no
		// incoming connection is ever seen.
		Listening      bool   `json:"listening"`
		ActivePort     int    `json:"activePort,omitempty"`
		ActiveTorrents int    `json:"activeTorrents"`
		IncomingSeen   bool   `json:"incomingSeen"`
		LastIncomingAt string `json:"lastIncomingAt,omitempty"`
	} `json:"torrent"`
}

// handleDownloadsStatus summarises the two built-in downloaders for the
// Downloads settings page: Usenet is ready once at least one server is
// enabled; the torrent client is always ready unless the VPN kill switch is
// on and no VPN is connected.
func (s *Server) handleDownloadsStatus(w http.ResponseWriter, r *http.Request) {
	all, err := s.ClientRepo.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var out downloadsStatusPayload
	out.Usenet.Servers = len(all)
	for _, sc := range all {
		if sc.Enabled {
			out.Usenet.EnabledServers++
		}
	}
	out.Usenet.Ready = out.Usenet.EnabledServers > 0

	out.Torrent.ListenPort = s.torrentListenPort()
	engine := s.torrents.status()
	out.Torrent.Listening, out.Torrent.ActivePort, out.Torrent.ActiveTorrents = engine.Listening, engine.Port, engine.Torrents
	if !engine.LastIncoming.IsZero() {
		out.Torrent.IncomingSeen = true
		out.Torrent.LastIncomingAt = engine.LastIncoming.UTC().Format(time.RFC3339)
	}
	out.Torrent.VPNRequired = s.vpnRequiredForTorrents()
	out.Torrent.VPNConnected = s.VPNManager.Status().Connected
	out.Torrent.BlockedByVPN = out.Torrent.VPNRequired && !out.Torrent.VPNConnected
	out.Torrent.Enabled = s.torrentsEnabled()
	out.Torrent.Ready = out.Torrent.Enabled && !out.Torrent.BlockedByVPN
	switch {
	case !out.Torrent.Enabled:
		out.Torrent.State = "disabled"
		// A switched-off client has no VPN story to tell.
		out.Torrent.VPNRequired, out.Torrent.BlockedByVPN = false, false
	case out.Torrent.BlockedByVPN:
		out.Torrent.State = "blocked"
	default:
		out.Torrent.State = "ready"
	}
	writeJSON(w, http.StatusOK, out)
}
