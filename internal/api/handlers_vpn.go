package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ryanborg/mediarium/internal/vpn"
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
		// Private/preshared keys intentionally omitted from the response (PRD §11).
		out[i] = vpnConfigPayload{
			ID: c.ID, Label: c.Label, Provider: c.Provider, PeerPublicKey: c.Config.PeerPublicKey,
			Endpoint: c.Config.Endpoint, AllowedIPs: c.Config.AllowedIPs, LocalAddresses: c.Config.LocalAddresses,
			DNS: c.Config.DNS, Active: c.Active,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateVPNConfig accepts a generic WireGuard config — the "paste
// your own config" fallback PRD §4.7 requires always works, and is
// exactly the same path a named-provider picker in the UI would use.
func (s *Server) handleCreateVPNConfig(w http.ResponseWriter, r *http.Request) {
	var req vpnConfigPayload
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Label == "" || req.PrivateKey == "" || req.PeerPublicKey == "" || req.Endpoint == "" || len(req.LocalAddresses) == 0 {
		writeError(w, http.StatusBadRequest, "label, privateKey, peerPublicKey, endpoint, and localAddresses are required")
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
		writeError(w, http.StatusBadRequest, "invalid vpn config id")
		return
	}
	if err := s.VPNRepo.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

// handleActivateVPNConfig brings the tunnel up for this config (PRD §4.7 —
// "multiple provider configs stored, one active at a time"). Connecting a
// real WireGuard tunnel involves a network handshake to whatever endpoint
// the config points at, so this can legitimately fail (bad key, endpoint
// unreachable) — that's surfaced as a 502, not swallowed.
func (s *Server) handleActivateVPNConfig(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid vpn config id")
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
		writeError(w, http.StatusNotFound, "vpn config not found")
		return
	}

	if err := s.VPNManager.Activate(target.Label, target.Config); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := s.VPNRepo.SetActive(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
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
	Connected   bool      `json:"connected"`
	Label       string    `json:"label,omitempty"`
	ConnectedAt time.Time `json:"connectedAt,omitempty"`
}

// handleVPNStatus surfaces connection state for the UI (PRD §4.7 —
// "status visibility... shown clearly in the UI, not just in logs").
func (s *Server) handleVPNStatus(w http.ResponseWriter, r *http.Request) {
	status := s.VPNManager.Status()
	writeJSON(w, http.StatusOK, vpnStatusPayload{Connected: status.Connected, Label: status.Label, ConnectedAt: status.ConnectedAt})
}

// handleVPNEgressIP makes a real outbound request through the active
// tunnel (PRD §4.7 — "status visibility"), so the user can independently
// confirm traffic is actually egressing via the VPN provider rather than
// leaking. Only fired when this endpoint is actually hit, not polled in
// the background, since it means a live network round trip each time.
func (s *Server) handleVPNEgressIP(w http.ResponseWriter, r *http.Request) {
	tunnel := s.VPNManager.Tunnel()
	if tunnel == nil {
		writeError(w, http.StatusConflict, "no VPN is connected")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	ip, err := tunnel.PublicIP(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("couldn't determine egress IP: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ip": ip})
}
