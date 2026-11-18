package notify

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/inputcheck"
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
	EventUpdate   = "update"   // a new version of Mediarium is available
	EventRequest  = "request"  // someone asked for a title (see Activity > Requests)
	EventDigest   = "digest"   // the weekly summary

	// The two below are opt-in: a target only gets them when it was set up to,
	// so nobody receives messages they never asked for.
	EventClientDown = "clientdown" // a download client can't be reached
	EventClientUp   = "clientup"   // a download client can be reached again
	EventTrashSync  = "trashsync"  // a check of the TRaSH Guides changed custom formats or their scores
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
		{EventAdded, "Added", "A release was sent to the downloader."},
		{EventImported, "Downloaded", "A download finished and joined your library."},
		{EventFailed, "Failed", "A download or import failed."},
		{EventConflict, "Needs attention", "An imported file already exists and needs your decision."},
		{EventSubtitle, "Subtitles", "A subtitle file was downloaded."},
		{EventHealth, "Health", "A Usenet server or indexer stopped working, or came back."},
		{EventUpdate, "New version", "A new version of Mediarium is available."},
		{EventRequest, "Requests", "Someone asked for a movie, show, artist or book."},
		{EventDigest, "Weekly summary", "Once a week: what arrived, what failed and what is still missing."},
		{EventClientDown, "Download client down", "A download client (built-in or external) can't be reached."},
		{EventClientUp, "Download client back", "A download client that was down can be reached again."},
		{EventTrashSync, "TRaSH Guides sync applied", "The update from the TRaSH Guides (automatic or by button) changed custom formats or profile scores."},
	}
}

// OptIn reports whether a kind is only sent to targets that chose it. An empty
// list of events (older targets) means every kind except these.
func OptIn(kind string) bool {
	return kind == EventClientDown || kind == EventClientUp || kind == EventTrashSync
}

// DefaultEvents is what a target subscribes to when it doesn't say.
func DefaultEvents() []string {
	var out []string
	for _, k := range EventKinds() {
		if !OptIn(k.ID) {
			out = append(out, k.ID)
		}
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
	Kind        string   `json:"kind"` // text | password | number | select | push (filled in by the browser)
	Required    bool     `json:"required"`
	Placeholder string   `json:"placeholder,omitempty"`
	Default     string   `json:"default,omitempty"`
	Options     []Option `json:"options,omitempty"`
	Help        string   `json:"help"`
}

// Secret fields are stored encrypted and never returned by the API.
func (f Field) Secret() bool { return f.Kind == "password" || f.Kind == "push" }

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
				Help: "Your provider's outgoing (SMTP) server, like smtp.gmail.com or smtp-mail.outlook.com."},
			{Name: "port", Label: "Port", Kind: "number", Required: true, Placeholder: "587", Default: "587",
				Help: "587 for STARTTLS, 465 for SSL, 25 for none."},
			{Name: "security", Label: "Security", Kind: "select", Required: true, Default: "starttls",
				Options: []Option{{"starttls", "STARTTLS (port 587)"}, {"ssl", "SSL/TLS (port 465)"}, {"none", "None (port 25, not recommended)"}},
				Help:    "How the connection is encrypted. Match it to the port."},
			{Name: "username", Label: "Username", Kind: "text", Placeholder: "you@example.com",
				Help: "Usually your full email address. Empty if no login is needed."},
			{Name: "password", Label: "Password", Kind: "password",
				Help: "Gmail and Outlook need an \"app password\" from your account's security settings."},
			{Name: "fromName", Label: "From name", Kind: "text", Placeholder: "Mediarium", Default: "Mediarium",
				Help: "The name your emails show as coming from. Leave it empty to use Mediarium."},
			{Name: "from", Label: "From address", Kind: "text", Required: true, Placeholder: "you@example.com",
				Help: "The address emails are sent from. Most providers need your own address."},
			{Name: "to", Label: "Send to", Kind: "text", Required: true, Placeholder: "you@example.com",
				Help: "Where notifications go. Separate several addresses with commas."},
		}},
		{Type: string(TargetNtfy), Label: "ntfy", Fields: []Field{
			{Name: "server", Label: "Server", Kind: "text", Placeholder: "https://ntfy.sh", Default: "https://ntfy.sh",
				Help: "Leave as https://ntfy.sh unless you run your own server."},
			{Name: "topic", Label: "Topic", Kind: "password", Required: true, Placeholder: "mediarium-a1b2c3",
				Help: "Pick a name nobody could guess, then subscribe to it in the ntfy app. On a public server anyone with the name can read the messages."},
			{Name: "token", Label: "Access token", Kind: "password",
				Help: "Only for servers with a login. Create one in the ntfy web app under Account > Access tokens."},
		}},
		{Type: string(TargetGotify), Label: "Gotify", Fields: []Field{
			{Name: "url", Label: "Server URL", Kind: "text", Required: true, Placeholder: "https://gotify.example.com",
				Help: "The address you open Gotify at."},
			{Name: "token", Label: "App token", Kind: "password", Required: true,
				Help: "In Gotify, open Apps, create one called Mediarium and copy its token."},
		}},
		{Type: string(TargetPushover), Label: "Pushover", Fields: []Field{
			{Name: "userKey", Label: "User key", Kind: "password", Required: true,
				Help: "Top right of your Pushover dashboard."},
			{Name: "token", Label: "App token", Kind: "password", Required: true,
				Help: "On pushover.net choose \"Create an Application/API Token\", name it Mediarium and copy the token."},
		}},
		{Type: string(TargetSlack), Label: "Slack", Fields: []Field{
			{Name: "url", Label: "Webhook URL", Kind: "password", Required: true, Placeholder: "https://hooks.slack.com/services/...",
				Help: "Create an app at api.slack.com/apps, turn on Incoming Webhooks, add one to a channel and copy the URL."},
		}},
		{Type: string(TargetDiscord), Label: "Discord", Fields: []Field{
			{Name: "url", Label: "Webhook URL", Kind: "password", Required: true, Placeholder: "https://discord.com/api/webhooks/...",
				Help: "In the channel's settings, go to Integrations > Webhooks > New Webhook, then copy the URL."},
		}},
		{Type: string(TargetTelegram), Label: "Telegram", Fields: []Field{
			{Name: "botToken", Label: "Bot token", Kind: "password", Required: true,
				Help: "Message @BotFather, send /newbot and copy the token it replies with."},
			{Name: "chatId", Label: "Chat ID", Kind: "text", Required: true, Placeholder: "123456789",
				Help: "Send your new bot any message, then open https://api.telegram.org/bot<token>/getUpdates and copy the number after \"chat\":{\"id\":."},
		}},
		{Type: string(TargetMatrix), Label: "Matrix", Fields: []Field{
			{Name: "url", Label: "Homeserver address", Kind: "text", Required: true, Placeholder: "https://matrix.example.org",
				Help: "The address of your Matrix server, for example https://matrix.org. It is the one Element shows under Settings > Basics > General."},
			{Name: "roomId", Label: "Room ID", Kind: "text", Required: true, Placeholder: "!abcdefg:example.org",
				Help: "In Element open the room's settings > Advanced and copy the internal room ID. It starts with an exclamation mark. A room alias like #room:example.org will not work."},
			{Name: "accessToken", Label: "Access token", Kind: "password", Required: true,
				Help: "From the account that should post. In Element: Settings > Help & About > Advanced > Access Token. That account must already have joined the room. A separate bot account is best."},
		}},
		{Type: string(TargetHomeAssistant), Label: "Home Assistant", Fields: []Field{
			{Name: "url", Label: "Home Assistant address", Kind: "text", Required: true, Placeholder: "http://homeassistant.local:8123",
				Help: "The address you open Home Assistant at."},
			{Name: "service", Label: "Notify service", Kind: "text", Placeholder: "notify", Default: "notify",
				Help: "The name after notify. in Developer tools > Actions, for example mobile_app_your_phone. Leave it as notify to use notify.notify. Not used with a webhook."},
			{Name: "token", Label: "Long-lived access token", Kind: "password",
				Help: "Create one at the bottom of your profile page in Home Assistant. Needed unless you use a webhook."},
			{Name: "webhookId", Label: "Webhook ID", Kind: "password",
				Help: "Optional. To trigger an automation instead, give its webhook ID. Mediarium then posts the message to /api/webhook/<id> and needs no token."},
		}},
		{Type: string(TargetApprise), Label: "Apprise", Fields: []Field{
			{Name: "server", Label: "Apprise API address", Kind: "text", Required: true, Placeholder: "http://apprise:8000",
				Help: "The address of your Apprise API server."},
			{Name: "key", Label: "Configuration key", Kind: "password",
				Help: "Use this if you saved your services on the Apprise server under a key. Otherwise leave it empty and fill in the addresses below."},
			{Name: "urls", Label: "Service addresses", Kind: "password", Placeholder: "discord://webhook_id/token,mailto://user:pass@gmail.com",
				Help: "One or more Apprise addresses, like discord://... or tgram://..., separated by commas. They hold tokens, so they are stored encrypted. Not used when you give a key."},
			{Name: "tag", Label: "Tag", Kind: "text", Placeholder: "mediarium",
				Help: "Optional. Only send to the services with this tag. Separate several with commas."},
			{Name: "username", Label: "Username", Kind: "text",
				Help: "Only if your Apprise server asks for a login. Empty if not."},
			{Name: "password", Label: "Password", Kind: "password",
				Help: "The password for that username."},
		}},
		{Type: string(TargetPush), Label: "Push to this device", Fields: []Field{
			{Name: "subscription", Label: "This device", Kind: "push", Required: true,
				Help: "Press Allow on the phone or computer that should get the notifications. On an iPhone or iPad, add Mediarium to the Home Screen first and open it from there."},
		}},
		{Type: string(TargetWebhook), Label: "Webhook", Fields: []Field{
			{Name: "url", Label: "URL", Kind: "password", Required: true, Placeholder: "https://example.com/hook",
				Help: "Receives an HTTP POST with a JSON body."},
			{Name: "template", Label: "Custom JSON body", Kind: "text", Placeholder: `{"text": "{{title}}: {{message}}"}`,
				Help: "Optional. Write your own JSON using {{event}}, {{title}}, {{message}}, {{at}}, {{customFormatScore}}, {{customFormats}}, {{releaseGroup}}, {{indexer}}, {{size}} and {{sizeText}}."},
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
			return fmt.Errorf("%s can't be left empty.", f.Label)
		}
		if len(get(f.Name)) > maxFieldLen {
			return fmt.Errorf("%s is too long. Check that you pasted the right thing.", f.Label)
		}
		// A secret that is an address is checked as an address further down,
		// and a push subscription by ParsePushSubscription.
		if f.Secret() && f.Name != "url" && f.Name != "urls" && f.Kind != "push" {
			if msg := inputcheck.APIKey(get(f.Name)); msg != "" {
				return fmt.Errorf("%s: %s", f.Label, msg)
			}
		}
	}
	switch TargetType(typeName) {
	case TargetEmail:
		if msg := inputcheck.Host(get("host"), "smtp.gmail.com"); msg != "" {
			return errors.New(msg)
		}
		if p, err := strconv.Atoi(get("port")); err != nil || p < 1 || p > 65535 {
			return errors.New("Port must be a number between 1 and 65535.")
		}
		switch get("security") {
		case "starttls", "ssl", "none":
		default:
			return errors.New("Pick how the connection is secured: STARTTLS, SSL/TLS or none.")
		}
		if (get("username") == "") != (get("password") == "") {
			return errors.New("Fill in both the username and the password, or leave both empty.")
		}
		if n := get("fromName"); len(n) > 100 || strings.ContainsAny(n, "\r\n") {
			return errors.New("Keep the From name on one line and under 100 characters.")
		}
		if a, err := mail.ParseAddress(get("from")); err != nil || !inputcheck.ValidEmail(a.Address) {
			return errors.New("The From address doesn't look right. It should look like you@example.com or Mediarium <you@example.com>.")
		}
		if len(SplitAddresses(get("to"))) == 0 {
			return errors.New("Add at least one email address to send to, for example you@example.com.")
		}
		for _, addr := range SplitAddresses(get("to")) {
			if a, err := mail.ParseAddress(addr); err != nil || !inputcheck.ValidEmail(a.Address) {
				return fmt.Errorf("%q doesn't look like an email address. It should look like name@example.com.", addr)
			}
		}
	case TargetNtfy:
		if msg := inputcheck.HTTPURL(get("server"), "https://ntfy.sh", true); msg != "" {
			return errors.New(msg)
		}
		if strings.ContainsAny(get("topic"), "/ ?#") {
			return errors.New("The topic can't contain spaces or slashes. Use letters, numbers and dashes, for example mediarium-a1b2c3.")
		}
	case TargetGotify:
		return checkHTTPURL("https://gotify.example.com", get("url"))
	case TargetSlack:
		return checkHTTPURL("https://hooks.slack.com/services/...", get("url"))
	case TargetDiscord:
		return checkHTTPURL("https://discord.com/api/webhooks/...", get("url"))
	case TargetMatrix:
		if err := checkHTTPURL("https://matrix.example.org", get("url")); err != nil {
			return err
		}
		if !matrixRoomID.MatchString(get("roomId")) {
			return errors.New("The room ID should look like !abcdefg:example.org. Aliases that start with # won't work: copy the internal room ID from the room's advanced settings.")
		}
	case TargetHomeAssistant:
		if err := checkHTTPURL("http://homeassistant.local:8123", get("url")); err != nil {
			return err
		}
		if svc := get("service"); svc != "" && !haServiceName.MatchString(svc) {
			return errors.New("The notify service can only have lowercase letters, numbers and underscores, for example mobile_app_your_phone.")
		}
		if id := get("webhookId"); id != "" && !haWebhookID.MatchString(id) {
			return errors.New("The webhook ID can only have letters, numbers, dashes and underscores.")
		}
		if get("webhookId") == "" && get("token") == "" {
			return errors.New("Add a long-lived access token, or a webhook ID if you want to trigger an automation.")
		}
	case TargetApprise:
		if err := checkHTTPURL("http://apprise:8000", get("server")); err != nil {
			return err
		}
		key, urls := get("key"), get("urls")
		switch {
		case key != "" && urls != "":
			return errors.New("Use either a configuration key or service addresses, not both.")
		case key == "" && urls == "":
			return errors.New("Add the service addresses to send to, or the key of a configuration saved on your Apprise server.")
		case key != "" && !appriseKey.MatchString(key):
			return errors.New("The configuration key can only have letters, numbers, dashes and underscores, up to 64 characters.")
		}
		for _, u := range SplitAppriseURLs(urls) {
			if !appriseURL.MatchString(u) {
				return fmt.Errorf("%q isn't an Apprise address. They look like discord://id/token, with the service name first, and are separated by commas.", clipPlain(u, 30))
			}
		}
		if t := get("tag"); len(t) > 100 || strings.ContainsAny(t, "\r\n") {
			return errors.New("Keep the tag on one line and under 100 characters.")
		}
		if (get("username") == "") != (get("password") == "") {
			return errors.New("Fill in both the username and the password, or leave both empty.")
		}
	case TargetPush:
		if _, err := ParsePushSubscription(get("subscription")); err != nil {
			return err
		}
	case TargetTelegram:
		if !telegramChatID.MatchString(get("chatId")) {
			return errors.New("The chat ID should be a number, like 123456789 (groups start with a minus sign), or a channel name like @mychannel.")
		}
	case TargetWebhook:
		if err := checkHTTPURL("https://example.com/hook", get("url")); err != nil {
			return err
		}
		if t := get("template"); t != "" {
			if _, err := renderTemplate(t, Event{Type: "test", Title: "t", Message: "m"}); err != nil {
				return fmt.Errorf("Custom JSON body is not valid JSON once the placeholders are filled in")
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

// maxFieldLen is the longest value any notification field takes.
const maxFieldLen = 4000

// telegramChatID is a numeric chat id (negative for groups) or an @channel name.
var telegramChatID = regexp.MustCompile(`^(-?\d{1,20}|@[A-Za-z][A-Za-z0-9_]{3,31})$`)

// matrixRoomID is a Matrix room id: !opaque:server.
var matrixRoomID = regexp.MustCompile(`^![^\s:/]+:[A-Za-z0-9.\-\[\]:]+$`)

// haServiceName is the part after "notify." in a Home Assistant service name.
var haServiceName = regexp.MustCompile(`^[a-z0-9_]{1,100}$`)

// haWebhookID is what Home Assistant allows in a webhook id.
var haWebhookID = regexp.MustCompile(`^[A-Za-z0-9_\-]{1,255}$`)

// appriseKey is a configuration key on an Apprise API server.
var appriseKey = regexp.MustCompile(`^[A-Za-z0-9_\-]{1,64}$`)

// appriseURL is an Apprise service address: a schema, then ://, then something.
var appriseURL = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.\-]*://\S+$`)

func clipPlain(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "..."
}

// checkHTTPURL wants a full web address that starts with http:// or https://.
func checkHTTPURL(example, raw string) error {
	if msg := inputcheck.HTTPURL(raw, example, true); msg != "" {
		return errors.New(msg)
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

// renderTemplate fills {{event}}, {{title}}, {{message}}, {{at}}, {{subtitles}}
// and the release details ({{customFormatScore}}, {{customFormats}},
// {{releaseGroup}}, {{indexer}}, {{size}} in bytes, {{sizeText}}; empty when
// unknown) into a user-written JSON template, escaping each value for use inside a JSON
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
	score, size := "", ""
	if ev.ScoreKnown {
		score = strconv.Itoa(ev.Score)
	}
	if ev.SizeBytes > 0 {
		size = strconv.FormatInt(ev.SizeBytes, 10)
	}
	body := strings.NewReplacer(
		"{{event}}", esc(ev.Type), "{{title}}", esc(ev.Title), "{{message}}", esc(ev.Message), "{{at}}", esc(atText), "{{subtitles}}", esc(ev.Subtitles),
		"{{customFormatScore}}", esc(score), "{{customFormats}}", esc(strings.Join(ev.Formats, ", ")),
		"{{releaseGroup}}", esc(ev.ReleaseGroup), "{{indexer}}", esc(ev.Indexer), "{{size}}", esc(size), "{{sizeText}}", esc(formatSize(ev.SizeBytes)),
	).Replace(tmpl)
	if !json.Valid([]byte(body)) {
		return "", fmt.Errorf("invalid JSON")
	}
	return body, nil
}
