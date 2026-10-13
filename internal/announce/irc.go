package announce

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
	"github.com/rdborg/mediarium/internal/netproxy"
)

// This file is a small IRC client: just what a tracker announce channel needs
// (RFC 1459 / 2812 registration, IRCv3 SASL PLAIN, NickServ, JOIN, INVITE,
// PRIVMSG and NOTICE). A general IRC library would bring a lot of code that
// reads the network for features nobody uses here; this keeps every read and
// write in one place that can be bounded and tested.

// Auth modes for signing in to the network.
const (
	AuthNone     = "none"     // no sign-in (the network does not need a registered nick)
	AuthSASL     = "sasl"     // IRCv3 SASL PLAIN while connecting
	AuthNickServ = "nickserv" // a message to NickServ after connecting
)

const (
	maxIncomingLine = 4096             // bytes; an IRC line is at most 512, with tags 8191, none requested
	maxOutgoingLine = 480              // bytes of a command we send
	readIdle        = 5 * time.Minute  // no data at all from the server for this long ends the session
	pingEvery       = 2 * time.Minute  // we ask the server for a sign of life this often
	registerWithin  = 60 * time.Second // the server has to finish signing us in within this
	inviteGap       = time.Second      // between invite commands
	inviteAgain     = 10 * time.Minute // an invite is repeated no sooner than this
	maxSleepCommand = 30 * time.Second
)

// Channel is a channel to join. Key is its join password, "" for none.
type Channel struct {
	Name string
	Key  string
}

// Message is one line said in a channel by an announcer. Text is already
// cleaned (see Clean).
type Message struct {
	Channel string
	Nick    string
	Text    string
}

// Config is everything a client needs. Secrets are held in memory only while
// the client runs.
type Config struct {
	Server      string
	Port        int
	TLS         bool
	InsecureTLS bool // accept a certificate that does not verify (self-signed, expired)

	Nick     string
	Auth     string // AuthNone, AuthSASL or AuthNickServ
	Account  string // SASL login, or the NickServ account when it is not the nick
	Password string

	// Invite is one or more commands for a tracker's bot, separated by
	// commas: "BotName announce KEY". "/sleep N" waits N seconds.
	Invite string

	Channels   []Channel
	Announcers []string // nicks whose channel messages are announcements; everyone else is ignored

	// OnMessage gets every announcement. It runs on the reading goroutine and
	// must return quickly.
	OnMessage func(Message)

	// BackoffMin and BackoffMax bound the wait between connection attempts.
	// The defaults (15 seconds, 15 minutes) are kind to the network; tests
	// shorten them.
	BackoffMin, BackoffMax time.Duration

	// AllowPrivate lets the client connect to loopback and private addresses.
	// Real connections never set it: IRC servers are public hosts. Tests do.
	AllowPrivate bool

	// Dial replaces the network connection (tests).
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// ChannelStatus is how one channel is doing.
type ChannelStatus struct {
	Name   string `json:"name"`
	Joined bool   `json:"joined"`
	Error  string `json:"error,omitempty"`
}

// Status is a snapshot of a client for the settings page.
type Status struct {
	State        string          `json:"state"` // stopped, connecting, connected, joined
	Channels     []ChannelStatus `json:"channels"`
	LastError    string          `json:"lastError,omitempty"`
	ConnectedAt  time.Time       `json:"connectedAt,omitempty"`
	LastAnnounce time.Time       `json:"lastAnnounce,omitempty"`
	Reconnects   int             `json:"reconnects"`
}

// Client keeps one IRC connection up for as long as Run runs.
type Client struct {
	cfg        Config
	announcers map[string]bool

	mu         sync.Mutex
	state      string
	chans      map[string]*chanState // by lower-case name
	order      []string
	lastErr    string
	connAt     time.Time
	lastAnn    time.Time
	reconnects int
}

type chanState struct {
	Channel
	joined    bool
	err       string
	errCode   string
	holdUntil time.Time // after a kick or a ban, no join attempts before this
}

// NewClient prepares a client; Run connects.
func NewClient(cfg Config) *Client {
	if cfg.Port == 0 {
		cfg.Port = 6697
	}
	if cfg.BackoffMin <= 0 {
		cfg.BackoffMin = 15 * time.Second
	}
	if cfg.BackoffMax < cfg.BackoffMin {
		cfg.BackoffMax = 15 * time.Minute
	}
	c := &Client{cfg: cfg, announcers: map[string]bool{}, chans: map[string]*chanState{}, state: "stopped"}
	for _, a := range cfg.Announcers {
		if a = strings.ToLower(strings.TrimSpace(a)); a != "" {
			c.announcers[a] = true
		}
	}
	for _, ch := range cfg.Channels {
		key := strings.ToLower(ch.Name)
		if _, dup := c.chans[key]; dup || ch.Name == "" {
			continue
		}
		c.chans[key] = &chanState{Channel: ch}
		c.order = append(c.order, key)
	}
	return c
}

// Status reports how the client is doing now.
func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := Status{State: c.state, LastError: c.lastErr, ConnectedAt: c.connAt, LastAnnounce: c.lastAnn, Reconnects: c.reconnects, Channels: []ChannelStatus{}}
	for _, k := range c.order {
		ch := c.chans[k]
		st.Channels = append(st.Channels, ChannelStatus{Name: ch.Name, Joined: ch.joined, Error: ch.err})
	}
	return st
}

// Run connects and keeps the connection up, waiting longer after each failure,
// until ctx ends.
func (c *Client) Run(ctx context.Context) {
	defer c.setState("stopped")
	wait := c.cfg.BackoffMin
	for ctx.Err() == nil {
		c.setState("connecting")
		started := time.Now()
		err := c.session(ctx)
		c.setState("connecting")
		if ctx.Err() != nil {
			return
		}
		var fe *fatalError
		switch {
		case errors.As(err, &fe):
			wait = c.cfg.BackoffMax // wrong password, banned: asking again sooner only makes it worse
		case time.Since(started) > 2*time.Minute:
			wait = c.cfg.BackoffMin // it was up for a while: this is a fresh start
		}
		if err != nil {
			c.setError(err.Error())
		}
		c.mu.Lock()
		c.reconnects++
		c.mu.Unlock()
		// A little jitter so several connections to one network do not return together.
		d := wait + time.Duration(rand.Int64N(int64(wait)/4+1))
		select {
		case <-ctx.Done():
			return
		case <-time.After(d):
		}
		wait = min(wait*2, c.cfg.BackoffMax)
	}
}

// fatalError is a failure that retrying quickly will not fix.
type fatalError struct{ msg string }

func (e *fatalError) Error() string { return e.msg }

func (c *Client) setState(s string) {
	c.mu.Lock()
	c.state = s
	if s != "joined" && s != "connected" {
		c.connAt = time.Time{}
		for _, ch := range c.chans {
			ch.joined = false
		}
	}
	c.mu.Unlock()
}

func (c *Client) setError(msg string) {
	c.mu.Lock()
	c.lastErr = msg
	c.mu.Unlock()
}

// updateJoinState sets the overall state to joined while any channel is in.
func (c *Client) updateJoinState() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != "connected" && c.state != "joined" {
		return
	}
	c.state = "connected"
	for _, ch := range c.chans {
		if ch.joined {
			c.state = "joined"
			break
		}
	}
}

// dial opens the connection. IRC servers are public hosts: a name that
// resolves to a loopback, private or link-local address is refused (the
// address check that guards every other outgoing connection also applies).
func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	host := c.cfg.Server
	addr := net.JoinHostPort(host, strconv.Itoa(c.cfg.Port))
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var raw net.Conn
	var err error
	switch {
	case c.cfg.Dial != nil:
		raw, err = c.cfg.Dial(dctx, "tcp", addr)
	case netproxy.Routed(netproxy.Indexers):
		// The VPN or proxy the person set up for indexer traffic carries this
		// too. It resolves the name, so only a literal address can be judged here.
		if ip, perr := netip.ParseAddr(host); perr == nil && !c.cfg.AllowPrivate && !PublicAddr(ip) {
			return nil, &fatalError{"IRC servers have to be on the internet. " + host + " is a local address."}
		}
		raw, err = netproxy.DialContext(dctx, netproxy.Indexers, 30*time.Second, "tcp", addr)
	default:
		var ips []netip.Addr
		if ip, perr := netip.ParseAddr(host); perr == nil {
			ips = []netip.Addr{ip}
		} else {
			found, lerr := net.DefaultResolver.LookupNetIP(dctx, "ip", host)
			if lerr != nil {
				var dnsErr *net.DNSError
				if errors.As(lerr, &dnsErr) && dnsErr.IsNotFound {
					return nil, fmt.Errorf("couldn't find the server %s. Check the name", host)
				}
				return nil, fmt.Errorf("couldn't find the server %s: %w", host, lerr)
			}
			ips = found
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("the server %s has no address", host)
		}
		for _, ip := range ips {
			if !c.cfg.AllowPrivate && !PublicAddr(ip) {
				return nil, &fatalError{"IRC servers have to be on the internet. " + host + " points to a local address."}
			}
		}
		// Connect to the address that was checked, not to the name again.
		d := netguard.Dialer(30 * time.Second)
		for _, ip := range ips {
			raw, err = d.DialContext(dctx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(c.cfg.Port)))
			if err == nil {
				break
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("couldn't connect to %s: %w", addr, netguardPlain(err))
	}
	if !c.cfg.TLS {
		return raw, nil
	}
	tc := tls.Client(raw, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, InsecureSkipVerify: c.cfg.InsecureTLS}) //nolint:gosec // the person chose to accept an unverified certificate
	hctx, hcancel := context.WithTimeout(ctx, 20*time.Second)
	defer hcancel()
	if err := tc.HandshakeContext(hctx); err != nil {
		raw.Close()
		return nil, fmt.Errorf("the secure connection to %s failed: %w", host, err)
	}
	return tc, nil
}

func netguardPlain(err error) error {
	if errors.Is(err, netguard.ErrBlocked) {
		return &fatalError{"that address is not allowed"}
	}
	return err
}

// PublicAddr reports whether ip is an address on the public internet: not
// loopback, private (RFC 1918, unique local), carrier-grade NAT, link-local,
// unspecified or multicast.
func PublicAddr(ip netip.Addr) bool {
	ip = ip.Unmap().WithZone("")
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	if netguard.Blocked(ip) {
		return false
	}
	if ip.Is4() {
		b := ip.As4()
		switch {
		case b[0] == 100 && b[1]&0xc0 == 64: // 100.64.0.0/10, carrier-grade NAT
			return false
		case b[0] == 192 && b[1] == 0 && b[2] == 0, b[0] == 198 && (b[1] == 18 || b[1] == 19): // protocol assignments, benchmarking
			return false
		case b[0] >= 240: // reserved
			return false
		}
	}
	return true
}

// session is one connection from dial to hang-up.
type session struct {
	c    *Client
	conn net.Conn
	ctx  context.Context

	wmu sync.Mutex

	nick        string
	registered  bool
	saslPending bool
	idDone      chan struct{} // closed when NickServ has answered (or nothing is expected)
	idOnce      sync.Once
	lastInvite  time.Time
}

func (c *Client) session(ctx context.Context) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer conn.Close()
	s := &session{c: c, conn: conn, ctx: sctx, nick: safeParam(c.cfg.Nick, 30), idDone: make(chan struct{})}
	go func() {
		<-sctx.Done()
		// Say goodbye if the server is still listening, but never wait for it.
		s.wmu.Lock()
		_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
		_, _ = conn.Write([]byte("QUIT :Mediarium\r\n"))
		s.wmu.Unlock()
		conn.Close()
	}()

	if c.cfg.Auth != AuthNickServ {
		s.idOnce.Do(func() { close(s.idDone) })
	}
	if c.cfg.Auth == AuthSASL {
		s.saslPending = true
		s.send("CAP REQ :sasl")
	}
	s.send("NICK " + s.nick)
	s.send("USER " + s.nick + " 0 * :" + s.nick)

	go s.keepAlive()
	deadline := time.Now().Add(registerWithin)
	r := bufio.NewReaderSize(conn, maxIncomingLine)
	for {
		wait := time.Now().Add(readIdle)
		if !s.registered && deadline.Before(wait) {
			wait = deadline
		}
		_ = conn.SetReadDeadline(wait)
		line, err := readLine(r)
		if err != nil {
			if sctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if !s.registered && errors.As(err, &ne) && ne.Timeout() {
				return errors.New("the server did not finish signing in. Check the nick and the sign-in method")
			}
			if errors.Is(err, errLineTooLong) {
				continue // dropped; the rest of the line was skipped
			}
			return fmt.Errorf("the connection ended: %w", err)
		}
		if line == "" {
			continue
		}
		if err := s.handle(line); err != nil {
			return err
		}
	}
}

var errLineTooLong = errors.New("line too long")

// readLine reads one line without ever holding more than maxIncomingLine
// bytes. A longer line is skipped to its end and reported as errLineTooLong.
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		for errors.Is(err, bufio.ErrBufferFull) {
			_, err = r.ReadSlice('\n')
		}
		if err != nil {
			return "", err
		}
		return "", errLineTooLong
	}
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(line), "\r\n"), nil
}

// send writes one command. Anything that could end the line early is removed,
// and the line is cut to the length IRC allows.
func (s *session) send(line string) {
	line = safeParam(line, maxOutgoingLine)
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	_, _ = s.conn.Write([]byte(line + "\r\n"))
}

func (s *session) keepAlive() {
	t := time.NewTicker(pingEvery)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			s.send("PING :mediarium")
		}
	}
}

type ircMessage struct {
	nick   string
	cmd    string
	params []string
}

// parseIRC splits a line into the sender's nick, the command and the
// parameters. Message tags are skipped.
func parseIRC(line string) (ircMessage, bool) {
	var m ircMessage
	if strings.HasPrefix(line, "@") {
		_, rest, ok := strings.Cut(line, " ")
		if !ok {
			return m, false
		}
		line = strings.TrimLeft(rest, " ")
	}
	if strings.HasPrefix(line, ":") {
		prefix, rest, ok := strings.Cut(line[1:], " ")
		if !ok {
			return m, false
		}
		m.nick, _, _ = strings.Cut(prefix, "!")
		m.nick, _, _ = strings.Cut(m.nick, "@")
		line = strings.TrimLeft(rest, " ")
	}
	if line == "" {
		return m, false
	}
	var trailing string
	hasTrailing := false
	if i := strings.Index(line, " :"); i >= 0 {
		trailing, hasTrailing = line[i+2:], true
		line = line[:i]
	} else if strings.HasPrefix(line, ":") { // a command with only a trailing parameter cannot happen, but do not crash on it
		return m, false
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return m, false
	}
	m.cmd = strings.ToUpper(fields[0])
	m.params = fields[1:]
	if hasTrailing {
		m.params = append(m.params, trailing)
	}
	return m, true
}

func (s *session) handle(line string) error {
	m, ok := parseIRC(line)
	if !ok {
		return nil
	}
	c := s.c
	p := m.params
	arg := func(i int) string {
		if i < len(p) {
			return p[i]
		}
		return ""
	}
	switch m.cmd {
	case "PING":
		s.send("PONG :" + arg(0))
	case "ERROR":
		return fmt.Errorf("the server closed the connection: %s", Clean(arg(0)))
	case "CAP":
		if len(p) >= 2 && strings.EqualFold(arg(1), "ACK") && strings.Contains(strings.ToLower(arg(2)), "sasl") {
			s.send("AUTHENTICATE PLAIN")
		} else if len(p) >= 2 && strings.EqualFold(arg(1), "NAK") && s.saslPending {
			return &fatalError{"The server does not offer SASL. Choose NickServ or no sign-in instead."}
		}
	case "AUTHENTICATE":
		if arg(0) == "+" && s.saslPending {
			account := c.cfg.Account
			if account == "" {
				account = c.cfg.Nick
			}
			payload := base64.StdEncoding.EncodeToString([]byte("\x00" + account + "\x00" + c.cfg.Password))
			for len(payload) >= 400 {
				s.send("AUTHENTICATE " + payload[:400])
				payload = payload[400:]
			}
			if payload == "" {
				payload = "+"
			}
			s.send("AUTHENTICATE " + payload)
		}
	case "900":
		// logged in; 903 follows
	case "903", "907":
		s.finishCap()
	case "902", "904", "905", "906":
		return &fatalError{"The server refused the sign-in (SASL). Check the account name and password."}
	case "001":
		s.registered = true
		s.nick = arg(0)
		c.mu.Lock()
		c.state = "connected"
		c.connAt = time.Now()
		c.lastErr = ""
		c.mu.Unlock()
		go s.afterRegister()
	case "432", "433", "436":
		return fmt.Errorf("the nick %s can't be used: %s", s.nick, Clean(arg(len(p)-1)))
	case "464":
		return &fatalError{"The server wants a password that was not given or is wrong."}
	case "465", "466":
		return &fatalError{"The server has banned this address: " + Clean(arg(len(p)-1))}
	case "JOIN":
		if strings.EqualFold(m.nick, s.nick) {
			s.setJoined(arg(0), true, "", "")
		}
	case "PART":
		if strings.EqualFold(m.nick, s.nick) {
			s.setJoined(arg(0), false, "", "")
		}
	case "KICK":
		if strings.EqualFold(arg(1), s.nick) {
			s.kicked(arg(0))
		}
	case "366": // end of names: we are in
		s.setJoined(arg(1), true, "", "")
	case "INVITE":
		// An invite to one of our channels is taken up at once.
		if strings.EqualFold(arg(0), s.nick) {
			s.joinNow(arg(1))
		}
	case "471", "473", "474", "475", "477", "403", "405", "476", "520":
		s.joinFailed(arg(1), m.cmd, Clean(arg(len(p)-1)))
	case "NOTICE":
		s.notice(m.nick, arg(0), arg(1))
	case "PRIVMSG":
		s.message(m.nick, arg(0), arg(1))
	}
	return nil
}

func (s *session) finishCap() {
	if s.saslPending {
		s.saslPending = false
		s.send("CAP END")
	}
}

func (s *session) notice(from, target, text string) {
	if strings.EqualFold(from, "NickServ") && !strings.HasPrefix(target, "#") {
		t := strings.ToLower(Clean(text))
		switch {
		case containsAny(t, "you are now identified", "you are now logged in", "password accepted", "you are already identified", "is now recognized", "is now recognised", "successfully identified", "you have been identified"):
			s.idOnce.Do(func() { close(s.idDone) })
		case containsAny(t, "incorrect", "invalid password", "not registered", "isn't registered", "is not a registered", "password mismatch", "authentication failed", "access denied", "wrong password"):
			s.c.setError("NickServ said: " + truncate(Clean(text), 160))
			s.idOnce.Do(func() { close(s.idDone) })
		}
		return
	}
	s.message(from, target, text)
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// message handles something said in a channel. Only the announcers' lines in
// a channel we were asked to join are passed on.
func (s *session) message(from, target, text string) {
	c := s.c
	if from == "" || (!strings.HasPrefix(target, "#") && !strings.HasPrefix(target, "&")) {
		return
	}
	if !c.announcers[strings.ToLower(from)] {
		return
	}
	c.mu.Lock()
	ch := c.chans[strings.ToLower(target)]
	c.mu.Unlock()
	if ch == nil || strings.HasPrefix(text, "\x01") { // not our channel, or a CTCP
		return
	}
	text = Clean(text)
	if text == "" {
		return
	}
	c.mu.Lock()
	c.lastAnn = time.Now()
	c.mu.Unlock()
	if c.cfg.OnMessage != nil {
		c.cfg.OnMessage(Message{Channel: ch.Name, Nick: from, Text: text})
	}
}

func (s *session) setJoined(name string, joined bool, code, msg string) {
	c := s.c
	c.mu.Lock()
	if ch := c.chans[strings.ToLower(name)]; ch != nil {
		ch.joined = joined
		ch.err, ch.errCode = msg, code
	}
	c.mu.Unlock()
	c.updateJoinState()
}

func (s *session) kicked(name string) {
	c := s.c
	c.mu.Lock()
	if ch := c.chans[strings.ToLower(name)]; ch != nil {
		ch.joined = false
		ch.err = "removed from the channel"
		ch.holdUntil = time.Now().Add(10 * time.Minute)
	}
	c.mu.Unlock()
	c.updateJoinState()
}

func (s *session) joinFailed(name, code, msg string) {
	c := s.c
	c.mu.Lock()
	if ch := c.chans[strings.ToLower(name)]; ch != nil {
		ch.joined = false
		ch.err, ch.errCode = joinReason(code, msg), code
		if code == "474" { // banned
			ch.holdUntil = time.Now().Add(30 * time.Minute)
		}
	}
	c.mu.Unlock()
	c.updateJoinState()
}

func joinReason(code, msg string) string {
	switch code {
	case "473":
		return "the channel is invite only. Check the invite command"
	case "474":
		return "banned from the channel"
	case "475":
		return "wrong channel password"
	case "477":
		return "the channel needs a registered nick"
	case "471":
		return "the channel is full"
	case "403":
		return "no such channel"
	}
	if msg == "" {
		return "could not join"
	}
	return msg
}

func (s *session) joinNow(name string) {
	c := s.c
	c.mu.Lock()
	ch := c.chans[strings.ToLower(name)]
	var key string
	if ch != nil {
		key = ch.Key
		name = ch.Name
	}
	c.mu.Unlock()
	if ch == nil {
		return
	}
	s.sendJoin(name, key)
}

func (s *session) sendJoin(name, key string) {
	if key != "" {
		s.send("JOIN " + name + " " + key)
		return
	}
	s.send("JOIN " + name)
}

// afterRegister signs in with NickServ, sends the invite commands and joins
// the channels, retrying the joins with growing gaps. It ends with the session.
func (s *session) afterRegister() {
	c := s.c
	if c.cfg.Auth == AuthNickServ && c.cfg.Password != "" {
		if c.cfg.Account != "" && !strings.EqualFold(c.cfg.Account, c.cfg.Nick) {
			s.send("PRIVMSG NickServ :IDENTIFY " + c.cfg.Account + " " + c.cfg.Password)
		} else {
			s.send("PRIVMSG NickServ :IDENTIFY " + c.cfg.Password)
		}
		select {
		case <-s.idDone:
		case <-time.After(6 * time.Second):
		case <-s.ctx.Done():
			return
		}
	}
	s.runInvite()
	delays := []time.Duration{20 * time.Second, time.Minute, 3 * time.Minute, 10 * time.Minute}
	for attempt := 0; s.ctx.Err() == nil; attempt++ {
		pending := s.pendingJoins()
		if len(pending) == 0 {
			attempt = -1 // all in: start over if one drops
			if !sleepCtx(s.ctx, 20*time.Second) {
				return
			}
			continue
		}
		for _, ch := range pending {
			s.sendJoin(ch.Name, ch.Key)
		}
		d := delays[min(attempt, len(delays)-1)]
		if !sleepCtx(s.ctx, d) {
			return
		}
		// A channel that said "invite only" gets a fresh invite now and then.
		if s.needsInvite() {
			s.runInvite()
		}
	}
}

func (s *session) pendingJoins() []Channel {
	c := s.c
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []Channel
	for _, k := range c.order {
		ch := c.chans[k]
		if !ch.joined && time.Now().After(ch.holdUntil) {
			out = append(out, ch.Channel)
		}
	}
	return out
}

func (s *session) needsInvite() bool {
	c := s.c
	if c.cfg.Invite == "" || time.Since(s.lastInvite) < inviteAgain {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range c.chans {
		if !ch.joined && ch.errCode == "473" {
			return true
		}
	}
	return false
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// InviteCommands splits an invite setting into its commands: separated by
// commas, a leading "/msg " ignored, "/sleep N" kept as a pause. Each result
// is either {target, text} for a private message or {"", seconds} for a pause.
type InviteCommand struct {
	Target string
	Text   string
	Pause  time.Duration
}

// ParseInvite reads the invite setting. It reports a problem in words a person
// can act on.
func ParseInvite(setting string) ([]InviteCommand, error) {
	var out []InviteCommand
	for _, part := range strings.Split(setting, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		low := strings.ToLower(part)
		switch {
		case strings.HasPrefix(low, "/sleep "), strings.HasPrefix(low, "sleep "):
			n, err := strconv.Atoi(strings.TrimSpace(part[strings.IndexByte(part, ' ')+1:]))
			if err != nil || n < 1 || n > int(maxSleepCommand/time.Second) {
				return nil, fmt.Errorf("%q: a pause is a number of seconds from 1 to %d", part, int(maxSleepCommand/time.Second))
			}
			out = append(out, InviteCommand{Pause: time.Duration(n) * time.Second})
			continue
		case strings.HasPrefix(low, "/msg "):
			part = strings.TrimSpace(part[5:])
		case strings.HasPrefix(part, "/"):
			return nil, fmt.Errorf("%q: only messages to a bot are sent. Write the bot's name, then what to say", part)
		}
		target, text, ok := strings.Cut(part, " ")
		text = strings.TrimSpace(text)
		if !ok || text == "" {
			return nil, fmt.Errorf("%q: write the bot's name, then what to say, for example: BotName announce YOURKEY", part)
		}
		if !ValidNick(target) {
			return nil, fmt.Errorf("%q: %q isn't a valid IRC nick", part, target)
		}
		out = append(out, InviteCommand{Target: target, Text: text})
	}
	return out, nil
}

func (s *session) runInvite() {
	cmds, err := ParseInvite(s.c.cfg.Invite)
	if err != nil || len(cmds) == 0 {
		return
	}
	s.lastInvite = time.Now()
	for i, cmd := range cmds {
		if cmd.Pause > 0 {
			if !sleepCtx(s.ctx, cmd.Pause) {
				return
			}
			continue
		}
		if i > 0 && !sleepCtx(s.ctx, inviteGap) {
			return
		}
		s.send("PRIVMSG " + cmd.Target + " :" + cmd.Text)
	}
	sleepCtx(s.ctx, 2*time.Second) // give the bot a moment to invite us
}

// ValidNick reports whether s can be an IRC nick (RFC 2812 plus the
// characters trackers use in bot names).
func ValidNick(s string) bool {
	if s == "" || len(s) > 30 {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', strings.ContainsRune(`[]\`+"`"+`_^{|}-`, r):
		case r >= '0' && r <= '9', r == '.':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// ValidChannel reports whether s can be an IRC channel name.
func ValidChannel(s string) bool {
	if len(s) < 2 || len(s) > 50 || (s[0] != '#' && s[0] != '&') {
		return false
	}
	for _, r := range s[1:] {
		if r <= 0x20 || r == 0x7f || r == ',' || r == ':' || r == 0x07 {
			return false
		}
	}
	return true
}
