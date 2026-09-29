package api

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ryanborg/mediarium/internal/notify"
)

// notifyEvent fires every configured notification target that
// subscribed to this kind of event, without blocking the caller — a slow/
// unreachable webhook shouldn't delay the download/import pipeline.
func (s *Server) notifyEvent(eventType, title, message string) {
	go func() {
		senders, err := s.NotifyRepo.SendersFor(notify.EventKind(eventType))
		if err != nil {
			log.Printf("notify: load senders: %v", err)
			return
		}
		if len(senders) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		errs := notify.NotifyAll(ctx, senders, notify.Event{Type: eventType, Title: title, Message: message, Timestamp: time.Now()})
		for _, err := range errs {
			log.Printf("notify: %v", err)
		}
	}()
}

// notifyTargetRequest is the body of create, update and test. Config holds the
// values of the fields listed for the type by GET /api/notifications/types;
// url, botToken and chatId are still accepted at the top level for the
// original webhook/Discord/Telegram forms.
type notifyTargetRequest struct {
	ID       int64             `json:"id,omitempty"` // test only: a saved target to test
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Enabled  *bool             `json:"enabled,omitempty"`
	Events   []string          `json:"events,omitempty"`
	Config   map[string]string `json:"config,omitempty"`
	URL      string            `json:"url,omitempty"`
	BotToken string            `json:"botToken,omitempty"`
	ChatID   string            `json:"chatId,omitempty"`
}

// config merges the legacy top-level fields into Config (Config wins).
func (r notifyTargetRequest) config() map[string]string {
	cfg := map[string]string{}
	for name, v := range map[string]string{"url": r.URL, "botToken": r.BotToken, "chatId": r.ChatID} {
		if v != "" {
			cfg[name] = v
		}
	}
	for k, v := range r.Config {
		cfg[k] = v
	}
	return cfg
}

// notifyTargetPayload is what targets look like on the way out. Secret field
// values are never included: HasSecrets says, per secret field, whether one is
// stored, and Config carries only the non-secret fields.
type notifyTargetPayload struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Enabled     bool              `json:"enabled"`
	Events      []string          `json:"events"`
	Config      map[string]string `json:"config"`
	HasSecrets  map[string]bool   `json:"hasSecrets"`
	HasPassword bool              `json:"hasPassword"`
	URL         string            `json:"url,omitempty"`    // the non-secret url field, as before
	ChatID      string            `json:"chatId,omitempty"` // the non-secret chatId field, as before
}

func toNotifyPayload(t notify.Target) notifyTargetPayload {
	p := notifyTargetPayload{
		ID: t.ID, Name: t.Name, Type: string(t.Type), Enabled: t.Enabled, Events: t.Events,
		Config: map[string]string{}, HasSecrets: map[string]bool{},
	}
	if len(p.Events) == 0 {
		p.Events = notify.DefaultEvents()
	}
	if info, ok := notify.TypeByName(string(t.Type)); ok {
		for _, f := range info.Fields {
			v := t.Config[f.Name]
			if f.Secret() {
				p.HasSecrets[f.Name] = v != ""
			} else if v != "" {
				p.Config[f.Name] = v
			}
		}
	}
	p.HasPassword = p.HasSecrets["password"]
	p.URL, p.ChatID = p.Config["url"], p.Config["chatId"]
	return p
}

func (s *Server) handleListNotifyTargets(w http.ResponseWriter, r *http.Request) {
	list, err := s.NotifyRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]notifyTargetPayload, len(list))
	for i, t := range list {
		out[i] = toNotifyPayload(t)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleNotifyTypes describes every target type and its form fields, so the UI
// can render the forms generically.
func (s *Server) handleNotifyTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, notify.Types())
}

// handleNotifyEvents lists the events a target can subscribe to.
func (s *Server) handleNotifyEvents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, notify.EventKinds())
}

func (s *Server) handleCreateNotifyTarget(w http.ResponseWriter, r *http.Request) {
	var req notifyTargetRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	info, ok := notify.TypeByName(req.Type)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown notification type "+strconv.Quote(req.Type))
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = info.Label
	}
	cfg := notify.ApplyDefaults(req.Type, req.config())
	if err := notify.Validate(req.Type, cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	events, err := notify.NormalizeEvents(req.Events)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.NotifyRepo.Create(notify.Target{Name: req.Name, Type: notify.TargetType(req.Type), Events: events, Config: cfg})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Enabled != nil && !*req.Enabled {
		created.Enabled = false
		if created, err = s.NotifyRepo.Update(created); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusCreated, toNotifyPayload(created))
}

// handleUpdateNotifyTarget edits a saved target. A secret field left empty
// keeps the stored value, so the form never needs the secrets back.
func (s *Server) handleUpdateNotifyTarget(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid notification target id")
		return
	}
	var req notifyTargetRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	old, err := s.NotifyRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "notification target not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	typ := req.Type
	if typ == "" {
		typ = string(old.Type)
	}
	if _, ok := notify.TypeByName(typ); !ok {
		writeError(w, http.StatusBadRequest, "unknown notification type "+strconv.Quote(typ))
		return
	}
	cfg := s.withStoredSecrets(typ, req.config(), old)
	cfg = notify.ApplyDefaults(typ, cfg)
	if err := notify.Validate(typ, cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	events := old.Events
	if req.Events != nil {
		if events, err = notify.NormalizeEvents(req.Events); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = old.Name
	}
	enabled := old.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	updated, err := s.NotifyRepo.Update(notify.Target{
		ID: id, Name: name, Type: notify.TargetType(typ), Enabled: enabled, Events: events, Config: cfg,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toNotifyPayload(updated))
}

// withStoredSecrets fills secret fields the request left empty from a saved
// target of the same type.
func (s *Server) withStoredSecrets(typ string, cfg map[string]string, saved notify.Target) map[string]string {
	out := map[string]string{}
	for k, v := range cfg {
		out[k] = v
	}
	if string(saved.Type) != typ {
		return out
	}
	if info, ok := notify.TypeByName(typ); ok {
		for _, f := range info.Fields {
			if f.Secret() && out[f.Name] == "" {
				out[f.Name] = saved.Config[f.Name]
			}
		}
	}
	return out
}

func (s *Server) handleDeleteNotifyTarget(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid notification target id")
		return
	}
	if err := s.NotifyRepo.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

type notifyTestResponse struct {
	Sent  bool   `json:"sent"`
	Error string `json:"error,omitempty"`
}

// handleTestNotifyTarget sends a test message through a target that need not be
// saved: send the type and config from the form as they stand, or just an id to
// test a saved target, or an id plus form values to test an edit (empty secret
// fields fall back to the stored ones). Bad input is a 400; a delivery that
// fails is a 200 with sent:false and the reason.
func (s *Server) handleTestNotifyTarget(w http.ResponseWriter, r *http.Request) {
	var req notifyTargetRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	typ := req.Type
	cfg := req.config()
	if req.ID != 0 {
		saved, err := s.NotifyRepo.Get(req.ID)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "notification target not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if typ == "" {
			typ = string(saved.Type)
		}
		if len(cfg) == 0 && typ == string(saved.Type) {
			cfg = saved.Config
		} else {
			cfg = s.withStoredSecrets(typ, cfg, saved)
		}
	}
	if _, ok := notify.TypeByName(typ); !ok {
		writeError(w, http.StatusBadRequest, "unknown notification type "+strconv.Quote(typ))
		return
	}
	cfg = notify.ApplyDefaults(typ, cfg)
	if err := notify.Validate(typ, cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sender, err := notify.NewSender(notify.Target{Type: notify.TargetType(typ), Config: cfg})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	err = sender.Send(ctx, notify.Event{
		Type: "test", Title: "Mediarium test notification",
		Message: "If you can read this, notifications from Mediarium are working.", Timestamp: time.Now(),
	})
	if err != nil {
		writeJSON(w, http.StatusOK, notifyTestResponse{Sent: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, notifyTestResponse{Sent: true})
}
