// Package notify sends event notifications ("Discord/
// Telegram/webhook/email on events... generic, not Discord-only") for
// things happening in the activity feed: grabbed, downloaded/imported,
// failed.
package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
	"github.com/rdborg/mediarium/internal/plainerror"
)

// Event is one activity-feed-worthy occurrence.
type Event struct {
	Type      string // "grabbed" | "imported" | "failed" | "conflict" | "subtitle" | "health" | "update" | "test"
	Title     string // the subject: "Titanic (1997) is ready to watch"
	Message   string // the whole message as plain text
	Timestamp time.Time

	// The rest is set by Compose and ComposeText. An event without a Lead is
	// sent as Title and Message only.
	Lead        string   // the sentence under the subject
	Details     []Detail // the rows of the small table
	PosterURL   string   // a hosted poster image, or empty
	LinkURL     string   // where the title is in Mediarium, or empty
	SettingsURL string   // the notification settings in Mediarium, or empty
}

// rich reports whether the event carries the structure Compose adds.
func (e Event) rich() bool { return e.Lead != "" }

// Sender delivers one Event to one configured destination.
type Sender interface {
	Send(ctx context.Context, event Event) error
}

func doJSONPost(ctx context.Context, client *http.Client, url string, body any) error {
	return doJSONPostHeaders(ctx, client, url, body, nil)
}

// doJSONPostHeaders POSTs body as JSON with extra headers. The URL of a
// webhook, Slack or Telegram target is itself a secret, and net/http's errors
// include it, so failures are reported without it. When ctx carries a Steps
// log (see WithSteps) every stage is written to it, again without the URL:
// only the server's name shows.
func doJSONPostHeaders(ctx context.Context, client *http.Client, target string, body any, headers map[string]string) error {
	var raw []byte
	switch v := body.(type) {
	case []byte:
		raw = v
	default:
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}
		raw = b
	}
	steps := stepsFrom(ctx)
	host := "the server"
	if u, err := url.Parse(target); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	failed := false
	if steps != nil {
		steps.Add(true, "Preparing the message for %s", host)
		ctx = traceRequest(ctx, steps, host, &failed)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		steps.Add(false, "The address is not valid")
		return fmt.Errorf("build request: %w", redactURLError(err))
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		if !failed {
			steps.Add(false, "%s", failureText(redactURLError(err)))
		}
		return fmt.Errorf("send request: %w", redactURLError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		steps.Add(false, "%s", statusText(resp.StatusCode, string(snippet)))
		return errors.New(statusText(resp.StatusCode, ""))
	}
	steps.Add(true, "Done: accepted by the server (%d)", resp.StatusCode)
	return nil
}

// traceRequest writes the connection stages of one request into steps.
func traceRequest(ctx context.Context, steps *Steps, host string, failed *bool) context.Context {
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GetConn: func(hostPort string) { steps.Add(true, "Connecting to %s", hostPort) },
		DNSDone: func(i httptrace.DNSDoneInfo) {
			if i.Err != nil {
				*failed = true
				steps.Add(false, "Could not find %s: the name does not exist or there is no internet", host)
			}
		},
		ConnectDone: func(_, _ string, err error) {
			if err != nil {
				if !*failed {
					steps.Add(false, "%s", failureText(err))
				}
				*failed = true
				return
			}
			steps.Add(true, "Connected")
		},
		TLSHandshakeDone: func(cs tls.ConnectionState, err error) {
			if err != nil {
				*failed = true
				steps.Add(false, "Could not start a secure connection: %s", plainerror.Message(err))
				return
			}
			steps.Add(true, "Secure connection started (%s)", tlsVersionName(cs.Version))
		},
		WroteRequest: func(i httptrace.WroteRequestInfo) {
			if i.Err == nil {
				steps.Add(true, "Sent the message, waiting for the answer")
			}
		},
	})
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS13:
		return "TLS 1.3"
	case tls.VersionTLS12:
		return "TLS 1.2"
	}
	return "TLS"
}

// failureText is a failed connection in the words the log uses.
func failureText(err error) string {
	msg := plainerror.Message(err)
	low := strings.ToLower(err.Error())
	switch {
	case strings.Contains(low, "connection refused"):
		return "Could not connect: connection refused. Nothing is listening at that address and port."
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(low, "timeout") || strings.Contains(low, "timed out"):
		return "Could not connect: no answer in time."
	case strings.Contains(low, "address not allowed"):
		return "Could not connect: that address is not allowed."
	}
	if msg == "" {
		return "Could not connect."
	}
	return "Could not connect: " + msg
}

// statusText explains an HTTP answer that is not a success. The start of what
// the server said is added when there is one.
func statusText(code int, body string) string {
	var what string
	switch {
	case code == 400:
		what = "The server did not accept the message"
	case code == 401 || code == 403:
		what = "Refused: wrong token, key or password"
	case code == 404 || code == 410:
		what = "Not found: check the address, topic or chat"
	case code == 413:
		what = "The message was too large"
	case code == 429:
		what = "Too many messages: try again in a while"
	case code >= 500:
		what = "The server had a problem on its side"
	default:
		what = "The server answered with an error"
	}
	out := fmt.Sprintf("%s (%d)", what, code)
	if s := strings.TrimSpace(strings.Join(strings.Fields(body), " ")); s != "" && !strings.HasPrefix(s, "<") {
		out += ". It said: " + clip(s, 160)
	}
	return out
}

// redactURLError drops the request URL that *url.Error carries.
func redactURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// WebhookSender POSTs a generic JSON payload — the "not Discord-only"
// fallback that works with anything expecting a plain JSON webhook.
type WebhookSender struct {
	URL    string
	Client *http.Client
	// Template, when set, is a JSON body with {{event}}, {{title}}, {{message}}
	// and {{at}} placeholders; empty sends the fixed {event,title,message,at}.
	Template string
}

func NewWebhookSender(url string) *WebhookSender {
	return &WebhookSender{URL: url, Client: netguard.Client(10 * time.Second)}
}

func (s *WebhookSender) Send(ctx context.Context, event Event) error {
	if s.Template != "" {
		body, err := renderTemplate(s.Template, event)
		if err != nil {
			stepsFrom(ctx).Add(false, "Your custom JSON body is not valid once the placeholders are filled in")
			return fmt.Errorf("render webhook body: %w", err)
		}
		return doJSONPost(ctx, s.Client, s.URL, []byte(body))
	}
	at := event.Timestamp.Format(time.RFC3339)
	payload := map[string]any{
		"event":     event.Type,
		"title":     event.Title,
		"message":   event.Message,
		"at":        at,
		"timestamp": at, // the name earlier versions used
	}
	if event.rich() {
		payload["details"] = event.Details
		if event.LinkURL != "" {
			payload["link"] = event.LinkURL
		}
		if event.PosterURL != "" {
			payload["poster"] = event.PosterURL
		}
	}
	return doJSONPost(ctx, s.Client, s.URL, payload)
}

// DiscordSender posts to a Discord incoming webhook URL.
type DiscordSender struct {
	WebhookURL string
	Client     *http.Client
}

func NewDiscordSender(webhookURL string) *DiscordSender {
	return &DiscordSender{WebhookURL: webhookURL, Client: netguard.Client(10 * time.Second)}
}

func (s *DiscordSender) Send(ctx context.Context, event Event) error {
	// Titles come from indexers: one containing @everyone must not ping the channel.
	noMentions := map[string]any{"parse": []string{}}
	if !event.rich() {
		return doJSONPost(ctx, s.Client, s.WebhookURL, map[string]any{
			"content":          fmt.Sprintf("**%s**\n%s", event.Title, event.Message),
			"allowed_mentions": noMentions,
		})
	}
	return doJSONPost(ctx, s.Client, s.WebhookURL, map[string]any{
		"embeds":           []any{discordEmbed(event)},
		"allowed_mentions": noMentions,
	})
}

// discordEmbed lays the event out as one embed: subject, sentence, a row per
// detail, the poster as a thumbnail and a link to the title.
func discordEmbed(ev Event) map[string]any {
	embed := map[string]any{
		"title":       clip(ev.Title, 250),
		"description": clip(ev.Lead, 2000),
		"color":       eventColor(ev),
		"footer":      map[string]any{"text": "Mediarium"},
	}
	if !ev.Timestamp.IsZero() {
		embed["timestamp"] = ev.Timestamp.UTC().Format(time.RFC3339)
	}
	if u := httpsURL(ev.LinkURL); u != "" {
		embed["url"] = u
	}
	if u := httpsURL(ev.PosterURL); u != "" {
		embed["thumbnail"] = map[string]any{"url": u}
	}
	var fields []map[string]any
	for _, d := range ev.Details {
		if d.Label == "Time" {
			continue // the embed's own timestamp says it
		}
		if len(fields) == 25 {
			break
		}
		fields = append(fields, map[string]any{"name": clip(d.Label, 250), "value": clip(d.Value, 1000), "inline": len(d.Value) <= 24})
	}
	if len(fields) > 0 {
		embed["fields"] = fields
	}
	return embed
}

// TelegramSender posts via the Telegram Bot API's sendMessage method.
type TelegramSender struct {
	BotToken string
	ChatID   string
	Client   *http.Client
	baseURL  string // overridable for tests
}

func NewTelegramSender(botToken, chatID string) *TelegramSender {
	return &TelegramSender{BotToken: botToken, ChatID: chatID, Client: netguard.Client(10 * time.Second), baseURL: "https://api.telegram.org"}
}

// NewTelegramSenderWithBaseURL is used by tests to point at a local
// fixture server instead of the real Telegram Bot API (tests use local
// fixtures, not live network calls) — same pattern as
// internal/metadata.NewWithBaseURL.
func NewTelegramSenderWithBaseURL(botToken, chatID, baseURL string) *TelegramSender {
	s := NewTelegramSender(botToken, chatID)
	s.baseURL = baseURL
	return s
}

func (s *TelegramSender) Send(ctx context.Context, event Event) error {
	url := fmt.Sprintf("%s/bot%s/sendMessage", s.baseURL, s.BotToken)
	body := map[string]any{"chat_id": s.ChatID}
	if event.rich() {
		body["text"] = telegramHTML(event)
		body["parse_mode"] = "HTML"
		body["disable_web_page_preview"] = true
	} else {
		body["text"] = fmt.Sprintf("%s\n%s", event.Title, event.Message)
	}
	return doJSONPost(ctx, s.Client, url, body)
}

// telegramHTML is the message in the small HTML Telegram accepts: a bold
// subject, the sentence, a line per detail and a link.
func telegramHTML(ev Event) string {
	e := html.EscapeString
	var b strings.Builder
	b.WriteString("<b>" + e(ev.Title) + "</b>\n" + e(ev.Lead))
	if len(ev.Details) > 0 {
		b.WriteString("\n")
		for _, d := range ev.Details {
			b.WriteString("\n<b>" + e(d.Label) + ":</b> " + e(d.Value))
		}
	}
	if u := httpsURL(ev.LinkURL); u != "" {
		b.WriteString("\n\n<a href=\"" + e(u) + "\">Open in Mediarium</a>")
	}
	return b.String()
}

// NotifyAll sends event to every sender, collecting (not stopping on) the
// first error — one bad notification target shouldn't suppress the
// others, mirroring the indexer search engine's tolerate-one-failure
// design.
func NotifyAll(ctx context.Context, senders []Sender, event Event) []error {
	var errs []error
	for _, s := range senders {
		if err := s.Send(ctx, event); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}
