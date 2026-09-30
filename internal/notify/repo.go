package notify

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/crypto"
)

type TargetType string

const (
	TargetWebhook  TargetType = "webhook"
	TargetDiscord  TargetType = "discord"
	TargetTelegram TargetType = "telegram"
	TargetEmail    TargetType = "email"
	TargetNtfy     TargetType = "ntfy"
	TargetGotify   TargetType = "gotify"
	TargetPushover TargetType = "pushover"
	TargetSlack    TargetType = "slack"
)

// Target is a configured notification destination.
//
// Config holds every field of the target's type (see Types), secrets
// included; the repo stores the secret ones encrypted. URL, BotToken and
// ChatID are the original webhook/Discord/Telegram fields, kept as aliases of
// Config["url"], ["botToken"] and ["chatId"].
type Target struct {
	ID       int64
	Name     string
	Type     TargetType
	URL      string // alias of Config["url"]
	BotToken string // alias of Config["botToken"]
	ChatID   string // alias of Config["chatId"]
	Enabled  bool
	Events   []string          // kinds this target hears about (see EventKinds); empty means all
	Config   map[string]string // field name -> value
}

// Subscribed reports whether the target wants events of the given kind.
func (t Target) Subscribed(kind string) bool {
	if len(t.Events) == 0 {
		return true
	}
	for _, e := range t.Events {
		if e == kind {
			return true
		}
	}
	return false
}

// withAliases returns t with Config and the legacy alias fields in agreement.
func (t Target) withAliases() Target {
	cfg := make(map[string]string, len(t.Config)+3)
	for k, v := range t.Config {
		cfg[k] = v
	}
	for name, v := range map[string]string{"url": t.URL, "botToken": t.BotToken, "chatId": t.ChatID} {
		if v != "" && cfg[name] == "" {
			cfg[name] = v
		}
	}
	t.Config = cfg
	t.URL, t.BotToken, t.ChatID = cfg["url"], cfg["botToken"], cfg["chatId"]
	return t
}

type Repo struct {
	db  *sql.DB
	box *crypto.Box
}

func NewRepo(db *sql.DB, box *crypto.Box) *Repo {
	return &Repo{db: db, box: box}
}

// encodeConfig splits cfg by the type's field kinds into the JSON stored in
// plain text and the JSON stored encrypted. A type outside the catalog keeps
// everything encrypted rather than risk storing a secret in the clear.
func (r *Repo) encodeConfig(typ TargetType, cfg map[string]string) (public, secret string, err error) {
	pub, sec := map[string]string{}, map[string]string{}
	if info, ok := TypeByName(string(typ)); ok {
		pub, sec = SplitFields(info, cfg)
	} else {
		for k, v := range cfg {
			if v != "" {
				sec[k] = v
			}
		}
	}
	if len(pub) > 0 {
		b, err := json.Marshal(pub)
		if err != nil {
			return "", "", fmt.Errorf("encode notification config: %w", err)
		}
		public = string(b)
	}
	if len(sec) > 0 {
		b, err := json.Marshal(sec)
		if err != nil {
			return "", "", fmt.Errorf("encode notification secrets: %w", err)
		}
		secret, err = r.box.Encrypt(string(b))
		if err != nil {
			return "", "", fmt.Errorf("encrypt notification secrets: %w", err)
		}
	}
	return public, secret, nil
}

func normalizeEventsForStore(events []string) string {
	norm, err := NormalizeEvents(events)
	if err != nil {
		norm = DefaultEvents()
	}
	return strings.Join(norm, ",")
}

func (r *Repo) Create(t Target) (Target, error) {
	t = t.withAliases()
	public, secret, err := r.encodeConfig(t.Type, t.Config)
	if err != nil {
		return Target{}, err
	}
	events := normalizeEventsForStore(t.Events)
	res, err := r.db.Exec(
		`INSERT INTO notification_targets (name, type, enabled, events, config_json, secrets_encrypted) VALUES (?, ?, ?, ?, ?, ?)`,
		t.Name, string(t.Type), true, events, public, secret,
	)
	if err != nil {
		return Target{}, fmt.Errorf("insert notification target: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Target{}, fmt.Errorf("get inserted notification target id: %w", err)
	}
	t.ID = id
	t.Enabled = true
	t.Events = strings.Split(events, ",")
	return t, nil
}

// Update replaces a target's name, enabled flag, events and config. A secret
// field left empty in t.Config keeps the value already stored, so an edit form
// doesn't need the secrets it was never shown.
func (r *Repo) Update(t Target) (Target, error) {
	old, err := r.Get(t.ID)
	if err != nil {
		return Target{}, err
	}
	t = t.withAliases()
	if info, ok := TypeByName(string(t.Type)); ok && old.Type == t.Type {
		for _, f := range info.Fields {
			if f.Secret() && t.Config[f.Name] == "" {
				t.Config[f.Name] = old.Config[f.Name]
			}
		}
	}
	public, secret, err := r.encodeConfig(t.Type, t.Config)
	if err != nil {
		return Target{}, err
	}
	events := normalizeEventsForStore(t.Events)
	// The legacy columns are cleared: their values now live in the generic
	// config, which List reads last.
	_, err = r.db.Exec(
		`UPDATE notification_targets SET name = ?, type = ?, enabled = ?, events = ?, config_json = ?, secrets_encrypted = ?,
		 url = NULL, bot_token_encrypted = NULL, chat_id = NULL WHERE id = ?`,
		t.Name, string(t.Type), t.Enabled, events, public, secret, t.ID,
	)
	if err != nil {
		return Target{}, fmt.Errorf("update notification target %d: %w", t.ID, err)
	}
	return r.Get(t.ID)
}

// Get returns one target, or sql.ErrNoRows.
func (r *Repo) Get(id int64) (Target, error) {
	all, err := r.List()
	if err != nil {
		return Target{}, err
	}
	for _, t := range all {
		if t.ID == id {
			return t, nil
		}
	}
	return Target{}, sql.ErrNoRows
}

func (r *Repo) List() ([]Target, error) {
	rows, err := r.db.Query(`SELECT id, name, type, COALESCE(url, ''), bot_token_encrypted, COALESCE(chat_id, ''), enabled, events, config_json, secrets_encrypted FROM notification_targets ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list notification targets: %w", err)
	}
	defer rows.Close()

	var out []Target
	for rows.Next() {
		var (
			t                       Target
			typ                     string
			encToken                sql.NullString
			url, chatID             string
			events, cfgJSON, secEnc string
		)
		if err := rows.Scan(&t.ID, &t.Name, &typ, &url, &encToken, &chatID, &t.Enabled, &events, &cfgJSON, &secEnc); err != nil {
			return nil, fmt.Errorf("scan notification target: %w", err)
		}
		t.Type = TargetType(typ)
		t.Events = splitEvents(events)
		t.Config = map[string]string{}

		// Rows written before targets had a generic config keep their values in
		// the original columns; the generic config, when present, wins.
		if url != "" {
			t.Config["url"] = url
		}
		if chatID != "" {
			t.Config["chatId"] = chatID
		}
		if encToken.Valid && encToken.String != "" {
			token, err := r.box.Decrypt(encToken.String)
			if err != nil {
				return nil, fmt.Errorf("decrypt bot token for target %d: %w", t.ID, err)
			}
			t.Config["botToken"] = token
		}
		if cfgJSON != "" {
			if err := mergeJSON(t.Config, cfgJSON); err != nil {
				return nil, fmt.Errorf("decode config for target %d: %w", t.ID, err)
			}
		}
		if secEnc != "" {
			plain, err := r.box.Decrypt(secEnc)
			if err != nil {
				return nil, fmt.Errorf("decrypt secrets for target %d: %w", t.ID, err)
			}
			if err := mergeJSON(t.Config, plain); err != nil {
				return nil, fmt.Errorf("decode secrets for target %d: %w", t.ID, err)
			}
		}
		t = t.withAliases()
		out = append(out, t)
	}
	return out, rows.Err()
}

func mergeJSON(dst map[string]string, raw string) error {
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return err
	}
	for k, v := range m {
		dst[k] = v
	}
	return nil
}

// splitEvents splits the comma-separated events column.
func splitEvents(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (r *Repo) Delete(id int64) error {
	_, err := r.db.Exec(`DELETE FROM notification_targets WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete notification target %d: %w", id, err)
	}
	return nil
}

// NewSender builds the live Sender for a target from its config.
func NewSender(t Target) (Sender, error) {
	t = t.withAliases()
	cfg := ApplyDefaults(string(t.Type), t.Config)
	switch t.Type {
	case TargetWebhook:
		s := NewWebhookSender(cfg["url"])
		s.Template = cfg["template"]
		return s, nil
	case TargetDiscord:
		return NewDiscordSender(cfg["url"]), nil
	case TargetTelegram:
		return NewTelegramSender(cfg["botToken"], cfg["chatId"]), nil
	case TargetSlack:
		return NewSlackSender(cfg["url"]), nil
	case TargetNtfy:
		return NewNtfySender(cfg["server"], cfg["topic"], cfg["token"]), nil
	case TargetGotify:
		return NewGotifySender(cfg["url"], cfg["token"]), nil
	case TargetPushover:
		return NewPushoverSender(cfg["userKey"], cfg["token"]), nil
	case TargetEmail:
		port, err := strconv.Atoi(cfg["port"])
		if err != nil {
			return nil, fmt.Errorf("email port %q is not a number", cfg["port"])
		}
		return &EmailSender{
			Host: cfg["host"], Port: port, Security: cfg["security"],
			Username: cfg["username"], Password: cfg["password"], From: cfg["from"], To: SplitAddresses(cfg["to"]),
		}, nil
	}
	return nil, fmt.Errorf("unknown notification type %q", t.Type)
}

// Senders builds a live Sender for every enabled target, ready to pass to
// NotifyAll.
func (r *Repo) Senders() ([]Sender, error) {
	return r.SendersFor("")
}

// SendersFor builds a Sender for every enabled target subscribed to kind (see
// EventKind); an empty kind means every enabled target. A target that can't be
// built (a type this version doesn't know) is skipped.
func (r *Repo) SendersFor(kind string) ([]Sender, error) {
	targets, err := r.List()
	if err != nil {
		return nil, err
	}
	var senders []Sender
	for _, t := range targets {
		if !t.Enabled || (kind != "" && !t.Subscribed(kind)) {
			continue
		}
		s, err := NewSender(t)
		if err != nil {
			continue
		}
		senders = append(senders, s)
	}
	return senders, nil
}
