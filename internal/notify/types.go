package notify

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
)

// Event kinds a target can subscribe to. The activity feed's own event names
// map onto these with EventKind.
const (
	EventAdded    = "added"    // a release was grabbed / sent to download
	EventImported = "imported" // a download finished and was imported
	EventFailed   = "failed"   // a download or import failed
	EventConflict = "conflict" // an import hit a file that already exists
	EventSubtitle = "subtitle" // a subtitle was downloaded
	EventHealth   = "health"   // something is wrong with the app (or recovered)
)

// EventInfo describes one subscribable event for the UI.
type EventInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Help  string `json:"help"`
}

// EventKinds lists every subscribable event in display order.
func EventKinds() []EventInfo {
	return []EventInfo{
		{EventAdded, "Added", "A release was found and sent to the downloader."},
		{EventImported, "Downloaded", "A download finished and was imported into your library."},
		{EventFailed, "Failed", "A download or import failed."},
		{EventConflict, "Needs attention", "An imported file collided with one already in your library and needs a decision."},
		{EventSubtitle, "Subtitles", "A subtitle file was downloaded."},
		{EventHealth, "Health", "Something is wrong with Mediarium (a Usenet server or indexer stopped working), and when it recovers."},
	}
}

// DefaultEvents is what a target subscribes to when it doesn't say.
func DefaultEvents() []string {
	kinds := EventKinds()
	out := make([]string, len(kinds))
	for i, k := range kinds {
		out[i] = k.ID
	}
	return out
}

// EventKind maps an event type from the activity feed to the kind a target
// subscribes to ("grabbed" is the "added" kind). Unknown types map to
// themselves, so they reach only targets that somehow subscribed to them.
func EventKind(eventType string) string {
	if eventType == "grabbed" {
		return EventAdded
	}
	return eventType
}

// NormalizeEvents keeps the valid, de-duplicated kinds in canonical order. An
// empty input yields DefaultEvents; input with only unknown kinds is an error.
func NormalizeEvents(in []string) ([]string, error) {
	if len(in) == 0 {
		return DefaultEvents(), nil
	}
	want := map[string]bool{}
	for _, e := range in {
		e = strings.TrimSpace(strings.ToLower(e))
		if e == "" {
			continue
		}
		known := false
		for _, k := range EventKinds() {
			if k.ID == e {
				known = true
			}
		}
		if !known {
			return nil, fmt.Errorf("unknown event %q", e)
		}
		want[e] = true
	}
	var out []string
	for _, k := range EventKinds() {
		if want[k.ID] {
			out = append(out, k.ID)
		}
	}
	return out, nil
}

// Option is one choice of a "select" field.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Field describes one input of a target's form.
type Field struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Kind        string   `json:"kind"` // text | password | number | select
	Required    bool     `json:"required"`
	Placeholder string   `json:"placeholder,omitempty"`
	Default     string   `json:"default,omitempty"`
	Options     []Option `json:"options,omitempty"`
	Help        string   `json:"help"`
}

// Secret fields are stored encrypted and never returned by the API.
func (f Field) Secret() bool { return f.Kind == "password" }

// TypeInfo describes one kind of notification target.
type TypeInfo struct {
	Type   string  `json:"type"`
	Label  string  `json:"label"`
	Fields []Field `json:"fields"`
}

// Types is the catalog of target types, in display order. The UI builds its
// forms from this, so every field carries its own short how-to-find-it help.
func Types() []TypeInfo {
	return []TypeInfo{
		{Type: string(TargetEmail), Label: "Email", Fields: []Field{
			{Name: "host", Label: "SMTP server", Kind: "text", Required: true, Placeholder: "smtp.gmail.com",
				Help: "Your email provider's outgoing (SMTP) server. Search \"<provider> SMTP settings\" - Gmail uses smtp.gmail.com, Outlook uses smtp-mail.outlook.com."},
			{Name: "port", Label: "Port", Kind: "number", Required: true, Placeholder: "587", Default: "587",
				Help: "Usually 587 for STARTTLS, 465 for SSL, 25 for no encryption."},
			{Name: "security", Label: "Security", Kind: "select", Required: true, Default: "starttls",
				Options: []Option{{"starttls", "STARTTLS (port 587)"}, {"ssl", "SSL/TLS (port 465)"}, {"none", "None (port 25, not recommended)"}},
				Help:    "How the connection to the server is encrypted. Pick the one that matches the port."},
			{Name: "username", Label: "Username", Kind: "text", Placeholder: "you@example.com",
				Help: "Usually your full email address. Leave empty if your server needs no login."},
			{Name: "password", Label: "Password", Kind: "password",
				Help: "Your email password. Gmail and Outlook need an \"app password\" created in your account's security settings, not your normal password."},
			{Name: "from", Label: "From address", Kind: "text", Required: true, Placeholder: "Mediarium <you@example.com>",
				Help: "Who the message appears to come from. Most providers require this to be your own address."},
			{Name: "to", Label: "Send to", Kind: "text", Required: true, Placeholder: "you@example.com",
				Help: "Where notifications are delivered. Separate several addresses with commas."},
		}},
		{Type: string(TargetNtfy), Label: "ntfy", Fields: []Field{
			{Name: "server", Label: "Server", Kind: "text", Placeholder: "https://ntfy.sh", Default: "https://ntfy.sh",
				Help: "The ntfy server. Leave as https://ntfy.sh unless you run your own."},
			{Name: "topic", Label: "Topic", Kind: "text", Required: true, Placeholder: "mediarium-a1b2c3",
				Help: "Pick any hard-to-guess name (anyone who knows it can read the messages on a public server), then subscribe to the same topic in the ntfy phone app."},
			{Name: "token", Label: "Access token", Kind: "password",
				Help: "Only for servers that need a login: create one in the ntfy web app under Account > Access tokens."},
		}},
		{Type: string(TargetGotify), Label: "Gotify", Fields: []Field{
			{Name: "url", Label: "Server URL", Kind: "text", Required: true, Placeholder: "https://gotify.example.com",
				Help: "The address of your Gotify server, as you open it in a browser."},
			{Name: "token", Label: "App token", Kind: "password", Required: true,
				Help: "In the Gotify web app open Apps, create an application (for example \"Mediarium\") and copy its token."},
		}},
		{Type: string(TargetPushover), Label: "Pushover", Fields: []Field{
			{Name: "userKey", Label: "User key", Kind: "password", Required: true,
				Help: "Shown at the top right of your Pushover dashboard (pushover.net) after you log in."},
			{Name: "token", Label: "App token", Kind: "password", Required: true,
				Help: "On pushover.net choose \"Create an Application/API Token\", name it Mediarium, and copy the API token it gives you."},
		}},
		{Type: string(TargetSlack), Label: "Slack", Fields: []Field{
			{Name: "url", Label: "Webhook URL", Kind: "password", Required: true, Placeholder: "https://hooks.slack.com/services/...",
				Help: "In Slack create an app (api.slack.com/apps), turn on Incoming Webhooks, add one to a channel, and copy its URL."},
		}},
		{Type: string(TargetDiscord), Label: "Discord", Fields: []Field{
			{Name: "url", Label: "Webhook URL", Kind: "text", Required: true, Placeholder: "https://discord.com/api/webhooks/...",
				Help: "In Discord open the channel's settings > Integrations > Webhooks > New Webhook, then Copy Webhook URL."},
		}},
		{Type: string(TargetTelegram), Label: "Telegram", Fields: []Field{
			{Name: "botToken", Label: "Bot token", Kind: "password", Required: true,
				Help: "Message @BotFather on Telegram, send /newbot, and copy the token it replies with."},
			{Name: "chatId", Label: "Chat ID", Kind: "text", Required: true, Placeholder: "123456789",
				Help: "Send your new bot any message, then open https://api.telegram.org/bot<token>/getUpdates and copy the number after \"chat\":{\"id\":."},
		}},
		{Type: string(TargetWebhook), Label: "Webhook", Fields: []Field{
			{Name: "url", Label: "URL", Kind: "text", Required: true, Placeholder: "https://example.com/hook",
				Help: "Mediarium sends an HTTP POST with a JSON body to this address."},
			{Name: "template", Label: "Custom JSON body", Kind: "text", Placeholder: `{"text": "{{title}}: {{message}}"}`,
				Help: "Optional. Leave empty to send {\"event\",\"title\",\"message\",\"at\"}. Otherwise write your own JSON using {{event}}, {{title}}, {{message}} and {{at}}."},
		}},
	}
}

// TypeByName finds a type in the catalog.
func TypeByName(name string) (TypeInfo, bool) {
	for _, t := range Types() {
		if t.Type == name {
			return t, true
		}
	}
	return TypeInfo{}, false
}

// SplitFields separates cfg into public and secret values by the type's field
// kinds, dropping names the type doesn't have.
func SplitFields(typ TypeInfo, cfg map[string]string) (public, secret map[string]string) {
	public, secret = map[string]string{}, map[string]string{}
	for _, f := range typ.Fields {
		v, ok := cfg[f.Name]
		if !ok || v == "" {
			continue
		}
		if f.Secret() {
			secret[f.Name] = v
		} else {
			public[f.Name] = v
		}
	}
	return public, secret
}

// Validate checks cfg against the type's rules and returns a sentence a person
// can act on, or nil. Defaults are not applied here (see ApplyDefaults).
func Validate(typeName string, cfg map[string]string) error {
	typ, ok := TypeByName(typeName)
	if !ok {
		return fmt.Errorf("unknown notification type %q", typeName)
	}
	get := func(name string) string { return strings.TrimSpace(cfg[name]) }
	for _, f := range typ.Fields {
		if f.Required && get(f.Name) == "" {
			return fmt.Errorf("%s is required", f.Label)
		}
	}
	switch TargetType(typeName) {
	case TargetEmail:
		if p, err := strconv.Atoi(get("port")); err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("Port must be a number between 1 and 65535")
		}
		switch get("security") {
		case "starttls", "ssl", "none":
		default:
			return fmt.Errorf(`Security must be "starttls", "ssl" or "none"`)
		}
		if strings.ContainsAny(get("host"), "/: ") {
			return fmt.Errorf("SMTP server should be just a host name such as smtp.example.com")
		}
		if (get("username") == "") != (get("password") == "") {
			return fmt.Errorf("Username and Password go together: fill in both, or leave both empty")
		}
		if _, err := mail.ParseAddress(get("from")); err != nil {
			return fmt.Errorf("From address does not look like an email address")
		}
		if len(SplitAddresses(get("to"))) == 0 {
			return fmt.Errorf("Send to must contain at least one email address")
		}
		for _, a := range SplitAddresses(get("to")) {
			if _, err := mail.ParseAddress(a); err != nil {
				return fmt.Errorf("%q does not look like an email address", a)
			}
		}
	case TargetNtfy:
		if s := get("server"); s != "" {
			if err := checkHTTPURL("Server", s); err != nil {
				return err
			}
		}
		if strings.ContainsAny(get("topic"), "/ ?#") {
			return fmt.Errorf("Topic can't contain spaces or slashes")
		}
	case TargetGotify:
		return checkHTTPURL("Server URL", get("url"))
	case TargetSlack, TargetDiscord:
		return checkHTTPURL("Webhook URL", get("url"))
	case TargetWebhook:
		if err := checkHTTPURL("URL", get("url")); err != nil {
			return err
		}
		if t := get("template"); t != "" {
			if _, err := renderTemplate(t, Event{Type: "test", Title: "t", Message: "m"}); err != nil {
				return fmt.Errorf("Custom JSON body is not valid JSON after filling in the placeholders: %v", err)
			}
		}
	}
	return nil
}

// ApplyDefaults fills in the defaults of empty optional fields (so "port"
// left blank means 587) and trims whitespace. It returns a fresh map.
func ApplyDefaults(typeName string, cfg map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range cfg {
		out[k] = strings.TrimSpace(v)
	}
	if typ, ok := TypeByName(typeName); ok {
		for _, f := range typ.Fields {
			if out[f.Name] == "" && f.Default != "" {
				out[f.Name] = f.Default
			}
		}
	}
	return out
}

func checkHTTPURL(label, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%s must be a full web address starting with http:// or https://", label)
	}
	return nil
}

// SplitAddresses splits a comma- or semicolon-separated recipient list.
func SplitAddresses(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' }) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// renderTemplate fills {{event}}, {{title}}, {{message}} and {{at}} into a
// user-written JSON template, escaping each value for use inside a JSON
// string, and checks the result is valid JSON.
func renderTemplate(tmpl string, ev Event) (string, error) {
	esc := func(s string) string {
		b, _ := json.Marshal(s)
		return string(b[1 : len(b)-1])
	}
	at := ev.Timestamp
	atText := ""
	if !at.IsZero() {
		atText = at.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	body := strings.NewReplacer(
		"{{event}}", esc(ev.Type), "{{title}}", esc(ev.Title), "{{message}}", esc(ev.Message), "{{at}}", esc(atText),
	).Replace(tmpl)
	if !json.Valid([]byte(body)) {
		return "", fmt.Errorf("invalid JSON")
	}
	return body, nil
}
