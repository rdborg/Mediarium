package vpn

import (
	"fmt"
	"sync"
	"time"
)

// Manager owns the single live Tunnel (if any) for the whole app — only
// one VPN config is ever active at a time.
type Manager struct {
	mu          sync.Mutex
	tunnel      *Tunnel
	activeLabel string
	connectedAt time.Time
}

func NewManager() *Manager { return &Manager{} }

// Activate tears down any existing tunnel and brings up a new one for
// label/cfg.
func (m *Manager) Activate(label string, cfg Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.tunnel != nil {
		m.tunnel.Close()
		m.tunnel = nil
	}
	tunnel, err := Up(cfg)
	if err != nil {
		return fmt.Errorf("activate vpn %q: %w", label, err)
	}
	m.tunnel = tunnel
	m.activeLabel = label
	m.connectedAt = time.Now()
	return nil
}

// Deactivate tears down the current tunnel, if any.
func (m *Manager) Deactivate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tunnel != nil {
		m.tunnel.Close()
		m.tunnel = nil
	}
	m.activeLabel = ""
}

// Status reports current connection state for the UI ("status
// visibility... shown clearly in the UI, not just in logs").
type Status struct {
	Connected   bool
	Label       string
	ConnectedAt time.Time
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Status{Connected: m.tunnel != nil, Label: m.activeLabel, ConnectedAt: m.connectedAt}
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
