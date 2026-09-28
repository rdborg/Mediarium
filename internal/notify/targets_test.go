package notify_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryanborg/mediarium/internal/crypto"
	"github.com/ryanborg/mediarium/internal/notify"
	"github.com/ryanborg/mediarium/internal/store"
)

type captured struct {
	path    string
	query   string
	headers http.Header
	body    string
}

func captureServer(t *testing.T, status int) (*httptest.Server, *captured) {
	t.Helper()
	got := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.path, got.query, got.headers, got.body = r.URL.Path, r.URL.RawQuery, r.Header.Clone(), string(b)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("body is not JSON: %q: %v", s, err)
	}
	return m
}

func TestNtfySender(t *testing.T) {
	srv, got := captureServer(t, 200)
	if err := notify.NewNtfySender(srv.URL+"/", "my-topic", "tk_secret").Send(context.Background(), fixtureEvent); err != nil {
		t.Fatal(err)
	}
	body := decode(t, got.body)
	if got.path != "/" || body["topic"] != "my-topic" || body["title"] != fixtureEvent.Title || body["message"] != fixtureEvent.Message {
		t.Fatalf("unexpected request %s %+v", got.path, body)
	}
	if got.headers.Get("Authorization") != "Bearer tk_secret" {
		t.Fatalf("expected bearer token, got %q", got.headers.Get("Authorization"))
	}

	srv2, got2 := captureServer(t, 200)
	if err := notify.NewNtfySender(srv2.URL, "t", "").Send(context.Background(), fixtureEvent); err != nil {
		t.Fatal(err)
	}
	if got2.headers.Get("Authorization") != "" {
		t.Fatal("no token, no Authorization header")
	}
}

func TestGotifySender(t *testing.T) {
	srv, got := captureServer(t, 200)
	if err := notify.NewGotifySender(srv.URL+"/", "AppTok").Send(context.Background(), fixtureEvent); err != nil {
		t.Fatal(err)
	}
	if got.path != "/message" || got.query != "" || got.headers.Get("X-Gotify-Key") != "AppTok" {
		t.Fatalf("the token belongs in a header, not the URL: %+v", got)
	}
	body := decode(t, got.body)
	if body["title"] != fixtureEvent.Title || body["message"] != fixtureEvent.Message {
		t.Fatalf("unexpected body %+v", body)
	}
}

func TestPushoverSender(t *testing.T) {
	srv, got := captureServer(t, 200)
	if err := notify.NewPushoverSenderWithBaseURL("uk", "at", srv.URL).Send(context.Background(), fixtureEvent); err != nil {
		t.Fatal(err)
	}
	body := decode(t, got.body)
	if got.path != "/1/messages.json" || body["user"] != "uk" || body["token"] != "at" || body["message"] != fixtureEvent.Message {
		t.Fatalf("unexpected request %s %+v", got.path, body)
	}
}

func TestSlackSender(t *testing.T) {
	srv, got := captureServer(t, 200)
	if err := notify.NewSlackSender(srv.URL).Send(context.Background(), fixtureEvent); err != nil {
		t.Fatal(err)
	}
	text, _ := decode(t, got.body)["text"].(string)
	if !strings.Contains(text, fixtureEvent.Title) || !strings.Contains(text, fixtureEvent.Message) {
		t.Fatalf("unexpected slack text %q", text)
	}
}

func TestWebhookFixedBodyAndTemplate(t *testing.T) {
	srv, got := captureServer(t, 200)
	if err := notify.NewWebhookSender(srv.URL).Send(context.Background(), fixtureEvent); err != nil {
		t.Fatal(err)
	}
	body := decode(t, got.body)
	if body["event"] != "imported" || body["at"] != "2024-01-02T03:04:05Z" || body["title"] != fixtureEvent.Title {
		t.Fatalf("unexpected fixed body %+v", body)
	}

	w := notify.NewWebhookSender(srv.URL)
	w.Template = `{"text": "{{title}}: {{message}}", "kind": "{{event}}", "when": "{{at}}"}`
	ev := notify.Event{Type: "failed", Title: `He said "hi"`, Message: "line1\nline2 \\ end", Timestamp: fixtureEvent.Timestamp}
	if err := w.Send(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	body = decode(t, got.body)
	if body["text"] != "He said \"hi\": line1\nline2 \\ end" || body["kind"] != "failed" || body["when"] != "2024-01-02T03:04:05Z" {
		t.Fatalf("template values must be escaped into valid JSON: %q -> %+v", got.body, body)
	}
}

func TestSenderErrorsNeverLeakTheSecretURL(t *testing.T) {
	// A closed port makes the request itself fail, and net/http would normally
	// put the whole URL (a webhook's secret) into the error text.
	srv, _ := captureServer(t, 200)
	secretURL := srv.URL + "/services/T000/B000/SECRETSECRET"
	srv.Close()
	for name, s := range map[string]notify.Sender{
		"slack":   notify.NewSlackSender(secretURL),
		"discord": notify.NewDiscordSender(secretURL),
		"webhook": notify.NewWebhookSender(secretURL),
	} {
		err := s.Send(context.Background(), fixtureEvent)
		if err == nil {
			t.Fatalf("%s: expected an error", name)
		}
		if strings.Contains(err.Error(), "SECRETSECRET") {
			t.Fatalf("%s: error leaks the secret URL: %v", name, err)
		}
	}

	bad, _ := captureServer(t, 500)
	if err := notify.NewSlackSender(bad.URL).Send(context.Background(), fixtureEvent); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected a status error, got %v", err)
	}
}

func TestValidate(t *testing.T) {
	good := map[string]map[string]string{
		"email":    {"host": "smtp.example.com", "port": "587", "security": "starttls", "username": "u", "password": "p", "from": "Me <me@example.com>", "to": "a@example.com, b@example.com"},
		"ntfy":     {"topic": "abc"},
		"gotify":   {"url": "https://gotify.example.com", "token": "t"},
		"pushover": {"userKey": "u", "token": "t"},
		"slack":    {"url": "https://hooks.slack.com/services/x"},
		"discord":  {"url": "https://discord.com/api/webhooks/x"},
		"telegram": {"botToken": "1:a", "chatId": "5"},
		"webhook":  {"url": "https://example.com/h", "template": `{"m": "{{message}}"}`},
	}
	for typ, cfg := range good {
		if err := notify.Validate(typ, notify.ApplyDefaults(typ, cfg)); err != nil {
			t.Errorf("%s should be valid: %v", typ, err)
		}
	}

	bad := []struct {
		name string
		typ  string
		cfg  map[string]string
		want string
	}{
		{"unknown type", "carrier-pigeon", nil, "unknown"},
		{"email missing host", "email", map[string]string{"port": "587", "security": "ssl", "from": "a@b.co", "to": "c@d.co"}, "SMTP server"},
		{"email bad port", "email", map[string]string{"host": "h", "port": "99999", "security": "ssl", "from": "a@b.co", "to": "c@d.co"}, "Port"},
		{"email bad security", "email", map[string]string{"host": "h", "port": "25", "security": "tls13", "from": "a@b.co", "to": "c@d.co"}, "Security"},
		{"email host with scheme", "email", map[string]string{"host": "smtp://h", "port": "25", "security": "none", "from": "a@b.co", "to": "c@d.co"}, "host name"},
		{"email user without password", "email", map[string]string{"host": "h", "port": "25", "security": "none", "username": "u", "from": "a@b.co", "to": "c@d.co"}, "Username and Password"},
		{"email bad to", "email", map[string]string{"host": "h", "port": "25", "security": "none", "from": "a@b.co", "to": "a@b.co, nope"}, "nope"},
		{"ntfy no topic", "ntfy", map[string]string{"server": "https://ntfy.sh"}, "Topic"},
		{"ntfy bad server", "ntfy", map[string]string{"topic": "t", "server": "ntfy.sh"}, "http"},
		{"gotify no token", "gotify", map[string]string{"url": "https://g.example.com"}, "App token"},
		{"slack not a url", "slack", map[string]string{"url": "hooks"}, "http"},
		{"webhook bad template", "webhook", map[string]string{"url": "https://e.co", "template": `{"a": {{message}}`}, "JSON"},
	}
	for _, tc := range bad {
		err := notify.Validate(tc.typ, notify.ApplyDefaults(tc.typ, tc.cfg))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: expected an error containing %q, got %v", tc.name, tc.want, err)
		}
	}
}

func TestCatalogIsSelfConsistent(t *testing.T) {
	for _, typ := range notify.Types() {
		if typ.Label == "" || len(typ.Fields) == 0 {
			t.Errorf("%s needs a label and fields", typ.Type)
		}
		for _, f := range typ.Fields {
			switch f.Kind {
			case "text", "password", "number", "select":
			default:
				t.Errorf("%s.%s has unknown kind %q", typ.Type, f.Name, f.Kind)
			}
			if f.Help == "" || f.Label == "" {
				t.Errorf("%s.%s needs a label and help text", typ.Type, f.Name)
			}
			if (f.Kind == "select") != (len(f.Options) > 0) {
				t.Errorf("%s.%s: options belong to select fields only", typ.Type, f.Name)
			}
		}
	}
}

func TestEventNormalizationAndMapping(t *testing.T) {
	if notify.EventKind("grabbed") != "added" || notify.EventKind("imported") != "imported" {
		t.Fatal("grabbed should be delivered as the added kind")
	}
	got, err := notify.NormalizeEvents([]string{"health", "Failed", "health"})
	if err != nil || strings.Join(got, ",") != "failed,health" {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := notify.NormalizeEvents([]string{"lunch"}); err == nil {
		t.Fatal("unknown events must be rejected")
	}
	if def, _ := notify.NormalizeEvents(nil); len(def) != len(notify.EventKinds()) {
		t.Fatalf("empty means all events, got %v", def)
	}
}

func newTestRepoWithDB(t *testing.T) (*notify.Repo, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := crypto.LoadOrCreateKey(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	return notify.NewRepo(db, box), db
}

func TestRepoEventsSecretsAndUpdate(t *testing.T) {
	repo, db := newTestRepoWithDB(t)

	email, err := repo.Create(notify.Target{Name: "Mail", Type: notify.TargetEmail, Events: []string{"failed", "health"}, Config: map[string]string{
		"host": "smtp.example.com", "port": "587", "security": "starttls", "username": "u", "password": "s3cret", "from": "a@b.co", "to": "c@d.co",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(notify.Target{Name: "Hook", Type: notify.TargetWebhook, URL: "https://example.com/h"}); err != nil {
		t.Fatal(err)
	}

	// Only the subscribed target is built for an event.
	failed, _ := repo.SendersFor("failed")
	imported, _ := repo.SendersFor("imported")
	if len(failed) != 2 || len(imported) != 1 {
		t.Fatalf("subscriptions not honoured: failed=%d imported=%d", len(failed), len(imported))
	}

	got, err := repo.Get(email.ID)
	if err != nil || got.Config["password"] != "s3cret" || strings.Join(got.Events, ",") != "failed,health" {
		t.Fatalf("round trip: %+v %v", got, err)
	}

	// The password must not sit in the plain-text config column.
	var cfgJSON, secEnc string
	if err := db.QueryRow(`SELECT config_json, secrets_encrypted FROM notification_targets WHERE id = ?`, email.ID).Scan(&cfgJSON, &secEnc); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfgJSON, "s3cret") || strings.Contains(secEnc, "s3cret") || secEnc == "" {
		t.Fatalf("secret stored in the clear? config=%q secrets=%q", cfgJSON, secEnc)
	}

	// Updating with a blank password keeps the stored one.
	upd, err := repo.Update(notify.Target{ID: email.ID, Name: "Mail 2", Type: notify.TargetEmail, Enabled: false, Events: []string{"added"}, Config: map[string]string{
		"host": "smtp2.example.com", "port": "465", "security": "ssl", "username": "u", "from": "a@b.co", "to": "c@d.co",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if upd.Config["password"] != "s3cret" || upd.Config["host"] != "smtp2.example.com" || upd.Enabled || upd.Name != "Mail 2" {
		t.Fatalf("unexpected update result: %+v", upd)
	}
	if senders, _ := repo.SendersFor("added"); len(senders) != 1 {
		t.Fatalf("a disabled target must not be built, got %d senders", len(senders))
	}
}
