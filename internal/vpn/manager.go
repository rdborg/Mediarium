package vpn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// State says whether the VPN is doing its job right now.
type State string

const (
	// StateOff means no connection is selected.
	StateOff State = "off"
	// StateConnecting means a connection is selected and being brought up, or
	// its server has not answered yet.
	StateConnecting State = "connecting"
	// StateConnected means the tunnel is up and its server answered recently.
	StateConnected State = "connected"
	// StateDown means a connection is selected but it is not working.
	StateDown State = "down"
)

const (
	// handshakeGrace is how long a new tunnel gets to reach its server before
	// it counts as not working.
	handshakeGrace = 20 * time.Second
	// handshakeFresh is how old the last handshake may be for the tunnel to
	// count as working. WireGuard starts a new one about every two minutes and
	// stops accepting traffic after three.
	handshakeFresh = 4 * time.Minute
	// defaultKeepalive makes the tunnel talk to its server on its own, so the
	// handshake happens right away and the state shown here is real even when
	// no torrent is running.
	defaultKeepalive = 25
)

// Manager owns the single live Tunnel (if any) for the whole app — only
// one VPN config is ever active at a time. It also remembers which
// connection is meant to be on, so a restart, a failed start or a silent
// server shows up as "not connected" instead of looking fine.
type Manager struct {
	mu          sync.Mutex
	tunnel      *Tunnel
	label       string // the connection that is meant to be on; empty when none
	upAt        time.Time
	problem     string // why the selected connection isn't up, in plain words
	cancel      context.CancelFunc
	gen         int
	up          func(Config) (*Tunnel, error)
	now         func() time.Time
	retryAfter  func(attempt int) time.Duration
	restoreDone chan struct{} // closed when the current background start ends
}

func NewManager() *Manager {
	return &Manager{up: Up, now: time.Now, retryAfter: retryDelay}
}

// retryDelay is the wait before the next try to bring a saved connection up:
// quick at first, then every minute.
func retryDelay(attempt int) time.Duration {
	switch {
	case attempt < 1:
		return 2 * time.Second
	case attempt < 2:
		return 5 * time.Second
	case attempt < 3:
		return 15 * time.Second
	default:
		return time.Minute
	}
}

func withKeepalive(cfg Config) Config {
	if cfg.PersistentKeepalive == 0 {
		cfg.PersistentKeepalive = defaultKeepalive
	}
	return cfg
}

// Activate tears down any existing tunnel and brings up a new one for
// label/cfg.
func (m *Manager) Activate(label string, cfg Config) error {
	m.mu.Lock()
	m.stopLocked()
	m.gen++
	gen := m.gen
	m.label = label // shown as "connecting" while the tunnel is being made
	up := m.up
	m.mu.Unlock()

	// Bringing a tunnel up can wait on a DNS lookup, so the lock is not held
	// meanwhile: the status and the torrent checks keep answering.
	tunnel, err := up(withKeepalive(cfg))

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gen != gen { // someone else activated or deactivated meanwhile
		if tunnel != nil {
			tunnel.Close()
		}
		return fmt.Errorf("activate vpn %q: %w", label, errSuperseded)
	}
	if err != nil {
		m.label = ""
		return fmt.Errorf("activate vpn %q: %w", label, err)
	}
	m.tunnel = tunnel
	m.upAt = m.now()
	m.problem = ""
	return nil
}

var errSuperseded = errors.New("another connection was chosen in the meantime")

// Restore selects a saved connection and brings it up in the background,
// trying again until it works or someone activates or deactivates. The state
// reads "connecting" meanwhile, so nothing claims the VPN is connected before
// it is. Used at start-up, when the network may not be ready yet.
func (m *Manager) Restore(label string, cfg Config) {
	m.mu.Lock()
	m.stopLocked()
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.gen++
	gen := m.gen
	m.label = label
	m.problem = ""
	done := make(chan struct{})
	m.restoreDone = done
	m.mu.Unlock()

	go func() {
		defer close(done)
		m.restoreLoop(ctx, gen, label, withKeepalive(cfg))
	}()
}

func (m *Manager) restoreLoop(ctx context.Context, gen int, label string, cfg Config) {
	for attempt := 0; ; attempt++ {
		tunnel, err := m.up(cfg)
		m.mu.Lock()
		if ctx.Err() != nil || m.gen != gen {
			m.mu.Unlock()
			if tunnel != nil {
				tunnel.Close()
			}
			return
		}
		if err == nil {
			m.tunnel = tunnel
			m.upAt = m.now()
			m.problem = ""
			m.mu.Unlock()
			slog.Info("vpn: connection restored", "label", label)
			return
		}
		m.problem = Explain(err)
		wait := m.retryAfter(attempt)
		m.mu.Unlock()
		slog.Warn("vpn: couldn't bring the saved connection up, will try again", "label", label, "err", err, "retryIn", wait.String())

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// Broken selects a connection that can't be started at all (for example its
// saved keys can't be read), so the app shows it as down with a reason
// instead of pretending nothing was selected.
func (m *Manager) Broken(label, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
	m.label = label
	m.problem = reason
}

// Deactivate tears down the current tunnel, if any.
func (m *Manager) Deactivate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

// Close stops the tunnel and any background start.
func (m *Manager) Close() { m.Deactivate() }

// stopLocked closes the tunnel, ends a background start and forgets which
// connection was selected. The caller holds m.mu.
func (m *Manager) stopLocked() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.gen++
	if m.tunnel != nil {
		m.tunnel.Close()
		m.tunnel = nil
	}
	m.label = ""
	m.problem = ""
	m.upAt = time.Time{}
}

// Status reports current connection state for the UI ("status
// visibility... shown clearly in the UI, not just in logs").
type Status struct {
	// Connected is true only when the tunnel is up and its server has answered
	// recently. A tunnel that exists but hears nothing is not connected.
	Connected bool
	State     State
	// Label is the connection that is meant to be on, even while it isn't up.
	Label       string
	ConnectedAt time.Time
	// Reason says, in plain words, why a selected connection isn't connected.
	Reason string
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.label == "" {
		return Status{State: StateOff}
	}
	st := Status{Label: m.label}
	if m.tunnel == nil {
		if m.problem != "" {
			st.State, st.Reason = StateDown, m.problem
		} else {
			st.State, st.Reason = StateConnecting, "Starting the connection."
		}
		return st
	}
	handshake, err := m.tunnel.LastHandshake()
	now := m.now()
	switch {
	case err != nil:
		st.State, st.Reason = StateDown, "Couldn't check the connection."
	case !handshake.IsZero() && now.Sub(handshake) < handshakeFresh:
		st.Connected, st.State, st.ConnectedAt = true, StateConnected, m.upAt
	case handshake.IsZero() && now.Sub(m.upAt) < handshakeGrace:
		st.State, st.Reason = StateConnecting, "Waiting for the VPN server to answer."
	case handshake.IsZero():
		st.State, st.Reason = StateDown, "The VPN server isn't answering. Check the address and the keys."
	default:
		st.State, st.Reason = StateDown, "The VPN server stopped answering."
	}
	return st
}

// Wanted reports whether a connection is selected, whether or not it is up
// right now. Callers that must not fall back to a direct connection use it to
// tell "no VPN chosen" from "VPN chosen but down".
func (m *Manager) Wanted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.label != ""
}

// WaitConnected waits up to d for the connection to work, and reports whether
// it does.
func (m *Manager) WaitConnected(ctx context.Context, d time.Duration) bool {
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if m.Status().Connected {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return m.Status().Connected
		case <-tick.C:
		}
	}
}

// Tunnel returns the live tunnel for dialing through, or nil if none is
// active. Callers (e.g. the torrent engine) should treat a nil tunnel or
// a failed Dial through it as "can't proceed" rather than silently
// falling back to a direct connection when the caller's configuration
// says traffic should be tunneled — that fallback is exactly what the
// kill-switch requirement rules out.
func (m *Manager) Tunnel() *Tunnel {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tunnel
}

// Explain turns an error from bringing a tunnel up into a short sentence a
// person can act on. The technical detail belongs in the log, not on screen.
func Explain(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "no such host"), strings.Contains(msg, "lookup"):
		return "Couldn't find the VPN server. Check its address and your internet connection."
	case strings.Contains(msg, "endpoint"), strings.Contains(msg, "resolve"):
		return "The VPN server address isn't right. Check it and try again."
	case strings.Contains(msg, "key"):
		return "One of the keys isn't right. Copy it again from your VPN provider."
	default:
		return "Couldn't start the connection. Check the details you entered."
	}
}
