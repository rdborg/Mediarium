// Package torrentclient wraps anacrolix/torrent (PRD.md §4.4 Phase 2 —
// "torrent via anacrolix/torrent ... rather than reimplementing the
// BitTorrent wire protocol"). Unlike internal/download's NNTP client,
// there's no remote server to configure: the torrent engine runs embedded
// in this process, so "download client" settings here are local (listen
// port, seed limits, save path) rather than host/port/credentials.
package torrentclient

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/anacrolix/torrent"
)

// TunnelDialer is satisfied by *vpn.Tunnel. Declared narrowly here rather
// than importing internal/vpn, per CLAUDE.md's one-way package dependency
// rule (feature packages shouldn't import sideways into each other) — api
// is the only package that needs to know about both and wires them
// together.
type TunnelDialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// Config are the local engine settings (PRD §7 Phase 2 — "seed ratio/time
// limits, category-based save paths").
type Config struct {
	DataDir        string
	ListenPort     int // 0 = let the OS pick
	SeedRatioLimit float64
	SeedTimeLimit  time.Duration

	// Tunnel, when set, routes every peer connection through it exclusively
	// instead of the host network (PRD §4.7's VPN kill switch). See New's
	// doc comment for exactly what that does and doesn't cover.
	Tunnel TunnelDialer
}

type Client struct {
	tc  *torrent.Client
	cfg Config
}

// New starts the embedded torrent engine. When cfg.Tunnel is set, this is a
// real kill switch, not just "prefer the tunnel": DisableTCP/DisableUTP/
// NoDHT/NoDefaultPortForwarding/AcceptPeerConnections are all forced off so
// the client never binds a real host socket for peer traffic at all — there
// is no direct-connection path to silently fall back to if the tunnel goes
// down. The only dialer added is cfg.Tunnel itself, so a dropped tunnel
// simply fails every new dial rather than reverting to a direct one.
//
// DHT and uTP are casualties of this: both are UDP-based and need a real
// socket bound to arbitrary remote peers (uTP a shared local PacketConn per
// socket, DHT its own routable UDP endpoint) rather than anacrolix/torrent's
// per-connection Dialer interface, which only really suits stream
// protocols. Tunneling them would mean binding a real host UDP socket —
// exactly the leak this mode exists to prevent. Peer discovery instead
// relies on tracker announces and PEX over the tunneled TCP connections.
func New(cfg Config) (*Client, error) {
	tcfg := torrent.NewDefaultClientConfig()
	tcfg.DataDir = cfg.DataDir
	if cfg.ListenPort != 0 {
		tcfg.ListenPort = cfg.ListenPort
	}
	if cfg.Tunnel != nil {
		tcfg.DisableTCP = true
		tcfg.DisableUTP = true
		tcfg.NoDHT = true
		tcfg.NoDefaultPortForwarding = true
		tcfg.AcceptPeerConnections = false
	}
	tc, err := torrent.NewClient(tcfg)
	if err != nil {
		return nil, fmt.Errorf("create torrent client: %w", err)
	}
	if cfg.Tunnel != nil {
		tc.AddDialer(torrent.NetworkDialer{Network: "tcp", Dialer: cfg.Tunnel})
	}
	return &Client{tc: tc, cfg: cfg}, nil
}

func (c *Client) Close() {
	c.tc.Close()
}

// ListenAddrs reports the client's real host listen addresses, if any —
// empty when the client is running in tunnel-only mode (see New).
func (c *Client) ListenAddrs() []net.Addr {
	return c.tc.ListenAddrs()
}

// AddMagnet adds a torrent from a magnet URI and blocks until its
// metadata (piece layout, file list) is available.
func (c *Client) AddMagnet(ctx context.Context, magnetURI string) (*torrent.Torrent, error) {
	t, err := c.tc.AddMagnet(magnetURI)
	if err != nil {
		return nil, fmt.Errorf("add magnet: %w", err)
	}
	return t, c.waitForInfo(ctx, t)
}

// AddTorrentFile adds a torrent from a local .torrent file's bytes.
func (c *Client) AddTorrentFile(ctx context.Context, path string) (*torrent.Torrent, error) {
	mi, err := torrentMetaInfoFromFile(path)
	if err != nil {
		return nil, err
	}
	t, err := c.tc.AddTorrent(mi)
	if err != nil {
		return nil, fmt.Errorf("add torrent file: %w", err)
	}
	return t, c.waitForInfo(ctx, t)
}

func (c *Client) waitForInfo(ctx context.Context, t *torrent.Torrent) error {
	select {
	case <-t.GotInfo():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ProgressFunc mirrors internal/download's callback shape so the API
// pipeline can treat both protocols uniformly.
type ProgressFunc func(bytesDone, bytesTotal int64)

// Download starts downloading every file in t and blocks until complete,
// reporting progress as it goes. It does not stop seeding afterward —
// that's EnforceSeedLimits' job, run separately so a caller can return
// control to the pipeline (import, etc.) as soon as the download itself
// finishes.
func Download(ctx context.Context, t *torrent.Torrent, onProgress ProgressFunc) error {
	t.DownloadAll()
	total := t.Length()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.Complete().On():
			if onProgress != nil {
				onProgress(total, total)
			}
			return nil
		case <-ticker.C:
			if onProgress != nil {
				onProgress(t.BytesCompleted(), total)
			}
		}
	}
}

// EnforceSeedLimits stops seeding (drops the torrent) once either the
// configured seed ratio or seed time limit is reached (PRD §7 Phase 2).
// Meant to be started in its own goroutine right after Download returns.
func (c *Client) EnforceSeedLimits(t *torrent.Torrent, startedSeedingAt time.Time) {
	if c.cfg.SeedRatioLimit <= 0 && c.cfg.SeedTimeLimit <= 0 {
		return // unlimited seeding
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		stats := t.Stats()
		uploaded := stats.BytesWrittenData.Int64()
		size := t.Length()
		ratio := 0.0
		if size > 0 {
			ratio = float64(uploaded) / float64(size)
		}
		exceededRatio := c.cfg.SeedRatioLimit > 0 && ratio >= c.cfg.SeedRatioLimit
		exceededTime := c.cfg.SeedTimeLimit > 0 && time.Since(startedSeedingAt) >= c.cfg.SeedTimeLimit
		if exceededRatio || exceededTime {
			t.Drop()
			return
		}
	}
}
