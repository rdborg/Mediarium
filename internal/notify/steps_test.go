package notify_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/notify"
)

func texts(steps *notify.Steps) []string {
	var out []string
	for _, s := range steps.List() {
		out = append(out, s.Text)
	}
	return out
}

func lastStep(t *testing.T, steps *notify.Steps) notify.Step {
	t.Helper()
	l := steps.List()
	if len(l) == 0 {
		t.Fatal("no steps were written")
	}
	return l[len(l)-1]
}

func hasLine(lines []string, want string) bool {
	for _, l := range lines {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}

func TestStepsScrubSecretsAndIgnoreNil(t *testing.T) {
	var none *notify.Steps
	none.Add(true, "nothing happens") // must not panic
	if none.List() != nil {
		t.Fatal("a nil log should stay empty")
	}

	st := notify.NewSteps("hunter2-token", "ab") // "ab" is too short to hide
	st.Add(true, "Sending with hunter2-token to a webhook at https://hooks.example.com/services/T000/B000/abcdefghijklmnopqrstuvwx")
	got := st.List()[0].Text
	if strings.Contains(got, "hunter2-token") {
		t.Errorf("secret left in %q", got)
	}
	if !strings.Contains(got, "•••") {
		t.Errorf("no mask in %q", got)
	}
	if st.List()[0].Time.IsZero() || !st.List()[0].OK {
		t.Errorf("step %+v", st.List()[0])
	}
}

func TestStepsForWebhookStyleSenders(t *testing.T) {
	ev := notify.ComposeText("test", "", "", notify.Links{}, time.Now())

	t.Run("accepted", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
		defer srv.Close()
		st := notify.NewSteps()
		s := notify.NewWebhookSender(srv.URL + "/secret/path/token123")
		if err := s.Send(notify.WithSteps(context.Background(), st), ev); err != nil {
			t.Fatal(err)
		}
		lines := texts(st)
		for _, want := range []string{"Preparing the message for 127.0.0.1", "Connecting to 127.0.0.1:", "Connected", "Sent the message", "Done: accepted by the server (204)"} {
			if !hasLine(lines, want) {
				t.Errorf("no line with %q in %v", want, lines)
			}
		}
		if last := lastStep(t, st); !last.OK {
			t.Errorf("last step should be ok: %+v", last)
		}
		for _, l := range lines {
			if strings.Contains(l, "token123") || strings.Contains(l, "/secret") {
				t.Errorf("the address path leaked: %q", l)
			}
		}
	})

	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"wrong token", 401, `{"error":"Unauthorized"}`, "Refused: wrong token, key or password (401)"},
		{"not found", 404, "", "Not found: check the address, topic or chat (404)"},
		{"rate limit", 429, "", "Too many messages"},
		{"server error", 502, "<html>bad gateway</html>", "The server had a problem on its side (502)"},
		{"bad request says why", 400, `{"description":"chat not found"}`, "chat not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			st := notify.NewSteps()
			if err := notify.NewGotifySender(srv.URL, "tok").Send(notify.WithSteps(context.Background(), st), ev); err == nil {
				t.Fatal("expected an error")
			}
			last := lastStep(t, st)
			if last.OK || !strings.Contains(last.Text, tc.want) {
				t.Errorf("last step %+v, want a failure with %q", last, tc.want)
			}
			if strings.Contains(last.Text, "<html>") {
				t.Errorf("markup in %q", last.Text)
			}
		})
	}

	t.Run("nothing listening", func(t *testing.T) {
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		addr := ln.Addr().String()
		ln.Close()
		st := notify.NewSteps()
		if err := notify.NewWebhookSender("http://"+addr+"/hook").Send(notify.WithSteps(context.Background(), st), ev); err == nil {
			t.Fatal("expected an error")
		}
		last := lastStep(t, st)
		if last.OK || !strings.Contains(last.Text, "Could not connect: connection refused") {
			t.Errorf("last step %+v", last)
		}
		failures := 0
		for _, s := range st.List() {
			if !s.OK {
				failures++
			}
		}
		if failures != 1 {
			t.Errorf("%d failure lines, want one: %v", failures, texts(st))
		}
	})

	t.Run("a custom body that is not JSON", func(t *testing.T) {
		st := notify.NewSteps()
		s := notify.NewWebhookSender("http://127.0.0.1:1/x")
		s.Template = `{"text": {{title}}`
		if err := s.Send(notify.WithSteps(context.Background(), st), ev); err == nil {
			t.Fatal("expected an error")
		}
		if last := lastStep(t, st); last.OK || !strings.Contains(last.Text, "custom JSON body") {
			t.Errorf("last step %+v", last)
		}
	})

	t.Run("without a log nothing changes", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		defer srv.Close()
		if err := notify.NewWebhookSender(srv.URL).Send(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
	})
}

func TestStepsForEmail(t *testing.T) {
	ev := notify.ComposeText("test", "", "", notify.Links{}, time.Now())

	t.Run("starttls with login", func(t *testing.T) {
		f := startFakeSMTP(t, &fakeSMTP{starttls: true, user: "ryan", pass: "s3cret-pass"})
		st := notify.NewSteps("s3cret-pass")
		if err := f.sender("starttls").Send(notify.WithSteps(context.Background(), st), ev); err != nil {
			t.Fatal(err)
		}
		lines := texts(st)
		want := []string{"Connecting to 127.0.0.1:", "Connected", "Starting a secure connection", "Secure connection started (TLS", "Signing in as ryan", "Signed in", "Sending the message from mediarium@example.com to a@example.com, b@example.com", "Done: accepted by the server"}
		next := 0
		for _, l := range lines {
			if next < len(want) && strings.Contains(l, want[next]) {
				next++
			}
		}
		if next != len(want) {
			t.Errorf("steps are missing %q (in this order) in %v", want[next], lines)
		}
		for _, l := range lines {
			if strings.Contains(l, "s3cret-pass") {
				t.Errorf("password in %q", l)
			}
		}
	})

	t.Run("implicit ssl", func(t *testing.T) {
		f := startFakeSMTP(t, &fakeSMTP{implicit: true})
		st := notify.NewSteps()
		if err := f.sender("ssl").Send(notify.WithSteps(context.Background(), st), ev); err != nil {
			t.Fatal(err)
		}
		if !hasLine(texts(st), "Secure connection started (TLS") || hasLine(texts(st), "Signing in") {
			t.Errorf("steps %v", texts(st))
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		f := startFakeSMTP(t, &fakeSMTP{starttls: true, user: "u", pass: "right"})
		s := f.sender("starttls")
		s.Password = "nope-nope"
		st := notify.NewSteps("nope-nope")
		err := s.Send(notify.WithSteps(context.Background(), st), ev)
		if err == nil || !strings.Contains(err.Error(), "Sign-in refused: wrong username or password (535)") {
			t.Fatalf("error %v", err)
		}
		last := lastStep(t, st)
		if last.OK || last.Text != "Sign-in refused: wrong username or password (535)" {
			t.Errorf("last step %+v", last)
		}
	})

	t.Run("starttls not offered", func(t *testing.T) {
		f := startFakeSMTP(t, &fakeSMTP{})
		st := notify.NewSteps()
		if err := f.sender("starttls").Send(notify.WithSteps(context.Background(), st), ev); err == nil {
			t.Fatal("expected an error")
		}
		if last := lastStep(t, st); last.OK || !strings.Contains(last.Text, "STARTTLS") {
			t.Errorf("last step %+v", last)
		}
	})

	t.Run("connection refused", func(t *testing.T) {
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		port := ln.Addr().(*net.TCPAddr).Port
		ln.Close()
		s := &notify.EmailSender{Host: "127.0.0.1", Port: port, Security: "none", From: "a@example.com", To: []string{"b@example.com"}, Timeout: 2 * time.Second}
		st := notify.NewSteps()
		if err := s.Send(notify.WithSteps(context.Background(), st), ev); err == nil {
			t.Fatal("expected an error")
		}
		if last := lastStep(t, st); last.OK || !strings.Contains(last.Text, "Could not connect: connection refused") {
			t.Errorf("last step %+v", last)
		}
	})
}
