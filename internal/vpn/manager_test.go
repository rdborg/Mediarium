package vpn

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// quietConfig is a valid config for a server that will never answer: real
// keys, an address nobody listens on.
func quietConfig(t *testing.T) Config {
	t.Helper()
	mine, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	theirs, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return Config{
		PrivateKey:     mine.PrivateKey,
		PeerPublicKey:  theirs.PublicKey,
		Endpoint:       "127.0.0.1:1",
		AllowedIPs:     []string{"0.0.0.0/0"},
		LocalAddresses: []string{"10.77.0.2/32"},
	}
}

// setClock makes the manager see time as t.
func (m *Manager) setClock(t time.Time) {
	m.mu.Lock()
	m.now = func() time.Time { return t }
	m.mu.Unlock()
}

func TestParseHandshake(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want time.Time
	}{
		{"empty", "", time.Time{}},
		{"never shook hands", "public_key=aa\nlast_handshake_time_sec=0\nlast_handshake_time_nsec=0\n", time.Time{}},
		{"one peer", "last_handshake_time_sec=1700000000\nlast_handshake_time_nsec=500\n", time.Unix(1700000000, 500)},
		{"newest of two peers", "last_handshake_time_sec=100\nlast_handshake_time_nsec=0\nlast_handshake_time_sec=200\nlast_handshake_time_nsec=7\n", time.Unix(200, 7)},
		{"garbage numbers", "last_handshake_time_sec=abc\nlast_handshake_time_nsec=x\n", time.Time{}},
		{"missing nsec", "last_handshake_time_sec=42\n", time.Unix(42, 0)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseHandshake(tc.raw); !got.Equal(tc.want) {
				t.Fatalf("parseHandshake = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExplain(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{errors.New("apply wireguard config: lookup vpn.example.com: no such host"), "Couldn't find the VPN server. Check its address and your internet connection."},
		{errors.New("build wireguard config: private key: decode base64: illegal"), "One of the keys isn't right. Copy it again from your VPN provider."},
		{errors.New("parse local addresses: nope"), "Couldn't start the connection. Check the details you entered."},
	}
	for _, tc := range tests {
		if got := Explain(tc.err); got != tc.want {
			t.Errorf("Explain(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestManagerOffByDefault(t *testing.T) {
	m := NewManager()
	st := m.Status()
	if st.Connected || st.State != StateOff || st.Label != "" || m.Wanted() {
		t.Fatalf("a new manager should be off, got %+v wanted=%v", st, m.Wanted())
	}
}

// A tunnel whose server never answers must not be reported as connected: it
// is "connecting" for a short while, then "down" with a reason.
func TestManagerNeverReportsConnectedWithoutAHandshake(t *testing.T) {
	m := NewManager()
	defer m.Close()
	base := time.Now()
	m.setClock(base)
	if err := m.Activate("Quiet", quietConfig(t)); err != nil {
		t.Fatalf("activate: %v", err)
	}
	st := m.Status()
	if st.Connected || st.State != StateConnecting || st.Label != "Quiet" {
		t.Fatalf("right after activating: %+v, want connecting", st)
	}
	if !m.Wanted() || m.Tunnel() == nil {
		t.Fatal("the connection should be selected and the tunnel up")
	}

	m.setClock(base.Add(handshakeGrace + time.Second))
	st = m.Status()
	if st.Connected || st.State != StateDown || st.Reason == "" {
		t.Fatalf("after the grace time: %+v, want down with a reason", st)
	}
	if !m.Wanted() {
		t.Fatal("a down connection is still selected")
	}
}

// With a real server on the other end the manager reports connected, and goes
// back to down when that server stops answering.
func TestManagerConnectedWithRealPeer(t *testing.T) {
	keyA, _ := GenerateKeyPair()
	keyB, _ := GenerateKeyPair()
	server, err := Up(Config{
		PrivateKey:     keyB.PrivateKey,
		PeerPublicKey:  keyA.PublicKey,
		AllowedIPs:     []string{"10.98.0.1/32"},
		LocalAddresses: []string{"10.98.0.2/32"},
	})
	if err != nil {
		t.Fatalf("server up: %v", err)
	}
	defer server.Close()
	port, err := server.ListenPort()
	if err != nil {
		t.Fatalf("listen port: %v", err)
	}

	m := NewManager()
	defer m.Close()
	err = m.Activate("Real", Config{
		PrivateKey:     keyA.PrivateKey,
		PeerPublicKey:  keyB.PublicKey,
		Endpoint:       "127.0.0.1:" + strconv.Itoa(port),
		AllowedIPs:     []string{"10.98.0.2/32"},
		LocalAddresses: []string{"10.98.0.1/32"},
	})
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if !m.WaitConnected(context.Background(), 10*time.Second) {
		t.Fatalf("never connected: %+v", m.Status())
	}
	st := m.Status()
	if !st.Connected || st.State != StateConnected || st.Reason != "" || st.ConnectedAt.IsZero() {
		t.Fatalf("connected status wrong: %+v", st)
	}

	m.setClock(time.Now().Add(handshakeFresh + time.Minute))
	st = m.Status()
	if st.Connected || st.State != StateDown {
		t.Fatalf("a silent server must read as down, got %+v", st)
	}
}

// After a restart the saved connection is brought back by itself. Until it is
// up the state says so, and a start that fails (network not ready) is retried.
func TestRestoreRetriesUntilUp(t *testing.T) {
	cfg := quietConfig(t)
	var calls atomic.Int32
	m := NewManager()
	defer m.Close()
	m.retryAfter = func(int) time.Duration { return time.Millisecond }
	m.up = func(c Config) (*Tunnel, error) {
		if calls.Add(1) < 3 {
			return nil, errors.New("apply wireguard config: lookup vpn.example.com: no such host")
		}
		return Up(c)
	}

	m.Restore("Saved", cfg)
	if st := m.Status(); st.Connected || st.Label != "Saved" || !m.Wanted() {
		t.Fatalf("right after restore: %+v, want selected but not connected", st)
	}

	deadline := time.Now().Add(5 * time.Second)
	for m.Tunnel() == nil {
		if time.Now().After(deadline) {
			t.Fatalf("the connection never came back after %d tries: %+v", calls.Load(), m.Status())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("tried %d times, want 3", got)
	}
	<-m.restoreDone
	if st := m.Status(); st.Connected {
		t.Fatalf("no server answered, so it must not read as connected: %+v", st)
	}
}

func TestRestoreFailureShowsReason(t *testing.T) {
	m := NewManager()
	defer m.Close()
	m.retryAfter = func(int) time.Duration { return time.Hour }
	m.up = func(Config) (*Tunnel, error) {
		return nil, errors.New("lookup vpn.example.com: no such host")
	}
	m.Restore("Saved", Config{})
	deadline := time.Now().Add(5 * time.Second)
	for m.Status().State != StateDown {
		if time.Now().After(deadline) {
			t.Fatalf("never reported the failure: %+v", m.Status())
		}
		time.Sleep(5 * time.Millisecond)
	}
	st := m.Status()
	if st.Connected || st.Reason == "" || st.Label != "Saved" {
		t.Fatalf("status = %+v, want down with a reason", st)
	}
	if m.Tunnel() != nil {
		t.Fatal("there must be no tunnel")
	}
}

func TestDeactivateStopsRestoring(t *testing.T) {
	var calls atomic.Int32
	m := NewManager()
	m.retryAfter = func(int) time.Duration { return time.Millisecond }
	m.up = func(Config) (*Tunnel, error) {
		calls.Add(1)
		return nil, errors.New("no such host")
	}
	m.Restore("Saved", Config{})
	time.Sleep(20 * time.Millisecond)
	m.Deactivate()
	<-m.restoreDone
	after := calls.Load()
	time.Sleep(30 * time.Millisecond)
	if calls.Load() != after {
		t.Fatal("kept retrying after Deactivate")
	}
	if m.Wanted() || m.Status().State != StateOff {
		t.Fatalf("should be off, got %+v", m.Status())
	}
}

// Choosing another connection while the saved one is still retrying must win:
// the old background start may not bring its tunnel up over the new one.
func TestActivateBeatsBackgroundRestore(t *testing.T) {
	first := make(chan struct{})
	m := NewManager()
	defer m.Close()
	m.retryAfter = func(int) time.Duration { return time.Millisecond }
	var slow atomic.Bool
	m.up = func(c Config) (*Tunnel, error) {
		if slow.CompareAndSwap(false, true) {
			<-first // the first (restore) attempt is stuck until released
			return Up(c)
		}
		return Up(c)
	}
	m.Restore("Old", quietConfig(t))
	time.Sleep(20 * time.Millisecond)

	if err := m.Activate("New", quietConfig(t)); err != nil {
		t.Fatalf("activate: %v", err)
	}
	close(first)
	<-m.restoreDone

	if st := m.Status(); st.Label != "New" {
		t.Fatalf("label = %q, want New", st.Label)
	}
	// Only one tunnel may be alive: the late one from the restore was closed.
	if m.Tunnel() == nil {
		t.Fatal("the new tunnel is missing")
	}
}

func TestBrokenShowsReason(t *testing.T) {
	m := NewManager()
	m.Broken("Home VPN", "Couldn't read the saved keys.")
	st := m.Status()
	if st.Connected || st.State != StateDown || st.Reason != "Couldn't read the saved keys." || st.Label != "Home VPN" || !m.Wanted() {
		t.Fatalf("status = %+v", st)
	}
}

// The status keeps answering while a tunnel is still being made, and a second
// choice made in the meantime wins.
func TestActivateDoesNotHoldTheLockWhileConnecting(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	m := NewManager()
	defer m.Close()
	var calls atomic.Int32
	m.up = func(c Config) (*Tunnel, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release // a slow DNS lookup
		}
		return Up(c)
	}
	first := make(chan error, 1)
	go func() { first <- m.Activate("Slow", quietConfig(t)) }()
	<-entered

	done := make(chan Status, 1)
	go func() { done <- m.Status() }()
	select {
	case st := <-done:
		if st.Connected || st.State != StateConnecting || st.Label != "Slow" {
			t.Fatalf("status while connecting = %+v", st)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Status blocked while a tunnel was being made")
	}

	if err := m.Activate("Fast", quietConfig(t)); err != nil {
		t.Fatalf("second activate: %v", err)
	}
	close(release)
	if err := <-first; err == nil {
		t.Fatal("the superseded activation should report that it lost")
	}
	if st := m.Status(); st.Label != "Fast" || m.Tunnel() == nil {
		t.Fatalf("the later choice must stay: %+v", st)
	}
}

func TestActivateFailureClearsTheSelection(t *testing.T) {
	m := NewManager()
	m.up = func(Config) (*Tunnel, error) { return nil, errors.New("apply wireguard config: bad") }
	if err := m.Activate("Bad", Config{}); err == nil {
		t.Fatal("want an error")
	}
	if m.Wanted() || m.Tunnel() != nil {
		t.Fatal("a connection that failed to start must not stay selected")
	}
}
