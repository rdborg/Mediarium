package notify

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func newHTTPClient() *http.Client { return &http.Client{Timeout: 10 * time.Second} }

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
	return doJSONPostHeaders(ctx, s.Client, s.Server+"/", map[string]any{
		"topic":    s.Topic,
		"title":    ev.Title,
		"message":  ev.Message,
		"priority": priorityFor(ev),
	}, headers)
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
	return doJSONPostHeaders(ctx, s.Client, s.URL+"/message", map[string]any{
		"title":    ev.Title,
		"message":  ev.Message,
		"priority": priorityFor(ev) + 1, // Gotify: 4 is "normal-high", 5+ rings
	}, map[string]string{"X-Gotify-Key": s.Token})
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
	return doJSONPost(ctx, s.Client, s.baseURL+"/1/messages.json", map[string]any{
		"token":    s.Token,
		"user":     s.UserKey,
		"title":    ev.Title,
		"message":  ev.Message,
		"priority": prio,
	})
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
	return doJSONPost(ctx, s.Client, s.WebhookURL, map[string]any{
		"text": fmt.Sprintf("*%s*\n%s", ev.Title, ev.Message),
	})
}
