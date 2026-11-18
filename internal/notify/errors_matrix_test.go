package notify_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/notify"
)

// Whatever a service or a proxy answers, a sender that cannot deliver says so,
// in words, without the token or key in them, and does not claim success when a
// login page answered 200 after a redirect.
func TestSendersReportEveryBadAnswerWithoutTheSecret(t *testing.T) {
	const secret = "s3cret-token-abcdef123456"
	answers := []struct {
		name    string
		handler http.HandlerFunc
		fails   bool
	}{
		{"401", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "bad "+secret, 401) }, true},
		{"403", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 403) }, true},
		{"404", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }, true},
		{"410", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "gone", 410) }, true},
		{"500", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom "+secret, 500) }, true},
		{"503", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "later", 503) }, true},
		{"a redirect to a login page", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/login" {
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}
			w.Write([]byte("<html>Sign in</html>"))
		}, true},
		{"a redirect that keeps the message", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/moved" && !strings.HasSuffix(r.URL.Path, "/moved") {
				http.Redirect(w, r, r.URL.Path+"/moved", http.StatusTemporaryRedirect)
				return
			}
			w.Write([]byte("{}"))
		}, false},
		{"slow", func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(3 * time.Second):
			}
		}, true},
		{"fine", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"status":1,"ok":true,"result":{}}`)) }, false},
	}
	for _, a := range answers {
		t.Run(a.name, func(t *testing.T) {
			srv := httptest.NewServer(a.handler)
			defer srv.Close()
			senders := map[string]notify.Sender{
				"webhook":       notify.NewWebhookSender(srv.URL + "/hook/" + secret),
				"discord":       notify.NewDiscordSender(srv.URL + "/api/webhooks/1/" + secret),
				"slack":         notify.NewSlackSender(srv.URL + "/services/" + secret),
				"ntfy":          notify.NewNtfySender(srv.URL, "topic", secret),
				"gotify":        notify.NewGotifySender(srv.URL, secret),
				"pushover":      notify.NewPushoverSenderWithBaseURL("user", secret, srv.URL),
				"telegram":      notify.NewTelegramSenderWithBaseURL(secret, "123", srv.URL),
				"matrix":        notify.NewMatrixSender(srv.URL, "!room:example.org", secret),
				"apprise":       notify.NewAppriseSender(srv.URL, "key", "", "", "", secret),
				"homeassistant": notify.NewHomeAssistantSender(srv.URL, "notify.mobile", secret, ""),
			}
			for name, s := range senders {
				ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
				err := s.Send(ctx, fixtureEvent)
				cancel()
				if a.fails && err == nil {
					t.Errorf("%s: claimed to deliver on %q", name, a.name)
				}
				if err != nil {
					if strings.Contains(err.Error(), secret) {
						t.Errorf("%s: the secret is in the error: %v", name, err)
					}
					for _, bad := range []string{"EOF", "invalid character"} {
						if strings.Contains(err.Error(), bad) {
							t.Errorf("%s: parser text in the error: %v", name, err)
						}
					}
				}
				if !a.fails && err != nil && a.name != "fine" {
					t.Logf("%s: %v", name, err)
				}
			}
		})
	}
}
