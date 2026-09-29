// Package torrentclient wraps anacrolix/torrent
// ("torrent via anacrolix/torrent ... rather than reimplementing the
// BitTorrent wire protocol"). Unlike internal/download's NNTP client,
// there's no remote server to configure: the torrent engine runs embedded
// in this process, so "download client" settings here are local (listen
// port, seed limits, save path) rather than host/port/credentials.
package torrentclient

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"
)

// DefaultListenPort is the port the engine listens on for incoming peer
// connections (TCP and UDP) unless Settings say otherwise. It sits in the
// dynamic range (49152-65535) that no service is ever assigned, and echoes
// the web interface's 8264.
const DefaultListenPort = 58264

// TunnelDialer is satisfied by *vpn.Tunnel. Declared narrowly here rather
// than importing internal/vpn, per the project's one-way package dependency
// rule (feature packages shouldn't import sideways into each other) — api
// is the only package that needs to know about both and wires them
// together.
type TunnelDialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// Config are the local engine settings ("seed ratio/time
// limits, category-based save paths").
type Config struct {
	// DataDir is where torrents added without a folder of their own
	// (AddMagnet, AddTorrentFile) are saved.
	DataDir    string
	ListenPort int // 0 = let the OS pick

	// Tunnel, when set, routes every peer connection through it exclusively
	// instead of the host network. See New's
	// doc comment for exactly what that does and doesn't cover.
	Tunnel TunnelDialer
}

// Client is one running torrent engine. Many torrents share it, each saved
// in its own folder (AddMagnetIn, AddTorrentFileIn), so the whole app
// listens on one port however many torrents are active.
type Client struct {
	tc  *torrent.Client
	cfg Config

	mu       sync.Mutex
	storages map[*torrent.Torrent]storage.ClientImplCloser

	// lastIncoming is when a peer last connected to us (UnixNano; 0 = never):
	// proof that the listen port is reachable from outside.
	lastIncoming atomic.Int64
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
	c := &Client{cfg: cfg, storages: map[*torrent.Torrent]storage.ClientImplCloser{}}

	tcfg := torrent.NewDefaultClientConfig()
	tcfg.DataDir = cfg.DataDir
	// Piece completion is kept in memory: nothing is written to DataDir
	// until a torrent is actually saved there.
	tcfg.DefaultStorage = storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir:   cfg.DataDir,
		PieceCompletion: storage.NewMapPieceCompletion(),
	})
	tcfg.ListenPort = cfg.ListenPort
	tcfg.Seed = true
	if cfg.Tunnel != nil {
		tcfg.DisableTCP = true
		tcfg.DisableUTP = true
		tcfg.NoDHT = true
		tcfg.NoDefaultPortForwarding = true
		tcfg.AcceptPeerConnections = false
	}
	tcfg.Callbacks.PeerConnAdded = append(tcfg.Callbacks.PeerConnAdded, func(pc *torrent.PeerConn) {
		if pc.Discovery == torrent.PeerSourceIncoming {
			c.lastIncoming.Store(time.Now().UnixNano())
		}
	})
	tc, err := torrent.NewClient(tcfg)
	if err != nil {
		return nil, fmt.Errorf("create torrent client: %w", err)
	}
	if cfg.Tunnel != nil {
		tc.AddDialer(torrent.NetworkDialer{Network: "tcp", Dialer: cfg.Tunnel})
	}
	c.tc = tc
	return c, nil
}

// Close stops the engine and every torrent in it.
func (c *Client) Close() {
	c.tc.Close()
	c.mu.Lock()
	defer c.mu.Unlock()
	for t, st := range c.storages {
		_ = st.Close()
		delete(c.storages, t)
	}
}

// ListenAddrs reports the client's real host listen addresses, if any —
// empty when the client is running in tunnel-only mode (see New).
func (c *Client) ListenAddrs() []net.Addr {
	return c.tc.ListenAddrs()
}

// ListenPort is the port actually listened on (0 when nothing is, as in
// tunnel-only mode).
func (c *Client) ListenPort() int {
	return c.tc.LocalPort()
}

// LastIncoming is when another peer last connected to this engine on its
// own initiative (zero if never): proof the listen port can be reached from
// the internet.
func (c *Client) LastIncoming() time.Time {
	n := c.lastIncoming.Load()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

// AddMagnet adds a torrent from a magnet URI, saved in Config.DataDir, and
// blocks until its metadata (piece layout, file list) is available.
func (c *Client) AddMagnet(ctx context.Context, magnetURI string) (*torrent.Torrent, error) {
	t, err := c.tc.AddMagnet(magnetURI)
	if err != nil {
		return nil, fmt.Errorf("add magnet: %w", err)
	}
	return t, c.waitForInfo(ctx, t)
}

// AddTorrentFile adds a torrent from a local .torrent file, saved in
// Config.DataDir.
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

// AddMagnetIn is AddMagnet saving the torrent's files in dir.
func (c *Client) AddMagnetIn(ctx context.Context, magnetURI, dir string) (*torrent.Torrent, error) {
	spec, err := torrent.TorrentSpecFromMagnetUri(magnetURI)
	if err != nil {
		return nil, fmt.Errorf("add magnet: %w", err)
	}
	return c.addSpecIn(ctx, spec, dir)
}

// AddTorrentFileIn is AddTorrentFile saving the torrent's files in dir.
func (c *Client) AddTorrentFileIn(ctx context.Context, path, dir string) (*torrent.Torrent, error) {
	mi, err := torrentMetaInfoFromFile(path)
	if err != nil {
		return nil, err
	}
	spec, err := torrent.TorrentSpecFromMetaInfoErr(mi)
	if err != nil {
		return nil, fmt.Errorf("add torrent file: %w", err)
	}
	return c.addSpecIn(ctx, spec, dir)
}

func (c *Client) addSpecIn(ctx context.Context, spec *torrent.TorrentSpec, dir string) (*torrent.Torrent, error) {
	st := storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir:   dir,
		PieceCompletion: storage.NewMapPieceCompletion(),
	})
	spec.Storage = st
	t, isNew, err := c.tc.AddTorrentSpec(spec)
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("add torrent: %w", err)
	}
	if !isNew {
		// The same torrent is already running (saved elsewhere): two copies
		// of one torrent cannot share an engine.
		_ = st.Close()
		return nil, fmt.Errorf("this torrent is already being downloaded")
	}
	c.mu.Lock()
	c.storages[t] = st
	c.mu.Unlock()
	if err := c.waitForInfo(ctx, t); err != nil {
		c.Remove(t)
		return nil, err
	}
	return t, nil
}

// Remove stops t (downloading and seeding) and releases its files, which
// stay on disk.
func (c *Client) Remove(t *torrent.Torrent) {
	t.Drop()
	c.mu.Lock()
	st := c.storages[t]
	delete(c.storages, t)
	c.mu.Unlock()
	if st != nil {
		_ = st.Close()
	}
}

// Torrents is how many torrents the engine is running.
func (c *Client) Torrents() int {
	return len(c.tc.Torrents())
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
// that's SeedUntilGoal's job, run separately so a caller can return
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

// SeedGoal is when seeding a finished torrent may stop:
// once it has uploaded Ratio times its size, or after Time of seeding,
// whichever comes first. Zero values mean no limit of that kind; with
// neither set a torrent seeds until it is stopped.
type SeedGoal struct {
	Ratio float64
	Time  time.Duration
}

// Unlimited reports whether the goal can never be met.
func (g SeedGoal) Unlimited() bool { return g.Ratio <= 0 && g.Time <= 0 }

// Met reports whether a torrent of size bytes that has uploaded uploaded
// bytes over seeding time has reached the goal.
func (g SeedGoal) Met(uploaded, size int64, seeding time.Duration) bool {
	if g.Ratio > 0 && size > 0 && float64(uploaded)/float64(size) >= g.Ratio {
		return true
	}
	return g.Time > 0 && seeding >= g.Time
}

// SeedUntilGoal keeps t seeding until goal is met, checking every interval,
// and reports true when it was; it returns false as soon as ctx is done or
// t is removed. It does not stop t: the caller does that (Remove).
func SeedUntilGoal(ctx context.Context, t *torrent.Torrent, startedAt time.Time, goal SeedGoal, interval time.Duration) bool {
	check := func() bool {
		stats := t.Stats()
		return goal.Met(stats.BytesWrittenData.Int64(), t.Length(), time.Since(startedAt))
	}
	if !goal.Unlimited() && check() {
		return true
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-t.Closed():
			return false
		case <-ticker.C:
			if !goal.Unlimited() && check() {
				return true
			}
		}
	}
}
