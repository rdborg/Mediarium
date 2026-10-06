package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
	"github.com/rdborg/mediarium/internal/plainerror"
)

// Send delivers the event by SMTP. When ctx carries a Steps log (see
// WithSteps) each stage is written to it, with the reason for the first thing
// that fails.
func (s *EmailSender) Send(ctx context.Context, ev Event) error {
	st := stepsFrom(ctx)
	from, err := mail.ParseAddress(s.From)
	if err != nil {
		st.Add(false, "The From address is not valid")
		return fmt.Errorf("from address: %w", err)
	}
	var rcpts []string
	for _, a := range s.To {
		addr, err := mail.ParseAddress(a)
		if err != nil {
			st.Add(false, "The address %s is not valid", a)
			return fmt.Errorf("recipient %q: %w", a, err)
		}
		rcpts = append(rcpts, addr.Address)
	}
	if len(rcpts) == 0 {
		st.Add(false, "There is no address to send to")
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

	st.Add(true, "Connecting to %s", addr)
	dialer := netguard.Dialer(0)
	dialer.Deadline = deadline
	var conn net.Conn
	if s.Security == "ssl" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		st.Add(false, "%s", smtpConnectFailure(err, s.Security == "ssl"))
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)
	if tc, ok := conn.(*tls.Conn); ok {
		st.Add(true, "Secure connection started (%s)", tlsVersionName(tc.ConnectionState().Version))
	} else {
		st.Add(true, "Connected")
	}

	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		st.Add(false, "The server did not answer like a mail server. Check the port and the security setting.")
		return fmt.Errorf("start SMTP session: %w", err)
	}
	defer c.Close()

	if s.Security == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			st.Add(false, "The server does not offer a secure connection (STARTTLS)")
			return errors.New("The server does not offer STARTTLS. Pick another security mode or port.")
		}
		st.Add(true, "Starting a secure connection")
		if err := c.StartTLS(tlsCfg); err != nil {
			st.Add(false, "Could not start a secure connection: %s", plainerror.Message(err))
			return fmt.Errorf("start TLS: %w", err)
		}
		if state, ok := c.TLSConnectionState(); ok {
			st.Add(true, "Secure connection started (%s)", tlsVersionName(state.Version))
		}
	}
	if s.Username != "" {
		auth, err := s.chooseAuth(c)
		if err != nil {
			st.Add(false, "%s", err.Error())
			return err
		}
		st.Add(true, "Signing in as %s", s.Username)
		if err := c.Auth(auth); err != nil {
			reason := smtpReason(err)
			st.Add(false, "Sign-in refused: %s", reason)
			return fmt.Errorf("Sign-in refused: %s", reason)
		}
		st.Add(true, "Signed in")
	}
	st.Add(true, "Sending the message from %s to %s", from.Address, strings.Join(rcpts, ", "))
	if err := c.Mail(from.Address); err != nil {
		reason := smtpReason(err)
		st.Add(false, "The sender address was refused: %s", reason)
		return fmt.Errorf("sender rejected: %s", reason)
	}
	for _, r := range rcpts {
		if err := c.Rcpt(r); err != nil {
			reason := smtpReason(err)
			st.Add(false, "The address %s was refused: %s", r, reason)
			return fmt.Errorf("recipient %s rejected: %s", r, reason)
		}
	}
	w, err := c.Data()
	if err != nil {
		st.Add(false, "The server would not take the message: %s", smtpReason(err))
		return fmt.Errorf("start message: %w", err)
	}
	header := fromHeader(s.From, s.FromName)
	msg := buildMessage(header, s.To, ev)
	if ev.rich() {
		msg = buildRichMessage(header, s.To, ev)
	}
	if _, err := w.Write(msg); err != nil {
		st.Add(false, "Could not send the message: %s", plainerror.Message(err))
		return fmt.Errorf("write message: %w", err)
	}
	if err := w.Close(); err != nil {
		reason := smtpReason(err)
		st.Add(false, "The server did not accept the message: %s", reason)
		return fmt.Errorf("send message: %s", reason)
	}
	st.Add(true, "Done: accepted by the server")
	return c.Quit()
}

// smtpReason explains an error from the mail server: what the code means and
// the code itself, or the plain text of a network error.
func smtpReason(err error) string {
	var te *textproto.Error
	if !errors.As(err, &te) {
		return plainerror.Message(err)
	}
	var what string
	switch te.Code {
	case 535:
		what = "wrong username or password"
	case 534, 530:
		what = "the server wants a different kind of sign-in (Gmail and Outlook need an app password)"
	case 538:
		what = "the server only allows sign-in over a secure connection"
	case 454, 421, 450, 451, 452:
		what = "the server can't do it right now, try again later"
	case 550, 551, 553:
		what = "the address was not accepted"
	case 552, 554:
		what = "the server refused the message"
	case 501, 503:
		what = "the server did not understand the request"
	default:
		what = strings.TrimSpace(strings.Join(strings.Fields(te.Msg), " "))
		what = clip(what, 120)
	}
	return fmt.Sprintf("%s (%d)", what, te.Code)
}

// smtpConnectFailure is a failed connection to the mail server in plain words.
func smtpConnectFailure(err error, secure bool) string {
	low := strings.ToLower(err.Error())
	switch {
	case strings.Contains(low, "connection refused"):
		return "Could not connect: connection refused. Check the server name and the port."
	case strings.Contains(low, "no such host") || strings.Contains(low, "server misbehaving"):
		return "Could not connect: the server name was not found."
	case strings.Contains(low, "timeout") || strings.Contains(low, "timed out") || errors.Is(err, context.DeadlineExceeded):
		return "Could not connect: no answer in time. Check the server name and the port."
	case strings.Contains(low, "x509") || strings.Contains(low, "certificate"):
		return "Could not start a secure connection: the server's certificate is not trusted."
	case strings.Contains(low, "address not allowed"):
		return "Could not connect: that address is not allowed."
	case secure && (strings.Contains(low, "first record does not look like a tls") || strings.Contains(low, "tls:")):
		return "Could not start a secure connection. This port may not use SSL/TLS; try STARTTLS on port 587."
	}
	return "Could not connect: " + plainerror.Message(err)
}
