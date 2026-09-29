// Package notify sends event notifications ("Discord/
// Telegram/webhook/email on events... generic, not Discord-only") for
// things happening in the activity feed: grabbed, downloaded/imported,
// failed.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Event is one activity-feed-worthy occurrence.
type Event struct {
	Type      string // "grabbed" | "imported" | "failed" | "conflict" | "subtitle" | "health" | "test"
	Title     string
	Message   string
	Timestamp time.Time
}

// Sender delivers one Event to one configured destination.
type Sender interface {
	Send(ctx context.Context, event Event) error
}

func doJSONPost(ctx context.Context, client *http.Client, url string, body any) error {
	return doJSONPostHeaders(ctx, client, url, body, nil)
}

// doJSONPostHeaders POSTs body as JSON with extra headers. The URL of a
// webhook, Slack or Telegram target is itself a secret, and net/http's errors
// include it, so failures are reported without it.
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("build request: %w", redactURLError(err))
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", redactURLError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("notification endpoint returned status %d", resp.StatusCode)
	}
	return nil
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
	return &WebhookSender{URL: url, Client: &http.Client{Timeout: 10 * time.Second}}
}

func (s *WebhookSender) Send(ctx context.Context, event Event) error {
	if s.Template != "" {
		body, err := renderTemplate(s.Template, event)
		if err != nil {
			return fmt.Errorf("render webhook body: %w", err)
		}
		return doJSONPost(ctx, s.Client, s.URL, []byte(body))
	}
	at := event.Timestamp.Format(time.RFC3339)
	return doJSONPost(ctx, s.Client, s.URL, map[string]any{
		"event":     event.Type,
		"title":     event.Title,
		"message":   event.Message,
		"at":        at,
		"timestamp": at, // the name earlier versions used
	})
}

// DiscordSender posts to a Discord incoming webhook URL.
type DiscordSender struct {
	WebhookURL string
	Client     *http.Client
}

func NewDiscordSender(webhookURL string) *DiscordSender {
	return &DiscordSender{WebhookURL: webhookURL, Client: &http.Client{Timeout: 10 * time.Second}}
}

func (s *DiscordSender) Send(ctx context.Context, event Event) error {
	return doJSONPost(ctx, s.Client, s.WebhookURL, map[string]any{
		"content": fmt.Sprintf("**%s**\n%s", event.Title, event.Message),
	})
}

// TelegramSender posts via the Telegram Bot API's sendMessage method.
type TelegramSender struct {
	BotToken string
	ChatID   string
	Client   *http.Client
	baseURL  string // overridable for tests
}

func NewTelegramSender(botToken, chatID string) *TelegramSender {
	return &TelegramSender{BotToken: botToken, ChatID: chatID, Client: &http.Client{Timeout: 10 * time.Second}, baseURL: "https://api.telegram.org"}
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
	return doJSONPost(ctx, s.Client, url, map[string]any{
		"chat_id": s.ChatID,
		"text":    fmt.Sprintf("%s\n%s", event.Title, event.Message),
	})
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
