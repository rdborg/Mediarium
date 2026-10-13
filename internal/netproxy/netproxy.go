// Package netproxy sends chosen kinds of Mediarium's traffic through a
// SOCKS5 or HTTP proxy: torrent peers and trackers, indexer searches and
// downloads, and Usenet connections. It's the lighter alternative to the
// built-in VPN for people who already run a proxy (a VPN container with a
// SOCKS5 port, a seedbox, Tor).
//
// The setting is process-wide and read on every new connection, so a change
// applies straight away without rebuilding clients. Connections to the proxy
// itself go through netguard like every other outgoing connection.
//
// The built-in VPN can carry the same kinds of traffic (see SetVPN). It comes
// before the proxy, and addresses on the home network still connect directly.
package netproxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/proxy"

	"github.com/rdborg/mediarium/internal/netguard"
)

// Use is a kind of traffic that can go through the proxy.
type Use string

const (
	Torrents Use = "torrents"
	Indexers Use = "indexers"
	Usenet   Use = "usenet"
)

// Uses lists every kind, in the order they are shown.
var Uses = []Use{Torrents, Indexers, Usenet}

// Config is the proxy and what goes through it. A nil URL means no proxy.
type Config struct {
	URL *url.URL
	For map[Use]bool
}

// Parse checks a proxy address: socks5://, socks5h:// or http://, with a
// host and port and optionally user:password.
func Parse(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, errors.New("that isn't a proxy address")
	}
	switch u.Scheme {
	case "socks5", "socks5h", "http":
	default:
		return nil, errors.New("the proxy must be SOCKS5 or HTTP")
	}
	if u.Hostname() == "" {
		return nil, errors.New("the proxy needs a host name or IP address")
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil || p < 1 || p > 65535 {
		return nil, errors.New("the proxy needs a port from 1 to 65535")
	}
	return &url.URL{Scheme: u.Scheme, User: u.User, Host: u.Host}, nil
}

// state is the current setting with the dialer built for it, swapped as a
// whole so a connection never sees half of a change.
type state struct {
	cfg    Config
	dialer *Dialer // nil without a proxy
}

var current atomic.Pointer[state]

// Set replaces the setting.
func Set(c Config) {
	st := &state{cfg: c}
	if c.URL != nil {
		st.dialer = newDialer(c.URL)
	}
	current.Store(st)
}

// For returns the dialer for a kind of traffic, or nil when it goes direct.
func For(use Use) *Dialer {
	st := current.Load()
	if st == nil || st.dialer == nil || !st.cfg.For[use] {
		return nil
	}
	return st.dialer
}

// Dialer connects through one proxy. The same *Dialer is returned for as
// long as the setting doesn't change, so it can key a cache (the torrent
// engine keeps one engine per dialer).
type Dialer struct {
	url  *url.URL
	dial func(ctx context.Context, network, addr string) (net.Conn, error)

	mu        sync.Mutex
	transport *http.Transport // for HTTP clients, built when first needed
}

// New returns a dialer for a proxy, for trying one out before it's saved.
func New(u *url.URL) *Dialer { return newDialer(u) }

func newDialer(u *url.URL) *Dialer {
	d := &Dialer{url: u}
	forward := netguard.Dialer(30 * time.Second)
	if u.Scheme == "http" {
		d.dial = (&connectDialer{proxyAddr: u.Host, user: u.User, forward: forward}).DialContext
		return d
	}
	var auth *proxy.Auth
	if u.User != nil {
		pass, _ := u.User.Password()
		auth = &proxy.Auth{User: u.User.Username(), Password: pass}
	}
	sd, err := proxy.SOCKS5("tcp", u.Host, auth, forward)
	if err != nil { // only for a bad address, which Parse already refused
		d.dial = func(context.Context, string, string) (net.Conn, error) { return nil, err }
		return d
	}
	cd := sd.(proxy.ContextDialer)
	d.dial = cd.DialContext
	return d
}

// Address is the proxy without its password, for messages and logs.
func (d *Dialer) Address() string {
	return d.url.Scheme + "://" + d.url.Host
}

// DialContext connects to addr through the proxy.
func (d *Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	conn, err := d.dial(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("through the proxy %s: %w", d.Address(), plain(err))
	}
	return conn, nil
}

// plain makes the proxy library's errors readable.
func plain(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "username/password authentication failed"), strings.Contains(msg, "407"):
		return errors.New("the proxy refused the user name or password")
	case strings.Contains(msg, "no acceptable authentication methods"):
		return errors.New("the proxy wants a user name and password")
	case strings.Contains(msg, "connection refused"):
		return errors.New("nothing answered at the proxy's address and port")
	}
	return netguard.CleanError(err)
}

// httpTransport is a transport that sends requests through the proxy.
func (d *Dialer) httpTransport() *http.Transport {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.transport == nil {
		t := netguard.Transport()
		if d.url.Scheme == "http" {
			t.Proxy = http.ProxyURL(d.url)
		} else {
			t.Proxy = nil
			t.DialContext = d.DialContext
		}
		d.transport = t
	}
	return d.transport
}

// Transport returns a RoundTripper that sends requests through the proxy
// while it's switched on for use, and straight to the server otherwise.
func Transport(use Use) http.RoundTripper {
	return &switchingTransport{use: use, direct: netguard.Transport()}
}

// Client is netguard.Client whose requests follow the proxy setting for use.
func Client(timeout time.Duration, use Use) *http.Client {
	c := netguard.Client(timeout)
	c.Transport = Transport(use)
	return c
}

type switchingTransport struct {
	use    Use
	direct *http.Transport
}

func (t *switchingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if src := viaVPN(t.use); src != nil {
		tunnel, err := tunnelFor(src, t.use, req.URL.Host)
		if err != nil {
			return nil, err
		}
		if tunnel == nil {
			return t.direct.RoundTrip(req)
		}
		return tunnelTransport(tunnel).RoundTrip(req)
	}
	if d := For(t.use); d != nil {
		resp, err := d.httpTransport().RoundTrip(req)
		if err != nil {
			return nil, fmt.Errorf("through the proxy %s: %w", d.Address(), plain(err))
		}
		return resp, nil
	}
	return t.direct.RoundTrip(req)
}

// CloseIdleConnections lets http.Client.CloseIdleConnections reach the
// transport in use.
func (t *switchingTransport) CloseIdleConnections() {
	t.direct.CloseIdleConnections()
	closeTunnelIdle()
	if d := For(t.use); d != nil {
		d.httpTransport().CloseIdleConnections()
	}
}

// DialContext connects to addr for use: through the VPN or the proxy when
// one is on for that kind of traffic, otherwise directly (through netguard).
func DialContext(ctx context.Context, use Use, timeout time.Duration, network, addr string) (net.Conn, error) {
	var through ContextDialer
	if src := viaVPN(use); src != nil {
		tunnel, err := tunnelFor(src, use, addr)
		if err != nil {
			return nil, err
		}
		if tunnel != nil {
			through = tunnel
		}
	} else if d := For(use); d != nil {
		through = d
	}
	if through == nil {
		return netguard.Dialer(timeout).DialContext(ctx, network, addr)
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return through.DialContext(ctx, network, addr)
}

// connectDialer opens a tunnel through an HTTP proxy with CONNECT.
type connectDialer struct {
	proxyAddr string
	user      *url.Userinfo
	forward   *net.Dialer
}

func (c *connectDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	conn, err := c.forward.DialContext(ctx, "tcp", c.proxyAddr)
	if err != nil {
		return nil, err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	}
	req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: addr}, Host: addr, Header: http.Header{}}
	if c.user != nil {
		pass, _ := c.user.Password()
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.user.Username()+":"+pass)))
	}
	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("the proxy didn't answer like an HTTP proxy: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		conn.Close()
		if resp.StatusCode == http.StatusProxyAuthRequired {
			return nil, errors.New("the proxy refused the user name or password (407)")
		}
		return nil, fmt.Errorf("the proxy refused the connection to %s (%s)", addr, resp.Status)
	}
	_ = conn.SetDeadline(time.Time{})
	if br.Buffered() > 0 {
		return &bufferedConn{Conn: conn, r: br}, nil
	}
	return conn, nil
}

// bufferedConn reads what the proxy sent after its answer before the rest.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.r.Read(p) }
