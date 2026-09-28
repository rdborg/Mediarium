package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
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

func (s *EmailSender) Send(ctx context.Context, ev Event) error {
	from, err := mail.ParseAddress(s.From)
	if err != nil {
		return fmt.Errorf("from address: %w", err)
	}
	var rcpts []string
	for _, a := range s.To {
		addr, err := mail.ParseAddress(a)
		if err != nil {
			return fmt.Errorf("recipient %q: %w", a, err)
		}
		rcpts = append(rcpts, addr.Address)
	}
	if len(rcpts) == 0 {
		return errors.New("no recipients")
	}

	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	tlsCfg := s.TLSConfig
	if tlsCfg == nil {
		tlsCfg = &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}
	} else if tlsCfg.ServerName == "" {
		tlsCfg = tlsCfg.Clone()
		tlsCfg.ServerName = s.Host
	}

	dialer := &net.Dialer{Deadline: deadline}
	var conn net.Conn
	if s.Security == "ssl" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)

	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		return fmt.Errorf("start SMTP session: %w", err)
	}
	defer c.Close()

	if s.Security == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("the server does not offer STARTTLS - pick another security mode or port")
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("start TLS: %w", err)
		}
	}
	if s.Username != "" {
		auth, err := s.chooseAuth(c)
		if err != nil {
			return err
		}
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("login rejected by the mail server: %w", err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("sender rejected: %w", err)
	}
	for _, r := range rcpts {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("recipient %s rejected: %w", r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("start message: %w", err)
	}
	if _, err := w.Write(buildMessage(s.From, s.To, ev)); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	return c.Quit()
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
		return nil, errors.New("this server does not support logging in (no AUTH offered) - leave Username and Password empty, or check the security mode")
	}
	return nil, fmt.Errorf("the server only offers login methods Mediarium doesn't support (%s)", mechs)
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
