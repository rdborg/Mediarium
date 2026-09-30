package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/download"
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
	// defaultServerConnections is what a new server gets. Deliberately modest:
	// a login that opens more connections than its plan allows is refused, and
	// so is every other program using the same login.
	defaultServerConnections = 10
	maxServerConnections     = 100
	maxServerPriority        = 99
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
func (req usenetServerPayload) validate(stored *download.StoredClient) (download.StoredClient, string) {
	req.Host = strings.TrimSpace(req.Host)
	req.Name = strings.TrimSpace(req.Name)
	// A host or port an older version saved is left alone when it is not being changed.
	hostProblem, portProblem := checkHost(req.Host, "news.example.com"), checkPort(req.Port, "Port")
	if stored != nil && req.Host == stored.Config.Host {
		hostProblem = ""
	}
	if stored != nil && req.Port == stored.Config.Port {
		portProblem = ""
	}
	if msg := firstProblem(
		checkRequired(req.Host, "Add the host name of your Usenet server, for example news.example.com."),
		hostProblem,
		portProblem,
		checkMaxLen(req.Name, "Name", nameMax),
		checkNoControl(req.Name, "Name"),
		checkMaxLen(req.Username, "Username", 200),
		checkNoControl(req.Username, "Username"),
		checkMaxLen(req.Password, "Password", 200),
		checkNoControl(req.Password, "Password"),
	); msg != "" {
		return download.StoredClient{}, msg
	}
	if req.Name == "" {
		req.Name = req.Host
	}
	if req.Connections == 0 {
		req.Connections = defaultServerConnections
	}
	if msg := firstProblem(
		checkIntRange(req.Connections, "Connections", 1, maxServerConnections),
		checkIntRange(req.Priority, "Priority", 0, maxServerPriority),
	); msg != "" {
		return download.StoredClient{}, msg
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
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	sc, msg := req.validate(nil)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	created, err := s.ClientRepo.Create(sc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	download.ConfigureServer(created.Config)
	writeJSON(w, http.StatusCreated, toUsenetServerPayload(created))
}

// handleUpdateUsenetServer edits a server. Fields left out of the request
// keep their saved values, and a blank or missing password keeps the stored
// one, so an edit form can send only what changed.
func (s *Server) handleUpdateUsenetServer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid server ID.")
		return
	}
	stored, err := s.ClientRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That server no longer exists.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Decoding over the saved values overwrites only the fields sent.
	req := toUsenetServerPayload(stored)
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	req.ID = id
	if req.Password == "" && stored.Config.Password != "" && addrMoved(stored.Config.Host, req.Host) {
		writeError(w, http.StatusBadRequest, retypeSecretMessage("password"))
		return
	}
	sc, msg := req.validate(&stored)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.ClientRepo.Update(sc); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That server no longer exists.")
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
	// Downloads running now: a server that moved to another address or login,
	// or was switched off, is let go (its connections close); a new connection
	// count takes effect at once.
	if !updated.Enabled || serverMoved(stored.Config, updated.Config) {
		download.RetireServer(stored.Config)
	}
	if updated.Enabled {
		download.ConfigureServer(updated.Config)
	}
	writeJSON(w, http.StatusOK, toUsenetServerPayload(updated))
}

func (s *Server) handleDeleteUsenetServer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid server ID.")
		return
	}
	stored, err := s.ClientRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That server no longer exists.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.ClientRepo.Delete(id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That server no longer exists.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Close its connections and stop downloads that are running from using it.
	download.RetireServer(stored.Config)
	writeJSON(w, http.StatusOK, nil)
}

// serverMoved reports whether a server now points somewhere else: a different
// address, port, encryption setting or login than before.
func serverMoved(before, after download.ClientConfig) bool {
	return !strings.EqualFold(before.Host, after.Host) || before.Port != after.Port ||
		before.UseSSL != after.UseSSL || before.Username != after.Username
}

// handleTestUsenetServerConfig tests the server as typed into the form. If
// the request names an existing server (id) and leaves the password blank,
// the stored password is used, so a saved server can be re-tested without
// retyping it.
func (s *Server) handleTestUsenetServerConfig(w http.ResponseWriter, r *http.Request) {
	var req usenetServerPayload
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if msg := firstProblem(
		checkRequired(req.Host, "Add the host name of your Usenet server, for example news.example.com."),
		checkHost(req.Host, "news.example.com"),
		checkPort(req.Port, "Port"),
	); msg != "" {
		writeJSON(w, http.StatusOK, connTestResult{Message: msg})
		return
	}
	password := req.Password
	if password == "" && req.ID != 0 {
		if stored, err := s.ClientRepo.Get(req.ID); err == nil {
			if addrMoved(stored.Config.Host, req.Host) {
				writeJSON(w, http.StatusOK, connTestResult{Message: retypeSecretMessage("password")})
				return
			}
			password = stored.Config.Password
		}
	}
	writeJSON(w, http.StatusOK, testNNTP(strings.TrimSpace(req.Host), req.Port, req.UseSSL, req.Username, password))
}

// handleTestUsenetServer tests a saved server with its stored login.
func (s *Server) handleTestUsenetServer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid server ID.")
		return
	}
	sc, err := s.ClientRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That server no longer exists.")
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
		// VPN kill switch or a VPN that isn't up is holding it back) or "ready".
		State        string `json:"state"`
		Enabled      bool   `json:"enabled"`
		Ready        bool   `json:"ready"`
		ListenPort   int    `json:"listenPort"`
		VPNRequired  bool   `json:"vpnRequired"`
		VPNConnected bool   `json:"vpnConnected"`
		// VPNState is "off" (no VPN switched on), "connecting", "connected" or
		// "down" (switched on but not working).
		VPNState     string `json:"vpnState"`
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
	vpnStatus := s.VPNManager.Status()
	out.Torrent.VPNConnected = vpnStatus.Connected
	out.Torrent.VPNState = string(vpnStatus.State)
	// Blocked is exactly when a torrent would be refused: the kill switch is
	// on with no VPN, or a VPN is switched on but not up yet.
	_, tunnelErr := s.torrentTunnel()
	out.Torrent.BlockedByVPN = tunnelErr != nil
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
