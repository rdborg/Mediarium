// Package torrentclient wraps anacrolix/torrent
// ("torrent via anacrolix/torrent ... rather than reimplementing the
// BitTorrent wire protocol"). Unlike internal/download's NNTP client,
// there's no remote server to configure: the torrent engine runs embedded
// in this process, so "download client" settings here are local (listen
// port, seed limits, save path) rather than host/port/credentials.
package torrentclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"

	"github.com/rdborg/mediarium/internal/speed"
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

// TunnelPacket is the optional half of a tunnel that can carry UDP inside
// itself. ListenPacket must not bind a socket on the host network: every
// packet has to leave from the VPN address. A tunnel that only implements
// TunnelDialer keeps UDP trackers and DHT switched off.
type TunnelPacket interface {
	ListenPacket(network, addr string) (net.PacketConn, error)
}

// TunnelNames resolves a tracker or DHT hostname through the tunnel (the
// VPN's own DNS). It must not ask the host's resolver. An error fails the
// announce; nothing is then sent from the home address.
type TunnelNames interface {
	LookupHost(ctx context.Context, host string) ([]net.IP, error)
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

	// DiscoverThroughTunnel sends UDP tracker announces and DHT queries
	// through Tunnel. It does nothing unless Tunnel also implements
	// TunnelPacket. Off, those are skipped so the home address is never used.
	DiscoverThroughTunnel bool

	// PortMapping asks the router to open the listen port (UPnP, then
	// NAT-PMP). Never done with a Tunnel, whatever this says.
	PortMapping bool

	// AllowLocalAddresses lets trackers, web seeds and magnet metadata
	// addresses be on the home network or this machine. Off by default: those
	// addresses come from torrents anyone can write.
	AllowLocalAddresses bool
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

	pm *portMapper // asks the router to open the listen port; nil with a tunnel

	// closeDHT stops the DHT server that runs on a tunnel socket, when one
	// was started. Nil when discovery through the tunnel is off.
	closeDHT func()
}

// New starts the embedded torrent engine. When cfg.Tunnel is set, this is a
// real kill switch, not just "prefer the tunnel": DisableTCP/DisableUTP/
// NoDHT/NoDefaultPortForwarding/AcceptPeerConnections are all forced off so
// the client never binds a real host socket for peer traffic at all — there
// is no direct-connection path to silently fall back to if the tunnel goes
// down. The only dialer added is cfg.Tunnel itself, so a dropped tunnel
// simply fails every new dial rather than reverting to a direct one.
//
// uTP stays off: the engine would otherwise open a UDP socket on the host.
// DHT and UDP trackers can still run, but only on a socket the tunnel
// itself provides (DiscoverThroughTunnel), so replies come back to the VPN
// address. Without that, peer discovery uses HTTP trackers and PEX over
// the tunneled TCP connections.
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
	tcfg.Logger = engineLogger()
	// The download speed limit set in Settings, shared with Usenet.
	tcfg.DownloadRateLimiter = speed.Limiter()
	// The upload limit has its own setting.
	tcfg.UploadRateLimiter = speed.UploadLimiter()
	tcfg.Seed = true
	// Opening the port on the router is done here (see portMapper), not by the
	// engine, so its result can be shown.
	tcfg.NoDefaultPortForwarding = true
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
	tcfg.Callbacks.PeerConnReadExtensionMessage = append(tcfg.Callbacks.PeerConnReadExtensionMessage, guardMetadataMessage)
	applyNetworkPolicy(tcfg, cfg.Tunnel, cfg.AllowLocalAddresses, cfg.DiscoverThroughTunnel)
	// Replace the engine's bootstrap list before the client exists. The
	// default one resolves the DHT routers with the host's DNS.
	if cfg.DiscoverThroughTunnel {
		if names, ok := cfg.Tunnel.(TunnelNames); ok {
			if _, ok := cfg.Tunnel.(TunnelPacket); ok {
				tcfg.DhtStartingNodes = tunnelBootstrap(names)
			}
		}
	}
	tc, err := torrent.NewClient(tcfg)
	if err != nil {
		return nil, fmt.Errorf("create torrent client: %w", err)
	}
	if cfg.Tunnel != nil {
		tc.AddDialer(torrent.NetworkDialer{Network: "tcp", Dialer: cfg.Tunnel})
	}
	if cfg.DiscoverThroughTunnel {
		if err := c.enableTunnelDiscovery(tc); err != nil {
			tc.Close()
			return nil, err
		}
	}
	c.tc = tc
	if cfg.Tunnel == nil {
		c.pm = newPortMapper(tc.LocalPort)
		if cfg.PortMapping {
			c.pm.Start()
		}
	}
	return c, nil
}

// enableTunnelDiscovery starts DHT on a UDP socket that belongs to the
// tunnel. UDP tracker announces use the same kind of socket, set up in
// applyNetworkPolicy. A tunnel that cannot carry UDP leaves both off: no
// host socket is opened instead.
func (c *Client) enableTunnelDiscovery(tc *torrent.Client) error {
	udp, ok := c.cfg.Tunnel.(TunnelPacket)
	if !ok {
		return nil
	}
	if _, ok := c.cfg.Tunnel.(TunnelNames); !ok {
		return fmt.Errorf("dht through the vpn: the tunnel cannot look up names")
	}
	pc, err := udp.ListenPacket("udp4", ":0")
	if err != nil {
		return fmt.Errorf("dht through the vpn: %w", err)
	}
	ds, err := tc.NewAnacrolixDhtServer(pc)
	if err != nil {
		_ = pc.Close()
		return fmt.Errorf("dht through the vpn: %w", err)
	}
	tc.AddDhtServer(torrent.AnacrolixDhtServerWrapper{Server: ds})
	c.closeDHT = func() { ds.Close() }
	return nil
}

// SetPortMapping switches asking the router to open the listen port on or off.
// It has no effect on an engine that runs through a tunnel.
func (c *Client) SetPortMapping(on bool) {
	if c.pm == nil {
		return
	}
	if on {
		c.pm.Start()
	} else {
		c.pm.Stop()
	}
}

// PortMapping says whether the router opened the listen port.
func (c *Client) PortMapping() PortMapStatus {
	if c.pm == nil {
		return PortMapStatus{State: MapOff}
	}
	return c.pm.Status()
}

// Close stops the engine and every torrent in it.
func (c *Client) Close() {
	if c.pm != nil {
		c.pm.Stop()
	}
	if c.closeDHT != nil {
		c.closeDHT()
	}
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
	spec, err := torrent.TorrentSpecFromMagnetUri(magnetURI)
	if err != nil {
		return nil, fmt.Errorf("add magnet: %w", err)
	}
	c.resolveTrackers(ctx, spec)
	t, _, err := c.tc.AddTorrentSpec(spec)
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
	spec, err := torrent.TorrentSpecFromMetaInfoErr(mi)
	if err != nil {
		return nil, fmt.Errorf("add torrent file: %w", err)
	}
	c.resolveTrackers(ctx, spec)
	t, _, err := c.tc.AddTorrentSpec(spec)
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
	c.resolveTrackers(ctx, spec)
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
	announces.forget(t.InfoHash().HexString())
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

// MetadataTimeout is how long a magnet link may wait for a peer to send the
// torrent's details (file list, pieces) before ErrNoMetadata. A dead or fake
// magnet never gets them, and without a limit it would hold its place in the
// download line for good. A variable so tests can shorten it.
var MetadataTimeout = 10 * time.Minute

// ErrNoMetadata is returned when a magnet link's details never arrive.
var ErrNoMetadata = errors.New("no one sent the torrent's details within 10 minutes: no one seems to be sharing it")

func (c *Client) waitForInfo(ctx context.Context, t *torrent.Torrent) error {
	select {
	case <-t.GotInfo():
		return nil
	default:
	}
	timer := time.NewTimer(MetadataTimeout)
	defer timer.Stop()
	started := time.Now()
	logIn := waitLogFirst
	for {
		// A debug line shortly after the start and then once a minute, so the
		// log shows whether trackers answered and peers turned up, without
		// filling up while a magnet link waits.
		logTimer := time.NewTimer(logIn)
		select {
		case <-t.GotInfo():
			logTimer.Stop()
			return nil
		case <-ctx.Done():
			logTimer.Stop()
			return ctx.Err()
		case <-timer.C:
			logTimer.Stop()
			err := &NoMetadataError{Waited: MetadataTimeout, Diag: Diagnose(t)}
			logSwarm("torrent: gave up waiting for the torrent's details", t, "waited", time.Since(started).Round(time.Second).String())
			return err
		case <-logTimer.C:
			logSwarm("torrent: still waiting for the torrent's details", t, "waited", time.Since(started).Round(time.Second).String())
			logIn = waitLogEvery
		}
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
	return DownloadStalling(ctx, t, StallAfter, onProgress)
}

// DownloadStalling is Download with its own stall limit: how long the torrent
// may go without a byte before ErrStalled. Zero means never give up.
func DownloadStalling(ctx context.Context, t *torrent.Torrent, stallAfter time.Duration, onProgress ProgressFunc) error {
	select {
	case <-t.GotInfo():
	case <-ctx.Done():
		return ctx.Err()
	}
	return NewControl(t).Download(ctx, stallAfter, onProgress)
}

// StallAfter is how long a torrent may go without receiving a single byte
// before it counts as stalled (no one is sharing it). A variable so tests can
// shorten it.
var StallAfter = 2 * time.Hour

// ErrStalled is returned by Download for a torrent that stopped getting data.
var ErrStalled = errors.New("the torrent got no data for 2 hours: no one seems to be sharing it")

// SeedGoal is when seeding a finished torrent may stop:
// once it has uploaded Ratio times its size, or after Time of seeding,
// whichever comes first. Zero values mean no limit of that kind; with
// neither set a torrent seeds until it is stopped. NoSeed is the goal of a
// torrent that is not to be shared at all: it is met from the start.
type SeedGoal struct {
	Ratio  float64
	Time   time.Duration
	NoSeed bool
}

// GoalOf makes the goal from the two limits as the settings hold them: a
// negative one is no limit of that kind, 0 is "do not seed" (either of the two
// being 0 is enough), and a positive one is the limit.
func GoalOf(ratio, hours float64) SeedGoal {
	if ratio == 0 || hours == 0 {
		return SeedGoal{NoSeed: true}
	}
	var g SeedGoal
	if ratio > 0 {
		g.Ratio = ratio
	}
	if hours > 0 {
		g.Time = time.Duration(hours * float64(time.Hour))
	}
	return g
}

// Unlimited reports whether the goal can never be met.
func (g SeedGoal) Unlimited() bool { return !g.NoSeed && g.Ratio <= 0 && g.Time <= 0 }

// Met reports whether a torrent of size bytes that has uploaded uploaded
// bytes over seeding time has reached the goal.
func (g SeedGoal) Met(uploaded, size int64, seeding time.Duration) bool {
	if g.NoSeed {
		return true
	}
	if g.Ratio > 0 && size > 0 && float64(uploaded)/float64(size) >= g.Ratio {
		return true
	}
	return g.Time > 0 && seeding >= g.Time
}

// SeedUntilGoal keeps t seeding until goal is met, checking every interval,
// and reports true when it was; it returns false as soon as ctx is done or
// t is removed. It does not stop t: the caller does that (Remove).
func SeedUntilGoal(ctx context.Context, t *torrent.Torrent, startedAt time.Time, goal SeedGoal, interval time.Duration) bool {
	return SeedUntilGoalOf(ctx, t, t.Length(), startedAt, goal, interval)
}

// SeedUntilGoalOf is SeedUntilGoal for a torrent of which only size bytes were
// downloaded (some files left out): the ratio counts against that size.
func SeedUntilGoalOf(ctx context.Context, t *torrent.Torrent, size int64, startedAt time.Time, goal SeedGoal, interval time.Duration) bool {
	check := func() bool {
		stats := t.Stats()
		return goal.Met(stats.BytesWrittenData.Int64(), size, time.Since(startedAt))
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
