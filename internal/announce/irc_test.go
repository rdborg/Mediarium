package announce

import (
	"context"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/netproxy"
)

// testClient runs a client against f and returns the messages it passes on.
func testClient(t *testing.T, f *fakeIRC, tune func(*Config)) (*Client, chan Message) {
	t.Helper()
	host, port := f.addr()
	msgs := make(chan Message, 32)
	cfg := Config{
		Server: host, Port: port, Nick: "mybot", Channels: []Channel{{Name: "#announce"}}, Announcers: []string{"AnnounceBot"},
		OnMessage:  func(m Message) { msgs <- m },
		BackoffMin: 20 * time.Millisecond, BackoffMax: 100 * time.Millisecond, AllowPrivate: true,
	}
	if tune != nil {
		tune(&cfg)
	}
	c := NewClient(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return c, msgs
}

func expectMessage(t *testing.T, msgs chan Message, want string) Message {
	t.Helper()
	select {
	case m := <-msgs:
		if m.Text != want {
			t.Fatalf("got message %q, want %q", m.Text, want)
		}
		return m
	case <-time.After(3 * time.Second):
		t.Fatalf("no message, wanted %q", want)
	}
	return Message{}
}

func expectNoMessage(t *testing.T, msgs chan Message) {
	t.Helper()
	select {
	case m := <-msgs:
		t.Fatalf("unexpected message %+v", m)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestSASLJoinAndAnnounce(t *testing.T) {
	f := newFakeIRC(t)
	f.saslUser, f.saslPass = "alice", "s3cret"
	f.serve(nil)
	c, msgs := testClient(t, f, func(cfg *Config) { cfg.Auth, cfg.Account, cfg.Password = AuthSASL, "alice", "s3cret" })

	waitFor(t, "joined", func() bool { return c.Status().State == "joined" })
	st := c.Status()
	if len(st.Channels) != 1 || !st.Channels[0].Joined {
		t.Fatalf("channel status = %+v", st.Channels)
	}
	if !f.saw("> CAP REQ :sasl") || !f.saw("> CAP END") {
		t.Fatalf("SASL negotiation not seen: %v", f.lines())
	}

	// Bold and colour codes are removed before the text is passed on.
	f.Say("AnnounceBot", "#announce", "\x02\x0304,01New:\x0f Some.Release.2024.1080p\x03")
	m := expectMessage(t, msgs, "New: Some.Release.2024.1080p")
	if m.Channel != "#announce" || m.Nick != "AnnounceBot" {
		t.Fatalf("message = %+v", m)
	}

	// A NOTICE from the announcer counts too (some bots use it), in any letter case.
	f.Notice("announcebot", "#announce", "second line")
	expectMessage(t, msgs, "second line")

	// Anyone else is ignored: another nick in the channel, a private message,
	// a channel that was not asked for, and the announcer's own CTCP.
	f.Say("Intruder", "#announce", "Free.Stuff.2024.1080p")
	f.Say("AnnounceBot", "mybot", "private line")
	f.Say("AnnounceBot", "#elsewhere", "other channel")
	f.Say("AnnounceBot", "#announce", "\x01ACTION waves\x01")
	expectNoMessage(t, msgs)
	if st := c.Status(); st.LastAnnounce.IsZero() {
		t.Fatal("last announce time not recorded")
	}
}

func TestSaysGoodbyeWhenStopped(t *testing.T) {
	f := newFakeIRC(t)
	f.serve(nil)
	host, port := f.addr()
	c := NewClient(Config{Server: host, Port: port, Nick: "mybot", Channels: []Channel{{Name: "#announce"}}, Announcers: []string{"AnnounceBot"}, AllowPrivate: true})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	waitFor(t, "joined", func() bool { return c.Status().State == "joined" })
	cancel()
	<-done
	waitFor(t, "QUIT", func() bool { return f.saw("QUIT :Mediarium") })
	if c.Status().State != "stopped" {
		t.Fatalf("state = %q", c.Status().State)
	}
}

func TestSASLWrongPasswordIsReported(t *testing.T) {
	f := newFakeIRC(t)
	f.saslUser, f.saslPass = "alice", "s3cret"
	f.serve(nil)
	c, _ := testClient(t, f, func(cfg *Config) { cfg.Auth, cfg.Account, cfg.Password = AuthSASL, "alice", "wrong" })
	waitFor(t, "an error", func() bool { return c.Status().LastError != "" })
	if e := c.Status().LastError; !strings.Contains(e, "sign-in") {
		t.Fatalf("error = %q", e)
	}
	if c.Status().State == "joined" {
		t.Fatal("joined with a wrong password")
	}
	if strings.Contains(c.Status().LastError, "wrong") {
		t.Fatal("the password must not be repeated in the status")
	}
}

func TestSASLNotOffered(t *testing.T) {
	f := newFakeIRC(t)
	f.serve(nil) // no SASL support: answers NAK
	c, _ := testClient(t, f, func(cfg *Config) { cfg.Auth, cfg.Password = AuthSASL, "x" })
	waitFor(t, "an error", func() bool { return strings.Contains(c.Status().LastError, "SASL") })
}

func TestNickServAndInviteCommand(t *testing.T) {
	f := newFakeIRC(t)
	f.nickservPass = "npw"
	f.inviteOnly["#announce"] = true
	f.inviteBot, f.inviteText, f.inviteChan = "TrackerBot", "announce THEKEY", "#announce"
	f.serve(nil)
	c, msgs := testClient(t, f, func(cfg *Config) {
		cfg.Auth, cfg.Password = AuthNickServ, "npw"
		cfg.Invite = "/msg TrackerBot announce THEKEY"
	})
	waitFor(t, "joined after the invite", func() bool { return c.Status().State == "joined" })
	if !f.saw("PRIVMSG NickServ :IDENTIFY npw") {
		t.Fatalf("NickServ identify not sent: %v", f.lines())
	}
	if !f.saw("PRIVMSG TrackerBot :announce THEKEY") {
		t.Fatalf("invite command not sent: %v", f.lines())
	}
	f.Say("AnnounceBot", "#announce", "hello")
	expectMessage(t, msgs, "hello")
}

func TestNickServWithAccountName(t *testing.T) {
	f := newFakeIRC(t)
	f.nickservPass = "acct pw"
	f.serve(nil)
	c, _ := testClient(t, f, func(cfg *Config) {
		cfg.Auth, cfg.Account, cfg.Password = AuthNickServ, "realaccount", "pw"
	})
	waitFor(t, "joined", func() bool { return c.Status().State == "joined" })
	if !f.saw("PRIVMSG NickServ :IDENTIFY realaccount pw") {
		t.Fatalf("lines: %v", f.lines())
	}
}

func TestInviteOnlyChannelReportsWhy(t *testing.T) {
	f := newFakeIRC(t)
	f.inviteOnly["#announce"] = true
	f.serve(nil)
	c, _ := testClient(t, f, nil)
	waitFor(t, "channel error", func() bool {
		st := c.Status()
		return len(st.Channels) == 1 && strings.Contains(st.Channels[0].Error, "invite only")
	})
	if c.Status().State != "connected" {
		t.Fatalf("state = %q, want connected (on the network, in no channel)", c.Status().State)
	}
}

func TestReconnectsAndRejoins(t *testing.T) {
	f := newFakeIRC(t)
	f.serve(nil)
	c, msgs := testClient(t, f, nil)
	waitFor(t, "joined", func() bool { return c.Status().State == "joined" })
	f.DropAll()
	waitFor(t, "second welcome", func() bool { return f.welcomeCount() >= 2 })
	waitFor(t, "joined again", func() bool { return c.Status().State == "joined" })
	f.Say("AnnounceBot", "#announce", "back again")
	expectMessage(t, msgs, "back again")
	if c.Status().Reconnects < 1 {
		t.Fatalf("reconnects = %d", c.Status().Reconnects)
	}
}

func TestOverlongLineIsSkipped(t *testing.T) {
	f := newFakeIRC(t)
	f.bigLine = true
	f.serve(nil)
	c, msgs := testClient(t, f, nil)
	waitFor(t, "joined", func() bool { return c.Status().State == "joined" })
	f.Say("AnnounceBot", "#announce", "after the long line")
	expectMessage(t, msgs, "after the long line")
	// Text beyond the limit is cut.
	f.Say("AnnounceBot", "#announce", strings.Repeat("a", 3000))
	select {
	case m := <-msgs:
		if len(m.Text) != MaxTextRunes {
			t.Fatalf("length %d", len(m.Text))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no message")
	}
}

func TestTLSCertificateIsChecked(t *testing.T) {
	f := newFakeIRC(t)
	f.serve(selfSigned(t))
	// A certificate nobody vouches for is refused.
	c, _ := testClient(t, f, func(cfg *Config) { cfg.TLS = true })
	waitFor(t, "a certificate error", func() bool { return strings.Contains(c.Status().LastError, "secure connection") })
	if f.welcomeCount() != 0 {
		t.Fatal("signed in over a connection that failed verification")
	}
}

func TestTLSWithUnverifiedCertificateWhenAllowed(t *testing.T) {
	f := newFakeIRC(t)
	f.serve(selfSigned(t))
	c, msgs := testClient(t, f, func(cfg *Config) { cfg.TLS, cfg.InsecureTLS = true, true })
	waitFor(t, "joined", func() bool { return c.Status().State == "joined" })
	f.Say("AnnounceBot", "#announce", "over tls")
	expectMessage(t, msgs, "over tls")
}

func TestPrivateServerAddressIsRefused(t *testing.T) {
	f := newFakeIRC(t)
	f.serve(nil)
	c, _ := testClient(t, f, func(cfg *Config) { cfg.AllowPrivate = false })
	waitFor(t, "a refusal", func() bool { return strings.Contains(c.Status().LastError, "on the internet") })
	if f.welcomeCount() != 0 || len(f.lines()) != 0 {
		t.Fatal("connected to a loopback address")
	}
}

func TestPublicAddr(t *testing.T) {
	for _, tc := range []struct {
		ip   string
		want bool
	}{
		{"93.184.216.34", true},
		{"2606:4700:4700::1111", true},
		{"127.0.0.1", false},
		{"::1", false},
		{"10.1.2.3", false},
		{"192.168.0.5", false},
		{"172.20.0.1", false},
		{"100.64.0.1", false},
		{"169.254.169.254", false},
		{"0.0.0.0", false},
		{"fd00::1", false},
		{"fe80::1", false},
		{"224.0.0.1", false},
		{"::ffff:10.0.0.1", false},
	} {
		if got := PublicAddr(netip.MustParseAddr(tc.ip)); got != tc.want {
			t.Errorf("PublicAddr(%s) = %v, want %v", tc.ip, got, tc.want)
		}
	}
}

func TestParseIRC(t *testing.T) {
	for _, tc := range []struct {
		line   string
		nick   string
		cmd    string
		params []string
		ok     bool
	}{
		{":bot!u@h PRIVMSG #chan :hello there", "bot", "PRIVMSG", []string{"#chan", "hello there"}, true},
		{"PING :irc.test", "", "PING", []string{"irc.test"}, true},
		{"@time=2024-01-01T00:00:00Z;x=y :bot!u@h NOTICE #c :hi", "bot", "NOTICE", []string{"#c", "hi"}, true},
		{":irc.test 473 me #c :Cannot join", "irc.test", "473", []string{"me", "#c", "Cannot join"}, true},
		{":irc.test 366 me #c :End of /NAMES list.", "irc.test", "366", []string{"me", "#c", "End of /NAMES list."}, true},
		{"", "", "", nil, false},
		{":onlyprefix", "", "", nil, false},
	} {
		m, ok := parseIRC(tc.line)
		if ok != tc.ok {
			t.Errorf("%q: ok = %v", tc.line, ok)
			continue
		}
		if !ok {
			continue
		}
		if m.nick != tc.nick || m.cmd != tc.cmd || strings.Join(m.params, "|") != strings.Join(tc.params, "|") {
			t.Errorf("%q: got %+v", tc.line, m)
		}
	}
}

func TestParseInvite(t *testing.T) {
	cmds, err := ParseInvite("NickServ IDENTIFY key,/sleep 5,/msg BotName !invite USER KEY")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 3 || cmds[0].Target != "NickServ" || cmds[0].Text != "IDENTIFY key" || cmds[1].Pause != 5*time.Second ||
		cmds[2].Target != "BotName" || cmds[2].Text != "!invite USER KEY" {
		t.Fatalf("cmds = %+v", cmds)
	}
	for _, bad := range []string{"justonebot", "/join #x", "/sleep 99", "bad@nick hello", "/sleep x"} {
		if _, err := ParseInvite(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestValidNickAndChannel(t *testing.T) {
	for _, n := range []string{"user|bot", "user-bot", "user_bot", "[x]", "Bot1"} {
		if !ValidNick(n) {
			t.Errorf("nick %q should be valid", n)
		}
	}
	for _, n := range []string{"", "1bot", "a b", "a\r\nQUIT", "x@y", strings.Repeat("a", 31)} {
		if ValidNick(n) {
			t.Errorf("nick %q should be invalid", n)
		}
	}
	for _, ch := range []string{"#announce", "#ipt.announce", "&local"} {
		if !ValidChannel(ch) {
			t.Errorf("channel %q should be valid", ch)
		}
	}
	for _, ch := range []string{"", "#", "announce", "#a b", "#a,b", "#a\r\nQUIT"} {
		if ValidChannel(ch) {
			t.Errorf("channel %q should be invalid", ch)
		}
	}
}

func TestOutgoingLinesCannotCarryExtraCommands(t *testing.T) {
	if got := safeParam("PRIVMSG x :hi\r\nQUIT :bye\x00", 480); strings.ContainsAny(got, "\r\n\x00") {
		t.Fatalf("control characters survived: %q", got)
	}
	if got := safeParam(strings.Repeat("é", 400), 480); len(got) > 480 {
		t.Fatalf("length %d", len(got))
	}
}

// vpnTo is a stand-in VPN tunnel that connects every dial to one address.
type vpnTo struct {
	addr    string
	carried atomic.Int32
}

func (v *vpnTo) Tunnel() netproxy.ContextDialer { return v }
func (v *vpnTo) Selected() (string, string)     { return "My VPN", "" }
func (v *vpnTo) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	v.carried.Add(1)
	var d net.Dialer
	return d.DialContext(ctx, network, v.addr)
}

func TestConnectsThroughTheVPNWhenIndexersUseIt(t *testing.T) {
	f := newFakeIRC(t)
	f.serve(nil)
	host, port := f.addr()
	tunnel := &vpnTo{addr: net.JoinHostPort(host, strconv.Itoa(port))}
	netproxy.SetVPN(tunnel, map[netproxy.Use]bool{netproxy.Indexers: true})
	t.Cleanup(func() { netproxy.SetVPN(nil, nil) })

	c, _ := testClient(t, f, func(cfg *Config) { cfg.Server, cfg.AllowPrivate = "irc.example.net", false })
	waitFor(t, "joined", func() bool { return c.Status().State == "joined" })
	if tunnel.carried.Load() == 0 {
		t.Fatal("the IRC connection didn't go through the VPN")
	}
}
