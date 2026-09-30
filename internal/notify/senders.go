package notify

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
)

func newHTTPClient() *http.Client { return netguard.Client(10 * time.Second) }

// priorityFor maps an event to a 1-5 push priority (ntfy and Pushover scales
// agree closely enough): problems are louder than good news.
func priorityFor(ev Event) int {
	switch EventKind(ev.Type) {
	case EventFailed, EventHealth, EventConflict:
		return 4
	default:
		return 3
	}
}

// NtfySender publishes to an ntfy topic (ntfy.sh or a self-hosted server)
// using its JSON publish API, so non-ASCII titles need no header encoding.
type NtfySender struct {
	Server string // base URL, default https://ntfy.sh
	Topic  string
	Token  string // optional bearer token
	Client *http.Client
}

func NewNtfySender(server, topic, token string) *NtfySender {
	if server == "" {
		server = "https://ntfy.sh"
	}
	return &NtfySender{Server: strings.TrimRight(server, "/"), Topic: topic, Token: token, Client: newHTTPClient()}
}

func (s *NtfySender) Send(ctx context.Context, ev Event) error {
	headers := map[string]string{}
	if s.Token != "" {
		headers["Authorization"] = "Bearer " + s.Token
	}
	body := map[string]any{
		"topic":    s.Topic,
		"title":    ev.Title,
		"message":  ev.Message,
		"priority": priorityFor(ev),
	}
	if ev.rich() {
		body["tags"] = []string{eventTag(ev)}
		if u := httpsURL(ev.LinkURL); u != "" {
			body["click"] = u
		}
		if u := httpsURL(ev.PosterURL); u != "" {
			body["icon"] = u
		}
	}
	return doJSONPostHeaders(ctx, s.Client, s.Server+"/", body, headers)
}

// GotifySender pushes to a Gotify server with an application token.
type GotifySender struct {
	URL    string
	Token  string
	Client *http.Client
}

func NewGotifySender(url, token string) *GotifySender {
	return &GotifySender{URL: strings.TrimRight(url, "/"), Token: token, Client: newHTTPClient()}
}

func (s *GotifySender) Send(ctx context.Context, ev Event) error {
	// The token travels in a header, not the query string, so it can't end up
	// in an error message or a proxy log.
	body := map[string]any{
		"title":    ev.Title,
		"message":  ev.Message,
		"priority": priorityFor(ev) + 1, // Gotify: 4 is "normal-high", 5+ rings
	}
	if ev.rich() {
		var md strings.Builder
		md.WriteString(mdEscape(ev.Lead))
		if len(ev.Details) > 0 {
			md.WriteString("\n")
			for _, d := range ev.Details {
				md.WriteString("\n**" + mdEscape(d.Label) + ":** " + mdEscape(d.Value) + "  ")
			}
		}
		body["message"] = md.String()
		extras := map[string]any{"client::display": map[string]any{"contentType": "text/markdown"}}
		if u := httpsURL(ev.LinkURL); u != "" {
			extras["client::notification"] = map[string]any{"click": map[string]any{"url": u}}
		}
		body["extras"] = extras
	}
	return doJSONPostHeaders(ctx, s.Client, s.URL+"/message", body, map[string]string{"X-Gotify-Key": s.Token})
}

// PushoverSender sends through the Pushover message API.
type PushoverSender struct {
	UserKey string
	Token   string
	Client  *http.Client
	baseURL string // overridable for tests
}

func NewPushoverSender(userKey, token string) *PushoverSender {
	return &PushoverSender{UserKey: userKey, Token: token, Client: newHTTPClient(), baseURL: "https://api.pushover.net"}
}

// NewPushoverSenderWithBaseURL points the sender at a fixture server in tests.
func NewPushoverSenderWithBaseURL(userKey, token, baseURL string) *PushoverSender {
	s := NewPushoverSender(userKey, token)
	s.baseURL = strings.TrimRight(baseURL, "/")
	return s
}

func (s *PushoverSender) Send(ctx context.Context, ev Event) error {
	prio := 0 // Pushover: 0 normal, 1 high
	if priorityFor(ev) >= 4 {
		prio = 1
	}
	body := map[string]any{
		"token":    s.Token,
		"user":     s.UserKey,
		"title":    ev.Title,
		"message":  ev.Message,
		"priority": prio,
	}
	if ev.rich() {
		e := html.EscapeString
		var msg strings.Builder
		msg.WriteString(e(ev.Lead))
		for _, d := range ev.Details {
			msg.WriteString("\n<b>" + e(d.Label) + ":</b> " + e(d.Value))
		}
		body["message"], body["html"] = msg.String(), 1
		if u := httpsURL(ev.LinkURL); u != "" {
			body["url"], body["url_title"] = u, "Open in Mediarium"
		}
	}
	return doJSONPost(ctx, s.Client, s.baseURL+"/1/messages.json", body)
}

// SlackSender posts to a Slack incoming webhook.
type SlackSender struct {
	WebhookURL string
	Client     *http.Client
}

func NewSlackSender(webhookURL string) *SlackSender {
	return &SlackSender{WebhookURL: webhookURL, Client: newHTTPClient()}
}

func (s *SlackSender) Send(ctx context.Context, ev Event) error {
	if !ev.rich() {
		return doJSONPost(ctx, s.Client, s.WebhookURL, map[string]any{
			"text": fmt.Sprintf("*%s*\n%s", ev.Title, ev.Message),
		})
	}
	return doJSONPost(ctx, s.Client, s.WebhookURL, slackBlocks(ev))
}

// slackBlocks lays the event out with Slack's block kit: a header, the
// sentence, the rows as two columns, and a button. "text" is what shows in a
// notification pop-up.
func slackBlocks(ev Event) map[string]any {
	blocks := []any{
		map[string]any{"type": "header", "text": map[string]any{"type": "plain_text", "text": clip(ev.Title, 150)}},
		map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": clip(slackEscape(ev.Lead), 2900)}},
	}
	var fields []any
	for _, d := range ev.Details {
		if len(fields) == 10 {
			break
		}
		fields = append(fields, map[string]any{"type": "mrkdwn", "text": clip("*"+slackEscape(d.Label)+"*\n"+slackEscape(d.Value), 1900)})
	}
	if len(fields) > 0 {
		blocks = append(blocks, map[string]any{"type": "section", "fields": fields})
	}
	if u := httpsURL(ev.LinkURL); u != "" {
		blocks = append(blocks, map[string]any{"type": "actions", "elements": []any{
			map[string]any{"type": "button", "text": map[string]any{"type": "plain_text", "text": "Open in Mediarium"}, "url": u},
		}})
	}
	blocks = append(blocks, map[string]any{"type": "context", "elements": []any{map[string]any{"type": "mrkdwn", "text": "Mediarium"}}})
	return map[string]any{"text": ev.Title, "blocks": blocks}
}
