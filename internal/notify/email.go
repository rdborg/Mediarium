package notify

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/smtp"
	"strings"
	"time"
)

// EmailSender delivers events over SMTP, using only the standard library.
type EmailSender struct {
	Host     string
	Port     int
	Security string // "starttls" | "ssl" | "none"
	Username string
	Password string
	From     string   // "you@example.com" or "Name <you@example.com>"
	To       []string // one or more addresses

	// TLSConfig overrides the TLS settings (tests use it to trust a
	// self-signed certificate). Nil means verify against the system roots.
	TLSConfig *tls.Config
	// Timeout bounds the whole conversation; zero means 20 seconds.
	Timeout time.Duration
}

// chooseAuth picks PLAIN, or LOGIN for servers (Office 365 among them) that
// only advertise that. Unlike smtp.PlainAuth these don't refuse an
// unencrypted connection: choosing security "none" with credentials is the
// user's explicit decision, and the settings help says it is not recommended.
func (s *EmailSender) chooseAuth(c *smtp.Client) (smtp.Auth, error) {
	_, mechs := c.Extension("AUTH")
	upper := strings.ToUpper(mechs)
	switch {
	case strings.Contains(upper, "PLAIN"):
		return plainAuth{user: s.Username, pass: s.Password}, nil
	case strings.Contains(upper, "LOGIN"):
		return loginAuth{user: s.Username, pass: s.Password}, nil
	case upper == "":
		return nil, errors.New("This server does not support logging in (it offered no AUTH). Leave Username and Password empty, or check the security mode.")
	}
	return nil, fmt.Errorf("The server only offers login methods Mediarium doesn't support (%s).", mechs)
}

type plainAuth struct{ user, pass string }

func (a plainAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.user + "\x00" + a.pass), nil
}
func (a plainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("unexpected server challenge")
	}
	return nil, nil
}

type loginAuth struct{ user, pass string }

func (a loginAuth) Start(*smtp.ServerInfo) (string, []byte, error) { return "LOGIN", nil, nil }
func (a loginAuth) Next(challenge []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(challenge))) {
	case "username:":
		return []byte(a.user), nil
	case "password:":
		return []byte(a.pass), nil
	}
	return nil, fmt.Errorf("unexpected server challenge %q", challenge)
}

// oneLine keeps header values on a single line so nothing in an event's text
// can inject extra headers.
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("\r", " ", "\n", " ").Replace(s)), " ")
}

// buildMessage renders the RFC 5322 message: UTF-8 subject (RFC 2047) and a
// quoted-printable plain-text body, CRLF line endings.
func buildMessage(from string, to []string, ev Event) []byte {
	var b bytes.Buffer
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", oneLine(from))
	h("To", oneLine(strings.Join(to, ", ")))
	h("Subject", mime.QEncoding.Encode("utf-8", "[Mediarium] "+oneLine(ev.Title)))
	ts := ev.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	h("Date", ts.Format(time.RFC1123Z))
	h("MIME-Version", "1.0")
	h("Content-Type", `text/plain; charset="utf-8"`)
	h("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	body := strings.ReplaceAll(strings.ReplaceAll(ev.Message, "\r\n", "\n"), "\n", "\r\n")
	_, _ = qp.Write([]byte(body + "\r\n"))
	_ = qp.Close()
	return b.Bytes()
}
