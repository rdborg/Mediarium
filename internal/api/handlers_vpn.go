package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/vpn"
)

type vpnConfigPayload struct {
	ID             int64    `json:"id,omitempty"`
	Label          string   `json:"label"`
	Provider       string   `json:"provider"`
	PrivateKey     string   `json:"privateKey,omitempty"`
	PeerPublicKey  string   `json:"peerPublicKey"`
	PresharedKey   string   `json:"presharedKey,omitempty"`
	Endpoint       string   `json:"endpoint"`
	AllowedIPs     []string `json:"allowedIps,omitempty"`
	LocalAddresses []string `json:"localAddresses"`
	DNS            []string `json:"dns,omitempty"`
	Active         bool     `json:"active"`
}

func (s *Server) handleListVPNConfigs(w http.ResponseWriter, r *http.Request) {
	list, err := s.VPNRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]vpnConfigPayload, len(list))
	for i, c := range list {
		// Private/preshared keys intentionally omitted from the response.
		out[i] = vpnConfigPayload{
			ID: c.ID, Label: c.Label, Provider: c.Provider, PeerPublicKey: c.Config.PeerPublicKey,
			Endpoint: c.Config.Endpoint, AllowedIPs: c.Config.AllowedIPs, LocalAddresses: c.Config.LocalAddresses,
			DNS: c.Config.DNS, Active: c.Active,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateVPNConfig saves a WireGuard config that was pasted in. Any
// provider's standard config works.
func (s *Server) handleCreateVPNConfig(w http.ResponseWriter, r *http.Request) {
	var req vpnConfigPayload
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	req = cleanVPNConfig(req)
	if rejectBad(w, checkVPNConfig(req)) {
		return
	}
	provider := req.Provider
	if provider == "" {
		provider = "custom"
	}
	created, err := s.VPNRepo.Create(req.Label, provider, vpn.Config{
		PrivateKey: req.PrivateKey, PeerPublicKey: req.PeerPublicKey, PresharedKey: req.PresharedKey,
		Endpoint: req.Endpoint, AllowedIPs: req.AllowedIPs, LocalAddresses: req.LocalAddresses, DNS: req.DNS,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, vpnConfigPayload{
		ID: created.ID, Label: created.Label, Provider: created.Provider, PeerPublicKey: created.Config.PeerPublicKey,
		Endpoint: created.Config.Endpoint, LocalAddresses: created.Config.LocalAddresses,
	})
}

func (s *Server) handleDeleteVPNConfig(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid VPN config ID.")
		return
	}
	// Removing the connection that is switched on must also switch it off, or
	// the tunnel would keep running for a connection the list no longer shows.
	if active, ok, err := s.VPNRepo.Active(); err == nil && ok && active.ID == id {
		s.VPNManager.Deactivate()
	}
	if err := s.VPNRepo.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

// handleActivateVPNConfig brings the tunnel up for this config. Several
// configs can be stored, but only one is active at a time. Connecting a
// real WireGuard tunnel involves a network handshake to whatever endpoint
// the config points at, so this can legitimately fail (bad key, endpoint
// unreachable) — that's surfaced as a 502, not swallowed.
func (s *Server) handleActivateVPNConfig(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid VPN config ID.")
		return
	}
	list, err := s.VPNRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var target *vpn.StoredConfig
	for i := range list {
		if list[i].ID == id {
			target = &list[i]
			break
		}
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "That VPN config no longer exists.")
		return
	}

	if err := s.VPNManager.Activate(target.Label, target.Config); err != nil {
		slog.Warn("vpn: couldn't switch a connection on", "label", target.Label, "err", err)
		// Switching on tore the old tunnel down, so nothing is running now.
		_ = s.VPNRepo.Deactivate()
		writeError(w, http.StatusBadGateway, vpn.Explain(err))
		return
	}
	if err := s.VPNRepo.SetActive(id); err != nil {
		s.VPNManager.Deactivate()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Give the VPN server a moment to answer, so the page can show the real
	// state straight away. If it doesn't answer, the status says so.
	s.VPNManager.WaitConnected(r.Context(), vpnConnectWait)
	writeJSON(w, http.StatusOK, nil)
}

// vpnConnectWait is how long switching a connection on waits for the VPN
// server to answer before it replies.
const vpnConnectWait = 6 * time.Second

// restoreVPN brings back the connection that was switched on before the
// restart. It is started in the background and retried, because the network
// is often not ready yet when the app starts. Until it works, the status says
// so, and torrents wait (see torrentTunnel).
func (s *Server) restoreVPN() {
	active, ok, err := s.VPNRepo.Active()
	if err != nil {
		slog.Warn("vpn: couldn't read the saved connection", "err", err)
		s.VPNManager.Broken("VPN", "Couldn't read the saved connection. Add it again.")
		return
	}
	if !ok {
		return
	}
	s.VPNManager.Restore(active.Label, active.Config)
}

func (s *Server) handleDeactivateVPN(w http.ResponseWriter, r *http.Request) {
	s.VPNManager.Deactivate()
	if err := s.VPNRepo.Deactivate(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

type vpnStatusPayload struct {
	// Connected is true only when the tunnel is up and the VPN server is
	// answering. State is "off", "connecting", "connected" or "down"; Label is
	// the connection that is switched on, even while it isn't working, and
	// Reason says why it isn't.
	Connected   bool      `json:"connected"`
	State       string    `json:"state"`
	Label       string    `json:"label,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	ConnectedAt time.Time `json:"connectedAt,omitzero"`
}

// handleVPNStatus surfaces connection state for the UI, so VPN status is
// shown clearly in the UI, not just in logs.
func (s *Server) handleVPNStatus(w http.ResponseWriter, r *http.Request) {
	status := s.VPNManager.Status()
	writeJSON(w, http.StatusOK, vpnStatusPayload{
		Connected: status.Connected, State: string(status.State), Label: status.Label,
		Reason: status.Reason, ConnectedAt: status.ConnectedAt,
	})
}

// handleVPNEgressIP makes a real request through the active tunnel, so a
// person can check for themselves that traffic leaves through the VPN
// provider and is not leaking. Only fired when this endpoint is actually hit, not polled in
// the background, since it means a live network round trip each time.
func (s *Server) handleVPNEgressIP(w http.ResponseWriter, r *http.Request) {
	tunnel := s.VPNManager.Tunnel()
	if tunnel == nil {
		writeError(w, http.StatusConflict, "No VPN is connected.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	ip, err := tunnel.PublicIP(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Couldn't work out the VPN's outgoing IP address: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ip": ip})
}

// Limits for a saved VPN connection. A real WireGuard config has a handful of
// addresses; these only stop a runaway request.
const (
	vpnLabelMax     = 60
	vpnProviderMax  = 40
	vpnMaxAddresses = 8
	vpnMaxAllowed   = 64
	vpnMaxDNS       = 8
)

// cleanVPNConfig trims the pasted values and drops blank list entries, so a
// trailing comma or an empty line in the form doesn't count as a mistake.
func cleanVPNConfig(req vpnConfigPayload) vpnConfigPayload {
	req.Label = strings.TrimSpace(req.Label)
	req.Provider = strings.TrimSpace(req.Provider)
	req.PrivateKey = strings.TrimSpace(req.PrivateKey)
	req.PeerPublicKey = strings.TrimSpace(req.PeerPublicKey)
	req.PresharedKey = strings.TrimSpace(req.PresharedKey)
	req.Endpoint = strings.TrimSpace(req.Endpoint)
	req.AllowedIPs = trimNonEmpty(req.AllowedIPs)
	req.LocalAddresses = trimNonEmpty(req.LocalAddresses)
	req.DNS = trimNonEmpty(req.DNS)
	return req
}

func trimNonEmpty(in []string) []string {
	var out []string
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// checkVPNConfig says what is wrong with a new WireGuard connection, or "".
// The tunnel needs valid keys and a reachable endpoint, so a value that could
// never work is refused here instead of failing later when it is switched on.
func checkVPNConfig(req vpnConfigPayload) string {
	return firstProblem(
		checkRequired(req.Label, "Give this connection a name, for example My VPN."),
		checkMaxLen(req.Label, "The name", vpnLabelMax),
		checkNoControl(req.Label, "The name"),
		checkMaxLen(req.Provider, "The provider", vpnProviderMax),
		checkNoControl(req.Provider, "The provider"),
		checkRequired(req.PrivateKey, "Paste the private key from your provider's WireGuard config."),
		checkWireGuardKey(req.PrivateKey, "The private key"),
		checkRequired(req.PeerPublicKey, "Paste the server's public key from your provider's WireGuard config."),
		checkWireGuardKey(req.PeerPublicKey, "The server's public key"),
		checkWireGuardKey(req.PresharedKey, "The preshared key"),
		checkRequired(req.Endpoint, "Add the server address and port from your provider's config, for example vpn.example.com:51820."),
		checkHostPort(req.Endpoint, "vpn.example.com:51820"),
		checkRequired(strings.Join(req.LocalAddresses, ""), "Add the address your VPN gave this device, for example 10.2.0.2/32."),
		checkList(req.LocalAddresses, "Your addresses", vpnMaxAddresses, func(v string) string { return checkIPAddress(v, "10.2.0.2/32", true) }),
		checkList(req.AllowedIPs, "Allowed IPs", vpnMaxAllowed, func(v string) string { return checkCIDR(v, "0.0.0.0/0") }),
		checkList(req.DNS, "DNS servers", vpnMaxDNS, func(v string) string { return checkIPAddress(v, "10.64.0.1", false) }),
	)
}
