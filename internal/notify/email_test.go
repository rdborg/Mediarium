package notify_test

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"math/big"
	"mime"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/notify"
)

// fakeSMTP is a tiny in-process SMTP server: enough of the protocol (EHLO,
// STARTTLS, AUTH PLAIN/LOGIN, MAIL/RCPT/DATA) to check what a client sends.
type fakeSMTP struct {
	addr      string
	host      string
	port      int
	starttls  bool // advertise and accept STARTTLS
	implicit  bool // TLS from the first byte
	user      string
	pass      string
	authLogin bool // advertise only AUTH LOGIN

	mu       sync.Mutex
	from     string
	rcpts    []string
	data     string
	authed   bool
	usedTLS  bool
	authFail bool
}

func testCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func startFakeSMTP(t *testing.T, f *fakeSMTP) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cert := testCert(t)
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	if f.implicit {
		ln = tls.NewListener(ln, tlsCfg)
	}
	f.addr = ln.Addr().String()
	f.host, _, _ = net.SplitHostPort(f.addr)
	f.port = ln.Addr().(*net.TCPAddr).Port
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c, tlsCfg)
		}
	}()
	return f
}

func (f *fakeSMTP) serve(c net.Conn, tlsCfg *tls.Config) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	secure := f.implicit
	r := bufio.NewReader(c)
	say := func(s string) { fmt.Fprint(c, s+"\r\n") }
	say("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			lines := []string{"250-fake"}
			if f.starttls && !secure {
				lines = append(lines, "250-STARTTLS")
			}
			if f.user != "" {
				if f.authLogin {
					lines = append(lines, "250-AUTH LOGIN")
				} else {
					lines = append(lines, "250-AUTH PLAIN LOGIN")
				}
			}
			lines = append(lines, "250 8BITMIME")
			say(strings.Join(lines, "\r\n"))
		case cmd == "STARTTLS":
			say("220 go ahead")
			tc := tls.Server(c, tlsCfg)
			if err := tc.Handshake(); err != nil {
				return
			}
			c, r, secure = tc, bufio.NewReader(tc), true
			say = func(s string) { fmt.Fprint(tc, s+"\r\n") }
		case strings.HasPrefix(cmd, "AUTH PLAIN"):
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(line[len("AUTH PLAIN"):]))
			parts := strings.Split(string(raw), "\x00")
			f.finishAuth(say, len(parts) == 3 && parts[1] == f.user && parts[2] == f.pass)
		case cmd == "AUTH LOGIN":
			say("334 " + base64.StdEncoding.EncodeToString([]byte("Username:")))
			u, _ := r.ReadString('\n')
			say("334 " + base64.StdEncoding.EncodeToString([]byte("Password:")))
			p, _ := r.ReadString('\n')
			ub, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(u))
			pb, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(p))
			f.finishAuth(say, string(ub) == f.user && string(pb) == f.pass)
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			f.mu.Lock()
			f.from = angleAddr(line[len("MAIL FROM:"):])
			f.usedTLS = secure
			f.mu.Unlock()
			say("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			f.mu.Lock()
			f.rcpts = append(f.rcpts, angleAddr(line[len("RCPT TO:"):]))
			f.mu.Unlock()
			say("250 ok")
		case cmd == "DATA":
			say("354 go")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			f.mu.Lock()
			f.data = b.String()
			f.mu.Unlock()
			say("250 queued")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}

// angleAddr pulls the address out of "<addr> PARAMS...".
func angleAddr(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ">"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimPrefix(s, "<")
}

func (f *fakeSMTP) finishAuth(say func(string), ok bool) {
	f.mu.Lock()
	f.authed = ok
	f.authFail = !ok
	f.mu.Unlock()
	if ok {
		say("235 authenticated")
	} else {
		say("535 5.7.8 bad credentials")
	}
}

func (f *fakeSMTP) sender(security string) *notify.EmailSender {
	return &notify.EmailSender{
		Host: f.host, Port: f.port, Security: security, Username: f.user, Password: f.pass,
		From: "Mediarium <mediarium@example.com>", To: []string{"a@example.com", "b@example.com"},
		TLSConfig: &tls.Config{InsecureSkipVerify: true}, Timeout: 5 * time.Second,
	}
}

var emailEvent = notify.Event{
	Type: "failed", Title: "Movie échec\nInjected: header", Message: "Line one\nLine two ☃",
	Timestamp: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
}

func checkDelivered(t *testing.T, f *fakeSMTP, wantTLS bool) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.from != "mediarium@example.com" || strings.Join(f.rcpts, ",") != "a@example.com,b@example.com" {
		t.Fatalf("envelope: from=%q rcpts=%v", f.from, f.rcpts)
	}
	if f.usedTLS != wantTLS {
		t.Fatalf("expected TLS=%v, got %v", wantTLS, f.usedTLS)
	}
	head, body, _ := strings.Cut(f.data, "\r\n\r\n")
	if strings.Contains(head, "\nInjected:") {
		t.Fatalf("header injection through the title: %q", head)
	}
	var subject string
	for _, l := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(l, "Subject: ") {
			subject, _ = new(mime.WordDecoder).DecodeHeader(strings.TrimPrefix(l, "Subject: "))
		}
	}
	if !strings.HasPrefix(subject, "[Mediarium] Movie échec Injected: header") {
		t.Fatalf("unexpected subject %q", subject)
	}
	if !strings.Contains(head, "To: a@example.com, b@example.com") || !strings.Contains(head, "quoted-printable") {
		t.Fatalf("unexpected headers: %q", head)
	}
	if !strings.Contains(body, "Line one") || !strings.Contains(body, "Line two") {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestEmailSenderModes(t *testing.T) {
	cases := []struct {
		name    string
		fake    *fakeSMTP
		mode    string
		wantTLS bool
	}{
		{"starttls with login", &fakeSMTP{starttls: true, user: "u", pass: "p"}, "starttls", true},
		{"implicit ssl with login", &fakeSMTP{implicit: true, user: "u", pass: "p"}, "ssl", true},
		{"plain without login", &fakeSMTP{}, "none", false},
		{"login-only server", &fakeSMTP{starttls: true, user: "u", pass: "p", authLogin: true}, "starttls", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := startFakeSMTP(t, tc.fake)
			if err := f.sender(tc.mode).Send(context.Background(), emailEvent); err != nil {
				t.Fatalf("send: %v", err)
			}
			checkDelivered(t, f, tc.wantTLS)
			if f.user != "" && !f.authed {
				t.Fatal("expected the client to log in")
			}
		})
	}
}

func TestEmailSenderErrors(t *testing.T) {
	t.Run("wrong password", func(t *testing.T) {
		f := startFakeSMTP(t, &fakeSMTP{starttls: true, user: "u", pass: "right"})
		s := f.sender("starttls")
		s.Password = "wrong"
		err := s.Send(context.Background(), emailEvent)
		if err == nil || !strings.Contains(err.Error(), "Sign-in refused") {
			t.Fatalf("expected a login-rejected error, got %v", err)
		}
	})
	t.Run("starttls not offered", func(t *testing.T) {
		f := startFakeSMTP(t, &fakeSMTP{})
		err := f.sender("starttls").Send(context.Background(), emailEvent)
		if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
			t.Fatalf("expected a STARTTLS error, got %v", err)
		}
	})
	t.Run("nothing listening", func(t *testing.T) {
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		port := ln.Addr().(*net.TCPAddr).Port
		ln.Close()
		s := &notify.EmailSender{Host: "127.0.0.1", Port: port, Security: "none", From: "a@example.com", To: []string{"b@example.com"}, Timeout: 2 * time.Second}
		if err := s.Send(context.Background(), emailEvent); err == nil {
			t.Fatal("expected a connection error")
		}
	})
	t.Run("bad addresses", func(t *testing.T) {
		s := &notify.EmailSender{Host: "127.0.0.1", Port: 1, Security: "none", From: "not an address", To: []string{"b@example.com"}}
		if err := s.Send(context.Background(), emailEvent); err == nil {
			t.Fatal("expected an address error")
		}
	})
}

func TestEmailSenderBuiltFromTarget(t *testing.T) {
	f := startFakeSMTP(t, &fakeSMTP{})
	sender, err := notify.NewSender(notify.Target{Type: notify.TargetEmail, Config: map[string]string{
		"host": f.host, "port": strconv.Itoa(f.port), "security": "none",
		"from": "mediarium@example.com", "to": "a@example.com; b@example.com",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(context.Background(), emailEvent); err != nil {
		t.Fatalf("send: %v", err)
	}
	checkDelivered(t, f, false)
}
