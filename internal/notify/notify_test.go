package notify_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ryanborg/mediarium/internal/notify"
)

var fixtureEvent = notify.Event{
	Type:      "imported",
	Title:     "The Matrix (1999)",
	Message:   "Imported to /movies/The Matrix (1999)/The Matrix (1999).mkv",
	Timestamp: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
}

func TestWebhookSender(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected JSON content type, got %s", r.Header.Get("Content-Type"))
		}
		json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()

	sender := notify.NewWebhookSender(srv.URL)
	if err := sender.Send(context.Background(), fixtureEvent); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got["event"] != "imported" || got["title"] != fixtureEvent.Title || got["message"] != fixtureEvent.Message {
		t.Fatalf("unexpected payload: %+v", got)
	}
}

func TestDiscordSender(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()

	sender := notify.NewDiscordSender(srv.URL)
	if err := sender.Send(context.Background(), fixtureEvent); err != nil {
		t.Fatalf("send: %v", err)
	}
	content, _ := got["content"].(string)
	if content == "" {
		t.Fatal("expected non-empty discord content")
	}
	if !strings.Contains(content, fixtureEvent.Title) || !strings.Contains(content, fixtureEvent.Message) {
		t.Fatalf("expected discord content to include title and message, got %q", content)
	}
}

func TestTelegramSenderFormatsBotAPIURL(t *testing.T) {
	var gotPath string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()

	sender := notify.NewTelegramSenderWithBaseURL("test-token", "12345", srv.URL)

	if err := sender.Send(context.Background(), fixtureEvent); err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotPath != "/bottest-token/sendMessage" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if got["chat_id"] != "12345" {
		t.Fatalf("unexpected chat_id: %v", got["chat_id"])
	}
}

func TestNotifyAllCollectsErrorsWithoutStopping(t *testing.T) {
	goodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer goodSrv.Close()
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badSrv.Close()

	senders := []notify.Sender{
		notify.NewWebhookSender(badSrv.URL),
		notify.NewWebhookSender(goodSrv.URL),
	}
	errs := notify.NotifyAll(context.Background(), senders, fixtureEvent)
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 error (from the bad sender), got %d: %v", len(errs), errs)
	}
}
