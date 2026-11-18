package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/announce"
)

// IRC announce (Settings > Finding releases > Indexers & Search > IRC announce): connections to
// the IRC channels where trackers announce new releases, so a wanted release
// can be grabbed within seconds of being posted instead of at the next RSS
// check. The engine is internal/announce; irc_announce_decide.go decides what
// is wanted. Nothing runs until an administrator adds a connection and
// switches it on.

const maxIRCConnections = 10

// StartAnnounce starts the saved IRC connections that are switched on. In
// safe mode (MEDIARIUM_PAUSE_AUTOMATION) nothing is started. The returned
// function stops them.
func (s *Server) StartAnnounce() (stop func()) {
	if s.cfg.PauseAutomation {
		return func() {}
	}
	if err := s.Announce.Start(context.Background()); err != nil {
		slog.Warn("irc announce: could not start", "error", err)
	}
	return s.Announce.Stop
}

type ircKeyInput struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type ircChannelInput struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// ircConnRequest is the body of create, update and test. On update, a blank
// password, invite command, channel key or tracker key keeps the saved one.
type ircConnRequest struct {
	ID            int64              `json:"id,omitempty"` // test only: a saved connection to take blank fields from
	Name          string             `json:"name"`
	Enabled       *bool              `json:"enabled,omitempty"`
	Preset        *string            `json:"preset,omitempty"`
	Server        string             `json:"server"`
	Port          int                `json:"port"`
	TLS           *bool              `json:"tls,omitempty"`
	InsecureTLS   *bool              `json:"insecureTls,omitempty"`
	Nick          string             `json:"nick"`
	Auth          string             `json:"auth"`
	AuthAccount   *string            `json:"authAccount,omitempty"`
	AuthPassword  string             `json:"authPassword,omitempty"`
	Invite        string             `json:"invite,omitempty"`
	ClearInvite   bool               `json:"clearInvite,omitempty"`
	Channels      []ircChannelInput  `json:"channels"`
	Announcers    []string           `json:"announcers"`
	TrackerURL    string             `json:"trackerUrl"`
	Template      *announce.Template `json:"template,omitempty"`
	Keys          []ircKeyInput      `json:"keys"`
	FreeleechOnly *bool              `json:"freeleechOnly,omitempty"`
	MaxPerHour    *int               `json:"maxPerHour,omitempty"`
	MaxPerDay     *int               `json:"maxPerDay,omitempty"`
}

type ircConnPayload struct {
	ID            int64             `json:"id"`
	Name          string            `json:"name"`
	Enabled       bool              `json:"enabled"`
	Preset        string            `json:"preset"`
	Server        string            `json:"server"`
	Port          int               `json:"port"`
	TLS           bool              `json:"tls"`
	InsecureTLS   bool              `json:"insecureTls"`
	Nick          string            `json:"nick"`
	Auth          string            `json:"auth"`
	AuthAccount   string            `json:"authAccount"`
	HasPassword   bool              `json:"hasPassword"`
	HasInvite     bool              `json:"hasInvite"`
	Channels      []ircChannelOut   `json:"channels"`
	Announcers    []string          `json:"announcers"`
	TrackerURL    string            `json:"trackerUrl"`
	Template      announce.Template `json:"template"`
	Keys          []ircKeyOut       `json:"keys"`
	NeedKeys      []string          `json:"needKeys"`
	FreeleechOnly bool              `json:"freeleechOnly"`
	MaxPerHour    int               `json:"maxPerHour"`
	MaxPerDay     int               `json:"maxPerDay"`
	Status        ircStatusPayload  `json:"status"`
}

type ircChannelOut struct {
	Name   string `json:"name"`
	HasKey bool   `json:"hasKey"`
}

type ircKeyOut struct {
	Name     string `json:"name"`
	HasValue bool   `json:"hasValue"`
}

type ircStatusPayload struct {
	State        string                   `json:"state"` // stopped, connecting, connected, joined
	Channels     []announce.ChannelStatus `json:"channels"`
	LastError    string                   `json:"lastError"`
	ConnectedAt  string                   `json:"connectedAt,omitempty"`
	LastAnnounce string                   `json:"lastAnnounce,omitempty"`
	Reconnects   int                      `json:"reconnects"`
	Heard        int                      `json:"heard"`
	Grabbed      int                      `json:"grabbed"`
	Dropped      int                      `json:"dropped"`
	Problem      string                   `json:"problem"`
}

func (s *Server) ircPayload(c announce.Connection) ircConnPayload {
	p := ircConnPayload{
		ID: c.ID, Name: c.Name, Enabled: c.Enabled, Preset: c.Preset, Server: c.Server, Port: c.Port, TLS: c.TLS, InsecureTLS: c.InsecureTLS,
		Nick: c.Nick, Auth: c.Auth, AuthAccount: c.AuthAccount, HasPassword: c.AuthPass != "", HasInvite: c.Invite != "",
		Announcers: c.Announcers, TrackerURL: c.TrackerURL, Template: c.Template, FreeleechOnly: c.FreeleechOnly,
		MaxPerHour: c.MaxPerHour, MaxPerDay: c.MaxPerDay, Channels: []ircChannelOut{}, Keys: []ircKeyOut{}, NeedKeys: []string{},
	}
	if p.Announcers == nil {
		p.Announcers = []string{}
	}
	if p.Template.Patterns == nil {
		p.Template.Patterns = []string{}
	}
	for _, ch := range c.Channels {
		p.Channels = append(p.Channels, ircChannelOut{Name: ch.Name, HasKey: ch.Key != ""})
	}
	if comp, err := announce.Compile(c.Template); err == nil {
		p.NeedKeys = comp.NeedKeys()
		if p.NeedKeys == nil {
			p.NeedKeys = []string{}
		}
	}
	names := append([]string(nil), p.NeedKeys...)
	for k := range c.Keys {
		found := false
		for _, n := range names {
			found = found || n == k
		}
		if !found {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		p.Keys = append(p.Keys, ircKeyOut{Name: n, HasValue: c.Keys[n] != ""})
	}
	info := s.Announce.InfoFor(c.ID)
	p.Status = ircStatusPayload{
		State: info.State, Channels: info.Channels, LastError: info.LastError, Reconnects: info.Reconnects,
		Heard: info.Heard, Grabbed: info.Grabbed, Dropped: info.Dropped, Problem: info.Problem,
	}
	if !info.ConnectedAt.IsZero() {
		p.Status.ConnectedAt = info.ConnectedAt.UTC().Format(time.RFC3339)
	}
	if !info.LastAnnounce.IsZero() {
		p.Status.LastAnnounce = info.LastAnnounce.UTC().Format(time.RFC3339)
	}
	if p.Status.Channels == nil {
		p.Status.Channels = []announce.ChannelStatus{}
	}
	return p
}

// buildIRCConnection applies req on top of base (the saved connection, or the
// defaults for a new one) and checks the result.
func buildIRCConnection(req ircConnRequest, base announce.Connection) (announce.Connection, error) {
	out := base
	fail := func(msg string) (announce.Connection, error) { return out, errors.New(msg) }

	if name := strings.TrimSpace(req.Name); name != "" || out.Name == "" {
		if msg := firstProblem(checkRequired(name, "Give this connection a name."), checkMaxLen(name, "Name", 60), checkNoControl(name, "Name")); msg != "" {
			return fail(msg)
		}
		out.Name = name
	}
	if req.Enabled != nil {
		out.Enabled = *req.Enabled
	}
	if req.Preset != nil {
		p := strings.TrimSpace(*req.Preset)
		if _, known := announce.PresetByID(p); p != "" && p != "custom" && !known {
			return fail("That template isn't one of the built-in ones.")
		}
		out.Preset = p
	}
	if req.TLS != nil {
		out.TLS = *req.TLS
	}
	if req.InsecureTLS != nil {
		out.InsecureTLS = *req.InsecureTLS
	}
	if !out.TLS {
		out.InsecureTLS = false
	}

	// Server.
	oldServer := base.Server
	server := strings.TrimSpace(req.Server)
	if server != "" || out.Server == "" {
		const example = "irc.example.org"
		if msg := firstProblem(checkRequired(server, "Add the IRC server, for example "+example+"."), checkHost(server, example)); msg != "" {
			return fail(msg)
		}
		host := strings.ToLower(strings.TrimSuffix(strings.Trim(server, "[]"), "."))
		if ip, err := netip.ParseAddr(host); err == nil {
			if !announce.PublicAddr(ip) {
				return fail("IRC servers have to be on the internet. " + server + " is a local address.")
			}
		} else if !strings.Contains(host, ".") || host == "localhost" {
			return fail("IRC servers have to be on the internet. Use the full server name, like " + example + ".")
		}
		out.Server = host
	}
	switch {
	case req.Port != 0:
		if msg := checkPort(req.Port, "Port"); msg != "" {
			return fail(msg)
		}
		out.Port = req.Port
	case out.Port == 0 && out.TLS:
		out.Port = 6697
	case out.Port == 0:
		out.Port = 6667
	}
	serverMoved := oldServer != "" && addrMoved(oldServer, out.Server)

	// Nick and sign-in.
	if nick := strings.TrimSpace(req.Nick); nick != "" || out.Nick == "" {
		if nick == "" {
			return fail("Add the nick the bot signs in with, for example yourname_bot.")
		}
		if !announce.ValidNick(nick) {
			return fail("That nick isn't allowed on IRC. Use letters, numbers and - _ | [ ] only, starting with a letter, up to 30 characters.")
		}
		out.Nick = nick
	}
	if a := strings.TrimSpace(req.Auth); a != "" {
		switch a {
		case announce.AuthNone, announce.AuthSASL, announce.AuthNickServ:
			out.Auth = a
		default:
			return fail("The sign-in method is SASL, NickServ or none.")
		}
	} else if out.Auth == "" {
		out.Auth = announce.AuthNone
	}
	if req.AuthAccount != nil {
		acct := strings.TrimSpace(*req.AuthAccount)
		if msg := firstProblem(checkMaxLen(acct, "Account", 60), checkNoControl(acct, "Account")); msg != "" {
			return fail(msg)
		}
		if strings.ContainsAny(acct, " \t") {
			return fail("The account name can't contain spaces.")
		}
		out.AuthAccount = acct
	}
	switch {
	case req.AuthPassword != "":
		if msg := firstProblem(checkMaxLen(req.AuthPassword, "Password", 200), checkNoControl(req.AuthPassword, "Password")); msg != "" {
			return fail(msg)
		}
		out.AuthPass = req.AuthPassword
	case out.Auth == announce.AuthNone:
		out.AuthPass = ""
	case base.AuthPass != "" && serverMoved:
		return fail(retypeSecretMessage("password"))
	}
	if out.Auth != announce.AuthNone && out.AuthPass == "" {
		return fail("Enter the password for the sign-in method you chose.")
	}
	if out.Auth == announce.AuthNone {
		out.AuthAccount = ""
	}
	switch {
	case req.Invite != "":
		inv := strings.TrimSpace(req.Invite)
		if msg := firstProblem(checkMaxLen(inv, "Invite command", 400), checkNoControl(inv, "Invite command")); msg != "" {
			return fail(msg)
		}
		if _, err := announce.ParseInvite(inv); err != nil {
			return fail("Invite command: " + err.Error() + ".")
		}
		out.Invite = inv
	case req.ClearInvite:
		out.Invite = ""
	case base.Invite != "" && serverMoved:
		return fail(retypeSecretMessage("invite command"))
	}

	// Channels and announcers.
	if req.Channels != nil {
		saved := map[string]string{}
		for _, ch := range base.Channels {
			saved[strings.ToLower(ch.Name)] = ch.Key
		}
		out.Channels = nil
		seen := map[string]bool{}
		for _, in := range req.Channels {
			name := strings.TrimSpace(in.Name)
			if name == "" {
				continue
			}
			if !strings.HasPrefix(name, "#") && !strings.HasPrefix(name, "&") {
				name = "#" + name
			}
			if !announce.ValidChannel(name) {
				return fail(fmt.Sprintf("%q isn't a valid channel name. It starts with # and has no spaces or commas.", in.Name))
			}
			if seen[strings.ToLower(name)] {
				continue
			}
			seen[strings.ToLower(name)] = true
			key := in.Key
			if key == "" {
				key = saved[strings.ToLower(name)]
				if key != "" && serverMoved {
					return fail(retypeSecretMessage("channel password"))
				}
			} else if msg := firstProblem(checkMaxLen(key, "Channel password", 64), checkNoControl(key, "Channel password")); msg != "" || strings.ContainsAny(key, " ,") {
				if msg == "" {
					msg = "The channel password can't contain spaces or commas."
				}
				return fail(msg)
			}
			out.Channels = append(out.Channels, announce.Channel{Name: name, Key: key})
		}
	}
	if len(out.Channels) == 0 {
		return fail("Add the announce channel to join, for example #announce.")
	}
	if len(out.Channels) > 10 {
		return fail("A connection can join at most 10 channels.")
	}
	if req.Announcers != nil {
		out.Announcers = nil
		seen := map[string]bool{}
		for _, a := range req.Announcers {
			a = strings.TrimSpace(a)
			if a == "" || seen[strings.ToLower(a)] {
				continue
			}
			if !announce.ValidNick(a) {
				return fail(fmt.Sprintf("%q isn't a valid nick for an announcer.", a))
			}
			seen[strings.ToLower(a)] = true
			out.Announcers = append(out.Announcers, a)
		}
	}
	if len(out.Announcers) == 0 {
		return fail("Add the nick of the bot that posts the announcements. Lines from anyone else are ignored.")
	}
	if len(out.Announcers) > 10 {
		return fail("At most 10 announcer nicks.")
	}

	// Template and tracker.
	oldTracker := base.TrackerURL
	if tu := strings.TrimSpace(req.TrackerURL); tu != "" || out.TrackerURL == "" {
		const example = "https://tracker.example.org"
		if msg := firstProblem(checkRequired(tu, "Add the tracker's web address, for example "+example+"."), checkHTTPURL(tu, example, true)); msg != "" {
			return fail(msg)
		}
		u, err := url.Parse(tu)
		if err != nil || u.Host == "" || u.User != nil {
			return fail("That doesn't look like the tracker's web address. It should look like " + example + ".")
		}
		if u.RawQuery != "" || u.Fragment != "" {
			return fail("The tracker address is just the site, for example " + example + ", with nothing after a ? or #.")
		}
		out.TrackerURL = strings.ToLower(u.Scheme) + "://" + u.Host + strings.TrimRight(u.Path, "/")
	}
	trackerMoved := oldTracker != "" && addrMoved(oldTracker, out.TrackerURL)
	if req.Template != nil {
		out.Template = *req.Template
		out.Template.DownloadURL = strings.TrimSpace(out.Template.DownloadURL)
	} else if out.Template.Patterns == nil && out.Preset != "" {
		if p, ok := announce.PresetByID(out.Preset); ok {
			out.Template = p.Template
		}
	}
	compiled, err := announce.Compile(out.Template)
	if err != nil {
		return fail("Template: " + problemText(err, announce.ErrTemplate))
	}

	// Keys: exactly the ones the download address uses.
	given := map[string]string{}
	for _, k := range req.Keys {
		if k.Value == "" {
			continue
		}
		name := strings.TrimSpace(k.Name)
		if !announce.ValidKeyName(name) {
			return fail(fmt.Sprintf("%q can't be used as the name of a key.", k.Name))
		}
		if msg := firstProblem(checkMaxLen(k.Value, "The "+name, 200), checkNoControl(k.Value, "The "+name)); msg != "" || strings.ContainsAny(k.Value, " \t") {
			if msg == "" {
				msg = "The " + name + " can't contain spaces."
			}
			return fail(msg)
		}
		given[name] = k.Value
	}
	out.Keys = map[string]string{}
	for _, name := range compiled.NeedKeys() {
		switch {
		case given[name] != "":
			out.Keys[name] = given[name]
		case base.Keys[name] != "" && !trackerMoved:
			out.Keys[name] = base.Keys[name]
		case base.Keys[name] != "":
			return fail(retypeSecretMessage("keys"))
		default:
			return fail(fmt.Sprintf("Enter your %s. The download address needs it.", name))
		}
	}

	// Filters and limits.
	if req.FreeleechOnly != nil {
		out.FreeleechOnly = *req.FreeleechOnly
	}
	if out.MaxPerHour == 0 {
		out.MaxPerHour = announce.DefaultMaxPerHour
	}
	if out.MaxPerDay == 0 {
		out.MaxPerDay = announce.DefaultMaxPerDay
	}
	if req.MaxPerHour != nil {
		if msg := checkIntRange(*req.MaxPerHour, "The hourly limit", 1, 60); msg != "" {
			return fail(msg)
		}
		out.MaxPerHour = *req.MaxPerHour
	}
	if req.MaxPerDay != nil {
		if msg := checkIntRange(*req.MaxPerDay, "The daily limit", 1, 500); msg != "" {
			return fail(msg)
		}
		out.MaxPerDay = *req.MaxPerDay
	}
	return out, nil
}

func ircID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid connection ID.")
		return 0, false
	}
	return id, true
}

func (s *Server) loadIRC(w http.ResponseWriter, id int64) (announce.Connection, bool) {
	c, err := s.AnnounceRepo.Get(id)
	if errors.Is(err, announce.ErrNotFound) {
		writeError(w, http.StatusNotFound, "That connection no longer exists.")
		return c, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't read the connection.")
		slog.Warn("irc announce: load connection", "id", id, "error", err)
		return c, false
	}
	return c, true
}

type ircListPayload struct {
	Connections []ircConnPayload  `json:"connections"`
	Presets     []announce.Preset `json:"presets"`
}

// handleListIRC lists the connections with how each is doing, and the
// built-in templates. Passwords, the invite command and keys are never
// included, only whether they are saved.
func (s *Server) handleListIRC(w http.ResponseWriter, r *http.Request) {
	list, err := s.AnnounceRepo.List()
	if err != nil {
		slog.Warn("irc announce: list", "error", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read the IRC connections.")
		return
	}
	out := ircListPayload{Connections: make([]ircConnPayload, 0, len(list)), Presets: announce.Presets()}
	for _, c := range list {
		out.Connections = append(out.Connections, s.ircPayload(c))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateIRC(w http.ResponseWriter, r *http.Request) {
	var req ircConnRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	list, err := s.AnnounceRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't read the IRC connections.")
		return
	}
	if len(list) >= maxIRCConnections {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("You can have at most %d IRC connections.", maxIRCConnections))
		return
	}
	c, err := buildIRCConnection(req, announce.Connection{TLS: true})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.AnnounceRepo.Create(c)
	if err != nil {
		slog.Warn("irc announce: create", "error", err)
		writeError(w, http.StatusInternalServerError, "Couldn't save the connection.")
		return
	}
	s.reloadIRC()
	writeJSON(w, http.StatusCreated, s.ircPayload(created))
}

func (s *Server) handleUpdateIRC(w http.ResponseWriter, r *http.Request) {
	id, ok := ircID(w, r)
	if !ok {
		return
	}
	var req ircConnRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	old, ok := s.loadIRC(w, id)
	if !ok {
		return
	}
	c, err := buildIRCConnection(req, old)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.AnnounceRepo.Update(c)
	if err != nil {
		slog.Warn("irc announce: update", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "Couldn't save the connection.")
		return
	}
	s.reloadIRC()
	writeJSON(w, http.StatusOK, s.ircPayload(updated))
}

func (s *Server) handleDeleteIRC(w http.ResponseWriter, r *http.Request) {
	id, ok := ircID(w, r)
	if !ok {
		return
	}
	if err := s.AnnounceRepo.Delete(id); err != nil {
		if errors.Is(err, announce.ErrNotFound) {
			writeError(w, http.StatusNotFound, "That connection no longer exists.")
			return
		}
		writeError(w, http.StatusInternalServerError, "Couldn't remove the connection.")
		return
	}
	s.reloadIRC()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSetIRCEnabled(w http.ResponseWriter, r *http.Request) {
	id, ok := ircID(w, r)
	if !ok {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if err := s.AnnounceRepo.SetEnabled(id, req.Enabled); err != nil {
		if errors.Is(err, announce.ErrNotFound) {
			writeError(w, http.StatusNotFound, "That connection no longer exists.")
			return
		}
		writeError(w, http.StatusInternalServerError, "Couldn't change the connection.")
		return
	}
	s.reloadIRC()
	c, ok := s.loadIRC(w, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.ircPayload(c))
}

// reloadIRC makes the running connections match what is saved.
func (s *Server) reloadIRC() {
	s.irc.forget()
	if err := s.Announce.Reload(); err != nil {
		slog.Warn("irc announce: reload", "error", err)
	}
}

type ircLogPayload struct {
	At        string `json:"at"`
	Channel   string `json:"channel"`
	Announcer string `json:"announcer"`
	Release   string `json:"release"`
	Category  string `json:"category"`
	SizeBytes int64  `json:"sizeBytes"`
	Decision  string `json:"decision"`
	Reason    string `json:"reason"`
	Item      string `json:"item"`
}

// handleIRCLog returns the last 50 announcements of a connection and what
// became of each, newest first.
func (s *Server) handleIRCLog(w http.ResponseWriter, r *http.Request) {
	id, ok := ircID(w, r)
	if !ok {
		return
	}
	if _, ok := s.loadIRC(w, id); !ok {
		return
	}
	rows := s.Announce.Log(id)
	out := make([]ircLogPayload, 0, len(rows))
	for _, e := range rows {
		out = append(out, ircLogPayload{At: e.At.UTC().Format(time.RFC3339), Channel: e.Channel, Announcer: e.Announcer, Release: e.Release,
			Category: e.Category, SizeBytes: e.SizeBytes, Decision: e.Decision, Reason: e.Reason, Item: e.Item})
	}
	writeJSON(w, http.StatusOK, out)
}

type ircTestRequest struct {
	ircConnRequest
	Lines []string `json:"lines"`
}

// handleTestIRC reads pasted announcement lines with a template and says how
// they were read and whether the release would be grabbed. It grabs nothing,
// changes nothing and does not touch the log.
func (s *Server) handleTestIRC(w http.ResponseWriter, r *http.Request) {
	var req ircTestRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if len(req.Lines) == 0 || len(req.Lines) > 20 {
		writeError(w, http.StatusBadRequest, "Paste an announcement line to test (up to 20 lines).")
		return
	}
	for _, l := range req.Lines {
		if len(l) > 4000 {
			writeError(w, http.StatusBadRequest, "That line is too long to be an announcement.")
			return
		}
	}
	if req.Template == nil {
		writeError(w, http.StatusBadRequest, "Choose or write a template to test.")
		return
	}
	var base announce.Connection
	if req.ID != 0 {
		saved, ok := s.loadIRC(w, req.ID)
		if !ok {
			return
		}
		base = saved
	}
	tracker := strings.TrimSpace(req.TrackerURL)
	if tracker == "" {
		tracker = base.TrackerURL
	}
	if tracker == "" {
		tracker = "https://tracker.example.org"
	}
	tpl := *req.Template
	tpl.DownloadURL = strings.TrimSpace(tpl.DownloadURL)
	conn := announce.Connection{ID: req.ID, Name: "test", Template: tpl, TrackerURL: tracker}
	if req.FreeleechOnly != nil {
		conn.FreeleechOnly = *req.FreeleechOnly
	} else {
		conn.FreeleechOnly = base.FreeleechOnly
	}
	if base.MaxPerHour > 0 {
		conn.MaxPerHour, conn.MaxPerDay = base.MaxPerHour, base.MaxPerDay
	}
	writeJSON(w, http.StatusOK, s.Announce.Test(r.Context(), conn, req.Lines))
}
