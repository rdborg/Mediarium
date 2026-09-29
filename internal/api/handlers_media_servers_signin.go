package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ryanborg/mediarium/internal/mediaservers"
	"github.com/ryanborg/mediarium/internal/settings"
)

// Finding media servers on the network and signing in to them (Sign in with
// Plex, Jellyfin Quick Connect, username and password), instead of typing an
// address and pasting a token. Account tokens and Quick Connect secrets stay
// in memory on the server while a sign-in is in progress; only the finished
// server, with its own token encrypted, is saved, and no token is ever sent
// to the browser.

const (
	plexPinTTL      = 30 * time.Minute // plex.tv's strong PINs last 30 minutes
	quickConnectTTL = 10 * time.Minute // Jellyfin forgets a Quick Connect code after 10 minutes
)

// mediaSignIn is the state of sign-ins in progress. Its zero value is ready;
// Server.signIns sets it up on first use.
type mediaSignIn struct {
	once sync.Once
	idMu sync.Mutex // making the install's client id

	plexTVURL  string                   // plex.tv; tests point it at a fake
	discoverer *mediaservers.Discoverer // tests may replace it

	plexPins *mediaservers.Pending[*plexPinSession]
	quick    *mediaservers.Pending[*quickConnectSession]
}

type plexPinSession struct {
	mu           sync.Mutex
	pinID        int64
	clientID     string
	accountToken string // the Plex account's token; never leaves this process
	servers      []mediaservers.PlexResource
	done         bool
}

type quickConnectSession struct {
	baseURL  string
	clientID string
	code     string
	secret   string // never leaves this process
}

func (s *Server) signIns() *mediaSignIn {
	m := &s.mediaSignIn
	m.once.Do(func() {
		if m.plexTVURL == "" {
			m.plexTVURL = mediaservers.PlexTVURL
		}
		if m.discoverer == nil {
			m.discoverer = &mediaservers.Discoverer{}
		}
		m.plexPins = &mediaservers.Pending[*plexPinSession]{TTL: plexPinTTL}
		m.quick = &mediaservers.Pending[*quickConnectSession]{TTL: quickConnectTTL}
	})
	return m
}

// mediaClientID is this install's device id for Plex, Jellyfin and Emby,
// made on first use and kept in settings.
func (s *Server) mediaClientID() (string, error) {
	m := s.signIns()
	m.idMu.Lock()
	defer m.idMu.Unlock()
	id, err := s.Settings.Get(settings.KeyMediaServersClientID)
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}
	rnd, err := mediaservers.NewID()
	if err != nil {
		return "", err
	}
	id = "mediarium-" + rnd
	if err := s.Settings.Set(settings.KeyMediaServersClientID, id, false); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Server) plexTV(clientID string) *mediaservers.PlexTV {
	return &mediaservers.PlexTV{BaseURL: s.signIns().plexTVURL, ClientID: clientID, Version: s.version}
}

// writeSignInError answers a problem worth showing as it is with 400, and
// anything else with 500 (logged; never with a token in it).
func writeSignInError(w http.ResponseWriter, what string, err error) {
	var ue *mediaservers.UserError
	if errors.As(err, &ue) {
		writeError(w, http.StatusBadRequest, ue.Message)
		return
	}
	slog.Error("media servers: "+what, "err", err)
	writeError(w, http.StatusInternalServerError, "Something went wrong while "+what+". Check the log and try again.")
}

// saveSignedIn saves a server that was just signed in to. When the same
// server (same id or address) is already saved, its address and token are
// replaced instead of adding it twice, so signing in again repairs an
// expired token. created is false then.
func (s *Server) saveSignedIn(ctx context.Context, m mediaservers.Server, tested bool) (mediaServerPayload, bool, error) {
	saved, err := s.MediaServers.List()
	if err != nil {
		return mediaServerPayload{}, false, err
	}
	var out mediaservers.Server
	existing, found := mediaservers.FindSaved(saved, m.Kind, m.ServerID, m.BaseURL)
	if found {
		existing.BaseURL, existing.Token = m.BaseURL, m.Token
		if out, err = s.MediaServers.Update(existing); err != nil {
			return mediaServerPayload{}, false, err
		}
	} else if out, err = s.MediaServers.Create(m); err != nil {
		return mediaServerPayload{}, false, err
	}
	serverID, checkErr := m.ServerID, error(nil)
	if !tested {
		tctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		res, err := s.mediaClient.Test(tctx, out)
		cancel()
		if err != nil {
			checkErr = err
		} else if res.ServerID != "" {
			serverID = res.ServerID
		}
	}
	if err := s.MediaServers.RecordCheck(out.ID, serverID, checkErr); err != nil {
		slog.Error("media servers: record sign-in check", "server", out.Name, "err", err)
	}
	if fresh, err := s.MediaServers.Get(out.ID); err == nil {
		out = fresh
	}
	s.mediaFinder.Invalidate()
	slog.Info("media server signed in", "server", out.Name, "type", string(out.Kind), "updated", found)
	return toMediaServerPayload(out), !found, nil
}

func writeSaved(w http.ResponseWriter, p mediaServerPayload, created bool) {
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, p)
}

// decodeOptionalJSON is decodeJSON that also accepts an empty body.
func decodeOptionalJSON(r *http.Request, v any) error {
	if err := decodeJSON(r, v); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// handleDiscoverMediaServers looks for Plex, Jellyfin and Emby servers on the local network: broadcasts, plus a scan of private networks (subnets, or Mediarium's own networks and common home ones). Public networks are refused.
func (s *Server) handleDiscoverMediaServers(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Subnets []string `json:"subnets"`
	}
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	d := s.signIns().discoverer
	nets, err := d.ParseSubnets(req.Subnets)
	if err != nil {
		writeSignInError(w, "checking the networks to search", err)
		return
	}
	res := d.Discover(r.Context(), nets)
	saved, err := s.MediaServers.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	mediaservers.MarkAdded(res.Found, saved)
	slog.Info("media server discovery", "found", len(res.Found), "networks", strings.Join(res.Scanned, " "))
	writeJSON(w, http.StatusOK, res)
}

// handleStartPlexSignIn starts Sign in with Plex: it answers the PIN code and the app.plex.tv address where the person approves it.
func (s *Server) handleStartPlexSignIn(w http.ResponseWriter, r *http.Request) {
	clientID, err := s.mediaClientID()
	if err != nil {
		writeSignInError(w, "starting Sign in with Plex", err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	pin, err := s.plexTV(clientID).CreatePIN(ctx)
	if err != nil {
		writeSignInError(w, "starting Sign in with Plex", err)
		return
	}
	s.signIns().plexPins.Put(strconv.FormatInt(pin.ID, 10), &plexPinSession{pinID: pin.ID, clientID: clientID})
	expires := pin.ExpiresIn
	if expires <= 0 || expires > int(plexPinTTL.Seconds()) {
		expires = int(plexPinTTL.Seconds())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"pinId": pin.ID, "code": pin.Code, "authUrl": mediaservers.PlexAuthURL(clientID, pin.Code), "expiresIn": expires,
	})
}

// plexServerChoice is one server on the Plex account, without its token.
type plexServerChoice struct {
	Name              string                     `json:"name"`
	MachineIdentifier string                     `json:"machineIdentifier"`
	Version           string                     `json:"version,omitempty"`
	Owned             bool                       `json:"owned"`
	AlreadyAdded      bool                       `json:"alreadyAdded"`
	Connections       []mediaservers.PlexAddress `json:"connections"`
}

func (s *Server) loadPlexPin(w http.ResponseWriter, r *http.Request) (*plexPinSession, bool) {
	sess, ok := s.signIns().plexPins.Get(r.PathValue("pinId"))
	if !ok {
		writeError(w, http.StatusNotFound, "This Plex sign-in has expired or was not started here. Start again.")
		return nil, false
	}
	return sess, true
}

// handlePollPlexSignIn reports whether the Plex PIN was approved yet; once it is, it lists the account's servers (never their tokens).
func (s *Server) handlePollPlexSignIn(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.loadPlexPin(w, r)
	if !ok {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if !sess.done {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		tv := s.plexTV(sess.clientID)
		pin, err := tv.CheckPIN(ctx, sess.pinID)
		if errors.Is(err, mediaservers.ErrPlexPINExpired) {
			s.signIns().plexPins.Delete(r.PathValue("pinId"))
			writeJSON(w, http.StatusOK, map[string]any{"done": false, "expired": true, "error": "The Plex sign-in expired. Start again."})
			return
		}
		if err != nil {
			writeSignInError(w, "checking the Plex sign-in", err)
			return
		}
		if pin.AuthToken == "" {
			writeJSON(w, http.StatusOK, map[string]any{"done": false})
			return
		}
		servers, err := tv.Servers(ctx, pin.AuthToken)
		if err != nil {
			writeSignInError(w, "listing your Plex servers", err)
			return
		}
		sess.accountToken, sess.servers, sess.done = pin.AuthToken, servers, true
		slog.Info("signed in to Plex", "servers", len(servers))
	}
	saved, err := s.MediaServers.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []plexServerChoice{}
	for _, res := range sess.servers {
		_, added := mediaservers.FindSaved(saved, mediaservers.KindPlex, res.ClientIdentifier, "")
		conns := res.Addresses()
		if conns == nil {
			conns = []mediaservers.PlexAddress{}
		}
		out = append(out, plexServerChoice{
			Name: res.Name, MachineIdentifier: res.ClientIdentifier, Version: res.ProductVersion,
			Owned: res.Owned, AlreadyAdded: added, Connections: conns,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"done": true, "servers": out})
}

// handleAddPlexServer adds a server from the signed-in Plex account with its own access token, using uri or, if that doesn't answer, the first of its addresses that does (local ones first).
func (s *Server) handleAddPlexServer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MachineIdentifier string `json:"machineIdentifier"`
		URI               string `json:"uri"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	sess, ok := s.loadPlexPin(w, r)
	if !ok {
		return
	}
	sess.mu.Lock()
	done, servers := sess.done, sess.servers
	sess.mu.Unlock()
	if !done {
		writeError(w, http.StatusConflict, "Finish signing in to Plex first.")
		return
	}
	var res *mediaservers.PlexResource
	for i := range servers {
		if servers[i].ClientIdentifier == strings.TrimSpace(req.MachineIdentifier) {
			res = &servers[i]
			break
		}
	}
	if res == nil {
		writeError(w, http.StatusNotFound, "That server is not on your Plex account.")
		return
	}
	if strings.TrimSpace(req.URI) != "" && !res.HasAddress(req.URI) {
		writeError(w, http.StatusBadRequest, "That is not one of this server's addresses.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	m, _, err := s.mediaClient.ConnectPlex(ctx, *res, req.URI)
	if err != nil {
		writeSignInError(w, "adding the Plex server", err)
		return
	}
	p, created, err := s.saveSignedIn(ctx, m, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSaved(w, p, created)
}

// handleStartQuickConnect starts Jellyfin Quick Connect at baseUrl and answers the code to enter in a Jellyfin app, and an id to poll with.
func (s *Server) handleStartQuickConnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BaseURL string `json:"baseUrl"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	clientID, err := s.mediaClientID()
	if err != nil {
		writeSignInError(w, "starting Quick Connect", err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	base, code, secret, err := s.mediaClient.QuickConnectStart(ctx, req.BaseURL, clientID)
	if err != nil {
		writeSignInError(w, "starting Quick Connect", err)
		return
	}
	id, err := mediaservers.NewID()
	if err != nil {
		writeSignInError(w, "starting Quick Connect", err)
		return
	}
	s.signIns().quick.Put(id, &quickConnectSession{baseURL: base, clientID: clientID, code: code, secret: secret})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "code": code, "expiresIn": int(quickConnectTTL.Seconds())})
}

// handlePollQuickConnect reports whether the Quick Connect code was approved; once it is, the Jellyfin server is saved with the access token it gave and returned as server.
func (s *Server) handlePollQuickConnect(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pending := s.signIns().quick
	sess, ok := pending.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "This Quick Connect sign-in has expired or was not started here. Start again.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	approved, err := s.mediaClient.QuickConnectCheck(ctx, sess.baseURL, sess.clientID, sess.secret)
	if errors.Is(err, mediaservers.ErrQuickConnectExpired) {
		pending.Delete(id)
		writeJSON(w, http.StatusOK, map[string]any{"done": false, "expired": true, "error": "The Quick Connect code expired. Start again."})
		return
	}
	if err != nil {
		writeSignInError(w, "checking Quick Connect", err)
		return
	}
	if !approved {
		writeJSON(w, http.StatusOK, map[string]any{"done": false, "code": sess.code})
		return
	}
	if _, mine := pending.Take(id); !mine {
		// Another poll got there first and is saving the server.
		writeJSON(w, http.StatusOK, map[string]any{"done": false, "code": sess.code})
		return
	}
	m, err := s.mediaClient.QuickConnectFinish(ctx, sess.baseURL, sess.clientID, sess.secret)
	if err != nil {
		writeSignInError(w, "finishing Quick Connect", err)
		return
	}
	p, _, err := s.saveSignedIn(ctx, m, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"done": true, "server": p})
}

// handleMediaServerLogin signs in to Jellyfin or Emby with a username and password and saves the server with the access token it gives; the password is never stored.
func (s *Server) handleMediaServerLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind     string `json:"kind"`
		BaseURL  string `json:"baseUrl"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	kind, ok := mediaservers.ParseKind(req.Kind)
	if !ok {
		writeError(w, http.StatusBadRequest, "Choose the server type: jellyfin or emby.")
		return
	}
	clientID, err := s.mediaClientID()
	if err != nil {
		writeSignInError(w, "signing in", err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	m, err := s.mediaClient.SignIn(ctx, kind, req.BaseURL, clientID, req.Username, req.Password)
	if err != nil {
		writeSignInError(w, "signing in", err)
		return
	}
	p, created, err := s.saveSignedIn(ctx, m, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSaved(w, p, created)
}
